package config

import (
	"fmt"
	"io"
	"math"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/isnogudus/obsdconf"
)

// maxDuration is the largest TTL allowed by RFC 2181.
const maxDuration = math.MaxInt32 * time.Second

var keywords = []string{
	"alias", "cname", "email", "expire", "host", "inet", "inet6", "mx",
	"nameserver", "network", "no", "port", "priority", "ptr", "refresh",
	"retry", "reverse", "serial", "set", "srv", "ttl", "weight", "yes", "zone",
}

type scope int

const (
	scopeGlobal scope = iota
	scopeZone
	scopeReverse
)

// blockState tracks statement order inside a zone or reverse block.
type blockState struct {
	records bool
	hosts   bool
}

type parser struct {
	*obsdconf.Parser
	cfg       *Config
	seenBlock bool
}

// ParseFile parses the configuration file at path. A path of "-" reads
// standard input.
func ParseFile(path string) (*Config, error) {
	op, err := obsdconf.NewFile(path, obsdconf.Options{Keywords: keywords})
	if err != nil {
		return nil, err
	}
	return parse(op)
}

// Parse parses a configuration read from r. name is used in error messages
// and as the base for relative include paths. On failure the error is an
// ErrorList with every problem found.
func Parse(name string, r io.Reader) (*Config, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return parse(obsdconf.New(name, src, obsdconf.Options{Keywords: keywords}))
}

func parse(op *obsdconf.Parser) (*Config, error) {
	p := &parser{Parser: op, cfg: &Config{}}
	if err := p.Parse(p.topLevel); err != nil {
		return nil, err
	}
	return p.cfg, nil
}

func (p *parser) topLevel() bool {
	t := p.Tok()
	switch t.Text {
	case "set":
		if p.seenBlock {
			p.Errorf(t.Pos, "set must come before the first zone or reverse block")
			return false
		}
		return p.set(&p.cfg.Options, scopeGlobal)
	case "ptr", "no":
		if p.seenBlock {
			p.Errorf(t.Pos, "%s must come before the first zone or reverse block", statementName(p))
			return false
		}
		return p.ptrStatement(&p.cfg.Options, nil)
	case "nameserver":
		ns, ok := p.nameserver(true)
		p.cfg.Nameservers = append(p.cfg.Nameservers, ns...)
		return ok
	case "mx":
		mx, ok := p.mx(true)
		p.cfg.MX = append(p.cfg.MX, mx)
		return ok
	case "zone":
		p.seenBlock = true
		return p.zone()
	case "reverse":
		p.seenBlock = true
		return p.reverse()
	}
	p.Errorf(t.Pos, "unknown statement %q", t.Text)
	return false
}

func once[T any](p *parser, kw obsdconf.Token, dst **T, v T) bool {
	if *dst != nil {
		p.Errorf(kw.Pos, "%q given twice", kw.Text)
		return false
	}
	*dst = &v
	return true
}

// statementName names the ptr or no statement at the current token for
// messages, e.g. "no ptr".
func statementName(p *parser) string {
	if p.Tok().Text == "ptr" {
		return `"ptr"`
	}
	return fmt.Sprintf(`"no %s"`, p.Peek(1).Text)
}

// ptrStatement parses "ptr" or "no ptr" into o.PTR. If mx is not nil,
// "no mx" is accepted as well and sets *mx.
func (p *parser) ptrStatement(o *Options, mx *bool) bool {
	kw := p.Tok()
	if p.Accept("ptr") {
		return once(p, kw, &o.PTR, true)
	}
	p.Next() // "no"
	t := p.Tok()
	what := `"ptr" after "no"`
	if mx != nil {
		what = `"ptr" or "mx" after "no"`
	}
	switch {
	case p.Accept("ptr"):
		return once(p, t, &o.PTR, false)
	case mx != nil && p.Accept("mx"):
		if *mx {
			p.Errorf(t.Pos, `"no mx" given twice`)
			return false
		}
		*mx = true
		return true
	case t.Kind == obsdconf.Word && t.Text == "mx":
		p.Errorf(t.Pos, `"no mx" is only allowed in a zone`)
		return false
	}
	return p.Expected(what)
}

