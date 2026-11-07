package css

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// A tolerant CSS Syntax Level 3 tokenizer. Comments are dropped, escapes
// are decoded into token values, bad strings and bad urls become their
// own token kinds so the parser can recover the way browsers do. Every
// token keeps its byte span in the source so values can be sliced back
// out verbatim.

type tokKind uint8

const (
	tEOF tokKind = iota
	tIdent
	tFunction
	tAtKeyword
	tHash
	tString
	tBadString
	tURL
	tBadURL
	tDelim
	tNumber
	tPercentage
	tDimension
	tWS
	tCDO
	tCDC
	tColon
	tSemicolon
	tComma
	tLBracket
	tRBracket
	tLParen
	tRParen
	tLBrace
	tRBrace
)

type token struct {
	k     tokKind
	s     string  // ident/function/at-keyword/hash name, string/url value, delim char, dimension unit
	num   float64 // number/percentage/dimension
	isInt bool
	id    bool // hash: type flag "id"
	start int
	end   int
}

// is reports a delim token with byte c.
func (t *token) is(c byte) bool { return t.k == tDelim && len(t.s) == 1 && t.s[0] == c }

// ident reports an ident token equal (ASCII case-insensitively) to s.
func (t *token) ident(s string) bool { return t.k == tIdent && strings.EqualFold(t.s, s) }

type tokenizer struct {
	src string
	pos int
}

func tokenize(src string) []token {
	tz := tokenizer{src: src}
	out := make([]token, 0, len(src)/3+1)
	for {
		t := tz.next()
		if t.k == tEOF {
			return out
		}
		out = append(out, t)
	}
}

func isNameStart(c rune) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c >= 0x80
}

func isName(c rune) bool { return isNameStart(c) || c >= '0' && c <= '9' || c == '-' }

