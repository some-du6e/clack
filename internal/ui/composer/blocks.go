package composer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
)

// blocks.go converts between the composer Doc and Slack's message formats:
//
//   - RichTextBlocks: the rich_text `blocks` param of chat.postMessage /
//     chat.update, shaped like the one Slack's web client sends.
//   - Mrkdwn: the fallback `text` param (notifications, old clients).
//   - DocFromRichtext / DocFromBlocksJSON: load an existing message (edit) or
//     a draft back into the editor.
//
// Blank lines: a section/quote/preformatted run of lines is its lines joined
// with "\n". A run made only of empty lines (k of them) has no text to join,
// so it is written as k newlines, and a block whose text is only newlines
// (n of them) reads back as n empty lines. Every other block reads back as
// (number of "\n") + 1 lines.

// newBlockID returns the block_id of a new rich_text block. Tests stub it.
var newBlockID = func() string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 5)
	for i := range b {
		b[i] = alphabet[rand.IntN(len(alphabet))]
	}
	return string(b)
}

// AtomResolver supplies display names for atoms parsed from JSON. Any
// method may return "" (or ok=false) to fall back to the id.
type AtomResolver interface {
	UserName(id string) string
	ChannelName(id string) string
	UsergroupName(id string) string
	Emoji(name string) (url, text string, ok bool)
}

// ---------------------------------------------------------------------------
// Line grouping shared by the writers.

// rtSpan is one line of a Doc: runes [start, end) and its block format.
type rtSpan struct {
	start, end int
	line       Line
}

// rtLines splits d into lines by scanning runes (robust even if d.Lines is
// out of sync), with leading and trailing blank lines removed. keepLists
// keeps blank list items at either end, as Slack's send does: trimDelta
// (ASJc trimOps) never drops a newline carrying a block format
// (doesAttributesContainBlockFormatting: code-block, blockquote, list),
// and processDeltaAsList makes an empty line {type: rich_text_section,
// elements: []} (VxMM, from prepareAndSendMessage's
// message_send_attempt:blocks), which Slack's RichTextList renders as an
// item with its marker.
func rtLines(d *Doc, keepLists bool) []rtSpan {
	var ls []rtSpan
	start := 0
	for i := 0; i <= len(d.Runes); i++ {
		if i < len(d.Runes) && d.Runes[i] != '\n' {
			continue
		}
		var l Line
		if n := len(ls); n < len(d.Lines) {
			l = d.Lines[n]
		}
		if l.Kind != Bullet && l.Kind != Ordered {
			l.Indent = 0
			l.Quoted = l.Quoted && l.Kind == CodeBlock // only lists and code carry the border
		} else if l.Indent < 0 {
			l.Indent = 0
		}
		ls = append(ls, rtSpan{start: start, end: i, line: l})
		start = i + 1
	}
	trim := func(s rtSpan) bool { return rtBlank(d, s) && !(keepLists && rtIsList(s.line.Kind)) }
	for len(ls) > 0 && trim(ls[0]) {
		ls = ls[1:]
	}
	for len(ls) > 0 && trim(ls[len(ls)-1]) {
		ls = ls[:len(ls)-1]
	}
	return ls
}

func rtBlank(d *Doc, s rtSpan) bool {
	// A divider or heading line is real content even with no runes.
	if s.line.Kind == Divider || isHeading(s.line.Kind) {
		return false
	}
	for i := s.start; i < s.end; i++ {
		if d.Attrs[i].Atom != nil || !unicode.IsSpace(d.Runes[i]) {
			return false
		}
	}
	return true
}

func rtIsList(k BlockKind) bool { return k == Bullet || k == Ordered }

// rtGroup is a run of consecutive lines that becomes one block: same kind,
// and for lists also the same indent.
type rtGroup struct {
	line  Line
	lines []rtSpan
}

// rtOnlyEmpty reports whether every line of g has no runes.
func rtOnlyEmpty(g rtGroup) bool {
	for _, s := range g.lines {
		if s.end > s.start {
			return false
		}
	}
	return true
}

func rtGroups(ls []rtSpan) []rtGroup {
	var gs []rtGroup
	for _, s := range ls {
		if n := len(gs); n > 0 && gs[n-1].line == s.line {
			gs[n-1].lines = append(gs[n-1].lines, s)
			continue
		}
		gs = append(gs, rtGroup{line: s.line, lines: []rtSpan{s}})
	}
	return gs
}

// rtCounter numbers list items across sibling lists of one contiguous list
// run, the way Slack's "offset" does: a nested list does not restart the
// numbering of its parent level.
type rtCounter struct {
	n    map[int]int
	kind map[int]BlockKind
}

func (c *rtCounter) reset() { c.n, c.kind = map[int]int{}, map[int]BlockKind{} }

// take reports how many items precede the next m items at l's level and
// counts them.
func (c *rtCounter) take(l Line, m int) int {
	if c.n == nil {
		c.reset()
	}
	for ind := range c.n {
		if ind > l.Indent {
			delete(c.n, ind)
			delete(c.kind, ind)
		}
	}
	if k, ok := c.kind[l.Indent]; !ok || k != l.Kind {
		c.n[l.Indent] = 0
		c.kind[l.Indent] = l.Kind
	}
	off := c.n[l.Indent]
	c.n[l.Indent] += m
	return off
}

// ---------------------------------------------------------------------------
// rich_text writer

type rtOutBlock struct {
	Type     string `json:"type"`
	BlockID  string `json:"block_id"`
	Elements []any  `json:"elements"`
}

type rtOutHeaderText struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji bool   `json:"emoji"`
}

type rtOutHeader struct {
	Type    string          `json:"type"`
	BlockID string          `json:"block_id"`
	Text    rtOutHeaderText `json:"text"`
	Level   int             `json:"level"`
}

