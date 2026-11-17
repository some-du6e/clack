package richview

// Mention pills that wrap, as Chromium lays out and paints Slack's inline
// a.c-member_slug: .c-member_slug--link { padding: 1px 2px;
// border-radius: 3px } with no box-decoration-break (so slice), in
// message text with white-space: normal (client-boot-styles
// cf726c4f2eaecc74; MemberSlug is boot-shared ld4x, build 2c0d02ca130f).
// Karim, α.102 (Windows, light): "broken": an app mention in a thread
// reply wrapped after its first word, and MentionProfiles' picture (Slick:
// a 1em inline-block ::before inside .c-member_slug, margin 0 3px .2em
// 1px, vertical-align: middle) was drawn at the line start between the
// two lines, over the text, with an empty gap at the pill's start where
// it belongs. In Chromium the pill is a box per line: the first with its
// left padding and corners, ending flush after its last word there (the
// space it broke at collapses), the next from the line start with the
// right ones; the picture sits in the first, after the 2px padding; a
// pill that starts with an icon never leaves its padding at the end of
// the line before. These tests use only what ui had before the fix, so
// the messages-audit-shots workflow runs them on ui too (where they fail)
// and saves the shots of both (MESSAGES_AUDIT_SHOTS_DIR,
// PILL_WRAP_SHOTS_TAG). Synthetic text.

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

const pwApp = "U0RELAYAPP" // an app user

// pwPictureRule is MentionProfiles' ::before (Slick src/plugins/
// MentionProfiles), on the slug itself and in solid green instead of the
// member's picture.
const pwPictureRule = `.c-member_slug::before { content: ''; display: inline-block; width: 1em; height: 1em;
	margin: 0 3px 0.2em 1px; border-radius: 50%; background: #00ff00; vertical-align: middle }`

// pwDoc is "Fields relay connector (@relay connector) Contact", the
// mention an app user's pill right after "(".
func pwDoc() rt.Doc {
	return doc(para(txt("Fields relay connector ("), rt.Span{Kind: rt.User, ID: pwApp, Text: "@relay connector"}, txt(") Contact")))
}

// pwFrag is the text frag s of a pill or link (its target set).
func pwFrag(d *display, s string) *frag {
	for i := range d.frags {
		if f := &d.frags[i]; f.kind == fText && f.sh != nil && f.sh.str == s && f.target >= 0 {
			return f
		}
	}
	return nil
}

// pwBoxes are d's filled boxes of fill, in layout order (a pill's: one
// per line).
func pwBoxes(d *display, fill color.NRGBA) []box {
	var out []box
	for _, b := range d.boxes {
		if b.kind == bFill && b.fill == fill {
			out = append(out, b)
		}
	}
	return out
}

func pwCorners(b box) [4]int {
	if b.perCorner {
		return b.corners
	}
	return [4]int{b.radius, b.radius, b.radius, b.radius}
}

