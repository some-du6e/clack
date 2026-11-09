package richview

import (
	"image"
	"image/color"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	rt "github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// All documents in this file are synthetic. Never paste real Slack messages
// here (see docs/NATIVE.md, scraping policy).

// ---------- fixtures ----------

var (
	shaperOnce sync.Once
	testShaper *text.Shaper
)

func sharedShaper() *text.Shaper {
	shaperOnce.Do(func() {
		testShaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	})
	return testShaper
}

// noImages never has anything decoded.
type noImages struct{}

func (noImages) Get(string, int) (paint.ImageOp, bool) { return paint.ImageOp{}, false }

// fakeImages reports ok once ready is set and records requested sizes.
type fakeImages struct {
	ready bool
	img   paint.ImageOp
	gets  int
	px    map[string]int
}

func newFakeImages() *fakeImages {
	return &fakeImages{
		img: paint.NewImageOp(image.NewRGBA(image.Rect(0, 0, 4, 4))),
		px:  map[string]int{},
	}
}

func (f *fakeImages) Get(url string, px int) (paint.ImageOp, bool) {
	f.gets++
	f.px[url] = px
	if !f.ready {
		return paint.ImageOp{}, false
	}
	return f.img, true
}

func newTestStyle() *Style {
	st := NewStyle(sharedShaper(), noImages{}, LightPalette())
	st.Face = "Go"
	st.Mono = "Go Mono"
	return st
}

func txt(s string) rt.Span { return rt.Span{Kind: rt.Text, Text: s} }

func brk() rt.Span { return rt.Span{Kind: rt.Break} }

func para(spans ...rt.Span) rt.Block { return rt.Block{Kind: rt.Paragraph, Spans: spans} }

func doc(bs ...rt.Block) rt.Doc { return rt.Doc{Blocks: bs} }

// layoutAt lays doc out at width w (1px per dp/sp) without painting.
func layoutAt(st *Style, d rt.Doc, o Options, w int) *View {
	v := New(d, o)
	v.relayout(st, w, 1, 1)
	return v
}

func testGtx(ops *op.Ops, w int) layout.Context {
	return layout.Context{
		Ops:         ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(w, 1000)),
	}
}

func textFrags(d *display) []*frag {
	var out []*frag
	for i := range d.frags {
		if f := &d.frags[i]; f.kind == fText && f.sh != nil {
			out = append(out, f)
		}
	}
	return out
}

func fragStrings(d *display) []string {
	var out []string
	for _, f := range textFrags(d) {
		out = append(out, f.sh.str)
	}
	return out
}

func findFrag(t *testing.T, d *display, s string) *frag {
	t.Helper()
	for _, f := range textFrags(d) {
		if f.sh.str == s {
			return f
		}
	}
	t.Fatalf("no text frag %q; have %q", s, fragStrings(d))
	return nil
}

func hasFrag(d *display, s string) bool {
	for _, f := range textFrags(d) {
		if f.sh.str == s {
			return true
		}
	}
	return false
}

func boxesOf(d *display, k boxKind) []box {
	var out []box
	for _, b := range d.boxes {
		if b.kind == k {
			out = append(out, b)
		}
	}
	return out
}

func center(f *frag) f32.Point {
	return f32.Pt(f.x+f.w/2, float32(f.top+f.bot)/2)
}

func rectCenter(r image.Rectangle) f32.Point {
	return f32.Pt(float32(r.Min.X+r.Max.X)/2, float32(r.Min.Y+r.Max.Y)/2)
}

// boxContainsFrag reports whether b covers f horizontally and its baseline.
func boxContainsFrag(b box, f *frag) bool {
	return float32(b.r.Min.X) <= f.x+0.5 && float32(b.r.Max.X) >= f.x+f.w-0.5 &&
		b.r.Min.Y < f.y && b.r.Max.Y > f.y
}

// ---------- inline flow ----------

func TestWrapNarrowWidth(t *testing.T) {
	st := newTestStyle()
	words := strings.Fields("alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu")
	src := strings.Join(words, " ")
	d := doc(para(txt(src)))

	wide := layoutAt(st, d, Options{}, 2000)
	if n := len(wide.d.lines); n != 1 {
		t.Fatalf("wide: %d lines, want 1", n)
	}

	const w = 120
	v := layoutAt(st, d, Options{}, w)
	if n := len(v.d.lines); n < 3 {
		t.Fatalf("narrow: %d lines, want >= 3", n)
	}
	got := fragStrings(&v.d)
	if strings.Join(got, " ") != src {
		t.Fatalf("frags %q do not reassemble %q", got, src)
	}
	for _, f := range textFrags(&v.d) {
		if f.x+f.w > w+0.5 {
			t.Errorf("frag %q ends at %.1f > width %d", f.sh.str, f.x+f.w, w)
		}
	}
	for i := 1; i < len(v.d.lines); i++ {
		if v.d.lines[i].top != v.d.lines[i-1].bot {
			t.Errorf("line %d top %d != previous bottom %d", i, v.d.lines[i].top, v.d.lines[i-1].bot)
		}
	}
	if string(v.d.plain) != src {
		t.Errorf("plain = %q, want %q", v.d.plain, src)
	}
	if v.d.size.X > w {
		t.Errorf("size.X = %d > %d", v.d.size.X, w)
	}
	if want := len(v.d.lines) * 22; v.d.size.Y != want {
		t.Errorf("size.Y = %d, want %d (22px lines)", v.d.size.Y, want)
	}
}

func TestLongWordForcedSplit(t *testing.T) {
	st := newTestStyle()
	long := strings.Repeat("x", 120)
	const w = 100
	v := layoutAt(st, doc(para(txt("see "+long))), Options{}, w)

	fs := textFrags(&v.d)
	if len(fs) < 3 {
		t.Fatalf("got %d frags, want the long word split over several lines", len(fs))
	}
	if fs[0].sh.str != "see" {
		t.Fatalf("first frag %q, want \"see\"", fs[0].sh.str)
	}
	var joined strings.Builder
	for i, f := range fs[1:] {
		if f.x+f.w > w+1 {
			t.Errorf("piece %d (%d runes) ends at %.1f > %d", i, f.sh.runes, f.x+f.w, w)
		}
		if f.x != 0 {
			t.Errorf("piece %d starts at x=%.1f, want 0 (own line)", i, f.x)
		}
		joined.WriteString(f.sh.str)
	}
	if joined.String() != long {
		t.Fatalf("pieces do not reassemble the word")
	}
	// Plain-text ranges of the pieces are contiguous and cover the word.
	for i := 2; i < len(fs); i++ {
		if fs[i].p0 != fs[i-1].p1 {
			t.Errorf("piece %d p0=%d, previous p1=%d", i, fs[i].p0, fs[i-1].p1)
		}
	}
	if last := fs[len(fs)-1]; int(last.p1) != len(v.d.plain) {
		t.Errorf("last piece p1=%d, plain len %d", last.p1, len(v.d.plain))
	}
	if string(v.d.plain) != "see "+long {
		t.Errorf("plain text changed by splitting")
	}
	// "see" stays alone on line 0; every piece has its own line.
	if len(v.d.lines) != len(fs) {
		t.Errorf("%d lines for %d frags", len(v.d.lines), len(fs))
	}
}

func TestHardBreaks(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, doc(para(txt("one"), brk(), txt("two"), brk())), Options{}, 400)
	if n := len(v.d.lines); n != 2 {
		t.Fatalf("%d lines, want 2 (trailing break dropped)", n)
	}
	if v.d.size.Y != 44 {
		t.Errorf("height %d, want 44", v.d.size.Y)
	}
	if !strings.HasPrefix(string(v.d.plain), "one\ntwo") {
		t.Errorf("plain = %q", v.d.plain)
	}
	one, two := findFrag(t, &v.d, "one"), findFrag(t, &v.d, "two")
	if one.line == two.line || two.top <= one.top {
		t.Errorf("break did not start a new line: one line %d top %d, two line %d top %d", one.line, one.top, two.line, two.top)
	}

	// Two breaks in a row leave an empty line in between.
	v = layoutAt(st, doc(para(txt("a"), brk(), brk(), txt("b"))), Options{}, 400)
	if n := len(v.d.lines); n != 3 {
		t.Fatalf("%d lines, want 3", n)
	}
	if v.d.size.Y != 52 {
		t.Errorf("height %d, want 52 (an empty line is 8px)", v.d.size.Y)
	}
	if string(v.d.plain) != "a\n\nb" {
		t.Errorf("plain = %q", v.d.plain)
	}
}

func TestMentionPills(t *testing.T) {
	cases := []struct {
		name string
		span rt.Span
		kind targetKind
	}{
		{"user", rt.Span{Kind: rt.User, Text: "@alice", ID: "U1"}, tUser},
		{"channel", rt.Span{Kind: rt.Channel, Text: "#general", ID: "C1"}, tChannel},
		{"usergroup", rt.Span{Kind: rt.Usergroup, Text: "@team", ID: "S1"}, tUsergroup},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := newTestStyle()
			p := st.Palette
			v := layoutAt(st, doc(para(txt("hi "), c.span, txt(" there"))), Options{}, 400)
			d := &v.d
			if len(d.targets) != 1 {
				t.Fatalf("%d targets, want 1", len(d.targets))
			}
			tg := d.targets[0]
			if tg.kind != c.kind || tg.id != c.span.ID {
				t.Fatalf("target = %+v, want kind %d id %q", tg, c.kind, c.span.ID)
			}
			f := findFrag(t, d, c.span.Text)
			if f.target != 0 {
				t.Errorf("mention frag target %d, want 0", f.target)
			}
			if f.col != p.MentionText {
				t.Errorf("mention color %v, want %v", f.col, p.MentionText)
			}
			if other := findFrag(t, d, "there"); other.target != -1 {
				t.Errorf("plain text got target %d", other.target)
			}
			var pill *box
			for i := range d.boxes {
				if d.boxes[i].kind == bFill && d.boxes[i].fill == p.MentionBg {
					pill = &d.boxes[i]
				}
			}
			if pill == nil {
				t.Fatalf("no pill box; boxes %+v", d.boxes)
			}
			if pill.target != 0 {
				t.Errorf("pill target %d, want 0", pill.target)
			}
			if pill.hover.A == 0 {
				t.Errorf("pill has no hover color")
			}
			if !boxContainsFrag(*pill, f) {
				t.Errorf("pill %v does not contain frag x=%.1f w=%.1f y=%d", pill.r, f.x, f.w, f.y)
			}
			found := false
			for _, h := range d.hits {
				if h.t == 0 && h.r == pill.r {
					found = true
				}
			}
			if !found {
				t.Errorf("no hit rect for the pill; hits %+v", d.hits)
			}
			if got := v.targetAt(center(f)); got != 0 {
				t.Errorf("targetAt(mention) = %d, want 0", got)
			}
			if got := v.targetAt(center(findFrag(t, d, "hi"))); got != -1 {
				t.Errorf("targetAt(plain word) = %d, want -1", got)
			}
			if string(d.plain) != "hi "+c.span.Text+" there" {
				t.Errorf("plain = %q", d.plain)
			}
		})
	}
}

func TestSelfMentionAndBroadcast(t *testing.T) {
	st := newTestStyle()
	p := st.Palette
	v := layoutAt(st, doc(para(rt.Span{Kind: rt.User, Text: "@me", ID: "U9", Self: true})), Options{}, 400)
	if len(boxesOf(&v.d, bFill)) != 1 || boxesOf(&v.d, bFill)[0].fill != p.SelfMentionBg {
		t.Errorf("self mention boxes %+v, want one SelfMentionBg fill", v.d.boxes)
	}
	if f := findFrag(t, &v.d, "@me"); f.col != p.MentionText {
		t.Errorf("self mention color %v, want %v (Slack keeps the link color)", f.col, p.MentionText)
	}

	v = layoutAt(st, doc(para(txt("ping "), rt.Span{Kind: rt.Broadcast, Text: "@here", ID: "here"})), Options{}, 400)
	bs := boxesOf(&v.d, bFill)
	if len(bs) != 1 || bs[0].fill != p.SelfMentionBg {
		t.Fatalf("broadcast boxes %+v, want one SelfMentionBg fill", v.d.boxes)
	}
	if bs[0].target != -1 || len(v.d.targets) != 0 || len(v.d.hits) != 0 {
		t.Errorf("broadcast must not be clickable: box target %d, targets %d, hits %d", bs[0].target, len(v.d.targets), len(v.d.hits))
	}
	if f := findFrag(t, &v.d, "@here"); f.target != -1 {
		t.Errorf("broadcast frag target %d", f.target)
	}
}

