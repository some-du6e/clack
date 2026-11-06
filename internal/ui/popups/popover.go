package popups

import (
	"image"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// ---- Popover ----

// PopoverOpts configure a popover.
type PopoverOpts struct {
	Anchor  image.Rectangle // element the popover belongs to (window coords)
	Side    Side            // preferred side (Below default)
	Gap     float32         // dp between anchor and card (default 4)
	Width   float32         // dp; 0 = content width
	Radius  float32         // dp; default 8
	OnClose func()
	// Modal dims the window behind the popover.
	Modal bool
}

// Popover is a card with arbitrary content anchored to a rectangle; it
// closes on outside click and Escape.
type Popover struct {
	base
	h       Host
	o       PopoverOpts
	content layout.Widget
	scrim   scrim
	block   blocker
	rect    image.Rectangle
	focus   bool
}

// NewPopover builds a popover; show it with h.ShowOverlay.
func NewPopover(h Host, content layout.Widget, o PopoverOpts) *Popover {
	p := &Popover{h: h, o: o, content: content}
	p.onClose = o.OnClose
	return p
}

// Rect is the card's last laid-out rectangle (window coords).
func (p *Popover) Rect() image.Rectangle { return p.rect }

// Layout draws the popover.
func (p *Popover) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	if p.done {
		return layout.Dimensions{}
	}
	stacked(gtx, p)
	th := p.h.Theme()
	cl := colorsOf(th)
	win := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{} // robust to roots passing Exact constraints
	if !p.focus {
		gtx.Execute(key.FocusCmd{Tag: p})
		p.focus = true
	}
	if isTop(p) && escape(gtx, nil) {
		p.Dismiss()
		return layout.Dimensions{}
	}
	if p.o.Modal {
		fill(gtx, image.Rectangle{Max: win}, cl.Backdrop)
	}
	if p.scrim.Layout(gtx) {
		p.Dismiss()
		return layout.Dimensions{}
	}
	m := dp(gtx, 8)
	maxW := win.X - 2*m
	cg := loose(gtx, image.Pt(maxW, win.Y-2*m))
	if p.o.Width > 0 {
		w := dp(gtx, p.o.Width)
		if w > maxW {
			w = maxW
		}
		cg.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, win.Y-2*m)}
	}
	call, d := record(cg, p.content)
	gap := p.o.Gap
	if gap == 0 {
		gap = 4
	}
	g := dp(gtx, gap)
	a := p.o.Anchor
	switch p.o.Side {
	case Below, Above:
		a.Min.Y -= g
		a.Max.Y += g
	default:
		a.Min.X -= g
		a.Max.X += g
	}
	pos := place(a, d.Size, win, m, p.o.Side)
	p.rect = image.Rectangle{Min: pos, Max: pos.Add(d.Size)}
	rad := p.o.Radius
	if rad == 0 {
		rad = 8
	}
	card(gtx, cl, p.rect, dp(gtx, rad))
	p.block.Layout(gtx, p.rect)
	keyTarget(gtx, p, p.rect)
	cs := clipRRect(gtx, p.rect, dp(gtx, rad))
	macroAt(gtx, call, pos)
	cs.Pop()
	return layout.Dimensions{Size: win}
}

// ---- Tooltip ----
//
// Measured (c-tooltip__tip): bg #1d1c1d, radius 8, padding 8 12, 13px/700
// white (line 18). Arrow: a 12.7px rotated square. 8px gap to the anchor.
// Shortcut chips under the label (margin-top 4): bg #000, radius 4, 20px
// tall, padding 0 4, 13px/400, 4px apart. Extra lines (after "\n") are
// 13px regular.

// TipState tracks hover timing for one tooltip owner.
type TipState struct {
	since   time.Time
	hovered bool
}

// TipDelay is how long the pointer must rest before a tooltip shows.
const TipDelay = 450 * time.Millisecond

// Tooltip shows a Slack tooltip for a widget of size sz laid out at the
// current origin, while hovered. Call it right after laying the widget out.
// label may contain "\n" for a second (regular weight) line; shortcut like
// "Ctrl+Shift+K" draws key chips. It draws via op.Defer, above everything.
func Tooltip(gtx layout.Context, th *theme.Theme, st *TipState, hovered bool, sz image.Point, side Side, label, shortcut string) {
	if !hovered {
		st.hovered = false
		return
	}
	if !st.hovered {
		st.hovered = true
		st.since = gtx.Now
	}
	if wait := st.since.Add(TipDelay); gtx.Now.Before(wait) {
		gtx.Execute(op.InvalidateCmd{At: wait})
		return
	}
	call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
		return tipBox(gtx, th, label, shortcut)
	})
	gap := dp(gtx, 8)
	var p image.Point
	switch side {
	case Above:
		p = image.Pt((sz.X-d.Size.X)/2, -gap-d.Size.Y)
	case Right:
		p = image.Pt(sz.X+gap, (sz.Y-d.Size.Y)/2)
	case Left:
		p = image.Pt(-gap-d.Size.X, (sz.Y-d.Size.Y)/2)
	default:
		p = image.Pt((sz.X-d.Size.X)/2, sz.Y+gap)
	}
	cl := colorsOf(th)
	m := op.Record(gtx.Ops)
	// arrow
	ar := dp(gtx, 6)
	var ac image.Point
	switch side {
	case Above:
		ac = image.Pt(sz.X/2, -gap)
	case Right:
		ac = image.Pt(sz.X+gap, sz.Y/2)
	case Left:
		ac = image.Pt(-gap, sz.Y/2)
	default:
		ac = image.Pt(sz.X/2, sz.Y+gap)
	}
	var path clip.Path
	path.Begin(gtx.Ops)
	fx, fy, fr := float32(ac.X), float32(ac.Y), float32(ar)
	path.MoveTo(pt(fx, fy-fr))
	path.LineTo(pt(fx+fr, fy))
	path.LineTo(pt(fx, fy+fr))
	path.LineTo(pt(fx-fr, fy))
	path.Close()
	paint.FillShape(gtx.Ops, cl.Tooltip, clip.Outline{Path: path.End()}.Op())
	macroAt(gtx, call, p)
	op.Defer(gtx.Ops, m.Stop())
}

