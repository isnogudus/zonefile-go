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
		Inet6:   ptr(false),
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
		{"no inet6 at top level", "no inet6\n", []string{
			`test.conf:1: "no inet6" is only allowed in a zone or on a host`}},
		{"no inet6 in reverse", "reverse 10.0.0.0/8 {\n\tno inet6\n}\n", []string{
			`test.conf:2: "no inet6" is not allowed in a reverse block`}},
		{"zone no inet and no inet6", "zone a {\n\tno inet\n\tno inet6\n}\n", []string{
			`test.conf:3: "no inet" and "no inet6" together leave no addresses`}},
		{"zone no inet6 after record", "zone a {\n\thost x 10.0.0.1\n\tno inet6\n}\n", []string{
			`test.conf:3: "no inet6" must come before the first record`}},
		{"host inet and no inet", "zone a {\n\thost x 10.0.0.1 inet no inet\n}\n", []string{
			`test.conf:2: "inet" given twice`}},
		{"no mx at top level", "no mx\n", []string{
			`test.conf:1: "no mx" is only allowed in a zone`}},
		{"no what", "zone a {\n\tno cname\n}\n", []string{
			`test.conf:2: expected "ptr", "mx", "dhcp", "inet" or "inet6" after "no", got "cname"`}},
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
	option routers .1
	option domain-name-servers { .1 192.0.2.53 }
	option domain-name example.com
	option domain-search { example.com, apps.example.com }
	option ntp-servers .1
	default-lease-time 1d
	max-lease-time 7d
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
		{"server-identifier twice", "dhcp 10.0.0.0/8 {\n\tserver-identifier .1\n\tserver-identifier .2\n}\n", `test.conf:3: "server-identifier" given twice`},
		{"server-identifier in profile", "dhcp 10.0.0.0/8 {\n\tdhcp-profile a {\n\t\tserver-identifier .1\n\t}\n}\n", `test.conf:3: unknown statement "server-identifier" in dhcp-profile`},
		{"global block twice", "dhcp {\n}\ndhcp {\n}\n", `test.conf:3: global dhcp block already defined at test.conf:1`},
		{"range in global block", "dhcp {\n\trange .1 .9\n}\n", `test.conf:2: "range" is not allowed in the global dhcp block`},
		{"unknown in global block", "dhcp {\n\tgateway .1\n}\n", `test.conf:2: unknown statement "gateway" in global dhcp block`},
		{"ipv6", "dhcp fd00::/64 {\n}\n", `test.conf:1: dhcp fd00::/64: dhcpd serves IPv4 networks only`},
		{"no block", "dhcp 10.0.0.0/8\n", `test.conf:1: expected "{", got end of line`},
		{"unknown", "dhcp 10.0.0.0/8 {\n\tgateway .1\n}\n", `test.conf:2: unknown statement "gateway" in dhcp block`},
		{"twice", "dhcp 10.0.0.0/8 {\n\toption routers .1\n\toption routers .2\n}\n", `test.conf:3: "option routers" given twice`},
		{"unknown option", "dhcp 10.0.0.0/8 {\n\toption tftp-server-name boot\n}\n", `test.conf:2: unknown dhcp option "tftp-server-name", known are routers, domain-name-servers, ntp-servers, smtp-server, domain-name, domain-search, autoproxy-script`},
		{"authoritative in profile", "dhcp 10.0.0.0/8 {\n\tdhcp-profile a {\n\t\tauthoritative\n\t}\n}\n", `test.conf:3: unknown statement "authoritative" in dhcp-profile`},
		{"not what", "dhcp 10.0.0.0/8 {\n\tnot ptr\n}\n", `test.conf:2: expected "authoritative", got "ptr"`},
		{"authoritative twice", "dhcp 10.0.0.0/8 {\n\tauthoritative\n\tnot authoritative\n}\n", `test.conf:3: "authoritative" given twice`},
		{"get-lease-hostnames value", "dhcp 10.0.0.0/8 {\n\tget-lease-hostnames yes\n}\n", `test.conf:2: expected "true" or "false", got "yes"`},
		{"option without name", "dhcp 10.0.0.0/8 {\n\toption\n}\n", `test.conf:2: expected option name, got end of line`},
		{"old word", "dhcp 10.0.0.0/8 {\n\trouter .1\n}\n", `test.conf:2: unknown statement "router" in dhcp block`},
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