func TestInlineCode(t *testing.T) {
	st := newTestStyle()
	p := st.Palette
	v := layoutAt(st, doc(para(txt("run "), rt.Span{Kind: rt.Text, Text: "make test", Style: rt.Code}, txt(" now"))), Options{}, 400)
	bs := boxesOf(&v.d, bBorder)
	if len(bs) != 1 {
		t.Fatalf("%d bBorder boxes, want 1 (the code chip)", len(bs))
	}
	b := bs[0]
	if b.fill != p.CodeBg || b.stroke != p.CodeBorder || b.bw < 1 {
		t.Errorf("code box %+v", b)
	}
	mk, tst := findFrag(t, &v.d, "make"), findFrag(t, &v.d, "test")
	for _, f := range []*frag{mk, tst} {
		if f.size != 12 {
			t.Errorf("code frag %q size %d, want 12", f.sh.str, f.size)
		}
		if f.col != p.CodeText {
			t.Errorf("code frag %q color %v", f.sh.str, f.col)
		}
		if !boxContainsFrag(b, f) {
			t.Errorf("code box %v misses frag %q", b.r, f.sh.str)
		}
	}
	if run := findFrag(t, &v.d, "run"); run.size != 15 || run.col != p.Text {
		t.Errorf("body frag size %d color %v", run.size, run.col)
	}
	if boxContainsFrag(b, findFrag(t, &v.d, "now")) {
		t.Errorf("code box spills over following text")
	}
}

func TestLinkTarget(t *testing.T) {
	st := newTestStyle()
	p := st.Palette
	const url = "https://example.test/page"
	v := layoutAt(st, doc(para(txt("see "), rt.Span{Kind: rt.Link, Text: "the docs", URL: url})), Options{}, 400)
	if len(v.d.targets) != 1 {
		t.Fatalf("%d targets", len(v.d.targets))
	}
	tg := v.d.targets[0]
	if tg.kind != tLink || tg.url != url || !tg.underline {
		t.Errorf("target %+v", tg)
	}
	for _, s := range []string{"the", "docs"} {
		f := findFrag(t, &v.d, s)
		if f.target != 0 || f.col != p.Link {
			t.Errorf("link frag %q target %d color %v", s, f.target, f.col)
		}
		if v.targetAt(center(f)) != 0 {
			t.Errorf("targetAt(%q) != link", s)
		}
	}
	if len(v.d.boxes) != 0 {
		t.Errorf("plain link should not draw boxes: %+v", v.d.boxes)
	}

	// A code-styled link keeps the code color and gets the chip.
	v = layoutAt(st, doc(para(rt.Span{Kind: rt.Link, Text: "x.go", URL: url, Style: rt.Code})), Options{}, 400)
	if f := findFrag(t, &v.d, "x.go"); f.col != p.CodeText || f.target != 0 {
		t.Errorf("code link frag color %v target %d", f.col, f.target)
	}
	if len(boxesOf(&v.d, bBorder)) != 1 {
		t.Errorf("code link has no chip")
	}
}

func TestEmojiSizes(t *testing.T) {
	st := newTestStyle()
	em := rt.Span{Kind: rt.Emoji, ID: "smile", URL: "https://emoji.test/smile.png"}
	imgFrag := func(v *View) *frag {
		t.Helper()
		for i := range v.d.frags {
			if v.d.frags[i].kind == fImage {
				return &v.d.frags[i]
			}
		}
		t.Fatalf("no image frag")
		return nil
	}

	v := layoutAt(st, doc(para(txt("hi "), em)), Options{}, 400)
	f := imgFrag(v)
	if f.size != 22 || f.w != 22 || f.url != em.URL {
		t.Errorf("emoji frag size %d w %.1f url %q, want 22", f.size, f.w, f.url)
	}
	if !f.alt || string(v.d.plain) != "hi :smile:" {
		t.Errorf("emoji plain = %q alt=%v", v.d.plain, f.alt)
	}
	if f.y < f.top || f.y+f.size > f.bot+1 {
		t.Errorf("emoji %d..%d outside its line %d..%d", f.y, f.y+f.size, f.top, f.bot)
	}

	jd := doc(para(em, em))
	jd.Jumbo = true
	v = layoutAt(st, jd, Options{}, 400)
	f = imgFrag(v)
	if f.size != 32 {
		t.Errorf("jumbo emoji size %d, want 32", f.size)
	}
	if lh := v.d.lines[0].bot - v.d.lines[0].top; lh < 34 {
		t.Errorf("jumbo line height %d, want >= 34", lh)
	}

	v = layoutAt(st, doc(para(em)), Options{Compact: true}, 400)
	if f = imgFrag(v); f.size != 16 {
		t.Errorf("compact emoji size %d, want 16", f.size)
	}

	// No image URL: falls back to the :name: text.
	v = layoutAt(st, doc(para(rt.Span{Kind: rt.Emoji, ID: "partyparrot"})), Options{}, 400)
	findFrag(t, &v.d, ":partyparrot:")
}

// ---------- blocks ----------

func TestPreformatted(t *testing.T) {
	st := newTestStyle()
	p := st.Palette
	const w = 300
	v := layoutAt(st, doc(rt.Block{Kind: rt.Preformatted, Spans: []rt.Span{txt("  indented"), brk(), txt("x")}}), Options{}, w)
	bs := boxesOf(&v.d, bBorder)
	if len(bs) != 1 {
		t.Fatalf("%d pre boxes", len(bs))
	}
	if bs[0].fill != p.PreBg || bs[0].stroke != p.PreBorder {
		t.Errorf("pre box colors %+v", bs[0])
	}
	if want := image.Rect(0, 0, w, 2*18+16); bs[0].r != want {
		t.Errorf("pre box %v, want %v (full width)", bs[0].r, want)
	}
	ind, x := findFrag(t, &v.d, "indented"), findFrag(t, &v.d, "x")
	if x.x != 8 {
		t.Errorf("pre text x %.1f, want 8 (padding)", x.x)
	}
	if ind.x <= 8 {
		t.Errorf("leading spaces were not preserved: x %.1f", ind.x)
	}
	if ind.size != 12 {
		t.Errorf("pre font size %d, want 12", ind.size)
	}
	if v.d.size.Y != 52 {
		t.Errorf("height %d, want 52", v.d.size.Y)
	}
}

func TestQuoteBar(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, doc(rt.Block{Kind: rt.Quote, Spans: []rt.Span{txt("quoted text")}}), Options{}, 300)
	bs := boxesOf(&v.d, bFill)
	if len(bs) != 1 || bs[0].fill != st.Palette.QuoteBar {
		t.Fatalf("quote boxes %+v", v.d.boxes)
	}
	if want := image.Rect(0, 0, 4, 22); bs[0].r != want {
		t.Errorf("quote bar %v, want %v", bs[0].r, want)
	}
	if f := findFrag(t, &v.d, "quoted"); f.x != 16 {
		t.Errorf("quote text x %.1f, want 16", f.x)
	}
}

func TestListBullets(t *testing.T) {
	kinds := []boxKind{bCircle, bRing, bSquare, bCircle}
	for indent, want := range kinds {
		st := newTestStyle()
		b := rt.Block{Kind: rt.List, Indent: indent, Items: [][]rt.Span{{txt("first")}, {txt("second")}}}
		v := layoutAt(st, doc(b), Options{}, 400)
		ms := boxesOf(&v.d, want)
		if len(ms) != 2 || len(v.d.boxes) != 2 {
			t.Errorf("indent %d: boxes %+v, want 2 of kind %d", indent, v.d.boxes, want)
			continue
		}
		first, second := findFrag(t, &v.d, "first"), findFrag(t, &v.d, "second")
		if wantX := float32(28 * (indent + 1)); first.x != wantX {
			t.Errorf("indent %d: text x %.1f, want %.1f", indent, first.x, wantX)
		}
		if float32(ms[0].r.Max.X) > first.x || ms[0].r.Min.X < 0 {
			t.Errorf("indent %d: marker %v not left of text at %.1f", indent, ms[0].r, first.x)
		}
		if ms[1].r.Min.Y <= ms[0].r.Min.Y || second.top <= first.top {
			t.Errorf("indent %d: second item not below first", indent)
		}
		if string(v.d.plain) != "first\nsecond" {
			t.Errorf("indent %d: plain %q", indent, v.d.plain)
		}
	}
}

func TestListOrderedLabels(t *testing.T) {
	cases := []struct {
		indent, offset int
		want           []string
	}{
		{0, 0, []string{"1.", "2.", "3."}},
		{1, 0, []string{"a.", "b.", "c."}},
		{2, 0, []string{"i.", "ii.", "iii."}},
		{0, 4, []string{"5.", "6.", "7."}},
		{3, 0, []string{"1.", "2.", "3."}},
	}
	for _, c := range cases {
		st := newTestStyle()
		b := rt.Block{Kind: rt.List, Ordered: true, Indent: c.indent, Offset: c.offset,
			Items: [][]rt.Span{{txt("one")}, {txt("two")}, {txt("three")}}}
		v := layoutAt(st, doc(b), Options{}, 400)
		var labels []string
		var labelFrags []*frag
		for _, f := range textFrags(&v.d) {
			if f.line == -1 {
				labels = append(labels, f.sh.str)
				labelFrags = append(labelFrags, f)
			}
		}
		if strings.Join(labels, " ") != strings.Join(c.want, " ") {
			t.Errorf("indent %d offset %d: labels %q, want %q", c.indent, c.offset, labels, c.want)
			continue
		}
		textX := findFrag(t, &v.d, "one").x
		for _, f := range labelFrags {
			if f.x+f.w > textX {
				t.Errorf("label %q ends at %.1f past text at %.1f", f.sh.str, f.x+f.w, textX)
			}
			if f.p0 != -1 || f.p1 != -1 {
				t.Errorf("label %q is part of plain text (%d..%d)", f.sh.str, f.p0, f.p1)
			}
		}
		if string(v.d.plain) != "one\ntwo\nthree" {
			t.Errorf("plain %q", v.d.plain)
		}
	}
}

