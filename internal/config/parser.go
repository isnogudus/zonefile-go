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
	"alias", "authoritative", "cname", "default-lease-time", "dhcp",
	"dhcp-profile", "email", "expire", "get-lease-hostnames", "host", "inet",
	"inet6", "mac", "max-lease-time", "mx", "nameserver", "network", "no",
	"not", "option", "port", "priority", "ptr", "range", "refresh",
	"retry", "reverse", "serial", "server-identifier", "set", "srv", "ttl",
	"weight", "yes", "zone",
}

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
	for _, m := range p.UnusedMacros() {
		p.cfg.Notes = append(p.cfg.Notes, &Error{Pos: m.Pos, Msg: fmt.Sprintf("note: macro %s is not used", m.Name)})
	}
	return p.cfg, nil
}

func (p *parser) topLevel() bool {
	t := p.Tok()
	if p.isSetting() {
		if p.seenBlock {
			p.Errorf(t.Pos, "%s must come before the first zone or reverse block", p.settingName())
			return false
		}
		return p.setting(&p.cfg.Options, true, nil)
	}
	switch t.Text {
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
	case "dhcp":
		p.seenBlock = true
		return p.dhcp()
	}
	return p.unknown("")
}

// unknown reports the current statement as unknown, with a hint for the
// forms that were removed from the language. where is "" or " in zone".
func (p *parser) unknown(where string) bool {
	t := p.Tok()
	switch t.Text {
	case "set":
		if next := p.Peek(1); next.Kind == obsdconf.Word {
			p.Errorf(t.Pos, `"set" is not supported, write %q without it`, next.Text+" ...")
		} else {
			p.Errorf(t.Pos, `"set" is not supported`)
		}
	case "dhcp-profile":
		p.Errorf(t.Pos, `"dhcp-profile" is defined in a dhcp block and used in a zone or on a host`)
	case "mx-priority":
		p.Errorf(t.Pos, `"mx-priority" is not supported, give the priority on each mx`)
	case "srv-priority", "srv-weight":
		p.Errorf(t.Pos, "%q is not supported, give priority and weight on each srv", t.Text)
	default:
		p.Errorf(t.Pos, "unknown statement %q%s", t.Text, where)
	}
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

// settingWords are the statements that set a value of a scope.
var settingWords = map[string]bool{
	"email": true, "ttl": true, "refresh": true, "retry": true,
	"expire": true, "negative-ttl": true, "serial": true, "ptr": true,
}

// isSetting reports whether the current statement is a setting: one of
// settingWords or a "no ..." statement.
func (p *parser) isSetting() bool {
	t := p.Tok().Text
	return settingWords[t] || t == "no"
}

// settingName names the setting at the current token for messages, e.g.
// "ttl" or "no ptr".
func (p *parser) settingName() string {
	if t := p.Tok().Text; t != "no" {
		return strconv.Quote(t)
	}
	return strconv.Quote("no " + p.Peek(1).Text)
}

// setting parses a setting into o. ptr says whether ptr and no ptr are
// allowed; if mx is not nil, no mx is accepted and sets *mx.
func (p *parser) setting(o *Options, ptr bool, mx *bool) bool {
	t := p.Tok()
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
	case "ptr":
		if !ptr {
			p.Errorf(t.Pos, `"ptr" is not allowed in a reverse block`)
			return false
		}
		return once(p, t, &o.PTR, true)
	}

	// "no"
	n := p.Tok()
	switch {
	case ptr && p.Accept("ptr"):
		return once(p, n, &o.PTR, false)
	case mx != nil && p.Accept("mx"):
		if *mx {
			p.Errorf(n.Pos, `"no mx" given twice`)
			return false
		}
		*mx = true
		return true
	case mx != nil && p.Accept("dhcp"):
		return once(p, n, &o.DHCP, false)
	case n.Kind == obsdconf.Word && n.Text == "mx" && ptr:
		p.Errorf(n.Pos, `"no mx" is only allowed in a zone`)
		return false
	case n.Kind == obsdconf.Word && n.Text == "dhcp" && ptr:
		p.Errorf(n.Pos, `"no dhcp" is only allowed in a zone or on a host`)
		return false
	case n.Kind == obsdconf.Word && (n.Text == "ptr" || n.Text == "mx" || n.Text == "dhcp"):
		p.Errorf(n.Pos, "%q is not allowed in a reverse block", "no "+n.Text)
		return false
	case mx != nil:
		return p.Expected(`"ptr", "mx" or "dhcp" after "no"`)
	}
	return p.Expected(`"ptr" after "no"`)
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
	case "dhcp":
		// A switch like ptr; at top level dhcp opens a block instead.
		if st.records {
			p.Errorf(t.Pos, `"dhcp" must come before the first record`)
			return false
		}
		p.Next()
		return once(p, t, &z.Options.DHCP, true)
	case "dhcp-profile":
		if st.records {
			p.Errorf(t.Pos, `"dhcp-profile" must come before the first record`)
			return false
		}
		p.Next()
		name, ok := p.name()
		return ok && once(p, t, &z.Options.DHCPProfile, name)
	}
	if p.isSetting() {
		isMX := p.Is("no", "mx")
		if st.records && !isMX {
			p.Errorf(t.Pos, "%s must come before the first record", p.settingName())
			return false
		}
		if isMX && len(z.MX) > 0 {
			p.Errorf(t.Pos, `"no mx" conflicts with the mx record at %s`, z.MX[0].Pos)
			return false
		}
		return p.setting(&z.Options, true, &z.NoMX)
	}
	switch t.Text {
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
	return p.unknown(" in zone")
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
	if !p.hostOptions(&h) {
		return h, false
	}
	// The block form: the options one or more per line.
	if p.Tok().Kind == obsdconf.LBrace {
		if !p.Block(fmt.Sprintf("host %q", h.Name), func() bool {
			return p.hostOptions(&h)
		}) {
			return h, false
		}
	}
	if h.NoInet && h.NoInet6 {
		p.Errorf(h.Pos, "host %q: no inet and no inet6 leave no addresses", h.Name)
		return h, false
	}
	return h, true
}

