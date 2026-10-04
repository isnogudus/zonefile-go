// Package zone turns a parsed configuration into complete zones: names are
// absolute, address suffixes are resolved, defaults are applied and PTR
// records are placed in their reverse zones.
package zone

import "net/netip"

// Defaults of zonefile-rs (src/constants.rs).
const (
	DefaultTTL         = 10800
	DefaultRefresh     = 7200
	DefaultRetry       = 3600
	DefaultExpire      = 1209600
	DefaultNegativeTTL = 3600
	DefaultMXPriority  = 0
	DefaultSRVPriority = 5
	DefaultSRVWeight   = 10
)

// Zone is a forward or reverse zone with all values resolved. All names
// are absolute and end in a dot.
type Zone struct {
	Name      string
	Reverse   bool
	TTL       uint32
	SOA       SOA
	NS        []NS
	MX        []MX
	Addresses []Address
	CNAMEs    []CNAME
	SRVs      []SRV
	// PTRs are sorted by address.
	PTRs []PTR
}

type SOA struct {
	// Email is the contact in RNAME form, e.g. admin.example.com.
	Email string
	// Serial is set if the configuration fixes it; otherwise the serial is
	// computed when the zone is written.
	Serial  *uint32
	Refresh uint32
	Retry   uint32
	Expire  uint32
	// Minimum is the negative caching TTL.
	Minimum uint32
}

type NS struct {
	Name string
	TTL  uint32
}

type MX struct {
	Name     string
	Priority uint16
	TTL      uint32
}

// Address is an A or AAAA record.
type Address struct {
	Name string
	Addr netip.Addr
	TTL  uint32
}

type CNAME struct {
	Name   string
	Target string
	TTL    uint32
}

type SRV struct {
	Name     string
	Target   string
	Port     uint16
	Priority uint16
	Weight   uint16
	TTL      uint32
}

type PTR struct {
	// Name is the full reverse name, e.g. 37.21.168.192.in-addr.arpa.
	Name   string
	Addr   netip.Addr
	Target string
	TTL    uint32
}
