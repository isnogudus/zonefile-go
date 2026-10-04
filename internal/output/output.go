// Package output writes resolved zones for Unbound and NSD. The layout
// follows zonefile-rs, so that the output of both tools can be compared;
// unlike zonefile-rs, records are always written in a fixed order.
package output

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/isnogudus/zonefile-go/internal/zone"
)

// serialOf returns the serial of z: the one fixed in the configuration,
// or else the computed one.
func serialOf(z *zone.Zone, serial uint32) uint32 {
	if z.SOA.Serial != nil {
		return *z.SOA.Serial
	}
	return serial
}

// ttlField returns the TTL to write for a record: empty if it is the
// default TTL of the zone.
func ttlField(ttl, zoneTTL uint32) string {
	if ttl == zoneTTL {
		return ""
	}
	return strconv.FormatUint(uint64(ttl), 10)
}

func u(n uint32) string {
	return strconv.FormatUint(uint64(n), 10)
}

// sortedAddresses returns the address records of z with the apex first,
// then by name and address.
func sortedAddresses(z *zone.Zone) []zone.Address {
	addrs := slices.Clone(z.Addresses)
	slices.SortStableFunc(addrs, func(a, b zone.Address) int {
		aApex, bApex := a.Name == z.Name, b.Name == z.Name
		switch {
		case aApex != bApex:
			if aApex {
				return -1
			}
			return 1
		case a.Name != b.Name:
			return strings.Compare(a.Name, b.Name)
		}
		return a.Addr.Compare(b.Addr)
	})
	return addrs
}

func sortedSRVs(z *zone.Zone) []zone.SRV {
	srvs := slices.Clone(z.SRVs)
	slices.SortStableFunc(srvs, func(a, b zone.SRV) int {
		return cmp.Or(
			strings.Compare(a.Name, b.Name),
			cmp.Compare(a.Priority, b.Priority),
			cmp.Compare(b.Weight, a.Weight),
			strings.Compare(a.Target, b.Target),
		)
	})
	return srvs
}

func sortedCNAMEs(z *zone.Zone) []zone.CNAME {
	cnames := slices.Clone(z.CNAMEs)
	slices.SortStableFunc(cnames, func(a, b zone.CNAME) int {
		return strings.Compare(a.Name, b.Name)
	})
	return cnames
}

// relative returns name relative to the zone origin: @ for the apex, the
// name without the origin below it, and the absolute name otherwise.
func relative(name, origin string) string {
	if name == origin {
		return "@"
	}
	if rel, ok := strings.CutSuffix(name, "."+origin); ok {
		return rel
	}
	return name
}
