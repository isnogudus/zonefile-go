// Package serial manages the SOA serial number in the YYYYMMDDnn format.
package serial

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/isnogudus/zonefile-go/internal/atomicfile"
)

// Load reads the serial stored at path. A missing file yields 0.
func Load(path string) (uint32, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(b))
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid serial %q", path, s)
	}
	return uint32(n), nil
}

// Next returns the serial that follows old: YYYYMMDD00 for the current UTC
// date, or old+1 if that is not larger, so that several runs on one day
// count up.
func Next(old uint32, now time.Time) uint32 {
	now = now.UTC()
	date := uint32(now.Year())*1000000 + uint32(now.Month())*10000 + uint32(now.Day())*100
	return max(old+1, date)
}

// Save stores serial at path.
func Save(path string, serial uint32) error {
	return atomicfile.WriteFile(path, []byte(strconv.FormatUint(uint64(serial), 10)+"\n"), 0o644)
}
