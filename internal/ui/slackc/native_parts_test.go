package slackc

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
)

type (
	layoutCtx = layout.Context
	dims      = layout.Dimensions
)

// Natives and the parts they draw themselves (Styled.Child) match
// descendant selectors through the whole element stack, a host node
// included, with Slack's own rules (the base sheet) competing by
// specificity and stopping inheritance.
func TestDescendantSelectorsReachNativeParts(t *testing.T) {
	defer func() { stack = stack[:0] }()
	var (
		row     = Class("c-message_kit__background c-message_kit__message")
		body    = Class("c-message_kit__blocks")
		block   = Class("p-rich_text_block")
		section = Class("p-rich_text_section")
		link    = Class("c-link").Tag("a")
		bold    = Class("").Tag("b")
		code    = Class("c-mrkdwn__code").Tag("code")
		button  = Class("c-button-unstyled c-reaction").Tag("button")
		ts      = Class("c-link c-timestamp").Tag("a")
	)
	red := color.NRGBA{R: 0xe0, G: 0x1e, B: 0x5a, A: 0xff}
	green := color.NRGBA{G: 0xff, A: 0xff}
	def := color.NRGBA{R: 1, G: 2, B: 3, A: 255}

	// parts under a row inside a plugin's host <div class="x-wrap">
	type parts struct{ section, link, linkBold, code, bold, button, ts Styled }
	under := func(wrapClass string, f func(p parts)) {
		t.Helper()
		g := ctx()
		hostEl := &fakeHost{classes: []string{wrapClass}}
		c := Styles.Cascade(hostEl, css.PseudoNone, nil, cssEnv())
		hs := css.Compute(&c, nil, cssEnv())
		// host keys are per node and style generation (box.go)
		hostKey++
		pushHost(hostEl, &hs, propsOf(&c), hostKey)
		r := row.Enter(g, 0, 0)
		b := body.Enter(g, 0, 0)
		sc := CurrentScope()
		blk := sc.Child(block, 0)
		sec := blk.Child(section, 0)
		lk := sec.Child(link, 0)
		f(parts{section: sec, link: lk, linkBold: lk.Child(bold, 0), code: sec.Child(code, 0), bold: sec.Child(bold, 0),
			button: b.Child(button, 0), ts: r.Child(ts, 0)})
		b.Leave()
		r.Leave()
		popHost()
	}

	// MessageLogger's red: every part, own color or not
	SetStyle("test:parts", `.slick-ml-deleted, .slick-ml-deleted * { color: #e01e5a !important; }`)
	under("slick-ml-deleted", func(p parts) {
		for name, s := range map[string]Styled{"section": p.section, "link": p.link, "link b": p.linkBold, "code": p.code, "b": p.bold, "button": p.button, "timestamp": p.ts} {
			if c := s.Color(def); c != red {
				t.Errorf(".slick-ml-deleted *: %s is %v, want red", name, c)
			}
		}
	})
	under("other", func(p parts) {
		if p.section.Color(def) != def || p.link.Color(def) != def {
			t.Error("rules for .slick-ml-deleted reached parts of another wrapper")
		}
	})

	// inheritance: the section and its b inherit, the link, its b and the
	// timestamp keep Slack's own color
	SetStyle("test:parts", `.x-tint { color: #00ff00 }`)
	under("x-tint", func(p parts) {
		if c := p.section.Color(def); c != green {
			t.Errorf("section: %v, want inherited green", c)
		}
		if c := p.bold.Color(def); c != green {
			t.Errorf("b: %v, want inherited green", c)
		}
		for name, s := range map[string]Styled{"link": p.link, "link b": p.linkBold, "code": p.code, "timestamp": p.ts, "reaction": p.button} {
			if s.Has("color") {
				t.Errorf("%s took the inherited color (Slack's own rule should keep it native)", name)
			}
		}
	})

	// specificity against Slack's rules
	SetStyle("test:parts", `* { color: #00ff00 } .x-spec .p-rich_text_section .c-link { color: #0000ff } .c-mrkdwn__code { color: #ff00ff }`)
	under("x-spec", func(p parts) {
		if c := p.link.Color(def); c != (color.NRGBA{B: 0xff, A: 0xff}) {
			t.Errorf("a more specific rule than Slack's .c-link: %v", c)
		}
		if c := p.code.Color(def); c != (color.NRGBA{R: 0xff, B: 0xff, A: 0xff}) {
			t.Errorf("an equally specific later rule than Slack's: %v", c)
		}
		if c := p.ts.Color(def); c != def {
			t.Errorf("* beat Slack's .c-timestamp: %v", c)
		}
		if c := p.section.Color(def); c != green {
			t.Errorf("* on the section: %v", c)
		}
	})

	// opacity isn't inherited; text-decoration propagates to inline text
	// but not into an atomic button
	SetStyle("test:parts", `.x-fx { opacity: 0.5; text-decoration: line-through }`)
	under("x-fx", func(p parts) {
		if o := p.section.Opacity(); o != 1 {
			t.Errorf("opacity inherited by a part: %v", o)
		}
		if l, _ := p.linkBold.Deco(); l&css.DecoLineThrough == 0 {
			t.Error("the wrapper's line-through didn't propagate to a word in a link")
		}
		if l, _ := p.button.Deco(); l != 0 {
			t.Error("a decoration propagated into a button")
		}
	})

	// with only Slack's own rules (no plugin CSS) natives skip CSS entirely
	RemoveStyle("test:parts")
	if !Styles.Empty() {
		t.Fatal("the base sheet made the engine non-empty")
	}
	if CurrentScope().Active() {
		t.Fatal("an active scope without CSS")
	}
	if s := (Styled{}).Child(link, 0); s.Style() != nil {
		t.Fatal("the zero Styled has a styled child")
	}
}