// tipBox draws the tooltip body at the origin.
func tipBox(gtx layout.Context, th *theme.Theme, label, shortcut string) layout.Dimensions {
	cl := colorsOf(th)
	lines := strings.Split(label, "\n")
	keys := splitKeys(shortcut)
	padX, padY := dp(gtx, 12), dp(gtx, 8)
	maxW := dp(gtx, 320)
	type piece struct {
		call op.CallOp
		d    layout.Dimensions
	}
	var ps []piece
	w := 0
	for i, l := range lines {
		wt := theme.Bold
		if i > 0 {
			wt = theme.Regular
		}
		c, d := record(loose(gtx, image.Pt(maxW, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 13, 18, wt, cl.TooltipText, l, 0)
		})
		ps = append(ps, piece{c, d})
		if d.Size.X > w {
			w = d.Size.X
		}
	}
	var kc op.CallOp
	kw := 0
	if len(keys) > 0 {
		kc, _ = record(gtx, func(gtx layout.Context) layout.Dimensions {
			x := 0
			for _, k := range keys {
				d := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions { return kbd(gtx, th, k, !th.Dark) })
				x += d.Size.X + dp(gtx, 4)
			}
			kw = x - dp(gtx, 4)
			return layout.Dimensions{Size: image.Pt(kw, dp(gtx, 20))}
		})
		if kw > w {
			w = kw
		}
	}
	h := 0
	for _, p := range ps {
		h += p.d.Size.Y
	}
	if len(keys) > 0 {
		h += dp(gtx, 24)
	}
	sz := image.Pt(w+2*padX, h+2*padY)
	rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 8), cl.Tooltip)
	y := padY
	for _, p := range ps {
		macroAt(gtx, p.call, image.Pt((sz.X-p.d.Size.X)/2, y))
		y += p.d.Size.Y
	}
	if len(keys) > 0 {
		macroAt(gtx, kc, image.Pt((sz.X-kw)/2, y+dp(gtx, 4)))
	}
	return layout.Dimensions{Size: sz}
}

// ---- Toast ----

// Toast is a transient message at the bottom center of the window
// (Slack's dark toast: #1d1c1d, radius 8, 15/700 white). Pill toasts use
// Slack's keyboard-hint pill instead (rgba(0,0,0,.7), fully rounded).
type Toast struct {
	base
	h     Host
	text  string
	pill  bool
	until time.Time
	dur   time.Duration
}

// NewToast returns a toast overlay that disappears after 3.5 s.
func NewToast(h Host, text string) *Toast {
	return &Toast{h: h, text: text, dur: 3500 * time.Millisecond}
}

// NewHintPill returns the translucent keyboard-hint pill ("Tip: …").
func NewHintPill(h Host, text string) *Toast {
	return &Toast{h: h, text: text, pill: true, dur: 4 * time.Second}
}

// Layout draws the toast.
func (t *Toast) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	if t.done {
		return layout.Dimensions{}
	}
	if t.until.IsZero() {
		t.until = gtx.Now.Add(t.dur)
	}
	if !gtx.Now.Before(t.until) {
		t.Dismiss()
		return layout.Dimensions{}
	}
	gtx.Execute(op.InvalidateCmd{At: t.until})
	th := t.h.Theme()
	cl := colorsOf(th)
	win := gtx.Constraints.Max
	fs := float32(15)
	tw := textW(gtx, th, fs, theme.Bold, t.text)
	if mx := win.X - dp(gtx, 80); tw > mx {
		tw = mx
	}
	padX, h := dp(gtx, 16), dp(gtx, 44)
	bg, rad := cl.Tooltip, dp(gtx, 8)
	if t.pill {
		h = dp(gtx, 34)
		bg = rgba(0x000000, .7)
		rad = h / 2
	}
	w := tw + 2*padX
	r := image.Rect((win.X-w)/2, win.Y-h-dp(gtx, 28), (win.X+w)/2, win.Y-dp(gtx, 28))
	if !t.pill {
		softShadow(gtx, r, rad, dp(gtx, 4), dp(gtx, 12), cl.Shadow)
	}
	rrect(gtx, r, rad, bg)
	at(gtx, image.Pt(r.Min.X+padX, r.Min.Y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(tw, h)}
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, fs, theme.Bold, rgb(0xffffff), t.text)
		})
	})
	return layout.Dimensions{}
}
