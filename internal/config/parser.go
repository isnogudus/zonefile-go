package config

import (
	"fmt"
	"io"
	"math"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	maxIncludeDepth = 16
	// maxDuration is the largest TTL allowed by RFC 2181.
	maxDuration = math.MaxInt32
)

var macroName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// keywords cannot be used as macro names.
var keywords = map[string]bool{
	"alias": true, "cname": true, "email": true, "expire": true,
	"host": true, "include": true, "inet": true, "inet6": true,
	"mx": true, "nameserver": true, "network": true, "no": true,
	"port": true, "priority": true, "ptr": true, "refresh": true,
	"retry": true, "reverse": true, "serial": true, "set": true,
	"srv": true, "ttl": true, "weight": true, "yes": true, "zone": true,
}

var durationUnits = map[byte]uint64{
	's': 1, 'm': 60, 'h': 3600, 'd': 86400, 'w': 604800,
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
	lexers []*lexer
	// active holds the absolute paths of the files being read, to detect
	// include loops.
	active map[string]bool
	// pending holds the remaining tokens of a macro expansion.
	pending   []token
	tok       token
	peeked    *token
	macros    map[string][]token
	errs      ErrorList
	cfg       *Config
	seenBlock bool
}

// ParseFile parses the configuration file at path. A path of "-" reads
// standard input.
func ParseFile(path string) (*Config, error) {
	if path == "-" {
		return Parse("-", os.Stdin)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(path, src)
}

// Parse parses a configuration read from r. name is used in error messages
// and as the base for relative include paths. On failure the error is an
// ErrorList with every problem found.
func Parse(name string, r io.Reader) (*Config, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return parse(name, src)
}

func parse(name string, src []byte) (*Config, error) {
	p := &parser{
		active: map[string]bool{},
		macros: map[string][]token{},
		cfg:    &Config{},
	}
	p.push(name, src)
	p.advance()
	p.parseConfig()
	if len(p.errs) > 0 {
		return nil, p.errs
	}
	return p.cfg, nil
}

func absPath(name string) string {
	if a, err := filepath.Abs(name); err == nil {
		return a
	}
	return name
}

func (p *parser) push(name string, src []byte) {
	p.lexers = append(p.lexers, newLexer(name, src))
	p.active[absPath(name)] = true
}

// raw returns the next token of the innermost file and continues with the
// including file at the end of an included one.
func (p *parser) raw() token {
	for {
		l := p.lexers[len(p.lexers)-1]
		t := l.next()
		if t.kind != tokEOF || len(p.lexers) == 1 {
			return t
		}
		delete(p.active, absPath(l.file))
		p.lexers = p.lexers[:len(p.lexers)-1]
	}
}

// fetch returns the next token with macros expanded and reports lexical
// errors.
func (p *parser) fetch() token {
	for {
		var t token
		if len(p.pending) > 0 {
			t, p.pending = p.pending[0], p.pending[1:]
		} else {
			t = p.raw()
		}
		if t.kind == tokWord && strings.HasPrefix(t.text, "$") {
			val, ok := p.macros[t.text[1:]]
			if !ok {
				t = token{kind: tokIllegal, text: fmt.Sprintf("undefined macro %q", t.text), pos: t.pos}
			} else {
				exp := make([]token, len(val), len(val)+len(p.pending))
				for i, v := range val {
					v.pos = t.pos
					exp[i] = v
				}
				p.pending = append(exp, p.pending...)
				continue
			}
		}
		if t.kind == tokIllegal {
			p.errorf(t.pos, "%s", t.text)
		}
		return t
	}
}

func (p *parser) advance() {
	if p.peeked != nil {
		p.tok, p.peeked = *p.peeked, nil
		return
	}
	p.tok = p.fetch()
}

func (p *parser) peek() token {
	if p.peeked == nil {
		t := p.fetch()
		p.peeked = &t
	}
	return *p.peeked
}

func (p *parser) errorf(pos Pos, format string, args ...any) {
	p.errs = append(p.errs, &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

func describe(t token) string {
	switch t.kind {
	case tokEOF:
		return "end of file"
	case tokNewline:
		return "end of line"
	}
	return strconv.Quote(t.text)
}

// expected reports that the current token is not what the grammar allows
// here. Lexical errors have already been reported by fetch. It always
// returns false.
func (p *parser) expected(what string) bool {
	if p.tok.kind != tokIllegal {
		p.errorf(p.tok.pos, "expected %s, got %s", what, describe(p.tok))
	}
	return false
}

// skipStatement skips the rest of a statement after an error, including
// any block it opened. Inside a block it stops before the closing brace of
// that block.
func (p *parser) skipStatement(inBlock bool) {
	depth := 0
	for {
		switch p.tok.kind {
		case tokEOF:
			return
		case tokNewline:
			if depth == 0 {
				p.advance()
				return
			}
		case tokLBrace:
			depth++
		case tokRBrace:
			if depth == 0 && inBlock {
				return
			}
			if depth > 0 {
				depth--
			}
		}
		p.advance()
	}
}

func (p *parser) endOfStatement() bool {
	if p.tok.kind == tokNewline {
		p.advance()
		return true
	}
	return p.expected("end of line")
}

func (p *parser) parseConfig() {
	for p.tok.kind != tokEOF {
		if p.tok.kind == tokNewline {
			p.advance()
			continue
		}
		if !p.topLevel() || !p.endOfStatement() {
			p.skipStatement(false)
		}
	}
}

func (p *parser) topLevel() bool {
	t := p.tok
	if t.kind != tokWord {
		return p.expected("statement")
	}
	if p.peek().kind == tokEquals {
		return p.macroDef()
	}
	switch t.text {
	case "include":
		return p.include()
	case "set":
		if p.seenBlock {
			p.errorf(t.pos, "set must come before the first zone or reverse block")
			return false
		}
		return p.set(&p.cfg.Options, scopeGlobal)
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
	p.errorf(t.pos, "unknown statement %q", t.text)
	return false
}

func (p *parser) macroDef() bool {
	t := p.tok
	if !macroName.MatchString(t.text) {
		p.errorf(t.pos, "invalid macro name %q", t.text)
		return false
	}
	if keywords[t.text] {
		p.errorf(t.pos, "%q is a keyword and cannot be used as a macro name", t.text)
		return false
	}
	p.advance() // name
	p.advance() // "="
	var val []token
	switch p.tok.kind {
	case tokWord:
		val = []token{p.tok}
		p.advance()
	case tokString:
		toks, ok := p.relex(p.tok)
		if !ok {
			return false
		}
		val = toks
		p.advance()
	case tokLBrace:
		toks, ok := p.collectList()
		if !ok {
			return false
		}
		val = toks
	default:
		return p.expected("macro value")
	}
	if len(val) == 0 {
		p.errorf(t.pos, "macro %q has an empty value", t.text)
		return false
	}
	p.macros[t.text] = val
	return true
}

// relex splits the contents of a quoted macro value into tokens, as
// pf.conf does, so that "{ a b }" defines a list.
func (p *parser) relex(t token) ([]token, bool) {
	l := newLexer(t.pos.File, []byte(t.text))
	var out []token
	for {
		u := l.next()
		switch u.kind {
		case tokEOF:
			return out, true
		case tokNewline:
			continue
		case tokIllegal:
			p.errorf(t.pos, "in macro value: %s", u.text)
			return nil, false
		}
		if u.kind == tokWord && strings.HasPrefix(u.text, "$") {
			val, ok := p.macros[u.text[1:]]
			if !ok {
				p.errorf(t.pos, "undefined macro %q", u.text)
				return nil, false
			}
			out = append(out, val...)
			continue
		}
		u.pos = t.pos
		out = append(out, u)
	}
}

// collectList returns the tokens of a brace-enclosed list, braces included.
func (p *parser) collectList() ([]token, bool) {
	open := p.tok
	var out []token
	depth := 0
	for {
		switch p.tok.kind {
		case tokEOF:
			p.errorf(open.pos, "unterminated list")
			return nil, false
		case tokIllegal:
			return nil, false
		case tokNewline:
			p.advance()
			continue
		case tokLBrace:
			depth++
		case tokRBrace:
			depth--
		}
		out = append(out, p.tok)
		p.advance()
		if depth == 0 {
			return out, true
		}
	}
}

func (p *parser) include() bool {
	kw := p.tok
	p.advance()
	if p.tok.kind != tokString {
		return p.expected("quoted file name")
	}
	path := p.tok.text
	p.advance()
	if p.tok.kind != tokNewline {
		return p.expected("end of line")
	}
	if !filepath.IsAbs(path) && kw.pos.File != "-" {
		path = filepath.Join(filepath.Dir(kw.pos.File), path)
	}
	if p.active[absPath(path)] {
		p.errorf(kw.pos, "include loop: %s", path)
		return false
	}
	if len(p.lexers) >= maxIncludeDepth {
		p.errorf(kw.pos, "include: nested more than %d levels deep", maxIncludeDepth)
		return false
	}
	src, err := os.ReadFile(path)
	if err != nil {
		p.errorf(kw.pos, "include: %v", err)
		return false
	}
	// The newline ending this statement is the current token, so the next
	// advance reads from the included file.
	p.push(path, src)
	return true
}

func once[T any](p *parser, kw token, dst **T, v T) bool {
	if *dst != nil {
		p.errorf(kw.pos, "%q given twice", kw.text)
		return false
	}
	*dst = &v
	return true
}

func (p *parser) set(o *Options, sc scope) bool {
	p.advance()
	t := p.tok
	if t.kind != tokWord {
		return p.expected("option name")
	}
	switch t.text {
	case "mx-priority", "srv-priority", "srv-weight", "ptr":
		if sc == scopeReverse {
			p.errorf(t.pos, "option %q is not allowed in a reverse block", t.text)
			return false
		}
	}
	p.advance()
	switch t.text {
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
		v, ok := p.number(math.MaxUint32)
		return ok && once(p, t, &o.Serial, uint32(v))
	case "mx-priority":
		return p.u16Value(t, &o.MXPriority)
	case "srv-priority":
		return p.u16Value(t, &o.SRVPriority)
	case "srv-weight":
		return p.u16Value(t, &o.SRVWeight)
	case "ptr":
		v, ok := p.yesNo()
		return ok && once(p, t, &o.PTR, v)
	}
	p.errorf(t.pos, "unknown option %q", t.text)
	return false
}

// durationValue parses the duration after kw into dst.
func (p *parser) durationValue(kw token, dst **uint32, min uint32) bool {
	v, ok := p.duration(min)
	return ok && once(p, kw, dst, v)
}

// u16Value parses the number after kw into dst.
func (p *parser) u16Value(kw token, dst **uint16) bool {
	v, ok := p.number(math.MaxUint16)
	return ok && once(p, kw, dst, uint16(v))
}

func (p *parser) word(what string) (token, bool) {
	if p.tok.kind != tokWord {
		return p.tok, p.expected(what)
	}
	t := p.tok
	p.advance()
	return t, true
}

func (p *parser) name() (string, bool) {
	if p.tok.kind != tokWord && p.tok.kind != tokString {
		return "", p.expected("name")
	}
	t := p.tok
	p.advance()
	if t.text == "" {
		p.errorf(t.pos, "empty name")
		return "", false
	}
	return t.text, true
}

func (p *parser) number(max uint64) (uint64, bool) {
	t, ok := p.word("number")
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseUint(t.text, 10, 64)
	if err != nil {
		p.errorf(t.pos, "invalid number %q", t.text)
		return 0, false
	}
	if n > max {
		p.errorf(t.pos, "number %d out of range (0-%d)", n, max)
		return 0, false
	}
	return n, true
}

func (p *parser) duration(min uint32) (uint32, bool) {
	t, ok := p.word("duration")
	if !ok {
		return 0, false
	}
	s, mult := t.text, uint64(1)
	if n := len(s); n > 1 {
		if u, ok := durationUnits[s[n-1]]; ok {
			s, mult = s[:n-1], u
		}
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		p.errorf(t.pos, "invalid duration %q", t.text)
		return 0, false
	}
	if v := n * mult; v >= uint64(min) && v <= maxDuration {
		return uint32(v), true
	}
	p.errorf(t.pos, "duration %q out of range (%d-%d seconds)", t.text, min, maxDuration)
	return 0, false
}

func (p *parser) yesNo() (bool, bool) {
	t, ok := p.word(`"yes" or "no"`)
	if !ok {
		return false, false
	}
	switch t.text {
	case "yes":
		return true, true
	case "no":
		return false, true
	}
	p.errorf(t.pos, `expected "yes" or "no", got %q`, t.text)
	return false, false
}

func (p *parser) email() (string, bool) {
	t := p.tok
	if t.kind != tokWord && t.kind != tokString {
		return "", p.expected("e-mail address")
	}
	p.advance()
	local, domain, found := strings.Cut(t.text, "@")
	if !found || local == "" || domain == "" || strings.Contains(domain, "@") {
		p.errorf(t.pos, "invalid e-mail address %q", t.text)
		return "", false
	}
	return t.text, true
}

func (p *parser) prefix() (netip.Prefix, bool) {
	t, ok := p.word("network")
	if !ok {
		return netip.Prefix{}, false
	}
	pfx, err := netip.ParsePrefix(t.text)
	if err != nil {
		p.errorf(t.pos, "invalid network %q", t.text)
		return netip.Prefix{}, false
	}
	if pfx != pfx.Masked() {
		p.errorf(t.pos, "network %s has host bits set, did you mean %s?", t.text, pfx.Masked())
		return netip.Prefix{}, false
	}
	return pfx, true
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
	t, ok := p.word("address")
	if !ok {
		return HostAddr{}, false
	}
	if strings.HasPrefix(t.text, ".") {
		s, ok := parseSuffix(t.text)
		if !ok {
			p.errorf(t.pos, "invalid address suffix %q", t.text)
			return HostAddr{}, false
		}
		return HostAddr{Suffix: s}, true
	}
	a, err := netip.ParseAddr(t.text)
	if err != nil || a.Zone() != "" {
		p.errorf(t.pos, "invalid address %q", t.text)
		return HostAddr{}, false
	}
	return HostAddr{Addr: a}, true
}

// list parses a single item or a brace-enclosed list of items. Newlines
// and commas between items are ignored.
func (p *parser) list(item func() bool) bool {
	if p.tok.kind != tokLBrace {
		return item()
	}
	open := p.tok
	p.advance()
	n := 0
	for {
		switch p.tok.kind {
		case tokNewline, tokComma:
			p.advance()
			continue
		case tokRBrace:
			p.advance()
			if n == 0 {
				p.errorf(open.pos, "empty list")
				return false
			}
			return true
		case tokEOF:
			p.errorf(open.pos, "unterminated list")
			return false
		}
		if !item() {
			p.skipList()
			return false
		}
		n++
	}
}

// skipList skips past the closing brace of a list after a failed item.
func (p *parser) skipList() {
	for p.tok.kind != tokEOF {
		k := p.tok.kind
		p.advance()
		if k == tokRBrace {
			return
		}
	}
}

// block parses the statements of a brace-enclosed block. The opening brace
// is the current token.
func (p *parser) block(what string, stmt func() bool) bool {
	open := p.tok
	if open.kind != tokLBrace {
		return p.expected(`"{"`)
	}
	p.advance()
	if p.tok.kind != tokNewline {
		// Report, but keep parsing the block.
		p.expected(`end of line after "{"`)
	}
	for {
		switch p.tok.kind {
		case tokNewline:
			p.advance()
			continue
		case tokRBrace:
			p.advance()
			return true
		case tokEOF:
			p.errorf(open.pos, `%s: missing "}"`, what)
			return false
		}
		if !stmt() || !p.endOfStatement() {
			p.skipStatement(true)
		}
	}
}

func absolute(name string) bool {
	return strings.HasSuffix(name, ".")
}

// nameserver parses a nameserver statement. If abs is set, the names must
// be absolute because there is no zone to resolve them against.
func (p *parser) nameserver(abs bool) ([]Nameserver, bool) {
	start := p.tok.pos
	p.advance()
	var names []string
	if !p.list(func() bool {
		t := p.tok
		n, ok := p.name()
		if ok && abs && !absolute(n) {
			p.errorf(t.pos, "nameserver %q must be absolute (end with a dot) here", n)
			return false
		}
		names = append(names, n)
		return ok
	}) {
		return nil, false
	}
	var ttl *uint32
	for p.tok.kind == tokWord {
		kw := p.tok
		if kw.text != "ttl" {
			p.errorf(kw.pos, "nameserver: unknown option %q", kw.text)
			return nil, false
		}
		p.advance()
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
	m := MX{Pos: p.tok.pos}
	p.advance()
	t := p.tok
	name, ok := p.name()
	if !ok {
		return m, false
	}
	if abs && !absolute(name) {
		p.errorf(t.pos, "mx %q must be absolute (end with a dot) here", name)
		return m, false
	}
	m.Name = name
	for p.tok.kind == tokWord {
		kw := p.tok
		p.advance()
		switch kw.text {
		case "priority":
			ok = p.u16Value(kw, &m.Priority)
		case "ttl":
			ok = p.durationValue(kw, &m.TTL, 1)
		default:
			p.errorf(kw.pos, "mx: unknown option %q", kw.text)
			ok = false
		}
		if !ok {
			return m, false
		}
	}
	return m, true
}

func (p *parser) zone() bool {
	z := &Zone{Pos: p.tok.pos}
	p.advance()
	name, ok := p.name()
	if !ok {
		return false
	}
	z.Name = name
	p.cfg.Zones = append(p.cfg.Zones, z)
	var st blockState
	return p.block(fmt.Sprintf("zone %q", name), func() bool {
		return p.zoneStmt(z, &st)
	})
}

func (p *parser) zoneStmt(z *Zone, st *blockState) bool {
	t := p.tok
	if t.kind != tokWord {
		return p.expected("statement")
	}
	switch t.text {
	case "set":
		if st.records {
			p.errorf(t.pos, "set must come before the first record")
			return false
		}
		return p.set(&z.Options, scopeZone)
	case "network":
		if st.hosts {
			p.errorf(t.pos, "network must come before the first host")
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
	p.errorf(t.pos, "unknown statement %q in zone", t.text)
	return false
}

func family(pfx netip.Prefix) string {
	if pfx.Addr().Is4() {
		return "IPv4"
	}
	return "IPv6"
}

func (p *parser) network(z *Zone) bool {
	p.advance()
	return p.list(func() bool {
		t := p.tok
		pfx, ok := p.prefix()
		if !ok {
			return false
		}
		for _, n := range z.Networks {
			if n.Addr().Is4() == pfx.Addr().Is4() {
				p.errorf(t.pos, "zone already has an %s network (%s)", family(pfx), n)
				return false
			}
		}
		z.Networks = append(z.Networks, pfx)
		return true
	})
}

func (p *parser) host() (Host, bool) {
	h := Host{Pos: p.tok.pos}
	p.advance()
	name, ok := p.name()
	if !ok {
		return h, false
	}
	h.Name = name
	if !p.list(func() bool {
		a, ok := p.hostAddr()
		h.Addrs = append(h.Addrs, a)
		return ok
	}) {
		return h, false
	}
	for p.tok.kind == tokWord {
		kw := p.tok
		p.advance()
		switch kw.text {
		case "alias":
			if h.Aliases != nil {
				p.errorf(kw.pos, "%q given twice", kw.text)
				return h, false
			}
			ok = p.list(func() bool {
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
			p.errorf(kw.pos, "host: unknown option %q", kw.text)
			ok = false
		}
		if !ok {
			return h, false
		}
	}
	if h.NoInet && h.NoInet6 {
		p.errorf(h.Pos, "host %q: no inet and no inet6 leave no addresses", h.Name)
		return h, false
	}
	return h, true
}

func (p *parser) hostNo(h *Host) bool {
	const what = `"ptr", "inet" or "inet6" after "no"`
	t, ok := p.word(what)
	if !ok {
		return false
	}
	var flag *bool
	switch t.text {
	case "ptr":
		return once(p, t, &h.PTR, false)
	case "inet":
		flag = &h.NoInet
	case "inet6":
		flag = &h.NoInet6
	default:
		p.errorf(t.pos, "expected %s, got %q", what, t.text)
		return false
	}
	if *flag {
		p.errorf(t.pos, "\"no %s\" given twice", t.text)
		return false
	}
	*flag = true
	return true
}

func (p *parser) cname() (CNAME, bool) {
	c := CNAME{Pos: p.tok.pos}
	p.advance()
	var ok bool
	if c.Name, ok = p.name(); !ok {
		return c, false
	}
	if c.Target, ok = p.name(); !ok {
		return c, false
	}
	for p.tok.kind == tokWord {
		kw := p.tok
		p.advance()
		if kw.text != "ttl" {
			p.errorf(kw.pos, "cname: unknown option %q", kw.text)
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
	s := SRV{Pos: p.tok.pos}
	p.advance()
	t := p.tok
	var ok bool
	if s.Name, ok = p.name(); !ok {
		return s, false
	}
	if !validSRVName(s.Name) {
		p.errorf(t.pos, "srv: %q must start with _service._proto", s.Name)
		return s, false
	}
	if s.Target, ok = p.name(); !ok {
		return s, false
	}
	if p.tok.kind != tokWord || p.tok.text != "port" {
		return s, p.expected(`"port"`)
	}
	p.advance()
	port, ok := p.number(math.MaxUint16)
	if !ok {
		return s, false
	}
	s.Port = uint16(port)
	for p.tok.kind == tokWord {
		kw := p.tok
		p.advance()
		switch kw.text {
		case "priority":
			ok = p.u16Value(kw, &s.Priority)
		case "weight":
			ok = p.u16Value(kw, &s.Weight)
		case "ttl":
			ok = p.durationValue(kw, &s.TTL, 1)
		default:
			p.errorf(kw.pos, "srv: unknown option %q", kw.text)
			ok = false
		}
		if !ok {
			return s, false
		}
	}
	return s, true
}

func (p *parser) reverse() bool {
	r := &Reverse{Pos: p.tok.pos}
	p.advance()
	if !p.list(func() bool {
		pfx, ok := p.prefix()
		r.Networks = append(r.Networks, pfx)
		return ok
	}) {
		return false
	}
	p.cfg.Reverse = append(p.cfg.Reverse, r)
	if p.tok.kind != tokLBrace {
		return true
	}
	var st blockState
	return p.block("reverse", func() bool {
		return p.reverseStmt(r, &st)
	})
}

func (p *parser) reverseStmt(r *Reverse, st *blockState) bool {
	t := p.tok
	if t.kind != tokWord {
		return p.expected("statement")
	}
	switch t.text {
	case "set":
		if st.records {
			p.errorf(t.pos, "set must come before the first record")
			return false
		}
		return p.set(&r.Options, scopeReverse)
	case "nameserver":
		st.records = true
		ns, ok := p.nameserver(true)
		r.Nameservers = append(r.Nameservers, ns...)
		return ok
	}
	p.errorf(t.pos, "unknown statement %q in reverse block", t.text)
	return false
}
