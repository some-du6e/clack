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
// Slack's rules (see prefixOK): *bold*, _italic_, ~strike~ and `code` need a
// word boundary outside the markers and stay within one line; `code` spans,
// <tokens> and resolved :emoji: are atomic and never formatted inside, except
// that a link's label is itself mrkdwn.
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
				for _, sp := range token(r, s[i+1:end-1], false) {
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
	ch   rune
	atom []Span
	code bool  // atom is a `code` span
	ent  bool  // ch was decoded from &amp; &lt; or &gt;
	mk   uint8 // consumed formatting marker (dropped from output): mkOpen or mkClose
	st   Style
}

const (
	mkOpen uint8 = 1 + iota
	mkClose
)

// Slack's word-boundary rule for *bold*, _italic_, ~strike~ and `code`,
// measured against Slack's own renderer (web client and the server-side
// mrkdwn → rich_text conversion agree). A marker needs, just outside it:
//   - line start/end, whitespace, or one of the ASCII sets below;
//   - U+2000–U+206F (General Punctuation: — – … “ ” ‘ ’ • ‰ ‽, ZWSP …) or
//     U+2E00–U+2E7F (Supplemental Punctuation: ⸺ ⸻ …);
//   - an emoji, :emoji:, <link/mention> or, after a closing marker, `code`.
//
// Everything else blocks formatting: letters and digits of any script
// (no mid-word a*b*c), CJK punctuation (。「」（）), Latin-1 punctuation
// (« » ¡ ¿ · § °) and other symbols (€ × → ★ ™). The raw text is what
// counts, so an escape (&amp; &lt; &gt;) ends in ';' (fine before an opening
// marker) and starts with '&' (not fine after a closing one).
const (
	prefixASCII = "!\"#$%(+,-./:;=?[^{"
	suffixASCII = "!\"#$%')+,-./:;=?[]^{}"
)

// suffixExtra lists the marker characters that may follow a closing m.
func suffixExtra(m rune) string {
	switch m {
	case '*':
		return "~"
	case '_':
		return "*`~"
	}
	return ""
}

func isWideBound(c rune) bool {
	return unicode.IsSpace(c) || c >= 0x2000 && c <= 0x206F || c >= 0x2E00 && c <= 0x2E7F || isEmojiRune(c)
}

// prefixOK reports whether u may sit just before an opening marker m.
func prefixOK(m rune, u *unit) bool {
	switch {
	case u.mk == mkOpen: // enclosing span: *_x_*
		return true
	case u.atom != nil:
		return !u.code
	case u.ent:
		return true
	case u.ch < utf8.RuneSelf:
		return unicode.IsSpace(u.ch) || strings.ContainsRune(prefixASCII, u.ch) || m == '`' && strings.ContainsRune("\\_*~", u.ch)
	}
	return isWideBound(u.ch)
}

// suffixOK reports whether u may sit just after a closing marker m.
func suffixOK(m rune, u *unit) bool {
	switch {
	case u.mk == mkClose: // enclosing span
		return true
	case u.atom != nil:
		return true
	case u.ent:
		return false
	case u.ch < utf8.RuneSelf:
		return unicode.IsSpace(u.ch) || strings.ContainsRune(suffixASCII, u.ch) || strings.ContainsRune(suffixExtra(m), u.ch)
	}
	return isWideBound(u.ch)
}

func isSpaceUnit(u *unit) bool { return u.atom == nil && u.mk == 0 && unicode.IsSpace(u.ch) }

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
			// `code` needs a boundary before it (anything may follow) and
			// some non-space content.
			if j := strings.IndexByte(s[i+1:], '`'); j > 0 && strings.TrimSpace(s[i+1:i+1+j]) != "" &&
				(len(us) == 0 || prefixOK('`', &us[len(us)-1])) {
				us = append(us, unit{atom: nonNil(literal(r, s[i+1:i+1+j], Code)), code: true})
				i += j + 2
				continue
			}
		case '<':
			if end := tokenEnd(s, i); end > 0 {
				us = append(us, unit{atom: nonNil(token(r, s[i+1:end-1], true))})
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
				us = append(us, unit{ch: rune(c), ent: true})
				i += n
				continue
			}
		}
		c, n := utf8.DecodeRuneInString(s[i:])
		us = append(us, unit{ch: c})
		i += n
	}
	applyMarkers(us)

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
		case u.mk != 0:
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

