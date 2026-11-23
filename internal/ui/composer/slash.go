package composer

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/some-du6e/clack/internal/core"
)

// Slack's slash-command path, ported on the composer's Quill view of the
// document (mdconvert.go's qDelta), from build 9f450b8ad3db (2026-11-22).
// Slack renumbers its modules each build, so find them by the names below.
//
//  1. prepareAndSendMessage (boot-shared s0Qa) first runs the message
//     processor (rNCs) over the delta: the smart-symbol pass (nC51: a
//     text presentation selector after a bare "↔"), the mention
//     auto-slugging (tEEs replacePotentialMentionsSynchronously,
//     slash_mentions.go), then linkifyDeltaText (l8g0) with
//     {limitCodeBlockLinksToFullURLs, ignoreExistingLinks} (linkifyQ).
//  2. After the speed bumps and the broadcast keyword warning, a send that
//     isn't scheduled reaches handleSlashCommand (vHCW) with that processed
//     delta: the slash test (s4/N, looksLikeSlashCommand in edit_last.go),
//     the "/s" edit (edit_last.go), then parseSlashCmdArgsFromDelta
//     (vHCW's C, parseSlashArgs): the tokens (a+H5
//     convertDeltaToSlashCommandParams, slashTokens), the first one, a
//     word, as the command (lowercased, ’ as '), the others as userParams,
//     and deltaWithoutCmd: the delta after the command word, its leading
//     whitespace trimmed (OQBl trimDelta {trimEnd: false}).
//  3. executeCommand (boot-render pymN): "/remind" without params opens
//     the reminder modal (E8e8, isMsgContext false); a local command
//     (Zrd3 getCommands) gets the parsed args; anything else goes to
//     chat.command with blocks = convertDeltaToBlocks(deltaWithoutCmd,
//     {expandTruncatedLinks: true}) (JQxa, commandBlocks) and no text.

// slashArgs is parseSlashCmdArgsFromDelta's result.
type slashArgs struct {
	Cmd    string              // the command word, lowercased, ’ as '
	Disp   string              // the command word as typed
	Params []core.CommandParam // userParams: every token after it
	Rest   qDelta              // deltaWithoutCmd
}

// parseSlashArgs is parseSlashCmdArgsFromDelta (vHCW C) on the processed
// delta. A delta whose first token isn't a word gives the zero args.
func parseSlashArgs(q qDelta) slashArgs {
	toks := slashTokens(q)
	if len(toks) == 0 || toks[0].Type != "string" || toks[0].Value == "" {
		return slashArgs{}
	}
	disp := toks[0].Value
	cmd := strings.ReplaceAll(jsToLower(disp), string(rune(0x2019)), "'")
	rest := q.compose(qDelta{}.delete(qRunesForUnits(q, jsLen(cmd))))
	return slashArgs{Cmd: cmd, Disp: disp, Params: toks[1:], Rest: qDelta{ops: qTrim(rest.ops, true, false)}}
}

// slashTokens is convertDeltaToSlashCommandParams (a+H5): an emoji is a
// word of its text, another embed nothing, a mention (not a broadcast
// keyword, not UNVERIFIED) its id, and any other text its words (split on
// JavaScript's \s).
func slashTokens(q qDelta) []core.CommandParam {
	var out []core.CommandParam
	for _, op := range qMergeMentions(q.ops) {
		switch op.k {
		case qEmb:
			if op.emb != nil && op.emb.atom != nil && op.emb.atom.Kind == AtomEmoji {
				out = append(out, core.CommandParam{Type: "string", Value: qEmojiText(op.emb.atom)})
			}
		case qIns:
			if id, ok := qMentionID(op); ok && id != "UNVERIFIED" && !slackKeywordID(id) {
				out = append(out, core.CommandParam{Type: "mention", Value: id})
				continue
			}
			for _, w := range strings.FieldsFunc(string(op.text), jsSpace) {
				out = append(out, core.CommandParam{Type: "string", Value: w})
			}
		}
	}
	return out
}

