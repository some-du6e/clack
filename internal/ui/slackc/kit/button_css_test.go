package kit

import (
	"image"
	"image/color"
	"math"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// What a rule adds to a plugin's Button that Slack's c-button look has
// none of, drawn as Chrome draws it on Slack's <button>: a box-shadow
// outside the border box, a background-image over the button's
// background, an inset shadow inside its padding box, a border of the
// rule's widths on a kind with none (primary). kit's Button draws its own
// box (HostElement.OwnBox), so the engine leaves these to it, and it drew
// none of them: a Slick theme's `.c-button{border:4px outset …;
// box-shadow:4px 4px 0 …}` or material's primary box-shadow left the
// button bare, and a 3px border neither showed nor widened the button.
// Synthetic rules and colours.
const buttonLayerCSS = `
.btntest-row { display: flex; flex-wrap: wrap; gap: 16px; padding: 12px; align-items: flex-start; }
.btntest-wide { border: 3px solid rgb(0, 160, 0); }
.btntest-shadow { box-shadow: 0 0 0 3px rgb(0, 0, 255); }
.btntest-image { background-image: linear-gradient(rgb(255, 0, 0), rgb(255, 0, 0)); }
.btntest-inset { box-shadow: inset 0 0 0 3px rgb(255, 0, 255); }
`

const buttonLayerJS = `{"t":"div","k":1,"p":{"class":"btntest-row"},"c":[` +
	`{"t":"@","n":"Button","k":2,"p":{"type":"primary","size":"small","className":"btntest-wide"},"c":[{"t":"#","v":"Wide"}]},` +
	`{"t":"@","n":"Button","k":3,"p":{"type":"primary","size":"small","className":"btntest-shadow"},"c":[{"t":"#","v":"Outer"}]},` +
	`{"t":"@","n":"Button","k":4,"p":{"type":"outline","size":"small","className":"btntest-image"},"c":[{"t":"#","v":"Image"}]},` +
	`{"t":"@","n":"Button","k":5,"p":{"type":"outline","size":"small","className":"btntest-inset"},"c":[{"t":"#","v":"Inset"}]}]}`

func TestButtonCSSBoxLayers(t *testing.T) {
	slackc.SetStyle("plugin:btntest", buttonLayerCSS)
	t.Cleanup(func() { slackc.RemoveStyle("plugin:btntest") })
	e := newOwlEnvTree(t, false, buttonLayerJS)
	// the border box: Slack's 28px, the rule's 3px border on each side of
	// the primary button's 12px padding; the others keep Slack's widths
	for _, c := range []struct {
		k      int64
		label  string
		border float32
	}{{2, "Wide", 3}, {3, "Outer", 0}, {4, "Image", 1}, {5, "Inset", 1}} {
		r := e.rect(c.k)
		want := 2*c.border + 12 + e.labelW(c.label) + 12
		if math.Abs(float64(r[2]-want)) > 1.5 || !owlNear(r[3], 28) {
			t.Errorf("%s: %vx%v, want %.1fx28 (%vpx borders, 12px sides)", c.label, r[2], r[3], want, c.border)
		}
	}

	img := pixeltest.Render(t, owlW, owlH, &e.ops)
	cl := colorsOf(e.th)
	at := func(k int64, dx, dy int) (image.Point, image.Rectangle) {
		r := e.rect(k)
		b := image.Rect(int(r[0]+.5), int(r[1]+.5), int(r[0]+r[2]+.5), int(r[1]+r[3]+.5))
		return image.Pt(b.Min.X+dx, b.Min.Y+dy), b
	}
	check := func(what string, p image.Point, want color.NRGBA) {
		t.Helper()
		if c := img.RGBAAt(p.X, p.Y); !rgbNear(c, want, 8) {
			t.Errorf("%s at %v: %v, want %v", what, p, c, want)
		}
	}
	green, blue, red, magenta := color.NRGBA{0, 160, 0, 255}, color.NRGBA{0, 0, 255, 255}, color.NRGBA{255, 0, 0, 255}, color.NRGBA{255, 0, 255, 255}
	// the wide border, its edges' middles, and the green fill inside it
	p, b := at(2, 1, 14)
	check("wide border, left", p, green)
	check("wide border, right", image.Pt(b.Max.X-2, p.Y), green)
	check("wide border, top", image.Pt(b.Min.X+b.Dx()/2, b.Min.Y+1), green)
	check("inside the wide border", image.Pt(b.Min.X+5, p.Y), cl.Primary)
	// the outer shadow: a 3px ring round the button, the button over it
	p, b = at(3, -2, 14)
	check("outer shadow, left", p, blue)
	check("outer shadow, right", image.Pt(b.Max.X+1, p.Y), blue)
	check("outer shadow, below", image.Pt(b.Min.X+b.Dx()/2, b.Max.Y+1), blue)
	check("over the outer shadow", image.Pt(b.Min.X+3, p.Y), cl.Primary)
	// the image over the outline button's background, inside its border
	p, b = at(4, 4, 4)
	check("background image", p, red)
	check("background image, right", image.Pt(b.Max.X-4, b.Max.Y-4), red)
	if c := img.RGBAAt(b.Min.X, b.Min.Y+14); rgbNear(c, red, 40) {
		t.Errorf("outline border at %v is the image's red %v: the image is the padding box's (background-clip)", image.Pt(b.Min.X, b.Min.Y+14), c)
	}
	// the inset shadow: 3px inside the 1px border, the background inside it
	p, b = at(5, 2, 14)
	check("inset shadow, left", p, magenta)
	check("inset shadow, top", image.Pt(b.Min.X+b.Dx()/2, b.Min.Y+2), magenta)
	check("inside the inset shadow", image.Pt(b.Min.X+6, p.Y), cl.BtnBg)
	if n := pixeltest.Count(img, image.Rect(b.Max.X+2, b.Min.Y, b.Max.X+12, b.Max.Y), magenta, 8); n > 0 {
		t.Errorf("%d inset-shadow px outside the button", n)
	}
}

// A plugin's own <button class="c-button c-button--…"> (an element it
// renders, not kit's Button): SlickMCP's toolbar buttons (c-button
// c-button--primary|outline c-button--small) and SelfCare's submit
// (c-button--primary c-button--medium) look like Slack's buttons, from
// Slack's c-button rules: 28 (36) px tall with 12 px sides and 56 (80) px
// at least, a 1px outline border, the primary green with white bold text,
// 4 (8) px corners. They had only the inline-flex row of the reduced
// .c-button rule and drew as bare text. kit's Button beside them is
// unchanged: it draws that look itself and takes none of those rules.
const pluginCButtonJS = `{"t":"div","k":1,"p":{"class":"btntest-row"},"c":[` +
	`{"t":"button","k":2,"p":{"class":"c-button c-button--outline c-button--small"},"c":[{"t":"#","v":"Connect"}]},` +
	`{"t":"button","k":3,"p":{"class":"c-button c-button--primary c-button--small"},"c":[{"t":"#","v":"Run"}]},` +
	`{"t":"button","k":4,"p":{"class":"c-button c-button--primary c-button--medium"},"c":[{"t":"#","v":"Submit"}]},` +
	`{"t":"@","n":"Button","k":5,"p":{"type":"outline","size":"small"},"c":[{"t":"#","v":"Connect"}]}]}`

func TestPluginCButtonElements(t *testing.T) {
	slackc.SetStyle("plugin:btntest", buttonLayerCSS)
	t.Cleanup(func() { slackc.RemoveStyle("plugin:btntest") })
	e := newOwlEnvTree(t, false, pluginCButtonJS)
	outline, primary, medium, kit := e.rect(2), e.rect(3), e.rect(4), e.rect(5)
	if w := 1 + 12 + e.labelW("Connect") + 12 + 1; math.Abs(float64(outline[2]-w)) > 1.5 || !owlNear(outline[3], 28) {
		t.Errorf("outline small: %vx%v, want %.1fx28", outline[2], outline[3], w)
	}
	// "Run" is narrower than min-width less the sides
	if !owlNear(primary[2], 56) || !owlNear(primary[3], 28) {
		t.Errorf("primary small: %vx%v, want 56x28 (min-width)", primary[2], primary[3])
	}
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Constraints{Max: image.Pt(owlW, owlH)}}
	if w := float32(max(80, 12+e.th.Measure(gtx, unit.Sp(15), theme.Bold, "Submit")+12)); math.Abs(float64(medium[2]-w)) > 1.5 || !owlNear(medium[3], 36) {
		t.Errorf("primary medium: %vx%v, want %.1fx36", medium[2], medium[3], w)
	}
	// kit's Button is the same button as the element
	if !owlNear(kit[2], outline[2]) || !owlNear(kit[3], outline[3]) {
		t.Errorf("kit's outline Button %vx%v, the element %vx%v: want the same box", kit[2], kit[3], outline[2], outline[3])
	}

	img := pixeltest.Render(t, owlW, owlH, &e.ops)
	px := func(r [4]float32, dx, dy int) (image.Point, color.RGBA) {
		p := image.Pt(int(r[0]+.5)+dx, int(r[1]+.5)+dy)
		return p, img.RGBAAt(p.X, p.Y)
	}
	green := color.NRGBA{0x00, 0x7a, 0x5a, 0xff} // --dt_color-base-inv-hgl-2
	for _, c := range []struct {
		what string
		r    [4]float32
	}{{"primary small", primary}, {"primary medium", medium}} {
		if p, got := px(c.r, 4, int(c.r[3])/2); !rgbNear(got, green, 6) {
			t.Errorf("%s's fill at %v: %v, want Slack's green %v", c.what, p, got, green)
		}
		if p, got := px(c.r, 0, 0); rgbNear(got, green, 6) {
			t.Errorf("%s's corner %v is filled %v: want its radius", c.what, p, got)
		}
		if n := pixeltest.Ink(img, image.Rect(int(c.r[0]+10), int(c.r[1]+4), int(c.r[0]+c.r[2]-10), int(c.r[1]+c.r[3]-4)), color.NRGBA{255, 255, 255, 255}, green); n < 20 {
			t.Errorf("%s: %d px of white label ink", c.what, n)
		}
	}
	// the outline's border: darker than the page along its left edge, the
	// same as kit's Button's
	bg := e.th.C.Bg
	_, el := px(outline, 0, 14)
	_, kb := px(kit, 0, 14)
	if rgbNear(el, bg, 10) || !rgbNear(el, color.NRGBA{kb.R, kb.G, kb.B, 255}, 12) {
		t.Errorf("outline element's left border %v, kit's %v, page %v: want a border like kit's", el, kb, bg)
	}
}