type rtOutDivider struct {
	Type    string `json:"type"`
	BlockID string `json:"block_id"`
}

type rtOutSection struct { // rich_text_section and rich_text_quote
	Type     string `json:"type"`
	Elements []any  `json:"elements"`
}

type rtOutList struct {
	Type     string `json:"type"`
	Elements []any  `json:"elements"`
	Style    string `json:"style"`
	Indent   int    `json:"indent"`
	Offset   int    `json:"offset,omitempty"`
	Border   int    `json:"border"`
}

type rtOutPre struct {
	Type     string `json:"type"`
	Elements []any  `json:"elements"`
	Border   int    `json:"border"`
}

type rtOutStyle struct {
	Bold      bool `json:"bold,omitempty"`
	Italic    bool `json:"italic,omitempty"`
	Strike    bool `json:"strike,omitempty"`
	Code      bool `json:"code,omitempty"`
	Underline bool `json:"underline,omitempty"`
}

type rtOutText struct {
	Type  string      `json:"type"`
	Text  string      `json:"text"`
	Style *rtOutStyle `json:"style,omitempty"`
}

type rtOutLink struct {
	Type      string      `json:"type"`
	URL       string      `json:"url"`
	Text      string      `json:"text,omitempty"`
	Truncated bool        `json:"truncated,omitempty"` // a link slug: Text is its label, not the URL
	Style     *rtOutStyle `json:"style,omitempty"`
}

type rtOutUser struct {
	Type   string `json:"type"`
	UserID string `json:"user_id"`
}

type rtOutChannel struct {
	Type      string `json:"type"`
	ChannelID string `json:"channel_id"`
}

type rtOutUsergroup struct {
	Type        string `json:"type"`
	UsergroupID string `json:"usergroup_id"`
}

type rtOutBroadcast struct {
	Type  string `json:"type"`
	Range string `json:"range"`
}

// rtOutMessageMention is Slack's createSlugChunkFromOp for a message slug:
// text is the URL (or the channel id), then url, author_id, thread_ts.
type rtOutMessageMention struct {
	Type      string `json:"type"`
	MessageTS string `json:"message_ts"`
	ChannelID string `json:"channel_id"`
	Text      string `json:"text"`
	URL       string `json:"url,omitempty"`
	AuthorID  string `json:"author_id,omitempty"`
	ThreadTS  string `json:"thread_ts,omitempty"`
}

type rtOutEmoji struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Unicode  string `json:"unicode,omitempty"`
	SkinTone int    `json:"skin_tone,omitempty"`
}

func rtStyleOut(s Style) *rtOutStyle {
	if s&(Bold|Italic|Strike|Code|Underline) == 0 {
		return nil
	}
	return &rtOutStyle{
		Bold:      s&Bold != 0,
		Italic:    s&Italic != 0,
		Strike:    s&Strike != 0,
		Code:      s&Code != 0,
		Underline: s&Underline != 0,
	}
}

// RichTextBlocks returns the `blocks` param for chat.postMessage and
// chat.update, shaped like Slack's web client builds it: a top-level array
// mixing rich_text blocks (runs of paragraphs, lists, quotes and code) with
// header blocks (one per heading line) and divider blocks. It returns nil
// for a document with no content.
func (d *Doc) RichTextBlocks() json.RawMessage {
	ls := rtLines(d, true)
	if !slices.ContainsFunc(ls, func(s rtSpan) bool { return !rtBlank(d, s) }) {
		return nil // nothing but blank list items
	}
	var out []any
	var run []rtSpan // the current run of rich_text (non-heading/divider) lines
	flush := func() {
		if len(run) == 0 {
			return
		}
		out = append(out, rtOutBlock{Type: "rich_text", BlockID: newBlockID(), Elements: d.rtElements(run)})
		run = run[:0]
	}
	for _, s := range ls {
		switch {
		case isHeading(s.line.Kind):
			flush()
			level := 1
			switch s.line.Kind {
			case Header2:
				level = 2
			case Header3:
				level = 3
			}
			out = append(out, rtOutHeader{
				Type:    "header",
				BlockID: newBlockID(),
				Text:    rtOutHeaderText{Type: "plain_text", Text: d.rtPlain(s.start, s.end), Emoji: true},
				Level:   level,
			})
		case s.line.Kind == Divider:
			flush()
			out = append(out, rtOutDivider{Type: "divider", BlockID: newBlockID()})
		default:
			run = append(run, s)
		}
	}
	flush()
	return rtMarshal(out)
}

// rtElements builds the inline elements of one rich_text block from a run of
// consecutive non-heading, non-divider lines (grouped into sections, lists,
// quotes and code blocks).
func (d *Doc) rtElements(ls []rtSpan) []any {
	els := []any{}
	var cnt rtCounter
	prevList := false
	for _, g := range rtGroups(ls) {
		first, last := g.lines[0], g.lines[len(g.lines)-1]
		switch g.line.Kind {
		case Bullet, Ordered:
			if !prevList {
				cnt.reset()
			}
			items := make([]any, 0, len(g.lines))
			for _, s := range g.lines {
				items = append(items, rtOutSection{Type: "rich_text_section", Elements: d.rtInline(s.start, s.end)})
			}
			l := rtOutList{Type: "rich_text_list", Elements: items, Style: "bullet", Indent: g.line.Indent}
			if g.line.Quoted {
				l.Border = 1 // the line's border format
			}
			off := cnt.take(g.line, len(g.lines))
			if g.line.Kind == Ordered {
				l.Style = "ordered"
				l.Offset = off
			}
			els = append(els, l)
		case CodeBlock:
			txt := d.rtPlain(first.start, last.end)
			if rtOnlyEmpty(g) {
				txt = strings.Repeat("\n", len(g.lines))
			}
			pre := rtOutPre{Type: "rich_text_preformatted", Elements: []any{rtOutText{Type: "text", Text: txt}}}
			if g.line.Quoted {
				pre.Border = 1 // a code block inside a quote (the line's border format)
			}
			els = append(els, pre)
		default: // Para, Quote
			typ := "rich_text_section"
			if g.line.Kind == Quote {
				typ = "rich_text_quote"
			}
			var inl []any
			if rtOnlyEmpty(g) {
				inl = []any{rtOutText{Type: "text", Text: strings.Repeat("\n", len(g.lines))}}
			} else {
				inl = d.rtInline(first.start, last.end)
			}
			els = append(els, rtOutSection{Type: typ, Elements: inl})
		}
		prevList = rtIsList(g.line.Kind)
	}
	return els
}

