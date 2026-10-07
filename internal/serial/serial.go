// Package serial manages the SOA serials of the zones in the YYYYMMDDnn
// format. A zone gets a new serial only when its content changes, which is
// detected through a hash of the zone stored next to its serial.
package serial

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/isnogudus/zonefile-go/internal/atomicfile"
	"github.com/isnogudus/zonefile-go/internal/zone"
)

// Entry is the serial and the content hash a zone was last written with.
type Entry struct {
	Serial uint32
	Hash   string
}

// State maps zone names, in lower case, to their last entry. Legacy holds
// the single serial of a serial file written before serials were kept per
// zone; it serves as the previous serial of zones that have no entry.
type State struct {
	Zones  map[string]Entry
	Legacy uint32
}

const header = "# zonefile-go: zone, serial and hash of its content; do not edit\n"

// Load reads the state stored at path. A missing file yields an empty
// state. A file that holds a single number, as written by earlier versions,
// yields that number as Legacy.
func Load(path string) (State, error) {
	st := State{Zones: map[string]Entry{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 32); err == nil {
		st.Legacy = uint32(n)
		return st, nil
	}
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 3 {
			return st, fmt.Errorf("%s:%d: expected zone, serial and hash", path, i+1)
		}
		n, err := strconv.ParseUint(f[1], 10, 32)
		if err != nil {
			return st, fmt.Errorf("%s:%d: invalid serial %q", path, i+1, f[1])
		}
		st.Zones[strings.ToLower(f[0])] = Entry{Serial: uint32(n), Hash: f[2]}
	}
	return st, nil
}

// Save stores st at path, one zone per line, sorted by name.
func Save(path string, st State) error {
	names := make([]string, 0, len(st.Zones))
	width := 0
	for name := range st.Zones {
		names = append(names, name)
		width = max(width, len(name))
	}
	sort.Strings(names)
	var b bytes.Buffer
	b.WriteString(header)
	for _, name := range names {
		e := st.Zones[name]
		fmt.Fprintf(&b, "%-*s %d %s\n", width, name, e.Serial, e.Hash)
	}
	return atomicfile.WriteFile(path, b.Bytes(), 0o644)
}

// Next returns the serial that follows old: YYYYMMDD00 for the current UTC
// date, or old+1 if that is not larger, so that several changes on one day
// count up.
func Next(old uint32, now time.Time) uint32 {
	now = now.UTC()
	date := uint32(now.Year())*1000000 + uint32(now.Month())*10000 + uint32(now.Day())*100
	return max(old+1, date)
}

// Assign sets the serial of every zone and returns the new state. A zone
// keeps its previous serial if its hash is unchanged; otherwise, and for a
// zone without an entry, it gets Next of the previous serial. A serial
// fixed in the configuration is kept as it is. Zones that are no longer
// configured drop out of the state.
func Assign(zones []*zone.Zone, prev State, hash func(*zone.Zone) string, now time.Time) State {
	next := State{Zones: map[string]Entry{}}
	for _, z := range zones {
		h := hash(z)
		key := strings.ToLower(z.Name)
		if z.SOA.Serial != nil {
			next.Zones[key] = Entry{Serial: *z.SOA.Serial, Hash: h}
			continue
		}
		var s uint32
		switch e, ok := prev.Zones[key]; {
		case ok && e.Hash == h:
			s = e.Serial
		case ok:
			s = Next(e.Serial, now)
		default:
			s = Next(prev.Legacy, now)
		}
		z.SOA.Serial = &s
		next.Zones[key] = Entry{Serial: s, Hash: h}
	}
	return next
}
