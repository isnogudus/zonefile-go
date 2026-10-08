package zone

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/isnogudus/zonefile-go/internal/config"
)

// DHCPGlobal is what the global dhcp block gives for the top level of
// dhcpd.conf: everything that does not depend on the subnet. dhcpd passes
// it on to the subnets and groups itself.
type DHCPGlobal struct {
	// ServerID is invalid if the dhcpd default is kept, or if it is a
	// suffix and so written into each subnet.
	ServerID netip.Addr
	// Authoritative is nil if the dhcpd default is kept.
	Authoritative *bool
	DHCPOptions
}

// Subnet is a dhcp block with all addresses resolved, and the hosts with
// dhcp on whose IPv4 address lies in it.
type Subnet struct {
	Network netip.Prefix
	Ranges  []Range
	// ServerID and Authoritative are written into the subnet; invalid or
	// nil if it inherits them or the dhcpd default.
	ServerID      netip.Addr
	Authoritative *bool
	// DHCPOptions are the options in effect for the subnet, after
	// inheritance from the global block; the checks use them.
	DHCPOptions
	// Write are the options written into the subnet: those the block
	// gives, and those of the global block that use suffixes and so differ
	// per subnet. The rest comes from the top level of dhcpd.conf.
	Write DHCPOptions
	// Hosts are the hosts without a profile. Hosts here and in profiles are
	// sorted by address, and for the same address in the order of the
	// configuration.
	Hosts []DHCPHost
	// Profiles are the profiles of the block in configuration order, with
	// the hosts that use them.
	Profiles []*Profile
	pos      config.Pos
}

// DHCPOptions are the options of a subnet or a profile. Zero values are
// not written.
type DHCPOptions struct {
	Routers     []netip.Addr
	DNSServers  []netip.Addr
	NTPServers  []netip.Addr
	SMTPServers []netip.Addr
	// Domain and Search are without the trailing dot.
	Domain          string
	Search          []string
	AutoproxyScript string
	// Lease and MaxLease are in seconds.
	Lease    uint32
	MaxLease uint32
	// GetLeaseHostnames is nil if the dhcpd default is kept.
	GetLeaseHostnames *bool
}

// Profile is a profile of a dhcp block or the global dhcp block, resolved
// for one subnet, and the hosts of that subnet that use it.
type Profile struct {
	Name string
	DHCPOptions
	Hosts []DHCPHost
	src   *config.DHCPProfile
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
	if a.IsRef() {
		r.errorf(pos, "dhcp %s: %s %s: only hosts may refer to the addresses of other hosts", n, what, a.Ref)
		return netip.Addr{}, false
	}
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
		if inside && n.IsValid() && !n.Contains(addr) {
			r.errorf(pos, "dhcp %s: %s %s is outside the network", n, what, addr)
			continue
		}
		out = append(out, addr)
	}
	return out
}

// domainName checks a name for domain-name or domain-search, named by
// what, and returns it without the trailing dot. A single label is almost
// never meant as a top-level domain, so it gets a warning.
func (r *resolver) domainName(pos config.Pos, what, name string) string {
	if name == "@" {
		// A dhcp block in a zone has replaced it with the zone already.
		r.errorf(pos, `"@" is only allowed in a dhcp block in a zone`)
		return name
	}
	abs := strings.TrimSuffix(name, ".") + "."
	if err := validName(abs); err != nil {
		r.errorf(pos, "%v", err)
	} else if !strings.Contains(strings.TrimSuffix(abs, "."), ".") {
		r.warnf(pos, "%s %s is a single label, so a top-level domain; give the full name, or @ for the zone in a dhcp block of a zone", what, strings.TrimSuffix(abs, "."))
	}
	return strings.TrimSuffix(abs, ".")
}

// dhcpOptions resolves the options of a dhcp block or profile for the
// subnet n.
func (r *resolver) dhcpOptions(pos config.Pos, n netip.Prefix, o config.DHCPOptions) DHCPOptions {
	var out DHCPOptions
	out.Routers = r.subnetAddrs(pos, n, o.Routers, "option routers", true)
	out.DNSServers = r.subnetAddrs(pos, n, o.DNSServers, "option domain-name-servers", false)
	out.NTPServers = r.subnetAddrs(pos, n, o.NTPServers, "option ntp-servers", false)
	out.SMTPServers = r.subnetAddrs(pos, n, o.SMTPServers, "option smtp-server", false)
	override(&out.AutoproxyScript, o.AutoproxyScript)
	out.GetLeaseHostnames = o.GetLeaseHostnames
	if o.Domain != nil {
		out.Domain = r.domainName(pos, "option domain-name", *o.Domain)
	}
	for _, name := range o.Search {
		out.Search = append(out.Search, r.domainName(pos, "option domain-search", name))
	}
	override(&out.Lease, o.Lease)
	override(&out.MaxLease, o.MaxLease)
	return out
}