// hostOptions parses the options of a host up to the end of the line or an
// opening brace.
func (p *parser) hostOptions(h *Host) bool {
	for p.Tok().Kind == obsdconf.Word {
		kw := p.Tok()
		p.Next()
		var ok bool
		switch kw.Text {
		case "alias":
			if h.Aliases != nil {
				p.Errorf(kw.Pos, "%q given twice", kw.Text)
				return false
			}
			ok = p.List(func() bool {
				n, ok := p.name()
				h.Aliases = append(h.Aliases, n)
				return ok
			})
		case "dhcp":
			ok = once(p, kw, &h.DHCP, true)
		case "dhcp-profile":
			var name string
			if name, ok = p.name(); ok {
				ok = once(p, kw, &h.DHCPProfile, name)
			}
		case "mac":
			if h.MACs != nil {
				p.Errorf(kw.Pos, "%q given twice", kw.Text)
				return false
			}
			ok = p.List(func() bool {
				m, ok := p.mac()
				h.MACs = append(h.MACs, m)
				return ok
			})
		case "ttl":
			ok = p.durationValue(kw, &h.TTL, 1)
		case "ptr":
			ok = once(p, kw, &h.PTR, true)
		case "no":
			ok = p.hostNo(h)
		default:
			p.Errorf(kw.Pos, "host: unknown option %q", kw.Text)
		}
		if !ok {
			return false
		}
	}
	return true
}

