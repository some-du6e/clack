package messages

// OwlMode, in a thread it replies to through Owl, patches ReactionBar to
// hand Original its className plus its own class (taut-owl-reactions and
// a theme class) and rings every pill in the owl colour:
// .c-reaction_bar.<its class> .c-reaction{box-shadow:0 0 0 1px <owl>},
// with an owl-tinted hover and reacted background. In Slack that ring
// replaces the reacted pill's own (client-boot-styles cf726c4f:
// .c-reaction--reacted{box-shadow:0 0 0 1px var(--dt_color-base-inv-hgl-1)}),
// and it follows the pill's 9999px radius. α.113 entered .c-reaction_bar
// without the className, so no rule reached the pills. The plugin here
// does the same with its own class and rule (Taut is GPLv3; its source
// isn't copied).

import (
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

const ringReactionsPlugin = `(() => {
const TautPlugin = globalThis.TautPlugin;
class RingReactions extends TautPlugin {
  static id = 'RingReactions'
  static pluginName = 'Ring Reactions'
  static description = 'rings the reaction pills in red'
  static authors = ['jeremy']
  static defaultConfig = { enabled: true }
  start() {
    this.api.setStyle('.c-reaction_bar.ring-test .c-reaction { box-shadow: 0 0 0 1px rgb(214, 48, 49); }')
    this.api.patchComponent('ReactionBar', (Original) => (props) =>
      React.createElement(Original, { ...props, className: (props.className ?? '') + ' ring-test' }))
  }
}
return RingReactions
})()`

var ringRed = color.NRGBA{R: 214, G: 48, B: 49, A: 0xff}

// near reports c within tol of want on every channel.
func ringNear(c color.RGBA, want color.NRGBA, tol int) bool {
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return d(c.R, want.R) <= tol && d(c.G, want.G) <= tol && d(c.B, want.B) <= tol
}

// ringPixels counts the pixels near c in img's rect r and their bounds.
func ringPixels(img *image.RGBA, r image.Rectangle, c color.NRGBA, tol int) (int, image.Rectangle) {
	n, bb := 0, image.Rectangle{}
	b := r.Intersect(img.Bounds())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if ringNear(img.RGBAAt(x, y), c, tol) {
				n++
				bb = bb.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return n, bb
}

func TestOwlReactionsRing(t *testing.T) {
	_, _, tt, _, h := pluginEnv(t)
	install(t, tt, []byte(ringReactionsPlugin))
	until(t, 5*time.Second, "ReactionBar patched", func() {}, func() bool { return slackc.Patched("ReactionBar") })
	v := NewChannelView(h, tGen)
	v.Preview = true
	const w, hgt = 700, 560
	ops := new(op.Ops)
	frame := func() { shotFrame(v, h.th, w, hgt, ops) }
	until(t, 5*time.Second, "the reacted message", frame, func() bool { return v.s.rows[tsTada] != nil })
	var img *image.RGBA
	var n int
	var ring image.Rectangle
	// the reacted message's row, in the view
	rowRect := func() image.Rectangle {
		s := v.s
		for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
			if s.items[i].key == tsTada && s.heights[i] > 0 {
				return image.Rect(0, s.pos[i], w, s.pos[i]+s.heights[i])
			}
		}
		return image.Rectangle{}
	}
	until(t, 10*time.Second, "the pill ringed in the rule's red", frame, func() bool {
		img = pixeltest.Render(t, w, hgt, ops)
		n, ring = ringPixels(img, rowRect(), ringRed, 40)
		return n >= 60
	})
	// one pill: the 24px pill and the 1px ring around it
	if ring.Dy() < 25 || ring.Dy() > 27 || ring.Dx() < 30 || ring.Dx() > 80 {
		t.Errorf("red ring bounds %v (%d px): want one 26px-tall ring around the 24px pill", ring, n)
	}
	// round ends: the bounds' corners are outside the ring, the middle of
	// its left edge on it
	mid := (ring.Min.Y + ring.Max.Y) / 2
	for _, p := range []image.Point{ring.Min, {ring.Max.X - 1, ring.Min.Y}, {ring.Min.X, ring.Max.Y - 1}, ring.Max.Sub(image.Pt(1, 1))} {
		if ringNear(img.RGBAAt(p.X, p.Y), ringRed, 60) {
			t.Errorf("ring corner %v is red: drawn square, not round like the pill", p)
		}
	}
	if !ringNear(img.RGBAAt(ring.Min.X, mid), ringRed, 60) && !ringNear(img.RGBAAt(ring.Min.X+1, mid), ringRed, 60) {
		t.Errorf("no ring at the pill's left end (%v)", image.Pt(ring.Min.X, mid))
	}
	// the rule's ring replaces the reacted pill's own (Slack's is a
	// box-shadow too): none of the native ring's blue along the pill's top
	// and bottom edges, just inside the rule's ring (the count's glyphs,
	// the same blue, sit in the middle)
	c := colorsFor(h.th)
	nb := 0
	for _, y := range []int{ring.Min.Y + 1, ring.Max.Y - 2} {
		nb += pixeltest.Count(img, image.Rect(ring.Min.X+12, y, ring.Max.X-12, y+1), c.ReactSelfBorder, 24)
	}
	if nb > 2 {
		t.Errorf("%d px of the reacted pill's own ring colour %v on its edges inside the rule's ring: both drawn", nb, c.ReactSelfBorder)
	}
}
