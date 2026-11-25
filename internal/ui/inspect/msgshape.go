package inspect

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// A message's shape, for a report about how one draws (Karim's /remind
// confirmation, 11-24: its text and buttons 16dp right of the name and a
// "#⬚" before a mention, and nothing in the report said whether the body
// was blocks or an attachment). Only structure goes in: which fields the
// message has, its blocks' and elements' types, its attachments' field
// names, colour and action types, and the mentions in its text by kind,
// each id its first letter (U, W, C, D, G, S, B…) and a tag. Nothing
// anyone wrote: no text, names, labels, URLs (a link is its scheme) or
// emoji names (a count), and invisible or private-use characters only as
// code points (U+200B), since those are what draw as a missing glyph.

// MessageFinder is an optional Host method: the message the store holds
// for ch/ts, as Slack sent it (blocks and attachments raw).
type MessageFinder interface {
	InspectMessage(ch, ts string) (slack.Message, bool)
}

// publicIDs are Slack's own fixed ids, the same in every workspace: they
// stay as they are.
var publicIDs = map[string]bool{"USLACKBOT": true, "B01": true}

// pref is a mention's id: its first letter and a tag ("U:3fa2b1"), or the
// id itself for one of Slack's own.
func (r *Redactor) pref(id string) string {
	if id == "" {
		return "?"
	}
	if publicIDs[id] {
		return id
	}
	t := r.Tag("m", id)
	return id[:1] + t[strings.IndexByte(t, ':'):]
}

// maxShapeLines and maxShapeLen bound what a message adds to a report.
const (
	maxShapeLines = 12
	maxShapeLen   = 600
)

// MessageShape is m's structure as report lines.
func (r *Redactor) MessageShape(m *slack.Message) []string {
	var out []string
	add := func(s string) {
		if len(out) < maxShapeLines {
			if len(s) > maxShapeLen {
				s = s[:maxShapeLen] + "…"
			}
			out = append(out, s)
		}
	}
	head := []string{}
	if m.Subtype != "" && safeValue(m.Subtype) {
		head = append(head, "subtype="+m.Subtype)
	}
	if m.User != "" {
		head = append(head, "user="+r.pref(m.User))
	}
	if m.BotID != "" {
		head = append(head, "bot="+r.pref(m.BotID))
	}
	if m.IsEphemeral {
		head = append(head, "ephemeral")
	}
	if m.Username != "" {
		head = append(head, "username=set")
	}
	if m.ThreadTS != "" {
		if m.ThreadTS == m.TS {
			head = append(head, "thread root")
		} else {
			head = append(head, "reply")
		}
	}
	if n := len(m.Files); n > 0 {
		head = append(head, fmt.Sprintf("files=%d", n))
	}
	add("msg: " + strings.Join(head, " · "))
	add("text: " + r.textShape(m.Text))
	var blocks []json.RawMessage
	if json.Unmarshal(m.Blocks, &blocks) == nil && len(blocks) > 0 {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			parts = append(parts, r.nodeShape(b, 0))
		}
		add("blocks: " + strings.Join(parts, " "))
	}
	raw := m.RawAttachments
	if len(raw) == 0 && len(m.Attachments) > 0 {
		raw, _ = json.Marshal(m.Attachments) // Clack's decoding: no raw copy held
	}
	var atts []map[string]json.RawMessage
	if json.Unmarshal(raw, &atts) == nil {
		for i, a := range atts {
			add(fmt.Sprintf("attachment %d: %s", i+1, r.attShape(a)))
		}
	}
	return out
}

// textShape is mrkdwn's tokens in order (mentions, specials, links),
// its emoji count and its invisible characters; "empty" for none.
func (r *Redactor) textShape(s string) string {
	if s == "" {
		return "empty"
	}
	var parts []string
	if toks := r.tokens(s); len(toks) > 0 {
		parts = append(parts, "tokens=["+strings.Join(toks, ", ")+"]")
	}
	if n := len(emojiRE.FindAllStringIndex(s, -1)); n > 0 {
		parts = append(parts, fmt.Sprintf("emoji×%d", n))
	}
	if inv := invisibles(s); inv != "" {
		parts = append(parts, "invisible="+inv)
	}
	if len(parts) == 0 {
		return "set (plain)"
	}
	return "set · " + strings.Join(parts, " · ")
}

