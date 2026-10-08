package output

import (
	"bytes"
	"flag"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isnogudus/zonefile-go/internal/config"
	"github.com/isnogudus/zonefile-go/internal/zone"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

func loadZones(t *testing.T) []*zone.Zone {
	t.Helper()
	cfg, err := config.ParseFile("testdata/small.conf")
	if err != nil {
		t.Fatal(err)
	}
	res, err := zone.Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return res.Zones
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from %s (run go test -update to accept):\n%s", name, path, got)
	}
}

func TestUnbound(t *testing.T) {
	golden(t, "unbound", Unbound(loadZones(t)))
}

func TestNSD(t *testing.T) {
	files := NSDFiles(loadZones(t))
	if len(files) != 4 {
		t.Errorf("got %d files, want 4", len(files))
	}
	for name, data := range files {
		golden(t, "nsd-"+strings.ReplaceAll(name, "/", "-"), data)
	}
}

func TestWriteNSD(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteNSD(dir, loadZones(t)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"zones.conf", "master/example.com.zone", "master/2.0.192.in-addr.arpa.zone"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Error(err)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "master"))
	if len(entries) != 3 {
		t.Errorf("master has %d entries, want 3 (no temporary files)", len(entries))
	}
}

func TestZoneHash(t *testing.T) {
	zones := loadZones(t)
	z := zones[0]
	h := ZoneHash(z)
	if !strings.HasPrefix(h, "sha256:") || len(h) != len("sha256:")+64 {
		t.Fatalf("hash = %q", h)
	}

	// The serial does not count, and the zone itself is not changed.
	other := uint32(2030010100)
	before := z.SOA.Serial
	c := *z
	c.SOA.Serial = &other
	if ZoneHash(&c) != h {
		t.Error("hash depends on the serial")
	}
	if z.SOA.Serial != before {
		t.Error("ZoneHash changed the zone")
	}

	// Any other change does.
	c.TTL++
	if ZoneHash(&c) == h {
		t.Error("hash ignores the TTL")
	}
	if ZoneHash(zones[1]) == h {
		t.Error("two zones have the same hash")
	}
}

func TestSerialPerZone(t *testing.T) {
	zones := loadZones(t)
	a, b := uint32(2026100701), uint32(2026100502)
	zones[0].SOA.Serial = &a
	zones[1].SOA.Serial = &b
	out := Unbound(zones)
	if !bytes.Contains(out, []byte(" 2026100701 ")) || !bytes.Contains(out, []byte(" 2026100502 ")) {
		t.Errorf("serials of the zones not in the output:\n%s", out)
	}
}

func TestWriteNSDRemovesStaleZones(t *testing.T) {
	dir := t.TempDir()
	zones := loadZones(t)
	if _, err := WriteNSD(dir, zones); err != nil {
		t.Fatal(err)
	}
	// A zone file that zonefile-go did not write must survive.
	manual := filepath.Join(dir, "master", "manual.example.zone")
	if err := os.WriteFile(manual, []byte("; by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Drop the IPv6 reverse zone.
	removed, err := WriteNSD(dir, zones[:2])
	if err != nil {
		t.Fatal(err)
	}
	stale := "master/0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.ip6.arpa.zone"
	if len(removed) != 1 || removed[0] != stale {
		t.Errorf("removed = %v, want [%s]", removed, stale)
	}
	if _, err := os.Stat(filepath.Join(dir, stale)); !os.IsNotExist(err) {
		t.Errorf("stale zone file still there: %v", err)
	}
	for _, keep := range []string{"master/example.com.zone", "master/2.0.192.in-addr.arpa.zone", "master/manual.example.zone"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Errorf("%s: %v", keep, err)
		}
	}

	// A second run has nothing left to remove.
	if removed, err := WriteNSD(dir, zones[:2]); err != nil || len(removed) != 0 {
		t.Errorf("second run: removed = %v, err = %v", removed, err)
	}
}

func TestGeneratedZoneFilesStaysInMaster(t *testing.T) {
	dir := t.TempDir()
	conf := `zone:
    name: a.
    zonefile: master/a.zone
zone:
    zonefile: master/../zones.conf
zone:
    zonefile: /etc/passwd
zone:
    zonefile: master/sub/b.zone
zone:
    zonefile: other/c.zone
`
	if err := os.WriteFile(filepath.Join(dir, "zones.conf"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := generatedZoneFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "master/a.zone" {
		t.Errorf("got %v, want [master/a.zone]", got)
	}
}

func TestDhcpd(t *testing.T) {
	cfg, err := config.ParseFile("testdata/small.conf")
	if err != nil {
		t.Fatal(err)
	}
	res, err := zone.Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "dhcpd", Dhcpd(res.DHCP))
}

func TestNetmask(t *testing.T) {
	for pfx, want := range map[string]string{
		"0.0.0.0/0": "0.0.0.0", "10.0.0.0/8": "255.0.0.0", "10.0.16.0/20": "255.255.240.0",
		"192.0.2.0/24": "255.255.255.0", "192.0.2.4/30": "255.255.255.252", "192.0.2.1/32": "255.255.255.255",
	} {
		if got := netmask(netip.MustParsePrefix(pfx)).String(); got != want {
			t.Errorf("netmask(%s) = %s, want %s", pfx, got, want)
		}
	}
}

// TestDhcpdKeywords guards the statement names of dhcpd.conf(5) and
// dhcp-options(5) independently of the golden file, which a careless
// search and replace could change along with the code.
func TestDhcpdKeywords(t *testing.T) {
	cfg, err := config.ParseFile("testdata/small.conf")
	if err != nil {
		t.Fatal(err)
	}
	res, err := zone.Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	out := string(Dhcpd(res.DHCP))
	for _, kw := range []string{
		"\n\tserver-identifier ", "\n\toption routers ", "\n\toption domain-name-servers ",
		"\n\toption domain-name ", "\n\tdefault-lease-time ", "\n\trange ",
		"\n\t\thardware ethernet ", "\n\t\tfixed-address ", "\n\t\toption host-name ",
		"\n\tgroup {\n",
	} {
		if !strings.Contains(out, kw) {
			t.Errorf("dhcpd.conf lacks %q", strings.TrimSpace(kw))
		}
	}
}
