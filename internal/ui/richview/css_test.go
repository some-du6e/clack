package richview

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/op"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// All documents here are synthetic (docs/NATIVE.md, scraping policy).

// cssDoc has every kind of rich-text part a message body draws.
func cssDoc() rt.Doc {
	return doc(
		para(txt("plainword "), rt.Span{Kind: rt.Text, Text: "boldword", Style: rt.Bold}, txt(" "),
			rt.Span{Kind: rt.Text, Text: "italicword", Style: rt.Italic}, txt(" "),
			rt.Span{Kind: rt.Text, Text: "strikeword", Style: rt.Strike}, txt(" "),
			rt.Span{Kind: rt.Link, Text: "linkword", URL: "https://example.invalid/x"}, txt(" "),
			rt.Span{Kind: rt.Text, Text: "codeword", Style: rt.Code}, txt(" "),
			rt.Span{Kind: rt.User, Text: "@mentionword", ID: "U0FAKE1"}, txt(" "),
			rt.Span{Kind: rt.Usergroup, Text: "@groupword", ID: "S0FAKE1"}, txt(" "),
			rt.Span{Kind: rt.Broadcast, Text: "@here", ID: "here", Self: true}),
		rt.Block{Kind: rt.List, Items: [][]rt.Span{{txt("listword")}, {txt("listtwo")}}, Ordered: true},
		rt.Block{Kind: rt.Quote, Spans: []rt.Span{txt("quoteword")}},
		rt.Block{Kind: rt.Preformatted, Spans: []rt.Span{txt("preword")}},
	)
}

var rowDef = slackc.Class("c-message_kit__background c-message_kit__message")

// cssRuns lays doc out under a fake message row carrying class rowClass
// (the way MessageLogger's wrapper div is the row's ancestor) and returns
// what each word is painted in.
func cssRuns(t *testing.T, rowClass string, o Options) (map[string]Run, *View) {
	t.Helper()
	st := newTestStyle()
	v := New(cssDoc(), o)
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := testGtx(ops, 900)
		row := rowDef.EnterClass(gtx, 0, 0, rowClass)
		v.Layout(gtx, st, nil)
		row.Leave()
	}
	frame()
	frame()
	runs := map[string]Run{}
	v.Runs(func(r Run) { runs[r.Text] = r })
	return runs, v
}

// mlDeletedRule is Slick MessageLogger's red deleted style, verbatim.
const mlDeletedRule = `.slick-ml-deleted, .slick-ml-deleted * { color: #e01e5a !important; }`

var (
	mlRed  = color.NRGBA{R: 0xe0, G: 0x1e, B: 0x5a, A: 0xff}
	words  = []string{"plainword", "boldword", "italicword", "strikeword", "linkword", "codeword", "@mentionword", "@groupword", "@here", "listword", "listtwo", "quoteword", "preword", "(edited)", "1.", "2."}
	native = []string{"linkword", "codeword", "@mentionword", "@groupword", "@here", "(edited)"}
)

func TestCSSMessageLoggerRedReachesEveryPart(t *testing.T) {
	// Slick MessageLogger's deleted style, verbatim (src/plugins/MessageLogger/index.ts)
	slackc.SetStyle("test:ml", mlDeletedRule)
	defer slackc.RemoveStyle("test:ml")
	runs, _ := cssRuns(t, "slick-ml-deleted", Options{Edited: true})
	for _, w := range words {
		r, ok := runs[w]
		if !ok {
			t.Errorf("%q not laid out; runs %v", w, runs)
			continue
		}
		if r.Color != mlRed {
			t.Errorf("%q painted %v, want MessageLogger's #e01e5a", w, r.Color)
		}
	}
	// the same body on a row that isn't deleted keeps Slack's colors
	plain, _ := cssRuns(t, "", Options{Edited: true})
	for _, w := range words {
		if plain[w].Color == mlRed {
			t.Errorf("%q red on a row without .slick-ml-deleted", w)
		}
	}
}