func TestToAlphaToRoman(t *testing.T) {
	alpha := map[int]string{0: "", 1: "a", 2: "b", 26: "z", 27: "aa", 28: "ab", 52: "az", 53: "ba", 702: "zz", 703: "aaa"}
	for n, want := range alpha {
		if got := toAlpha(n); got != want {
			t.Errorf("toAlpha(%d) = %q, want %q", n, got, want)
		}
	}
	roman := map[int]string{0: "", 1: "i", 3: "iii", 4: "iv", 5: "v", 9: "ix", 14: "xiv", 40: "xl", 90: "xc", 400: "cd", 1994: "mcmxciv", 3999: "mmmcmxcix"}
	for n, want := range roman {
		if got := toRoman(n); got != want {
			t.Errorf("toRoman(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestHeader(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, doc(rt.Block{Kind: rt.Header, Spans: []rt.Span{txt("Big title")}}), Options{}, 400)
	for _, s := range []string{"Big", "title"} {
		if f := findFrag(t, &v.d, s); f.size != 15 {
			t.Errorf("header frag %q size %d, want 15", s, f.size)
		}
	}
	if v.d.size.Y != 22 {
		t.Errorf("header height %d, want 22", v.d.size.Y)
	}
	body := layoutAt(st, doc(para(txt("Big title"))), Options{}, 400)
	if findFrag(t, &v.d, "title").w <= findFrag(t, &body.d, "title").w {
		t.Errorf("header text is not wider than body text")
	}
}

func TestDivider(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, doc(rt.Block{Kind: rt.Divider}), Options{}, 300)
	if len(v.d.boxes) != 1 {
		t.Fatalf("boxes %+v", v.d.boxes)
	}
	b := v.d.boxes[0]
	if b.kind != bFill || b.fill != st.Palette.Divider || b.r != image.Rect(0, 8, 300, 9) {
		t.Errorf("divider box %+v", b)
	}
	if v.d.size.Y != 17 {
		t.Errorf("divider height %d, want 17", v.d.size.Y)
	}
}

func TestContext(t *testing.T) {
	st := newTestStyle()
	b := rt.Block{Kind: rt.Context, Image: &rt.Image{URL: "https://img.test/icon.png"}, Spans: []rt.Span{txt("context words")}}
	v := layoutAt(st, doc(b), Options{}, 400)
	if len(v.d.imgs) != 1 {
		t.Fatalf("%d imgs", len(v.d.imgs))
	}
	if r := v.d.imgs[0].r; r.Dx() != 20 || r.Dy() != 20 || r.Min.X != 0 {
		t.Errorf("context icon %v, want 20x20 at x=0", r)
	}
	f := findFrag(t, &v.d, "context")
	if f.x != 24 {
		t.Errorf("context text x %.1f, want 24", f.x)
	}
	if f.size != 13 || f.col != st.Palette.Text2 {
		t.Errorf("context text size %d color %v", f.size, f.col)
	}
	if v.d.size.Y < 18 {
		t.Errorf("context height %d < 18", v.d.size.Y)
	}
}

func TestSectionFieldsTwoColumns(t *testing.T) {
	st := newTestStyle()
	const w = 400
	b := rt.Block{Kind: rt.Section, Spans: []rt.Span{txt("Summary")},
		Fields: [][]rt.Span{{txt("f1")}, {txt("f2")}, {txt("f3")}, {txt("f4")}}}
	v := layoutAt(st, doc(b), Options{}, w)
	d := &v.d
	s, f1, f2, f3, f4 := findFrag(t, d, "Summary"), findFrag(t, d, "f1"), findFrag(t, d, "f2"), findFrag(t, d, "f3"), findFrag(t, d, "f4")
	colW := (w - 12) / 2
	if f1.x != 0 || f3.x != 0 {
		t.Errorf("left column x: f1 %.1f f3 %.1f, want 0", f1.x, f3.x)
	}
	if want := float32(colW + 12); f2.x != want || f4.x != want {
		t.Errorf("right column x: f2 %.1f f4 %.1f, want %.1f", f2.x, f4.x, want)
	}
	if f1.top != f2.top || f3.top != f4.top {
		t.Errorf("fields of one row not aligned: %d/%d %d/%d", f1.top, f2.top, f3.top, f4.top)
	}
	if !(s.top < f1.top && f1.top < f3.top) {
		t.Errorf("rows out of order: summary %d row1 %d row2 %d", s.top, f1.top, f3.top)
	}
	if string(d.plain) != "Summary\nf1\nf2\nf3\nf4" {
		t.Errorf("plain %q", d.plain)
	}
}

func TestSectionAccessoryImage(t *testing.T) {
	st := newTestStyle()
	const w = 400
	b := rt.Block{Kind: rt.Section,
		Spans: []rt.Span{txt("some section text that is long enough to wrap around a couple of times at this width")},
		Image: &rt.Image{URL: "https://img.test/acc.png", Alt: "acc"}}
	v := layoutAt(st, doc(b), Options{}, w)
	if len(v.d.imgs) != 1 {
		t.Fatalf("%d imgs", len(v.d.imgs))
	}
	if r := v.d.imgs[0].r; r != image.Rect(w-88, 0, w, 88) {
		t.Errorf("accessory %v, want %v", r, image.Rect(w-88, 0, w, 88))
	}
	for _, f := range textFrags(&v.d) {
		if f.x+f.w > w-88-12+0.5 {
			t.Errorf("text %q at %.1f overlaps the accessory", f.sh.str, f.x+f.w)
		}
	}
	tg := v.d.imgs[0].target
	if tg < 0 || v.d.targets[tg].kind != tImage || v.d.targets[tg].img != b.Image {
		t.Errorf("accessory target %d", tg)
	}
	if v.d.size.Y < 88 {
		t.Errorf("height %d < accessory 88", v.d.size.Y)
	}
}

// buttonBoxes returns the boxes that belong to Block Kit buttons.
func buttonBoxes(d *display) []box {
	var out []box
	for _, b := range d.boxes {
		if b.target >= 0 && d.targets[b.target].kind == tButton {
			out = append(out, b)
		}
	}
	return out
}

func TestActionsButtonsWrap(t *testing.T) {
	st := newTestStyle()
	p := st.Palette
	btns := []rt.Button{
		{Text: "Approve request", Style: "primary", ActionID: "a1"},
		{Text: "Deny", Style: "danger", ActionID: "a2"},
		{Text: "Open dashboard", ActionID: "a3"},
		{Text: "Snooze for later", ActionID: "a4"},
		{Text: "More options", ActionID: "a5"},
	}
	b := rt.Block{Kind: rt.Actions, Buttons: btns}

	wide := layoutAt(st, doc(b), Options{}, 2000)
	if bs := buttonBoxes(&wide.d); len(bs) != len(btns) {
		t.Fatalf("%d button boxes, want %d", len(bs), len(btns))
	}
	rows := map[int]bool{}
	for _, bx := range buttonBoxes(&wide.d) {
		rows[bx.r.Min.Y] = true
	}
	if len(rows) != 1 || wide.d.size.Y != 28 {
		t.Errorf("wide: %d rows height %d, want 1 row of 28", len(rows), wide.d.size.Y)
	}

	const w = 260
	v := layoutAt(st, doc(b), Options{}, w)
	bs := buttonBoxes(&v.d)
	if len(bs) != len(btns) {
		t.Fatalf("%d button boxes", len(bs))
	}
	rows = map[int]bool{}
	for i, bx := range bs {
		rows[bx.r.Min.Y] = true
		if bx.r.Max.X > w {
			t.Errorf("button %d %v overflows %d", i, bx.r, w)
		}
		if bx.r.Dy() != 28 {
			t.Errorf("button %d height %d", i, bx.r.Dy())
		}
		tg := v.d.targets[bx.target]
		if tg.btn == nil || tg.btn.ActionID != btns[i].ActionID {
			t.Errorf("button %d target %+v", i, tg)
		}
	}
	if len(rows) < 2 {
		t.Errorf("buttons did not wrap at width %d", w)
	}
	if want := len(rows)*28 + (len(rows)-1)*8; v.d.size.Y != want {
		t.Errorf("height %d, want %d for %d rows", v.d.size.Y, want, len(rows))
	}
	if bs[0].kind != bFill || bs[0].fill != p.Primary {
		t.Errorf("primary button %+v", bs[0])
	}
	if bs[1].kind != bFill || bs[1].fill != p.Danger {
		t.Errorf("danger button %+v", bs[1])
	}
	if bs[2].kind != bBorder || bs[2].stroke != p.ButtonBorder || bs[2].hover.A == 0 {
		t.Errorf("default button %+v", bs[2])
	}
	if f := findFrag(t, &v.d, "Approve"); f.col != p.PrimaryText {
		t.Errorf("primary label color %v", f.col)
	}
}

// ---------- attachments and unfurls ----------

func TestParseColor(t *testing.T) {
	def := rgb(1, 2, 3)
	cases := []struct {
		in   string
		want color.NRGBA
	}{
		{"good", rgb(46, 182, 125)},
		{"warning", rgb(236, 178, 46)},
		{"danger", rgb(224, 30, 90)},
		{"#abc", rgb(0xaa, 0xbb, 0xcc)},
		{"abc", rgb(0xaa, 0xbb, 0xcc)},
		{"ff0000", rgb(255, 0, 0)},
		{"#FF0000", rgb(255, 0, 0)},
		{"#1d9bd1", rgb(0x1d, 0x9b, 0xd1)},
		{"", def},
		{"nope", def},
		{"#zzzzzz", def},
		{"#12345", def},
		{"#1234567", def},
	}
	for _, c := range cases {
		if got := parseColor(c.in, def); got != c.want {
			t.Errorf("parseColor(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestAttachmentColorBar(t *testing.T) {
	st := newTestStyle()
	const title = "https://example.test/title"
	att := &rt.Attachment{
		Color:     "#ff0000",
		Title:     []rt.Span{txt("Build passed")},
		TitleLink: title,
		Text:      []rt.Block{para(txt("all checks green"))},
	}
	v := layoutAt(st, doc(rt.Block{Kind: rt.AttachmentBlock, Att: att}), Options{}, 400)
	if len(v.d.boxes) == 0 {
		t.Fatalf("no boxes")
	}
	bar := v.d.boxes[0]
	if bar.fill != rgb(255, 0, 0) || bar.r.Dx() != 4 || bar.r.Min.X != 0 {
		t.Errorf("bar %+v, want 4px red at x=0", bar)
	}
	if bar.r.Dy() != v.d.size.Y || bar.r.Dy() < 44 {
		t.Errorf("bar height %d, block height %d", bar.r.Dy(), v.d.size.Y)
	}
	tf := findFrag(t, &v.d, "Build")
	if tf.x != 16 || tf.col != st.Palette.Link {
		t.Errorf("title frag x %.1f color %v", tf.x, tf.col)
	}
	if tf.target < 0 || v.d.targets[tf.target].url != title || v.d.targets[tf.target].kind != tLink {
		t.Errorf("title target %d", tf.target)
	}
	if body := findFrag(t, &v.d, "checks"); body.top <= tf.top {
		t.Errorf("body not below title")
	}

	att2 := *att
	att2.Color = ""
	v = layoutAt(st, doc(rt.Block{Kind: rt.AttachmentBlock, Att: &att2}), Options{}, 400)
	if v.d.boxes[0].fill != st.Palette.Placeholder {
		t.Errorf("default bar color %v, want Placeholder (#eaeaea)", v.d.boxes[0].fill)
	}
}

func TestUnfurlCard(t *testing.T) {
	st := newTestStyle()
	const link = "https://example.test/article"
	att := &rt.Attachment{
		IsUnfurl:    true,
		Title:       []rt.Span{txt("An example article")},
		TitleLink:   link,
		Text:        []rt.Block{para(txt("A short synthetic description."))},
		ServiceName: "Example",
		Thumb:       &rt.Image{URL: "https://img.test/thumb.png", W: 200, H: 200},
	}
	v := layoutAt(st, doc(rt.Block{Kind: rt.AttachmentBlock, Att: att}), Options{}, 800)
	d := &v.d
	var card *box
	for i := range d.boxes {
		if d.boxes[i].kind == bBorder && d.boxes[i].radius == 12 {
			card = &d.boxes[i]
			break
		}
	}
	if card == nil {
		t.Fatalf("no rounded card; boxes %+v", d.boxes)
	}
	if card.r.Min != (image.Point{}) || card.r.Dx() != 716 {
		t.Errorf("card %v, want width capped at 716", card.r)
	}
	if card.r.Dy() < 84 {
		t.Errorf("card height %d < 84 with a thumbnail", card.r.Dy())
	}
	if card.target < 0 || d.targets[card.target].kind != tCard || d.targets[card.target].url != link {
		t.Fatalf("card target %d", card.target)
	}
	if card.hover.A == 0 {
		t.Errorf("card has no hover border")
	}
	if len(d.imgs) != 1 {
		t.Fatalf("%d imgs, want the thumbnail", len(d.imgs))
	}
	th := d.imgs[0]
	if th.url != att.Thumb.URL || th.r.Max.X != card.r.Max.X-1 || th.r.Min.X != card.r.Max.X-106 {
		t.Errorf("thumbnail %v not on the right edge of %v", th.r, card.r)
	}
	if th.radii[0] != 0 || th.radii[3] != 0 || th.radii[1] == 0 {
		t.Errorf("thumbnail radii %v, want rounded on the right only", th.radii)
	}
	for _, f := range textFrags(d) {
		if f.x+f.w > float32(th.r.Min.X) {
			t.Errorf("text %q overlaps the thumbnail", f.sh.str)
		}
	}
	if f := findFrag(t, d, "article"); f.target != card.target {
		t.Errorf("title target %d, want card %d", f.target, card.target)
	}
	if v.targetAt(rectCenter(th.r)) != card.target {
		t.Errorf("click on thumbnail does not open the card")
	}
	findFrag(t, d, "Example")
}

// ---------- files ----------

func fileBlock(f *rt.File) rt.Block { return rt.Block{Kind: rt.FileBlock, File: f} }

func TestFileImage(t *testing.T) {
	st := newTestStyle()
	f := &rt.File{Name: "pic.png", IsImage: true, Thumb: &rt.Image{URL: "https://files.test/pic.png", W: 800, H: 400}}
	v := layoutAt(st, doc(fileBlock(f)), Options{}, 600)
	if len(v.d.imgs) != 1 {
		t.Fatalf("%d imgs", len(v.d.imgs))
	}
	im := v.d.imgs[0]
	// Slack's gallery v3 (getDimensions, one file): 600 wide → gallery
	// 574 (medium), landscape cap 154 → 308x154, with no name row above.
	if im.r != image.Rect(0, 0, 308, 154) {
		t.Errorf("image %v, want 308x154 at the top (Slack gallery size, no name row)", im.r)
	}
	if im.target < 0 || v.d.targets[im.target].kind != tFile || v.d.targets[im.target].file != f {
		t.Errorf("image target %d", im.target)
	}
	var overlay bool
	for _, b := range v.d.boxes {
		if b.kind == bBorder && b.layer == 1 && b.r == im.r {
			overlay = true
		}
	}
	if !overlay {
		t.Errorf("no border overlay over the image")
	}
	if len(boxesOf(&v.d, bCaret)) != 0 {
		t.Errorf("a name-row caret above a gallery image")
	}
	if string(v.d.plain) != "pic.png" {
		t.Errorf("plain %q", v.d.plain)
	}
	if v.d.size != image.Pt(308, 154) {
		t.Errorf("size %v", v.d.size)
	}
}

func TestGalleryBox(t *testing.T) {
	l := &lay{scale: 1}
	cases := []struct {
		iw, ih, w int
		bw, bh    int
	}{
		{1024, 393, 962, 612, 235}, // wide screenshot, large gallery: height cap 235
		{1024, 161, 962, 934, 147}, // very wide: nearly the full gallery (Slack: 934)
		{393, 1024, 962, 135, 354}, // portrait cap
		{500, 500, 962, 314, 314},  // square cap
		{100, 80, 962, 108, 108},   // small image: 108 minimum box
		{1024, 393, 600, 401, 154}, // medium gallery (574)
		{1024, 393, 60, 60, 108},   // never wider than the message
	}
	for _, c := range cases {
		if bw, bh := galleryBox(c.iw, c.ih, c.w, l); bw != c.bw || bh != c.bh {
			t.Errorf("galleryBox(%d,%d,%d) = %d,%d want %d,%d", c.iw, c.ih, c.w, bw, bh, c.bw, c.bh)
		}
	}
}

func TestFitSize(t *testing.T) {
	l := &lay{scale: 1}
	cases := []struct {
		iw, ih, mw, mh int
		w, h           int
	}{
		{100, 50, 360, 360, 100, 50},
		{800, 400, 360, 360, 360, 180},
		{400, 800, 360, 360, 180, 360},
		{0, 0, 300, 300, 300, 240},
		{1000, 1, 100, 100, 100, 1},
	}
	for _, c := range cases {
		if w, h := fitSize(c.iw, c.ih, c.mw, c.mh, l); w != c.w || h != c.h {
			t.Errorf("fitSize(%d,%d,%d,%d) = %d,%d want %d,%d", c.iw, c.ih, c.mw, c.mh, w, h, c.w, c.h)
		}
	}
	l2 := &lay{scale: 2}
	if w, h := fitSize(100, 50, 1000, 1000, l2); w != 200 || h != 100 {
		t.Errorf("scaled fitSize = %d,%d want 200,100", w, h)
	}
}

// galleryFiles makes n image file blocks with the given original sizes
// (cycled).
func galleryFiles(n int, sizes ...[2]int) ([]*rt.File, []rt.Block) {
	var files []*rt.File
	var blocks []rt.Block
	for i := 0; i < n; i++ {
		sz := sizes[i%len(sizes)]
		name := string(rune('a'+i)) + ".png"
		f := &rt.File{Name: name, IsImage: true, OrigW: sz[0], OrigH: sz[1],
			Thumb: &rt.Image{URL: "https://files.test/" + name, W: sz[0], H: sz[1]}}
		files = append(files, f)
		blocks = append(blocks, fileBlock(f))
	}
	return files, blocks
}

func imgRects(v *View) []image.Rectangle {
	var out []image.Rectangle
	for _, im := range v.d.imgs {
		out = append(out, im.r)
	}
	return out
}

// TestMediaGallery checks Slack's gallery v3 (seRe getDimensions /
// getProportionalWidthV3 / getGalleryV3MaxVisibleFiles): two files share
// a height with widths grown from 108, three or four fill one row by their
// v3 aspects, and past the visible limit the last tile reads "+ N".
func TestMediaGallery(t *testing.T) {
	st := newTestStyle()
	land := [2]int{1000, 600}

	// three landscape files at 600 (gallery 574): one row of 186x140
	files, blocks := galleryFiles(3, land)
	v := layoutAt(st, doc(blocks...), Options{}, 600)
	want := []image.Rectangle{image.Rect(0, 0, 186, 140), image.Rect(194, 0, 380, 140), image.Rect(388, 0, 574, 140)}
	if got := imgRects(v); !reflect.DeepEqual(got, want) {
		t.Errorf("3 tiles %v, want %v", got, want)
	}
	for i, im := range v.d.imgs {
		if tg := v.d.targets[im.target]; tg.kind != tFile || tg.file != files[i] {
			t.Errorf("tile %d target %+v", i, tg)
		}
	}
	for _, n := range []string{"a.png", "b.png", "c.png"} {
		if !strings.Contains(string(v.d.plain), n) {
			t.Errorf("plain %q lacks %q", v.d.plain, n)
		}
	}
	if v.d.size.Y != 140 {
		t.Errorf("height %d", v.d.size.Y)
	}

	// at 400 (gallery 374) only two fit: 183x110 each, the second "+ 2"
	v = layoutAt(st, doc(blocks...), Options{}, 400)
	want = []image.Rectangle{image.Rect(0, 0, 183, 110), image.Rect(191, 0, 374, 110)}
	if got := imgRects(v); !reflect.DeepEqual(got, want) {
		t.Errorf("2 of 3 tiles %v, want %v", got, want)
	}
	plus := findFrag(t, &v.d, "+")
	if plus.x < 191 || plus.y > 110 {
		t.Errorf("\"+ 2\" at %v,%v, want on the second tile", plus.x, plus.y)
	}
	findFrag(t, &v.d, "2")
	var scrim bool
	for _, b := range boxesOf(&v.d, bFill) {
		if b.layer == 1 && b.r == want[1] {
			scrim = true
		}
	}
	if !scrim {
		t.Errorf("no overflow scrim over the last tile")
	}
	// the hidden file still opens from the tile underneath
	if tg := v.d.targets[v.d.imgs[1].target]; tg.file != files[1] {
		t.Errorf("overflow tile opens %v", tg.file)
	}

	// four at 1000 (gallery 936): 228x171 each
	_, blocks = galleryFiles(4, land)
	v = layoutAt(st, doc(blocks...), Options{}, 1000)
	if got := imgRects(v); len(got) != 4 || got[0] != image.Rect(0, 0, 228, 171) || got[3] != image.Rect(708, 0, 936, 171) {
		t.Errorf("4 tiles %v", got)
	}

	// six at 1000: four tiles, the fourth "+ 3"
	_, blocks = galleryFiles(6, land)
	v = layoutAt(st, doc(blocks...), Options{}, 1000)
	if n := len(v.d.imgs); n != 4 {
		t.Errorf("%d tiles for 6 files, want 4", n)
	}
	findFrag(t, &v.d, "+")
	findFrag(t, &v.d, "3")

	// a portrait among landscapes narrows to its 3/4 aspect
	_, blocks = galleryFiles(3, land, [2]int{600, 1000}, land)
	v = layoutAt(st, doc(blocks...), Options{}, 1000)
	got := imgRects(v)
	if len(got) != 3 || got[1].Dx() >= got[0].Dx() || got[0].Dy() != got[1].Dy() {
		t.Errorf("mixed aspects %v: the portrait should be narrower at the same height", got)
	}

	// two at 1000: shared height under the large landscape cap (235)
	_, blocks = galleryFiles(2, [2]int{1600, 900}, [2]int{800, 800})
	v = layoutAt(st, doc(blocks...), Options{}, 1000)
	got = imgRects(v)
	if len(got) != 2 || got[0].Dy() != got[1].Dy() || got[0].Dy() > 235 || got[0].Dy() < 108 {
		t.Errorf("pair %v", got)
	}
	if got[1].Min.X-got[0].Max.X != 8 {
		t.Errorf("pair gap %d, want 8", got[1].Min.X-got[0].Max.X)
	}
}

func TestGalleryPair(t *testing.T) {
	// getDimensions with two 1000x600 files in a 374 gallery (tiny): cap
	// 115, each grows 75 from 108 → 183, height 183/(5/3) = 109.8
	ws, h := galleryPair(374, [][2]float64{{1000, 600}, {1000, 600}})
	if ws[0] != 183 || ws[1] != 183 || math.Abs(h-109.8) > 1e-9 {
		t.Errorf("pair %v x %v", ws, h)
	}
	if got := galleryMaxVisible(599); got != 3 {
		t.Errorf("maxVisible(599) = %d", got)
	}
	if got := galleryMaxVisible(223); got != 1 {
		t.Errorf("maxVisible(223) = %d", got)
	}
	ws, h = galleryRow(936, [][2]float64{{4, 3}, {3, 4}, {1, 1}})
	if math.Abs(ws[0]+ws[1]+ws[2]+16-936) > 1e-9 || math.Abs(ws[0]/h-4.0/3) > 1e-9 || math.Abs(ws[1]/h-0.75) > 1e-9 {
		t.Errorf("row %v x %v", ws, h)
	}
}

func TestVideoCard(t *testing.T) {
	st := newTestStyle()
	f := &rt.File{Name: "clip.mp4", IsVideo: true, DurationMS: 83000, OrigW: 640, OrigH: 360, Thumb: &rt.Image{URL: "https://files.test/clip.jpg", W: 640, H: 360}}
	v := layoutAt(st, doc(fileBlock(f)), Options{}, 600)
	if len(v.d.imgs) != 1 {
		t.Fatalf("%d imgs", len(v.d.imgs))
	}
	// the single-file box (574 gallery, medium landscape cap 154): 273x154,
	// the poster contained in it
	r := v.d.imgs[0].r
	if r.Min != image.Pt(0, 0) || r.Dx() != 273 || r.Dy() < 153 || r.Dy() > 154 {
		t.Errorf("video poster %v, want 273x154 at the top", r)
	}
	if n := len(boxesOf(&v.d, bCircle)); n != 0 {
		t.Errorf("%d play circles; Slack's gallery video has the bottom-left pill", n)
	}
	plays := boxesOf(&v.d, bPlay)
	if len(plays) != 1 || plays[0].layer != 1 || plays[0].r.Min.X < 12 || plays[0].r.Max.Y > 154-12 {
		t.Fatalf("play glyph %+v", plays)
	}
	d := findFrag(t, &v.d, "1:23")
	if d.y < 154-12-28 || d.x < 12+16 {
		t.Errorf("duration at %v,%v, want in the pill 12px from the bottom-left", d.x, d.y)
	}
	if !strings.Contains(string(v.d.plain), "clip.mp4") {
		t.Errorf("plain %q", v.d.plain)
	}

	// Without a poster: the base-ter tile, still clickable, "Video"
	f2 := &rt.File{Name: "raw.mov", IsVideo: true}
	v = layoutAt(st, doc(fileBlock(f2)), Options{}, 600)
	var ph bool
	for _, b := range v.d.boxes {
		if b.kind == bFill && b.r == image.Rect(0, 0, 108, 108) && b.fill == st.Palette.Placeholder {
			ph = true
		}
	}
	if !ph {
		t.Errorf("no 108px placeholder tile; boxes %+v", v.d.boxes)
	}
	if v.targetAt(f32.Pt(54, 54)) < 0 {
		t.Errorf("placeholder is not clickable")
	}
}

func TestAudioCard(t *testing.T) {
	st := newTestStyle()
	f := &rt.File{Title: "Voice memo", IsAudio: true, DurationMS: 5000}
	v := layoutAt(st, doc(fileBlock(f)), Options{}, 600)
	cards := boxesOf(&v.d, bBorder)
	if len(cards) != 1 || cards[0].r != image.Rect(0, 0, 426, 64) || cards[0].radius != 12 {
		t.Fatalf("audio card %+v", cards)
	}
	if tg := cards[0].target; tg < 0 || v.d.targets[tg].kind != tFile || v.d.targets[tg].file != f {
		t.Errorf("card target %d", cards[0].target)
	}
	if cs := boxesOf(&v.d, bCircle); len(cs) != 1 || cs[0].fill != st.Palette.Link {
		t.Errorf("play circle %+v", cs)
	}
	if len(boxesOf(&v.d, bPlay)) != 1 {
		t.Errorf("no play triangle")
	}
	findFrag(t, &v.d, "0:05")
	if name := findFrag(t, &v.d, "memo"); name.target != cards[0].target {
		t.Errorf("name target %d", name.target)
	}
	if v.d.size.Y != 64 {
		t.Errorf("height %d", v.d.size.Y)
	}

	v = layoutAt(st, doc(fileBlock(&rt.File{Name: "x.mp3", IsAudio: true})), Options{}, 300)
	findFrag(t, &v.d, "Audio")
	if r := boxesOf(&v.d, bBorder)[0].r; r.Dx() != 300 {
		t.Errorf("audio card %v not clamped to width 300", r)
	}
}

func TestSnippetCard(t *testing.T) {
	st := newTestStyle()
	f := &rt.File{Name: "main.go", IsSnippet: true, PrettyType: "Go", Lines: 3, Preview: "package main\n\nfunc main() {}\n"}
	v := layoutAt(st, doc(fileBlock(f)), Options{}, 600)
	d := &v.d
	var card *box
	for i := range d.boxes {
		if d.boxes[i].kind == bBorder {
			card = &d.boxes[i]
			break
		}
	}
	if card == nil {
		t.Fatalf("no card")
	}
	if card.r.Min != (image.Point{}) || card.r.Dx() != 560 || card.r.Max.Y != d.size.Y {
		t.Errorf("snippet card %v, size %v", card.r, d.size)
	}
	var pre *box
	for i := range d.boxes {
		if d.boxes[i].perCorner && d.boxes[i].fill == st.Palette.PreBg {
			pre = &d.boxes[i]
		}
	}
	if pre == nil {
		t.Fatalf("no preview background")
	}
	if pre.corners[0] != 0 || pre.corners[2] == 0 {
		t.Errorf("preview corners %v, want only bottom rounded", pre.corners)
	}
	if !pre.r.In(card.r) {
		t.Errorf("preview %v outside card %v", pre.r, card.r)
	}
	pk, fn := findFrag(t, d, "package"), findFrag(t, d, "func")
	if pk.size != 12 || fn.top <= pk.top {
		t.Errorf("preview text size %d, lines %d/%d", pk.size, pk.top, fn.top)
	}
	if fn.top-pk.top != 2*18 {
		t.Errorf("blank preview line not kept: %d", fn.top-pk.top)
	}
	findFrag(t, d, "lines")
	if hdr := findFrag(t, d, "main.go"); hdr.target != card.target || hdr.top >= pk.top {
		t.Errorf("header frag target %d top %d", hdr.target, hdr.top)
	}
}

func TestGenericFileCard(t *testing.T) {
	st := newTestStyle()
	p := st.Palette
	f := &rt.File{Name: "report.pdf", Type: "pdf", PrettyType: "PDF", Size: 2048}
	v := layoutAt(st, doc(fileBlock(f)), Options{}, 600)
	d := &v.d
	cards := boxesOf(d, bBorder)
	if len(cards) != 1 || cards[0].r != image.Rect(0, 0, 426, 62) {
		t.Fatalf("card %+v", cards)
	}
	var icon *box
	for i := range d.boxes {
		if d.boxes[i].kind == bFill && d.boxes[i].fill == p.FileIconPDF {
			icon = &d.boxes[i]
		}
	}
	if icon == nil || icon.r != image.Rect(12, 12, 48, 48) {
		t.Fatalf("pdf icon %+v", icon)
	}
	name := findFrag(t, d, "report.pdf")
	if name.target != cards[0].target || name.x != 60 {
		t.Errorf("name frag target %d x %.1f", name.target, name.x)
	}
	// FileSize: the pretty_type alone, no size (Slack drops "· 2 KB")
	var meta *frag
	for _, f := range textFrags(d) {
		if f.sh.str == "PDF" && f.top > name.top {
			meta = f
		}
	}
	if meta == nil || meta.col != p.Text2 {
		t.Errorf("meta line %+v, want PDF in Text2 under the name", meta)
	}
	for _, f := range textFrags(d) {
		if f.sh.str == "KB" || f.sh.str == "·" {
			t.Errorf("meta has %q; Slack shows only the type", f.sh.str)
		}
	}

	icons := func(f *rt.File) (color.NRGBA, []string) {
		v := layoutAt(st, doc(fileBlock(f)), Options{}, 600)
		for _, b := range v.d.boxes {
			if b.kind == bFill && b.r.Dx() == 36 {
				return b.fill, fragStrings(&v.d)
			}
		}
		t.Fatalf("no icon for %+v", f)
		return color.NRGBA{}, nil
	}
	if c, _ := icons(&rt.File{Name: "a.zip", Type: "zip"}); c != p.FileIconZip {
		t.Errorf("zip icon %v", c)
	}
	if c, strs := icons(&rt.File{Name: "notes"}); c != p.FileIconBg || strs[0] != "FILE" {
		t.Errorf("untyped icon %v labels %q", c, strs)
	}
	if _, strs := icons(&rt.File{Name: "data", Type: "parquet"}); strs[0] != "PARQ" {
		t.Errorf("long type label %q, want 4 chars", strs[0])
	}
	if _, strs := icons(&rt.File{}); !strings.Contains(strings.Join(strs, " "), "Untitled") {
		t.Errorf("nameless file labels %q", strs)
	}
}

func TestTombstone(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, doc(rt.Block{Kind: rt.Tombstone}), Options{}, 400)
	if got := strings.Join(fragStrings(&v.d), " "); got != "This message was deleted." {
		t.Errorf("tombstone text %q", got)
	}
	for _, f := range textFrags(&v.d) {
		if f.col != st.Palette.Text2 {
			t.Errorf("tombstone color %v", f.col)
		}
	}
	v = layoutAt(st, doc(rt.Block{Kind: rt.Tombstone, Spans: []rt.Span{txt("Removed by admin")}}), Options{}, 400)
	findFrag(t, &v.d, "admin")
}

func TestMutedAndCompact(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, doc(para(txt("joined the channel"))), Options{Muted: true}, 400)
	if f := findFrag(t, &v.d, "joined"); f.col != st.Palette.Text3 {
		t.Errorf("muted color %v", f.col)
	}
	v = layoutAt(st, doc(para(txt("short preview"))), Options{Compact: true}, 400)
	if f := findFrag(t, &v.d, "short"); f.size != 13 || f.bot-f.top != 20 {
		t.Errorf("compact size %d line %d", f.size, f.bot-f.top)
	}
}

// ---------- edited label ----------

func TestEditedLabel(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, doc(para(txt("hello"))), Options{Edited: true}, 400)
	hello, ed := findFrag(t, &v.d, "hello"), findFrag(t, &v.d, "(edited)")
	if ed.line != hello.line || ed.x <= hello.x+hello.w {
		t.Errorf("(edited) not after the text on the same line")
	}
	if ed.col != st.Palette.Edited || ed.size != 13 {
		t.Errorf("(edited) color %v size %d", ed.col, ed.size)
	}
	if ed.p0 != ed.p1 {
		t.Errorf("(edited) occupies plain text %d..%d", ed.p0, ed.p1)
	}
	if string(v.d.plain) != "hello" {
		t.Errorf("plain %q, want \"hello\"", v.d.plain)
	}

	// Files after the text do not steal the label.
	v = layoutAt(st, doc(para(txt("hello")), fileBlock(&rt.File{Name: "a.bin"})), Options{Edited: true}, 400)
	if findFrag(t, &v.d, "(edited)").line != findFrag(t, &v.d, "hello").line {
		t.Errorf("(edited) not on the text line when a file follows")
	}

	// Nothing textual: a line of its own below.
	v = layoutAt(st, doc(rt.Block{Kind: rt.Divider}), Options{Edited: true}, 400)
	if f := findFrag(t, &v.d, "(edited)"); f.top < 17 || f.x != 0 {
		t.Errorf("standalone (edited) at x %.1f top %d", f.x, f.top)
	}
	if len(v.d.plain) != 0 {
		t.Errorf("plain %q", v.d.plain)
	}

	// Without the option there is no label.
	v = layoutAt(st, doc(para(txt("hello"))), Options{}, 400)
	if hasFrag(&v.d, "(edited)") {
		t.Errorf("(edited) without Options.Edited")
	}
}

func TestLastTextual(t *testing.T) {
	b := func(k rt.BlockKind) rt.Block { return rt.Block{Kind: k} }
	cases := []struct {
		bs   []rt.Block
		want int
	}{
		{nil, -1},
		{[]rt.Block{b(rt.Paragraph)}, 0},
		{[]rt.Block{b(rt.Paragraph), b(rt.FileBlock)}, 0},
		{[]rt.Block{b(rt.Paragraph), b(rt.AttachmentBlock), b(rt.FileBlock)}, 0},
		{[]rt.Block{b(rt.Paragraph), b(rt.Divider)}, 0}, // label goes after the last text
		{[]rt.Block{b(rt.Quote), b(rt.List)}, 1},
		{[]rt.Block{b(rt.Section)}, 0},
		{[]rt.Block{b(rt.Section), b(rt.FileBlock)}, 0},
		{[]rt.Block{b(rt.Preformatted)}, 0},
	}
	for i, c := range cases {
		if got := lastTextual(c.bs); got != c.want {
			t.Errorf("case %d: lastTextual = %d, want %d", i, got, c.want)
		}
	}
}

// ---------- plain text and selection ----------

func TestPlainTextSelection(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, doc(para(txt("hello world")), para(txt("second line"))), Options{}, 400)
	if string(v.d.plain) != "hello world\nsecond line" {
		t.Fatalf("plain %q", v.d.plain)
	}
	if v.HasSelection() || v.SelectedText() != "" {
		t.Errorf("fresh view has a selection")
	}
	v.selA, v.selB = 6, 11
	if v.SelectedText() != "world" || !v.HasSelection() {
		t.Errorf("selected %q", v.SelectedText())
	}
	v.selA, v.selB = 11, 6
	if v.SelectedText() != "world" {
		t.Errorf("reversed selection %q", v.SelectedText())
	}
	v.selA, v.selB = 6, 18
	if v.SelectedText() != "world\nsecond" {
		t.Errorf("multi-block selection %q", v.SelectedText())
	}
	v.ClearSelection()
	if v.HasSelection() {
		t.Errorf("ClearSelection kept a selection")
	}

	hello, world, second := findFrag(t, &v.d, "hello"), findFrag(t, &v.d, "world"), findFrag(t, &v.d, "second")
	mid := float32(world.top+world.bot) / 2
	if off := v.offsetAt(f32.Pt(0, mid)); off != 0 {
		t.Errorf("offsetAt(line start) = %d", off)
	}
	if off := v.offsetAt(f32.Pt(hello.x+hello.w+1, mid)); off != 5 && off != 6 {
		t.Errorf("offsetAt(after hello) = %d, want 5 or 6", off)
	}
	if off := v.offsetAt(f32.Pt(world.x+world.sh.caretX(2)+0.1, mid)); off != 8 {
		t.Errorf("offsetAt(inside world) = %d, want 8", off)
	}
	if off := v.offsetAt(f32.Pt(390, mid)); off != 11 {
		t.Errorf("offsetAt(right of line 0) = %d, want 11", off)
	}
	if off := v.offsetAt(f32.Pt(0, float32(second.top+second.bot)/2)); off != 12 {
		t.Errorf("offsetAt(line 1 start) = %d, want 12", off)
	}
	if off := v.offsetAt(f32.Pt(390, 1000)); int(off) != len(v.d.plain) {
		t.Errorf("offsetAt(below everything) = %d, want %d", off, len(v.d.plain))
	}
	if off := v.offsetAt(f32.Pt(50, -100)); off != 0 {
		t.Errorf("offsetAt(above everything) = %d, want 0", off)
	}

	v.selectWord(8)
	if v.SelectedText() != "world" {
		t.Errorf("selectWord(8) = %q", v.SelectedText())
	}
	v.selectWord(0)
	if v.SelectedText() != "hello" {
		t.Errorf("selectWord(0) = %q", v.SelectedText())
	}
	v.selectWord(int32(len(v.d.plain)))
	if v.SelectedText() != "line" {
		t.Errorf("selectWord(end) = %q", v.SelectedText())
	}
	v.selectLine(14)
	if v.SelectedText() != "second line" {
		t.Errorf("selectLine(14) = %q", v.SelectedText())
	}
	v.selectLine(3)
	if v.SelectedText() != "hello world" {
		t.Errorf("selectLine(3) = %q", v.SelectedText())
	}

	// A relayout keeps an in-range selection and drops one past the end.
	v.selA, v.selB = 0, 5
	v.relayout(st, 300, 1, 1)
	if v.SelectedText() != "hello" {
		t.Errorf("selection lost on relayout: %q", v.SelectedText())
	}
	v.selA, v.selB = 0, 500
	v.relayout(st, 300, 1, 1)
	if v.HasSelection() {
		t.Errorf("out-of-range selection survived relayout")
	}
}

func TestSelectWordUnicode(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, doc(para(txt("naïve café, don't"))), Options{}, 400)
	v.selectWord(2)
	if v.SelectedText() != "naïve" {
		t.Errorf("selectWord in naïve = %q", v.SelectedText())
	}
	v.selectWord(int32(strings.Index(string(v.d.plain), "don")))
	if v.SelectedText() != "don't" {
		t.Errorf("selectWord in don't = %q", v.SelectedText())
	}
}

