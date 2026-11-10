package i18n

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// The MessageFormat Slack's strings go through: format-message
// (message-format/format-message-parse/format-message-interpret, vendors
// modules YZtm, BYkh, 2bDf), called as new MessageFormat(text, locale)
// without tag parsing, so "<b>" and "<link>" stay literal text.
//
// Syntax: text, '' for a quote, '{…}' quoting (also '#' inside a plural),
// {arg}, {arg, number[, integer|percent]}, {arg, date|time[, style]},
// {arg, plural|selectordinal, [offset:n] =n {…} one {…} other {…}} with #
// for the number, {arg, select, a {…} other {…}}.

type node struct {
	text   string // a literal (kind 0)
	kind   byte   // 0 text, 1 {arg}, 2 number, 3 plural, 4 selectordinal, 5 select, 6 #, 7 date, 8 time
	arg    string
	style  string
	offset float64
	cases  map[string][]node
}

// Message is a parsed message.
type Message struct {
	nodes []node
	err   error
}

type parser struct {
	s string
	i int
}

// Parse parses a message. A syntax error doesn't fail: the message then
// formats as its raw text (format-message would throw and Slack's
// component would show nothing; Clack shows the source).
func Parse(s string) *Message {
	p := &parser{s: s}
	nodes, err := p.message("")
	if err == nil && p.i < len(s) {
		err = fmt.Errorf("i18n: unexpected %q at %d", s[p.i], p.i)
	}
	if err != nil {
		return &Message{nodes: []node{{text: s}}, err: err}
	}
	return &Message{nodes: nodes}
}

// Err is the parse error, if any.
func (m *Message) Err() error { return m.err }

// message reads elements until a closing } (parent is the enclosing type,
// "plural"/"selectordinal" make # special).
func (p *parser) message(parent string) ([]node, error) {
	var out []node
	for p.i < len(p.s) {
		if t := p.text(parent); t != "" {
			out = append(out, node{text: t})
		}
		if p.i >= len(p.s) {
			break
		}
		switch p.s[p.i] {
		case '}':
			if parent == "" {
				return nil, fmt.Errorf("i18n: unmatched } at %d", p.i)
			}
			return out, nil
		case '#':
			p.i++
			out = append(out, node{kind: 6})
		case '{':
			n, err := p.placeholder()
			if err != nil {
				return nil, err
			}
			out = append(out, n)
		}
	}
	if parent != "" {
		return nil, fmt.Errorf("i18n: unclosed {")
	}
	return out, nil
}

// text is BYkh parseText.
func (p *parser) text(parent string) string {
	plural := parent == "plural" || parent == "selectordinal"
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '{' || c == '}' || (plural && c == '#') {
			break
		}
		if c != '\'' {
			b.WriteByte(c)
			p.i++
			continue
		}
		p.i++
		if p.i >= len(p.s) {
			b.WriteByte('\'')
			break
		}
		c = p.s[p.i]
		switch {
		case c == '\'':
			b.WriteByte('\'')
			p.i++
		case c == '{' || c == '}' || (plural && c == '#'):
			// quoted run until the next unpaired '
			b.WriteByte(c)
			p.i++
			for p.i < len(p.s) {
				c = p.s[p.i]
				if c == '\'' && p.i+1 < len(p.s) && p.s[p.i+1] == '\'' {
					b.WriteByte('\'')
					p.i += 2
				} else if c == '\'' {
					p.i++
					break
				} else {
					b.WriteByte(c)
					p.i++
				}
			}
		default:
			b.WriteByte('\'')
		}
	}
	return b.String()
}

func isSpace(c byte) bool { return c == ' ' || (c >= 9 && c <= 13) }

func (p *parser) skip() {
	for p.i < len(p.s) {
		if isSpace(p.s[p.i]) {
			p.i++
			continue
		}
		// U+00A0 and the other Unicode spaces format-message skips
		if strings.HasPrefix(p.s[p.i:], " ") {
			p.i += len(" ")
			continue
		}
		break
	}
}

func (p *parser) word() string {
	st := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if isSpace(c) || c == ',' || c == '{' || c == '}' || c == '\'' || c == '#' {
			break
		}
		p.i++
	}
	return p.s[st:p.i]
}

