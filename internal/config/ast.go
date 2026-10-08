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
	// Notes point out what the parser found valid but possibly
	// unintended, such as macros that are not used.
	Notes ErrorList
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
	// Inet and Inet6 by no inet and no inet6; they exist only in zones.
	DHCP        *bool
	DHCPProfile *string
	Inet        *bool
	Inet6       *bool
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
	NoMX bool
	// DHCPBlocks are the dhcp blocks in the zone. They are also in
	// Config.DHCP, and turn dhcp on for the hosts of the zone unless the
	// zone says no dhcp.
	DHCPBlocks []*DHCP
	Hosts      []Host
	CNAMEs     []CNAME
	SRVs       []SRV
}

// HostAddr is a full address; or, if Suffix is not nil, an address suffix
// such as .37 that is resolved against the networks of the zone; or, if
// Ref is not empty, the absolute name of another host whose addresses are
// taken.
type HostAddr struct {
	Addr   netip.Addr
	Suffix []byte
	Ref    string
}

func (a HostAddr) IsSuffix() bool {
	return a.Suffix != nil
}

func (a HostAddr) IsRef() bool {
	return a.Ref != ""
}

type Host struct {
	Pos     Pos
	Name    string
	Addrs   []HostAddr
	Aliases []string
	TTL     *uint32
	PTR     *bool
	// Inet and Inet6 are set by inet, no inet, inet6 and no inet6.
	Inet  *bool
	Inet6 *bool
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
	// Authoritative is set by authoritative and not authoritative.
	Authoritative *bool
	DHCPOptions
	Profiles []*DHCPProfile
	// implicit is set for a dhcp block in a zone that takes the network of
	// the zone.
	implicit bool
}

// DHCPOptions are the options and lease times of a dhcp block or a profile
// in it, as option routers, option domain-name-servers, option ntp-servers,
// option domain-name, option domain-search, default-lease-time and
// max-lease-time.
type DHCPOptions struct {
	Routers           []HostAddr
	DNSServers        []HostAddr
	NTPServers        []HostAddr
	SMTPServers       []HostAddr
	Domain            *string
	Search            []string
	AutoproxyScript   *string
	Lease             *uint32
	MaxLease          *uint32
	GetLeaseHostnames *bool
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