func (p *parser) set(o *Options, sc scope) bool {
	p.Next()
	t := p.Tok()
	if t.Kind != obsdconf.Word {
		return p.Expected("option name")
	}
	switch t.Text {
	case "ptr":
		p.Errorf(t.Pos, `"set ptr" is not supported, use "ptr" or "no ptr"`)
		return false
	case "mx-priority", "srv-priority", "srv-weight":
		if sc == scopeReverse {
			p.Errorf(t.Pos, "option %q is not allowed in a reverse block", t.Text)
			return false
		}
	}
	p.Next()
	switch t.Text {
	case "email":
		v, ok := p.email()
		return ok && once(p, t, &o.Email, v)
	case "ttl":
		return p.durationValue(t, &o.TTL, 1)
	case "refresh":
		return p.durationValue(t, &o.Refresh, 1)
	case "retry":
		return p.durationValue(t, &o.Retry, 1)
	case "expire":
		return p.durationValue(t, &o.Expire, 1)
	case "negative-ttl":
		return p.durationValue(t, &o.NegativeTTL, 0)
	case "serial":
		v, ok := p.Number(0, math.MaxUint32)
		return ok && once(p, t, &o.Serial, uint32(v))
	case "mx-priority":
		return p.u16Value(t, &o.MXPriority)
	case "srv-priority":
		return p.u16Value(t, &o.SRVPriority)
	case "srv-weight":
		return p.u16Value(t, &o.SRVWeight)
	}
	p.Errorf(t.Pos, "unknown option %q", t.Text)
	return false
}

// seconds parses a duration of whole seconds, at least min.
func (p *parser) seconds(min uint32) (uint32, bool) {
	t := p.Tok()
	d, ok := p.Duration(time.Duration(min)*time.Second, maxDuration)
	if !ok {
		return 0, false
	}
	if d%time.Second != 0 {
		p.Errorf(t.Pos, "duration %q must be whole seconds", t.Text)
		return 0, false
	}
	return uint32(d / time.Second), true
}

// durationValue parses the duration after kw into dst.
func (p *parser) durationValue(kw obsdconf.Token, dst **uint32, min uint32) bool {
	v, ok := p.seconds(min)
	return ok && once(p, kw, dst, v)
}

// u16Value parses the number after kw into dst.
func (p *parser) u16Value(kw obsdconf.Token, dst **uint16) bool {
	v, ok := p.Number(0, math.MaxUint16)
	return ok && once(p, kw, dst, uint16(v))
}

func (p *parser) name() (string, bool) {
	return p.Text("name")
}

func (p *parser) email() (string, bool) {
	t := p.Tok()
	s, ok := p.Text("e-mail address")
	if !ok {
		return "", false
	}
	if err := validEmail(s); err != nil {
		p.Errorf(t.Pos, "invalid e-mail address %q: %v", s, err)
		return "", false
	}
	return s, true
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// validEmail applies the checks of zonefile-rs: a dot-atom local part of at
// most 64 characters and a domain of at least two labels with a TLD that is
// not all digits. A trailing dot on the domain is allowed.
func validEmail(email string) error {
	if len(email) > 254 {
		return fmt.Errorf("longer than 254 characters")
	}
	local, domain, found := strings.Cut(email, "@")
	switch {
	case !found:
		return fmt.Errorf(`missing "@"`)
	case local == "":
		return fmt.Errorf("empty local part")
	case len(local) > 64:
		return fmt.Errorf("local part longer than 64 characters")
	case local[0] == '.' || local[len(local)-1] == '.' || strings.Contains(local, ".."):
		return fmt.Errorf("misplaced dot in local part")
	}
	for i := 0; i < len(local); i++ {
		if c := local[i]; !isAlnum(c) && !strings.ContainsRune(".+-_", rune(c)) {
			return fmt.Errorf("invalid character %q in local part", c)
		}
	}
	labels := strings.Split(strings.TrimSuffix(domain, "."), ".")
	if len(labels) < 2 {
		return fmt.Errorf("domain needs at least two labels")
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return fmt.Errorf("invalid domain label %q", l)
		}
		for i := 0; i < len(l); i++ {
			if !isAlnum(l[i]) && l[i] != '-' {
				return fmt.Errorf("invalid character %q in domain", l[i])
			}
		}
	}
	if strings.Trim(labels[len(labels)-1], "0123456789") == "" {
		return fmt.Errorf("top-level domain is all digits")
	}
	return nil
}

