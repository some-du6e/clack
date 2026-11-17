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

// MentionProfiles' picture in a pill that wraps: drawn once, at the
// start of the pill's first line just before "@relay", aligned
// vertical-align: middle on that line (its margin box's middle half the
// x-height above the baseline), over no text.
func TestWrappedPillPictureDraws(t *testing.T) {
	slackc.SetStyle("test:pill-wrap", pwPictureRule)
	t.Cleanup(func() { slackc.RemoveStyle("test:pill-wrap") })
	green := color.NRGBA{G: 0xff, A: 0xff}
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	const h = 110
	ops := new(op.Ops)
	checked, last := 0, -100
	for w := 150; w <= 520 && checked < 6; w++ {
		v := New(pwDoc(), Options{})
		for i := 0; i < 4; i++ {
			pwFrame(v, st, w, h, ops)
		}
		at, conn := pwFrag(&v.d, "@relay"), pwFrag(&v.d, "connector")
		if at == nil || conn == nil {
			t.Fatalf("w=%d: no pill words in %q", w, fragStrings(&v.d))
		}
		if at.line == conn.line || w-last < 25 {
			continue
		}
		last = w
		checked++
		img := pixeltest.Render(t, w, h, ops)
		pwSave(t, fmt.Sprintf("pill-wrap-richview-picture-%d", w), img)
		n, bb := pwCount(img, green)
		if n < 80 || bb.Dx() > 18 || bb.Dy() > 18 {
			t.Errorf("w=%d: %d picture pixels in %v, want one 15px circle", w, n, bb)
			continue
		}
		if bb.Min.Y < at.top || bb.Max.Y > at.bot {
			t.Errorf("w=%d: the picture at %v isn't on the pill's first line (y %d..%d)", w, bb, at.top, at.bot)
		}
		if bb.Max.X > int(at.x)+1 || bb.Min.X < int(at.x)-22 {
			t.Errorf("w=%d: the picture at %v isn't just before @relay (x %.1f)", w, bb, at.x)
		}
		for i := range v.d.frags {
			if f := &v.d.frags[i]; f.kind == fText && f.sh != nil && fragRect(f).Overlaps(bb) {
				t.Errorf("w=%d: the picture at %v is drawn over %q at %v", w, bb, f.sh.str, fragRect(f))
			}
		}
		cy := float32(bb.Min.Y+bb.Max.Y) / 2
		want := float32(at.y) - at.sh.asc*0.52/2 - 0.1*float32(at.size)
		if math.Abs(float64(cy-want)) > 1.5 {
			t.Errorf("w=%d: the picture's middle at y=%.1f, want %.1f (vertical-align: middle on the baseline at %d)", w, cy, want, at.y)
		}
	}
	if checked < 3 {
		t.Fatalf("checked the picture at %d wrapped widths, want a sweep", checked)
	}
}

// A rule's background on the slug (here magenta) under a pill that wraps
// is painted on each line's box, as Chromium paints the inline element's
// fragments, never on one box around both lines (over the text beside
// them).
func TestWrappedPillCSSBackground(t *testing.T) {
	slackc.SetStyle("test:pill-bg", `a.c-member_slug.c-member_slug--link { background-color: #ff00ff }`)
	t.Cleanup(func() { slackc.RemoveStyle("test:pill-bg") })
	magenta := color.NRGBA{R: 0xff, B: 0xff, A: 0xff}
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	const h = 110
	ops := new(op.Ops)
	checked, last := 0, -100
	for w := 150; w <= 520 && checked < 4; w++ {
		v := New(pwDoc(), Options{})
		for i := 0; i < 4; i++ {
			pwFrame(v, st, w, h, ops)
		}
		at, conn := pwFrag(&v.d, "@relay"), pwFrag(&v.d, "connector")
		if at == nil || conn == nil || at.line == conn.line || w-last < 40 {
			continue
		}
		bs := pwBoxes(&v.d, st.Palette.MentionBg)
		if len(bs) != 2 {
			t.Errorf("w=%d: %d pill boxes", w, len(bs))
			continue
		}
		last = w
		checked++
		img := pixeltest.Render(t, w, h, ops)
		pwSave(t, fmt.Sprintf("pill-wrap-richview-css-background-%d", w), img)
		out, in := 0, [2]int{}
		var stray image.Rectangle
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := img.RGBAAt(x, y)
				if max(absDiff(c.R, magenta.R), absDiff(c.G, magenta.G), absDiff(c.B, magenta.B)) > 40 {
					continue
				}
				p := image.Pt(x, y)
				switch {
				case p.In(bs[0].r):
					in[0]++
				case p.In(bs[1].r):
					in[1]++
				default:
					out++
					stray = stray.Union(image.Rect(x, y, x+1, y+1))
				}
			}
		}
		if out > 0 {
			t.Errorf("w=%d: %d px of the slug's background outside its line boxes %v %v (in %v): painted around both lines", w, out, bs[0].r, bs[1].r, stray)
		}
		for i := range in {
			if area := bs[i].r.Dx() * bs[i].r.Dy(); in[i] < area/3 {
				t.Errorf("w=%d: line %d's box %v has %d px of the background (area %d)", w, i+1, bs[i].r, in[i], area)
			}
		}
	}
	if checked < 2 {
		t.Fatalf("checked %d wrapped widths", checked)
	}
}
