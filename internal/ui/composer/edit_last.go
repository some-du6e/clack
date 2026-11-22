package composer

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/taut"
)

// "/s/old/new": the edit-last-message command, ported from Slack's
// handleSlashCommand (gantry-v2-async-client-boot-shared vHCW, build
// a09b625d90aa; Slack renumbers module ids each build, so find it by the
// names below):
//
//  1. Where: prepareAndSendMessage (s0Qa) calls handleSlashCommand for
//     every send that isn't scheduled and doesn't skip interceptors, so in
//     channels, DMs, group DMs and threads alike. Once the delta looks like
//     a slash command (s4/N: its first line has no line format and its
//     first op is text starting "/" or "／" and then neither a slash nor
//     a space), parseEditLastMessageCommand runs before anything else: a
//     match never reaches parseSlashCmdArgsFromDelta, the earlier command
//     feedback stays and nothing goes to chat.command. There is no gate
//     (no experiment or pref).
//  2. parseEditLastMessageCommand: the delta trimmed at the end (trimDelta
//     with trimStart false) must be exactly one plain text op (no
//     formatting split, no mention or slug), its text, with every "／"
//     made "/", matching /^\/s\/([^/]+)\/([^/]*)(?:\/([a-z]+)?)?$/i.
//     replaceAll is a "g" among the flags and ignoreCase an "i"; both are
//     case-sensitive (lodash includes) and other letters are ignored.
//  3. editLastUserMessageText: the message is getLastEditableMessage
//     (qszY W4): the viewer's newest loaded message in the conversation or
//     thread, a plain, me_message or thread_broadcast one, only if
//     canEditMessage allows it; an older one is never tried instead. No
//     message, or one without blocks, rejects "Message not found".
//  4. The message's blocks become a delta (convertBlocksToDelta, rich_text
//     blocks only) and each plain text op (formatted text and links
//     included, mentions, slugs and emoji not) gets
//     text.replace(RegExp(`(^|W+)(?:escaped old)(?=W+|$)`, flags), `$1new`)
//     with W = [\s\n\r\t.,'"+!?\-_|] and flags "i" and/or "g": old is a
//     literal matched as a whole word, inside one op only, and new is a
//     replacement template ($&, $1, $$ … work). Without g only the first
//     op that changes is edited. The op keeps its attributes, so bold,
//     links and mentions survive.
//  5. Nothing changed: the promise resolves and nothing else happens. The
//     edit trimmed to nothing rejects "No message text". Otherwise
//     saveMessageEdit (IHC0) saves it the way the edit composer does:
//     chat.update with the blocks, "(edited)" from the server.
//  6. sendDraftMessage (4Nr8) cleared the draft before sending; a
//     rejection puts it back (restoreDraftOnFailure) and is only logged
//     (logSendFailures): no toast, no ephemeral.

// editLastCmd is parseEditLastMessageCommand's result.
type editLastCmd struct {
	Prev, Next             string
	ReplaceAll, IgnoreCase bool
}

// editLastRe is Slack's f, /^\/s\/([^/]+)\/([^/]*)(?:\/([a-z]+)?)?$/i.
// The flags group is spelled out: Go's (?i) would also let [a-z] match
// U+017F and U+212A, which JavaScript's non-unicode /i doesn't.
var editLastRe = regexp.MustCompile(`^/[sS]/([^/]+)/([^/]*)(?:/([a-zA-Z]+)?)?$`)

// fullSlash is U+FF0F, the full-width solidus Slack reads as "/".
const fullSlash = 0xff0f

var errEditLastNotFound = errors.New("Message not found")
var errEditLastNoText = errors.New("No message text")

// qTextItem is iRqW isOpTextItem: a string insert that isn't a mention or
// a slug (bZFu isOpSlugOrMention).
func qTextItem(op qOp) bool {
	if op.k != qIns {
		return false
	}
	_, m := op.attrs["slackmention"]
	_, s := op.attrs["slackslug"]
	return !m && !s
}

