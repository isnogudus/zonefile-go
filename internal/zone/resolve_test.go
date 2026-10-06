package zone

import (
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/isnogudus/zonefile-go/internal/config"
)

func resolve(t *testing.T, src string) ([]*Zone, error) {
	t.Helper()
	cfg, err := config.Parse("test.conf", strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	zones, _, err := Resolve(cfg)
	return zones, err
}

func mustResolve(t *testing.T, src string) []*Zone {
	t.Helper()
	zones, err := resolve(t, src)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return zones
}

func find(zones []*Zone, name string) *Zone {
	for _, z := range zones {
		if z.Name == name {
			return z
		}
	}
	return nil
}

func addrsOf(z *Zone, name string) []string {
	var out []string
	for _, a := range z.Addresses {
		if a.Name == name {
			out = append(out, a.Addr.String())
		}
	}
	return out
}

func ptrTarget(z *Zone, addr string) string {
	for _, p := range z.PTRs {
		if p.Addr == netip.MustParseAddr(addr) {
			return p.Target
		}
	}
	return ""
}

func TestResolveExample(t *testing.T) {
	cfg, err := config.ParseFile("../../examples/zones.conf")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	zones, warnings, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("example has warnings:\n%v", warnings)
	}

	var names []string
	for _, z := range zones {
		names = append(names, z.Name)
	}
	want := []string{
		"internal.example.com.", "apps.example.com.", "example.com.",
		"dmz.example.com.", "iot.example.com.", "devices.example.com.",
		"cluster.example.com.",
		"168.192.in-addr.arpa.",
		"0.0.0.1.8.7.6.5.4.3.2.1.0.0.d.f.ip6.arpa.",
		"0.0.0.2.8.7.6.5.4.3.2.1.0.0.d.f.ip6.arpa.",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("zones:\ngot  %v\nwant %v", names, want)
	}

	ex := find(zones, "example.com.")
	if got := addrsOf(ex, "homeassistant.example.com."); !reflect.DeepEqual(got, []string{"192.168.21.37"}) {
		t.Errorf("example.com homeassistant = %v (no inet6)", got)
	}
	if got := addrsOf(ex, "mqtt.example.com."); !reflect.DeepEqual(got, []string{"192.168.21.1", "fd00:1234:5678:1000::1"}) {
		t.Errorf("mqtt alias = %v", got)
	}
	if got := ex.SOA.Email; got != "admin.example.com." {
		t.Errorf("rname = %q", got)
	}
	if mx := ex.MX[0]; mx.Name != "mail.example.com." || mx.Priority != 10 {
		t.Errorf("mx = %+v", mx)
	}

	apps := find(zones, "apps.example.com.")
	if got := addrsOf(apps, "homeassistant.apps.example.com."); !reflect.DeepEqual(got, []string{"192.168.21.37", "fd00:1234:5678:1000::25"}) {
		t.Errorf("apps homeassistant = %v", got)
	}
	if got := addrsOf(apps, "*.apps.example.com."); len(got) != 2 {
		t.Errorf("wildcard alias = %v", got)
	}

	if len(apps.MX) != 0 || len(find(zones, "internal.example.com.").MX) != 0 {
		t.Errorf("zones with no mx got MX records: %+v", apps.MX)
	}
	if mx := find(zones, "iot.example.com.").MX; len(mx) != 1 || mx[0].Name != "mail.example.com." || mx[0].Priority != 10 {
		t.Errorf("iot inherits the top-level mx: %+v", mx)
	}

	cl := find(zones, "cluster.example.com.")
	if got := addrsOf(cl, "mailserver.cluster.example.com."); !reflect.DeepEqual(got, []string{"192.168.93.67", "fd00:1234:5678:2000::43"}) {
		t.Errorf("mailserver = %v", got)
	}

	v4 := find(zones, "168.192.in-addr.arpa.")
	for addr, target := range map[string]string{
		"192.168.21.37":  "homeassistant.example.com.", // apps has no ptr
		"192.168.21.1":   "router.example.com.",
		"192.168.21.5":   "docker.example.com.",
		"192.168.93.67":  "mailserver.cluster.example.com.",
		"192.168.201.30": "relay-01.devices.example.com.",
	} {
		if got := ptrTarget(v4, addr); got != target {
			t.Errorf("PTR %s = %q, want %q", addr, got, target)
		}
	}
	if !slicesSorted(v4.PTRs) {
		t.Error("PTRs not sorted by address")
	}
	if p := v4.PTRs[0]; p.Name != "1.21.168.192.in-addr.arpa." {
		t.Errorf("first PTR name = %q", p.Name)
	}

	v6 := find(zones, "0.0.0.2.8.7.6.5.4.3.2.1.0.0.d.f.ip6.arpa.")
	if got := ptrTarget(v6, "fd00:1234:5678:2000::43"); got != "mailserver.cluster.example.com." {
		t.Errorf("v6 PTR = %q", got)
	}
	for _, z := range zones {
		if !z.Reverse {
			continue
		}
		for _, p := range z.PTRs {
			if p.Addr.String() == "203.0.113.1" || p.Addr.String() == "fd00:1234:5678:3000::1" {
				t.Errorf("PTR for %s outside the reverse networks", p.Addr)
			}
		}
	}
}