// pwFrame draws v at width w over the panel color.
func pwFrame(v *View, st *Style, w, h int, ops *op.Ops) {
	ops.Reset()
	paint.FillShape(ops, st.Palette.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
	v.Layout(testGtx(ops, w), st, nil)
}

// pwCount counts img's pixels within 40 of col and their bounds.
func pwCount(img *image.RGBA, col color.NRGBA) (int, image.Rectangle) {
	n := 0
	var bb image.Rectangle
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			d := max(absDiff(c.R, col.R), absDiff(c.G, col.G), absDiff(c.B, col.B))
			if d > 40 {
				continue
			}
			n++
			r := image.Rect(x, y, x+1, y+1)
			if bb.Empty() {
				bb = r
			} else {
				bb = bb.Union(r)
			}
		}
	}
	return n, bb
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// pwSave saves img as the shot name (tag: PILL_WRAP_SHOTS_TAG, "after").
func pwSave(t *testing.T, name string, img image.Image) {
	t.Helper()
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	if dir == "" {
		return
	}
	tag := os.Getenv("PILL_WRAP_SHOTS_TAG")
	if tag == "" {
		tag = "after"
	}
	f, err := os.Create(filepath.Join(dir, name+"-"+tag+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// At every width the pill wraps at, its boxes slice it as Chromium does:
// the first from its 2px start padding to flush after "@relay" (the space
// it broke at collapses, no padding where it broke) with the left
// corners rounded, the second from the line start to its 2px end padding
// with the right ones.
func TestWrappedPillSlicesLikeChromium(t *testing.T) {
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	bg := st.Palette.MentionBg
	wrapped := 0
	for w := 100; w <= 560; w++ {
		v := layoutAt(st, pwDoc(), Options{}, w)
		at, conn := pwFrag(&v.d, "@relay"), pwFrag(&v.d, "connector")
		if at == nil || conn == nil {
			t.Fatalf("w=%d: no pill words in %q", w, fragStrings(&v.d))
		}
		bs := pwBoxes(&v.d, bg)
		if at.line == conn.line {
			if len(bs) != 1 || pwCorners(bs[0]) != [4]int{3, 3, 3, 3} {
				t.Errorf("w=%d, on one line: boxes %+v, want one with Slack's 3px corners", w, bs)
			}
			continue
		}
		wrapped++
		if len(bs) != 2 {
			t.Errorf("w=%d: the wrapped pill has %d boxes, want one per line", w, len(bs))
			continue
		}
		b1, b2 := bs[0], bs[1]
		if x := int(at.x - 2 + 0.5); b1.r.Min.X != x {
			t.Errorf("w=%d: the first line's box starts at %d, want %d (2px before @relay)", w, b1.r.Min.X, x)
		}
		if x := int(at.x + at.w + 0.5); b1.r.Max.X != x {
			t.Errorf("w=%d: the first line's box ends at %d, want %d: flush after @relay (the space it broke at collapses, no padding at the break)", w, b1.r.Max.X, x)
		}
		if x := int(conn.x + 0.5); b2.r.Min.X != x {
			t.Errorf("w=%d: the second line's box starts at %d, want %d (no padding at the break)", w, b2.r.Min.X, x)
		}
		if x := int(conn.x + conn.w + 2 + 0.5); b2.r.Max.X != x {
			t.Errorf("w=%d: the second line's box ends at %d, want %d (2px after connector)", w, b2.r.Max.X, x)
		}
		if c := pwCorners(b1); c != [4]int{3, 0, 0, 3} {
			t.Errorf("w=%d: the first line's corners %v, want NW SW rounded (slice)", w, c)
		}
		if c := pwCorners(b2); c != [4]int{0, 3, 3, 0} {
			t.Errorf("w=%d: the second line's corners %v, want NE SE rounded (slice)", w, c)
		}
		if b1.r.Dy() != b2.r.Dy() {
			t.Errorf("w=%d: box heights %d and %d", w, b1.r.Dy(), b2.r.Dy())
		}
	}
	if wrapped < 8 {
		t.Fatalf("the pill wrapped at %d widths, want a sweep", wrapped)
	}
}

// A pill that starts with an icon (a message link's RichTextSlug, a
// truncated link's chip, a canvas slug): its 2px start padding goes to
// the next line with the icon, as Chromium moves an inline box's start to
// the line its first content goes to; no sliver of pill ends the line
// before, and the icon sits in the pill's first box after the padding.
func TestPillPaddingStaysWithItsIcon(t *testing.T) {
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	bg := st.Palette.MentionBg
	for _, c := range []struct {
		name, icon string
		span       rt.Span
	}{
		{"message link", "message", rt.Span{Kind: rt.MessageMention, Text: "alice in #general", ID: "1700000000.000100",
			URL: "https://synthetic.slack.com/archives/C1/p1700000000000100"}},
		{"truncated link", "link", rt.Span{Kind: rt.Link, Text: "example.com/docs/…/abcdefghij…", URL: "https://example.com/docs/guides/abcdefghij", Truncated: true}},
		{"canvas", "canvas-content", rt.Span{Kind: rt.Canvas, ID: "F0CANV0001", URL: "https://synthetic.slack.com/docs/T1/F0CANV0001", Icon: "canvas-content",
			Text: "Launch plan", Parts: &rt.SpanParts{Spans: []rt.Span{{Kind: rt.Text, Text: "Launch plan"}}}}},
	} {
		moved := 0
		for w := 30; w <= 260; w++ {
			v := layoutAt(st, doc(para(txt("see the "), c.span, txt(" today"))), Options{}, w)
			var icon *frag
			for i := range v.d.frags {
				if f := &v.d.frags[i]; f.kind == fImage && f.url == iconURL+c.icon {
					icon = f
					break
				}
			}
			bs := pwBoxes(&v.d, bg)
			if icon == nil || len(bs) == 0 {
				t.Fatalf("%s, w=%d: icon %v, boxes %v", c.name, w, icon, bs)
			}
			for _, b := range bs {
				has := false
				for i := range v.d.frags {
					f := &v.d.frags[i]
					if f.target < 0 || f.top > b.r.Min.Y || f.bot < b.r.Max.Y {
						continue
					}
					if r := fragRect(f); r.Min.X < b.r.Max.X && r.Max.X > b.r.Min.X {
						has = true
						break
					}
				}
				if !has {
					t.Errorf("%s, w=%d: a %dpx box of the pill at %v holds nothing of it: its start padding was left at the end of the line", c.name, w, b.r.Dx(), b.r)
				}
			}
			b1 := bs[0]
			if math.Abs(float64(icon.x-1-float32(b1.r.Min.X)-2)) > 0.6 || icon.top > b1.r.Min.Y || icon.bot < b1.r.Max.Y {
				t.Errorf("%s, w=%d: the icon at x=%.1f (line %d..%d) isn't 2px into the pill's first box %v", c.name, w, icon.x-1, icon.top, icon.bot, b1.r)
			}
			if see := findFrag(t, &v.d, "see"); icon.line > see.line {
				moved++
			}
		}
		if moved == 0 {
			t.Errorf("%s: the pill never went to the next line", c.name)
		}
	}
}

// MentionProfiles' picture in a pill that wraps: drawn once, in the
// pill's first line box 3px in (its 2px padding and 1px margin), aligned
// vertical-align: middle on that line (its margin box's middle half the
// x-height above the baseline), over no text, in each of Chromium's three
// ways the pill breaks: at the space in it ("space"), after the picture
// with "@relay" on the next line ("after", the picture then ends its box
// 3px before the box's end), and after "(" with the picture opening the
// next line ("paren").
func TestWrappedPillPictureDraws(t *testing.T) {
	slackc.SetStyle("test:pill-wrap", pwPictureRule)
	t.Cleanup(func() { slackc.RemoveStyle("test:pill-wrap") })
	green := color.NRGBA{G: 0xff, A: 0xff}
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	const h = 110
	ops := new(op.Ops)
	done, last := map[string]int{}, map[string]int{}
	for w := 150; w <= 520; w++ {
		v := New(pwDoc(), Options{})
		for i := 0; i < 4; i++ {
			pwFrame(v, st, w, h, ops)
		}
		at := pwFrag(&v.d, "@relay")
		lp := findFrag(t, &v.d, "(")
		bs := pwBoxes(&v.d, st.Palette.MentionBg)
		if at == nil || len(bs) == 0 {
			t.Fatalf("w=%d: @relay %v, boxes %v", w, at, bs)
		}
		b1 := bs[0].r
		kind, base := "", at.y
		switch {
		case b1.Min.Y >= lp.bot:
			kind = "paren" // "(" ended its line, the pill opens the next
		case len(bs) < 2:
			continue // on one line
		case b1.Max.Y <= at.top:
			kind, base = "after", lp.y
		default:
			kind = "space"
		}
		if l, ok := last[kind]; done[kind] >= 2 || ok && w-l < 12 {
			continue
		}
		last[kind] = w
		done[kind]++
		img := pixeltest.Render(t, w, h, ops)
		pwSave(t, fmt.Sprintf("pill-wrap-richview-picture-%s-%d", kind, w), img)
		n, bb := pwCount(img, green)
		if n < 80 || bb.Dx() > 18 || bb.Dy() > 18 {
			t.Errorf("%s, w=%d: %d picture pixels in %v, want one 15px circle", kind, w, n, bb)
			continue
		}
		if bb.Min.Y < b1.Min.Y-1 || bb.Max.Y > b1.Max.Y+1 || bb.Min.X < b1.Min.X+2 || bb.Min.X > b1.Min.X+4 {
			t.Errorf("%s, w=%d: the picture at %v isn't 3px into the pill's first line box %v", kind, w, bb, b1)
		}
		if kind == "after" {
			if d := b1.Max.X - bb.Max.X; d < 2 || d > 4 || bb.Max.Y > at.top+1 {
				t.Errorf("after, w=%d: the picture at %v, its box %v and @relay's line from %d: want it to end 3px before the box, on the line above", w, bb, b1, at.top)
			}
		} else if bb.Max.X > int(at.x)+1 || bb.Min.X < int(at.x)-22 || bb.Min.Y < at.top || bb.Max.Y > at.bot {
			t.Errorf("%s, w=%d: the picture at %v isn't just before @relay (x %.1f, line %d..%d)", kind, w, bb, at.x, at.top, at.bot)
		}
		for i := range v.d.frags {
			if f := &v.d.frags[i]; f.kind == fText && f.sh != nil && fragRect(f).Overlaps(bb) {
				t.Errorf("%s, w=%d: the picture at %v is drawn over %q at %v", kind, w, bb, f.sh.str, fragRect(f))
			}
		}
		cy := float32(bb.Min.Y+bb.Max.Y) / 2
		want := float32(base) - at.sh.asc*0.52/2 - 0.1*float32(at.size)
		if math.Abs(float64(cy-want)) > 1.5 {
			t.Errorf("%s, w=%d: the picture's middle at y=%.1f, want %.1f (vertical-align: middle on the baseline at %d)", kind, w, cy, want, base)
		}
	}
	for _, k := range []string{"space", "after", "paren"} {
		if done[k] == 0 {
			t.Errorf("never saw the pill break %q: Chromium breaks at the space, after the picture and after \"(\" before it", k)
		}
	}
}

// pwOutside counts img's pixels within 40 of col that lie in none of rs,
// and their bounds; in counts those in each.
func pwOutside(img *image.RGBA, col color.NRGBA, rs []image.Rectangle) (out int, in []int, stray image.Rectangle) {
	in = make([]int, len(rs))
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if max(absDiff(c.R, col.R), absDiff(c.G, col.G), absDiff(c.B, col.B)) > 40 {
				continue
			}
			p, hit := image.Pt(x, y), false
			for i, r := range rs {
				if p.In(r) {
					in[i]++
					hit = true
					break
				}
			}
			if !hit {
				out++
				stray = stray.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return out, in, stray
}

// A rule's background on the slug (here magenta) under a pill that wraps
// is painted on each line's box, as Chromium paints the inline element's
// fragments, never on one box around both lines (over the text beside
// them): a member's pill and a channel's (BaseMrkdwnChannel, whose link
// is a.c-member_slug too).
func TestWrappedPillCSSBackground(t *testing.T) {
	slackc.SetStyle("test:pill-bg", `a.c-member_slug.c-member_slug--link { background-color: #ff00ff }`)
	t.Cleanup(func() { slackc.RemoveStyle("test:pill-bg") })
	magenta := color.NRGBA{R: 0xff, B: 0xff, A: 0xff}
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	const h = 110
	ops := new(op.Ops)
	for _, c := range []struct {
		name string
		d    rt.Doc
	}{
		{"member", pwDoc()},
		{"channel", doc(para(txt("Fields relay connector ("), rt.Span{Kind: rt.Channel, ID: "C0RELAY001", Text: "#relay-connector-intake"}, txt(") Contact")))},
	} {
		checked, last := 0, -100
		for w := 150; w <= 520 && checked < 4; w++ {
			v := New(c.d, Options{})
			for i := 0; i < 4; i++ {
				pwFrame(v, st, w, h, ops)
			}
			bs := pwBoxes(&v.d, st.Palette.MentionBg)
			if len(bs) < 2 || w-last < 40 {
				continue
			}
			last = w
			checked++
			img := pixeltest.Render(t, w, h, ops)
			pwSave(t, fmt.Sprintf("pill-wrap-richview-css-background-%s-%d", c.name, w), img)
			var rs []image.Rectangle
			for _, b := range bs {
				rs = append(rs, b.r)
			}
			out, in, stray := pwOutside(img, magenta, rs)
			if out > 0 {
				t.Errorf("%s, w=%d: %d px of the slug's background outside its line boxes %v (in %v): painted around the lines", c.name, w, out, rs, stray)
			}
			for i := range in {
				if area := rs[i].Dx() * rs[i].Dy(); in[i] < area/3 {
					t.Errorf("%s, w=%d: line %d's box %v has %d px of the background (area %d)", c.name, w, i+1, rs[i], in[i], area)
				}
			}
		}
		if checked < 2 {
			t.Errorf("%s: checked %d wrapped widths", c.name, checked)
		}
	}
}

// A rule's border and box-shadow (here a 2px magenta border, a 3px cyan
// spread) on a pill that wraps, in a quote (its text starts 16px in, so
// the second line's left side has room): each line's box gets them on its
// top and bottom; the start edge's border and shadow only on the first,
// the end edge's only on the last; nothing at the sides where it broke,
// as Chromium's slice and its shadow clip (ClipToSides) paint them.
func TestWrappedPillBorderShadow(t *testing.T) {
	slackc.SetStyle("test:pill-edges", `a.c-member_slug.c-member_slug--link { border: 2px solid #ff00ff; box-shadow: 0 0 0 3px #00ffff }`)
	t.Cleanup(func() { slackc.RemoveStyle("test:pill-edges") })
	magenta, cyan := color.NRGBA{R: 0xff, B: 0xff, A: 0xff}, color.NRGBA{G: 0xff, B: 0xff, A: 0xff}
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	const h = 120
	ops := new(op.Ops)
	d := doc(rt.Block{Kind: rt.Quote, Spans: pwDoc().Blocks[0].Spans})
	checked, last := 0, -100
	for w := 170; w <= 540 && checked < 3; w++ {
		v := New(d, Options{})
		for i := 0; i < 4; i++ {
			pwFrame(v, st, w, h, ops)
		}
		at, conn := pwFrag(&v.d, "@relay"), pwFrag(&v.d, "connector")
		bs := pwBoxes(&v.d, st.Palette.MentionBg)
		if at == nil || conn == nil || len(bs) != 2 || at.line == conn.line || w-last < 30 {
			continue
		}
		last = w
		checked++
		img := pixeltest.Render(t, w, h, ops)
		pwSave(t, fmt.Sprintf("pill-wrap-richview-border-shadow-%d", w), img)
		b1, b2 := bs[0].r, bs[1].r
		at2 := func(x, y int) color.NRGBA { c := img.RGBAAt(x, y); return color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0xff} }
		is := func(c, want color.NRGBA) bool {
			return max(absDiff(c.R, want.R), absDiff(c.G, want.G), absDiff(c.B, want.B)) <= 40
		}
		check := func(what string, x, y int, want color.NRGBA, yes bool) {
			if got := at2(x, y); is(got, want) != yes {
				t.Errorf("w=%d: %s at (%d,%d) is %v", w, what, x, y, got)
			}
		}
		m1, m2 := (b1.Min.Y+b1.Max.Y)/2, (b2.Min.Y+b2.Max.Y)/2
		check("line 1's start border", b1.Min.X-1, m1, magenta, true)
		check("line 1's start shadow", b1.Min.X-4, m1, cyan, true)
		check("line 1's top border", (b1.Min.X+b1.Max.X)/2, b1.Min.Y-1, magenta, true)
		check("no border where line 1 broke", b1.Max.X, m1, magenta, false)
		check("no shadow where line 1 broke", b1.Max.X+3, m1, cyan, false)
		check("no border where line 2 goes on", b2.Min.X-1, m2, magenta, false)
		check("no shadow where line 2 goes on", b2.Min.X-4, m2, cyan, false)
		check("line 2's end border", b2.Max.X, m2, magenta, true)
		check("line 2's end shadow", b2.Max.X+4, m2, cyan, true)
		check("line 2's bottom border", (b2.Min.X+b2.Max.X)/2, b2.Max.Y, magenta, true)
		check("line 2's top shadow up to where it goes on", b2.Min.X+1, b2.Min.Y-4, cyan, true)
	}
	if checked < 2 {
		t.Fatalf("checked %d wrapped widths", checked)
	}
}

// Hovering a pill that wraps, on its second line: the pointer there is
// on the mention, and both lines' boxes take Slack's hover background
// (rgba(--sk_highlight_accent, .2), .c-member_slug--link:hover) in place
// of the pill's own, square where it broke; its text takes
// --sk_highlight_hover. Natively, and as a component (a stylesheet on).
func TestWrappedPillHover(t *testing.T) {
	pal := LightPalette()
	st := NewStyle(themeShaper(), noImages{}, pal)
	const h = 110
	for _, mode := range []string{"native", "component"} {
		if mode == "component" {
			slackc.SetStyle("test:pill-hover", pwPictureRule)
		}
		checked, last := 0, -100
		for w := 150; w <= 520 && checked < 3; w++ {
			v := New(pwDoc(), Options{})
			var ops op.Ops
			for i := 0; i < 4; i++ {
				pwFrame(v, st, w, h, &ops)
			}
			at, conn := pwFrag(&v.d, "@relay"), pwFrag(&v.d, "connector")
			bs := pwBoxes(&v.d, pal.MentionBg)
			if at == nil || conn == nil || len(bs) != 2 || at.line == conn.line || w-last < 30 {
				continue
			}
			last = w
			checked++
			b1, b2 := bs[0].r, bs[1].r
			if got := v.targetAt(rectCenter(b2)); got != at.target || got < 0 {
				t.Errorf("%s, w=%d: the pointer on the second line's box is on target %d, want the mention's %d", mode, w, got, at.target)
				continue
			}
			draw := func(hover int16) *image.RGBA {
				ops.Reset()
				paint.FillShape(&ops, pal.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
				v.hover = hover
				v.draw(testGtx(&ops, w), st)
				return pixeltest.Render(t, w, h, &ops)
			}
			plain, hov := draw(-1), draw(at.target)
			pwSave(t, fmt.Sprintf("pill-wrap-richview-hover-%s-%d", mode, w), hov)
			for li, r := range []image.Rectangle{b1, b2} {
				// over the box's top padding, under no glyph, inside its corners
				y := r.Min.Y + 2
				for _, x := range []int{r.Min.X + 2, (r.Min.X + r.Max.X) / 2, r.Max.X - 3} {
					if n := pixeltest.Count(plain, image.Rect(x, y, x+1, y+1), pal.MentionBg, 6); n != 1 {
						t.Errorf("%s, w=%d: line %d at (%d,%d) isn't the pill's background unhovered: %v", mode, w, li+1, x, y, plain.RGBAAt(x, y))
					}
					if n := pixeltest.Count(hov, image.Rect(x, y, x+1, y+1), pal.MentionHoverBg, 6); n != 1 {
						t.Errorf("%s, w=%d: line %d at (%d,%d) is %v hovered, want the hover background %v", mode, w, li+1, x, y, hov.RGBAAt(x, y), pal.MentionHoverBg)
					}
				}
			}
			// square where it broke: line 1's top-right corner pixel and line
			// 2's top-left are the hover color too
			for _, p := range []image.Point{{b1.Max.X - 1, b1.Min.Y}, {b2.Min.X, b2.Min.Y}} {
				if n := pixeltest.Count(hov, image.Rect(p.X, p.Y, p.X+1, p.Y+1), pal.MentionHoverBg, 10); n != 1 {
					t.Errorf("%s, w=%d: the hovered box's corner at %v is %v, want square where the pill broke", mode, w, p, hov.RGBAAt(p.X, p.Y))
				}
			}
			// the text in the hover color
			txt := image.Rect(int(at.x), at.y-at.size, int(at.x+at.w), at.y+2)
			if n, was := pixeltest.Count(hov, txt, pal.SlugHoverText, 10), pixeltest.Count(plain, txt, pal.SlugHoverText, 10); n < 10 || n < 3*was {
				t.Errorf("%s, w=%d: %d px of @relay in the hover text color (%d unhovered)", mode, w, n, was)
			}
		}
		if mode == "component" {
			slackc.RemoveStyle("test:pill-hover")
		}
		if checked < 2 {
			t.Errorf("%s: checked %d wrapped widths", mode, checked)
		}
	}
}

// Inline code that wraps, as Chromium slices code.c-mrkdwn__code: its
// 1px border is closed only at the code's two ends, not where it broke,
// and the space it broke at collapses (its first line's box ends at its
// last word, no border on it).
func TestCodeChipWrapBorders(t *testing.T) {
	pal := LightPalette()
	st := NewStyle(themeShaper(), noImages{}, pal)
	d := doc(para(txt("run "), rt.Span{Kind: rt.Text, Text: "npm install --save-dev some-package", Style: rt.Code}, txt(" now")))
	const h = 120
	ops := new(op.Ops)
	checked, last := 0, -100
	for w := 140; w <= 420 && checked < 3; w++ {
		v := New(d, Options{})
		pwFrame(v, st, w, h, ops)
		var bs []image.Rectangle
		for _, b := range v.d.boxes {
			if b.kind == bBorder && b.fill == pal.CodeBg {
				bs = append(bs, b.r)
			}
		}
		if len(bs) < 2 || w-last < 30 {
			continue
		}
		last = w
		checked++
		img := pixeltest.Render(t, w, h, ops)
		pwSave(t, fmt.Sprintf("pill-wrap-richview-code-%d", w), img)
		at := func(what string, x, y int, want color.NRGBA) {
			if pixeltest.Count(img, image.Rect(x, y, x+1, y+1), want, 6) != 1 {
				t.Errorf("w=%d: %s at (%d,%d) is %v, want %v", w, what, x, y, img.RGBAAt(x, y), want)
			}
		}
		first, end := bs[0], bs[len(bs)-1]
		at("the code's start border", first.Min.X, (first.Min.Y+first.Max.Y)/2, pal.CodeBorder)
		at("the code's end border", end.Max.X-1, (end.Min.Y+end.Max.Y)/2, pal.CodeBorder)
		for i, r := range bs {
			y := r.Min.Y + 3 // under the corner, above the glyphs
			if i < len(bs)-1 {
				at(fmt.Sprintf("line %d's open end", i+1), r.Max.X-1, y, pal.CodeBg)
			}
			if i > 0 {
				at(fmt.Sprintf("line %d's open start", i+1), r.Min.X, y, pal.CodeBg)
			}
		}
	}
	if checked < 2 {
		t.Fatalf("checked %d wrapped widths", checked)
	}
}

// A rich text colour (boot-shared d7OQ Color): the value, then its
// swatch, div.c-mrkdwn__hex: 13px square plus a 1px border in
// --sk_foreground_max_solid (97,96,97), 2px after the value, its bottom
// 1px under the baseline; an atomic inline, which can start the next line
// alone.
func TestColorSwatchAfterValue(t *testing.T) {
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	d := doc(para(txt("brand colour "), rt.Span{Kind: rt.ColorSwatch, Text: "#1D9BD1"}, txt(" ok")))
	swatch := color.NRGBA{R: 0x1d, G: 0x9b, B: 0xd1, A: 0xff}
	alone := 0
	for w := 120; w <= 320; w++ {
		v := layoutAt(st, d, Options{}, w)
		var sw *box
		for i := range v.d.boxes {
			if b := &v.d.boxes[i]; b.kind == bBorder && b.fill == swatch {
				sw = b
			}
		}
		val := findFrag(t, &v.d, "#1D9BD1")
		if sw == nil {
			t.Fatalf("w=%d: no swatch in %+v", w, v.d.boxes)
		}
		if sw.r.Dx() != 15 || sw.r.Dy() != 15 || sw.stroke != (color.NRGBA{R: 97, G: 96, B: 97, A: 0xff}) || sw.bw != 1 || sw.radius != 3 {
			t.Errorf("w=%d: the swatch %v stroke %v bw %d radius %d, want 15px with a 1px rgb(97,96,97) border, radius 3", w, sw.r, sw.stroke, sw.bw, sw.radius)
		}
		switch {
		case sw.r.Min.Y >= val.bot:
			alone++
			if sw.r.Min.X != 2 {
				t.Errorf("w=%d: the swatch alone on the next line at x=%d, want 2 (its margin)", w, sw.r.Min.X)
			}
		case sw.r.Min.X != int(val.x+val.w+0.5)+2:
			t.Errorf("w=%d: the swatch at x=%d, the value ends at %.1f: want it 2px after the value", w, sw.r.Min.X, val.x+val.w)
		}
		var lineBase int
		for i := range v.d.frags {
			if f := &v.d.frags[i]; f.kind == fText && f.top <= sw.r.Min.Y && f.bot >= sw.r.Max.Y {
				lineBase = f.y
			}
		}
		if lineBase != 0 && sw.r.Max.Y != lineBase+1 {
			t.Errorf("w=%d: the swatch ends at y=%d, its line's baseline is %d: want 1px under it", w, sw.r.Max.Y, lineBase)
		}
	}
	if alone == 0 {
		t.Error("the swatch never went to the next line on its own: it's an atomic inline")
	}
}
