package zone

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/isnogudus/zonefile-go/internal/config"
)

// Subnet is a dhcp block with all addresses resolved, and the hosts with
// dhcp on whose IPv4 address lies in it.
type Subnet struct {
	Network netip.Prefix
	Ranges  []Range
	DHCPOptions
	// Hosts are the hosts without a profile. Hosts here and in profiles are
	// sorted by address, and for the same address in the order of the
	// configuration.
	Hosts []DHCPHost
	// Profiles are the profiles of the block in configuration order, with
	// the hosts that use them.
	Profiles []*Profile
}

// DHCPOptions are the options of a subnet or a profile. Zero values are
// not written.
type DHCPOptions struct {
	Routers    []netip.Addr
	DNSServers []netip.Addr
	NTPServers []netip.Addr
	// Domain and Search are without the trailing dot.
	Domain string
	Search []string
	// Lease and MaxLease are in seconds.
	Lease    uint32
	MaxLease uint32
}

// Profile is a profile of a dhcp block and the hosts that use it.
type Profile struct {
	Name string
	DHCPOptions
	Hosts []DHCPHost
}

type Range struct {
	Low, High netip.Addr
}

// DHCPHost is one host declaration: a MAC address with the fixed addresses
// of the host in the subnet.
type DHCPHost struct {
	// Name is the name of the declaration, unique in the output: the name
	// of the host without the trailing dot, with -2, -3, … added to the
	// first label for further MAC addresses of the same host.
	Name string
	// HostName is the first label of the host name, sent as host-name.
	HostName string
	MAC      string
	Addrs    []netip.Addr
}

// dhcpHost is a host with dhcp on and its MAC addresses, collected while
// resolving zones.
type dhcpHost struct {
	pos     config.Pos
	name    string
	addrs   []netip.Addr // IPv4 only
	macs    []string
	profile string // "" for none
}

// subnetAddr resolves an address of a dhcp block: a suffix against the
// network, or a full IPv4 address.
func (r *resolver) subnetAddr(pos config.Pos, n netip.Prefix, a config.HostAddr, what string) (netip.Addr, bool) {
	if a.IsSuffix() {
		addr, err := applySuffix(n, a.Suffix)
		if err != nil {
			r.errorf(pos, "dhcp %s: %s: %v", n, what, err)
			return netip.Addr{}, false
		}
		return addr, true
	}
	if !a.Addr.Is4() {
		r.errorf(pos, "dhcp %s: %s %s is not an IPv4 address", n, what, a.Addr)
		return netip.Addr{}, false
	}
	return a.Addr, true
}

func (r *resolver) subnetAddrs(pos config.Pos, n netip.Prefix, list []config.HostAddr, what string, inside bool) []netip.Addr {
	var out []netip.Addr
	for _, a := range list {
		addr, ok := r.subnetAddr(pos, n, a, what)
		if !ok {
			continue
		}
		if inside && !n.Contains(addr) {
			r.errorf(pos, "dhcp %s: %s %s is outside the network", n, what, addr)
			continue
		}
		out = append(out, addr)
	}
	return out
}

// domainName checks a name for domain or search and returns it without
// the trailing dot.
func (r *resolver) domainName(pos config.Pos, name string) string {
	abs := strings.TrimSuffix(name, ".") + "."
	if err := validName(abs); err != nil {
		r.errorf(pos, "%v", err)
	}
	return strings.TrimSuffix(abs, ".")
}

// dhcpOptions resolves the options of a dhcp block or profile.
func (r *resolver) dhcpOptions(pos config.Pos, n netip.Prefix, o config.DHCPOptions) DHCPOptions {
	var out DHCPOptions
	out.Routers = r.subnetAddrs(pos, n, o.Routers, "router", true)
	out.DNSServers = r.subnetAddrs(pos, n, o.DNSServers, "dns-server", false)
	out.NTPServers = r.subnetAddrs(pos, n, o.NTPServers, "ntp-server", false)
	if o.Domain != nil {
		out.Domain = r.domainName(pos, *o.Domain)
	}
	for _, name := range o.Search {
		out.Search = append(out.Search, r.domainName(pos, name))
	}
	override(&out.Lease, o.Lease)
	override(&out.MaxLease, o.MaxLease)
	if out.Lease != 0 && out.MaxLease != 0 && out.Lease > out.MaxLease {
		r.errorf(pos, "dhcp %s: lease (%d) must not be longer than max-lease (%d)", n, out.Lease, out.MaxLease)
	}
	return out
}

