package config

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) *Config {
	t.Helper()
	cfg, err := Parse("test.conf", strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return cfg
}

func ptr[T any](v T) *T { return &v }

func TestParseExample(t *testing.T) {
	cfg, err := ParseFile("../../examples/zones.conf")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if got := *cfg.Options.Email; got != "admin@example.com" {
		t.Errorf("email = %q", got)
	}
	if len(cfg.Nameservers) != 1 || cfg.Nameservers[0].Name != "ns1.example.com." {
		t.Errorf("nameservers = %+v", cfg.Nameservers)
	}
	if len(cfg.Reverse) != 1 || len(cfg.Reverse[0].Networks) != 3 {
		t.Fatalf("reverse = %+v", cfg.Reverse)
	}

	wantHosts := map[string]int{
		"internal.example.com": 1,
		"apps.example.com":     5,
		"example.com":          26,
		"dmz.example.com":      10,
		"iot.example.com":      3,
		"devices.example.com":  43,
		"cluster.example.com":  8,
	}
	if len(cfg.Zones) != len(wantHosts) {
		t.Fatalf("got %d zones, want %d", len(cfg.Zones), len(wantHosts))
	}
	for _, z := range cfg.Zones {
		if got := len(z.Hosts); got != wantHosts[z.Name] {
			t.Errorf("zone %s: %d hosts, want %d", z.Name, got, wantHosts[z.Name])
		}
	}

	// The $lan macro expands to both networks.
	ex := cfg.Zones[2]
	want := []netip.Prefix{
		netip.MustParsePrefix("192.168.21.0/24"),
		netip.MustParsePrefix("fd00:1234:5678:1000::/64"),
	}
	if !reflect.DeepEqual(ex.Networks, want) {
		t.Errorf("example.com networks = %v", ex.Networks)
	}
	if len(ex.SRVs) != 5 || ex.SRVs[2].Port != 1883 {
		t.Errorf("example.com srv = %+v", ex.SRVs)
	}
	if mx := ex.MX[0]; mx.Name != "mail" || *mx.Priority != 10 {
		t.Errorf("example.com mx = %+v", ex.MX)
	}
}

func TestParseHost(t *testing.T) {
	cfg := mustParse(t, `
zone example.com {
	network { 192.168.21.0/24 fd00::/64 }
	host router { .1 2001:db8::1 } alias { @ www } ttl 1h no ptr no inet6
}
`)
	got := cfg.Zones[0].Hosts[0]
	want := Host{
		Pos:  Pos{File: "test.conf", Line: 4},
		Name: "router",
		Addrs: []HostAddr{
			{Suffix: []byte{1}},
			{Addr: netip.MustParseAddr("2001:db8::1")},
		},
		Aliases: []string{"@", "www"},
		TTL:     ptr(uint32(3600)),
		PTR:     ptr(false),
		NoInet6: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestParseOptions(t *testing.T) {
	cfg := mustParse(t, `
email "hostmaster@example.com"
ttl 3h
refresh 2h
retry 3600
expire 2w
negative-ttl 0
serial 2026100400
no ptr

reverse 10.0.0.0/8 {
	ttl 1d
	nameserver { ns1.example.com. ns2.example.com. } ttl 5m
}
`)
	want := Options{
		Email:       ptr("hostmaster@example.com"),
		TTL:         ptr(uint32(10800)),
		Refresh:     ptr(uint32(7200)),
		Retry:       ptr(uint32(3600)),
		Expire:      ptr(uint32(1209600)),
		NegativeTTL: ptr(uint32(0)),
		Serial:      ptr(uint32(2026100400)),
		PTR:         ptr(false),
	}
	if !reflect.DeepEqual(cfg.Options, want) {
		t.Errorf("options:\ngot  %+v\nwant %+v", cfg.Options, want)
	}
	r := cfg.Reverse[0]
	if *r.Options.TTL != 86400 {
		t.Errorf("reverse ttl = %d", *r.Options.TTL)
	}
	if len(r.Nameservers) != 2 || *r.Nameservers[1].TTL != 300 {
		t.Errorf("reverse nameservers = %+v", r.Nameservers)
	}
}

func TestParseMacros(t *testing.T) {
	cfg := mustParse(t, `
v4   = 192.168.1.0/24
nets = "{ $v4 fd00::/64 }"
alt  = { www, ftp }
zone example.com {
	network $nets
	host a .1 alias $alt
}
`)
	z := cfg.Zones[0]
	if len(z.Networks) != 2 || z.Networks[0].String() != "192.168.1.0/24" {
		t.Errorf("networks = %v", z.Networks)
	}
	if got := z.Hosts[0].Aliases; !reflect.DeepEqual(got, []string{"www", "ftp"}) {
		t.Errorf("aliases = %v", got)
	}
	if got := z.Hosts[0].Pos.Line; got != 7 {
		t.Errorf("host line = %d, want 7", got)
	}
}

func TestParseInclude(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.conf", "include \"common.conf\"\nzone example.com {\n\thost a $addr\n}\n")
	write("common.conf", "email admin@example.com\naddr = 192.0.2.1") // no final newline

	cfg, err := ParseFile(filepath.Join(dir, "main.conf"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if *cfg.Options.Email != "admin@example.com" {
		t.Errorf("email = %q", *cfg.Options.Email)
	}
	if got := cfg.Zones[0].Hosts[0].Addrs[0].Addr.String(); got != "192.0.2.1" {
		t.Errorf("addr = %s", got)
	}

	write("loop.conf", "include \"loop.conf\"\n")
	_, err = ParseFile(filepath.Join(dir, "loop.conf"))
	if err == nil || !strings.Contains(err.Error(), "include loop") {
		t.Errorf("include loop: err = %v", err)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"unknown statement", "frobnicate\n", []string{
			`test.conf:1: unknown statement "frobnicate"`}},
		{"unknown option", "tll 3h\n", []string{
			`test.conf:1: unknown statement "tll"`}},
		{"option twice", "ttl 1h\nttl 2h\n", []string{
			`test.conf:2: "ttl" given twice`}},
		{"ttl zero", "ttl 0\n", []string{
			`test.conf:1: duration "0" out of range (1s-2147483647s)`}},
		{"sub-second ttl", "ttl 1500ms\n", []string{
			`test.conf:1: duration "1500ms" must be whole seconds`}},
		{"setting after block", "zone a {\n}\nttl 1h\n", []string{
			`test.conf:3: "ttl" must come before the first zone or reverse block`}},
		{"zone setting after record", "zone a {\n\thost x 10.0.0.1\n\tttl 1h\n}\n", []string{
			`test.conf:3: "ttl" must come before the first record`}},
		{"set", "set ttl 3h\n", []string{
			`test.conf:1: "set" is not supported, write "ttl ..." without it`}},
		{"mx-priority", "mx-priority 10\n", []string{
			`test.conf:1: "mx-priority" is not supported, give the priority on each mx`}},
		{"srv-weight", "zone a {\n\tsrv-weight 1\n}\n", []string{
			`test.conf:2: "srv-weight" is not supported, give priority and weight on each srv`}},
		{"email without at", "email admin.example.com\n", []string{
			`test.conf:1: invalid e-mail address "admin.example.com": missing "@"`}},
		{"email numeric tld", "email a@example.123\n", []string{
			`test.conf:1: invalid e-mail address "a@example.123": top-level domain is all digits`}},
		{"undefined macro", "nameserver $ns\n", []string{
			`test.conf:1: undefined macro "$ns"`}},
		{"keyword macro", "host = 1\n", []string{
			`test.conf:1: "host" is a keyword and cannot be used as a macro name`}},
		{"relative top-level nameserver", "nameserver ns1\n", []string{
			`test.conf:1: nameserver "ns1" must be absolute (end with a dot) here`}},
		{"host bits", "reverse 192.168.1.1/24\n", []string{
			`test.conf:1: network 192.168.1.1/24 has host bits set, did you mean 192.168.1.0/24?`}},
		{"bad suffix", "zone a {\n\thost x .256\n}\n", []string{
			`test.conf:2: invalid address suffix ".256"`}},
		{"no inet both", "zone a {\n\thost x .1 no inet no inet6\n}\n", []string{
			`test.conf:2: host "x": no inet and no inet6 leave no addresses`}},
		{"second network", "zone a {\n\tnetwork { 10.0.0.0/8 192.168.0.0/16 }\n}\n", []string{
			`test.conf:2: zone already has an IPv4 network (10.0.0.0/8)`}},
		{"network after host", "zone a {\n\thost x 10.0.0.1\n\tnetwork 10.0.0.0/8\n}\n", []string{
			`test.conf:3: network must come before the first host`}},
		{"srv name", "zone a {\n\tsrv mqtt._tcp x port 1\n}\n", []string{
			`test.conf:2: srv: "mqtt._tcp" must start with _service._proto`}},
		{"srv without port", "zone a {\n\tsrv _a._tcp x\n}\n", []string{
			`test.conf:2: expected "port", got end of line`}},
		{"ptr in reverse", "reverse 10.0.0.0/8 {\n\tptr\n}\n", []string{
			`test.conf:2: "ptr" is not allowed in a reverse block`}},
		{"ptr twice", "ptr\nno ptr\n", []string{
			`test.conf:2: "ptr" given twice`}},
		{"ptr after block", "zone a {\n}\nno ptr\n", []string{
			`test.conf:3: "no ptr" must come before the first zone or reverse block`}},
		{"no mx at top level", "no mx\n", []string{
			`test.conf:1: "no mx" is only allowed in a zone`}},
		{"no what", "zone a {\n\tno cname\n}\n", []string{
			`test.conf:2: expected "ptr", "mx" or "dhcp" after "no", got "cname"`}},
		{"zone ptr after record", "zone a {\n\thost x 10.0.0.1\n\tptr\n}\n", []string{
			`test.conf:3: "ptr" must come before the first record`}},
		{"no mx after mx", "zone a {\n\tmx m\n\tno mx\n}\n", []string{
			`test.conf:3: "no mx" conflicts with the mx record at test.conf:2`}},
		{"mx after no mx", "zone a {\n\tno mx\n\tmx m\n}\n", []string{
			`test.conf:3: mx conflicts with "no mx" in this zone`}},
		{"no mx twice", "zone a {\n\tno mx\n\tno mx\n}\n", []string{
			`test.conf:3: "no mx" given twice`}},
		{"no ptr in reverse", "reverse 10.0.0.0/8 {\n\tno ptr\n}\n", []string{
			`test.conf:2: "no ptr" is not allowed in a reverse block`}},
		{"missing brace", "zone a {\n\thost x 10.0.0.1\n", []string{
			`test.conf:1: zone "a": missing "}"`}},
		{"brace on next line", "zone a\n{\n}\n", []string{
			`test.conf:1: expected "{", got end of line`,
			`test.conf:2: expected statement, got "{"`}},
		{"several errors in one block", "zone a {\n\thost x\n\tcname y\n\thost z 10.0.0.1\n}\n", []string{
			`test.conf:2: expected address, got end of line`,
			`test.conf:3: expected name, got end of line`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("test.conf", strings.NewReader(tt.src))
			var list ErrorList
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

func TestParsePTRAndNoMX(t *testing.T) {
	cfg := mustParse(t, `
no ptr
zone a.example {
	ptr
	no mx
	host x 10.0.0.1 no ptr
}
zone b.example {
	host y 10.0.0.2
}
`)
	if cfg.Options.PTR == nil || *cfg.Options.PTR {
		t.Errorf("global PTR = %v, want false", cfg.Options.PTR)
	}
	a, b := cfg.Zones[0], cfg.Zones[1]
	if a.Options.PTR == nil || !*a.Options.PTR || !a.NoMX {
		t.Errorf("zone a: PTR = %v, NoMX = %v", a.Options.PTR, a.NoMX)
	}
	if b.Options.PTR != nil || b.NoMX {
		t.Errorf("zone b: PTR = %v, NoMX = %v", b.Options.PTR, b.NoMX)
	}
	if h := a.Hosts[0]; h.PTR == nil || *h.PTR {
		t.Errorf("host x PTR = %v, want false", h.PTR)
	}
}

func TestParseDHCP(t *testing.T) {
	cfg := mustParse(t, `
dhcp 192.168.21.0/24 {
	range .100 .199
	range 192.168.21.220 .230
	router .1
	dns-server { .1 192.0.2.53 }
	domain example.com
	search { example.com, apps.example.com }
	ntp-server .1
	lease 1d
	max-lease 7d
}
zone example.com {
	host printer .12 mac 00:00:5E:00:53:12 dhcp
	host laptop .40 mac { 00:00:5e:00:53:66 00:00:5e:00:53:67 } dhcp
}
`)
	d := cfg.DHCP[0]
	if d.Network.String() != "192.168.21.0/24" || len(d.Ranges) != 2 || len(d.DNSServers) != 2 ||
		*d.Domain != "example.com" || len(d.Search) != 2 || *d.Lease != 86400 || *d.MaxLease != 604800 {
		t.Errorf("dhcp = %+v", d)
	}
	if r := d.Ranges[1]; r.Low.Addr.String() != "192.168.21.220" || r.High.Suffix[0] != 230 {
		t.Errorf("second range = %+v", r)
	}
	hosts := cfg.Zones[0].Hosts
	if !reflect.DeepEqual(hosts[0].MACs, []string{"00:00:5e:00:53:12"}) || len(hosts[1].MACs) != 2 {
		t.Errorf("macs = %v, %v", hosts[0].MACs, hosts[1].MACs)
	}
	if hosts[0].DHCP == nil || !*hosts[0].DHCP {
		t.Errorf("printer dhcp = %v", hosts[0].DHCP)
	}

	cfg = mustParse(t, "zone a {\n\tdhcp\n\thost x 10.0.0.1 mac 00:00:5e:00:53:01 no dhcp\n}\nzone b {\n\tno dhcp\n}\n")
	if d := cfg.Zones[0].Options.DHCP; d == nil || !*d {
		t.Errorf("zone a dhcp = %v", d)
	}
	if d := cfg.Zones[0].Hosts[0].DHCP; d == nil || *d {
		t.Errorf("host x dhcp = %v", d)
	}
	if d := cfg.Zones[1].Options.DHCP; d == nil || *d {
		t.Errorf("zone b dhcp = %v", d)
	}
}

func TestParseDHCPErrors(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"ipv6", "dhcp fd00::/64 {\n}\n", `test.conf:1: dhcp fd00::/64: dhcpd serves IPv4 networks only`},
		{"no block", "dhcp 10.0.0.0/8\n", `test.conf:1: expected "{", got end of line`},
		{"unknown", "dhcp 10.0.0.0/8 {\n\tgateway .1\n}\n", `test.conf:2: unknown statement "gateway" in dhcp block`},
		{"twice", "dhcp 10.0.0.0/8 {\n\trouter .1\n\trouter .2\n}\n", `test.conf:3: "router" given twice`},
		{"dhcp twice", "zone a {\n\thost x 10.0.0.1 dhcp no dhcp\n}\n", `test.conf:2: "dhcp" given twice`},
		{"zone dhcp after record", "zone a {\n\thost x 10.0.0.1\n\tdhcp\n}\n", `test.conf:3: "dhcp" must come before the first record`},
		{"no dhcp at top level", "no dhcp\n", `test.conf:1: "no dhcp" is only allowed in a zone or on a host`},
		{"no dhcp in reverse", "reverse 10.0.0.0/8 {\n\tno dhcp\n}\n", `test.conf:2: "no dhcp" is not allowed in a reverse block`},
		{"bad mac", "zone a {\n\thost x 10.0.0.1 mac 00:11:22:33:44 dhcp\n}\n", `test.conf:2: invalid MAC address "00:11:22:33:44", expected six octets such as 00:00:5e:00:53:01`},
		{"bad mac digit", "zone a {\n\thost x 10.0.0.1 mac 00:11:22:33:44:gg dhcp\n}\n", `test.conf:2: invalid MAC address "00:11:22:33:44:gg", expected six octets such as 00:00:5e:00:53:01`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("test.conf", strings.NewReader(tt.src))
			if err == nil || err.Error() != tt.want {
				t.Errorf("err = %v\nwant  %s", err, tt.want)
			}
		})
	}
}