func parseSuffix(s string) ([]byte, bool) {
	parts := strings.Split(strings.TrimPrefix(s, "."), ".")
	if len(parts) > 4 {
		return nil, false
	}
	out := make([]byte, len(parts))
	for i, part := range parts {
		if len(part) > 1 && part[0] == '0' {
			return nil, false
		}
		n, err := strconv.ParseUint(part, 10, 8)
		if err != nil {
			return nil, false
		}
		out[i] = byte(n)
	}
	return out, true
}

func (p *parser) hostAddr() (HostAddr, bool) {
	t := p.Tok()
	if t.Kind == obsdconf.Word && strings.HasPrefix(t.Text, ".") {
		p.Next()
		s, ok := parseSuffix(t.Text)
		if !ok {
			p.Errorf(t.Pos, "invalid address suffix %q", t.Text)
			return HostAddr{}, false
		}
		return HostAddr{Suffix: s}, true
	}
	a, ok := p.Addr()
	return HostAddr{Addr: a}, ok
}

func absolute(name string) bool {
	return strings.HasSuffix(name, ".")
}

// nameserver parses a nameserver statement. If abs is set, the names must
// be absolute because there is no zone to resolve them against.
func (p *parser) nameserver(abs bool) ([]Nameserver, bool) {
	start := p.Tok().Pos
	p.Next()
	var names []string
	if !p.List(func() bool {
		t := p.Tok()
		n, ok := p.name()
		if ok && abs && !absolute(n) {
			p.Errorf(t.Pos, "nameserver %q must be absolute (end with a dot) here", n)
			return false
		}
		names = append(names, n)
		return ok
	}) {
		return nil, false
	}
	var ttl *uint32
	for p.Tok().Kind == obsdconf.Word {
		kw := p.Tok()
		if kw.Text != "ttl" {
			p.Errorf(kw.Pos, "nameserver: unknown option %q", kw.Text)
			return nil, false
		}
		p.Next()
		if !p.durationValue(kw, &ttl, 1) {
			return nil, false
		}
	}
	out := make([]Nameserver, len(names))
	for i, n := range names {
		out[i] = Nameserver{Pos: start, Name: n, TTL: ttl}
	}
	return out, true
}

func (p *parser) mx(abs bool) (MX, bool) {
	m := MX{Pos: p.Tok().Pos}
	p.Next()
	t := p.Tok()
	name, ok := p.name()
	if !ok {
		return m, false
	}
	if abs && !absolute(name) {
		p.Errorf(t.Pos, "mx %q must be absolute (end with a dot) here", name)
		return m, false
	}
	m.Name = name
	for p.Tok().Kind == obsdconf.Word {
		kw := p.Tok()
		p.Next()
		switch kw.Text {
		case "priority":
			ok = p.u16Value(kw, &m.Priority)
		case "ttl":
			ok = p.durationValue(kw, &m.TTL, 1)
		default:
			p.Errorf(kw.Pos, "mx: unknown option %q", kw.Text)
			ok = false
		}
		if !ok {
			return m, false
		}
	}
	return m, true
}