func rtMarshal(v any) json.RawMessage {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil
	}
	return json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n"))
}

// rtPlain is [start, end) as plain text, atoms as their display text.
func (d *Doc) rtPlain(start, end int) string {
	var b strings.Builder
	for i := start; i < end; i++ {
		if a := d.Attrs[i].Atom; a != nil {
			b.WriteString(rtAtomText(a))
			continue
		}
		b.WriteRune(d.Runes[i])
	}
	return b.String()
}

func rtAtomText(a *Atom) string {
	if a.Kind == AtomEmoji && a.Uni != "" {
		return a.Uni
	}
	if a.Text != "" {
		return a.Text
	}
	switch a.Kind {
	case AtomUser, AtomUsergroup, AtomBroadcast:
		return "@" + a.ID
	case AtomChannel:
		return "#" + a.ID
	case AtomEmoji:
		return ":" + a.ID + ":"
	}
	return a.ID
}

// rtInline returns the inline elements for [start, end): runs of identical
// style and link merge into one text or link element; bare URLs outside code
// and links become link elements (Slack's client autolinks them).
func (d *Doc) rtInline(start, end int) []any {
	out := []any{}
	auto := d.rtAutoLinks(start, end)
	for i := start; i < end; {
		if a := d.Attrs[i].Atom; a != nil {
			out = append(out, rtAtomOut(a))
			i++
			continue
		}
		at := d.Attrs[i]
		au := auto[i-start]
		j := i + 1
		for j < end && d.Attrs[j].Atom == nil && d.Attrs[j].Style == at.Style &&
			d.Attrs[j].Link == at.Link && auto[j-start] == au {
			j++
		}
		txt := string(d.Runes[i:j])
		st := rtStyleOut(at.Style)
		switch {
		case at.Link != "":
			l := rtOutLink{Type: "link", URL: at.Link, Style: st}
			if txt != at.Link {
				l.Text = txt
			}
			out = append(out, l)
		case au != "":
			l := rtOutLink{Type: "link", URL: au, Style: st}
			if txt != au {
				l.Text = txt
			}
			out = append(out, l)
		default:
			out = append(out, rtOutText{Type: "text", Text: txt, Style: st})
		}
		i = j
	}
	return out
}

func rtAtomOut(a *Atom) any {
	switch a.Kind {
	case AtomUser:
		return rtOutUser{Type: "user", UserID: a.ID}
	case AtomChannel:
		return rtOutChannel{Type: "channel", ChannelID: a.ID}
	case AtomUsergroup:
		return rtOutUsergroup{Type: "usergroup", UsergroupID: a.ID}
	case AtomBroadcast:
		return rtOutBroadcast{Type: "broadcast", Range: a.ID}
	case AtomEmoji:
		// Slack's createEmojiChunkFromOp: a standard emoji is its base name
		// plus the (toned) unicode, a custom one its name plus skin_tone.
		name, tone := rtSplitSkinTone(a.ID)
		if a.Uni != "" {
			tone = 0
		}
		return rtOutEmoji{Type: "emoji", Name: name, Unicode: rtUnicodeHex(a.Uni), SkinTone: tone}
	case AtomMessage:
		return rtOutMessageMention{Type: "message_mention", MessageTS: a.ID, ChannelID: a.Channel,
			Text: firstNonEmpty(a.URL, a.Channel), URL: a.URL, AuthorID: a.Author, ThreadTS: a.Thread}
	case AtomLink:
		// createSlugChunkFromOp's "link": url is the slug's id; the
		// insert, when it isn't the URL, is the text, and a label that
		// isn't the URL marks it truncated
		l := rtOutLink{Type: "link", URL: a.ID}
		if a.Text != a.ID {
			l.Text, l.Truncated = a.Text, true
		}
		return l
	}
	return rtOutText{Type: "text", Text: rtAtomText(a)}
}

// rtToneFromUnicode is the skin tone (2-6) of a unicode hex sequence with
// exactly one tone modifier ("1f44b-1f3fc" → 3), else 0.
func rtToneFromUnicode(hex string) int {
	tone := 0
	for _, p := range strings.Split(strings.ToLower(hex), "-") {
		switch p {
		case "1f3fb", "1f3fc", "1f3fd", "1f3fe", "1f3ff":
			if tone != 0 {
				return 0 // two-tone pairs keep their base name
			}
			tone = int(p[4]-'b') + 2
		}
	}
	return tone
}

// rtSplitSkinTone splits "wave::skin-tone-3" into "wave", 3.
func rtSplitSkinTone(id string) (string, int) {
	name, tone, ok := strings.Cut(id, "::skin-tone-")
	if !ok {
		return id, 0
	}
	n, err := strconv.Atoi(tone)
	if err != nil {
		return id, 0
	}
	return name, n
}

// rtUnicodeHex is Slack's "unicode" field: lowercase hex code points joined
// by "-" ("👍" → "1f44d").
func rtUnicodeHex(s string) string {
	var parts []string
	for _, r := range s {
		parts = append(parts, strconv.FormatInt(int64(r), 16))
	}
	return strings.Join(parts, "-")
}