// qProtectedBlock is OQBl's attributesAreProtectedBlockFormat (header
// newlines not preserved): uAuK doesAttributesContainBlockFormatting.
func qProtectedBlock(at qAttrs) bool {
	for _, k := range [...]string{"code-block", "blockquote", "list"} {
		if _, ok := at[k]; ok {
			return true
		}
	}
	return false
}

func qBlankText(op qOp) bool {
	for _, r := range op.text {
		if !jsSpace(r) {
			return false
		}
	}
	return true
}

// qTrim is OQBl trimDelta over a document's ops: whitespace-only text ops
// go from the ends, then the outer ops lose their leading and trailing
// whitespace, except inside lists, quotes and code blocks.
func qTrim(ops []qOp, start, end bool) []qOp {
	m := make([]qOp, len(ops))
	copy(m, ops)
	drop := func(op qOp) bool { return qTextItem(op) && qBlankText(op) && !qProtectedBlock(op.attrs) }
	if start {
		for len(m) > 0 && drop(m[0]) {
			m = m[1:]
		}
		// getAttributesAtIndex(0): the first op's and its line's
		if len(m) > 0 && qTextItem(m[0]) && !qProtectedBlock(qFirstLineAttrs(m)) {
			op := m[0]
			op.text = []rune(strings.TrimLeftFunc(string(op.text), jsSpace))
			m[0] = op
		}
	}
	if end {
		for len(m) > 0 && drop(m[len(m)-1]) {
			m = m[:len(m)-1]
		}
		if n := len(m); n > 0 && qTextItem(m[n-1]) && !qProtectedBlock(m[n-1].attrs) {
			op := m[n-1]
			op.text = []rune(strings.TrimRightFunc(string(op.text), jsSpace))
			m[n-1] = op
		}
	}
	return m
}

// qFirstLineAttrs is 0523's attributes at index 0: the first op's own
// with its line's (the op holding the first "\n") over them.
func qFirstLineAttrs(ops []qOp) qAttrs {
	if len(ops) == 0 {
		return nil
	}
	at := qAttrs{}
	for k, v := range ops[0].attrs {
		at[k] = v
	}
	for _, op := range ops {
		if op.k == qIns && strings.ContainsRune(string(op.text), '\n') {
			for k, v := range op.attrs {
				at[k] = v
			}
			break
		}
	}
	return at
}

// looksLikeSlashCommand is s4/N: the first line has no line format and
// the first op is text (a mention isn't, nor inline code: 67Az isOpCode)
// starting with a slash and then neither a slash nor a space. (s4/N also
// takes a command shortcut URL op, which parseEditLast never does.)
func looksLikeSlashCommand(q qDelta) bool {
	first := true
	plain := true
	q.eachLine(func(_ qDelta, at qAttrs) {
		if first {
			plain = len(at) == 0
			first = false
		}
	})
	if !plain || len(q.ops) == 0 || !qTextItem(q.ops[0]) || truthy(q.ops[0].attrs["code"]) {
		return false
	}
	t := q.ops[0].text
	if len(t) < 2 || t[0] != '/' && t[0] != fullSlash {
		return false
	}
	return t[1] != '/' && t[1] != fullSlash && !jsSpace(t[1])
}

// parseEditLast is parseEditLastMessageCommand on the composer's Quill
// document (after looksLikeSlashCommand, as handleSlashCommand asks).
func parseEditLast(q qDelta) (editLastCmd, bool) {
	ops := qTrim(q.ops, false, true)
	if len(ops) != 1 || !qTextItem(ops[0]) {
		return editLastCmd{}, false
	}
	s := strings.ReplaceAll(string(ops[0].text), string(rune(fullSlash)), "/")
	m := editLastRe.FindStringSubmatch(s)
	if m == nil {
		return editLastCmd{}, false
	}
	return editLastCmd{Prev: m[1], Next: m[2], ReplaceAll: strings.Contains(m[3], "g"),
		IgnoreCase: strings.Contains(m[3], "i")}, true
}

// editLastFromDoc reads the /s command out of the composer's document.
func editLastFromDoc(d *Doc) (editLastCmd, bool) {
	q, _ := qFromDoc(d)
	if !looksLikeSlashCommand(q) {
		return editLastCmd{}, false
	}
	return parseEditLast(q)
}