var (
	tokenRE  = regexp.MustCompile(`<([^<>]*)>`)
	emojiRE  = regexp.MustCompile(`:[a-z0-9_+'\-]+(::[a-z0-9_\-]+)?:`)
	schemeRE = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9+.\-]{0,20}):`)
)

// tokens are s's <…> entities by kind: @U:tag, #C:tag, !here,
// !subteam^S:tag, !date, link:https; "|label" when the entity carries one
// (its text left out).
func (r *Redactor) tokens(s string) []string {
	var out []string
	for _, m := range tokenRE.FindAllStringSubmatch(s, 32) {
		body, label, labelled := strings.Cut(m[1], "|")
		suffix := ""
		if labelled {
			suffix = "|label"
			if inv := invisibles(label); inv != "" {
				suffix = "|label(" + inv + ")"
			}
		}
		switch {
		case strings.HasPrefix(body, "@"):
			out = append(out, "@"+r.pref(body[1:])+suffix)
		case strings.HasPrefix(body, "#"):
			out = append(out, "#"+r.pref(body[1:])+suffix)
		case strings.HasPrefix(body, "!subteam^"):
			out = append(out, "!subteam^"+r.pref(strings.TrimPrefix(body, "!subteam^"))+suffix)
		case strings.HasPrefix(body, "!date^"):
			out = append(out, "!date"+suffix)
		case strings.HasPrefix(body, "!"):
			w, _, _ := strings.Cut(body[1:], "^")
			if !safeValue(w) {
				w = "special"
			}
			out = append(out, "!"+w+suffix)
		default:
			if sm := schemeRE.FindStringSubmatch(body); sm != nil && safeValue(strings.ToLower(sm[1])) {
				out = append(out, "link:"+strings.ToLower(sm[1])+suffix)
			} else {
				out = append(out, "<?>"+suffix)
			}
		}
	}
	return out
}

// invisibles are s's format, control (but tab and newline) and
// private-use characters, each once, as code points.
func invisibles(s string) string {
	seen := map[rune]bool{}
	var out []string
	for _, c := range s {
		if c == '\n' || c == '\t' || seen[c] {
			continue
		}
		if unicode.Is(unicode.Cf, c) || unicode.Is(unicode.Co, c) || unicode.Is(unicode.Cc, c) || c == '\ufffd' {
			seen[c] = true
			out = append(out, fmt.Sprintf("U+%04X", c))
		}
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// nodeShape is a block's or element's type, with what says how it draws
// (a mention's id kind, a text object's type and tokens, a button's
// style), and its children's in parentheses.
func (r *Redactor) nodeShape(raw json.RawMessage, depth int) string {
	var n map[string]json.RawMessage
	if json.Unmarshal(raw, &n) != nil {
		return "?"
	}
	str := func(k string) string {
		var s string
		_ = json.Unmarshal(n[k], &s)
		return s
	}
	typ := str("type")
	if !safeValue(typ) {
		typ = "?"
	}
	s := typ
	switch typ {
	case "user":
		s += "@" + r.pref(str("user_id"))
	case "channel":
		s += "#" + r.pref(str("channel_id"))
	case "usergroup":
		s += "^" + r.pref(str("usergroup_id"))
	case "broadcast":
		if rg := str("range"); safeValue(rg) {
			s += "!" + rg
		}
	case "link":
		if sm := schemeRE.FindStringSubmatch(str("url")); sm != nil && safeValue(strings.ToLower(sm[1])) {
			s += ":" + strings.ToLower(sm[1])
		}
	case "text", "plain_text", "mrkdwn":
		t := str("text")
		if typ == "mrkdwn" {
			if toks := r.tokens(t); len(toks) > 0 {
				s += "[" + strings.Join(toks, ", ") + "]"
			}
		}
		if inv := invisibles(t); inv != "" {
			s += "{" + inv + "}"
		}
	case "button":
		if st := str("style"); safeValue(st) {
			s += ":" + st
		}
	}
	if st, ok := n["style"]; ok && typ != "button" {
		var m map[string]bool
		if json.Unmarshal(st, &m) == nil {
			var on []string
			for k, v := range m {
				if v && safeValue(k) {
					on = append(on, k)
				}
			}
			sort.Strings(on)
			if len(on) > 0 {
				s += "<" + strings.Join(on, ",") + ">"
			}
		}
	}
	if depth >= 6 {
		return s
	}
	var kids []string
	for _, k := range []string{"text", "elements", "fields", "accessory"} {
		v, ok := n[k]
		if !ok {
			continue
		}
		var list []json.RawMessage
		if json.Unmarshal(v, &list) == nil {
			for _, c := range list {
				kids = append(kids, r.nodeShape(c, depth+1))
			}
			continue
		}
		if k == "text" && (typ == "text" || typ == "plain_text" || typ == "mrkdwn") {
			continue // the element's own text, a string
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(v, &obj) == nil {
			kids = append(kids, k+":"+r.nodeShape(v, depth+1))
		}
	}
	if len(kids) > 0 {
		s += "(" + strings.Join(kids, ", ") + ")"
	}
	return s
}

// attShape is a legacy attachment's field names, its colour, and its
// actions', blocks' and text fields' shapes.
func (r *Redactor) attShape(a map[string]json.RawMessage) string {
	keys := make([]string, 0, len(a))
	for k := range a {
		if safeValue(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := []string{"fields=" + strings.Join(keys, ",")}
	var col string
	_ = json.Unmarshal(a["color"], &col)
	switch {
	case col == "":
		parts = append(parts, "color=none")
	case colorRE.MatchString(col) || safeValue(col):
		parts = append(parts, "color="+col)
	default:
		parts = append(parts, "color=set")
	}
	var acts []map[string]json.RawMessage
	if json.Unmarshal(a["actions"], &acts) == nil && len(acts) > 0 {
		var as []string
		for _, x := range acts {
			var t, st string
			_ = json.Unmarshal(x["type"], &t)
			_ = json.Unmarshal(x["style"], &st)
			if !safeValue(t) {
				t = "?"
			}
			if safeValue(st) {
				t += ":" + st
			}
			as = append(as, t)
		}
		parts = append(parts, "actions("+strings.Join(as, ", ")+")")
	}
	var blocks []json.RawMessage
	if json.Unmarshal(a["blocks"], &blocks) == nil && len(blocks) > 0 {
		var bs []string
		for _, b := range blocks {
			bs = append(bs, r.nodeShape(b, 0))
		}
		parts = append(parts, "blocks("+strings.Join(bs, " ")+")")
	}
	for _, k := range []string{"pretext", "text", "fallback"} {
		var t string
		if json.Unmarshal(a[k], &t) == nil && t != "" {
			parts = append(parts, k+": "+r.textShape(t))
		}
	}
	return strings.Join(parts, " · ")
}

var colorRE = regexp.MustCompile(`^#?[0-9a-fA-F]{3,8}$`)

// propMessage is the conversation and ts of the message a component's
// props are for: its msg's (or message's) channel and ts, else the props'
// channelId and ts.
func propMessage(p map[string]any) (ch, ts string) {
	get := func(m map[string]any, k string) string { v, _ := m[k].(string); return v }
	for _, k := range []string{"msg", "message"} {
		var m map[string]any
		switch x := p[k].(type) {
		case map[string]any:
			m = x
		case slackc.Props:
			m = x
		}
		if m != nil {
			if c, t := get(m, "channel"), get(m, "ts"); c != "" && t != "" {
				return c, t
			}
		}
	}
	return get(p, "channelId"), firstOf(get(p, "ts"), get(p, "messageTs"))
}

func firstOf(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