func (p *parser) hostNo(h *Host) bool {
	const what = `"ptr", "dhcp", "inet" or "inet6" after "no"`
	t := p.Tok()
	if _, ok := p.Enum(what, "ptr", "dhcp", "inet", "inet6"); !ok {
		return false
	}
	var flag *bool
	switch t.Text {
	case "ptr":
		return once(p, t, &h.PTR, false)
	case "dhcp":
		return once(p, t, &h.DHCP, false)
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
	if p.isSetting() {
		if st.records {
			p.Errorf(t.Pos, "%s must come before the first record", p.settingName())
			return false
		}
		return p.setting(&r.Options, false, nil)
	}
	switch t.Text {
	case "nameserver":
		st.records = true
		ns, ok := p.nameserver(true)
		r.Nameservers = append(r.Nameservers, ns...)
		return ok
	}
	return p.unknown(" in reverse block")
}

// mac parses an Ethernet address of six hexadecimal octets separated by
// colons and returns it in lower case.
func (p *parser) mac() (string, bool) {
	t := p.Tok()
	s, ok := p.Word("MAC address")
	if !ok {
		return "", false
	}
	octets := strings.Split(s, ":")
	valid := len(octets) == 6
	for _, o := range octets {
		if len(o) != 2 || strings.Trim(strings.ToLower(o), "0123456789abcdef") != "" {
			valid = false
		}
	}
	if !valid {
		p.Errorf(t.Pos, "invalid MAC address %q, expected six octets such as 00:00:5e:00:53:01", s)
		return "", false
	}
	return strings.ToLower(s), true
}

// addrList parses an address, suffix or list of them into dst.
func (p *parser) addrList(kw obsdconf.Token, dst *[]HostAddr) bool {
	if *dst != nil {
		p.Errorf(kw.Pos, "%q given twice", kw.Text)
		return false
	}
	return p.List(func() bool {
		a, ok := p.hostAddr()
		*dst = append(*dst, a)
		return ok
	})
}

func (p *parser) dhcp() bool {
	d := &DHCP{Pos: p.Tok().Pos}
	p.Next()
	if p.Tok().Kind == obsdconf.LBrace {
		// Without a network: the global block with the defaults.
		if g := p.cfg.DHCPDefaults; g != nil {
			p.Errorf(d.Pos, "global dhcp block already defined at %s", g.Pos)
			return false
		}
		p.cfg.DHCPDefaults = d
		return p.Block("global dhcp block", func() bool {
			if p.Tok().Text == "range" {
				p.Errorf(p.Tok().Pos, `"range" is not allowed in the global dhcp block`)
				return false
			}
			return p.dhcpStmt(d, "global dhcp block")
		})
	}
	pfx, ok := p.Prefix()
	if !ok {
		return false
	}
	if !pfx.Addr().Is4() {
		p.Errorf(d.Pos, "dhcp %s: dhcpd serves IPv4 networks only", pfx)
		return false
	}
	d.Network = pfx
	p.cfg.DHCP = append(p.cfg.DHCP, d)
	if p.Tok().Kind != obsdconf.LBrace {
		return p.Expected(`"{"`)
	}
	return p.Block(fmt.Sprintf("dhcp %s", pfx), func() bool {
		return p.dhcpStmt(d, "dhcp block")
	})
}

// dhcpStmt parses a statement of a dhcp block; where names the block for
// an unknown statement.
func (p *parser) dhcpStmt(d *DHCP, where string) bool {
	kw := p.Tok()
	switch kw.Text {
	case "range":
		p.Next()
		r := DHCPRange{Pos: kw.Pos}
		var ok bool
		if r.Low, ok = p.hostAddr(); !ok {
			return false
		}
		if r.High, ok = p.hostAddr(); !ok {
			return false
		}
		d.Ranges = append(d.Ranges, r)
		return true
	case "server-identifier":
		p.Next()
		a, ok := p.hostAddr()
		return ok && once(p, kw, &d.ServerID, a)
	case "authoritative":
		p.Next()
		return once(p, kw, &d.Authoritative, true)
	case "not":
		p.Next()
		t := p.Tok()
		if !p.Expect("authoritative") {
			return false
		}
		return once(p, t, &d.Authoritative, false)
	case "dhcp-profile":
		p.Next()
		name, ok := p.name()
		if !ok {
			return false
		}
		for _, o := range d.Profiles {
			if o.Name == name {
				p.Errorf(kw.Pos, "dhcp-profile %q already defined at %s", name, o.Pos)
				return false
			}
		}
		prof := &DHCPProfile{Pos: kw.Pos, Name: name}
		d.Profiles = append(d.Profiles, prof)
		if p.Tok().Kind != obsdconf.LBrace {
			return p.Expected(`"{"`)
		}
		return p.Block(fmt.Sprintf("dhcp-profile %q", name), func() bool {
			return p.dhcpOption(&prof.DHCPOptions, "dhcp-profile")
		})
	}
	return p.dhcpOption(&d.DHCPOptions, where)
}

var dhcpOptionWords = map[string]bool{
	"option": true, "default-lease-time": true, "max-lease-time": true,
	"get-lease-hostnames": true,
}

// dhcpOptionNames are the options of dhcp-options(5) that zonefile-go
// knows, with suffixes and checks for their values.
var dhcpOptionNames = []string{
	"routers", "domain-name-servers", "ntp-servers", "smtp-server",
	"domain-name", "domain-search", "autoproxy-script",
}

// dhcpOption parses an option or a lease time of a dhcp block or profile
// into o, named as in dhcpd.conf(5). where names the block for an unknown
// statement.
func (p *parser) dhcpOption(o *DHCPOptions, where string) bool {
	kw := p.Tok()
	if !dhcpOptionWords[kw.Text] {
		p.Errorf(kw.Pos, "unknown statement %q in %s", kw.Text, where)
		return false
	}
	p.Next()
	switch kw.Text {
	case "default-lease-time":
		return p.durationValue(kw, &o.Lease, 1)
	case "max-lease-time":
		return p.durationValue(kw, &o.MaxLease, 1)
	case "get-lease-hostnames":
		v, ok := p.Enum(`"true" or "false"`, "true", "false")
		return ok && once(p, kw, &o.GetLeaseHostnames, v == "true")
	}

	t := p.Tok()
	name, ok := p.Word("option name")
	if !ok {
		return false
	}
	// once reports the option by its full name, e.g. "option routers".
	opt := obsdconf.Token{Kind: obsdconf.Word, Text: "option " + name, Pos: kw.Pos}
	switch name {
	case "routers":
		return p.addrList(opt, &o.Routers)
	case "domain-name-servers":
		return p.addrList(opt, &o.DNSServers)
	case "ntp-servers":
		return p.addrList(opt, &o.NTPServers)
	case "smtp-server":
		return p.addrList(opt, &o.SMTPServers)
	case "autoproxy-script":
		v, ok := p.Text("URL")
		return ok && once(p, opt, &o.AutoproxyScript, v)
	case "domain-name":
		v, ok := p.name()
		return ok && once(p, opt, &o.Domain, v)
	case "domain-search":
		if o.Search != nil {
			p.Errorf(kw.Pos, "%q given twice", opt.Text)
			return false
		}
		return p.List(func() bool {
			n, ok := p.name()
			o.Search = append(o.Search, n)
			return ok
		})
	}
	p.Errorf(t.Pos, "unknown dhcp option %q, known are %s", name, strings.Join(dhcpOptionNames, ", "))
	return false
}
