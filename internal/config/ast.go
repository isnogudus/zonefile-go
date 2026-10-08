package config

import (
	"net/netip"

	"github.com/isnogudus/obsdconf"
)

// Pos, Error and ErrorList come from obsdconf, so that the later stages
// report errors in the same form.
type (
	Pos       = obsdconf.Pos
	Error     = obsdconf.Error
	ErrorList = obsdconf.ErrorList
)

// Config is a parsed zonefile.conf. It holds the values as written: names
// are not yet made absolute, suffixes are not resolved and defaults are not
// inherited.
type Config struct {
	Options     Options
	Nameservers []Nameserver
	MX          []MX
	Zones       []*Zone
	Reverse     []*Reverse
	DHCP        []*DHCP
	// DHCPDefaults is the dhcp block without a network, if any. Its
	// Network is the zero Prefix, and it has no Ranges.
	DHCPDefaults *DHCP
}

// Options holds the settings of one scope: email, ttl and the other SOA
// values, and ptr or no ptr. A nil field was not set in that scope.
type Options struct {
	Email       *string
	TTL         *uint32
	Refresh     *uint32
	Retry       *uint32
	Expire      *uint32
	NegativeTTL *uint32
	Serial      *uint32
	PTR         *bool
	// DHCP and DHCPProfile are set by dhcp, no dhcp and dhcp-profile,
	// which exist only in zones.
	DHCP        *bool
	DHCPProfile *string
}

type Nameserver struct {
	Pos  Pos
	Name string
	TTL  *uint32
}

type MX struct {
	Pos      Pos
	Name     string
	Priority *uint16
	TTL      *uint32
}

type Zone struct {
	Pos         Pos
	Name        string
	Options     Options
	Networks    []netip.Prefix
	Nameservers []Nameserver
	MX          []MX
	// NoMX is set by no mx: the zone has no MX records, not even the
	// top-level ones.
	NoMX   bool
	Hosts  []Host
	CNAMEs []CNAME
	SRVs   []SRV
}

// HostAddr is either a full address or, if Suffix is not nil, an address
// suffix such as .37 that is resolved against the networks of the zone.
type HostAddr struct {
	Addr   netip.Addr
	Suffix []byte
}

func (a HostAddr) IsSuffix() bool {
	return a.Suffix != nil
}

type Host struct {
	Pos     Pos
	Name    string
	Addrs   []HostAddr
	Aliases []string
	TTL     *uint32
	PTR     *bool
	NoInet  bool
	NoInet6 bool
	// MACs are the hardware addresses given with mac, in lower case.
	MACs []string
	// DHCP is set by dhcp and no dhcp on the host, DHCPProfile by
	// dhcp-profile.
	DHCP        *bool
	DHCPProfile *string
}

type CNAME struct {
	Pos    Pos
	Name   string
	Target string
	TTL    *uint32
}

type SRV struct {
	Pos      Pos
	Name     string
	Target   string
	Port     uint16
	Priority *uint16
	Weight   *uint16
	TTL      *uint32
}

type Reverse struct {
	Pos         Pos
	Networks    []netip.Prefix
	Options     Options
	Nameservers []Nameserver
}

// DHCP is a dhcp block: the settings of one IPv4 subnet for dhcpd. Addresses
// may be suffixes, resolved against Network.
type DHCP struct {
	Pos      Pos
	Network  netip.Prefix
	Ranges   []DHCPRange
	ServerID *HostAddr
	DHCPOptions
	Profiles []*DHCPProfile
}

// DHCPOptions are the options and lease times of a dhcp block or a profile
// in it, as option routers, option domain-name-servers, option ntp-servers,
// option domain-name, option domain-search, default-lease-time and
// max-lease-time.
type DHCPOptions struct {
	Routers    []HostAddr
	DNSServers []HostAddr
	NTPServers []HostAddr
	Domain     *string
	Search     []string
	Lease      *uint32
	MaxLease   *uint32
}

// DHCPProfile is a profile in a dhcp block: options for the hosts that
// name it with dhcp-profile.
type DHCPProfile struct {
	Pos  Pos
	Name string
	DHCPOptions
}

// DHCPRange is a range of addresses that dhcpd hands out dynamically.
type DHCPRange struct {
	Pos       Pos
	Low, High HostAddr
}