func slicesSorted(ptrs []PTR) bool {
	for i := 1; i < len(ptrs); i++ {
		if ptrs[i-1].Addr.Compare(ptrs[i].Addr) >= 0 {
			return false
		}
	}
	return true
}

func TestResolveDefaults(t *testing.T) {
	zones := mustResolve(t, `
email john.doe@example.com
ttl 1h
nameserver ns1.example.com.
mx mail.example.com.

reverse 10.0.0.0/24 {
	ttl 1d
}

zone example.com {
	serial 7
	host a 10.0.0.1 ttl 5m
	host b 10.0.0.2 no ptr
	srv _x._tcp a port 1 weight 3
}
`)
	z := zones[0]
	if z.TTL != 3600 || z.SOA.Email != `john\.doe.example.com.` || *z.SOA.Serial != 7 ||
		z.SOA.Refresh != DefaultRefresh || z.SOA.Minimum != DefaultNegativeTTL {
		t.Errorf("zone = %+v, soa = %+v", z, z.SOA)
	}
	if want := []MX{{Name: "mail.example.com.", Priority: DefaultMXPriority, TTL: 3600}}; !reflect.DeepEqual(z.MX, want) {
		t.Errorf("mx = %+v", z.MX)
	}
	if want := []NS{{Name: "ns1.example.com.", TTL: 3600}}; !reflect.DeepEqual(z.NS, want) {
		t.Errorf("ns = %+v", z.NS)
	}
	if want := (SRV{Name: "_x._tcp.example.com.", Target: "a.example.com.", Port: 1, Priority: DefaultSRVPriority, Weight: 3, TTL: 3600}); z.SRVs[0] != want {
		t.Errorf("srv = %+v", z.SRVs[0])
	}

	rz := zones[1]
	if rz.Name != "0.0.10.in-addr.arpa." || rz.TTL != 86400 || rz.SOA.Serial != nil {
		t.Errorf("reverse zone = %+v", rz)
	}
	want := []PTR{{Name: "1.0.0.10.in-addr.arpa.", Addr: netip.MustParseAddr("10.0.0.1"), Target: "a.example.com.", TTL: 300}}
	if !reflect.DeepEqual(rz.PTRs, want) {
		t.Errorf("ptrs = %+v", rz.PTRs)
	}
}

func TestApplySuffix(t *testing.T) {
	tests := []struct {
		net    string
		suffix []byte
		want   string
		err    string
	}{
		{"192.168.21.0/24", []byte{37}, "192.168.21.37", ""},
		{"192.168.0.0/16", []byte{21, 37}, "192.168.21.37", ""},
		{"192.168.0.0/16", []byte{37}, "192.168.0.37", ""},
		{"10.0.16.0/20", []byte{1, 5}, "10.0.17.5", ""},
		{"fd00:1234:5678:1000::/64", []byte{37}, "fd00:1234:5678:1000::25", ""},
		{"fd00::/64", []byte{200}, "fd00::c8", ""},
		{"fd00::/64", []byte{21, 37}, "fd00::1525", ""},
		{"192.168.21.0/24", []byte{1, 2}, "", "does not fit"},
		{"10.0.16.0/20", []byte{16, 5}, "", "does not fit"},
		{"192.168.21.0/24", []byte{0}, "", "network or broadcast"},
		{"192.168.21.0/24", []byte{255}, "", "network or broadcast"},
		{"192.168.21.4/31", []byte{1}, "192.168.21.5", ""},
	}
	for _, tt := range tests {
		got, err := applySuffix(netip.MustParsePrefix(tt.net), tt.suffix)
		switch {
		case tt.err != "":
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("%s %v: err = %v, want %q", tt.net, tt.suffix, err, tt.err)
			}
		case err != nil:
			t.Errorf("%s %v: %v", tt.net, tt.suffix, err)
		case got.String() != tt.want:
			t.Errorf("%s %v = %s, want %s", tt.net, tt.suffix, got, tt.want)
		}
	}
}