func isWS(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func isNL(c byte) bool { return c == '\n' || c == '\r' || c == '\f' }

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func (tz *tokenizer) at(i int) byte {
	if i < len(tz.src) {
		return tz.src[i]
	}
	return 0
}

func (tz *tokenizer) runeAt(i int) rune {
	if i >= len(tz.src) {
		return -1
	}
	r, _ := utf8.DecodeRuneInString(tz.src[i:])
	return r
}

// validEscape: the two bytes at i start a valid escape.
func (tz *tokenizer) validEscape(i int) bool {
	return i < len(tz.src) && tz.src[i] == '\\' && (i+1 >= len(tz.src) || !isNL(tz.src[i+1])) && i+1 < len(tz.src)
}

// startsIdent: the bytes at i start an ident sequence.
func (tz *tokenizer) startsIdent(i int) bool {
	c := tz.at(i)
	switch {
	case c == '-':
		n := tz.runeAt(i + 1)
		return n == '-' || n >= 0 && isNameStart(n) || tz.validEscape(i+1)
	case c == '\\':
		return tz.validEscape(i)
	case c == 0:
		return false
	}
	return isNameStart(tz.runeAt(i))
}

// startsNumber: the bytes at i start a number.
func (tz *tokenizer) startsNumber(i int) bool {
	c := tz.at(i)
	if c == '+' || c == '-' {
		c1 := tz.at(i + 1)
		return c1 >= '0' && c1 <= '9' || c1 == '.' && tz.at(i+2) >= '0' && tz.at(i+2) <= '9'
	}
	if c == '.' {
		c1 := tz.at(i + 1)
		return c1 >= '0' && c1 <= '9'
	}
	return c >= '0' && c <= '9'
}

// escape consumes an escape (pos is just past the backslash).
func (tz *tokenizer) escape() rune {
	if tz.pos >= len(tz.src) {
		return 0xFFFD
	}
	c := tz.src[tz.pos]
	if isHex(c) {
		n := 0
		var v rune
		for n < 6 && tz.pos < len(tz.src) && isHex(tz.src[tz.pos]) {
			d, _ := strconv.ParseUint(tz.src[tz.pos:tz.pos+1], 16, 8)
			v = v*16 + rune(d)
			tz.pos++
			n++
		}
		if tz.pos < len(tz.src) && isWS(tz.src[tz.pos]) {
			if tz.src[tz.pos] == '\r' && tz.at(tz.pos+1) == '\n' {
				tz.pos++
			}
			tz.pos++
		}
		if v == 0 || v > 0x10FFFF || v >= 0xD800 && v <= 0xDFFF {
			return 0xFFFD
		}
		return v
	}
	r, sz := utf8.DecodeRuneInString(tz.src[tz.pos:])
	tz.pos += sz
	return r
}

// name consumes an ident sequence.
func (tz *tokenizer) name() string {
	start := tz.pos
	var b *strings.Builder
	for tz.pos < len(tz.src) {
		c := tz.src[tz.pos]
		if c == '\\' && tz.validEscape(tz.pos) {
			if b == nil {
				b = &strings.Builder{}
				b.WriteString(tz.src[start:tz.pos])
			}
			tz.pos++
			b.WriteRune(tz.escape())
			continue
		}
		r, sz := utf8.DecodeRuneInString(tz.src[tz.pos:])
		if !isName(r) {
			break
		}
		if b != nil {
			b.WriteString(tz.src[tz.pos : tz.pos+sz])
		}
		tz.pos += sz
	}
	if b != nil {
		return b.String()
	}
	return tz.src[start:tz.pos]
}

func (tz *tokenizer) number() (float64, bool) {
	start := tz.pos
	isInt := true
	if c := tz.at(tz.pos); c == '+' || c == '-' {
		tz.pos++
	}
	for c := tz.at(tz.pos); c >= '0' && c <= '9'; c = tz.at(tz.pos) {
		tz.pos++
	}
	if tz.at(tz.pos) == '.' && tz.at(tz.pos+1) >= '0' && tz.at(tz.pos+1) <= '9' {
		isInt = false
		tz.pos++
		for c := tz.at(tz.pos); c >= '0' && c <= '9'; c = tz.at(tz.pos) {
			tz.pos++
		}
	}
	if c := tz.at(tz.pos); c == 'e' || c == 'E' {
		c1 := tz.at(tz.pos + 1)
		off := 1
		if c1 == '+' || c1 == '-' {
			c1 = tz.at(tz.pos + 2)
			off = 2
		}
		if c1 >= '0' && c1 <= '9' {
			isInt = false
			tz.pos += off
			for c := tz.at(tz.pos); c >= '0' && c <= '9'; c = tz.at(tz.pos) {
				tz.pos++
			}
		}
	}
	s := tz.src[start:tz.pos]
	if s[0] == '+' {
		s = s[1:]
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v, isInt
}

func (tz *tokenizer) next() token {
	// comments
	for tz.pos+1 < len(tz.src) && tz.src[tz.pos] == '/' && tz.src[tz.pos+1] == '*' {
		end := strings.Index(tz.src[tz.pos+2:], "*/")
		if end < 0 {
			tz.pos = len(tz.src)
		} else {
			tz.pos += end + 4
		}
	}
	start := tz.pos
	if tz.pos >= len(tz.src) {
		return token{k: tEOF, start: start, end: start}
	}
	t := tz.consume()
	t.start = start
	t.end = tz.pos
	return t
}

func (tz *tokenizer) consume() token {
	c := tz.src[tz.pos]
	switch {
	case isWS(c):
		for tz.pos < len(tz.src) && isWS(tz.src[tz.pos]) {
			tz.pos++
		}
		return token{k: tWS}
	case c == '"' || c == '\'':
		return tz.str(c)
	case c == '#':
		if r := tz.runeAt(tz.pos + 1); r >= 0 && isName(r) || tz.validEscape(tz.pos+1) {
			tz.pos++
			id := tz.startsIdent(tz.pos)
			return token{k: tHash, s: tz.name(), id: id}
		}
	case c == '(':
		tz.pos++
		return token{k: tLParen}
	case c == ')':
		tz.pos++
		return token{k: tRParen}
	case c == '+' || c == '.':
		if tz.startsNumber(tz.pos) {
			return tz.numeric()
		}
	case c == ',':
		tz.pos++
		return token{k: tComma}
	case c == '-':
		if tz.startsNumber(tz.pos) {
			return tz.numeric()
		}
		if tz.at(tz.pos+1) == '-' && tz.at(tz.pos+2) == '>' {
			tz.pos += 3
			return token{k: tCDC}
		}
		if tz.startsIdent(tz.pos) {
			return tz.identLike()
		}
	case c == ':':
		tz.pos++
		return token{k: tColon}
	case c == ';':
		tz.pos++
		return token{k: tSemicolon}
	case c == '<':
		if strings.HasPrefix(tz.src[tz.pos:], "<!--") {
			tz.pos += 4
			return token{k: tCDO}
		}
	case c == '@':
		if tz.startsIdent(tz.pos + 1) {
			tz.pos++
			return token{k: tAtKeyword, s: tz.name()}
		}
	case c == '[':
		tz.pos++
		return token{k: tLBracket}
	case c == '\\':
		if tz.validEscape(tz.pos) {
			return tz.identLike()
		}
	case c == ']':
		tz.pos++
		return token{k: tRBracket}
	case c == '{':
		tz.pos++
		return token{k: tLBrace}
	case c == '}':
		tz.pos++
		return token{k: tRBrace}
	case c >= '0' && c <= '9':
		return tz.numeric()
	default:
		if tz.startsIdent(tz.pos) {
			return tz.identLike()
		}
	}
	r, sz := utf8.DecodeRuneInString(tz.src[tz.pos:])
	_ = r
	s := tz.src[tz.pos : tz.pos+sz]
	tz.pos += sz
	return token{k: tDelim, s: s}
}

func (tz *tokenizer) str(q byte) token {
	tz.pos++
	start := tz.pos
	var b *strings.Builder
	flush := func() {
		if b == nil {
			b = &strings.Builder{}
			b.WriteString(tz.src[start:tz.pos])
		}
	}
	for tz.pos < len(tz.src) {
		c := tz.src[tz.pos]
		switch {
		case c == q:
			s := tz.src[start:tz.pos]
			if b != nil {
				s = b.String()
			}
			tz.pos++
			return token{k: tString, s: s}
		case isNL(c):
			// unterminated: bad string, newline not consumed
			return token{k: tBadString}
		case c == '\\':
			flush()
			tz.pos++
			if tz.pos >= len(tz.src) {
				continue
			}
			if isNL(tz.src[tz.pos]) {
				if tz.src[tz.pos] == '\r' && tz.at(tz.pos+1) == '\n' {
					tz.pos++
				}
				tz.pos++
				continue
			}
			b.WriteRune(tz.escape())
			continue
		}
		if b != nil {
			b.WriteByte(c)
		}
		tz.pos++
	}
	// EOF: the string is returned as is
	s := tz.src[start:tz.pos]
	if b != nil {
		s = b.String()
	}
	return token{k: tString, s: s}
}

func (tz *tokenizer) numeric() token {
	v, isInt := tz.number()
	if tz.startsIdent(tz.pos) {
		return token{k: tDimension, num: v, isInt: isInt, s: tz.name()}
	}
	if tz.at(tz.pos) == '%' {
		tz.pos++
		return token{k: tPercentage, num: v}
	}
	return token{k: tNumber, num: v, isInt: isInt}
}

func (tz *tokenizer) identLike() token {
	s := tz.name()
	if tz.at(tz.pos) == '(' {
		tz.pos++
		if strings.EqualFold(s, "url") {
			p := tz.pos
			for p < len(tz.src) && isWS(tz.src[p]) {
				p++
			}
			if c := tz.at(p); c == '"' || c == '\'' {
				return token{k: tFunction, s: s}
			}
			return tz.url()
		}
		return token{k: tFunction, s: s}
	}
	return token{k: tIdent, s: s}
}

func (tz *tokenizer) url() token {
	for tz.pos < len(tz.src) && isWS(tz.src[tz.pos]) {
		tz.pos++
	}
	var b strings.Builder
	for tz.pos < len(tz.src) {
		c := tz.src[tz.pos]
		switch {
		case c == ')':
			tz.pos++
			return token{k: tURL, s: b.String()}
		case isWS(c):
			for tz.pos < len(tz.src) && isWS(tz.src[tz.pos]) {
				tz.pos++
			}
			if tz.pos >= len(tz.src) {
				return token{k: tURL, s: b.String()}
			}
			if tz.src[tz.pos] == ')' {
				tz.pos++
				return token{k: tURL, s: b.String()}
			}
			tz.badURL()
			return token{k: tBadURL}
		case c == '"' || c == '\'' || c == '(' || c < 0x20 && c != '\t' || c == 0x7f:
			tz.badURL()
			return token{k: tBadURL}
		case c == '\\':
			if tz.validEscape(tz.pos) {
				tz.pos++
				b.WriteRune(tz.escape())
				continue
			}
			tz.badURL()
			return token{k: tBadURL}
		}
		b.WriteByte(c)
		tz.pos++
	}
	return token{k: tURL, s: b.String()}
}

func (tz *tokenizer) badURL() {
	for tz.pos < len(tz.src) {
		c := tz.src[tz.pos]
		if c == ')' {
			tz.pos++
			return
		}
		if c == '\\' && tz.validEscape(tz.pos) {
			tz.pos++
			tz.escape()
			continue
		}
		tz.pos++
	}
}

// --- component values ---

// cv is a component value: a preserved token, a function with its
// arguments, or a simple block. Whitespace is dropped (it only separates);
// commas and delims stay. start/end are byte spans in the source text.
type cv struct {
	t    token
	name string // lowercase function name
	args []cv   // function arguments / block contents
	end  int
}

func (c *cv) isFn(name string) bool { return c.t.k == tFunction && c.name == name }

// text returns the source text of c.
func (c *cv) text(src string) string { return src[c.t.start:c.end] }

// parseCVs turns a value string into component values.
func parseCVs(src string) []cv {
	toks := tokenize(src)
	i := 0
	return cvList(toks, &i, tEOF)
}

func cvList(toks []token, i *int, close tokKind) []cv {
	var out []cv
	for *i < len(toks) {
		t := toks[*i]
		*i++
		switch t.k {
		case tWS:
			continue
		case close:
			if close != tEOF {
				return out
			}
		case tFunction, tLParen, tLBracket, tLBrace:
			cl := tRParen
			switch t.k {
			case tLBracket:
				cl = tRBracket
			case tLBrace:
				cl = tRBrace
			}
			c := cv{t: t}
			if t.k == tFunction {
				c.name = strings.ToLower(t.s)
			}
			c.args = cvList(toks, i, cl)
			if *i > 0 && *i <= len(toks) {
				c.end = toks[*i-1].end
			}
			out = append(out, c)
			continue
		}
		out = append(out, cv{t: t, end: t.end})
	}
	return out
}

// splitComma splits a cv list at top-level commas.
func splitComma(cs []cv) [][]cv {
	var out [][]cv
	start := 0
	for i := range cs {
		if cs[i].t.k == tComma {
			out = append(out, cs[start:i])
			start = i + 1
		}
	}
	return append(out, cs[start:])
}

// splitDelim splits a cv list at a top-level delim (e.g. '/').
func splitDelim(cs []cv, d byte) [][]cv {
	var out [][]cv
	start := 0
	for i := range cs {
		if cs[i].t.is(d) {
			out = append(out, cs[start:i])
			start = i + 1
		}
	}
	return append(out, cs[start:])
}

// spanText is the source text from the first to the last cv of cs.
func spanText(src string, cs []cv) string {
	if len(cs) == 0 {
		return ""
	}
	return src[cs[0].t.start:cs[len(cs)-1].end]
}