// overlay returns base with every option replaced that set gives; top is
// set resolved. Lists are replaced as a whole, not merged.
func overlay(base, top DHCPOptions, set config.DHCPOptions) DHCPOptions {
	if set.Routers != nil {
		base.Routers = top.Routers
	}
	if set.DNSServers != nil {
		base.DNSServers = top.DNSServers
	}
	if set.NTPServers != nil {
		base.NTPServers = top.NTPServers
	}
	if set.SMTPServers != nil {
		base.SMTPServers = top.SMTPServers
	}
	if set.AutoproxyScript != nil {
		base.AutoproxyScript = top.AutoproxyScript
	}
	if set.GetLeaseHostnames != nil {
		base.GetLeaseHostnames = top.GetLeaseHostnames
	}
	if set.Domain != nil {
		base.Domain = top.Domain
	}
	if set.Search != nil {
		base.Search = top.Search
	}
	if set.Lease != nil {
		base.Lease = top.Lease
	}
	if set.MaxLease != nil {
		base.MaxLease = top.MaxLease
	}
	return base
}

func (r *resolver) checkLease(pos config.Pos, n netip.Prefix, o DHCPOptions) {
	if o.Lease != 0 && o.MaxLease != 0 && o.Lease > o.MaxLease {
		r.errorf(pos, "dhcp %s: default-lease-time (%d) must not be longer than max-lease-time (%d)", n, o.Lease, o.MaxLease)
	}
}

// profileDefined reports whether any dhcp block, the global one included,
// defines a profile name.
func (r *resolver) profileDefined(name string) bool {
	blocks := r.cfg.DHCP
	if g := r.cfg.DHCPDefaults; g != nil {
		blocks = append([]*config.DHCP{g}, blocks...)
	}
	for _, d := range blocks {
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
		s := &Subnet{Network: n, pos: d.Pos}
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
		if d.ServerID != nil {
			s.ServerID, _ = r.subnetAddr(d.Pos, n, *d.ServerID, "server-identifier")
		}
		s.Authoritative = d.Authoritative
		// Defaults, then the block, then the profile.
		glob := r.cfg.DHCPDefaults
		var base, perSubnet DHCPOptions
		if glob != nil {
			base = r.dhcpOptions(glob.Pos, n, glob.DHCPOptions)
			_, local := splitGlobal(glob.DHCPOptions)
			perSubnet = overlay(DHCPOptions{}, base, local)
			if d.ServerID == nil && glob.ServerID != nil && glob.ServerID.IsSuffix() {
				s.ServerID, _ = r.subnetAddr(glob.Pos, n, *glob.ServerID, "server-identifier")
			}
		}
		own := r.dhcpOptions(d.Pos, n, d.DHCPOptions)
		s.DHCPOptions = overlay(base, own, d.DHCPOptions)
		s.Write = overlay(perSubnet, own, d.DHCPOptions)
		r.checkLease(d.Pos, n, s.DHCPOptions)

		addProfile := func(cp *config.DHCPProfile) {
			p := &Profile{Name: cp.Name, DHCPOptions: r.dhcpOptions(cp.Pos, n, cp.DHCPOptions), src: cp}
			r.checkLease(cp.Pos, n, overlay(s.DHCPOptions, p.DHCPOptions, cp.DHCPOptions))
			s.Profiles = append(s.Profiles, p)
		}
		local := map[string]bool{}
		for _, cp := range d.Profiles {
			local[cp.Name] = true
		}
		if glob != nil {
			for _, gp := range glob.Profiles {
				if !local[gp.Name] {
					addProfile(gp)
				}
			}
		}
		for _, cp := range d.Profiles {
			if glob != nil {
				if i := slices.IndexFunc(glob.Profiles, func(g *config.DHCPProfile) bool { return g.Name == cp.Name }); i >= 0 {
					r.notef(cp.Pos, "dhcp-profile %s in dhcp %s hides the global dhcp-profile %s at %s", cp.Name, n, cp.Name, glob.Profiles[i].Pos)
				}
			}
			addProfile(cp)
		}
		subnets = append(subnets, s)
	}

	names := map[string]int{}
	used := map[*config.DHCPProfile]bool{}
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
					r.errorf(h.pos, "host %s: dhcp-profile %s is defined neither in dhcp %s nor in the global dhcp block", h.name, h.profile, s.Network)
					continue
				}
				dst = &s.Profiles[i].Hosts
				used[s.Profiles[i].src] = true
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

	var all []*config.DHCPProfile
	if g := r.cfg.DHCPDefaults; g != nil {
		all = append(all, g.Profiles...)
	}
	for _, d := range r.cfg.DHCP {
		all = append(all, d.Profiles...)
	}
	for _, cp := range all {
		if !used[cp] {
			r.notef(cp.Pos, "dhcp-profile %s is not used by any host with dhcp", cp.Name)
		}
	}

	// By address; for the same address, in configuration order.
	byAddr := func(a, b DHCPHost) int { return a.Addrs[0].Compare(b.Addrs[0]) }
	for _, s := range subnets {
		slices.SortStableFunc(s.Hosts, byAddr)
		hosts := len(s.Hosts)
		for _, p := range s.Profiles {
			slices.SortStableFunc(p.Hosts, byAddr)
			hosts += len(p.Hosts)
		}
		// Valid for a network dhcpd listens on but should not serve, yet
		// also the result of a forgotten dhcp.
		if len(s.Ranges) == 0 && hosts == 0 {
			r.notef(s.pos, "dhcp %s has no range and no host with dhcp; dhcpd answers no client there", s.Network)
		}
	}
	return subnets
}