func TestValidName(t *testing.T) {
	for _, n := range []string{"example.com.", "*.example.com.", "_mqtt._tcp.example.com.", "a-b.c1.", "123.example.com."} {
		if err := validName(n); err != nil {
			t.Errorf("validName(%q) = %v", n, err)
		}
	}
	for _, n := range []string{"a..b.", "-a.b.", "a-.b.", "a.*.b.", "a*.b.", "a b.", "ä.b.", strings.Repeat("a", 64) + ".b."} {
		if err := validName(n); err == nil {
			t.Errorf("validName(%q) = nil, want error", n)
		}
	}
}

func TestResolveErrors(t *testing.T) {
	const head = "email admin@example.com\nnameserver ns1.example.com.\n"
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"no email", "nameserver ns.example.com.\nzone a.example {\n}\n", []string{
			`test.conf:2: zone a.example. has no e-mail address, add "email"`}},
		{"no nameserver", "email a@example.com\nzone a.example {\n}\n", []string{
			`test.conf:2: zone a.example. has no nameserver`}},
		{"retry", head + "zone a.example {\n\tretry 3h\n}\n", []string{
			`test.conf:3: zone a.example.: retry (10800) must be less than refresh (7200)`}},
		{"duplicate zone", head + "zone a.example {\n}\nzone A.example. {\n}\n", []string{
			`test.conf:5: zone A.example. already declared at test.conf:3`}},
		{"suffix without network", head + "zone a.example {\n\thost x .5\n}\n", []string{
			`test.conf:4: host x: address suffix .5 needs a network statement in the zone`}},
		{"suffix too large", head + "zone a.example {\n\tnetwork 10.0.0.0/24\n\thost x .1.5\n}\n", []string{
			`test.conf:5: host x: suffix .1.5 does not fit into the host part of 10.0.0.0/24`}},
		{"nothing left", head + "zone a.example {\n\tnetwork 10.0.0.0/24\n\thost x .5 no inet\n}\n", []string{
			`test.conf:5: host x has no addresses left`}},
		{"duplicate host", head + "zone a.example {\n\thost x 10.0.0.1\n\thost x 10.0.0.2\n}\n", []string{
			`test.conf:5: host x.a.example. already declared at test.conf:4`}},
		{"duplicate record", head + "zone a.example {\n\thost mqtt 10.0.0.1 no ptr\n\thost router 10.0.0.1 alias mqtt\n}\n", []string{
			`test.conf:5: mqtt.a.example. 10.0.0.1 duplicates the record from test.conf:4`}},
		{"cname conflict", head + "zone a.example {\n\thost www 10.0.0.1\n\tcname www other\n}\n", []string{
			`test.conf:5: cname www.a.example. conflicts with other records for that name at test.conf:4`}},
		{"cname at apex", head + "zone a.example {\n\tcname @ other\n}\n", []string{
			`test.conf:4: cname a.example. conflicts with other records for that name at test.conf:3`}},
		{"bad label", head + "zone a.example {\n\thost -x 10.0.0.1\n}\n", []string{
			`test.conf:4: label "-x" in "-x.a.example." starts or ends with a hyphen`}},
		{"unaligned reverse", head + "reverse 10.0.0.0/20\n", []string{
			`test.conf:3: reverse network 10.0.0.0/20: prefix length must be a multiple of 8`}},
		{"overlapping reverse", head + "reverse { 10.0.0.0/8 10.1.0.0/16 }\n", []string{
			`test.conf:3: reverse networks 10.1.0.0/16 and 10.0.0.0/8 overlap`}},
		{"two PTRs", head + "reverse 10.0.0.0/8\nzone a.example {\n\thost x 10.0.0.1\n}\nzone b.example {\n\thost y 10.0.0.1\n}\n", []string{
			`test.conf:8: 10.0.0.1 already gets a PTR to x.a.example. from test.conf:5, add "no ptr" to one of the hosts`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolve(t, tt.src)
			var list config.ErrorList
			if !errors.As(err, &list) {
				t.Fatalf("err = %v, want ErrorList", err)
			}
			var got []string
			for _, e := range list {
				got = append(got, e.Error())
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(tt.want, "\n  "))
			}
		})
	}
}

