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
	email     string
	ttl       uint32
	refresh   uint32
	retry     uint32
	expire    uint32
	negTTL    uint32
	serial    *uint32
	mxPrio    uint16
	srvPrio   uint16
	srvWeight uint16
	ptr       bool
}

func defaults() settings {
	return settings{
		ttl:       DefaultTTL,
		refresh:   DefaultRefresh,
		retry:     DefaultRetry,
		expire:    DefaultExpire,
		negTTL:    DefaultNegativeTTL,
		mxPrio:    DefaultMXPriority,
		srvPrio:   DefaultSRVPriority,
		srvWeight: DefaultSRVWeight,
		ptr:       true,
	}
}

func override[T any](dst *T, v *T) {
	if v != nil {
		*dst = *v
	}
}

// with returns s overridden by the options set in a scope.
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
	override(&s.mxPrio, o.MXPriority)
	override(&s.srvPrio, o.SRVPriority)
	override(&s.srvWeight, o.SRVWeight)
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
	cfg  *config.Config
	errs config.ErrorList
}

func (r *resolver) errorf(pos config.Pos, format string, args ...any) {
	r.errs = append(r.errs, &config.Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

// name makes name absolute relative to origin and validates it.
func (r *resolver) name(pos config.Pos, name, origin string) (string, bool) {
	abs := absolute(name, origin)
	if err := validName(abs); err != nil {
		r.errorf(pos, "%v", err)
		return "", false
	}
	return abs, true
}

// Resolve builds the forward zones of cfg, in configuration order,
// followed by its reverse zones. On failure the error is a
// config.ErrorList with every problem found.
func Resolve(cfg *config.Config) ([]*Zone, error) {
	r := &resolver{cfg: cfg}
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
	zones = append(zones, r.reverse(global, ptrs)...)

	if len(r.errs) > 0 {
		return nil, r.errs
	}
	return zones, nil
}

func (r *resolver) soa(pos config.Pos, name string, s settings) SOA {
	if s.email == "" {
		r.errorf(pos, `zone %s has no e-mail address, add "set email"`, name)
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
	if len(mxs) == 0 {
		mxs = r.cfg.MX
	}
	for _, m := range mxs {
		mxName, ok := r.name(m.Pos, m.Name, name)
		if !ok {
			continue
		}
		mx := MX{Name: mxName, Priority: s.mxPrio, TTL: s.ttl}
		override(&mx.Priority, m.Priority)
		override(&mx.TTL, m.TTL)
		z.MX = append(z.MX, mx)
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
	for _, a := range h.Addrs {
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
	addrs = slices.DeleteFunc(addrs, func(a netip.Addr) bool {
		return a.Is4() && h.NoInet || a.Is6() && h.NoInet6
	})
	if len(addrs) == 0 {
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
	name, ok := r.name(h.Pos, h.Name, z.Name)
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
		if alias, ok := r.name(h.Pos, a, z.Name); ok {
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
	name, ok1 := r.name(c.Pos, c.Name, z.Name)
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
	name, ok1 := r.name(sv.Pos, sv.Name, z.Name)
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
	rec := SRV{Name: name, Target: target, Port: sv.Port, Priority: s.srvPrio, Weight: s.srvWeight, TTL: s.ttl}
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
