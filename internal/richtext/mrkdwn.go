package richtext

import (
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// FromMrkdwn parses Slack mrkdwn (the escaped `text` field of messages,
// attachments and Block Kit mrkdwn objects) into blocks: paragraphs, quotes
// (`>` and `>>>`) and preformatted (```) blocks. Inline formatting follows
// Slack's rules: *bold*, _italic_, ~strike~ need a word boundary outside the
// markers and non-space just inside them; `code` spans, <tokens> and resolved
// :emoji: are atomic and never formatted inside.
func FromMrkdwn(r Resolver, text string) []Block {
	if text == "" {
		return nil
	}
	parts := strings.Split(text, "```")
	if len(parts)%2 == 0 { // unterminated fence: the last one is literal
		n := len(parts)
		parts[n-2] += "```" + parts[n-1]
		parts = parts[:n-1]
	}
	var out []Block
	for i, p := range parts {
		if i%2 == 1 {
			p = strings.TrimPrefix(p, "\n")
			p = strings.TrimSuffix(p, "\n")
			if strings.TrimSpace(p) == "" {
				out = appendLines(r, out, "``````")
				continue
			}
			out = append(out, Block{Kind: Preformatted, Spans: preSpans(r, p)})
			continue
		}
		if i > 0 {
			p = strings.TrimPrefix(p, "\n")
		}
		if i < len(parts)-1 {
			p = strings.TrimSuffix(p, "\n")
		}
		out = appendLines(r, out, p)
	}
	return out
}

// appendLines splits s into paragraph and quote blocks.
func appendLines(r Resolver, out []Block, s string) []Block {
	if s == "" {
		return out
	}
	var cur *Block
	rest := false // ">>>" quotes everything that follows
	for _, ln := range strings.Split(s, "\n") {
		q := rest
		if !q {
			if t, ok := cutQuote(ln, 3); ok {
				rest, q, ln = true, true, t
			} else if t, ok := cutQuote(ln, 1); ok {
				q, ln = true, t
			}
		}
		kind := Paragraph
		if q {
			kind = Quote
		}
		if cur == nil || cur.Kind != kind {
			out = append(out, Block{Kind: kind})
			cur = &out[len(out)-1]
		} else {
			cur.Spans = append(cur.Spans, Span{Kind: Break})
		}
		cur.Spans = append(cur.Spans, inline(r, ln)...)
	}
	return out
}

// cutQuote strips n quote markers (escaped "&gt;" or raw ">") and one space.
func cutQuote(ln string, n int) (string, bool) {
	for k := 0; k < n; k++ {
		switch {
		case strings.HasPrefix(ln, "&gt;"):
			ln = ln[4:]
		case strings.HasPrefix(ln, ">"):
			ln = ln[1:]
		default:
			return "", false
		}
	}
	return strings.TrimPrefix(ln, " "), true
}

// preSpans expands tokens and unescapes a ``` block, one line per Break.
func preSpans(r Resolver, s string) []Span {
	var out []Span
	for i, ln := range strings.Split(s, "\n") {
		if i > 0 {
			out = append(out, Span{Kind: Break})
		}
		out = append(out, literal(r, ln, 0)...)
	}
	return out
}

// literal expands <tokens> and unescapes text without any formatting
// (inside code spans and pre blocks).
func literal(r Resolver, s string, st Style) []Span {
	var out []Span
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, Span{Kind: Text, Text: buf.String(), Style: st})
			buf.Reset()
		}
	}
	for i := 0; i < len(s); {
		if s[i] == '<' {
			if end := tokenEnd(s, i); end > 0 {
				flush()
				for _, sp := range token(r, s[i+1:end-1]) {
					if sp.Kind == Text && sp.Text == "" {
						continue
					}
					sp.Style |= st
					out = append(out, sp)
				}
				i = end
				continue
			}
		}
		if s[i] == '&' {
			if c, n := entity(s[i:]); n > 0 {
				buf.WriteByte(c)
				i += n
				continue
			}
		}
		buf.WriteByte(s[i])
		i++
	}
	flush()
	return out
}

// entity decodes Slack's three escapes at the start of s.
func entity(s string) (byte, int) {
	switch {
	case strings.HasPrefix(s, "&amp;"):
		return '&', 5
	case strings.HasPrefix(s, "&lt;"):
		return '<', 4
	case strings.HasPrefix(s, "&gt;"):
		return '>', 4
	}
	return 0, 0
}

func unescape(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(s)
}