func (p *parser) placeholder() (node, error) {
	p.i++ // {
	p.skip()
	id := p.word()
	if id == "" {
		return node{}, fmt.Errorf("i18n: empty placeholder at %d", p.i)
	}
	p.skip()
	if p.i >= len(p.s) {
		return node{}, fmt.Errorf("i18n: unclosed {%s", id)
	}
	if p.s[p.i] == '}' {
		p.i++
		return node{kind: 1, arg: id}, nil
	}
	if p.s[p.i] != ',' {
		return node{}, fmt.Errorf("i18n: expected , at %d", p.i)
	}
	p.i++
	p.skip()
	typ := p.word()
	p.skip()
	switch typ {
	case "plural", "selectordinal", "select":
		if p.i >= len(p.s) || p.s[p.i] != ',' {
			return node{}, fmt.Errorf("i18n: expected , after %s", typ)
		}
		p.i++
		p.skip()
		n := node{kind: 5, arg: id, cases: map[string][]node{}}
		if typ == "plural" {
			n.kind = 3
		} else if typ == "selectordinal" {
			n.kind = 4
		}
		if n.kind != 5 && strings.HasPrefix(p.s[p.i:], "offset:") {
			p.i += len("offset:")
			p.skip()
			w := p.word()
			f, err := strconv.ParseFloat(w, 64)
			if err != nil {
				return node{}, fmt.Errorf("i18n: bad offset %q", w)
			}
			n.offset = f
			p.skip()
		}
		for {
			p.skip()
			if p.i >= len(p.s) {
				return node{}, fmt.Errorf("i18n: unclosed %s", typ)
			}
			if p.s[p.i] == '}' {
				p.i++
				break
			}
			key := p.word()
			if key == "" {
				return node{}, fmt.Errorf("i18n: expected a case at %d", p.i)
			}
			p.skip()
			if p.i >= len(p.s) || p.s[p.i] != '{' {
				return node{}, fmt.Errorf("i18n: expected { after %s", key)
			}
			p.i++
			sub, err := p.message(typ)
			if err != nil {
				return node{}, err
			}
			p.i++ // }
			n.cases[key] = sub
		}
		if _, ok := n.cases["other"]; !ok {
			return node{}, fmt.Errorf("i18n: %s without other", typ)
		}
		return n, nil
	case "number", "date", "time", "ordinal", "spellout", "duration":
		n := node{kind: 2, arg: id}
		switch typ {
		case "date":
			n.kind = 7
		case "time":
			n.kind = 8
		}
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
			p.skip()
			st := p.i
			for p.i < len(p.s) && p.s[p.i] != '}' {
				p.i++
			}
			n.style = strings.TrimSpace(p.s[st:p.i])
		}
		if p.i >= len(p.s) || p.s[p.i] != '}' {
			return node{}, fmt.Errorf("i18n: unclosed {%s, %s", id, typ)
		}
		p.i++
		return n, nil
	}
	return node{}, fmt.Errorf("i18n: unknown type %q", typ)
}

// Args are a message's values by name.
type Args map[string]any

// Format formats m for locale loc.
func (m *Message) Format(loc string, args Args) string {
	if len(m.nodes) == 1 && m.nodes[0].kind == 0 {
		return m.nodes[0].text
	}
	var b strings.Builder
	formatNodes(&b, m.nodes, loc, args, nil)
	return b.String()
}

func formatNodes(b *strings.Builder, nodes []node, loc string, args Args, plural *node) {
	for i := range nodes {
		n := &nodes[i]
		switch n.kind {
		case 0:
			b.WriteString(n.text)
		case 1:
			b.WriteString(jsString(args[n.arg], args != nil && hasKey(args, n.arg)))
		case 2:
			v, _ := toFloat(args[n.arg])
			b.WriteString(formatNumberStyle(loc, v, n.style))
		case 6:
			if plural == nil {
				b.WriteByte('#')
				continue
			}
			v, _ := toFloat(args[plural.arg])
			b.WriteString(FormatNumber(loc, v-plural.offset))
		case 3, 4:
			v, _ := toFloat(args[n.arg])
			sub, ok := n.cases["="+jsNumber(v)]
			if !ok {
				var form string
				if n.kind == 3 {
					form = Cardinal(loc, v-n.offset)
				} else {
					form = Ordinal(loc, v-n.offset)
				}
				if sub, ok = n.cases[form]; !ok {
					sub = n.cases["other"]
				}
			}
			formatNodes(b, sub, loc, args, n)
		case 5:
			sub, ok := n.cases[jsString(args[n.arg], true)]
			if !ok {
				sub = n.cases["other"]
			}
			formatNodes(b, sub, loc, args, plural)
		case 7, 8:
			if t, ok := args[n.arg].(time.Time); ok {
				b.WriteString(intlDate(loc, t, n.kind == 8, n.style))
			} else {
				b.WriteString(jsString(args[n.arg], hasKey(args, n.arg)))
			}
		}
	}
}

func hasKey(a Args, k string) bool { _, ok := a[k]; return ok }

// jsString is String(v).
func jsString(v any, present bool) string {
	if !present {
		return "undefined"
	}
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	}
	if f, ok := toFloat(v); ok {
		return jsNumber(f)
	}
	return fmt.Sprint(v)
}

// jsNumber is Number#toString for ordinary values.
func jsNumber(f float64) string {
	if math.IsNaN(f) {
		return "NaN"
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return math.NaN(), false
		}
		return f, true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return math.NaN(), false
}