func rtUnicodeFromHex(h string) string {
	if h == "" {
		return ""
	}
	var b strings.Builder
	for _, p := range strings.Split(h, "-") {
		n, err := strconv.ParseInt(p, 16, 32)
		if err != nil || n <= 0 || n > unicode.MaxRune {
			return ""
		}
		b.WriteRune(rune(n))
	}
	return b.String()
}

// rtAutoLinks returns, per rune of [start, end), the URL of the bare URL it
// belongs to, or "". Runes in links, inline code and atoms never autolink.
func (d *Doc) rtAutoLinks(start, end int) []string {
	rs := make([]rune, end-start)
	for i := range rs {
		a := d.Attrs[start+i]
		if a.Atom != nil || a.Link != "" || a.Style&Code != 0 {
			rs[i] = 0
		} else {
			rs[i] = d.Runes[start+i]
		}
	}
	auto := make([]string, len(rs))
	for _, m := range rtFindURLs(rs) {
		for i := m.start; i < m.end; i++ {
			auto[i] = m.url
		}
	}
	return auto
}

type rtURLMatch struct {
	start, end int
	url        string
}

func rtHasPrefixFold(rs []rune, p string) bool {
	pr := []rune(p)
	if len(rs) < len(pr) {
		return false
	}
	for i, r := range pr {
		if unicode.ToLower(rs[i]) != r {
			return false
		}
	}
	return true
}

func rtURLStop(r rune) bool {
	return r == 0 || unicode.IsSpace(r) || r == '<' || r == '>' || r == '"' || r == '`' || r == ObjRune
}

// rtFindURLs finds bare http://, https:// and www. URLs in rs. Trailing
// sentence punctuation and unbalanced closing brackets are not part of a URL.
func rtFindURLs(rs []rune) []rtURLMatch {
	var out []rtURLMatch
	for i := 0; i < len(rs); i++ {
		var pre int
		www := false
		switch {
		case rtHasPrefixFold(rs[i:], "https://"):
			pre = len("https://")
		case rtHasPrefixFold(rs[i:], "http://"):
			pre = len("http://")
		case rtHasPrefixFold(rs[i:], "www."):
			pre = len("www.")
			www = true
		default:
			continue
		}
		if i > 0 {
			p := rs[i-1]
			if unicode.IsLetter(p) || unicode.IsDigit(p) || (www && strings.ContainsRune("./@-_:", p)) {
				continue
			}
		}
		j := i
		for j < len(rs) && !rtURLStop(rs[j]) {
			j++
		}
		for j > i+pre {
			c := rs[j-1]
			if strings.ContainsRune(".,;:!?'*_~", c) {
				j--
				continue
			}
			if c == ')' || c == ']' || c == '}' {
				open := map[rune]rune{')': '(', ']': '[', '}': '{'}[c]
				no, nc := 0, 0
				for _, r := range rs[i:j] {
					if r == open {
						no++
					} else if r == c {
						nc++
					}
				}
				if nc > no {
					j--
					continue
				}
			}
			break
		}
		if j <= i+pre {
			continue
		}
		if www && !strings.ContainsRune(string(rs[i+pre:j]), '.') {
			continue
		}
		u := string(rs[i:j])
		if www {
			u = "http://" + u
		}
		out = append(out, rtURLMatch{start: i, end: j, url: u})
		i = j - 1
	}
	return out
}

// rtIsAutoLink reports whether a link with this text and url is what the
// autolinker would produce from the bare text, so it can be loaded back as
// plain text.
func rtIsAutoLink(text, url string) bool {
	if text == "" {
		text = url
	}
	rs := []rune(text)
	ms := rtFindURLs(rs)
	return len(ms) == 1 && ms[0].start == 0 && ms[0].end == len(rs) && ms[0].url == url
}

// ---------------------------------------------------------------------------
// mrkdwn writer

// rtDividerMrkdwn is the fallback-text rendering of a divider: ten box-drawing
// horizontal rules on their own line.
const rtDividerMrkdwn = "──────────"

// Mrkdwn returns the fallback `text` of the message in Slack mrkdwn.
// Underline has no mrkdwn form and is dropped.
func (d *Doc) Mrkdwn() string {
	ls := rtLines(d, false)
	var out []string
	var cnt rtCounter
	prevList := false
	for _, g := range rtGroups(ls) {
		switch g.line.Kind {
		case Header1, Header2, Header3:
			// A heading's notification text is its plain text, bolded.
			for _, s := range g.lines {
				if t := rtEscape(d.rtPlain(s.start, s.end)); t != "" {
					out = append(out, "*"+t+"*")
				} else {
					out = append(out, "")
				}
			}
		case Divider:
			for range g.lines {
				out = append(out, rtDividerMrkdwn)
			}
		case CodeBlock:
			if g.line.Quoted {
				// convertBlocksToMrkdwnDelta (boot-shared ew6K): a
				// preformatted block with border >= 1 starts "> "
				out = append(out, "> ```")
			} else {
				out = append(out, "```")
			}
			for _, s := range g.lines {
				out = append(out, rtEscape(d.rtPlain(s.start, s.end)))
			}
			out = append(out, "```")
		case Bullet, Ordered:
			if !prevList {
				cnt.reset()
			}
			pad := strings.Repeat("    ", g.line.Indent)
			if g.line.Quoted {
				pad = "> " + pad // a list inside a quote
			}
			for _, s := range g.lines {
				mark := "• "
				if g.line.Kind == Ordered {
					mark = strconv.Itoa(cnt.take(g.line, 1)+1) + ". "
				} else {
					cnt.take(g.line, 1)
				}
				out = append(out, pad+mark+d.rtMrkdwnLine(s.start, s.end))
			}
		case Quote:
			for _, s := range g.lines {
				if t := d.rtMrkdwnLine(s.start, s.end); t != "" {
					out = append(out, "> "+t)
				} else {
					out = append(out, ">")
				}
			}
		default:
			for _, s := range g.lines {
				out = append(out, d.rtMrkdwnLine(s.start, s.end))
			}
		}
		prevList = rtIsList(g.line.Kind)
	}
	return strings.Join(out, "\n")
}

var rtEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func rtEscape(s string) string { return rtEscaper.Replace(s) }

func rtMarker(s Style) string {
	switch s {
	case Bold:
		return "*"
	case Italic:
		return "_"
	case Strike:
		return "~"
	case Code:
		return "`"
	}
	return ""
}

// rtMrkdwnLine writes one line (no '\n' inside). Style markers open and
// close as a stack so they nest properly, and hug the text: whitespace at
// the edge of a styled run goes outside the markers.
func (d *Doc) rtMrkdwnLine(start, end int) string {
	var b strings.Builder
	var active []Style
	pending := "" // trailing whitespace held back until we know if a marker closes first
	for i := start; i < end; {
		if a := d.Attrs[i].Atom; a != nil {
			b.WriteString(pending)
			pending = ""
			b.WriteString(rtAtomMrkdwn(a))
			i++
			continue
		}
		at := d.Attrs[i]
		j := i + 1
		for j < end && d.Attrs[j].Atom == nil && d.Attrs[j].Style == at.Style && d.Attrs[j].Link == at.Link {
			j++
		}
		raw := string(d.Runes[i:j])
		i = j
		var lead, core, trail string
		if at.Link != "" {
			if raw == at.Link {
				core = "<" + rtEscape(at.Link) + ">"
			} else {
				core = "<" + rtEscape(at.Link) + "|" + rtEscape(raw) + ">"
			}
		} else {
			txt := rtEscape(raw)
			core = strings.TrimFunc(txt, unicode.IsSpace)
			if core == "" {
				pending += txt
				continue
			}
			lead = txt[:len(txt)-len(strings.TrimLeftFunc(txt, unicode.IsSpace))]
			trail = txt[len(lead)+len(core):]
		}
		want := at.Style & (Bold | Italic | Strike | Code)
		k := len(active)
		for idx, s := range active {
			if want&s == 0 {
				k = idx
				break
			}
		}
		for x := len(active) - 1; x >= k; x-- {
			b.WriteString(rtMarker(active[x]))
		}
		active = active[:k]
		b.WriteString(pending)
		b.WriteString(lead)
		pending = ""
		have := Style(0)
		for _, s := range active {
			have |= s
		}
		for _, s := range []Style{Bold, Italic, Strike, Code} {
			if want&s != 0 && have&s == 0 {
				b.WriteString(rtMarker(s))
				active = append(active, s)
			}
		}
		b.WriteString(core)
		pending = trail
	}
	for x := len(active) - 1; x >= 0; x-- {
		b.WriteString(rtMarker(active[x]))
	}
	b.WriteString(pending)
	return b.String()
}

func rtAtomMrkdwn(a *Atom) string {
	switch a.Kind {
	case AtomUser:
		return "<@" + a.ID + ">"
	case AtomChannel:
		return "<#" + a.ID + ">"
	case AtomUsergroup:
		return "<!subteam^" + a.ID + ">"
	case AtomBroadcast:
		return "<!" + a.ID + ">"
	case AtomEmoji:
		return ":" + a.ID + ":"
	case AtomMessage:
		if a.URL != "" {
			return "<" + rtEscape(a.URL) + ">"
		}
	case AtomLink:
		// the link with its label, as a labelled link writes
		if a.Text == "" || a.Text == a.ID {
			return "<" + rtEscape(a.ID) + ">"
		}
		return "<" + rtEscape(a.ID) + "|" + rtEscape(a.Text) + ">"
	}
	return rtEscape(rtAtomText(a))
}

// ---------------------------------------------------------------------------
// Doc builder shared by the readers.

type rtBuilder struct {
	d     *Doc
	fresh bool // no line started yet: the first line reuses d.Lines[0]
}

func rtNewBuilder() *rtBuilder { return &rtBuilder{d: NewDoc(), fresh: true} }

// line starts a new line with format l.
func (b *rtBuilder) line(l Line) {
	if !rtIsList(l.Kind) || l.Indent < 0 {
		l.Indent = 0
	}
	if b.fresh {
		b.fresh = false
		b.d.Lines[0] = l
		return
	}
	b.d.Runes = append(b.d.Runes, '\n')
	b.d.Attrs = append(b.d.Attrs, Attr{})
	b.d.Lines = append(b.d.Lines, l)
}

// text appends s with attr a; each '\n' continues on a new line of the same
// format.
func (b *rtBuilder) text(s string, a Attr) {
	a.Atom = nil
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for _, r := range s {
		switch r {
		case '\n':
			b.line(b.d.Lines[len(b.d.Lines)-1])
			continue
		case '\r', ObjRune:
			r = ' '
		}
		b.d.Runes = append(b.d.Runes, r)
		b.d.Attrs = append(b.d.Attrs, a)
	}
}

func (b *rtBuilder) atom(at *Atom) {
	b.d.Runes = append(b.d.Runes, ObjRune)
	b.d.Attrs = append(b.d.Attrs, Attr{Atom: at})
}

// blankLines starts n empty lines of format l.
func (b *rtBuilder) blankLines(l Line, n int) {
	for i := 0; i < n; i++ {
		b.line(l)
	}
}