// ---- the replace, in JavaScript's terms ----

// editWordSep is editLastUserMessageText's w, [\s\n\r\t.,'"+!?\-_|].
func editWordSep(u uint16) bool {
	switch u {
	case '.', ',', '\'', '"', '+', '!', '?', '-', '_', '|':
		return true
	}
	return jsSpace(rune(u))
}

// jsUpperMulti are the BMP letters whose full uppercase (SpecialCasing)
// is more than one character while Go's simple mapping has one:
// JavaScript's ignoreCase canonicalize leaves them alone.
func jsUpperMulti(r rune) bool {
	switch {
	case r >= 0x1f80 && r <= 0x1f87, r >= 0x1f90 && r <= 0x1f97, r >= 0x1fa0 && r <= 0x1fa7:
		return true
	}
	return r == 0x1fb3 || r == 0x1fc3 || r == 0x1ff3
}

// jsCanonicalize is RegExp's Canonicalize for /i without /u: the code
// unit's uppercase when that is one code unit, never ASCII from beyond.
func jsCanonicalize(u uint16) uint16 {
	if u >= 0xd800 && u <= 0xdfff {
		return u
	}
	r := rune(u)
	if jsUpperMulti(r) {
		return u
	}
	up := unicode.ToUpper(r)
	if up > 0xffff || u >= 128 && up < 128 {
		return u
	}
	return uint16(up)
}

// editReplace is editLastUserMessageText's replace for one op's text:
// text.replace(RegExp(`(^|W+)(?:old)(?=W+|$)`, flags), "$1" + next).
func editReplace(text, prev, next string, ignoreCase, replaceAll bool) string {
	if text == "" || prev == "" {
		return text
	}
	s := utf16.Encode([]rune(text))
	p := utf16.Encode([]rune(prev))
	tmpl := utf16.Encode([]rune("$1" + next))
	eq := func(a, b uint16) bool {
		if ignoreCase {
			return jsCanonicalize(a) == jsCanonicalize(b)
		}
		return a == b
	}
	litAt := func(j int) bool {
		if j+len(p) > len(s) {
			return false
		}
		for k := range p {
			if !eq(s[j+k], p[k]) {
				return false
			}
		}
		return true
	}
	ahead := func(j int) bool { return j == len(s) || editWordSep(s[j]) }
	// match at i: the end of the match and the end of group 1
	match := func(i int) (end, g1 int, ok bool) {
		if i == 0 && litAt(0) && ahead(len(p)) {
			return len(p), 0, true
		}
		n := 0
		for i+n < len(s) && editWordSep(s[i+n]) {
			n++
		}
		for l := n; l >= 1; l-- { // W+ is greedy, then backtracks
			if litAt(i+l) && ahead(i+l+len(p)) {
				return i + l + len(p), i + l, true
			}
		}
		return 0, 0, false
	}
	var out []uint16
	last := 0
	for i := 0; i <= len(s); {
		end, g1, ok := match(i)
		if !ok {
			i++
			continue
		}
		out = append(out, s[last:i]...)
		out = append(out, jsSubstitution(tmpl, s, i, end, s[i:g1])...)
		last = end
		if !replaceAll {
			break
		}
		i = end
	}
	if last == 0 && out == nil {
		return text
	}
	out = append(out, s[last:]...)
	return string(utf16.Decode(out))
}

// jsSubstitution is GetSubstitution for a regexp with one capture group.
func jsSubstitution(tmpl, s []uint16, pos, end int, g1 []uint16) []uint16 {
	var out []uint16
	for i := 0; i < len(tmpl); i++ {
		c := tmpl[i]
		if c != '$' || i+1 == len(tmpl) {
			out = append(out, c)
			continue
		}
		n := tmpl[i+1]
		switch {
		case n == '$':
			out = append(out, '$')
			i++
		case n == '&':
			out = append(out, s[pos:end]...)
			i++
		case n == '`':
			out = append(out, s[:pos]...)
			i++
		case n == '\'':
			out = append(out, s[end:]...)
			i++
		case n >= '0' && n <= '9':
			digits := 1
			idx := int(n - '0')
			if i+2 < len(tmpl) && tmpl[i+2] >= '0' && tmpl[i+2] <= '9' {
				if two := idx*10 + int(tmpl[i+2]-'0'); two <= 1 {
					digits, idx = 2, two
				}
			}
			if idx == 1 {
				out = append(out, g1...)
			} else {
				out = append(out, tmpl[i:i+1+digits]...)
			}
			i += digits
		default:
			out = append(out, '$') // "$<" without named groups too
		}
	}
	return out
}

