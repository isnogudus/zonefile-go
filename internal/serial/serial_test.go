package serial

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
	path := filepath.Join(t.TempDir(), ".serial")
	if n, err := Load(path); n != 0 || err != nil {
		t.Fatalf("Load(missing) = %d, %v", n, err)
	}
	if err := Save(path, 2026100401); err != nil {
		t.Fatal(err)
	}
	if n, err := Load(path); n != 2026100401 || err != nil {
		t.Fatalf("Load = %d, %v", n, err)
	}
	if err := os.WriteFile(path, []byte("  2025012301 \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, err := Load(path); n != 2025012301 || err != nil {
		t.Fatalf("Load(whitespace) = %d, %v", n, err)
	}
	if err := os.WriteFile(path, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load(garbage) succeeded")
	}
}
