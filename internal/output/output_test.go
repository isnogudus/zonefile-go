package output

import (
	"bytes"
	"flag"
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
	zones, _, err := zone.Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return zones
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
	golden(t, "unbound", Unbound(loadZones(t), 1))
}

func TestNSD(t *testing.T) {
	files := NSDFiles(loadZones(t), 1)
	if len(files) != 4 {
		t.Errorf("got %d files, want 4", len(files))
	}
	for name, data := range files {
		golden(t, "nsd-"+strings.ReplaceAll(name, "/", "-"), data)
	}
}

func TestWriteNSD(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteNSD(dir, loadZones(t), 1); err != nil {
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

func TestComputedSerial(t *testing.T) {
	zones := loadZones(t)
	zones[0].SOA.Serial = nil
	if !bytes.Contains(Unbound(zones, 2026100499), []byte(" 2026100499 ")) {
		t.Error("computed serial not used for a zone without a fixed serial")
	}
}

func TestWriteNSDRemovesStaleZones(t *testing.T) {
	dir := t.TempDir()
	zones := loadZones(t)
	if _, err := WriteNSD(dir, zones, 1); err != nil {
		t.Fatal(err)
	}
	// A zone file that zonefile-go did not write must survive.
	manual := filepath.Join(dir, "master", "manual.example.zone")
	if err := os.WriteFile(manual, []byte("; by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Drop the IPv6 reverse zone.
	removed, err := WriteNSD(dir, zones[:2], 1)
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
	if removed, err := WriteNSD(dir, zones[:2], 1); err != nil || len(removed) != 0 {
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