// An inherited color (a rule on the row only, no "*") reaches the text
// that inherits in Slack (plain, bold, italic, lists, quotes), but not
// the parts with a color of their own in Slack's stylesheet (links,
// mentions, inline code, the edited label).
func TestCSSInheritedColorStopsAtNativeColors(t *testing.T) {
	slackc.SetStyle("test:inh", `.x-row-tint { color: #00ff00 }`)
	defer slackc.RemoveStyle("test:inh")
	base, _ := cssRuns(t, "", Options{Edited: true})
	runs, _ := cssRuns(t, "x-row-tint", Options{Edited: true})
	green := color.NRGBA{G: 0xff, A: 0xff}
	isNative := map[string]bool{}
	for _, w := range native {
		isNative[w] = true
	}
	for _, w := range words {
		got := runs[w].Color
		switch {
		case isNative[w] && got != base[w].Color:
			t.Errorf("%q: inherited color reached a part with its own color: %v, want %v", w, got, base[w].Color)
		case !isNative[w] && got != green:
			t.Errorf("%q: %v, want the row's inherited #00ff00", w, got)
		}
	}
	// a direct rule specific enough beats Slack's own .c-link color
	slackc.SetStyle("test:inh", `.x-row-tint .c-link { color: #0000ff } .c-mrkdwn__code { color: #ff00ff } * { color: #00ffff }`)
	runs, _ = cssRuns(t, "x-row-tint", Options{})
	if c := runs["linkword"].Color; c != (color.NRGBA{B: 0xff, A: 0xff}) {
		t.Errorf("link: %v, want the more specific rule's blue", c)
	}
	if c := runs["codeword"].Color; c != (color.NRGBA{R: 0xff, B: 0xff, A: 0xff}) {
		t.Errorf("code: %v, want the same-specificity later rule's magenta", c)
	}
	// a user group is a.c-link in Slack (UserGroup's Link), so the
	// specific .c-link rule reaches it, but * (0,0,0) mustn't beat Slack's
	// own .c-mrkdwn__user_group--link
	if c := runs["@groupword"].Color; c != (color.NRGBA{B: 0xff, A: 0xff}) {
		t.Errorf("usergroup: %v, want the .c-link rule's blue (never *'s cyan)", c)
	}
	if c := runs["plainword"].Color; c != (color.NRGBA{G: 0xff, B: 0xff, A: 0xff}) {
		t.Errorf("plain: %v, want * (it matches the section directly)", c)
	}
}

func TestCSSOpacityDecorationAndFonts(t *testing.T) {
	slackc.SetStyle("test:fx", `
		.x-row-fx * { opacity: 0.5 }
		.x-row-fx { text-decoration: line-through }
		.x-row-fx .p-rich_text_section { font-style: italic; font-weight: 700 }
		.x-row-fx b { font-weight: 400 }
		.x-row-fx .c-mrkdwn__code { background-color: #ffff00 }
		.x-row-fx i { background-color: rgb(0, 0, 255) }`)
	defer slackc.RemoveStyle("test:fx")
	runs, v := cssRuns(t, "x-row-fx", Options{})
	for _, w := range []string{"plainword", "listword", "quoteword", "linkword"} {
		if runs[w].Deco&css.DecoLineThrough == 0 {
			t.Errorf("%q: the row's line-through didn't propagate to it", w)
		}
	}
	if r := runs["plainword"]; !r.Bold {
		t.Errorf("plainword: section's font-weight 700 not applied")
	}
	if r := runs["boldword"]; r.Bold {
		t.Errorf("boldword: b's font-weight 400 not applied")
	}
	// opacity: the list item (ol > li) is two elements below the row, each 0.5
	if a := runs["listword"].Color.A; a < 50 || a > 80 {
		t.Errorf("listword alpha %d, want ~64 (0.5 per element)", a)
	}
	var code, ital bool
	for i := range v.d.boxes {
		b := &v.d.boxes[i]
		if b.part != 0 && v.boxFill(b).R == 0xff && v.boxFill(b).G == 0xff && v.boxFill(b).B == 0 {
			code = true
		}
	}
	v.partBackgrounds(func(_ image.Rectangle, c color.NRGBA) {
		if c.B == 0xff && c.R == 0 {
			ital = true
		}
	}, 0)
	if !code {
		t.Error("inline code's CSS background didn't replace its box fill")
	}
	if !ital {
		t.Error("the italic run's CSS background wasn't painted")
	}
}