// tokenEnd returns the index just past the <...> token starting at s[i], or 0.
// A user-typed "<" arrives escaped as &lt;, so only real tokens use literal
// angle brackets.
func tokenEnd(s string, i int) int {
	if i+1 >= len(s) {
		return 0
	}
	switch s[i+1] {
	case '<', '>', ' ', '\t', '\n', '\r':
		return 0
	}
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '>':
			return j + 1
		case '<', '\n':
			return 0
		}
	}
	return 0
}

func isEmojiNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '+' || c == '-' || c == '\''
}

// emojiAt matches :name: or :name::skin-tone-N: at s[i]; it returns the name
// (with the skin tone suffix) and the length of the match.
func emojiAt(s string, i int) (string, int) {
	j := i + 1
	for j < len(s) && isEmojiNameByte(s[j]) {
		j++
	}
	if j == i+1 || j >= len(s) || s[j] != ':' {
		return "", 0
	}
	if t := s[j:]; len(t) >= 14 && strings.HasPrefix(t, "::skin-tone-") && t[12] >= '2' && t[12] <= '6' && t[13] == ':' {
		return s[i+1 : j+13], j + 14 - i
	}
	return s[i+1 : j], j + 1 - i
}

// emojiSpan resolves an emoji name; ok is false for unknown names.
func emojiSpan(r Resolver, name string) (Span, bool) {
	u, text, ok := r.Emoji(name)
	if !ok {
		return Span{}, false
	}
	if text == "" {
		text = ":" + name + ":"
	}
	return Span{Kind: Emoji, Text: text, URL: u, ID: name}, true
}

// unit is one rune of a line, or an atomic group of spans (code, token, emoji).
type unit struct {
	ch     rune
	atom   []Span
	marker bool // consumed formatting marker (dropped from output)
	st     Style
}

const markerChars = "*_~"

func isOpenBound(u *unit) bool {
	if u.atom != nil || u.marker {
		return true
	}
	return unicode.IsSpace(u.ch) || strings.ContainsRune("([{>\"'“‘«", u.ch) || strings.ContainsRune(markerChars, u.ch)
}

func isCloseBound(u *unit) bool {
	if u.atom != nil || u.marker {
		return true
	}
	return unicode.IsSpace(u.ch) || strings.ContainsRune(".,!?;:)]}>\"'”’»-", u.ch) || strings.ContainsRune(markerChars, u.ch)
}

func isSpaceUnit(u *unit) bool { return u.atom == nil && !u.marker && unicode.IsSpace(u.ch) }

// inline formats one line of mrkdwn.
func inline(r Resolver, s string) []Span {
	if s == "" {
		return nil
	}
	if !strings.ContainsAny(s, "*_~`<:&") {
		return []Span{{Kind: Text, Text: s}}
	}
	us := make([]unit, 0, len(s))
	for i := 0; i < len(s); {
		switch s[i] {
		case '`':
			if j := strings.IndexByte(s[i+1:], '`'); j > 0 {
				us = append(us, unit{atom: nonNil(literal(r, s[i+1:i+1+j], Code))})
				i += j + 2
				continue
			}
		case '<':
			if end := tokenEnd(s, i); end > 0 {
				us = append(us, unit{atom: nonNil(token(r, s[i+1:end-1]))})
				i = end
				continue
			}
		case ':':
			if name, n := emojiAt(s, i); n > 0 {
				if sp, ok := emojiSpan(r, name); ok {
					us = append(us, unit{atom: []Span{sp}})
					i += n
					continue
				}
			}
		case '&':
			if c, n := entity(s[i:]); n > 0 {
				us = append(us, unit{ch: rune(c)})
				i += n
				continue
			}
		}
		c, n := utf8.DecodeRuneInString(s[i:])
		us = append(us, unit{ch: c})
		i += n
	}
	applyMarker(us, '*', Bold)
	applyMarker(us, '_', Italic)
	applyMarker(us, '~', Strike)

	var out []Span
	var buf strings.Builder
	var bst Style
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, Span{Kind: Text, Text: buf.String(), Style: bst})
			buf.Reset()
		}
	}
	for i := range us {
		u := &us[i]
		switch {
		case u.marker:
		case u.atom != nil:
			flush()
			for _, sp := range u.atom {
				if sp.Kind == Text && sp.Text == "" {
					continue
				}
				sp.Style |= u.st
				out = append(out, sp)
			}
		default:
			if buf.Len() > 0 && u.st != bst {
				flush()
			}
			bst = u.st
			buf.WriteRune(u.ch)
		}
	}
	flush()
	return out
}

// nonNil keeps a unit an atom even when it produced no spans.
func nonNil(ss []Span) []Span {
	if ss == nil {
		return []Span{}
	}
	return ss
}

