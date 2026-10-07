package output

import (
	"bytes"
	"fmt"

	"github.com/isnogudus/zonefile-go/internal/zone"
)

// unboundWidth is the column width of the owner name.
const unboundWidth = 46

// Unbound returns all zones as an unbound.conf(5) fragment with
// local-zone and local-data statements.
func Unbound(zones []*zone.Zone) []byte {
	var b bytes.Buffer
	b.WriteString("server:\n")
	for _, z := range zones {
		if z.Reverse {
			unboundReverse(&b, z)
		} else {
			unboundForward(&b, z)
		}
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func unboundData(b *bytes.Buffer, prefix, name, ttl, data string) {
	fmt.Fprintf(b, "%s\"%-*s %s %s\"\n", prefix, unboundWidth-len(ttl), name, ttl, data)
}

func unboundSOA(b *bytes.Buffer, prefix string, z *zone.Zone) {
	soa := z.SOA
	unboundData(b, prefix, z.Name, u(z.TTL), fmt.Sprintf("IN SOA  %s %s %d %d %d %d %d",
		z.NS[0].Name, soa.Email, serialOf(z), soa.Refresh, soa.Retry, soa.Expire, soa.Minimum))
	for _, ns := range z.NS {
		unboundData(b, prefix, z.Name, ttlField(ns.TTL, z.TTL), "IN NS   "+ns.Name)
	}
}

func unboundForward(b *bytes.Buffer, z *zone.Zone) {
	const prefix = "local-data: "
	fmt.Fprintf(b, "local-zone:  %s static\n", z.Name)
	unboundSOA(b, prefix, z)
	for _, mx := range z.MX {
		unboundData(b, prefix, z.Name, ttlField(mx.TTL, z.TTL), fmt.Sprintf("IN MX   %d %s", mx.Priority, mx.Name))
	}
	for _, a := range sortedAddresses(z) {
		typ := "IN A    "
		if a.Addr.Is6() {
			typ = "IN AAAA "
		}
		unboundData(b, prefix, a.Name, ttlField(a.TTL, z.TTL), typ+a.Addr.String())
	}
	for _, s := range sortedSRVs(z) {
		unboundData(b, prefix, s.Name, ttlField(s.TTL, z.TTL),
			fmt.Sprintf("IN SRV  %d %d %d %s", s.Priority, s.Weight, s.Port, s.Target))
	}
	for _, c := range sortedCNAMEs(z) {
		unboundData(b, prefix, c.Name, ttlField(c.TTL, z.TTL), "IN CNAME "+c.Target)
	}
}

func unboundReverse(b *bytes.Buffer, z *zone.Zone) {
	const prefix = "local-data:     "
	fmt.Fprintf(b, "local-zone:      %s static\n", z.Name)
	unboundSOA(b, prefix, z)
	for _, p := range z.PTRs {
		ttl := ttlField(p.TTL, z.TTL)
		fmt.Fprintf(b, "local-data-ptr: \"%-*s %s %s\"\n", unboundWidth-len(ttl), p.Addr, ttl, p.Target)
	}
}
