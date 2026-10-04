package output

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/isnogudus/zonefile-go/internal/atomicfile"
	"github.com/isnogudus/zonefile-go/internal/zone"
)

// nsdWidth is the column width of the owner name.
const nsdWidth = 32

// NSDFiles returns the files for NSD: zones.conf with one zone: entry per
// zone and master/<zone>zone with the zone data, keyed by path relative to
// the output directory. Zones without a serial of their own get serial.
func NSDFiles(zones []*zone.Zone, serial uint32) map[string][]byte {
	files := map[string][]byte{}
	var conf bytes.Buffer
	for _, z := range zones {
		file := "master/" + z.Name + "zone"
		fmt.Fprintf(&conf, "zone:\n    name: %s\n    zonefile: %s\n\n", z.Name, file)
		files[file] = nsdZone(z, serial)
	}
	files["zones.conf"] = conf.Bytes()
	return files
}

// WriteNSD writes the files of NSDFiles below dir, each one atomically.
func WriteNSD(dir string, zones []*zone.Zone, serial uint32) error {
	files := NSDFiles(zones, serial)
	if err := os.MkdirAll(filepath.Join(dir, "master"), 0o755); err != nil {
		return err
	}
	// Zone files first, so that zones.conf never names a missing file.
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == "zones.conf") != (names[j] == "zones.conf") {
			return names[j] == "zones.conf"
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		if err := atomicfile.WriteFile(filepath.Join(dir, name), files[name], 0o644); err != nil {
			return err
		}
	}
	return nil
}

// nsdLine writes one record line in the column layout of zonefile-rs.
func nsdLine(b *bytes.Buffer, name string, ttl, zoneTTL uint32, typ, data string) {
	var nameTTL string
	if t := ttlField(ttl, zoneTTL); t == "" {
		nameTTL = fmt.Sprintf("%-*s", nsdWidth-1, name)
	} else {
		nameTTL = fmt.Sprintf("%-*s %s", nsdWidth-len(t)-2, name, t)
	}
	typeWidth := max(0, 7-max(0, len(nameTTL)-nsdWidth-1))
	fmt.Fprintf(b, "%s %-*s %s\n", nameTTL, typeWidth, typ, data)
}

func nsdZone(z *zone.Zone, serial uint32) []byte {
	var b bytes.Buffer
	indent := strings.Repeat(" ", nsdWidth)
	soa := z.SOA
	fmt.Fprintf(&b, "$ORIGIN %s\n$TTL %d\n\n", z.Name, z.TTL)
	fmt.Fprintf(&b, "@%sIN SOA     %s %s (\n", strings.Repeat(" ", 28), z.NS[0].Name, soa.Email)
	for _, f := range []struct {
		value   uint32
		comment string
	}{
		{serialOf(z, serial), "serial number"},
		{soa.Refresh, "refresh"},
		{soa.Retry, "retry"},
		{soa.Expire, "expire"},
		{soa.Minimum, "min ttl"},
	} {
		fmt.Fprintf(&b, "%s           %-12d; %s\n", indent, f.value, f.comment)
	}
	fmt.Fprintf(&b, "%s        )\n", indent)
	for _, ns := range z.NS {
		nsdLine(&b, "", ns.TTL, z.TTL, "NS", ns.Name)
	}

	if z.Reverse {
		for _, p := range z.PTRs {
			nsdLine(&b, relative(p.Name, z.Name), p.TTL, z.TTL, "PTR", p.Target)
		}
		return b.Bytes()
	}

	for _, mx := range z.MX {
		nsdLine(&b, "", mx.TTL, z.TTL, fmt.Sprintf("MX %4d", mx.Priority), mx.Name)
	}
	prev := ""
	for _, a := range sortedAddresses(z) {
		// Repeated owner names are left blank.
		name := relative(a.Name, z.Name)
		owner := name
		if name == prev {
			owner = ""
		}
		prev = name
		typ := "A"
		if a.Addr.Is6() {
			typ = "AAAA"
		}
		nsdLine(&b, owner, a.TTL, z.TTL, typ, a.Addr.String())
	}
	for _, s := range sortedSRVs(z) {
		nsdLine(&b, relative(s.Name, z.Name), s.TTL, z.TTL, "SRV",
			fmt.Sprintf("%d %d %d %s", s.Priority, s.Weight, s.Port, s.Target))
	}
	for _, c := range sortedCNAMEs(z) {
		nsdLine(&b, relative(c.Name, z.Name), c.TTL, z.TTL, "CNAME", c.Target)
	}
	return b.Bytes()
}