var hostKey uint64 = 1 << 40

// fakeHost is a host-tree element (a plugin's wrapper div).
type fakeHost struct{ classes []string }

func (h *fakeHost) Tag() string { return "div" }
func (h *fakeHost) ID() string  { return "" }
func (h *fakeHost) HasClass(c string) bool {
	for _, x := range h.classes {
		if x == c {
			return true
		}
	}
	return false
}
func (h *fakeHost) Classes() []string               { return h.classes }
func (h *fakeHost) Attr(name string) (string, bool) { return "", false }
func (h *fakeHost) Parent() css.Element             { return nil }
func (h *fakeHost) Prev() css.Element               { return nil }
func (h *fakeHost) Index() (int, int)               { return 0, 0 }
func (h *fakeHost) TypeIndex() (int, int)           { return 0, 0 }
func (h *fakeHost) State() css.State                { return 0 }

// MessageLogger's stylesheet as the plugin installs it (Slick
// src/plugins/MessageLogger/index.ts css()): the deleted styles and the
// edit-history classes.
const mlCSSRed = `.slick-ml-deleted, .slick-ml-deleted * { color: #e01e5a !important; }`
const mlCSSOpacity = `.slick-ml-deleted { opacity: 0.5 !important; }`
const mlCSSEdits = `
      .slick-ml-edited-original { display: block; padding: 4px 20px 0 0; opacity: .62; white-space: pre-wrap; word-break: break-word; }
      .slick-ml-edited-original-line { display: block; }
      .slick-ml-edited-original s { text-decoration: line-through; }
      .slick-ml-edit-removed { color: #f27b9f; background: rgba(224, 30, 90, .25); text-decoration: line-through; }
      .slick-ml-edit-added { color: #75d9a5; background: rgba(46, 160, 100, .25); }
      .slick-ml-edited-marker { margin-left: 4px; font-size: .85em; opacity: .72; }`

