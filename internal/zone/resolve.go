package zone

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/isnogudus/zonefile-go/internal/config"
)

// settings are the effective options of a scope.
type settings struct {
	email   string
	ttl     uint32
	refresh uint32
	retry   uint32
	expire  uint32
	negTTL  uint32
	serial  *uint32
	ptr     bool
}

func defaults() settings {
	return settings{
		ttl:     DefaultTTL,
		refresh: DefaultRefresh,
		retry:   DefaultRetry,
		expire:  DefaultExpire,
		negTTL:  DefaultNegativeTTL,
		ptr:     true,
	}
}

func override[T any](dst *T, v *T) {
	if v != nil {
		*dst = *v
	}
}

// with returns s overridden by the settings of a scope.
func (s settings) with(o config.Options) settings {
	override(&s.email, o.Email)
	override(&s.ttl, o.TTL)
	override(&s.refresh, o.Refresh)
	override(&s.retry, o.Retry)
	override(&s.expire, o.Expire)
	override(&s.negTTL, o.NegativeTTL)
	if o.Serial != nil {
		s.serial = o.Serial
	}
	override(&s.ptr, o.PTR)
	return s
}

// ptrCandidate is an address of a host that wants a PTR record.
type ptrCandidate struct {
	addr   netip.Addr
	target string
	ttl    uint32
	pos    config.Pos
}

type resolver struct {
	cfg       *config.Config
	errs      config.ErrorList
	warns     config.ErrorList
	notes     config.ErrorList
	dhcpHosts []dhcpHost
	// macs maps every MAC address to the host that gives it.
	macs map[string]config.Pos
	// pending are the hosts that refer to other hosts, refOwners their
	// names and aliases.
	pending   []pendingRef
	refOwners map[string]bool
}

// Result is a resolved configuration.
type Result struct {
	// Zones are the forward zones in configuration order, followed by the
	// reverse zones.
	Zones []*Zone
	// DHCP are the subnets of the dhcp blocks, in configuration order;
	// DHCPGlobal is the top level of dhcpd.conf, nil without a global dhcp
	// block.
	DHCP       []*Subnet
	DHCPGlobal *DHCPGlobal
	// Warnings do not stop the output from being written.
	Warnings config.ErrorList
	// Notes point out valid configurations that may not be intended, such
	// as a profile that hides a global one. zonefile-go shows them with -n.
	Notes config.ErrorList
}

