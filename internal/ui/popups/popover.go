package popups

import (
	"image"
	"image/color"
	"math"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	gtext "gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
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

	// Popover places the card as Slack's Popover does at that position
	// ("right-top", "bottom-left", …; slack_place.go) with OffsetX /
	// OffsetY (dp) and keepInBounds' AllowanceX / AllowanceY (dp; 0 is
	// Slack's 16 / 10), instead of Side and Gap.
	Popover                string
	OffsetX, OffsetY       float32
	AllowanceX, AllowanceY float32
	// Peek opens the card as a PeekTrigger's hover peek (boot-shared
	// 20Sq): .p-peek_trigger__overlay is only as big as the card until it's
	// --clickable, so there is no scrim (the window under it keeps its
	// pointer and clicks), and shouldFocusAfterRender is off for a hover
	// open, so it doesn't take the keyboard focus. Its owner closes it
	// (the safe area, Escape through the window's dismissTop).
	Peek bool
	// AnimateIn is .p-peek_card--animate-in: the card slides in from 8px
	// to the left (.16s cubic-bezier(.25,1,.5,1)) while it fades in (.16s
	// cubic-bezier(.33,1,.68,1)). NoMotion is prefers-reduced-motion
	// (animation: none).
	AnimateIn, NoMotion bool
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
	opened  time.Time // the first frame (AnimateIn)
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
	if !p.focus && !p.o.Peek {
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
	if !p.o.Peek && p.scrim.Layout(gtx) {
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
	var pos image.Point
	if p.o.Popover != "" {
		ax, ay := p.o.AllowanceX, p.o.AllowanceY
		if ax == 0 {
			ax = 16
		}
		if ay == 0 {
			ay = 10
		}
		pos = slackPlace(p.o.Anchor, d.Size, win, p.o.Popover, dp(gtx, p.o.OffsetX), dp(gtx, p.o.OffsetY), dp(gtx, ax), dp(gtx, ay), false)
	} else {
		pos = place(a, d.Size, win, m, p.o.Side)
	}
	p.rect = image.Rectangle{Min: pos, Max: pos.Add(d.Size)}
	if p.opened.IsZero() {
		p.opened = gtx.Now
	}
	if p.o.AnimateIn && !p.o.NoMotion {
		if el := gtx.Now.Sub(p.opened); el < PeekAnimateIn {
			x := float64(el) / float64(PeekAnimateIn)
			slide := cubicBezierY(.25, 1, .5, 1, x)
			fade := cubicBezierY(.33, 1, .68, 1, x)
			pos.X += int(math.Round(float64(dp(gtx, -8)) * (1 - slide)))
			defer paint.PushOpacity(gtx.Ops, float32(fade)).Pop()
			gtx.Execute(op.InvalidateCmd{}) // a frame per vsync until it's in
		}
	}
	rad := p.o.Radius
	if rad == 0 {
		rad = 8
	}
	keyTarget(gtx, p, p.rect)
	st := popoverClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if !st.Hidden() {
		styledCard(gtx, st, cl, pos, d.Size, dp(gtx, rad), false, func(gtx layout.Context) {
			local := image.Rectangle{Max: d.Size}
			p.block.Layout(gtx, local)
			cs := clipRRect(gtx, local, dp(gtx, rad))
			call.Add(gtx.Ops)
			cs.Pop()
		})
	}
	return layout.Dimensions{Size: win}
}

// PeekAnimateIn is .p-peek_card--animate-in's duration.
const PeekAnimateIn = 160 * time.Millisecond

// InsideCard reports whether window point pt is on the card as last laid
// out (edges included, as pointInsideBounds).
func (p *Popover) InsideCard(pt image.Point) bool {
	r := p.rect
	return !r.Empty() && pt.X >= r.Min.X && pt.X <= r.Max.X && pt.Y >= r.Min.Y && pt.Y <= r.Max.Y
}

// cubicBezierY is the CSS cubic-bezier(x1, y1, x2, y2) timing function at
// x (0..1).
func cubicBezierY(x1, y1, x2, y2, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	bez := func(a, b, t float64) float64 { return 3*a*t*(1-t)*(1-t) + 3*b*t*t*(1-t) + t*t*t }
	lo, hi, t := 0.0, 1.0, x
	for i := 0; i < 40; i++ {
		t = (lo + hi) / 2
		if bez(x1, x2, t) < x {
			lo = t
		} else {
			hi = t
		}
	}
	return bez(y1, y2, t)
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

	// Delay overrides TipDelay for this tooltip (Slack's BaseTooltip
	// defaults to delay 150; some callers pass longer ones).
	Delay time.Duration
	// NoDelay shows it in the frame the hover starts: a Popover opened
	// from a mouseover handler, not a BaseTooltip.
	NoDelay bool
	// Texty is a TextyTooltip Popover (Texty's mouseover opens it): no
	// delay, allowance 0 and no tryOppositePosition (theme.TextyTip).
	Texty bool
	// org is the target's window origin, so the tooltip keeps inside the
	// window (theme.PlaceTooltip); last is where it was drawn (tests).
	org  theme.TipOrigin
	last theme.TipPlace
	// MaxWidth, when set, wraps the label at that width with each line
	// centred (c-tooltip__tip: max-width 200px, text-align center).
	MaxWidth unit.Dp
}

// Placed is where the tooltip last drew, in its widget's coordinates.
func (st *TipState) Placed() theme.TipPlace { return st.last }

// Showing reports whether the tooltip is up at now: hovered for its delay.
func (st *TipState) Showing(now time.Time) bool {
	return st.hovered && !now.Before(st.since.Add(st.delay()))
}

// delay is the tooltip's hover delay.
func (st *TipState) delay() time.Duration {
	switch {
	case st.NoDelay, st.Texty:
		return 0
	case st.Delay > 0:
		return st.Delay
	}
	return TipDelay
}

// TipDelay is how long the pointer must rest before a tooltip shows.
const TipDelay = 450 * time.Millisecond

// Tooltip shows a Slack tooltip for a widget of size sz laid out at the
// current origin, while hovered. Call it right after laying the widget out.
// label may contain "\n" for a second (regular weight) line; shortcut, a
// combo like "Mod+Shift+K" (internal/ui/shortcut), draws key chips. It
// draws via op.Defer, above everything.
func Tooltip(gtx layout.Context, th *theme.Theme, st *TipState, hovered bool, sz image.Point, side Side, label, shortcut string) {
	tooltipWith(gtx, th, st, hovered, sz, side, func(gtx layout.Context) layout.Dimensions {
		return tipBox(gtx, th, label, shortcut, st.MaxWidth)
	})
}

// TipLine is one line of a TooltipLines tooltip: a plain line (13px/700,
// the tip's text colour) or a TooltipSubtitle (UZW8: fontWeight base,
// colour inverse.secondary, --dt_color-content-inv-sec).
type TipLine struct {
	Text     string
	Subtitle bool
}

// TooltipLines is Tooltip for content of several styled lines, the way
// Slack's tooltips stack plain divs and TooltipSubtitles.
func TooltipLines(gtx layout.Context, th *theme.Theme, st *TipState, hovered bool, sz image.Point, side Side, lines []TipLine) {
	tooltipWith(gtx, th, st, hovered, sz, side, func(gtx layout.Context) layout.Dimensions {
		return tipLinesBox(gtx, th, lines, st.MaxWidth)
	})
}

// TooltipBox is Tooltip for a body the caller draws: box lays out the
// c-tooltip__tip's content (its padding included) at the origin and
// returns the box's size; the tip's background, arrow and dark-mode
// outline go under it. box runs only once the hover delay has passed.
func TooltipBox(gtx layout.Context, th *theme.Theme, st *TipState, hovered bool, sz image.Point, side Side, box func(gtx layout.Context) layout.Dimensions) {
	tooltipWith(gtx, th, st, hovered, sz, side, box)
}

// tooltipWith shows the tooltip body box draws for a widget of size sz at
// the current origin, after the hover delay, placed as Slack's
// BaseTooltip places it (theme.PlaceTooltip: the 8px gap, keepInBounds'
// 3px inside the window, the flip to the opposite side, the arrow on the
// widget).
func tooltipWith(gtx layout.Context, th *theme.Theme, st *TipState, hovered bool, sz image.Point, side Side, box func(gtx layout.Context) layout.Dimensions) {
	st.org.Track(gtx, image.Rectangle{Max: sz})
	if !hovered {
		st.hovered = false
		return
	}
	if !st.hovered {
		st.hovered = true
		st.since = gtx.Now
	}
	if wait := st.since.Add(st.delay()); gtx.Now.Before(wait) {
		gtx.Execute(op.InvalidateCmd{At: wait})
		return
	}
	call, d := record(gtx, box)
	o := theme.BaseTooltip(gtx, side.tipPos())
	if st.Texty {
		o = theme.TextyTip(side.tipPos())
	}
	pl := theme.PlaceTooltip(gtx, image.Rectangle{Max: sz}, d.Size, st.org.Bounds(), o)
	st.last = pl
	cl := colorsOf(th)
	m := op.Record(gtx.Ops)
	// the box and its arrow, outlined together in dark mode
	// (theme/tooltip.go), then the text
	theme.TooltipShape(gtx, pl.Box, dp(gtx, 8), pl.Arrow, cl.Tooltip, tipGlow(th))
	macroAt(gtx, call, pl.Box.Min)
	op.Defer(gtx.Ops, m.Stop())
}

// tipPos is a Side's TooltipTip position.
func (s Side) tipPos() string {
	switch s {
	case Above:
		return theme.TipTop
	case Right:
		return theme.TipRight
	case Left:
		return theme.TipLeft
	case BelowRight:
		return theme.TipBottomRight
	}
	return theme.TipBottom
}

// tipGlow is the dark mode tooltip outline (theme's TooltipGlow; none in
// light mode).
func tipGlow(th *theme.Theme) color.NRGBA {
	if th == nil {
		return color.NRGBA{}
	}
	return th.C.TooltipGlow
}

// tipLinesBox draws a TooltipLines body's lines at the origin, stacked
// and centred, 13px with an 18px line, inside tipBox's padding, and
// returns the box's size.
func tipLinesBox(gtx layout.Context, th *theme.Theme, lines []TipLine, maxWidth unit.Dp) layout.Dimensions {
	cl := colorsOf(th)
	padX, padY := dp(gtx, 12), dp(gtx, 8)
	maxW := dp(gtx, 320)
	if maxWidth > 0 {
		maxW = gtx.Dp(maxWidth) - 2*padX
	}
	sub := th.C.TooltipSub // TooltipSubtitle: content-inv-sec (theme/tooltip.go)
	type piece struct {
		call op.CallOp
		d    layout.Dimensions
	}
	var ps []piece
	w, h := 0, 0
	for _, l := range lines {
		wt, col := theme.Bold, cl.TooltipText
		if l.Subtitle {
			wt, col = theme.Regular, sub
		}
		c, d := record(loose(gtx, image.Pt(maxW, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 13, 18, wt, col, l.Text, 0)
		})
		ps = append(ps, piece{c, d})
		w = max(w, d.Size.X)
		h += d.Size.Y
	}
	sz := image.Pt(w+2*padX, h+2*padY) // tooltipWith fills the box under it
	y := padY
	for _, p := range ps {
		macroAt(gtx, p.call, image.Pt((sz.X-p.d.Size.X)/2, y))
		y += p.d.Size.Y
	}
	return layout.Dimensions{Size: sz}
}

// tipBox draws the tooltip body's text and shortcut chips at the origin
// and returns the box's size (Tooltip fills the box under it).
func tipBox(gtx layout.Context, th *theme.Theme, label, shortcut string, maxWidth unit.Dp) layout.Dimensions {
	cl := colorsOf(th)
	lines := strings.Split(label, "\n")
	keys := splitKeys(shortcut)
	padX, padY := dp(gtx, 12), dp(gtx, 8)
	maxW := dp(gtx, 320)
	align := gtext.Start
	if maxWidth > 0 {
		maxW, align = gtx.Dp(maxWidth)-2*padX, gtext.Middle
	}
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
			if align != gtext.Start {
				gtx.Constraints.Min = image.Point{}
				return th.Layout(gtx, theme.TextStyle{Size: 13, Weight: wt, Color: cl.TooltipText, LineHeight: 18, Align: align}, l)
			}
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
				// KeyboardKey's tooltip variant: black chips in both modes
				d := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions { return kbd(gtx, th, k, true) })
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
	fg := cl.TooltipText // .c-toast: content-inv-pry on base-inv-pry
	if t.pill {
		fg = rgb(0xffffff)
	}
	at(gtx, image.Pt(r.Min.X+padX, r.Min.Y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(tw, h)}
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, fs, theme.Bold, fg, t.text)
		})
	})
	return layout.Dimensions{}
}