// isRaw reports whether u is an unconsumed m character.
func isRaw(u *unit, m rune) bool { return u.atom == nil && u.mk == 0 && u.ch == m }

func markerStyle(c rune) Style {
	switch c {
	case '*':
		return Bold
	case '_':
		return Italic
	case '~':
		return Strike
	}
	return 0
}

// applyMarkers pairs *, _ and ~ markers in one left-to-right pass, the way
// Slack does: each opener (in text order) takes the first closer that
// satisfies the rules, and the scan then continues inside the span, so
// spans nest (*_x_*) and may even overlap (*a _b* c_ is bold "a b",
// italic "b c").
func applyMarkers(us []unit) {
	for i := range us {
		if us[i].atom != nil || us[i].mk != 0 {
			continue
		}
		m := us[i].ch
		st := markerStyle(m)
		if st == 0 || i > 0 && !prefixOK(m, &us[i-1]) {
			continue
		}
		if j := closer(us, i, m); j > 0 {
			us[i].mk, us[j].mk = mkOpen, mkClose
			for k := i + 1; k < j; k++ {
				us[k].st |= st
			}
		}
	}
}

// closer finds the marker closing the m opened at us[i], or 0. Bold and
// strike content never contains its own marker, so only the next one is
// tried; italic content may (_snake_case_ is italic "snake_case").
func closer(us []unit, i int, m rune) int {
	for j := i + 1; j < len(us); j++ {
		if !isRaw(&us[j], m) {
			continue
		}
		if contentOK(us[i+1:j], m) && (j+1 == len(us) || suffixOK(m, &us[j+1])) {
			return j
		}
		if m != '_' {
			return 0
		}
	}
	return 0
}

// contentOK checks the text between two m markers: not empty or all
// whitespace, not starting or ending with m itself (**x**, _x__); bold may
// not both start and end with a space (2 * 3 * 4), strike may not end with
// one; italic allows either.
func contentOK(c []unit, m rune) bool {
	if len(c) == 0 || isRaw(&c[0], m) || isRaw(&c[len(c)-1], m) {
		return false
	}
	blank := true
	for k := range c {
		if !isSpaceUnit(&c[k]) {
			blank = false
			break
		}
	}
	if blank {
		return false
	}
	lead, trail := isSpaceUnit(&c[0]), isSpaceUnit(&c[len(c)-1])
	switch m {
	case '*':
		return !(lead && trail)
	case '~':
		return !trail
	}
	return true
}

// emojiRanges approximates the characters Slack turns into emoji (they
// count as word boundaries): Unicode's Emoji property outside ASCII, minus
// © ® ™ which Slack keeps as text, plus the joiners and modifiers that
// appear next to them (VS16, keycap, tags). Sorted, inclusive.
var emojiRanges = [...][2]rune{
	{0x200D, 0x200D}, {0x203C, 0x203C}, {0x2049, 0x2049}, {0x20E3, 0x20E3}, {0x2139, 0x2139},
	{0x2194, 0x2199}, {0x21A9, 0x21AA}, {0x231A, 0x231B}, {0x2328, 0x2328}, {0x23CF, 0x23CF},
	{0x23E9, 0x23F3}, {0x23F8, 0x23FA}, {0x24C2, 0x24C2}, {0x25AA, 0x25AB}, {0x25B6, 0x25B6},
	{0x25C0, 0x25C0}, {0x25FB, 0x25FE}, {0x2600, 0x2604}, {0x260E, 0x260E}, {0x2611, 0x2611},
	{0x2614, 0x2615}, {0x2618, 0x2618}, {0x261D, 0x261D}, {0x2620, 0x2620}, {0x2622, 0x2623},
	{0x2626, 0x2626}, {0x262A, 0x262A}, {0x262E, 0x262F}, {0x2638, 0x263A}, {0x2640, 0x2640},
	{0x2642, 0x2642}, {0x2648, 0x2653}, {0x265F, 0x2660}, {0x2663, 0x2663}, {0x2665, 0x2666},
	{0x2668, 0x2668}, {0x267B, 0x267B}, {0x267E, 0x267F}, {0x2692, 0x2697}, {0x2699, 0x2699},
	{0x269B, 0x269C}, {0x26A0, 0x26A1}, {0x26A7, 0x26A7}, {0x26AA, 0x26AB}, {0x26B0, 0x26B1},
	{0x26BD, 0x26BE}, {0x26C4, 0x26C5}, {0x26C8, 0x26C8}, {0x26CE, 0x26CF}, {0x26D1, 0x26D1},
	{0x26D3, 0x26D4}, {0x26E9, 0x26EA}, {0x26F0, 0x26F5}, {0x26F7, 0x26FA}, {0x26FD, 0x26FD},
	{0x2702, 0x2702}, {0x2705, 0x2705}, {0x2708, 0x270D}, {0x270F, 0x270F}, {0x2712, 0x2712},
	{0x2714, 0x2714}, {0x2716, 0x2716}, {0x271D, 0x271D}, {0x2721, 0x2721}, {0x2728, 0x2728},
	{0x2733, 0x2734}, {0x2744, 0x2744}, {0x2747, 0x2747}, {0x274C, 0x274C}, {0x274E, 0x274E},
	{0x2753, 0x2755}, {0x2757, 0x2757}, {0x2763, 0x2764}, {0x2795, 0x2797}, {0x27A1, 0x27A1},
	{0x27B0, 0x27B0}, {0x27BF, 0x27BF}, {0x2934, 0x2935}, {0x2B05, 0x2B07}, {0x2B1B, 0x2B1C},
	{0x2B50, 0x2B50}, {0x2B55, 0x2B55}, {0x3030, 0x3030}, {0x303D, 0x303D}, {0x3297, 0x3297},
	{0x3299, 0x3299}, {0xFE0F, 0xFE0F}, {0x1F000, 0x1FAFF}, {0xE0020, 0xE007F},
}

