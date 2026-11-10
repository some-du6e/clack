package messages

import (
	"encoding/json"
	"image"
	"image/color"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// All messages here are synthetic (docs/NATIVE.md, scraping policy).

const (
	tCSS     = "C0CSSROWS"
	tsCSSRow = "1700000801.000100"
)

var mlRed = color.NRGBA{R: 0xe0, G: 0x1e, B: 0x5a, A: 0xff}

// cssRowBlocks is a body with every rich-text part: plain, bold, italic,
// strike, a link, inline code, a user mention, an emoji, a list, a quote
// and a code block.
func cssRowBlocks() json.RawMessage {
	b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "block_id": "c1", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{
			textEl("plainword "),
			map[string]any{"type": "text", "text": "boldword", "style": map[string]any{"bold": true}},
			textEl(" "),
			map[string]any{"type": "text", "text": "italicword", "style": map[string]any{"italic": true}},
			textEl(" "),
			map[string]any{"type": "text", "text": "strikeword", "style": map[string]any{"strike": true}},
			textEl(" "),
			map[string]any{"type": "link", "url": "https://example.invalid/page", "text": "linkword"},
			textEl(" "),
			map[string]any{"type": "text", "text": "codeword", "style": map[string]any{"code": true}},
			textEl(" "),
			map[string]any{"type": "user", "user_id": "UBOB"},
			textEl(" "),
			map[string]any{"type": "emoji", "name": "tada"},
		}},
		map[string]any{"type": "rich_text_list", "style": "bullet", "elements": []any{
			map[string]any{"type": "rich_text_section", "elements": []any{textEl("listword")}},
		}},
		map[string]any{"type": "rich_text_quote", "elements": []any{textEl("quoteword")}},
		map[string]any{"type": "rich_text_preformatted", "elements": []any{textEl("preword")}},
	}}})
	return b
}