func isMarker(u *unit, m rune) bool { return u.atom == nil && !u.marker && u.ch == m }

// applyMarker pairs m markers following Slack's boundary rules and styles
// the units between them.
func applyMarker(us []unit, m rune, st Style) {
	for i := 0; i < len(us); i++ {
		if !isMarker(&us[i], m) {
			continue
		}
		if i > 0 && !isOpenBound(&us[i-1]) {
			continue
		}
		j := i + 1
		for j < len(us) && !isMarker(&us[j], m) {
			j++
		}
		if j >= len(us) {
			return
		}
		if j == i+1 || isSpaceUnit(&us[i+1]) || isSpaceUnit(&us[j-1]) || (j+1 < len(us) && !isCloseBound(&us[j+1])) {
			continue
		}
		us[i].marker, us[j].marker = true, true
		for k := i + 1; k < j; k++ {
			us[k].st |= st
		}
		i = j
	}
}

// token turns the inside of a <...> control sequence into spans.
func token(r Resolver, inner string) []Span {
	target, label, _ := strings.Cut(inner, "|")
	label = unescape(label)
	switch {
	case strings.HasPrefix(target, "@"):
		return []Span{userSpan(r, target[1:], label)}
	case strings.HasPrefix(target, "#"):
		id := target[1:]
		name := label
		if name == "" {
			name = r.ChannelName(id)
		}
		if name == "" {
			name = id
		}
		return []Span{{Kind: Channel, Text: "#" + strings.TrimPrefix(name, "#"), ID: id}}
	case strings.HasPrefix(target, "!subteam^"):
		id := strings.TrimPrefix(target, "!subteam^")
		return []Span{usergroupSpan(r, id, label)}
	case target == "!here" || target == "!channel" || target == "!everyone" || target == "!group":
		name := target[1:]
		if name == "group" {
			name = "channel"
		}
		return []Span{broadcastSpan(name)}
	case strings.HasPrefix(target, "!date^"):
		return []Span{dateToken(target, label)}
	case strings.HasPrefix(target, "!"):
		if label != "" {
			return []Span{{Kind: Text, Text: label}}
		}
		return []Span{{Kind: Text, Text: target[1:]}}
	}
	href := unescape(target)
	if label == "" {
		label = strings.TrimPrefix(href, "mailto:")
	}
	return []Span{linkSpan(href, label)}
}

func linkSpan(href, label string) Span {
	if h := SafeURL(href); h != "" {
		return Span{Kind: Link, Text: label, URL: h}
	}
	return Span{Kind: Text, Text: label}
}

func userSpan(r Resolver, id, label string) Span {
	name := r.UserName(id)
	if name == "" {
		name = label
	}
	if name == "" {
		name = id
	}
	return Span{Kind: User, Text: "@" + strings.TrimPrefix(name, "@"), ID: id, Self: id != "" && id == r.SelfID()}
}

func usergroupSpan(r Resolver, id, label string) Span {
	name := label
	if name == "" {
		name = r.UsergroupName(id)
	}
	if name == "" {
		name = id
	}
	return Span{Kind: Usergroup, Text: "@" + strings.TrimPrefix(name, "@"), ID: id}
}

func broadcastSpan(name string) Span {
	return Span{Kind: Broadcast, Text: "@" + name, ID: name, Self: true}
}

// dateToken parses !date^1392734382^{date_short} at {time}^optional_link.
func dateToken(target, fallback string) Span {
	parts := strings.SplitN(strings.TrimPrefix(target, "!date^"), "^", 3)
	if len(parts) < 2 {
		return Span{Kind: Text, Text: fallback}
	}
	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return Span{Kind: Text, Text: fallback}
	}
	sp := Span{Kind: Date, Text: FormatDate(time.Unix(sec, 0), parts[1]), ID: strconv.FormatInt(sec, 10)}
	if len(parts) == 3 {
		sp.URL = SafeURL(unescape(parts[2]))
	}
	return sp
}

// onlyEmoji reports whether mrkdwn text is 1..23 resolvable emoji and
// whitespace (Slack "jumbomoji").
func onlyEmoji(r Resolver, text string) bool {
	t := strings.TrimSpace(text)
	if t == "" || len(t) > 23*64 {
		return false
	}
	n := 0
	for i := 0; i < len(t); {
		switch c := t[i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
			continue
		case c == ':':
			if name, k := emojiAt(t, i); k > 0 {
				if _, _, ok := r.Emoji(name); ok {
					n++
					i += k
					continue
				}
			}
		}
		return false
	}
	return n > 0 && n <= 23
}