// editLastOps is editLastUserMessageText's map over the message's ops:
// the new ops and whether any op's text changed.
func editLastOps(ops []qOp, c editLastCmd) ([]qOp, bool) {
	out := make([]qOp, len(ops))
	changed := false
	for i, op := range ops {
		out[i] = op
		if !c.ReplaceAll && changed || !qTextItem(op) {
			continue
		}
		t := string(op.text)
		a := editReplace(t, c.Prev, c.Next, c.IgnoreCase, c.ReplaceAll)
		if t == "" { // the replace's null for an empty text: t !== a
			changed = true
		} else if a != t {
			changed = true
		}
		op.text = []rune(a)
		out[i] = op
	}
	return out, changed
}

// ---- the message ----

// editLastTarget is qszY W4, getLastEditableMessage: the viewer's newest
// loaded message here (a reply in a thread), when canEditMessage allows
// it. ok is false when there is none.
func editLastTarget(ctx context.Context, api coreAPI, ch, threadTS string) (core.Message, bool) {
	conv, ok := api.conv(ch)
	if !ok || api.a == nil {
		return core.Message{}, false
	}
	msgs, ok := api.newestMessages(ctx, ch, threadTS)
	if !ok {
		return core.Message{}, false
	}
	self := api.self().ID
	var list []core.Message
	for _, m := range msgs {
		if m.NoDisplay {
			continue // isHidden / isHiddenInThread
		}
		if threadTS == "" && m.ThreadTS != "" && m.ThreadTS != m.TS && !m.Broadcast {
			continue // a reply the channel holds is _hidden_reply
		}
		list = append(list, m)
	}
	// getLastMessageByMember: orderBy ts desc, then the first own plain,
	// me_message or thread_broadcast message
	sort.SliceStable(list, func(i, j int) bool { return list[i].TS > list[j].TS })
	for _, m := range list {
		if m.User != self || m.Subtype != "" && m.Subtype != "me_message" && m.Subtype != "thread_broadcast" {
			continue
		}
		if m.Pending || m.Failed || !canEditOwnMessage(api, conv, m, msgs, self) {
			return core.Message{}, false // isTempMsg (a failed send keeps its temp ts), canEditMessage
		}
		return m, true
	}
	return core.Message{}, false
}

// canEditOwnMessage is canEditMessage (8AvF) for the viewer's own
// message. Slack's further checks that Clack can't make here are the
// Slackbot DM with Slackbot AI on, canUserPostInChannel and canReply
// (the composer the command was typed in is showing, so posting is
// allowed), and pending files (a pending message never gets this far).
func canEditOwnMessage(api coreAPI, conv core.Conv, m core.Message, msgs []core.Message, self string) bool {
	if conv.User == "USLACK" { // isChannelSystemNotificationIm
		return false
	}
	blocks := messageBlockTypes(m.Blocks)
	if len(blocks) > 0 && api.experiment("portable_content_composer") != "treatment" {
		rich := false
		for _, t := range blocks {
			switch t {
			case "rich_text":
				rich = true
			case "context":
			default:
				return false
			}
		}
		if !rich {
			return false
		}
	}
	if conv.Kind == "im" && conv.User == self {
		return true // your own DM: no time limit
	}
	// msg_edit_window_mins (JszJ): -1 is no limit, 0 no edits
	w := -1
	if v, ok := api.teamPrefInt("msg_edit_window_mins"); ok {
		w = v
	}
	if w == 0 || w > -1 && time.Since(tsTime(m.TS)).Minutes() > float64(w) {
		return false
	}
	if root := m.ThreadTS; root != "" { // isThreadLocked
		for _, r := range msgs {
			if r.TS == root && r.Locked {
				return false
			}
		}
	}
	for _, t := range blocks {
		if t == "event" { // hasEventBlockInMessage
			return false
		}
	}
	return true
}

