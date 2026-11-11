package i18n

import (
	"regexp"
	"strings"
)

// Part is one run of a rich string, as Slack's rt() renders it: the
// message's own text or a placeholder's value, inside a tag or not.
type Part struct {
	Text  string
	Tag   string // the innermost tag around Text ("" outside every tag): "b", "strong", "a", "link", "span", "t1", …
	Class string // that tag's class or className ("c-tooltip__subtitle")
	Arg   string // the {placeholder} Text is the value of ("" for the message's own text)
}

// RT is I18n#rt: src's translation in n formatted with args, cut into
// runs at its tags. The tags are read from the formatted text before the
// values go in (rt's placeholders: textToDomJson, then swapPlaceholders),
// so a name with "<" in it stays text.
func (n NS) RT(src string, args ...any) []Part { return rtIn(string(n), nil, src, pairs(args)) }

// RT is NS.RT over the chain.
func (c *Chain) RT(src string, args ...any) []Part { return rtIn(c.key, c.nss, src, pairs(args)) }

func rtIn(ns string, chain []string, src string, args Args) []Part {
	s := cur.Load()
	text := src
	if s.cat != nil || strings.IndexByte(src, '{') >= 0 {
		text = s.message(ns, chain, src).formatMarked(s.locale, args)
	}
	var out []Part
	parseTags(text, "", "", func(p Part) { out = splitArgs(out, p, args) })
	return out
}

// Plain is the parts' text, tags dropped.
func Plain(parts []Part) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// openTagRE is the opening half of C91c's tag pattern
// /<([a-z0-9-]+)(?: class(?:Name)?="([a-z0-9-_ ]+)")?>(.*?)<\/\1>/gi.
var openTagRE = regexp.MustCompile(`(?i)<([a-z0-9-]+)(?: class(?:Name)?="([a-z0-9-_ ]+)")?>`)

// parseTags is textToDomJson: each <tag>…</tag> (closed by the first
// </tag> on the same line, as the lazy (.*?) without the s flag does) is
// read recursively; the rest is text.
func parseTags(s, tag, class string, emit func(Part)) {
	for s != "" {
		loc := openTagRE.FindStringSubmatchIndex(s)
		if loc == nil {
			emit(Part{Text: s, Tag: tag, Class: class})
			return
		}
		name := s[loc[2]:loc[3]]
		cls := ""
		if loc[4] >= 0 {
			cls = s[loc[4]:loc[5]]
		}
		rest := s[loc[1]:]
		end := indexFold(rest, "</"+name+">")
		if end < 0 || strings.IndexByte(rest[:end], '\n') >= 0 {
			// no match here: the "<" is text, the scan goes on after it
			emit(Part{Text: s[:loc[0]+1], Tag: tag, Class: class})
			s = s[loc[0]+1:]
			continue
		}
		if loc[0] > 0 {
			emit(Part{Text: s[:loc[0]], Tag: tag, Class: class})
		}
		parseTags(rest[:end], strings.ToLower(name), cls, emit)
		s = rest[end+len(name)+3:]
	}
}

// indexFold is strings.Index, ASCII case-insensitive (the /i flag).
func indexFold(s, sub string) int {
	n := len(sub)
	for i := 0; i+n <= len(s); i++ {
		if strings.EqualFold(s[i:i+n], sub) {
			return i
		}
	}
	return -1
}

// splitArgs appends p to out with its placeholder marks replaced by the
// values, each value its own part (Arg set); empty runs are dropped, and a
// run of the message's own text joins the one before it when both are
// alike (the marks no longer split it).
func splitArgs(out []Part, p Part, args Args) []Part {
	add := func(q Part) {
		if q.Text == "" {
			return
		}
		if n := len(out); n > 0 && q.Arg == "" && out[n-1].Arg == "" && out[n-1].Tag == q.Tag && out[n-1].Class == q.Class {
			out[n-1].Text += q.Text
			return
		}
		out = append(out, q)
	}
	s := p.Text
	for {
		i := strings.Index(s, argOpen)
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], argClose)
		if j < 0 {
			break
		}
		add(Part{Text: s[:i], Tag: p.Tag, Class: p.Class})
		name := s[i+len(argOpen) : i+j]
		add(Part{Text: jsString(args[name], args != nil && hasKey(args, name)), Tag: p.Tag, Class: p.Class, Arg: name})
		s = s[i+j+len(argClose):]
	}
	add(Part{Text: s, Tag: p.Tag, Class: p.Class})
	return out
}

// Listify is Slack's listify (client-boot nTGe) for plain items in the
// active locale: en-US "A and B", "A, B, and C" (the serial comma from
// three on); es-ES "A y B", "A, B y C"; ja-JP "A、 B、 C" (no
// conjunction for "and"); zh "、"; ko-KR's serial comma only for "or".
// "and" and "or" are the general namespace's.
func Listify(items []string, or bool) string {
	if len(items) == 0 {
		return ""
	}
	sep, serial, conj := ",", false, true
	var word string
	if or {
		word = T("general", "or")
	} else {
		word = T("general", "and")
	}
	switch Locale() {
	case EnUS:
		serial = true
	case "ja-JP":
		if !or {
			conj = false
		}
		sep = "、"
	case "ko-KR":
		if or {
			serial = true
		}
	case "zh-CN", "zh-TW":
		sep = "、"
	}
	n := len(items)
	last := sep + " "
	switch {
	case serial && conj && n > 2:
		last = sep + " " + word + " "
	case conj:
		last = " " + word + " "
	}
	var b strings.Builder
	for i, it := range items {
		b.WriteString(it)
		if i < n-2 {
			b.WriteString(sep + " ")
		} else if i < n-1 {
			b.WriteString(last)
		}
	}
	return b.String()
}