// profileDefined reports whether any dhcp block defines a profile name.
func (r *resolver) profileDefined(name string) bool {
	for _, d := range r.cfg.DHCP {
		for _, p := range d.Profiles {
			if p.Name == name {
				return true
			}
		}
	}
	return false
}

// dhcp builds the subnets of the dhcp blocks and places the hosts with dhcp
// on in them, in their profile if they name one.
func (r *resolver) dhcp(hosts []dhcpHost) []*Subnet {
	var subnets []*Subnet
	for _, d := range r.cfg.DHCP {
		if i := slices.IndexFunc(subnets, func(s *Subnet) bool { return s.Network.Overlaps(d.Network) }); i >= 0 {
			r.errorf(d.Pos, "dhcp networks %s and %s overlap", d.Network, subnets[i].Network)
			continue
		}
		n := d.Network
		s := &Subnet{Network: n}
		for _, cr := range d.Ranges {
			low, ok1 := r.subnetAddr(cr.Pos, n, cr.Low, "range")
			high, ok2 := r.subnetAddr(cr.Pos, n, cr.High, "range")
			if !ok1 || !ok2 {
				continue
			}
			switch {
			case !n.Contains(low) || !n.Contains(high):
				r.errorf(cr.Pos, "dhcp %s: range %s-%s is outside the network", n, low, high)
				continue
			case low.Compare(high) > 0:
				r.errorf(cr.Pos, "dhcp %s: range %s-%s ends before it starts", n, low, high)
				continue
			}
			rng := Range{Low: low, High: high}
			if i := slices.IndexFunc(s.Ranges, func(o Range) bool {
				return o.Low.Compare(rng.High) <= 0 && rng.Low.Compare(o.High) <= 0
			}); i >= 0 {
				o := s.Ranges[i]
				r.errorf(cr.Pos, "dhcp %s: ranges %s-%s and %s-%s overlap", n, low, high, o.Low, o.High)
				continue
			}
			s.Ranges = append(s.Ranges, rng)
		}
		s.DHCPOptions = r.dhcpOptions(d.Pos, n, d.DHCPOptions)
		for _, cp := range d.Profiles {
			s.Profiles = append(s.Profiles, &Profile{Name: cp.Name, DHCPOptions: r.dhcpOptions(cp.Pos, n, cp.DHCPOptions)})
		}
		subnets = append(subnets, s)
	}

	names := map[string]int{}
	for _, h := range hosts {
		inNetwork := false
		for _, s := range subnets {
			var addrs []netip.Addr
			for _, a := range h.addrs {
				if !s.Network.Contains(a) {
					continue
				}
				inNetwork = true
				if i := slices.IndexFunc(s.Ranges, func(rg Range) bool {
					return rg.Low.Compare(a) <= 0 && a.Compare(rg.High) <= 0
				}); i >= 0 {
					rg := s.Ranges[i]
					r.errorf(h.pos, "host %s: fixed address %s lies in the dynamic range %s-%s", h.name, a, rg.Low, rg.High)
					continue
				}
				addrs = append(addrs, a)
			}
			if len(addrs) == 0 {
				continue
			}
			dst := &s.Hosts
			if h.profile != "" {
				i := slices.IndexFunc(s.Profiles, func(p *Profile) bool { return p.Name == h.profile })
				if i < 0 {
					r.errorf(h.pos, "host %s: dhcp %s has no profile %s", h.name, s.Network, h.profile)
					continue
				}
				dst = &s.Profiles[i].Hosts
			}
			base := strings.TrimSuffix(h.name, ".")
			short, rest, _ := strings.Cut(base, ".")
			for _, mac := range h.macs {
				names[base]++
				name := base
				if k := names[base]; k > 1 {
					name = fmt.Sprintf("%s-%d", short, k)
					if rest != "" {
						name += "." + rest
					}
				}
				*dst = append(*dst, DHCPHost{Name: name, HostName: short, MAC: mac, Addrs: addrs})
			}
		}
		if !inNetwork {
			r.errorf(h.pos, "host %s: dhcp needs an IPv4 address in the network of a dhcp block", h.name)
		}
	}

	// By address; for the same address, in configuration order.
	byAddr := func(a, b DHCPHost) int { return a.Addrs[0].Compare(b.Addrs[0]) }
	for _, s := range subnets {
		slices.SortStableFunc(s.Hosts, byAddr)
		for _, p := range s.Profiles {
			slices.SortStableFunc(p.Hosts, byAddr)
		}
	}
	return subnets
}