// ---------- shaping helpers ----------

func TestShapedCaretAndRuneAt(t *testing.T) {
	st := newTestStyle()
	k := faceKey{f: font.Font{Typeface: st.Face}, size: fixedPx(15)}
	w := st.word(k, "hello")
	if st.word(k, "hello") != w {
		t.Errorf("word cache returned a different *shaped")
	}
	if w.runes != 5 || w.adv <= 0 {
		t.Fatalf("shaped %+v", w)
	}
	prev := float32(-1)
	for r := 0; r <= 5; r++ {
		x := w.caretX(r)
		if x <= prev {
			t.Errorf("caretX(%d) = %.2f not increasing", r, x)
		}
		prev = x
	}
	if w.caretX(0) != 0 || w.caretX(5) != w.adv || w.caretX(99) != w.adv {
		t.Errorf("caretX bounds: %.2f %.2f %.2f adv %.2f", w.caretX(0), w.caretX(5), w.caretX(99), w.adv)
	}
	if w.runeAt(-3) != 0 || w.runeAt(w.adv+10) != 5 {
		t.Errorf("runeAt bounds")
	}
	if got := w.runeAt(w.caretX(2) + 0.1); got != 2 {
		t.Errorf("runeAt(caretX(2)+) = %d", got)
	}

	long := st.word(k, "abcdefghijklmnop")
	head, tail, nb := st.split(k, long, long.adv/2)
	if tail == nil || head.str+tail.str != long.str || nb != len(head.str) {
		t.Fatalf("split %q|%v nb=%d", head.str, tail, nb)
	}
	if head.adv > long.adv/2+0.5 {
		t.Errorf("head %.1f wider than %.1f", head.adv, long.adv/2)
	}
	// Too narrow for anything: still emits one cluster.
	head, tail, _ = st.split(k, long, 0.5)
	if head.runes != 1 || tail == nil || tail.runes != long.runes-1 {
		t.Errorf("split at 0.5px: head %q", head.str)
	}
	// Wide enough: nothing to split.
	head, tail, _ = st.split(k, long, long.adv+10)
	if head != long || tail != nil {
		t.Errorf("split of a fitting word")
	}

	gen := st.gen
	st.Invalidate()
	if st.gen == gen || len(st.words) != 0 || len(st.space) != 0 {
		t.Errorf("Invalidate did not reset caches")
	}
	gen = st.gen
	st.SetPalette(DarkPalette())
	if st.gen == gen || st.Palette.Text != DarkPalette().Text {
		t.Errorf("SetPalette did not bump gen / set colors")
	}
}

