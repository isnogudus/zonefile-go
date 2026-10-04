package config

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokNewline
	tokWord
	tokString
	tokLBrace
	tokRBrace
	tokEquals
	tokComma
	// tokIllegal carries a lexical error message in text.
	tokIllegal
)

type token struct {
	kind tokenKind
	text string
	pos  Pos
}

// lexer splits one configuration file into tokens.
type lexer struct {
	file string
	src  []byte
	off  int
	line int
	// eol is set once the newline that ends the last statement of the
	// file has been returned.
	eol bool
}

func newLexer(file string, src []byte) *lexer {
	return &lexer{file: file, src: src, line: 1}
}

func (l *lexer) pos() Pos {
	return Pos{File: l.file, Line: l.line}
}

func isDelim(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '#', '{', '}', '"', '=', ',', '\\':
		return true
	}
	return false
}

// next returns the next token. At the end of the input it returns a
// newline, so that the last statement is terminated even without a
// trailing newline, and then tokEOF.
func (l *lexer) next() token {
	for {
		if l.off >= len(l.src) {
			if !l.eol {
				l.eol = true
				return token{kind: tokNewline, pos: l.pos()}
			}
			return token{kind: tokEOF, pos: l.pos()}
		}
		c := l.src[l.off]
		switch c {
		case ' ', '\t', '\r':
			l.off++
		case '#':
			for l.off < len(l.src) && l.src[l.off] != '\n' {
				l.off++
			}
		case '\\':
			if rest := l.src[l.off+1:]; len(rest) > 0 && rest[0] == '\n' {
				l.off += 2
				l.line++
				continue
			} else if len(rest) > 1 && rest[0] == '\r' && rest[1] == '\n' {
				l.off += 3
				l.line++
				continue
			}
			p := l.pos()
			l.off++
			return token{kind: tokIllegal, text: `"\" is only allowed at the end of a line`, pos: p}
		case '\n':
			p := l.pos()
			l.off++
			l.line++
			return token{kind: tokNewline, pos: p}
		case '{':
			return l.single(tokLBrace)
		case '}':
			return l.single(tokRBrace)
		case '=':
			return l.single(tokEquals)
		case ',':
			return l.single(tokComma)
		case '"':
			return l.quoted()
		default:
			return l.word()
		}
	}
}

func (l *lexer) single(kind tokenKind) token {
	t := token{kind: kind, text: string(l.src[l.off]), pos: l.pos()}
	l.off++
	return t
}

func (l *lexer) word() token {
	start := l.off
	for l.off < len(l.src) && !isDelim(l.src[l.off]) {
		l.off++
	}
	return token{kind: tokWord, text: string(l.src[start:l.off]), pos: l.pos()}
}

func (l *lexer) quoted() token {
	p := l.pos()
	l.off++ // opening quote
	var buf []byte
	badEscape := false
	for {
		if l.off >= len(l.src) || l.src[l.off] == '\n' {
			return token{kind: tokIllegal, text: "unterminated string", pos: p}
		}
		c := l.src[l.off]
		l.off++
		switch c {
		case '"':
			if badEscape {
				return token{kind: tokIllegal, text: `invalid escape in string, only \" and \\ are allowed`, pos: p}
			}
			return token{kind: tokString, text: string(buf), pos: p}
		case '\\':
			if l.off < len(l.src) && (l.src[l.off] == '"' || l.src[l.off] == '\\') {
				buf = append(buf, l.src[l.off])
				l.off++
			} else {
				badEscape = true
			}
		default:
			buf = append(buf, c)
		}
	}
}