// qMergeMentions joins neighbouring ops of one mention (the same id,
// label and formats): Quill holds equal neighbours as one op ("@a@a"), so
// convertDeltaToSlashCommandParams reads two chips in a row as one token.
func qMergeMentions(ops []qOp) []qOp {
	var out []qOp
	for _, op := range ops {
		if n := len(out); n > 0 && qSameMention(out[n-1], op) {
			m := out[n-1]
			m.text = append(append([]rune(nil), m.text...), op.text...)
			out[n-1] = m
			continue
		}
		out = append(out, op)
	}
	return out
}

// qSameMention: a and b are mention ops Quill's attributes would call
// equal.
func qSameMention(a, b qOp) bool {
	if a.k != qIns || b.k != qIns || len(a.attrs) != len(b.attrs) {
		return false
	}
	ra, ok := a.attrs["slackmention"].(qAtomRef)
	rb, ok2 := b.attrs["slackmention"].(qAtomRef)
	if !ok || !ok2 || ra.a == nil || rb.a == nil || ra.a.Kind != rb.a.Kind || ra.a.ID != rb.a.ID || ra.a.Text != rb.a.Text {
		return false
	}
	for k, v := range a.attrs {
		if w, ok := b.attrs[k]; k != "slackmention" && (!ok || w != v) {
			return false
		}
	}
	return true
}

// qMentionID is getMentionOpIdText for a mention op (Slack's slackmention
// over the label): the atom's Slack id ("BKhere" for @here).
func qMentionID(op qOp) (string, bool) {
	if op.k != qIns {
		return "", false
	}
	ref, ok := op.attrs["slackmention"].(qAtomRef)
	if !ok || ref.a == nil {
		return "", false
	}
	if ref.a.Kind == AtomBroadcast {
		return broadcastRowID(ref.a.ID), true
	}
	return ref.a.ID, true
}

// slackKeywordID is mentionIdIsKeyword (CrkS over nPsK's list).
func slackKeywordID(id string) bool {
	for _, k := range broadcastKeywords {
		if id == k {
			return true
		}
	}
	return false
}

// qEmojiText is an emoji embed's slackemoji.text (":wave::skin-tone-3:").
func qEmojiText(a *Atom) string {
	if a.Text != "" {
		return a.Text
	}
	return ":" + a.ID + ":"
}

// qRunesForUnits is how many of q's first characters (runes; an embed is
// one) make n UTF-16 code units: Delta.delete counts JavaScript's
// string length. A count that ends inside a surrogate pair takes the pair.
func qRunesForUnits(q qDelta, n int) int {
	runes, units := 0, 0
	for _, op := range q.ops {
		if units >= n {
			break
		}
		switch op.k {
		case qEmb:
			runes++
			units++
		case qIns:
			for _, r := range op.text {
				if units >= n {
					break
				}
				runes++
				units += max(utf16.RuneLen(r), 1)
			}
		default:
			return runes
		}
	}
	return runes
}