func TestMaxLinesEllipsis(t *testing.T) {
	st := newTestStyle()
	const src = "one two three four five six seven eight nine ten eleven twelve"
	const w = 100

	l := &lay{st: st, d: &display{}, scale: 1, spx: 1}
	fs := l.bodyStyle()
	fs.maxLines, fs.ellipsis = 1, true
	h, _ := l.plainFlow(src, 0, 0, w, &fs, -1)
	if h != 22 || len(l.d.lines) != 1 {
		t.Fatalf("height %d lines %d, want one 22px line", h, len(l.d.lines))
	}
	last := l.d.frags[len(l.d.frags)-1]
	if last.sh == nil || last.sh.str != "…" {
		t.Fatalf("last frag %+v, want the ellipsis", last)
	}
	if last.x+last.w > w+0.5 {
		t.Errorf("ellipsis ends at %.1f > %d", last.x+last.w, w)
	}
	if l.d.frags[0].sh.str != "one" {
		t.Errorf("first frag %q", l.d.frags[0].sh.str)
	}

	l = &lay{st: st, d: &display{}, scale: 1, spx: 1}
	fs = l.bodyStyle()
	fs.maxLines = 2
	h, _ = l.plainFlow(src, 0, 0, w, &fs, -1)
	if h != 44 || len(l.d.lines) != 2 {
		t.Errorf("maxLines 2: height %d lines %d", h, len(l.d.lines))
	}
	for _, f := range l.d.frags {
		if f.sh != nil && f.sh.str == "…" {
			t.Errorf("ellipsis without fs.ellipsis")
		}
	}

	// Text that fits is not truncated.
	l = &lay{st: st, d: &display{}, scale: 1, spx: 1}
	fs = l.bodyStyle()
	fs.maxLines, fs.ellipsis = 1, true
	l.plainFlow("one", 0, 0, w, &fs, -1)
	if len(l.d.frags) != 1 || l.d.frags[0].sh.str != "one" {
		t.Errorf("short text truncated: %d frags", len(l.d.frags))
	}

	// A forced split of a single long word also respects maxLines.
	l = &lay{st: st, d: &display{}, scale: 1, spx: 1}
	fs = l.bodyStyle()
	fs.maxLines, fs.ellipsis = 2, true
	h, _ = l.plainFlow(strings.Repeat("w", 80), 0, 0, w, &fs, -1)
	if h != 44 || len(l.d.lines) != 2 {
		t.Errorf("long word maxLines 2: height %d lines %d", h, len(l.d.lines))
	}
}