func messageBlockTypes(raw json.RawMessage) []string {
	var bs []struct {
		Type string `json:"type"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &bs) != nil {
		return nil
	}
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Type
	}
	return out
}

func tsTime(ts string) time.Time {
	f, _ := strconv.ParseFloat(ts, 64)
	return time.Unix(0, int64(f*1e9))
}

// hasBlocks is editLastUserMessageText's d.blocks (an empty array counts).
func hasBlocks(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null"
}

// editLastDoc is the rest of editLastUserMessageText for message m: the
// edited document, or nil when the command changes nothing.
func editLastDoc(m core.Message, c editLastCmd, r AtomResolver) (*Doc, error) {
	if !hasBlocks(m.Blocks) {
		return nil, errEditLastNotFound
	}
	raw := richTextOnly(m.Blocks)
	if raw == nil {
		return nil, nil // convertBlocksToDelta: no rich_text, no ops
	}
	orig, err := DocFromBlocksJSON(raw, r)
	if err != nil || orig == nil {
		return nil, nil
	}
	q, _ := qFromDoc(orig)
	ops, changed := editLastOps(q.ops, c)
	if !changed {
		return nil, nil
	}
	ops = qTrim(ops, true, true)
	if len(ops) == 0 {
		return nil, errEditLastNoText
	}
	var nd qDelta
	for _, op := range ops {
		nd.push(op)
	}
	d, _ := qToDoc(qNormalize(nd))
	if docsEqual(d, orig) {
		return nil, nil // saveMessageEdit: the same as the message
	}
	return d, nil
}

// saveEditArgs is Slack's saveMessageEdit {channelId, ts, blocks, text}.
type saveEditArgs struct {
	ChannelID string          `json:"channelId"`
	TS        string          `json:"ts"`
	Blocks    json.RawMessage `json:"blocks,omitempty"`
	Text      string          `json:"text,omitempty"`
}

// editLast runs a parsed /s command (handleSlashCommand's search/replace)
// for the message the composer was about to send, doc. The composer is
// already cleared; a failure puts doc back.
func (c *Composer) editLast(cmd editLastCmd, doc *Doc) {
	api, ch, ts := c.api, c.ch, c.ts
	var r AtomResolver
	if api.a != nil {
		r = api.resolver()
	}
	go func() {
		ctx, cancel := context.WithTimeout(taut.UserAction(context.Background()), 30*time.Second)
		defer cancel()
		m, ok := editLastTarget(ctx, api, ch, ts)
		if !ok {
			c.restore(doc) // "Message not found": restoreDraftOnFailure
			return
		}
		nd, err := editLastDoc(m, cmd, r)
		if err != nil {
			c.restore(doc)
			return
		}
		if nd == nil {
			return // nothing matched: resolved, nothing else
		}
		pd := make(chan *Doc, 1)
		c.later(func() { pd <- c.prepareSend(nd) }) // saveMessageEdit's processDelta
		var prepared *Doc
		select {
		case prepared = <-pd:
		case <-ctx.Done():
			c.restore(doc)
			return
		}
		args := saveEditArgs{ChannelID: m.Channel, TS: m.TS, Text: prepared.Mrkdwn(), Blocks: withKeptBlocks(prepared.RichTextBlocks(), m.Blocks)}
		if args.ChannelID == "" {
			args.ChannelID = ch
		}
		_, err = taut.Dispatch(ctx, currentTaut(), "saveMessageEdit", args, func(ctx context.Context, a saveEditArgs) (any, error) {
			text, blocks := transformBlocks(ctx, a.Text, a.Blocks)
			return nil, api.edit(ctx, a.ChannelID, a.TS, text, blocks)
		})
		if err != nil {
			c.restore(doc)
			c.later(func() { c.h.Toast("Couldn't save your edit: " + err.Error()) })
		}
	}()
}