// With no CSS installed the body has no parts and paints as before.
func TestCSSNoneNoParts(t *testing.T) {
	if !slackc.Styles.Empty() {
		t.Skip("another test left CSS installed")
	}
	runs, v := cssRuns(t, "slick-ml-deleted", Options{})
	if len(v.css.parts) != 0 {
		t.Fatalf("%d parts without CSS", len(v.css.parts))
	}
	for _, f := range v.d.frags {
		if f.part != 0 {
			t.Fatal("a frag has a part without CSS")
		}
	}
	if runs["plainword"].Color == mlRed {
		t.Fatal("red without CSS")
	}
}

// The paths richview draws outside the static recording take CSS too: an
// animated emoji (drawn every frame) gets its part's opacity, and inline
// code in its own font gets MessageLogger's red.
func TestCSSAnimatedEmojiAndCodeRuns(t *testing.T) {
	slackc.SetStyle("test:live", mlDeletedRule+` .slick-ml-deleted .c-emoji { opacity: 0.5 }`)
	defer slackc.RemoveStyle("test:live")
	imgs := animImages{newFakeImages()}
	imgs.ready = true
	st := NewStyle(sharedShaper(), imgs, LightPalette())
	d := doc(para(txt("party "), rt.Span{Kind: rt.Emoji, ID: "party", URL: "https://e.test/party.gif"},
		txt(" "), rt.Span{Kind: rt.Text, Text: "codeword", Style: rt.Code}))
	v := New(d, Options{})
	ops := new(op.Ops)
	for i := 0; i < 2; i++ {
		ops.Reset()
		gtx := testGtx(ops, 400)
		row := rowDef.EnterClass(gtx, 0, 0, "slick-ml-deleted")
		v.Layout(gtx, st, &recHandler{})
		row.Leave()
	}
	if len(v.live) != 1 {
		t.Fatalf("live frags %v", v.live)
	}
	if ps := v.css.get(v.d.frags[v.live[0]].part); ps == nil || ps.alpha != 0.5 {
		t.Errorf("the animated emoji's part: %+v, want opacity 0.5", ps)
	}
	found := false
	v.Runs(func(r Run) {
		if r.Text == "codeword" {
			found = true
			if r.Color != mlRed {
				t.Errorf("inline code in its mono face: %v, want #e01e5a", r.Color)
			}
		}
	})
	if !found {
		t.Fatal("no code run")
	}
}

// The emoji tooltip is Slack's .c-tooltip__tip, a portal under <body>:
// a row's ".slick-ml-deleted *" doesn't reach it, a tooltip rule does.
func TestCSSEmojiTooltip(t *testing.T) {
	p := LightPalette()
	tip := func(rowClass string) (color.NRGBA, color.NRGBA, float32) {
		gtx := testGtx(new(op.Ops), 400)
		row := rowDef.EnterClass(gtx, 0, 0, rowClass)
		defer row.Leave()
		return tooltipStyle(&p)
	}
	slackc.SetStyle("test:tip", mlDeletedRule)
	if bg, fg, _ := tip("slick-ml-deleted"); bg != p.TooltipBg || fg != p.TooltipText {
		t.Errorf("a row's rule reached the tooltip: bg %v fg %v", bg, fg)
	}
	slackc.SetStyle("test:tip", `.c-tooltip__tip { background-color: #00ff00 !important; color: #0000ff; opacity: 0.8 }`)
	defer slackc.RemoveStyle("test:tip")
	bg, fg, o := tip("")
	if bg != (color.NRGBA{G: 0xff, A: 0xff}) || fg != (color.NRGBA{B: 0xff, A: 0xff}) || o != 0.8 {
		t.Errorf("tooltip rule: bg %v fg %v opacity %v", bg, fg, o)
	}
}