// ---------- formatting ----------

func TestSizeText(t *testing.T) {
	cases := map[int64]string{
		0:                      "",
		-5:                     "",
		1:                      "1 B",
		1023:                   "1023 B",
		1024:                   "1 KB",
		2048:                   "2 KB",
		1024 * 1024:            "1.0 MB",
		5 * 1024 * 1024 / 2:    "2.5 MB",
		1024 * 1024 * 1024:     "1.0 GB",
		3 * 1024 * 1024 * 1024: "3.0 GB",
	}
	for n, want := range cases {
		if got := sizeText(n); got != want {
			t.Errorf("sizeText(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestDurationText(t *testing.T) {
	cases := map[int]string{
		0:       "0:00",
		999:     "0:00",
		5000:    "0:05",
		83000:   "1:23",
		600000:  "10:00",
		3599000: "59:59",
		3600000: "1:00:00",
		3723000: "1:02:03",
	}
	for ms, want := range cases {
		if got := durationText(ms); got != want {
			t.Errorf("durationText(%d) = %q, want %q", ms, got, want)
		}
	}
}

func TestOrdinal(t *testing.T) {
	cases := map[int]string{1: "st", 2: "nd", 3: "rd", 4: "th", 10: "th", 11: "th", 12: "th", 13: "th", 21: "st", 22: "nd", 23: "rd", 24: "th", 30: "th", 31: "st"}
	for d, want := range cases {
		if got := ordinal(d); got != want {
			t.Errorf("ordinal(%d) = %q, want %q", d, got, want)
		}
	}
}

func TestSlackDate(t *testing.T) {
	if slackDate(0) != "" || slackDate(-1) != "" {
		t.Errorf("non-positive ts should format empty")
	}
	old := time.Date(2020, time.March, 1, 12, 0, 0, 0, time.Local)
	if got := slackDate(old.Unix()); got != "Mar 1st, 2020" {
		t.Errorf("slackDate(2020-03-01) = %q", got)
	}
	old = time.Date(2019, time.December, 22, 12, 0, 0, 0, time.Local)
	if got := slackDate(old.Unix()); got != "Dec 22nd, 2019" {
		t.Errorf("slackDate(2019-12-22) = %q", got)
	}
	now := time.Now()
	thisYear := time.Date(now.Year(), time.January, 13, 12, 0, 0, 0, time.Local)
	if got := slackDate(thisYear.Unix()); got != "Jan 13th" {
		t.Errorf("slackDate(this year) = %q, want no year", got)
	}
}

// ---------- View.Layout: caching, images, painting ----------

func TestRelayoutCache(t *testing.T) {
	st := newTestStyle()
	v := New(doc(para(txt("hello world"))), Options{})
	ops := new(op.Ops)
	dims := v.Layout(testGtx(ops, 400), st, nil)
	if v.dirty || v.laidW != 400 || !v.recorded {
		t.Fatalf("after Layout: dirty %v laidW %d recorded %v", v.dirty, v.laidW, v.recorded)
	}
	if dims.Size.X != 400 || dims.Size.Y != 22 {
		t.Errorf("dims %v, want 400x22 (min width from constraints)", dims.Size)
	}

	// Same width, scale and generation: the cached display is reused.
	const sentinel = 12345
	v.d.size.Y = sentinel
	ops.Reset()
	if dims = v.Layout(testGtx(ops, 400), st, nil); dims.Size.Y != sentinel {
		t.Errorf("relayout at an unchanged width")
	}

	// Width change relayouts.
	ops.Reset()
	if dims = v.Layout(testGtx(ops, 300), st, nil); dims.Size.Y == sentinel || v.laidW != 300 {
		t.Errorf("no relayout after width change (laidW %d)", v.laidW)
	}

	// Scale change relayouts.
	v.d.size.Y = sentinel
	ops.Reset()
	gtx := testGtx(ops, 300)
	gtx.Metric = unit.Metric{PxPerDp: 2, PxPerSp: 2}
	if dims = v.Layout(gtx, st, nil); dims.Size.Y != 44 || v.laidPx != 2 {
		t.Errorf("scale 2: height %d laidPx %.1f, want 44", dims.Size.Y, v.laidPx)
	}

	// Palette (generation) change relayouts.
	ops.Reset()
	v.Layout(testGtx(ops, 300), st, nil)
	v.d.size.Y = sentinel
	st.SetPalette(DarkPalette())
	ops.Reset()
	if dims = v.Layout(testGtx(ops, 300), st, nil); dims.Size.Y == sentinel || v.laidGen != st.gen {
		t.Errorf("no relayout after SetPalette")
	}

	// Equal options are a no-op; different options relayout.
	v.d.size.Y = sentinel
	v.SetOptions(Options{})
	if v.dirty {
		t.Errorf("SetOptions with equal options marked dirty")
	}
	v.SetOptions(Options{Edited: true})
	if !v.dirty {
		t.Errorf("SetOptions with new options not dirty")
	}
	ops.Reset()
	if dims = v.Layout(testGtx(ops, 300), st, nil); dims.Size.Y == sentinel || !hasFrag(&v.d, "(edited)") {
		t.Errorf("options change not laid out")
	}

	// MaxWidth caps the layout width.
	v.SetOptions(Options{MaxWidth: 200})
	ops.Reset()
	v.Layout(testGtx(ops, 400), st, nil)
	if v.laidW != 200 {
		t.Errorf("MaxWidth: laidW %d, want 200", v.laidW)
	}

	// Set replaces the doc and resets interaction state.
	v.selA, v.selB, v.hover = 1, 3, 0
	v.Set(doc(para(txt("other"))), Options{})
	if !v.dirty || v.HasSelection() || v.hover != -1 {
		t.Errorf("Set did not reset state")
	}
	ops.Reset()
	v.Layout(testGtx(ops, 400), st, nil)
	if string(v.d.plain) != "other" || v.Doc().Blocks[0].Spans[0].Text != "other" {
		t.Errorf("Set: plain %q", v.d.plain)
	}
}

func TestPendingImages(t *testing.T) {
	st := newTestStyle()
	imgs := newFakeImages()
	st.Images = imgs
	const emojiURL, thumbURL = "https://emoji.test/wave.png", "https://files.test/photo.png"
	d := doc(
		para(txt("hi "), rt.Span{Kind: rt.Emoji, ID: "wave", URL: emojiURL}),
		fileBlock(&rt.File{Name: "photo.png", IsImage: true, Thumb: &rt.Image{URL: thumbURL, W: 100, H: 80}}),
	)
	v := New(d, Options{})
	ops := new(op.Ops)
	v.Layout(testGtx(ops, 400), st, nil)
	if len(v.pending) != 2 {
		t.Fatalf("pending %v, want the thumbnail and the emoji", v.pending)
	}
	var nImg, nFrag int
	for _, p := range v.pending {
		if p >= 0 {
			nImg++
		} else {
			nFrag++
			if v.d.frags[^p].kind != fImage {
				t.Errorf("pending frag %d is not an image", ^p)
			}
		}
	}
	if nImg != 1 || nFrag != 1 {
		t.Errorf("pending imgs %d frags %d", nImg, nFrag)
	}
	if imgs.px[emojiURL] != 22 || imgs.px[thumbURL] != 100 {
		t.Errorf("requested sizes %v, want emoji 22, thumb 100", imgs.px)
	}

	// Still not decoded: stays pending, keeps the recording.
	ops.Reset()
	before := imgs.gets
	v.Layout(testGtx(ops, 400), st, nil)
	if len(v.pending) != 2 || !v.recorded || imgs.gets == before {
		t.Errorf("not-ready frame: pending %d recorded %v gets %d->%d", len(v.pending), v.recorded, before, imgs.gets)
	}

	// Decoded: the next frame re-records and nothing is pending.
	imgs.ready = true
	if !v.anyReady(st) {
		t.Fatalf("anyReady false with a ready cache")
	}
	ops.Reset()
	v.Layout(testGtx(ops, 400), st, nil)
	if len(v.pending) != 0 || !v.recorded {
		t.Errorf("after decode: pending %v", v.pending)
	}

	// A nil image source never reports ready.
	st.Images = nil
	v.pending = append(v.pending[:0], 0)
	if v.anyReady(st) {
		t.Errorf("anyReady with nil Images")
	}
}

// kitchenSink has one of every block kind (all synthetic).
func kitchenSink() rt.Doc {
	img := func(n string, w, h int) *rt.Image { return &rt.Image{URL: "https://img.test/" + n, W: w, H: h} }
	return doc(
		para(txt("Hello "), rt.Span{Kind: rt.User, Text: "@alice", ID: "U1"}, txt(" see "),
			rt.Span{Kind: rt.Link, Text: "the link", URL: "https://example.test"}, txt(" and "),
			rt.Span{Kind: rt.Text, Text: "code", Style: rt.Code}, txt(" "),
			rt.Span{Kind: rt.Text, Text: "bold", Style: rt.Bold}, txt(" "),
			rt.Span{Kind: rt.Text, Text: "struck", Style: rt.Strike | rt.Underline}, txt(" "),
			rt.Span{Kind: rt.Text, Text: "marked", Style: rt.Highlight}, txt(" "),
			rt.Span{Kind: rt.Emoji, ID: "tada", URL: "https://emoji.test/tada.png"},
			rt.Span{Kind: rt.ColorSwatch, Text: "#1D9BD1"},
			rt.Span{Kind: rt.Broadcast, Text: "@here", ID: "here"}),
		rt.Block{Kind: rt.Preformatted, Spans: []rt.Span{txt("func main() {"), brk(), txt("}")}},
		rt.Block{Kind: rt.Quote, Spans: []rt.Span{txt("a quote")}},
		rt.Block{Kind: rt.List, Items: [][]rt.Span{{txt("one")}, {txt("two")}}, Border: true},
		rt.Block{Kind: rt.List, Ordered: true, Indent: 1, Items: [][]rt.Span{{txt("x")}}},
		rt.Block{Kind: rt.Header, Spans: []rt.Span{txt("Header")}},
		rt.Block{Kind: rt.Divider},
		rt.Block{Kind: rt.Context, Image: img("ctx", 0, 0), Spans: []rt.Span{txt("context")}},
		rt.Block{Kind: rt.Section, Spans: []rt.Span{txt("section")}, Fields: [][]rt.Span{{txt("a")}, {txt("b")}}, Buttons: []rt.Button{{Text: "Go"}}},
		rt.Block{Kind: rt.ImageBlock, Image: &rt.Image{URL: "https://img.test/big", W: 600, H: 300, Title: "A picture"}},
		rt.Block{Kind: rt.Actions, Buttons: []rt.Button{{Text: "Yes", Style: "primary"}, {Text: "No", Style: "danger"}, {Text: "Maybe"}}},
		rt.Block{Kind: rt.AttachmentBlock, Att: &rt.Attachment{Color: "good", Pretext: []rt.Span{txt("pre")}, AuthorName: "Bot", AuthorIcon: "https://img.test/a",
			Title: []rt.Span{txt("Title")}, TitleLink: "https://example.test/t", Text: []rt.Block{para(txt("body"))},
			Footer: []rt.Span{txt("footer")}, FooterIcon: "https://img.test/f", TS: 1600000000, Thumb: img("thumb", 50, 50)},
			Fields: [][]rt.Span{{txt("k")}, {txt("v")}}, FieldShort: []bool{true, true}},
		rt.Block{Kind: rt.AttachmentBlock, Att: &rt.Attachment{IsUnfurl: true, Title: []rt.Span{txt("Unfurl")}, TitleLink: "https://example.test/u",
			Text: []rt.Block{para(txt("desc"))}, ServiceName: "Svc", ServiceIcon: "https://img.test/s", Image: img("u", 800, 400)}},
		rt.Block{Kind: rt.AttachmentBlock, Att: &rt.Attachment{IsShare: true, AuthorName: "Bob", Text: []rt.Block{para(txt("shared"))}, ShareChannel: "general"}},
		fileBlock(&rt.File{Name: "a.png", IsImage: true, Thumb: img("a", 100, 100)}),
		fileBlock(&rt.File{Name: "b.png", IsImage: true, Thumb: img("b", 100, 100)}),
		fileBlock(&rt.File{Name: "v.mp4", IsVideo: true, DurationMS: 4000000}),
		fileBlock(&rt.File{Name: "s.mp3", IsAudio: true}),
		fileBlock(&rt.File{Name: "x.txt", IsSnippet: true, Preview: strings.Repeat("line\n", 20)}),
		fileBlock(&rt.File{Name: "doc.pdf", Type: "pdf", Size: 123456}),
		rt.Block{Kind: rt.Tombstone},
	)
}

func TestKitchenSinkLayoutAndPaint(t *testing.T) {
	st := newTestStyle()
	imgs := newFakeImages()
	imgs.ready = true
	st.Images = imgs
	for _, w := range []int{1000, 320, 90, 1} {
		v := New(kitchenSink(), Options{Edited: true, Dim: true})
		ops := new(op.Ops)
		dims := v.Layout(testGtx(ops, w), st, nil)
		if dims.Size.Y <= 0 {
			t.Fatalf("width %d: empty layout", w)
		}
		if v.d.size.X > w {
			t.Errorf("width %d: size.X %d", w, v.d.size.X)
		}
		for i, ln := range v.d.lines {
			if ln.bot < ln.top || ln.f1 < ln.f0 || ln.f1 > len(v.d.frags) {
				t.Errorf("width %d: bad line %d %+v", w, i, ln)
			}
		}
		for i, f := range v.d.frags {
			if f.target >= int16(len(v.d.targets)) {
				t.Errorf("width %d: frag %d target %d out of range", w, i, f.target)
			}
			if f.p1 > int32(len(v.d.plain)) {
				t.Errorf("width %d: frag %d plain range %d..%d past %d", w, i, f.p0, f.p1, len(v.d.plain))
			}
		}
		for i, h := range v.d.hits {
			if h.t < 0 || int(h.t) >= len(v.d.targets) {
				t.Errorf("width %d: hit %d target %d", w, i, h.t)
			}
		}
		// Paint with a selection and every hover target.
		v.selA, v.selB = 0, int32(len(v.d.plain))
		for tg := range v.d.targets {
			v.hover = int16(tg)
			ops.Reset()
			v.Layout(testGtx(ops, w), st, nil)
		}
		if v.SelectedText() != string(v.d.plain) {
			t.Errorf("width %d: select-all text mismatch", w)
		}
	}
}

func TestColorSwatch(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, doc(para(txt("color "), rt.Span{Kind: rt.ColorSwatch, Text: "#1D9BD1"})), Options{}, 400)
	var sw *box
	for i := range v.d.boxes {
		if v.d.boxes[i].fill == rgb(0x1d, 0x9b, 0xd1) {
			sw = &v.d.boxes[i]
		}
	}
	if sw == nil {
		t.Fatalf("no swatch box: %+v", v.d.boxes)
	}
	if sw.r.Dx() != 12 || sw.r.Dy() != 12 {
		t.Errorf("swatch %v, want 12x12", sw.r)
	}
	if f := findFrag(t, &v.d, "#1D9BD1"); f.x < float32(sw.r.Max.X) {
		t.Errorf("hex text at %.1f overlaps swatch %v", f.x, sw.r)
	}
}

// ---------- input: clicks, selection gestures, shortcuts ----------

type recHandler struct {
	urls     []string
	users    []string
	anchors  []image.Point
	channels []string
	groups   []string
	images   []rt.Image
	files    []*rt.File
	buttons  []rt.Button
}

func (h *recHandler) OpenURL(u string) { h.urls = append(h.urls, u) }
func (h *recHandler) OpenUser(id string, a image.Point) {
	h.users = append(h.users, id)
	h.anchors = append(h.anchors, a)
}
func (h *recHandler) OpenChannel(id string) { h.channels = append(h.channels, id) }
func (h *recHandler) OpenUsergroup(id string, a image.Point) {
	h.groups = append(h.groups, id)
	h.anchors = append(h.anchors, a)
}
func (h *recHandler) OpenImage(img rt.Image) { h.images = append(h.images, img) }
func (h *recHandler) OpenFile(f *rt.File)    { h.files = append(h.files, f) }
func (h *recHandler) Button(b rt.Button)     { h.buttons = append(h.buttons, b) }
func (h *recHandler) total() int {
	return len(h.urls) + len(h.users) + len(h.channels) + len(h.groups) + len(h.images) + len(h.files) + len(h.buttons)
}

// harness drives a View through a real input.Router.
type harness struct {
	r   input.Router
	ops op.Ops
	st  *Style
	v   *View
	h   Handler
	w   int
}

func newHarness(d rt.Doc, h Handler) *harness {
	hs := &harness{st: newTestStyle(), v: New(d, Options{}), h: h, w: 400}
	hs.frame()
	return hs
}

func (hs *harness) frame() {
	hs.ops.Reset()
	gtx := layout.Context{
		Ops:         &hs.ops,
		Source:      hs.r.Source(),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(hs.w, 1000)),
	}
	hs.v.Layout(gtx, hs.st, hs.h)
	hs.r.Frame(&hs.ops)
}

// send queues events and runs a few frames so deferred events (after
// focus commands) are delivered too.
func (hs *harness) send(evs ...event.Event) {
	hs.r.Queue(evs...)
	for i := 0; i < 4; i++ {
		hs.frame()
	}
}

func press(p f32.Point, at time.Duration) pointer.Event {
	return pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p, Time: at}
}