func isEmojiRune(c rune) bool {
	lo, hi := 0, len(emojiRanges)
	for lo < hi {
		mid := (lo + hi) / 2
		switch e := emojiRanges[mid]; {
		case c < e[0]:
			hi = mid
		case c > e[1]:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

// token turns the inside of a <...> control sequence into spans. With
// format set (outside code), a link's explicit label is mrkdwn itself:
// <https://x|a *b*> shows "a " and a bold "b", both linked.
func token(r Resolver, inner string, format bool) []Span {
	target, raw, _ := strings.Cut(inner, "|")
	label := unescape(raw)
	switch {
	case strings.HasPrefix(target, "@"):
		return []Span{userSpan(r, target[1:], label)}
	case strings.HasPrefix(target, "#"):
		return []Span{channelSpan(r, target[1:], label)}
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
		return []Span{linkSpan(href, strings.TrimPrefix(href, "mailto:"))}
	}
	if !format || !strings.ContainsAny(raw, "*_~`:") {
		return []Span{linkSpan(href, label)}
	}
	h := SafeURL(href)
	out := inline(r, raw)
	for k := range out {
		if out[k].Kind == Text && h != "" {
			out[k].Kind, out[k].URL = Link, h
		}
	}
	return out
}

func linkSpan(href, label string) Span {
	if h := SafeURL(href); h != "" {
		return Span{Kind: Link, Text: label, URL: h}
	}
	return Span{Kind: Text, Text: label}
}

// MentionNamer is an optional Resolver method for user mentions: the
// member's name if it is already known, without reading the member for
// display (no fetch of a missing one). Slack's mention reads its member
// when it renders (MemberSlug's getMemberById), not when the message
// loads, so core builds message docs with it and the view asks for the
// mentions it draws (richview's MemberRead).
type MentionNamer interface {
	MentionName(id string) string
}

func userSpan(r Resolver, id, label string) Span {
	var name string
	if mn, ok := r.(MentionNamer); ok {
		name = mn.MentionName(id)
	} else {
		name = r.UserName(id)
	}
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
	// UserGroup (boot-shared fg3x): c-mrkdwn__user_group--mention, the
	// self highlight, when the viewer is in the group
	self := false
	if m, ok := r.(UsergroupMember); ok && id != "" {
		self = m.InUsergroup(id)
	}
	return Span{Kind: Usergroup, Text: "@" + strings.TrimPrefix(name, "@"), ID: id, Self: self}
}

// UsergroupMember is an optional Resolver method: whether the viewer is in
// user group id (Slack's isUserInGroup over userGroupMembership).
type UsergroupMember interface {
	InUsergroup(id string) bool
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