func (r *resolver) errorf(pos config.Pos, format string, args ...any) {
	r.errs = append(r.errs, &config.Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

func (r *resolver) notef(pos config.Pos, format string, args ...any) {
	r.notes = append(r.notes, &config.Error{Pos: pos, Msg: "note: " + fmt.Sprintf(format, args...)})
}

func (r *resolver) warnf(pos config.Pos, format string, args ...any) {
	r.warns = append(r.warns, &config.Error{Pos: pos, Msg: "warning: " + fmt.Sprintf(format, args...)})
}

// name makes name absolute relative to origin and validates it. A
// relative name that looks like a full one gets a warning, since a missing
// trailing dot silently appends the zone name.
func (r *resolver) name(pos config.Pos, name, origin string) (string, bool) {
	abs := absolute(name, origin)
	if err := validName(abs); err != nil {
		r.errorf(pos, "%v", err)
		return "", false
	}
	if why := looksAbsolute(name, origin); why != "" {
		r.warnf(pos, "%q %s but is relative, so it becomes %s; add a trailing dot if you mean %s.",
			name, why, abs, name)
	}
	return abs, true
}

// owner is name for the owner of a record: besides the checks of name, it
// must lie within the zone, or the zone file would hold out-of-zone data.
// what names the statement for the error message.
func (r *resolver) owner(pos config.Pos, what, name, origin string) (string, bool) {
	abs, ok := r.name(pos, name, origin)
	if !ok {
		return "", false
	}
	lower, zone := strings.ToLower(abs), strings.ToLower(origin)
	if lower != zone && !strings.HasSuffix(lower, "."+zone) {
		r.errorf(pos, "%s %s is outside zone %s", what, abs, origin)
		return "", false
	}
	return abs, true
}

// Resolve builds the zones and dhcp subnets of cfg. On failure the error
// is a config.ErrorList with every problem found; the warnings are
// returned in either case.
func Resolve(cfg *config.Config) (*Result, error) {
	r := &resolver{cfg: cfg, macs: map[string]config.Pos{}, refOwners: map[string]bool{},
		notes: append(config.ErrorList{}, cfg.Notes...)}
	global := defaults().with(cfg.Options)

	var zones []*Zone
	var ptrs []ptrCandidate
	declared := map[string]config.Pos{}
	for _, cz := range cfg.Zones {
		z, cands := r.forward(cz, global)
		if z == nil {
			continue
		}
		key := strings.ToLower(z.Name)
		if pos, dup := declared[key]; dup {
			r.errorf(cz.Pos, "zone %s already declared at %s", z.Name, pos)
			continue
		}
		declared[key] = cz.Pos
		zones = append(zones, z)
		ptrs = append(ptrs, cands...)
	}
	r.resolveRefs(zones)
	zones = append(zones, r.reverse(global, ptrs)...)
	r.checkNameservers(zones)
	subnets := r.dhcp(r.dhcpHosts)
	r.checkDHCPNameservers(zones, subnets)

	res := &Result{Zones: zones, DHCP: subnets, DHCPGlobal: r.dhcpGlobal(), Warnings: r.warns, Notes: r.notes}
	if len(r.errs) > 0 {
		return &Result{Warnings: r.warns, Notes: r.notes}, r.errs
	}
	return res, nil
}

func (r *resolver) soa(pos config.Pos, name string, s settings) SOA {
	if s.email == "" {
		r.errorf(pos, `zone %s has no e-mail address, add "email"`, name)
	}
	if s.retry >= s.refresh {
		r.errorf(pos, "zone %s: retry (%d) must be less than refresh (%d)", name, s.retry, s.refresh)
	}
	return SOA{
		Email:   rname(s.email),
		Serial:  s.serial,
		Refresh: s.refresh,
		Retry:   s.retry,
		Expire:  s.expire,
		Minimum: s.negTTL,
	}
}

// nameservers resolves the nameservers of a zone, falling back to the
// top-level ones.
func (r *resolver) nameservers(pos config.Pos, origin string, own []config.Nameserver, ttl uint32) []NS {
	src := own
	if len(src) == 0 {
		src = r.cfg.Nameservers
	}
	if len(src) == 0 {
		r.errorf(pos, "zone %s has no nameserver", origin)
		return nil
	}
	var out []NS
	for _, n := range src {
		name, ok := r.name(n.Pos, n.Name, origin)
		if !ok {
			continue
		}
		t := ttl
		override(&t, n.TTL)
		out = append(out, NS{Name: name, TTL: t})
	}
	return out
}

// owners records the owner names of a zone to find duplicates and CNAME
// conflicts. Keys are lower case.
type owners struct {
	hosts   map[string]config.Pos
	records map[string]config.Pos // name + " " + address
	other   map[string]config.Pos // every non-CNAME owner
	cnames  map[string]config.Pos
}

func newOwners() *owners {
	return &owners{
		hosts:   map[string]config.Pos{},
		records: map[string]config.Pos{},
		other:   map[string]config.Pos{},
		cnames:  map[string]config.Pos{},
	}
}

func (r *resolver) forward(cz *config.Zone, global settings) (*Zone, []ptrCandidate) {
	s := global.with(cz.Options)
	name := strings.TrimSuffix(cz.Name, ".") + "."
	if err := validName(name); err != nil || strings.HasPrefix(name, "*") {
		r.errorf(cz.Pos, "invalid zone name %q", cz.Name)
		return nil, nil
	}

	z := &Zone{Name: name, TTL: s.ttl, SOA: r.soa(cz.Pos, name, s)}
	z.NS = r.nameservers(cz.Pos, name, cz.Nameservers, s.ttl)

	mxs := cz.MX
	if len(mxs) == 0 && !cz.NoMX {
		mxs = r.cfg.MX
	}
	for _, m := range mxs {
		mxName, ok := r.name(m.Pos, m.Name, name)
		if !ok {
			continue
		}
		mx := MX{Name: mxName, Priority: DefaultMXPriority, TTL: s.ttl}
		override(&mx.Priority, m.Priority)
		override(&mx.TTL, m.TTL)
		z.MX = append(z.MX, mx)
	}

	zoneDHCP := len(cz.DHCPBlocks) > 0
	override(&zoneDHCP, cz.Options.DHCP)
	if p := cz.Options.DHCPProfile; p != nil && !zoneDHCP && !r.profileDefined(*p) {
		r.warnf(cz.Pos, "zone %s: dhcp-profile %s is not defined in any dhcp block", name, *p)
	}

	own := newOwners()
	own.other[strings.ToLower(name)] = cz.Pos // apex: SOA and NS

	var ptrs []ptrCandidate
	for _, h := range cz.Hosts {
		ptrs = append(ptrs, r.host(z, cz, h, s, own)...)
	}
	for _, c := range cz.CNAMEs {
		r.cname(z, c, s, own)
	}
	for _, sv := range cz.SRVs {
		r.srv(z, sv, s, own)
	}
	return z, ptrs
}

// hostAddrs resolves the addresses of h: suffixes per network of the
// zone, then "no inet" and "no inet6".
func (r *resolver) hostAddrs(cz *config.Zone, h config.Host) ([]netip.Addr, bool) {
	var addrs []netip.Addr
	refs := 0
	for _, a := range h.Addrs {
		if a.IsRef() {
			refs++ // resolved once all zones are, see resolveRefs
			continue
		}
		if !a.IsSuffix() {
			addrs = append(addrs, a.Addr)
			continue
		}
		if len(cz.Networks) == 0 {
			r.errorf(h.Pos, "host %s: address suffix %s needs a network statement in the zone", h.Name, suffixString(a.Suffix))
			return nil, false
		}
		for _, n := range cz.Networks {
			addr, err := applySuffix(n, a.Suffix)
			if err != nil {
				r.errorf(h.Pos, "host %s: %v", h.Name, err)
				return nil, false
			}
			addrs = append(addrs, addr)
		}
	}
	addrs = families(cz, h).filter(addrs)
	if len(addrs) == 0 && refs == 0 {
		r.errorf(h.Pos, "host %s has no addresses left", h.Name)
		return nil, false
	}
	for i, a := range addrs {
		if slices.Contains(addrs[:i], a) {
			r.errorf(h.Pos, "host %s: address %s given twice", h.Name, a)
			return nil, false
		}
	}
	return addrs, true
}

func (r *resolver) host(z *Zone, cz *config.Zone, h config.Host, s settings, own *owners) []ptrCandidate {
	name, ok := r.owner(h.Pos, "host", h.Name, z.Name)
	if !ok {
		return nil
	}
	key := strings.ToLower(name)
	if pos, dup := own.hosts[key]; dup {
		r.errorf(h.Pos, "host %s already declared at %s", name, pos)
		return nil
	}
	own.hosts[key] = h.Pos

	var aliases []string
	for _, a := range h.Aliases {
		if alias, ok := r.owner(h.Pos, "alias", a, z.Name); ok {
			aliases = append(aliases, alias)
		}
	}
	addrs, ok := r.hostAddrs(cz, h)
	if !ok {
		return nil
	}

	ttl := s.ttl
	override(&ttl, h.TTL)
	ptr := s.ptr
	override(&ptr, h.PTR)
	if strings.HasPrefix(name, "*") {
		ptr = false
	}

	r.hostDHCP(cz, h, name, addrs)

	// Addresses taken from other hosts follow once all zones are resolved.
	if slices.ContainsFunc(h.Addrs, config.HostAddr.IsRef) {
		ref := pendingRef{zone: z, own: own, host: h, name: name, aliases: aliases, ttl: ttl, direct: len(addrs), families: families(cz, h)}
		for _, a := range h.Addrs {
			if a.IsRef() {
				ref.refs = append(ref.refs, a.Ref)
			}
		}
		for _, owner := range append([]string{name}, aliases...) {
			own.other[strings.ToLower(owner)] = h.Pos
			r.refOwners[strings.ToLower(owner)] = true
		}
		r.pending = append(r.pending, ref)
	}

	var ptrs []ptrCandidate
	for _, addr := range addrs {
		for _, owner := range append([]string{name}, aliases...) {
			rec := strings.ToLower(owner) + " " + addr.String()
			if pos, dup := own.records[rec]; dup {
				r.errorf(h.Pos, "%s %s duplicates the record from %s", owner, addr, pos)
				continue
			}
			own.records[rec] = h.Pos
			own.other[strings.ToLower(owner)] = h.Pos
			z.Addresses = append(z.Addresses, Address{Name: owner, Addr: addr, TTL: ttl})
		}
		if ptr {
			ptrs = append(ptrs, ptrCandidate{addr: addr, target: name, ttl: ttl, pos: h.Pos})
		}
	}
	return ptrs
}

func (r *resolver) cname(z *Zone, c config.CNAME, s settings, own *owners) {
	name, ok1 := r.owner(c.Pos, "cname", c.Name, z.Name)
	target, ok2 := r.name(c.Pos, c.Target, z.Name)
	if !ok1 || !ok2 {
		return
	}
	key := strings.ToLower(name)
	if pos, dup := own.cnames[key]; dup {
		r.errorf(c.Pos, "cname %s already declared at %s", name, pos)
		return
	}
	if pos, dup := own.other[key]; dup {
		r.errorf(c.Pos, "cname %s conflicts with other records for that name at %s", name, pos)
		return
	}
	own.cnames[key] = c.Pos
	ttl := s.ttl
	override(&ttl, c.TTL)
	z.CNAMEs = append(z.CNAMEs, CNAME{Name: name, Target: target, TTL: ttl})
}

func (r *resolver) srv(z *Zone, sv config.SRV, s settings, own *owners) {
	name, ok1 := r.owner(sv.Pos, "srv", sv.Name, z.Name)
	target, ok2 := r.name(sv.Pos, sv.Target, z.Name)
	if !ok1 || !ok2 {
		return
	}
	key := strings.ToLower(name)
	if pos, dup := own.cnames[key]; dup {
		r.errorf(sv.Pos, "srv %s conflicts with the cname at %s", name, pos)
		return
	}
	own.other[key] = sv.Pos
	rec := SRV{Name: name, Target: target, Port: sv.Port, Priority: DefaultSRVPriority, Weight: DefaultSRVWeight, TTL: s.ttl}
	override(&rec.Priority, sv.Priority)
	override(&rec.Weight, sv.Weight)
	override(&rec.TTL, sv.TTL)
	z.SRVs = append(z.SRVs, rec)
}

// reverse builds the reverse zones and places each PTR candidate in the
// zone whose network contains its address. Candidates outside every
// reverse network are dropped: those addresses are not ours to answer for.
func (r *resolver) reverse(global settings, cands []ptrCandidate) []*Zone {
	type reverseZone struct {
		net  netip.Prefix
		zone *Zone
	}
	var all []reverseZone
	for _, cr := range r.cfg.Reverse {
		s := global.with(cr.Options)
	nets:
		for _, n := range cr.Networks {
			if !reverseAligned(n) {
				unit := 8
				if n.Addr().Is6() {
					unit = 4
				}
				r.errorf(cr.Pos, "reverse network %s: prefix length must be a multiple of %d", n, unit)
				continue
			}
			for _, o := range all {
				if o.net.Overlaps(n) {
					r.errorf(cr.Pos, "reverse networks %s and %s overlap", n, o.net)
					continue nets
				}
			}
			name := reverseZoneName(n)
			z := &Zone{Name: name, Reverse: true, TTL: s.ttl, SOA: r.soa(cr.Pos, name, s)}
			z.NS = r.nameservers(cr.Pos, name, cr.Nameservers, s.ttl)
			all = append(all, reverseZone{net: n, zone: z})
		}
	}

	claimed := map[netip.Addr]ptrCandidate{}
	for _, c := range cands {
		i := slices.IndexFunc(all, func(rz reverseZone) bool { return rz.net.Contains(c.addr) })
		if i < 0 {
			continue
		}
		if prev, dup := claimed[c.addr]; dup {
			r.errorf(c.pos, `%s already gets a PTR to %s from %s, add "no ptr" to one of the hosts`, c.addr, prev.target, prev.pos)
			continue
		}
		claimed[c.addr] = c
		z := all[i].zone
		z.PTRs = append(z.PTRs, PTR{Name: reverseAddrName(c.addr), Addr: c.addr, Target: c.target, TTL: c.ttl})
	}

	zones := make([]*Zone, len(all))
	for i, rz := range all {
		slices.SortFunc(rz.zone.PTRs, func(a, b PTR) int { return a.Addr.Compare(b.Addr) })
		zones[i] = rz.zone
	}
	return zones
}

// hostDHCP checks the MAC addresses of a host and, if dhcp is on for it,
// collects it for the dhcp subnets. dhcp on the host overrides the zone,
// and is off unless one of them turns it on.
func (r *resolver) hostDHCP(cz *config.Zone, h config.Host, name string, addrs []netip.Addr) {
	if len(h.MACs) > 0 && strings.HasPrefix(name, "*") {
		r.errorf(h.Pos, "host %s: a wildcard cannot have a mac", name)
		return
	}
	for _, mac := range h.MACs {
		if pos, dup := r.macs[mac]; dup {
			r.errorf(h.Pos, "host %s: MAC address %s already used at %s", name, mac, pos)
		}
		r.macs[mac] = h.Pos
	}

	on := len(cz.DHCPBlocks) > 0
	override(&on, cz.Options.DHCP)
	override(&on, h.DHCP)
	var profile string
	override(&profile, cz.Options.DHCPProfile)
	override(&profile, h.DHCPProfile)
	switch {
	case h.DHCP != nil && *h.DHCP && len(h.MACs) == 0:
		r.errorf(h.Pos, "host %s: dhcp needs a mac", name)
		return
	case !on || len(h.MACs) == 0:
		// A profile rests like a mac; only catch names defined nowhere.
		// The zone's own profile is checked once for the zone.
		if h.DHCPProfile != nil && !r.profileDefined(*h.DHCPProfile) {
			r.warnf(h.Pos, "host %s: dhcp-profile %s is not defined in any dhcp block", name, *h.DHCPProfile)
		}
		return
	}

	var v4 []netip.Addr
	for _, a := range addrs {
		if a.Is4() {
			v4 = append(v4, a)
		}
	}
	if len(v4) == 0 {
		r.errorf(h.Pos, "host %s: dhcp needs an IPv4 address", name)
		return
	}
	r.dhcpHosts = append(r.dhcpHosts, dhcpHost{pos: h.Pos, name: name, addrs: v4, macs: h.MACs, profile: profile})
}

// checkNameservers reports nameservers whose name lies in a forward zone
// of the configuration but has no A or AAAA record there, or is a CNAME,
// which RFC 2181 forbids. All records of such a zone come from the
// configuration, so the address cannot come from elsewhere. Nameservers in
// other zones are not checked.
func (r *resolver) checkNameservers(zones []*Zone) {
	// Records count only in their own zone: a name below a zone of its own
	// must have its address there, not in the parent.
	var forward []string
	addrs := map[string]bool{}  // zone + " " + name
	cnames := map[string]bool{} // zone + " " + name
	for _, z := range zones {
		if z.Reverse {
			continue
		}
		zn := strings.ToLower(z.Name)
		forward = append(forward, zn)
		for _, a := range z.Addresses {
			addrs[zn+" "+strings.ToLower(a.Name)] = true
		}
		for _, c := range z.CNAMEs {
			cnames[zn+" "+strings.ToLower(c.Name)] = true
		}
	}
	inZone := func(name string) string {
		best := ""
		for _, z := range forward {
			if (name == z || strings.HasSuffix(name, "."+z)) && len(z) > len(best) {
				best = z
			}
		}
		return best
	}

	// Each nameserver statement once, with its own position.
	type use struct {
		pos  config.Pos
		name string
	}
	var uses []use
	for _, n := range r.cfg.Nameservers {
		uses = append(uses, use{n.Pos, n.Name})
	}
	for _, cz := range r.cfg.Zones {
		origin := strings.TrimSuffix(cz.Name, ".") + "."
		for _, n := range cz.Nameservers {
			uses = append(uses, use{n.Pos, absolute(n.Name, origin)})
		}
	}
	for _, cr := range r.cfg.Reverse {
		for _, n := range cr.Nameservers {
			uses = append(uses, use{n.Pos, n.Name})
		}
	}
	for _, u := range uses {
		name := strings.ToLower(u.name)
		z := inZone(name)
		switch {
		case z == "":
		case cnames[z+" "+name]:
			r.errorf(u.pos, "nameserver %s is a CNAME in zone %s; a nameserver needs an A or AAAA record (RFC 2181)", u.name, z)
		case !addrs[z+" "+name]:
			r.errorf(u.pos, "nameserver %s has no address: zone %s has no A or AAAA record for it", u.name, z)
		}
	}
}

// pendingRef is a host that takes the addresses of other hosts.
type pendingRef struct {
	zone     *Zone
	own      *owners
	host     config.Host
	name     string
	aliases  []string
	ttl      uint32
	refs     []string
	direct   int // number of addresses of its own
	families addrFamilies
}

// addrFamilies are the address families in effect for a host.
type addrFamilies struct{ inet, inet6 bool }

// families returns the families of h: inet and inet6 on the host override
// no inet and no inet6 of the zone; without either, both are on.
func families(cz *config.Zone, h config.Host) addrFamilies {
	f := addrFamilies{true, true}
	override(&f.inet, cz.Options.Inet)
	override(&f.inet6, cz.Options.Inet6)
	override(&f.inet, h.Inet)
	override(&f.inet6, h.Inet6)
	return f
}

// filter drops the addresses of the families that are off.
func (f addrFamilies) filter(addrs []netip.Addr) []netip.Addr {
	return slices.DeleteFunc(addrs, func(a netip.Addr) bool {
		return a.Is4() && !f.inet || a.Is6() && !f.inet6
	})
}

// resolveRefs gives the hosts that refer to other hosts the A and AAAA
// records of those, after all forward zones are resolved, so that a host
// may refer to one declared later. The addresses get no PTR records: the
// host referred to has them. A reference to a host that refers itself is
// an error, so chains and cycles cannot arise.
func (r *resolver) resolveRefs(zones []*Zone) {
	if len(r.pending) == 0 {
		return
	}
	byName := map[string][]netip.Addr{}
	for _, z := range zones {
		for _, a := range z.Addresses {
			key := strings.ToLower(a.Name)
			byName[key] = append(byName[key], a.Addr)
		}
	}
	for _, p := range r.pending {
		h := p.host
		var addrs []netip.Addr
		ok := true
		for _, ref := range p.refs {
			key := strings.ToLower(ref)
			switch found := byName[key]; {
			case r.refOwners[key]:
				r.errorf(h.Pos, "host %s: %s takes its addresses from another host itself", p.name, ref)
				ok = false
			case len(found) == 0:
				r.errorf(h.Pos, "host %s: %s has no address in the configured zones", p.name, ref)
				ok = false
			default:
				addrs = append(addrs, found...)
			}
		}
		if !ok {
			continue
		}
		addrs = p.families.filter(addrs)
		if len(addrs) == 0 && p.direct == 0 {
			r.errorf(h.Pos, "host %s has no addresses left", p.name)
			continue
		}
		for _, addr := range addrs {
			for _, owner := range append([]string{p.name}, p.aliases...) {
				rec := strings.ToLower(owner) + " " + addr.String()
				if pos, dup := p.own.records[rec]; dup {
					r.errorf(h.Pos, "%s %s duplicates the record from %s", owner, addr, pos)
					continue
				}
				p.own.records[rec] = h.Pos
				p.zone.Addresses = append(p.zone.Addresses, Address{Name: owner, Addr: addr, TTL: p.ttl})
			}
		}
	}
}
