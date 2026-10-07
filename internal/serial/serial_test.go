package serial

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/isnogudus/zonefile-go/internal/zone"
)

func TestNext(t *testing.T) {
	day := time.Date(2026, 10, 4, 23, 30, 0, 0, time.UTC)
	tests := []struct {
		old, want uint32
	}{
		{0, 2026100400},
		{2025010105, 2026100400},
		{2026100400, 2026100401},
		{2026100417, 2026100418},
		{2030010100, 2030010101}, // never goes back
	}
	for _, tt := range tests {
		if got := Next(tt.old, day); got != tt.want {
			t.Errorf("Next(%d) = %d, want %d", tt.old, got, tt.want)
		}
	}
	// The date is taken in UTC.
	berlin := time.FixedZone("CEST", 2*3600)
	if got := Next(0, time.Date(2026, 10, 5, 1, 0, 0, 0, berlin)); got != 2026100400 {
		t.Errorf("Next in CEST = %d, want 2026100400", got)
	}
}

func TestLoadSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serial")
	st, err := Load(path)
	if err != nil || len(st.Zones) != 0 || st.Legacy != 0 {
		t.Fatalf("Load(missing) = %+v, %v", st, err)
	}

	want := State{Zones: map[string]Entry{
		"example.com.":          {2026100701, "sha256:aa"},
		"168.192.in-addr.arpa.": {2026100600, "sha256:bb"},
	}}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip:\ngot  %+v\nwant %+v", got, want)
	}
	b, _ := os.ReadFile(path)
	if lines := strings.Split(strings.TrimSpace(string(b)), "\n"); len(lines) != 3 ||
		!strings.HasPrefix(lines[0], "#") || !strings.HasPrefix(lines[1], "168.192.in-addr.arpa.") {
		t.Errorf("file:\n%s", b)
	}
}

func TestLoadLegacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".serial")
	if err := os.WriteFile(path, []byte("  2025012301 \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Load(path)
	if err != nil || st.Legacy != 2025012301 || len(st.Zones) != 0 {
		t.Fatalf("Load(legacy) = %+v, %v", st, err)
	}
}

func TestLoadInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serial")
	for _, content := range []string{"garbage\n", "example.com. 12 sha256:aa extra\n", "example.com. x sha256:aa\n"} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Errorf("Load(%q) succeeded", content)
		}
	}
}

func TestAssign(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	fixed := uint32(42)
	zones := []*zone.Zone{
		{Name: "same.example."},
		{Name: "Changed.Example."},
		{Name: "new.example."},
		{Name: "fixed.example.", SOA: zone.SOA{Serial: &fixed}},
	}
	hashes := map[string]string{
		"same.example.":    "h1",
		"Changed.Example.": "h2-new",
		"new.example.":     "h3",
		"fixed.example.":   "h4",
	}
	prev := State{Zones: map[string]Entry{
		"same.example.":    {2026100500, "h1"},
		"changed.example.": {2026100701, "h2-old"},
		"gone.example.":    {2026010100, "h5"},
	}, Legacy: 2026100799}

	next := Assign(zones, prev, func(z *zone.Zone) string { return hashes[z.Name] }, now)

	want := map[string]uint32{
		"same.example.":    2026100500, // unchanged: keeps its serial
		"Changed.Example.": 2026100702, // changed: counts up
		"new.example.":     2026100800, // new: follows the legacy serial
		"fixed.example.":   42,
	}
	for _, z := range zones {
		if got := *z.SOA.Serial; got != want[z.Name] {
			t.Errorf("%s: serial %d, want %d", z.Name, got, want[z.Name])
		}
	}
	wantState := map[string]Entry{
		"same.example.":    {2026100500, "h1"},
		"changed.example.": {2026100702, "h2-new"},
		"new.example.":     {2026100800, "h3"},
		"fixed.example.":   {42, "h4"},
	}
	if !reflect.DeepEqual(next.Zones, wantState) || next.Legacy != 0 {
		t.Errorf("state:\ngot  %+v\nwant %+v", next, wantState)
	}
}