// The row MessageLogger wraps (<div className="slick-ml-deleted"><Original/>)
// is a native under a host node: its parts see the div, red reaches them,
// and the div's opacity covers them. The edit history is the plugin's own
// markup and takes its classes' styles.
func TestMessageLoggerHostWrapper(t *testing.T) {
	defer func() { stack = stack[:0] }()
	row := Class("c-message_kit__background c-message_kit__message")
	sender := Class("c-link--button c-message__sender_button").Tag("button")
	section := Class("p-rich_text_section")
	link := Class("c-link").Tag("a")
	var got map[string]color.NRGBA
	var opac float32
	native := func(gtx layoutCtx, _ Props) dims {
		r := row.Enter(gtx, 0, 0)
		defer r.Leave()
		sb := sender.Enter(gtx, 0, 0)
		got["sender"] = sb.Color(color.NRGBA{})
		sb.Leave()
		sc := CurrentScope()
		sec := sc.Child(section, 0)
		got["text"] = sec.Color(color.NRGBA{})
		got["link"] = sec.Child(link, 0).Color(color.NRGBA{})
		opac = EffectiveOpacity()
		return dims{}
	}
	tree := `{"t":"div","k":1,"p":{"id":"wrap","className":"slick-ml-deleted"},"c":[{"t":"@","n":"MessageWrapper","k":2}]}`
	red := color.NRGBA{R: 0xe0, G: 0x1e, B: 0x5a, A: 0xff}

	SetStyle("test:ml", mlCSSRed+mlCSSEdits)
	defer RemoveStyle("test:ml")
	windowSize = image.Pt(1000, 800)
	v := NewView(1)
	for i := 0; i < 2; i++ {
		got = map[string]color.NRGBA{}
		v.Layout(testGtx(600, 400), mustTree(t, tree), "MessageWrapper", nil, native)
	}
	for _, k := range []string{"sender", "text", "link"} {
		if got[k] != red {
			t.Errorf("red style: the row's %s is %v, want #e01e5a", k, got[k])
		}
	}
	if len(stack) != 0 {
		t.Fatalf("%d elements left on the stack", len(stack))
	}

	SetStyle("test:ml", mlCSSOpacity+mlCSSEdits)
	v = NewView(2)
	for i := 0; i < 2; i++ {
		got = map[string]color.NRGBA{}
		v.Layout(testGtx(600, 400), mustTree(t, tree), "MessageWrapper", nil, native)
	}
	if b := byID(v, "wrap"); b == nil || b.st.Opacity != 0.5 {
		t.Fatalf("opacity style: the wrapper's opacity isn't 0.5")
	}
	if opac != 0.5 {
		t.Errorf("opacity style: the row is drawn at opacity %v, want 0.5", opac)
	}
	if got["text"] != (color.NRGBA{}) {
		t.Errorf("opacity style colored the text: %v", got["text"])
	}

	// the edit history: <div .slick-ml-edited-original><span …-line><span …-edit-removed/><span …-edit-added/></span></div>
	v = NewView(3)
	v.Layout(testGtx(600, 400), mustTree(t, `{"t":"div","k":1,"c":[
		{"t":"div","k":2,"p":{"id":"orig","className":"slick-ml-edited-original"},"c":[
			{"t":"span","k":3,"p":{"id":"line","className":"slick-ml-edited-original-line"},"c":[
				{"t":"span","k":4,"p":{"id":"rm","className":"slick-ml-edit-removed"},"c":["first"]},
				{"t":"span","k":5,"p":{"id":"add","className":"slick-ml-edit-added"},"c":["second"]}]}]},
		{"t":"span","k":6,"p":{"id":"mk","className":"slick-ml-edited-marker"},"c":["(edited)"]}]}`), "X", nil, nil)
	check := func(id string, f func(st *css.Style) bool, what string) {
		t.Helper()
		b := byID(v, id)
		if b == nil {
			t.Fatalf("no box %q", id)
		}
		if !f(&b.st) {
			t.Errorf("%s: %s (got color %v bg %v deco %b opacity %v size %v display %v)", id, what, b.st.Color, b.st.Background.Color, b.st.TextDecoration, b.st.Opacity, b.st.FontSize, b.st.Display)
		}
	}
	check("rm", func(s *css.Style) bool {
		return s.Color == (color.NRGBA{R: 0xf2, G: 0x7b, B: 0x9f, A: 0xff}) && s.Background.Color.R == 224 && s.Background.Color.A == 64 && s.TextDecoration&css.DecoLineThrough != 0
	}, "want #f27b9f on rgba(224,30,90,.25), struck through")
	check("add", func(s *css.Style) bool {
		return s.Color == (color.NRGBA{R: 0x75, G: 0xd9, B: 0xa5, A: 0xff}) && s.Background.Color.G == 160 && s.TextDecoration == 0
	}, "want #75d9a5 on rgba(46,160,100,.25)")
	check("orig", func(s *css.Style) bool { return s.Display == css.DisplayBlock && near(s.Opacity, 0.62) }, "want a block at opacity .62")
	check("line", func(s *css.Style) bool { return s.Display == css.DisplayBlock }, "want a block")
	check("mk", func(s *css.Style) bool { return near(s.Opacity, 0.72) && near(s.FontSize, 15*0.85) }, "want .85em at opacity .72")
}