// done trims leading and trailing blank lines and returns the document.
func (b *rtBuilder) done() *Doc {
	d := b.d
	for len(d.Lines) > 1 {
		if k := d.Lines[0].Kind; k == Divider || isHeading(k) {
			break
		}
		end := 0
		for end < len(d.Runes) && d.Runes[end] != '\n' {
			end++
		}
		if !rtBlank(d, rtSpan{start: 0, end: end}) {
			break
		}
		d.Runes = d.Runes[end+1:]
		d.Attrs = d.Attrs[end+1:]
		d.Lines = d.Lines[1:]
	}
	for len(d.Lines) > 1 {
		if k := d.Lines[len(d.Lines)-1].Kind; k == Divider || isHeading(k) {
			break
		}
		start := len(d.Runes)
		for start > 0 && d.Runes[start-1] != '\n' {
			start--
		}
		if !rtBlank(d, rtSpan{start: start, end: len(d.Runes)}) {
			break
		}
		d.Runes = d.Runes[:start-1]
		d.Attrs = d.Attrs[:start-1]
		d.Lines = d.Lines[:len(d.Lines)-1]
	}
	if len(d.Lines) == 1 && rtBlank(d, rtSpan{start: 0, end: len(d.Runes)}) {
		return NewDoc()
	}
	return &Doc{Runes: d.Runes, Attrs: d.Attrs, Lines: d.Lines}
}

func rtOnlyNewlines(s string) bool {
	return s != "" && strings.Trim(s, "\n") == ""
}

func rtEnsurePrefix(s, p string) string {
	if strings.HasPrefix(s, p) {
		return s
	}
	return p + s
}

func rtIsColonName(s string) bool {
	return len(s) > 2 && strings.HasPrefix(s, ":") && strings.HasSuffix(s, ":")
}

// ---------------------------------------------------------------------------
// richtext.Block reader

func rtStyleFromRichtext(s richtext.Style) Style {
	var o Style
	if s&richtext.Bold != 0 {
		o |= Bold
	}
	if s&richtext.Italic != 0 {
		o |= Italic
	}
	if s&richtext.Strike != 0 {
		o |= Strike
	}
	if s&richtext.Code != 0 {
		o |= Code
	}
	if s&richtext.Underline != 0 {
		o |= Underline
	}
	return o
}

// DocFromRichtext converts a rendered message (for editing) into a Doc.
// Paragraph, Section, Header, Quote, Preformatted and List blocks are kept;
// other blocks (images, actions, attachments, files…) are skipped.
func DocFromRichtext(blocks []richtext.Block) *Doc {
	b := rtNewBuilder()
	for _, bl := range blocks {
		switch bl.Kind {
		case richtext.Paragraph, richtext.Section:
			b.rtSpans(Line{Kind: Para}, bl.Spans, false)
		case richtext.Header:
			// richtext.Block carries no heading level; map to Big heading.
			b.rtSpans(Line{Kind: Header1}, bl.Spans, false)
		case richtext.Divider:
			b.line(Line{Kind: Divider})
		case richtext.Quote:
			b.rtSpans(Line{Kind: Quote}, bl.Spans, false)
		case richtext.Preformatted:
			b.rtSpans(Line{Kind: CodeBlock, Quoted: bl.Border}, bl.Spans, true)
		case richtext.List:
			l := Line{Kind: Bullet, Indent: bl.Indent, Quoted: bl.Border}
			if bl.Ordered {
				l.Kind = Ordered
			}
			for _, item := range bl.Items {
				b.rtSpans(l, item, false)
			}
		}
	}
	return b.done()
}

// rtSpans writes one block of spans starting on a new line of format l.
// plain drops styles, links and atoms (code blocks).
func (b *rtBuilder) rtSpans(l Line, spans []richtext.Span, plain bool) {
	nl := 0
	only := len(spans) > 0
	for _, s := range spans {
		switch {
		case s.Kind == richtext.Break:
			nl++
		case s.Kind == richtext.Text && rtOnlyNewlines(s.Text):
			nl += len(s.Text)
		default:
			only = false
		}
	}
	if only {
		b.blankLines(l, nl)
		return
	}
	b.line(l)
	for _, s := range spans {
		st := rtStyleFromRichtext(s.Style)
		if plain {
			st = 0
		}
		switch s.Kind {
		case richtext.Break:
			b.text("\n", Attr{})
		case richtext.Link:
			txt := s.Text
			if txt == "" {
				txt = s.URL
			}
			if s.Truncated && s.URL != "" {
				// a truncated link edits as its link slug (rtTruncatedLink)
				b.rtTruncatedLink(s.URL, txt, st, plain)
				continue
			}
			a := Attr{Style: st}
			if !plain && !(st&Code == 0 && rtIsAutoLink(txt, s.URL)) {
				a.Link = s.URL
			}
			b.text(txt, a)
		case richtext.User, richtext.Channel, richtext.Usergroup, richtext.Broadcast, richtext.Emoji:
			at := rtAtomFromSpan(s)
			if at == nil || plain {
				b.text(s.Text, Attr{Style: st})
				continue
			}
			b.atom(at)
		case richtext.ChannelTab:
			// a channel-tab slug edits as its channel's mention (the tab
			// comes back from the message's blocks, docForMessage)
			if plain || s.ID == "" {
				b.text(s.Text, Attr{Style: st})
				continue
			}
			b.atom(&Atom{Kind: AtomChannel, ID: s.ID, Text: rtEnsurePrefix(firstNonEmpty(s.Chan, s.ID), "#")})
		case richtext.MessageMention:
			// a rendered span keeps only the label, the message ts and the
			// url: the slug comes back from its permalink (the edit
			// composer reads the message's blocks, docForMessage)
			if pl, ok := core.ParsePermalink(s.URL); ok && pl.Kind == "message" && !plain {
				b.atom(&Atom{Kind: AtomMessage, ID: pl.TS, Channel: pl.ChannelID, Thread: pl.ThreadTS, URL: s.URL, Text: s.Text})
				continue
			}
			if s.URL != "" {
				b.text(s.URL, Attr{Style: st}) // the bare url autolinks again
				continue
			}
			b.text(s.Text, Attr{Style: st})
		default: // Text, Date, unknown
			b.text(s.Text, Attr{Style: st})
		}
	}
}