// cssRowView is a channel with one message that has every native part a
// row draws: header (name, time), rich body, "(edited)", a reaction and a
// reply bar. The view is laid out inside an element with class cls.
func cssRowView(t *testing.T, cls string) (*ChannelView, func()) {
	t.Helper()
	SetTaut(nil)
	app, _ := newTestApp(t)
	app.S.PutConv(slack.Conversation{ID: tCSS, Name: "css-rows", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.SetLatest(tCSS, []slack.Message{{Type: "message", TS: tsCSSRow, User: "UALICE", Text: "plainword boldword …",
		Blocks: cssRowBlocks(), Edited: &slack.Edited{User: "UALICE", TS: "1700000802.000000"},
		Reactions:  []slack.Reaction{{Name: "tada", Count: 3, Users: []string{"UBOB", tSelf, "UALICE"}}},
		ReplyCount: 2, ThreadTS: tsCSSRow, LatestReply: "1700000803.000100", ReplyUsers: []string{"UBOB"}}}, false)
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, tCSS)
	v.Preview = true
	f := frameFn(wrapped{v: v, cls: cls})
	loadList(v, f)
	until(t, 5*time.Second, "the row", f, func() bool { return v.s.rows[tsCSSRow] != nil && v.s.rows[tsCSSRow].rv != nil })
	return v, f
}

// paints is what a frame drew for the row: its labels (name, time,
// reaction count, reply bar) and its body's words.
type paints struct {
	labels map[string]color.NRGBA
	words  map[string]richview.Run
}

var labelMu sync.Mutex

func capture(t *testing.T, v *ChannelView, f func()) paints {
	t.Helper()
	p := paints{labels: map[string]color.NRGBA{}, words: map[string]richview.Run{}}
	labelMu.Lock()
	labelHook = func(txt string, s textStyle) { p.labels[txt] = s.col }
	f()
	labelHook = nil
	labelMu.Unlock()
	v.s.rows[tsCSSRow].rv.Runs(func(r richview.Run) { p.words[r.Text] = r })
	return p
}

var (
	cssBodyWords = []string{"plainword", "boldword", "italicword", "strikeword", "linkword", "codeword", "@bob", "listword", "quoteword", "preword", "(edited)"}
	// the row's labels: name, time, reaction count, reply bar
	cssLabels = func() []string {
		return []string{"alice", clock(tsTime(tsCSSRow)), "3", "2 replies", "Last reply"}
	}
)

func (p paints) label(t *testing.T, s string) (color.NRGBA, bool) {
	t.Helper()
	if c, ok := p.labels[s]; ok {
		return c, true
	}
	for k, c := range p.labels { // display name settings may shorten it
		if strings.HasPrefix(s, k) || strings.HasPrefix(k, s) {
			return c, true
		}
	}
	return color.NRGBA{}, false
}

// Slick MessageLogger's deleted style (red), applied to a row: in Slack's
// DOM ".slick-ml-deleted *" reaches every descendant of the wrapper, so
// every native part of the row is red: sender, timestamp, every word of
// the body (links, mentions, code, lists, quotes, the edited label), the
// reaction count and the reply bar. A row outside the wrapper isn't.
func TestCSSMessageLoggerDeletedRow(t *testing.T) {
	slackc.SetStyle("test:ml", mlDeletedCSS)
	defer slackc.RemoveStyle("test:ml")
	v, f := cssRowView(t, "slick-ml-deleted")
	p := capture(t, v, f)
	for _, w := range cssBodyWords {
		r, ok := p.words[w]
		if !ok {
			t.Errorf("body word %q not drawn; words %v", w, keys(p.words))
		} else if r.Color != mlRed {
			t.Errorf("body word %q painted %v, want #e01e5a", w, r.Color)
		}
	}
	for _, s := range cssLabels() {
		c, ok := p.label(t, s)
		if !ok {
			t.Errorf("label %q not drawn; labels %v", s, keys(p.labels))
		} else if c != mlRed {
			t.Errorf("label %q painted %v, want #e01e5a", s, c)
		}
	}

	v2, f2 := cssRowView(t, "")
	q := capture(t, v2, f2)
	for _, w := range cssBodyWords {
		if q.words[w].Color == mlRed {
			t.Errorf("body word %q red outside .slick-ml-deleted", w)
		}
	}
	for _, s := range cssLabels() {
		if c, _ := q.label(t, s); c == mlRed {
			t.Errorf("label %q red outside .slick-ml-deleted", s)
		}
	}
}

// A color on the row alone is inherited, as in Slack: the body's text
// takes it, the parts with their own color in Slack's stylesheet (the
// sender name, the timestamp, links, mentions, code) don't.
func TestCSSRowColorInherits(t *testing.T) {
	v0, f0 := cssRowView(t, "x-row-tint")
	base := capture(t, v0, f0)
	slackc.SetStyle("test:tint", `.x-row-tint { color: #00ff00 }`)
	defer slackc.RemoveStyle("test:tint")
	v, f := cssRowView(t, "x-row-tint")
	p := capture(t, v, f)
	green := color.NRGBA{G: 0xff, A: 0xff}
	for _, w := range []string{"plainword", "boldword", "italicword", "strikeword", "listword", "quoteword", "preword"} {
		if c := p.words[w].Color; c != green {
			t.Errorf("%q: %v, want the inherited #00ff00", w, c)
		}
	}
	for _, w := range []string{"linkword", "codeword", "@bob", "(edited)"} {
		if c := p.words[w].Color; c != base.words[w].Color {
			t.Errorf("%q: %v; the inherited color reached a part with its own (%v)", w, c, base.words[w].Color)
		}
	}
	for _, s := range []string{"alice", clock(tsTime(tsCSSRow))} {
		if c, _ := p.label(t, s); c == green {
			t.Errorf("label %q took the row's inherited color", s)
		}
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The reaction tooltip is Slack's .c-tooltip__tip, a portal under
// <body>: a deleted row's red doesn't reach it, a tooltip rule does.
func TestCSSReactionTooltip(t *testing.T) {
	th := theme.New(false)
	c := colorsFor(th)
	const txt = "Bob reacted with :tada:"
	drawn := func(rowClass string) color.NRGBA {
		t.Helper()
		var got color.NRGBA
		gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(400, 300)), Now: time.Now()}
		row := rowWrap.EnterClass(gtx, 0, 0, rowClass)
		labelMu.Lock()
		labelHook = func(s string, ts textStyle) {
			if s == txt {
				got = ts.col
			}
		}
		reactionTooltip(gtx, nilHost{}, th, c, txt, "", image.Pt(200, 250), image.Pt(400, 300))
		labelHook = nil
		labelMu.Unlock()
		row.Leave()
		return got
	}
	slackc.SetStyle("test:rtip", mlDeletedCSS)
	defer slackc.RemoveStyle("test:rtip")
	if got := drawn("slick-ml-deleted"); got != c.TooltipText {
		t.Errorf("a row's rule reached the reaction tooltip: %v", got)
	}
	slackc.SetStyle("test:rtip", `.c-tooltip__tip { color: #0000ff !important }`)
	if got := drawn(""); got != (color.NRGBA{B: 0xff, A: 0xff}) {
		t.Errorf("tooltip rule: %v, want #0000ff", got)
	}
}