// jsToLower is String.prototype.toLowerCase: Unicode's full lowercase
// mapping, where Go's simple one differs for İ (U+0130 → "i̇") and a
// final Σ (ς at the end of a word).
func jsToLower(s string) string {
	if !strings.ContainsRune(s, 0x130) && !strings.ContainsRune(s, 0x3a3) {
		return strings.ToLower(s)
	}
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		switch r {
		case 0x130: // capital I with dot above: i and a combining dot
			b.WriteRune('i')
			b.WriteRune(0x307)
		case 0x3a3: // capital sigma: final or not
			if jsFinalSigma(rs, i) {
				b.WriteRune(0x3c2)
			} else {
				b.WriteRune(0x3c3)
			}
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// jsFinalSigma is Unicode's Final_Sigma condition at i: a cased letter
// before (case-ignorable characters between) and none after.
func jsFinalSigma(rs []rune, i int) bool {
	before := false
	for j := i - 1; j >= 0; j-- {
		if jsCaseIgnorable(rs[j]) {
			continue
		}
		before = jsCased(rs[j])
		break
	}
	if !before {
		return false
	}
	for j := i + 1; j < len(rs); j++ {
		if jsCaseIgnorable(rs[j]) {
			continue
		}
		return !jsCased(rs[j])
	}
	return true
}

func jsCased(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) ||
		unicode.Is(unicode.Other_Lowercase, r) || unicode.Is(unicode.Other_Uppercase, r)
}

func jsCaseIgnorable(r rune) bool {
	switch r {
	case '\'', '.', ':', '^', '`', 0xad, 0xb7, 0x387, 0x55f, 0x5f4, 0x2018, 0x2019, 0x2024, 0x2027,
		0xfe13, 0xfe52, 0xfe55, 0xff07, 0xff0e, 0xff1a:
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk)
}

// ---- convertDeltaToBlocks (JQxa) ----

// Slack's rich text element types (N8wa Lv).
const (
	rtSection = "rich_text_section"
	rtList    = "rich_text_list"
	rtPre     = "rich_text_preformatted"
	rtQuote   = "rich_text_quote"
	rtHeader  = "rich_text_header"
	rtDivider = "rich_text_divider"
)

// cbOpts are createRichTextChunkElements' options.
type cbOpts struct {
	strip, pre, expand bool
}

// commandBlocks is convertDeltaToBlocks (JQxa) with executeCommand's
// options, {expandTruncatedLinks: true} and the defaults (trimStart, no
// trimEnd, convertEmpty false, no expanded rich text, sections not split
// on newlines, rich text headers and dividers): one rich_text block
// without a block_id, or nil when the delta is one character or less once
// its leading whitespace is trimmed (Slack sends no blocks then).
func commandBlocks(rest qDelta) json.RawMessage { return convertBlocks(rest, true) }

// convertBlocks is convertDeltaToBlocks with the defaults and
// expandTruncatedLinks as given.
func convertBlocks(rest qDelta, expand bool) json.RawMessage {
	d := qDelta{ops: qTrim(rest.ops, true, false)}
	if !qEndsInNewline(d) {
		d = d.insert("\n", nil)
	}
	if d.length() <= 1 {
		return nil
	}
	els := []any{}
	var group qDelta
	typ := rtSection
	qEachOpWithLine(d, func(op qOp, line qAttrs) {
		if t := qElementType(op, line); t != typ {
			els = cbAddGroup(els, typ, group, false, expand)
			typ, group = t, qDelta{}
		}
		if op.k == qEmb || op.k == qIns && len(op.text) > 0 {
			group.push(op)
		}
	})
	els = cbAddGroup(els, typ, group, true, expand)
	return rtMarshal([]any{rtOutCommandBlock{Type: "rich_text", Elements: els}})
}

// qEndsInNewline is hasTrailingNewline (dndK): the last character is a
// "\n" in a text op.
func qEndsInNewline(d qDelta) bool {
	n := len(d.ops)
	if n == 0 {
		return false
	}
	last := d.ops[n-1]
	return qTextItem(last) && len(last.text) > 0 && last.text[len(last.text)-1] == '\n'
}

// qEachOpWithLine is deltaForEachWithLineData (e0ER): each line's ops,
// with its "\n" (inserted, so it merges with a last op of the same
// formats), and the line's format.
func qEachOpWithLine(d qDelta, fn func(op qOp, line qAttrs)) {
	d.eachLine(func(line qDelta, at qAttrs) {
		line = line.insert("\n", at)
		for _, op := range line.ops {
			fn(op, at)
		}
	})
}

// qIsDivider is isOpHorizontalRule (Ov5q).
func qIsDivider(op qOp) bool { return op.k == qEmb && op.emb != nil && op.emb.hr }

// qListKind is isOpListItem's (XuSF) list formats.
func qListKind(v any) bool {
	switch v {
	case "bullet", "ordered", "unchecked", "checked":
		return true
	}
	return false
}

// qElementType is getElementType: by the op (a divider) or its line's
// format.
func qElementType(op qOp, line qAttrs) string {
	switch {
	case qIsDivider(op):
		return rtDivider
	case truthy(line["code-block"]):
		return rtPre
	case truthy(line["blockquote"]):
		return rtQuote
	case qListKind(line["list"]):
		return rtList
	case truthy(line["header"]):
		return rtHeader
	}
	return rtSection
}

type cbSection struct {
	Type     string `json:"type"`
	Elements []any  `json:"elements"`
}

type cbList struct {
	Type     string `json:"type"`
	Style    string `json:"style"`
	Indent   int    `json:"indent"`
	Border   int    `json:"border"`
	Elements []any  `json:"elements"`
	Offset   int    `json:"offset,omitempty"`
}

type cbPre struct {
	Type     string `json:"type"`
	Border   int    `json:"border"`
	Elements []any  `json:"elements"`
}

type cbQuote struct {
	Type     string `json:"type"`
	Border   int    `json:"border,omitempty"`
	Elements []any  `json:"elements"`
}

type cbHeader struct {
	Type     string `json:"type"`
	Level    int    `json:"level"`
	Elements []any  `json:"elements"`
}

type cbDivider struct {
	Type string `json:"type"`
}

// cbAddGroup is addDeltaToElements: one run of ops of a kind as its
// elements. Only the last run strips its trailing whitespace (code and
// quotes always do).
func cbAddGroup(els []any, typ string, a qDelta, strip, expand bool) []any {
	if len(a.ops) == 0 {
		return els
	}
	switch typ {
	case rtList:
		return append(els, cbListElements(a, expand)...)
	case rtPre:
		border, any := 1, false
		for _, op := range a.ops {
			if op.k == qIns && truthy(op.attrs["code-block"]) {
				any = true
				if attrInt(op.attrs["border"]) < 1 {
					border = 0
				}
			}
		}
		if !any {
			border = 0
		}
		return append(els, cbPre{Type: rtPre, Border: border, Elements: cbChunks(a, cbOpts{strip: true, pre: true, expand: expand})})
	case rtQuote:
		bordered := false
		for _, op := range a.ops {
			if op.k == qIns && truthy(op.attrs["blockquote"]) && attrInt(op.attrs["border"]) >= 1 {
				bordered = true
			}
		}
		if !bordered {
			return append(els, cbQuote{Type: rtQuote, Elements: cbChunks(a, cbOpts{strip: true, expand: expand})})
		}
		a.eachLine(func(line qDelta, at qAttrs) {
			els = append(els, cbQuote{Type: rtQuote, Border: attrInt(at["border"]), Elements: cbChunks(line, cbOpts{strip: true, expand: expand})})
		})
		return els
	case rtHeader:
		return append(els, cbHeaderElements(a)...)
	case rtDivider:
		for _, op := range a.ops {
			if qIsDivider(op) {
				els = append(els, cbDivider{Type: rtDivider})
			}
		}
		return els
	}
	return append(els, cbSection{Type: typ, Elements: cbChunks(a, cbOpts{strip: strip, expand: expand})})
}

// cbListElements is processDeltaAsList without splitListsOnQuoteChanges
// or restartNumberingOnListStyleChange: a list per run of lines of one
// style and indent, an ordered one's offset counting the items before it
// at that indent.
func cbListElements(e qDelta, expand bool) []any {
	var out []any
	var style string
	first := true
	indent, border := 0, 0
	counts := map[int]int{}
	var items [][]any
	flush := func(final bool) {
		l := cbList{Type: rtList, Style: style, Indent: indent, Border: border, Elements: make([]any, 0, len(items))}
		if l.Style == "" {
			l.Style = "bullet"
		}
		for _, it := range items {
			l.Elements = append(l.Elements, cbSection{Type: rtSection, Elements: it})
		}
		if style == "ordered" && counts[indent] > 0 {
			l.Offset = counts[indent]
		}
		out = append(out, l)
	}
	e.eachLine(func(line qDelta, at qAttrs) {
		ls, _ := at["list"].(string)
		li, lb := attrInt(at["indent"]), attrInt(at["border"])
		if first {
			style, indent, border, first = ls, li, lb, false
		}
		if _, ok := counts[indent]; !ok {
			counts[indent] = 0
		}
		if style != ls || indent != li {
			flush(false)
			if !qListKind(ls) {
				counts = map[int]int{}
			} else {
				if indent < li && ls == "ordered" {
					counts[li] = 0
				}
				if style == "ordered" {
					counts[indent] += len(items)
				}
			}
			style, indent, border, items = ls, li, lb, nil
		}
		items = append(items, cbChunks(line, cbOpts{expand: expand}))
	})
	flush(true)
	return out
}

// cbHeaderElements is processDeltaAsHeader: a rich_text_header per line,
// its level clamped to 1-3 (2 when unset); headers keep link slugs'
// labels (no expandTruncatedLinks).
func cbHeaderElements(e qDelta) []any {
	var out []any
	var level any
	started := false
	lines := 0
	var cur []any
	e.eachLine(func(line qDelta, at qAttrs) {
		h := at["header"]
		if !started {
			level, started = h, true
		}
		if level != h || lines > 0 && truthy(h) {
			out = append(out, cbHeader{Type: rtHeader, Level: cbLevel(level), Elements: cur})
			cur, level = nil, h
		}
		cur = cbChunks(line, cbOpts{})
		lines++
	})
	return append(out, cbHeader{Type: rtHeader, Level: cbLevel(level), Elements: cur})
}

// cbLevel is N8wa Zi: min(max(level || 2, 1), 3).
func cbLevel(v any) int {
	n := attrInt(v)
	if n == 0 {
		n = 2
	}
	return min(max(n, 1), 3)
}

// cbSpaceBeforeNewline is K, /(^|\S)( +)(\n)/gm, replaced with "$1$3":
// the spaces before a line break go, after text or at a line's start.
func cbSpaceBeforeNewline(rs []rune) []rune {
	out := make([]rune, 0, len(rs))
	for i := 0; i < len(rs); i++ {
		if rs[i] != '\n' {
			out = append(out, rs[i])
			continue
		}
		j := len(out)
		for j > 0 && out[j-1] == ' ' {
			j--
		}
		if j < len(out) && (j == 0 || jsLineTerm(out[j-1]) || !jsSpace(out[j-1])) {
			out = out[:j]
		}
		out = append(out, '\n')
	}
	return out
}

// cbColorRe is G: a "#" and a name after a start, a space or punctuation.
var cbColorRe = regexp.MustCompile(`(^|[` + jsS[1:len(jsS)-1] + `{\[(><*_/"\x{201C}\x{2018}',])#([^~` + "`" + `!@#$%^&*()+=\[\]{}\\|;:'"\x{201D}\x{2019},.<>/?` + jsS[1:len(jsS)-1] + `]+)`)

// cbHex6 is Q, /^[0-9a-fA-F]{6}$/.
var cbHex6 = regexp.MustCompile(`^[0-9a-fA-F]{6}$`)

type cbStyle struct {
	Bold      bool `json:"bold,omitempty"`
	Italic    bool `json:"italic,omitempty"`
	Underline bool `json:"underline,omitempty"`
	Strike    bool `json:"strike,omitempty"`
	Code      bool `json:"code,omitempty"`
	Unlink    bool `json:"unlink,omitempty"`
}

// cbStyleOf is annotateChunkWithStyle's style: the op's inline formats,
// unlink too on anything but a mention or an emoji.
func cbStyleOf(op qOp) *cbStyle {
	at := op.attrs
	s := cbStyle{Bold: truthy(at["bold"]), Italic: truthy(at["italic"]), Underline: truthy(at["underline"]),
		Strike: truthy(at["strike"]), Code: truthy(at["code"])}
	if truthy(at["unlink"]) && op.k == qIns {
		if _, m := at["slackmention"]; !m {
			s.Unlink = true
		}
	}
	if s == (cbStyle{}) {
		return nil
	}
	return &s
}

type cbText struct {
	Type  string   `json:"type"`
	Text  string   `json:"text"`
	Style *cbStyle `json:"style,omitempty"`
}

type cbColor struct {
	Type  string   `json:"type"`
	Value string   `json:"value"`
	Style *cbStyle `json:"style,omitempty"`
}

type cbLink struct {
	Type  string   `json:"type"`
	URL   string   `json:"url"`
	Text  string   `json:"text,omitempty"`
	Style *cbStyle `json:"style,omitempty"`
}

// cbChunks is createRichTextChunkElements: the inline elements of a run,
// neighbouring unstyled text merged.
func cbChunks(e qDelta, o cbOpts) []any {
	out := []any{}
	ops := e.ops
	lastIdx := len(ops) - 1
	if o.strip && lastIdx >= 0 && ops[lastIdx].k == qIns && string(ops[lastIdx].text) == "\n" {
		ops = ops[:lastIdx] // the run's last op, a bare "\n", goes (and nothing else is trimmed)
	}
	push := func(cs ...any) {
		for i, c := range cs {
			if t, ok := c.(cbText); ok && i == 0 && t.Style == nil && len(out) > 0 {
				if p, ok := out[len(out)-1].(cbText); ok && p.Style == nil {
					p.Text += t.Text
					out[len(out)-1] = p
					continue
				}
			}
			out = append(out, c)
		}
	}
	for i, op := range ops {
		switch {
		case op.k == qEmb && op.emb != nil && op.emb.atom != nil && op.emb.atom.Kind == AtomEmoji:
			if o.pre || truthy(op.attrs["code"]) {
				// createTextChunkFromOp({insert: its text}): no formats
				push(cbTextChunk(qEmojiText(op.emb.atom), qOp{k: qIns}, false, false, true)...)
				continue
			}
			push(rtAtomOut(op.emb.atom))
		case op.k == qEmb:
			// a divider in a run of text: nothing (isOpEmbed, not a chunk)
		case op.k != qIns:
		default:
			if id, ok := qMentionID(op); ok {
				if id == "" {
					push(cbTextChunk(string(op.text), op, false, false, true)...)
					continue
				}
				if id != "UNVERIFIED" {
					push(cbMentionChunks(op, o)...)
					continue
				}
			}
			if ref, ok := op.attrs["slackslug"].(qAtomRef); ok && ref.a != nil {
				push(cbSlugChunk(ref.a, op, o))
				continue
			}
			if l, ok := op.attrs["link"].(string); ok && l != "" {
				push(cbLinkChunk(op, l, o))
				continue
			}
			push(cbTextChunk(string(op.text), op, o.strip && i == lastIdx, o.pre, false)...)
		}
	}
	return out
}

// cbTextChunk is createTextChunkFromOp: the text (trimmed at the end for
// the run's last op, spaces before line breaks gone outside code), with
// "#RRGGBB" names as color elements unless conversions are skipped.
func cbTextChunk(s string, op qOp, strip, pre, skip bool) []any {
	rs := []rune(s)
	if strip {
		j := len(rs)
		for j > 0 && jsSpace(rs[j-1]) {
			j--
		}
		rs = rs[:j]
	}
	if !pre {
		rs = cbSpaceBeforeNewline(rs)
	}
	if len(rs) == 0 {
		return nil
	}
	var st *cbStyle
	if !pre {
		st = cbStyleOf(op)
	}
	text := string(rs)
	if skip || pre || truthy(op.attrs["code"]) {
		return []any{cbText{Type: "text", Text: text, Style: st}}
	}
	type span struct {
		start, end int
		color      string
	}
	var spans []span
	for _, m := range cbColorRe.FindAllStringSubmatchIndex(text, -1) {
		name := text[m[4]:m[5]]
		trimmed := strings.TrimRight(name, "_-."+string(rune(0x2026)))
		if !cbHex6.MatchString(trimmed) {
			continue
		}
		start := m[2] + (m[3] - m[2]) // after the group before "#", in bytes
		spans = append(spans, span{start: start, end: start + len(trimmed) + 1, color: "#" + strings.ToUpper(trimmed)})
	}
	if len(spans) == 0 {
		return []any{cbText{Type: "text", Text: text, Style: st}}
	}
	var out []any
	at := 0
	for _, sp := range spans {
		if at < sp.start {
			out = append(out, cbText{Type: "text", Text: text[at:sp.start], Style: st})
		}
		out = append(out, cbColor{Type: "color", Value: sp.color, Style: st})
		at = sp.end
	}
	if at < len(text) {
		out = append(out, cbText{Type: "text", Text: text[at:], Style: st})
	}
	return out
}

// cbLinkChunk is createLinkChunkFromOp: the link, with its text unless
// that is the URL (or, expanding, a truncated "a[…]b" label).
func cbLinkChunk(op qOp, link string, o cbOpts) any {
	l := cbLink{Type: "link", URL: link}
	if t := string(op.text); t != link && !(o.expand && cbTruncatedLink(t, link)) {
		l.Text = t
	}
	if !o.pre {
		l.Style = cbStyleOf(op)
	}
	return l
}

// cbEllipsis is a truncated link label's "[…]".
var cbEllipsis = "[" + string(rune(0x2026)) + "]"

// cbTruncatedLink is isOpTruncatedLink (vmJG uK): one "[…]" inside the
// label, the link starting with what is before it and holding what is
// after it.
func cbTruncatedLink(label, link string) bool {
	parts := strings.Split(label, cbEllipsis)
	if len(parts) != 2 {
		return false
	}
	i := strings.Index(label, cbEllipsis)
	if i == 0 || jsLen(label[:i]) == jsLen(label)-3 {
		return false
	}
	return strings.HasPrefix(link, parts[0]) && (parts[1] == "" || strings.Contains(link[len(parts[0]):], parts[1]))
}

type cbUser struct {
	Type   string   `json:"type"`
	UserID string   `json:"user_id"`
	Style  *cbStyle `json:"style,omitempty"`
}

type cbUsergroup struct {
	Type        string   `json:"type"`
	UsergroupID string   `json:"usergroup_id"`
	Style       *cbStyle `json:"style,omitempty"`
}

type cbChannel struct {
	Type      string   `json:"type"`
	ChannelID string   `json:"channel_id"`
	Style     *cbStyle `json:"style,omitempty"`
}

type cbBroadcast struct {
	Type  string   `json:"type"`
	Range string   `json:"range"`
	Style *cbStyle `json:"style,omitempty"`
}

type cbFile struct {
	Type   string   `json:"type"`
	FileID string   `json:"file_id"`
	Style  *cbStyle `json:"style,omitempty"`
}

// looksLikeChannelID is nQiy, /^(C|G|D|Z)[A-Z0-9]{8,}$/.
var looksLikeChannelID = regexp.MustCompile(`^[CGDZ][A-Z0-9]{8,}$`).MatchString

// looksLikeFileID is the file mention test (F…).
var looksLikeFileID = regexp.MustCompile(`^F[A-Z0-9]{8,}$`).MatchString

// cbMentionChunks is createMentionChunksFromOp: a broadcast keyword, a
// user group (S…), a channel, a file or else a user; in code, its label
// as text.
func cbMentionChunks(op qOp, o cbOpts) []any {
	id, _ := qMentionID(op)
	label := string(op.text) // getMentionOpLabelText: the atom's text (its label as inserted)
	if ref, ok := op.attrs["slackmention"].(qAtomRef); ok && ref.a != nil && ref.a.Text != "" {
		label = ref.a.Text
	}
	if o.pre || truthy(op.attrs["code"]) {
		return cbTextChunk(string(op.text), op, false, false, true)
	}
	st := cbStyleOf(op)
	var n any
	switch {
	case slackKeywordID(id):
		n = cbBroadcast{Type: "broadcast", Range: id[2:], Style: st}
	case strings.HasPrefix(id, "S"):
		n = cbUsergroup{Type: "usergroup", UsergroupID: id, Style: st}
	case looksLikeChannelID(id):
		n = cbChannel{Type: "channel", ChannelID: id, Style: st}
	case looksLikeFileID(id):
		n = cbFile{Type: "file", FileID: id, Style: st}
	default:
		n = cbUser{Type: "user", UserID: id, Style: st}
	}
	text := string(op.text)
	if label != "" && jsLen(text) > jsLen(label) {
		parts := strings.Split(text, label)
		if len(parts) > 1 {
			out := make([]any, len(parts)-1)
			for i := range out {
				out[i] = n
			}
			return out
		}
		return []any{n}
	}
	if text == label {
		return []any{n}
	}
	return cbTextChunk(text, op, false, false, true)
}

// cbSlugChunk is createSlugChunkFromOp for the composer's slugs: a
// message slug is its message_mention; a link slug its link, bare when
// expanding (no text, no truncated).
func cbSlugChunk(a *Atom, op qOp, o cbOpts) any {
	switch a.Kind {
	case AtomLink:
		l := rtOutLink{Type: "link", URL: a.ID}
		if !o.expand {
			if a.Text != a.ID {
				l.Text = a.Text
			}
			if a.Text != a.ID {
				l.Truncated = true
			}
		}
		if !o.pre {
			if st := cbStyleOf(op); st != nil {
				return struct {
					rtOutLink
					Style *cbStyle `json:"style,omitempty"`
				}{l, st}
			}
		}
		return l
	}
	return rtAtomOut(a)
}

// ---- linkifyDeltaText (l8g0) ----

// linkifyQ is linkifyDeltaText with prepareAndSendMessage's
// {limitCodeBlockLinksToFullURLs, ignoreExistingLinks}: in every text op
// that isn't a link already (nor unlinked), the links detectLinks (nVll)
// finds become link ops; in a code block only full http(s) URLs, no
// e-mail addresses or phone numbers. A delta without a trailing newline
// gets none back.
func linkifyQ(q qDelta) qDelta {
	had := qEndsInNewline(q)
	var out qDelta
	qEachOpWithLine(q, func(op qOp, line qAttrs) {
		if !qTextItem(op) || truthy(op.attrs["link"]) || truthy(op.attrs["unlink"]) {
			out.push(op)
			return
		}
		code := truthy(line["code-block"])
		text := op.text
		ms := detectLinksOpts(string(text), linkOpts{noPhones: code, noEmails: code, httpOnly: code})
		if len(ms) == 0 {
			out.push(op)
			return
		}
		f := 0
		for _, m := range ms {
			if m.at > f {
				out = out.insertRunes(text[f:m.at], op.attrs.clone())
				f = m.at
			}
			if m.url != "" && m.text != "" {
				at := op.attrs.clone()
				if at == nil {
					at = qAttrs{}
				}
				at["link"] = m.url
				out = out.insertRunes([]rune(m.text), at)
				f = m.at + len([]rune(m.text))
			}
		}
		if f < len(text) {
			out = out.insertRunes(text[f:], op.attrs.clone())
		}
	})
	if !had {
		out = qStripTrailingNewline(out)
	}
	return out
}

// qStripTrailingNewline is stripTrailingNewline (CnpR): a last "\n" goes
// unless it carries a code block, quote or list.
func qStripTrailingNewline(d qDelta) qDelta {
	n := d.length()
	if n == 0 {
		return d
	}
	last := d.slice(n-1, n)
	if len(last.ops) == 0 {
		return d
	}
	op := last.ops[len(last.ops)-1]
	if op.k != qIns || string(op.text) != "\n" || qProtectedBlock(op.attrs) {
		return d
	}
	return d.slice(0, n-1)
}