func rtAtomFromSpan(s richtext.Span) *Atom {
	switch s.Kind {
	case richtext.User, richtext.Channel, richtext.Usergroup:
		if s.ID == "" {
			return nil
		}
		k, p := AtomUser, "@"
		if s.Kind == richtext.Channel {
			k, p = AtomChannel, "#"
		} else if s.Kind == richtext.Usergroup {
			k = AtomUsergroup
		}
		txt := s.Text
		if txt == "" {
			txt = s.ID
		}
		return &Atom{Kind: k, ID: s.ID, Text: rtEnsurePrefix(txt, p)}
	case richtext.Broadcast:
		id := s.ID
		if id == "" {
			id = strings.TrimPrefix(s.Text, "@")
		}
		if id == "" {
			return nil
		}
		return &Atom{Kind: AtomBroadcast, ID: id, Text: "@" + id}
	case richtext.Emoji:
		id := s.ID
		if id == "" && rtIsColonName(s.Text) {
			id = s.Text[1 : len(s.Text)-1]
		}
		if id == "" {
			return nil
		}
		at := &Atom{Kind: AtomEmoji, ID: id, Text: ":" + id + ":", URL: s.URL}
		if s.Text != "" && !rtIsColonName(s.Text) {
			at.Uni = s.Text
		}
		return at
	}
	return nil
}

// ---------------------------------------------------------------------------
// rich_text JSON reader

// rtIn is any rich_text node as Slack sends it.
type rtIn struct {
	Type        string          `json:"type"`
	Elements    []rtIn          `json:"elements"`
	Text        json.RawMessage `json:"text"`  // string here; a text object in other blocks
	Style       json.RawMessage `json:"style"` // object for inline, string for lists
	URL         string          `json:"url"`
	UserID      string          `json:"user_id"`
	ChannelID   string          `json:"channel_id"`
	MessageTS   string          `json:"message_ts"` // message_mention
	AuthorID    string          `json:"author_id"`  // message_mention
	ThreadTS    string          `json:"thread_ts"`  // message_mention
	UsergroupID string          `json:"usergroup_id"`
	Name        string          `json:"name"`
	Unicode     string          `json:"unicode"`
	SkinTone    int             `json:"skin_tone"`
	Range       string          `json:"range"`
	Indent      int             `json:"indent"`
	Border      int             `json:"border"` // rich_text_list: a list inside a quote
	Level       int             `json:"level"`  // header block level 1/2/3
	Timestamp   json.RawMessage `json:"timestamp"`
	Fallback    string          `json:"fallback"`
	Value       string          `json:"value"`
	Truncated   bool            `json:"truncated"` // link: text is a link slug's label
}

// rtTruncatedLink writes a link element with truncated: true and a text
// (convertBlocksToDelta, V/2c): it comes back as a link slug labelled
// with that text (formatLinkSlugAttributesForDelta({url, display_url:
// text})), so an edit shows the slug, not the URL. Where atoms can't go
// (plain: a code block) the URL stands in for it, as
// expandTruncatedLinks writes it.
func (b *rtBuilder) rtTruncatedLink(url, text string, st Style, plain bool) {
	if plain {
		b.text(url, Attr{Style: st})
		return
	}
	b.atom(&Atom{Kind: AtomLink, ID: url, URL: url, Text: text})
}

func (e *rtIn) text() string {
	if len(e.Text) == 0 {
		return ""
	}
	var s string
	if e.Text[0] == '"' {
		_ = json.Unmarshal(e.Text, &s)
		return s
	}
	var o struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(e.Text, &o)
	return o.Text
}

func (e *rtIn) styleString() string {
	var s string
	if len(e.Style) > 0 && e.Style[0] == '"' {
		_ = json.Unmarshal(e.Style, &s)
	}
	return s
}

func (e *rtIn) style() Style {
	if len(e.Style) == 0 || e.Style[0] != '{' {
		return 0
	}
	var m map[string]bool
	_ = json.Unmarshal(e.Style, &m)
	var s Style
	if m["bold"] {
		s |= Bold
	}
	if m["italic"] {
		s |= Italic
	}
	if m["strike"] {
		s |= Strike
	}
	if m["code"] {
		s |= Code
	}
	if m["underline"] {
		s |= Underline
	}
	return s
}

// DocFromBlocksJSON parses rich_text blocks (as RichTextBlocks writes them,
// or as Slack sends messages and drafts) into a Doc. r names the atoms and
// may be nil (ids are used). Blocks other than rich_text are skipped.
func DocFromBlocksJSON(raw json.RawMessage, r AtomResolver) (*Doc, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return NewDoc(), nil
	}
	var bs []rtIn
	if raw[0] == '{' {
		var one rtIn
		if err := json.Unmarshal(raw, &one); err != nil {
			return nil, fmt.Errorf("composer: parse blocks: %w", err)
		}
		bs = []rtIn{one}
	} else if err := json.Unmarshal(raw, &bs); err != nil {
		return nil, fmt.Errorf("composer: parse blocks: %w", err)
	}
	b := rtNewBuilder()
	for _, bl := range bs {
		switch bl.Type {
		case "header":
			k := Header1
			switch bl.Level {
			case 2:
				k = Header2
			case 3:
				k = Header3
			}
			b.line(Line{Kind: k})
			b.text(bl.text(), Attr{})
			continue
		case "divider":
			b.line(Line{Kind: Divider})
			continue
		case "rich_text":
			// handled below
		default:
			continue
		}
		for i := range bl.Elements {
			el := &bl.Elements[i]
			switch el.Type {
			case "rich_text_section":
				b.rtJSONInline(Line{Kind: Para}, el.Elements, r, false)
			case "rich_text_quote":
				b.rtJSONInline(Line{Kind: Quote}, el.Elements, r, false)
			case "rich_text_preformatted":
				b.rtJSONInline(Line{Kind: CodeBlock, Quoted: el.Border > 0}, el.Elements, r, true)
			case "rich_text_list":
				l := Line{Kind: Bullet, Indent: el.Indent, Quoted: el.Border > 0}
				if el.styleString() == "ordered" {
					l.Kind = Ordered
				}
				for j := range el.Elements {
					item := &el.Elements[j]
					if item.Type == "rich_text_section" {
						b.rtJSONInline(l, item.Elements, r, false)
					} else {
						b.rtJSONInline(l, []rtIn{*item}, r, false)
					}
				}
			default:
				b.rtJSONInline(Line{Kind: Para}, []rtIn{*el}, r, false)
			}
		}
	}
	return b.done(), nil
}