func release(p f32.Point, at time.Duration) pointer.Event {
	return pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: at}
}

func TestClickLink(t *testing.T) {
	const url = "https://example.test/click"
	h := &recHandler{}
	hs := newHarness(doc(para(txt("go to "), rt.Span{Kind: rt.Link, Text: "here", URL: url})), h)
	p := center(findFrag(t, &hs.v.d, "here"))
	hs.send(press(p, 0), release(p, 20*time.Millisecond))
	if len(h.urls) != 1 || h.urls[0] != url {
		t.Fatalf("OpenURL calls %q, want [%q]", h.urls, url)
	}
	if h.total() != 1 {
		t.Errorf("extra handler calls: %+v", h)
	}
	if hs.v.HasSelection() {
		t.Errorf("a click left a selection")
	}

	// Clicking plain text opens nothing.
	q := center(findFrag(t, &hs.v.d, "go"))
	hs.send(press(q, time.Second), release(q, time.Second+20*time.Millisecond))
	if h.total() != 1 {
		t.Errorf("click on plain text called the handler: %+v", h)
	}
}

func TestClickMentionAnchor(t *testing.T) {
	h := &recHandler{}
	hs := newHarness(doc(para(txt("hey "), rt.Span{Kind: rt.User, Text: "@bob", ID: "U2"})), h)
	hs.v.Origin = image.Pt(100, 200)
	p := center(findFrag(t, &hs.v.d, "@bob"))
	hs.send(press(p, 0), release(p, 20*time.Millisecond))
	if len(h.users) != 1 || h.users[0] != "U2" {
		t.Fatalf("OpenUser calls %q", h.users)
	}
	if want := image.Pt(int(p.X), int(p.Y)).Add(image.Pt(100, 200)); h.anchors[0] != want {
		t.Errorf("anchor %v, want %v (position + Origin)", h.anchors[0], want)
	}
}

