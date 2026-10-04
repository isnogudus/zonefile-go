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
set email "hostmaster@example.com"
set ttl 3h
set refresh 2h
set retry 3600
set expire 2w
set negative-ttl 0
set serial 2026100400
set mx-priority 10
set srv-priority 5
set srv-weight 20
set ptr no

reverse 10.0.0.0/8 {
	set ttl 1d
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
		MXPriority:  ptr(uint16(10)),
		SRVPriority: ptr(uint16(5)),
		SRVWeight:   ptr(uint16(20)),
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
	write("common.conf", "set email admin@example.com\naddr = 192.0.2.1") // no final newline

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
		{"unknown option", "set tll 3h\n", []string{
			`test.conf:1: unknown option "tll"`}},
		{"option twice", "set ttl 1h\nset ttl 2h\n", []string{
			`test.conf:2: "ttl" given twice`}},
		{"ttl zero", "set ttl 0\n", []string{
			`test.conf:1: duration "0" out of range (1-2147483647 seconds)`}},
		{"set after block", "zone a {\n}\nset ttl 1h\n", []string{
			`test.conf:3: set must come before the first zone or reverse block`}},
		{"email without at", "set email admin.example.com\n", []string{
			`test.conf:1: invalid e-mail address "admin.example.com": missing "@"`}},
		{"email numeric tld", "set email a@example.123\n", []string{
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
		{"reverse option", "reverse 10.0.0.0/8 {\n\tset ptr no\n}\n", []string{
			`test.conf:2: option "ptr" is not allowed in a reverse block`}},
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