func (p *parser) zone() bool {
	z := &Zone{Pos: p.Tok().Pos}
	p.Next()
	name, ok := p.name()
	if !ok {
		return false
	}
	z.Name = name
	p.cfg.Zones = append(p.cfg.Zones, z)
	var st blockState
	return p.Block(fmt.Sprintf("zone %q", name), func() bool {
		return p.zoneStmt(z, &st)
	})
}

func (p *parser) zoneStmt(z *Zone, st *blockState) bool {
	t := p.Tok()
	switch t.Text {
	case "set":
		if st.records {
			p.Errorf(t.Pos, "set must come before the first record")
			return false
		}
		return p.set(&z.Options, scopeZone)
	case "ptr", "no":
		isMX := t.Text == "no" && p.Is("no", "mx")
		if st.records && !isMX {
			p.Errorf(t.Pos, "%s must come before the first record", statementName(p))
			return false
		}
		if isMX && len(z.MX) > 0 {
			p.Errorf(t.Pos, `"no mx" conflicts with the mx record at %s`, z.MX[0].Pos)
			return false
		}
		return p.ptrStatement(&z.Options, &z.NoMX)
	case "network":
		if st.hosts {
			p.Errorf(t.Pos, "network must come before the first host")
			return false
		}
		return p.network(z)
	case "nameserver":
		st.records = true
		ns, ok := p.nameserver(false)
		z.Nameservers = append(z.Nameservers, ns...)
		return ok
	case "mx":
		st.records = true
		if z.NoMX {
			p.Errorf(t.Pos, `mx conflicts with "no mx" in this zone`)
			return false
		}
		mx, ok := p.mx(false)
		z.MX = append(z.MX, mx)
		return ok
	case "host":
		st.records, st.hosts = true, true
		h, ok := p.host()
		z.Hosts = append(z.Hosts, h)
		return ok
	case "cname":
		st.records = true
		c, ok := p.cname()
		z.CNAMEs = append(z.CNAMEs, c)
		return ok
	case "srv":
		st.records = true
		s, ok := p.srv()
		z.SRVs = append(z.SRVs, s)
		return ok
	}
	p.Errorf(t.Pos, "unknown statement %q in zone", t.Text)
	return false
}

func family(pfx netip.Prefix) string {
	if pfx.Addr().Is4() {
		return "IPv4"
	}
	return "IPv6"
}

func (p *parser) network(z *Zone) bool {
	p.Next()
	return p.List(func() bool {
		t := p.Tok()
		pfx, ok := p.Prefix()
		if !ok {
			return false
		}
		for _, n := range z.Networks {
			if n.Addr().Is4() == pfx.Addr().Is4() {
				p.Errorf(t.Pos, "zone already has an %s network (%s)", family(pfx), n)
				return false
			}
		}
		z.Networks = append(z.Networks, pfx)
		return true
	})
}

func (p *parser) host() (Host, bool) {
	h := Host{Pos: p.Tok().Pos}
	p.Next()
	name, ok := p.name()
	if !ok {
		return h, false
	}
	h.Name = name
	if !p.List(func() bool {
		a, ok := p.hostAddr()
		h.Addrs = append(h.Addrs, a)
		return ok
	}) {
		return h, false
	}
	for p.Tok().Kind == obsdconf.Word {
		kw := p.Tok()
		p.Next()
		switch kw.Text {
		case "alias":
			if h.Aliases != nil {
				p.Errorf(kw.Pos, "%q given twice", kw.Text)
				return h, false
			}
			ok = p.List(func() bool {
				n, ok := p.name()
				h.Aliases = append(h.Aliases, n)
				return ok
			})
		case "ttl":
			ok = p.durationValue(kw, &h.TTL, 1)
		case "ptr":
			ok = once(p, kw, &h.PTR, true)
		case "no":
			ok = p.hostNo(&h)
		default:
			p.Errorf(kw.Pos, "host: unknown option %q", kw.Text)
			ok = false
		}
		if !ok {
			return h, false
		}
	}
	if h.NoInet && h.NoInet6 {
		p.Errorf(h.Pos, "host %q: no inet and no inet6 leave no addresses", h.Name)
		return h, false
	}
	return h, true
}

