package config

import (
	"reflect"
	"testing"
)

func lexAll(src string) []token {
	l := newLexer("t", []byte(src))
	var out []token
	for {
		t := l.next()
		out = append(out, t)
		if t.kind == tokEOF {
			return out
		}
	}
}

func TestLexer(t *testing.T) {
	type tk struct {
		kind tokenKind
		text string
		line int
	}
	tests := []struct {
		name string
		src  string
		want []tk
	}{
		{
			name: "statement with list and comment",
			src:  "host a { 1.2.3.4, ::1 } # comment\n",
			want: []tk{
				{tokWord, "host", 1}, {tokWord, "a", 1}, {tokLBrace, "{", 1},
				{tokWord, "1.2.3.4", 1}, {tokComma, ",", 1}, {tokWord, "::1", 1},
				{tokRBrace, "}", 1}, {tokNewline, "", 1}, {tokNewline, "", 2},
				{tokEOF, "", 2},
			},
		},
		{
			name: "continuation and missing final newline",
			src:  "a \\\nb",
			want: []tk{
				{tokWord, "a", 1}, {tokWord, "b", 2}, {tokNewline, "", 2},
				{tokEOF, "", 2},
			},
		},
		{
			name: "macro definition with string",
			src:  `x = "say \"hi\" \\"`,
			want: []tk{
				{tokWord, "x", 1}, {tokEquals, "=", 1},
				{tokString, `say "hi" \`, 1}, {tokNewline, "", 1},
				{tokEOF, "", 1},
			},
		},
		{
			name: "words keep @, * and dots",
			src:  "admin@example.com * @ .37 fd00::/64",
			want: []tk{
				{tokWord, "admin@example.com", 1}, {tokWord, "*", 1},
				{tokWord, "@", 1}, {tokWord, ".37", 1}, {tokWord, "fd00::/64", 1},
				{tokNewline, "", 1}, {tokEOF, "", 1},
			},
		},
		{
			name: "unterminated string",
			src:  "\"abc\nx",
			want: []tk{
				{tokIllegal, "unterminated string", 1}, {tokNewline, "", 1},
				{tokWord, "x", 2}, {tokNewline, "", 2}, {tokEOF, "", 2},
			},
		},
		{
			name: "stray backslash",
			src:  `a\b`,
			want: []tk{
				{tokWord, "a", 1},
				{tokIllegal, `"\" is only allowed at the end of a line`, 1},
				{tokWord, "b", 1}, {tokNewline, "", 1}, {tokEOF, "", 1},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []tk
			for _, tok := range lexAll(tt.src) {
				got = append(got, tk{tok.kind, tok.text, tok.pos.Line})
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %v\nwant %v", got, tt.want)
			}
		})
	}
}
