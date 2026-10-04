package config

import (
	"fmt"
	"net/netip"
	"strings"
)

// Pos is a position in a configuration file.
type Pos struct {
	File string
	Line int
}

func (p Pos) String() string {
	return fmt.Sprintf("%s:%d", p.File, p.Line)
}

// Error is a problem found at a position in the configuration.
type Error struct {
	Pos Pos
	Msg string
}

func (e *Error) Error() string {
	return e.Pos.String() + ": " + e.Msg
}

// ErrorList holds all errors found while parsing, in input order.
type ErrorList []*Error

func (l ErrorList) Error() string {
	msgs := make([]string, len(l))
	for i, e := range l {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}

// Config is a parsed zonefile.conf. It holds the values as written: names
// are not yet made absolute, suffixes are not resolved and defaults are not
// inherited.
type Config struct {
	Options     Options
	Nameservers []Nameserver
	MX          []MX
	Zones       []*Zone
	Reverse     []*Reverse
}

// Options holds the values of the set statements of one scope. A nil field
// was not set in that scope.
type Options struct {
	Email       *string
	TTL         *uint32
	Refresh     *uint32
	Retry       *uint32
	Expire      *uint32
	NegativeTTL *uint32
	Serial      *uint32
	MXPriority  *uint16
	SRVPriority *uint16
	SRVWeight   *uint16
	PTR         *bool
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
	Hosts       []Host
	CNAMEs      []CNAME
	SRVs        []SRV
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