func (p *parser) hostNo(h *Host) bool {
	const what = `"ptr", "inet" or "inet6" after "no"`
	t := p.Tok()
	if _, ok := p.Enum(what, "ptr", "inet", "inet6"); !ok {
		return false
	}
	var flag *bool
	switch t.Text {
	case "ptr":
		return once(p, t, &h.PTR, false)
	case "inet":
		flag = &h.NoInet
	case "inet6":
		flag = &h.NoInet6
	}
	if *flag {
		p.Errorf(t.Pos, "\"no %s\" given twice", t.Text)
		return false
	}
	*flag = true
	return true
}

func (p *parser) cname() (CNAME, bool) {
	c := CNAME{Pos: p.Tok().Pos}
	p.Next()
	var ok bool
	if c.Name, ok = p.name(); !ok {
		return c, false
	}
	if c.Target, ok = p.name(); !ok {
		return c, false
	}
	for p.Tok().Kind == obsdconf.Word {
		kw := p.Tok()
		p.Next()
		if kw.Text != "ttl" {
			p.Errorf(kw.Pos, "cname: unknown option %q", kw.Text)
			return c, false
		}
		if !p.durationValue(kw, &c.TTL, 1) {
			return c, false
		}
	}
	return c, true
}

func validSRVName(name string) bool {
	labels := strings.Split(name, ".")
	return len(labels) >= 2 &&
		len(labels[0]) > 1 && labels[0][0] == '_' &&
		len(labels[1]) > 1 && labels[1][0] == '_'
}

func (p *parser) srv() (SRV, bool) {
	s := SRV{Pos: p.Tok().Pos}
	p.Next()
	t := p.Tok()
	var ok bool
	if s.Name, ok = p.name(); !ok {
		return s, false
	}
	if !validSRVName(s.Name) {
		p.Errorf(t.Pos, "srv: %q must start with _service._proto", s.Name)
		return s, false
	}
	if s.Target, ok = p.name(); !ok {
		return s, false
	}
	if !p.Expect("port") {
		return s, false
	}
	port, ok := p.Number(0, math.MaxUint16)
	if !ok {
		return s, false
	}
	s.Port = uint16(port)
	for p.Tok().Kind == obsdconf.Word {
		kw := p.Tok()
		p.Next()
		switch kw.Text {
		case "priority":
			ok = p.u16Value(kw, &s.Priority)
		case "weight":
			ok = p.u16Value(kw, &s.Weight)
		case "ttl":
			ok = p.durationValue(kw, &s.TTL, 1)
		default:
			p.Errorf(kw.Pos, "srv: unknown option %q", kw.Text)
			ok = false
		}
		if !ok {
			return s, false
		}
	}
	return s, true
}

func (p *parser) reverse() bool {
	r := &Reverse{Pos: p.Tok().Pos}
	p.Next()
	if !p.List(func() bool {
		pfx, ok := p.Prefix()
		r.Networks = append(r.Networks, pfx)
		return ok
	}) {
		return false
	}
	p.cfg.Reverse = append(p.cfg.Reverse, r)
	if p.Tok().Kind != obsdconf.LBrace {
		return true
	}
	var st blockState
	return p.Block("reverse", func() bool {
		return p.reverseStmt(r, &st)
	})
}

func (p *parser) reverseStmt(r *Reverse, st *blockState) bool {
	t := p.Tok()
	switch t.Text {
	case "set":
		if st.records {
			p.Errorf(t.Pos, "set must come before the first record")
			return false
		}
		return p.set(&r.Options, scopeReverse)
	case "nameserver":
		st.records = true
		ns, ok := p.nameserver(true)
		r.Nameservers = append(r.Nameservers, ns...)
		return ok
	}
	p.Errorf(t.Pos, "unknown statement %q in reverse block", t.Text)
	return false
}