// checkDHCPNameservers adds a note for every DHCP name server that is not
// a nameserver of the zone given as domain-name, if zonefile-go manages
// that zone and knows the addresses of all its nameservers. Clients would
// then ask a resolver that need not know the zone; with a forwarding or
// filtering resolver that is intended, hence only a note.
func (r *resolver) checkDHCPNameservers(zones []*Zone, subnets []*Subnet) {
	byName := map[string]*Zone{}
	addrs := map[string][]netip.Addr{}
	for _, z := range zones {
		if z.Reverse {
			continue
		}
		byName[strings.ToLower(z.Name)] = z
		for _, a := range z.Addresses {
			if a.Addr.Is4() {
				key := strings.ToLower(a.Name)
				addrs[key] = append(addrs[key], a.Addr)
			}
		}
	}

	check := func(pos config.Pos, where, domain string, servers []netip.Addr) {
		z := byName[strings.ToLower(domain)+"."]
		if z == nil || len(servers) == 0 {
			return
		}
		var nsAddrs []netip.Addr
		var known []string
		for _, ns := range z.NS {
			a := addrs[strings.ToLower(ns.Name)]
			if len(a) == 0 {
				return // a nameserver outside the managed zones
			}
			nsAddrs = append(nsAddrs, a...)
			s := make([]string, len(a))
			for i, x := range a {
				s[i] = x.String()
			}
			known = append(known, ns.Name+" is "+strings.Join(s, ", "))
		}
		for _, srv := range servers {
			if !slices.Contains(nsAddrs, srv) {
				r.notef(pos, "%s: option domain-name-servers %s is not a nameserver of zone %s (%s)",
					where, srv, z.Name, strings.Join(known, "; "))
			}
		}
	}

	for _, s := range subnets {
		where := fmt.Sprintf("dhcp %s", s.Network)
		check(s.pos, where, s.Domain, s.DNSServers)
		for _, p := range s.Profiles {
			if len(p.Hosts) == 0 {
				continue
			}
			domain, servers := s.Domain, s.DNSServers
			if p.Domain != "" {
				domain = p.Domain
			}
			if len(p.DNSServers) > 0 {
				servers = p.DNSServers
			}
			check(p.src.Pos, fmt.Sprintf("%s, dhcp-profile %s", where, p.Name), domain, servers)
		}
	}
}

// splitGlobal splits the options of the global dhcp block into those that
// go to the top level of dhcpd.conf and those that use suffixes, which
// differ per subnet and so go into each subnet.
func splitGlobal(o config.DHCPOptions) (top, perSubnet config.DHCPOptions) {
	top = o
	hasSuffix := func(l []config.HostAddr) bool {
		return slices.ContainsFunc(l, config.HostAddr.IsSuffix)
	}
	for _, f := range []struct{ top, per *[]config.HostAddr }{
		{&top.Routers, &perSubnet.Routers},
		{&top.DNSServers, &perSubnet.DNSServers},
		{&top.NTPServers, &perSubnet.NTPServers},
		{&top.SMTPServers, &perSubnet.SMTPServers},
	} {
		if hasSuffix(*f.top) {
			*f.per, *f.top = *f.top, nil
		}
	}
	return top, perSubnet
}

// dhcpGlobal returns the top level of dhcpd.conf from the global dhcp
// block, or nil without one.
func (r *resolver) dhcpGlobal() *DHCPGlobal {
	g := r.cfg.DHCPDefaults
	if g == nil {
		return nil
	}
	top, _ := splitGlobal(g.DHCPOptions)
	out := &DHCPGlobal{Authoritative: g.Authoritative, DHCPOptions: r.dhcpOptions(g.Pos, netip.Prefix{}, top)}
	if g.ServerID != nil && !g.ServerID.IsSuffix() {
		out.ServerID, _ = r.subnetAddr(g.Pos, netip.Prefix{}, *g.ServerID, "server-identifier")
	}
	return out
}