func TestParseDHCPProfiles(t *testing.T) {
	cfg := mustParse(t, `
dhcp 192.168.21.0/24 {
	option domain-name-servers .1
	dhcp-profile kids {
		option domain-name-servers .53
		default-lease-time 1h
	}
	dhcp-profile iot {
		option routers .254
	}
}
zone example.com {
	dhcp-profile iot
	host tv .20 mac 00:00:5e:00:53:20 dhcp dhcp-profile kids
}
`)
	d := cfg.DHCP[0]
	if len(d.Profiles) != 2 || d.Profiles[0].Name != "kids" || len(d.Profiles[0].DNSServers) != 1 ||
		*d.Profiles[0].Lease != 3600 || d.Profiles[1].Routers[0].Suffix[0] != 254 {
		t.Errorf("profiles = %+v", d.Profiles)
	}
	z := cfg.Zones[0]
	if p := z.Options.DHCPProfile; p == nil || *p != "iot" {
		t.Errorf("zone profile = %v", p)
	}
	if p := z.Hosts[0].DHCPProfile; p == nil || *p != "kids" {
		t.Errorf("host profile = %v", p)
	}
}

func TestParseDHCPProfileErrors(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"duplicate", "dhcp 10.0.0.0/8 {\n\tdhcp-profile a {\n\t}\n\tdhcp-profile a {\n\t}\n}\n", `test.conf:4: dhcp-profile "a" already defined at test.conf:2`},
		{"range in profile", "dhcp 10.0.0.0/8 {\n\tdhcp-profile a {\n\t\trange .1 .2\n\t}\n}\n", `test.conf:3: unknown statement "range" in dhcp-profile`},
		{"definition without block", "dhcp 10.0.0.0/8 {\n\tdhcp-profile a\n}\n", `test.conf:2: expected "{", got end of line`},
		{"dhcp-profile at top level", "dhcp-profile a\n", `test.conf:1: "dhcp-profile" is defined in a dhcp block and used in a zone or on a host`},
		{"dhcp-profile after record", "zone a {\n\thost x 10.0.0.1\n\tdhcp-profile b\n}\n", `test.conf:3: "dhcp-profile" must come before the first record`},
		{"dhcp-profile twice", "zone a {\n\thost x 10.0.0.1 dhcp-profile b dhcp-profile c\n}\n", `test.conf:2: "dhcp-profile" given twice`},
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

func TestParseDHCPMore(t *testing.T) {
	cfg := mustParse(t, `
dhcp {
	not authoritative
	get-lease-hostnames true
}
dhcp 10.0.0.0/24 {
	authoritative
	option smtp-server { .25 192.0.2.25 }
	option autoproxy-script "http://wpad.example.com/wpad.dat"
}
`)
	g, d := cfg.DHCPDefaults, cfg.DHCP[0]
	if g.Authoritative == nil || *g.Authoritative || g.GetLeaseHostnames == nil || !*g.GetLeaseHostnames {
		t.Errorf("global = %+v", g)
	}
	if d.Authoritative == nil || !*d.Authoritative || len(d.SMTPServers) != 2 ||
		*d.AutoproxyScript != "http://wpad.example.com/wpad.dat" {
		t.Errorf("block = %+v", d)
	}
}

func TestParseHostBlock(t *testing.T) {
	cfg := mustParse(t, `
zone example.com {
	host tv .20 ttl 1h {
		alias { www media }
		mac 00:00:5e:00:53:20
		dhcp dhcp-profile kids
		no ptr
	}
	host pc .30
}
`)
	h := cfg.Zones[0].Hosts[0]
	if len(h.Aliases) != 2 || len(h.MACs) != 1 || h.DHCP == nil || !*h.DHCP ||
		*h.DHCPProfile != "kids" || h.PTR == nil || *h.PTR || *h.TTL != 3600 {
		t.Errorf("host = %+v", h)
	}
	if len(cfg.Zones[0].Hosts) != 2 {
		t.Errorf("hosts = %d, want 2", len(cfg.Zones[0].Hosts))
	}
}

