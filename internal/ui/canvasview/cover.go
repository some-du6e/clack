package canvasview

// The header's Add cover (docs/CANVASES.md, "Cover").
//
// Quip draws a canvas's cover in its header-image band (.header-image-
// content, quip-collab-editor HeaderImageContentView): with no cover the
// band is the 100px above the title (minHeight vertical padding - 20,
// paddingBottom 20), and for an editor it holds Slack's empty state
// (client-boot-shared EmbeddedQuipHeaderImageEmptyState): an 80px strip
// with the outline small Kit button, bottom-aligned at the text column's
// left edge (buttonLeftOffset), its 16px image-input icon 4px before
// "Add cover" (8px side padding, 13px bold).
//
// The strip (.add-header-image-container, canvas-controller CSS) is
// transparent until the pointer is over the band
// (.header-image-content:hover) or over the title section (Quip's p6:
// hoveredSectionChangeListenable on the title, and the canvas is
// editable, gives .visible), or the button has the focus (:focus-within).
// A key pressed while over the title hides it until the pointer moves
// again. Opacity fades over .25s after 30ms, both ways.
//
// A click runs Slack's addRandomHeaderImage (iXFX): canvases.listHeaders,
// one at random, docEditor.setHeaderImage. That last step is Quip's own
// editor writing the title section through edit-document, which Clack
// doesn't speak (canvases.edit has no cover operation), so Clack says it
// can't save a cover yet and sends nothing, as for other canvas parts it
// can't edit.

import (
	"image"
	"math"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// coverState is the Add cover strip's state (UI goroutine only).
type coverState struct {
	band, title     int  // tags: the header band's and the title section's hover areas
	inBand, inTitle bool // the pointer is over them
	keyed           bool // a key went down over the title: hidden until the pointer moves
	shown           bool // the strip's target opacity is 1
	from            float32
	at              time.Time // when shown last changed
	btn             widget.Clickable
	rect            image.Rectangle // the button, in the header row (tests)
}

// coverStrip is the empty state's height; the button sits on its bottom.
const coverStrip = 80

// cantCover is Clack's word for the one step it can't take.
const cantCover = "Clack can't save a cover image to a canvas yet"

// typed: a key went down in the document (p6's keydown listener).
func (c *coverState) typed() {
	if c.inTitle {
		c.keyed = true
	}
}

// visible is the strip's target state.
func (c *coverState) visible(gtx layout.Context) bool {
	return c.inBand || c.btn.Hovered() || gtx.Focused(&c.btn) || c.inTitle && !c.keyed
}

// opacity is the strip's opacity at now: CSS's opacity transition, .25s
// ease after a 30ms delay, from wherever the last change caught it.
func (c *coverState) opacity(now time.Time) (o float32, moving bool) {
	to := float32(0)
	if c.shown {
		to = 1
	}
	if c.at.IsZero() {
		return to, false
	}
	p := float64(now.Sub(c.at)-30*time.Millisecond) / float64(250*time.Millisecond)
	if p >= 1 {
		return to, false
	}
	return c.from + (to-c.from)*float32(easeCSS(math.Max(0, p))), true
}

// pointerEvents follows the pointer over tag.
func (c *coverState) pointerEvents(gtx layout.Context, tag event.Tag, in *bool) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Enter | pointer.Leave | pointer.Move | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Enter:
			*in = true
		case pointer.Move:
			*in, c.keyed = true, false
		case pointer.Leave, pointer.Cancel:
			*in = false
		}
	}
}

// layoutCover draws the Add cover strip into the header row (gtx is the
// row, w wide): x0 is the text column's left edge, and the title section
// spans titleTop to titleBottom. Editors only.
func (v *View) layoutCover(gtx layout.Context, w, x0, titleTop, titleBottom int) {
	c := &v.cover
	if !v.edit.canEdit(v) {
		*c = coverState{}
		return
	}
	th := v.h.Theme()
	col := &th.C
	c.pointerEvents(gtx, &c.band, &c.inBand)
	c.pointerEvents(gtx, &c.title, &c.inTitle)
	if c.btn.Clicked(gtx) {
		v.h.Toast(cantCover)
	}
	if vis := c.visible(gtx); vis != c.shown {
		c.from, _ = c.opacity(gtx.Now)
		c.shown, c.at = vis, gtx.Now
	}
	o, moving := c.opacity(gtx.Now)
	if moving {
		gtx.Execute(op.InvalidateCmd{})
	}

	label := nsCanvas.T("Add cover")
	pad, ip, gap := gtx.Dp(8), gtx.Dp(16), gtx.Dp(4)
	bw := pad + ip + gap + th.Measure(gtx, 13, theme.Bold, label) + pad
	bh := gtx.Dp(28)
	c.rect = image.Rectangle{Min: image.Pt(x0, gtx.Dp(coverStrip)-bh), Max: image.Pt(x0+bw, gtx.Dp(coverStrip))}
	at(gtx, c.rect.Min, c.rect.Size(), func(gtx layout.Context) layout.Dimensions {
		s := gtx.Constraints.Max
		if o > 0 {
			if o < 1 {
				defer paint.PushOpacity(gtx.Ops, o).Pop()
			}
			bg := col.Bg
			if c.btn.Hovered() {
				bg = col.BgHover
			}
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(4), bg)
			theme.Border(gtx, image.Rectangle{Max: s}, gtx.Dp(4), gtx.Dp(1), theme.WithAlpha(col.Fg, 0x21))
			at(gtx, image.Pt(pad, (s.Y-ip)/2), image.Pt(ip, ip), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("image-input")(gtx, 16, col.Fg)
			})
			x := pad + ip + gap
			at(gtx, image.Pt(x, 0), image.Pt(max(0, s.X-x), s.Y), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, s.Y, func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 13, theme.Bold, col.Fg, label)
				})
			})
		}
		return c.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			pointer.CursorPointer.Add(gtx.Ops)
			return layout.Dimensions{Size: s}
		})
	})

	// the hover areas, over everything in the row; presses pass through
	area := func(tag event.Tag, r image.Rectangle) {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, tag)
	}
	area(&c.band, image.Rect(0, 0, w, gtx.Dp(docTop)))
	if titleBottom > titleTop {
		area(&c.title, image.Rect(0, titleTop, w, titleBottom))
	}
}

// easeCSS is CSS's default "ease", cubic-bezier(.25, .1, .25, 1).
func easeCSS(x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	bez := func(a, b, t float64) float64 { u := 1 - t; return 3*u*u*t*a + 3*u*t*t*b + t*t*t }
	lo, hi, t := 0.0, 1.0, x
	for i := 0; i < 30; i++ {
		switch d := bez(0.25, 0.25, t) - x; {
		case math.Abs(d) < 1e-5:
			return bez(0.1, 1, t)
		case d < 0:
			lo = t
		default:
			hi = t
		}
		t = (lo + hi) / 2
	}
	return bez(0.1, 1, t)
}