func TestResolvePTRInheritance(t *testing.T) {
	zones := mustResolve(t, `
email admin@example.com
nameserver ns1.example.com.
mx mail.example.com.
no ptr
reverse 10.0.0.0/8
zone a.example {
	host x 10.0.0.1
	host y 10.0.0.2 ptr
}
zone b.example {
	ptr
	no mx
	host z 10.0.0.3
}
`)
	var targets []string
	for _, p := range zones[2].PTRs {
		targets = append(targets, p.Target)
	}
	if want := []string{"y.a.example.", "z.b.example."}; !reflect.DeepEqual(targets, want) {
		t.Errorf("PTR targets = %v, want %v", targets, want)
	}
	if len(zones[0].MX) != 1 || len(zones[1].MX) != 0 {
		t.Errorf("MX: a = %+v, b = %+v", zones[0].MX, zones[1].MX)
	}
}

func TestResolvePTROutsideReverse(t *testing.T) {
	// The same address in two zones is fine if no reverse network covers it.
	mustResolve(t, `
email admin@example.com
nameserver ns1.example.com.
zone a.example {
	host x 203.0.113.1
}
zone b.example {
	host y 203.0.113.1
}
`)
}

func TestLooksAbsolute(t *testing.T) {
	tests := []struct {
		name, origin, want string
	}{
		{"mail.home.arpa", "h.example.net.", "ends in the top-level domain arpa"},
		{"mail.h.example.net", "h.example.net.", "repeats the zone name"},
		{"H.Example.Net", "h.example.net.", "repeats the zone name"},
		{"backup-mx.example.net", "example.com.", "ends in the top-level domain net"},
		{"printer.local", "example.com.", "ends in the top-level domain local"},
		{"www.example.de", "example.com.", "ends in the top-level domain de"},
		{"mail.home.arpa.", "h.example.net.", ""},
		{"mail", "h.example.net.", ""},
		{"@", "h.example.net.", ""},
		{"_imaps._tcp", "example.com.", ""},
		{"db.services", "example.com.", ""},
		{"node1.cluster", "example.com.", ""},
		{"a.b2", "example.com.", ""},
	}
	for _, tt := range tests {
		if got := looksAbsolute(tt.name, tt.origin); got != tt.want {
			t.Errorf("looksAbsolute(%q, %q) = %q, want %q", tt.name, tt.origin, got, tt.want)
		}
	}
}

func TestResolveWarnings(t *testing.T) {
	cfg, err := config.Parse("test.conf", strings.NewReader(`
email admin@example.com
nameserver ns1.home.arpa.
zone h.example.net {
	host mail.h.example.net 192.0.2.1
	cname mail mail.home.arpa
	cname ok mail.home.arpa.
	mx mail
}
`))
	if err != nil {
		t.Fatal(err)
	}
	zones, warnings, err := Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, w := range warnings {
		got = append(got, w.Error())
	}
	want := []string{
		`test.conf:5: warning: "mail.h.example.net" repeats the zone name but is relative, so it becomes mail.h.example.net.h.example.net.; add a trailing dot if you mean mail.h.example.net.`,
		`test.conf:6: warning: "mail.home.arpa" ends in the top-level domain arpa but is relative, so it becomes mail.home.arpa.h.example.net.; add a trailing dot if you mean mail.home.arpa.`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	// Warnings do not change the result.
	if c := zones[0].CNAMEs[0]; c.Target != "mail.home.arpa.h.example.net." {
		t.Errorf("cname target = %q", c.Target)
	}
}