func TestParseHostBlockErrors(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"option twice", "zone a {\n\thost x .1 ttl 1h {\n\t\tttl 2h\n\t}\n}\n", `test.conf:3: "ttl" given twice`},
		{"unknown option", "zone a {\n\thost x .1 {\n\t\tcname y\n\t}\n}\n", `test.conf:3: host: unknown option "cname"`},
		{"missing brace", "zone a {\n\thost x .1 {\n\t\tdhcp\n}\n", `test.conf:1: zone "a": missing "}"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("test.conf", strings.NewReader(tt.src))
			if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
				t.Errorf("err = %v\nwant  %s", err, tt.want)
			}
		})
	}
}

func TestParseZoneDHCP(t *testing.T) {
	cfg := mustParse(t, `
zone haus.home.arpa {
	network { 192.168.200.0/24 fd00::/64 }
	dhcp {
		range .200 .219
	}
	host e3dc .13 mac 6c:c3:74:46:42:e3
}
zone other.example {
	network 10.0.0.0/24
	dhcp {
		option domain-name override.example
	}
}
`)
	z := cfg.Zones[0]
	if len(z.DHCPBlocks) != 1 || z.DHCPBlocks[0].Network.String() != "192.168.200.0/24" ||
		*z.DHCPBlocks[0].Domain != "haus.home.arpa" || len(z.DHCPBlocks[0].Ranges) != 1 {
		t.Fatalf("dhcp blocks = %+v", z.DHCPBlocks)
	}
	if len(cfg.DHCP) != 2 || cfg.DHCP[0] != z.DHCPBlocks[0] {
		t.Errorf("Config.DHCP = %v", cfg.DHCP)
	}
	if d := cfg.DHCP[1]; *d.Domain != "override.example" {
		t.Errorf("domain = %q", *d.Domain)
	}
}

func TestParseZoneDHCPErrors(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"no network", "zone a {\n\tdhcp {\n\t}\n}\n", `test.conf:2: dhcp block in zone "a" needs an IPv4 network statement before it, or a network of its own`},
		{"ipv6 only", "zone a {\n\tnetwork fd00::/64\n\tdhcp {\n\t}\n}\n", `test.conf:3: dhcp block in zone "a" needs an IPv4 network statement before it, or a network of its own`},
		{"twice", "zone a {\n\tnetwork 10.0.0.0/24\n\tdhcp {\n\t}\n\tdhcp {\n\t}\n}\n", `test.conf:5: zone "a" already has a dhcp block for its network at test.conf:3; name the network of a further one`},
		{"named ipv6", "zone a {\n\tdhcp fd00::/64 {\n\t}\n}\n", `test.conf:2: dhcp fd00::/64: dhcpd serves IPv4 networks only`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("test.conf", strings.NewReader(tt.src))
			if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
				t.Errorf("err = %v\nwant  %s", err, tt.want)
			}
		})
	}
}

func TestParseZoneDHCPNamed(t *testing.T) {
	cfg := mustParse(t, `
zone home.arpa {
	network 192.168.21.0/24
	dhcp {
		range .100 .199
	}
	dhcp 192.168.100.0/24 {
		range .100 .199
	}
}
zone nonet.example {
	dhcp 10.0.0.0/24 {
	}
}
`)
	z := cfg.Zones[0]
	if len(z.DHCPBlocks) != 2 || z.DHCPBlocks[1].Network.String() != "192.168.100.0/24" ||
		*z.DHCPBlocks[1].Domain != "home.arpa" {
		t.Errorf("home.arpa blocks = %+v", z.DHCPBlocks)
	}
	if b := cfg.Zones[1].DHCPBlocks; len(b) != 1 || b[0].Network.String() != "10.0.0.0/24" || *b[0].Domain != "nonet.example" {
		t.Errorf("nonet blocks = %+v", b)
	}
	if len(cfg.DHCP) != 3 {
		t.Errorf("Config.DHCP has %d blocks, want 3", len(cfg.DHCP))
	}
}