func TestClickButtonAndCard(t *testing.T) {
	h := &recHandler{}
	d := doc(
		rt.Block{Kind: rt.Actions, Buttons: []rt.Button{{Text: "Approve", ActionID: "approve", Value: "42"}}},
		rt.Block{Kind: rt.AttachmentBlock, Att: &rt.Attachment{IsUnfurl: true, Title: []rt.Span{txt("Card")}, TitleLink: "https://example.test/card",
			Text: []rt.Block{para(txt("synthetic"))}}},
	)
	hs := newHarness(d, h)
	bs := buttonBoxes(&hs.v.d)
	if len(bs) != 1 {
		t.Fatalf("%d buttons", len(bs))
	}
	p := rectCenter(bs[0].r)
	hs.send(press(p, 0), release(p, 20*time.Millisecond))
	if len(h.buttons) != 1 || h.buttons[0].ActionID != "approve" || h.buttons[0].Value != "42" {
		t.Fatalf("Button calls %+v", h.buttons)
	}

	var card image.Rectangle
	for _, b := range hs.v.d.boxes {
		if b.kind == bBorder && b.radius == 12 {
			card = b.r
		}
	}
	if card.Empty() {
		t.Fatalf("no card")
	}
	// Bottom-right corner of the card, away from the text.
	q := f32.Pt(float32(card.Max.X-4), float32(card.Max.Y-3))
	hs.send(press(q, time.Second), release(q, time.Second+20*time.Millisecond))
	if len(h.urls) != 1 || h.urls[0] != "https://example.test/card" {
		t.Errorf("card click: OpenURL %q", h.urls)
	}
}

func TestDoubleClickSelectsWordAndCopy(t *testing.T) {
	hs := newHarness(doc(para(txt("hello brave world"))), &recHandler{})
	p := center(findFrag(t, &hs.v.d, "brave"))
	hs.send(
		press(p, 0), release(p, 20*time.Millisecond),
		press(p, 100*time.Millisecond), release(p, 120*time.Millisecond),
	)
	if got := hs.v.SelectedText(); got != "brave" {
		t.Fatalf("double-click selected %q, want \"brave\"", got)
	}
	hs.send(key.Event{Name: "C", Modifiers: key.ModShortcut, State: key.Press})
	if _, data, ok := hs.r.WriteClipboard(); !ok || string(data) != "brave" {
		t.Errorf("clipboard %q ok=%v, want \"brave\"", data, ok)
	}
	hs.send(key.Event{Name: "A", Modifiers: key.ModShortcut, State: key.Press})
	if got := hs.v.SelectedText(); got != "hello brave world" {
		t.Errorf("select all = %q", got)
	}
}

func TestDragSelects(t *testing.T) {
	hs := newHarness(doc(para(txt("hello brave world"))), &recHandler{})
	f := findFrag(t, &hs.v.d, "hello")
	y := float32(f.top+f.bot) / 2
	start, end := f32.Pt(f.x+0.1, y), f32.Pt(390, y)
	hs.send(
		press(start, 0),
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(60, y), Time: 10 * time.Millisecond},
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: end, Time: 20 * time.Millisecond},
		release(end, 30*time.Millisecond),
	)
	if got := hs.v.SelectedText(); got != "hello brave world" {
		t.Errorf("drag selected %q", got)
	}
}

// ---------- known engine bugs (skipped until fixed; see report) ----------

func TestBugOffsetAtZeroWidthPlainFrag(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, doc(para(txt("hello"))), Options{Edited: true}, 400)
	f := findFrag(t, &v.d, "(edited)")
	off := v.offsetAt(center(f))
	if int(off) > len(v.d.plain) {
		t.Fatalf("offsetAt on (edited) = %d > plain len %d", off, len(v.d.plain))
	}
	v.selectWord(off)
	v.selectLine(off)
}

func TestBugHeaderDropsEdited(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, doc(rt.Block{Kind: rt.Header, Spans: []rt.Span{txt("Title")}}), Options{Edited: true}, 400)
	if !hasFrag(&v.d, "(edited)") {
		t.Fatalf("(edited) missing after a Header")
	}
}

func TestBugNoSelectTextInPlain(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, doc(rt.Block{Kind: rt.Actions, Buttons: []rt.Button{{Text: "Approve"}}}), Options{}, 400)
	if strings.Contains(string(v.d.plain), "Approve") {
		t.Fatalf("button label in plain text: %q", v.d.plain)
	}
}

func TestBugEllipsisDropsLongWord(t *testing.T) {
	st := newTestStyle()
	l := &lay{st: st, d: &display{}, scale: 1, spx: 1}
	fs := l.bodyStyle()
	fs.maxLines, fs.ellipsis = 1, true
	l.plainFlow(strings.Repeat("w", 80), 0, 0, 100, &fs, -1)
	if len(l.d.frags) < 2 || l.d.frags[0].sh.str == "…" {
		t.Fatalf("frags %q, want a prefix of the word before the ellipsis", fragStrings(l.d))
	}
	if ell, head := l.d.frags[len(l.d.frags)-1], l.d.frags[0]; ell.y != head.y {
		t.Errorf("ellipsis baseline %d != text baseline %d", ell.y, head.y)
	}
}

func TestUnicodeEmojiAsImages(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, doc(rt.Block{Kind: rt.Actions, Buttons: []rt.Button{{Text: "Launch 🚀"}}}), Options{}, 400)
	var img *frag
	for i := range v.d.frags {
		if v.d.frags[i].kind == fImage {
			img = &v.d.frags[i]
		}
	}
	if img == nil || !strings.Contains(img.url, "1f680") || img.size != 16 {
		t.Fatalf("button emoji frag %+v", img)
	}
	v = layoutAt(st, doc(para(txt("ok 👍🏽 done"))), Options{}, 400)
	n := 0
	for i := range v.d.frags {
		if v.d.frags[i].kind == fImage {
			n++
			if !strings.Contains(v.d.frags[i].url, "1f44d") {
				t.Errorf("thumbs up url %q", v.d.frags[i].url)
			}
		}
	}
	if n != 1 || string(v.d.plain) != "ok 👍🏽 done" {
		t.Errorf("%d emoji frags, plain %q", n, v.d.plain)
	}
	if segs := splitEmoji("plain text"); segs != nil {
		t.Errorf("no-emoji text split: %+v", segs)
	}
}

func TestTableLayout(t *testing.T) {
	st := newTestStyle()
	b := rt.Block{Kind: rt.Table, ColAlign: []string{"", "right"}, Rows: [][][]rt.Span{
		{{txt("Name")}, {txt("Score")}},
		{{txt("ann")}, {txt("42")}},
	}}
	v := layoutAt(st, doc(b), Options{}, 600)
	var card *box
	for i := range v.d.boxes {
		if v.d.boxes[i].kind == bBorder {
			card = &v.d.boxes[i]
		}
	}
	if card == nil || card.r.Dy() <= 0 || card.r.Dx() > 600 {
		t.Fatalf("table frame %+v", card)
	}
	name, score, n42 := findFrag(t, &v.d, "Name"), findFrag(t, &v.d, "Score"), findFrag(t, &v.d, "42")
	if name.top >= findFrag(t, &v.d, "ann").top {
		t.Errorf("rows not stacked")
	}
	// right-aligned column: "42" ends where "Score" ends
	if d := (n42.x + n42.w) - (score.x + score.w); d > 1 || d < -1 {
		t.Errorf("right alignment off by %.1f", d)
	}
	if !strings.Contains(string(v.d.plain), "Name\tScore") {
		t.Errorf("plain %q", v.d.plain)
	}
}

// TestFirstBaseline: Layout reports the first line's baseline (Gio
// Baseline, from the bottom), so hosts that put a doc inline (slackc's
// MrkdwnElement) line it up with the surrounding text like a browser.
func TestFirstBaseline(t *testing.T) {
	st := newTestStyle()
	v := New(doc(para(txt("Out of office")), para(txt("second paragraph"))), Options{})
	gtx := testGtx(new(op.Ops), 300)
	d := v.Layout(gtx, st, nil)
	f := findFrag(t, &v.d, "Out")
	if d.Baseline == 0 || d.Size.Y-d.Baseline != f.y {
		t.Errorf("Baseline %d (size %v), want first-line baseline %d from the top", d.Baseline, d.Size, f.y)
	}
	if b := (&display{}).firstBaseline(); b != -1 {
		t.Errorf("empty display baseline %d, want -1", b)
	}
}