// rtJSONInline writes one block of inline elements starting on a new line
// of format l. plain drops styles, links and atoms (code blocks).
func (b *rtBuilder) rtJSONInline(l Line, els []rtIn, r AtomResolver, plain bool) {
	nl := 0
	only := len(els) > 0
	for i := range els {
		if t := els[i].text(); els[i].Type == "text" && rtOnlyNewlines(t) {
			nl += len(t)
		} else {
			only = false
		}
	}
	if only {
		b.blankLines(l, nl)
		return
	}
	b.line(l)
	for i := range els {
		e := &els[i]
		st := e.style()
		if plain {
			st = 0
		}
		switch e.Type {
		case "text":
			b.text(e.text(), Attr{Style: st})
		case "link":
			txt := e.text()
			if e.Truncated && txt != "" && e.URL != "" {
				b.rtTruncatedLink(e.URL, txt, st, plain)
				continue
			}
			if txt == "" {
				txt = e.URL
			}
			a := Attr{Style: st}
			if !plain && !(st&Code == 0 && rtIsAutoLink(txt, e.URL)) {
				a.Link = e.URL
			}
			b.text(txt, a)
		case "user", "channel", "usergroup", "broadcast", "emoji", "message_mention":
			at := rtAtomFromJSON(e, r)
			if at == nil {
				continue
			}
			if plain {
				b.text(rtAtomText(at), Attr{})
				continue
			}
			b.atom(at)
		case "date":
			b.text(rtDateText(e), Attr{Style: st})
		case "color", "team":
			b.text(e.Value, Attr{Style: st})
		default:
			b.text(e.text(), Attr{Style: st})
		}
	}
}

func rtDateText(e *rtIn) string {
	if e.Fallback != "" {
		return e.Fallback
	}
	var sec int64
	if json.Unmarshal(e.Timestamp, &sec) != nil {
		var s string
		if json.Unmarshal(e.Timestamp, &s) != nil {
			return ""
		}
		sec, _ = strconv.ParseInt(s, 10, 64)
	}
	if sec <= 0 {
		return ""
	}
	return time.Unix(sec, 0).Format("January 2, 2006")
}

func rtAtomFromJSON(e *rtIn, r AtomResolver) *Atom {
	switch e.Type {
	case "message_mention":
		if e.MessageTS == "" || e.ChannelID == "" {
			return nil
		}
		return &Atom{Kind: AtomMessage, ID: e.MessageTS, Channel: e.ChannelID, Author: e.AuthorID, Thread: e.ThreadTS,
			URL: e.URL, Text: messageAtomLabel(r, e.ChannelID, e.AuthorID, e.URL)}
	case "user":
		if e.UserID == "" {
			return nil
		}
		name := ""
		if r != nil {
			name = r.UserName(e.UserID)
		}
		if name == "" {
			name = e.UserID
		}
		return &Atom{Kind: AtomUser, ID: e.UserID, Text: rtEnsurePrefix(name, "@")}
	case "channel":
		if e.ChannelID == "" {
			return nil
		}
		name := ""
		if r != nil {
			name = r.ChannelName(e.ChannelID)
		}
		if name == "" {
			name = e.ChannelID
		}
		return &Atom{Kind: AtomChannel, ID: e.ChannelID, Text: rtEnsurePrefix(name, "#")}
	case "usergroup":
		if e.UsergroupID == "" {
			return nil
		}
		name := ""
		if r != nil {
			name = r.UsergroupName(e.UsergroupID)
		}
		if name == "" {
			name = e.UsergroupID
		}
		return &Atom{Kind: AtomUsergroup, ID: e.UsergroupID, Text: rtEnsurePrefix(name, "@")}
	case "broadcast":
		if e.Range == "" {
			return nil
		}
		return &Atom{Kind: AtomBroadcast, ID: e.Range, Text: "@" + e.Range}
	case "emoji":
		if e.Name == "" {
			return nil
		}
		id := e.Name
		tone := e.SkinTone
		if tone < 2 {
			// Slack sends a standard emoji's tone only in its unicode
			// (and reads the name back from it)
			tone = rtToneFromUnicode(e.Unicode)
		}
		if tone >= 2 && !strings.Contains(id, "::skin-tone-") {
			id += "::skin-tone-" + strconv.Itoa(tone)
		}
		at := &Atom{Kind: AtomEmoji, ID: id, Text: ":" + id + ":", Uni: rtUnicodeFromHex(e.Unicode)}
		if r != nil {
			url, txt, ok := r.Emoji(id)
			if !ok && id != e.Name {
				url, txt, ok = r.Emoji(e.Name)
			}
			if ok {
				at.URL = url
				if at.Uni == "" && txt != "" && !rtIsColonName(txt) {
					at.Uni = txt
				}
			}
		}
		return at
	}
	return nil
}
