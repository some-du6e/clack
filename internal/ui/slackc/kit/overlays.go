package kit

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// hoverTracker reports whether the pointer is over an area without taking
// events from the widgets under it.
type hoverTracker struct {
	tag int
	in  bool
}

func (t *hoverTracker) layout(gtx layout.Context, sz image.Point) bool {
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &t.tag, Kinds: pointer.Enter | pointer.Leave | pointer.Move | pointer.Cancel})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok {
			switch pe.Kind {
			case pointer.Enter, pointer.Move:
				t.in = true
			case pointer.Leave, pointer.Cancel:
				t.in = false
			}
		}
	}
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &t.tag)
	return t.in
}

// ---- Tooltip ----

type tooltipState struct {
	hover hoverTracker
	org   origin
	since time.Time
	on    bool
}

// tooltip: tip (string or element), position (top default; Slack's
// TooltipTip positions: top, top-left, top-right, bottom, bottom-left,
// bottom-right, left, right), offsetX / offsetY, delay (ms), zIndex
// (always on top here), children. Drawn like popups.Tooltip: #1d1c1d
// card, radius 8, padding 8 12, 13px/700, a diamond arrow, and placed as
// BaseTooltip places it (theme.PlaceTooltip: 8px from the trigger,
// keepInBounds 3px inside the window, the opposite side when there's no
// room; offsetY adds to the top as getInitialPosition's does).
func tooltip(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := stateOf[tooltipState](h)
	d := h.Children(gtx)
	hovered := st.hover.layout(gtx, d.Size)
	o := tipOpts{prop: "tip", pos: h.Props.StrOr("position", "top"), delay: popups.TipDelay}
	if ms, ok := h.Props.Num("delay"); ok && ms >= 0 {
		o.delay = time.Duration(ms) * time.Millisecond
	}
	o.offX, o.hasX = h.Props.Num("offsetX")
	o.offY, o.hasY = h.Props.Num("offsetY")
	st.show(gtx, h, d.Size, hovered, o)
	return d
}

// tipOpts are a tooltip's tip (the host prop holding it, a string or an
// element) and Tooltip's position, offsetX, offsetY (CSS px) and delay.
type tipOpts struct {
	prop, pos  string
	offX, offY float64
	hasX, hasY bool
	delay      time.Duration
}

// show draws the tip of a trigger of size sz at the gtx origin while it
// is hovered, after the delay: Slack's BaseTooltip, as Tooltip draws it.
func (st *tooltipState) show(gtx layout.Context, h *slackc.Host, sz image.Point, hovered bool, opt tipOpts) {
	st.org.track(gtx, image.Rectangle{Max: sz})
	if !hovered || !h.HasElement(opt.prop) {
		st.on = false
		return
	}
	if !st.on {
		st.on = true
		st.since = gtx.Now
	}
	if wait := st.since.Add(opt.delay); gtx.Now.Before(wait) {
		gtx.Execute(op.InvalidateCmd{At: wait})
		return
	}
	th := thm()
	cl := colorsOf(th)
	o := theme.BaseTooltip(gtx, tipPosition(opt.pos))
	if opt.hasX {
		o.OffsetX = dp(gtx, float32(opt.offX))
	}
	if opt.hasY {
		o.OffsetY = dp(gtx, float32(opt.offY))
	}
	padX, padY := dp(gtx, 12), dp(gtx, 8)
	maxW := dp(gtx, 320)
	content, cs := record(loose(gtx, image.Pt(maxW, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return textOrElement(gtx, h, opt.prop, th, 13, 18, theme.Bold, cl.TooltipText, 0)
	})
	box := image.Pt(cs.Size.X+2*padX, cs.Size.Y+2*padY)
	var bounds image.Rectangle
	if st.org.ok {
		bounds = theme.WindowBounds(st.org.get())
	}
	pl := theme.PlaceTooltip(gtx, image.Rectangle{Max: sz}, box, bounds, o)
	m := op.Record(gtx.Ops)
	// box and arrow, outlined together in dark mode (theme/tooltip.go)
	var glow color.NRGBA
	if th != nil {
		glow = th.C.TooltipGlow
	}
	theme.TooltipShape(gtx, pl.Box, dp(gtx, 8), pl.Arrow, cl.Tooltip, glow)
	at(gtx, pl.Box.Min, func(gtx layout.Context) layout.Dimensions {
		callAt(gtx, content, image.Pt(padX, padY))
		return layout.Dimensions{Size: box}
	})
	op.Defer(gtx.Ops, m.Stop())
}

// tipPosition is a Tooltip position prop as TooltipTip's position
// ("above" / "below" too); anything else is "top".
func tipPosition(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case theme.TipTop, theme.TipTopLeft, theme.TipTopRight, theme.TipBottom, theme.TipBottomLeft, theme.TipBottomRight, theme.TipLeft, theme.TipRight:
		return s
	case "above":
		return theme.TipTop
	case "below":
		return theme.TipBottom
	}
	return theme.TipTop
}

// ---- Popover ----

type popoverState struct {
	org   origin
	layer layer
	h     *slackc.Host // this frame's (LayerEscape)
}

// LayerEscape (slackc.Layer) is an Escape that bubbled up from the
// popover's content, or reached it as the window's topmost layer: Slack's
// Popover is react-modal with shouldCloseOnEsc.
func (st *popoverState) LayerEscape() {
	if st.h != nil {
		st.h.Call("onClose")
	}
}

// domRect reads a DOMRect-like prop ({x,y,width,height,top,left,right,
// bottom}, CSS px) into window px.
func domRect(gtx layout.Context, p slackc.Props) (image.Rectangle, bool) {
	if p == nil {
		return image.Rectangle{}, false
	}
	num := func(keys ...string) (float64, bool) {
		for _, k := range keys {
			if v, ok := p.Num(k); ok {
				return v, true
			}
		}
		return 0, false
	}
	x, okx := num("left", "x")
	y, oky := num("top", "y")
	if !okx || !oky {
		return image.Rectangle{}, false
	}
	w, ok := num("width")
	if !ok {
		if r, ok := num("right"); ok {
			w = r - x
		}
	}
	hh, ok := num("height")
	if !ok {
		if b, ok := num("bottom"); ok {
			hh = b - y
		}
	}
	s := float64(gtx.Metric.PxPerDp)
	if s == 0 {
		s = 1
	}
	px := func(v float64) int { return int(v*s + .5) }
	return image.Rect(px(x), px(y), px(x+w), px(y+hh)), true
}

// popover: isOpen, targetBounds (window CSS px), windowRef (ignored),
// position (bottom default; corners like bottom-left), offsetX, offsetY,
// onClose (outside click or Escape), ariaRole, ariaLabel, children.
// Slack's c-popover is a bare layer: the children draw their own card.
func popover(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := stateOf[popoverState](h)
	st.org.track(gtx, image.Rectangle{Max: gtx.Constraints.Min})
	if !h.Props.Bool("isOpen") {
		st.layer.opened = false // the next open takes the focus again
		return layout.Dimensions{}
	}
	st.h = h
	anchor, ok := domRect(gtx, h.Props.Map("targetBounds"))
	if !ok {
		// no target: open at the popover's own place in the flow.
		anchor = image.Rectangle{Min: st.org.get(), Max: st.org.get()}
	}
	side, align := position(h.Props.StrOr("position", "bottom"), popups.Below)
	if v, ok := h.Props.Num("offsetX"); ok {
		anchor = anchor.Add(image.Pt(dp(gtx, float32(v)), 0))
	}
	if v, ok := h.Props.Num("offsetY"); ok {
		o := dp(gtx, float32(v))
		switch side {
		case popups.Above:
			anchor.Min.Y -= o
		case popups.Below:
			anchor.Max.Y += o
		default:
			anchor = anchor.Add(image.Pt(0, o))
		}
	}
	maxSz := image.Pt(dp(gtx, 1600), dp(gtx, 1200))
	st.layer.open(gtx, st) // before the children (see open)
	call, d := record(loose(gtx, maxSz), h.Children)
	p := place(anchor, d.Size, image.Point{}, dp(gtx, 8), side, align)
	outside, esc := st.layer.float(gtx, &st.org, p, call, d.Size, true)
	if outside || esc {
		h.Call("onClose")
	}
	return layout.Dimensions{}
}

// ---- ConfirmationModal ----

type confirmState struct {
	m      *popups.Modal
	shape  string
	h      *slackc.Host
	cancel *popups.ModalButton
	submit *popups.ModalButton
	area   int // its layer's (DrawLayer)
}

// LayerEscape (slackc.Layer): the dialog's Escape, as its X or backdrop
// (onClose), even laid out inside a Popover's content: the confirm on top
// closes first, the Popover under it stays.
func (st *confirmState) LayerEscape() { st.m.Dismiss() }

// confirmationModal: title, children (body), onSubmit, onCancel, onClose,
// submitButtonText, cancelButtonText, submitButtonType primary|danger,
// showCancelButton, showSubmitButton, disableSubmitButton. Drawn with
// popups.Modal (Slack's 520px dialog over the dimmed window). The
// buttons only report: JS closes the modal by unmounting it.
func confirmationModal(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := stateOf[confirmState](h)
	st.h = h
	p := h.Props
	showCancel := !p.Has("showCancelButton") || p.Bool("showCancelButton")
	showSubmit := !p.Has("showSubmitButton") || p.Bool("showSubmitButton")
	title := valueText(p["title"])
	shape := title + "\x00" + strconv.FormatBool(showCancel) + strconv.FormatBool(showSubmit)
	if st.m == nil || st.shape != shape {
		st.shape = shape
		st.cancel = &popups.ModalButton{Kind: popups.BtnOutline, Do: func() bool {
			st.h.Call("onCancel")
			return false
		}}
		st.submit = &popups.ModalButton{Kind: popups.BtnPrimary, Do: func() bool {
			st.h.Call("onSubmit")
			return false
		}}
		st.submit.Enabled = func() bool { return !st.h.Props.Bool("disableSubmitButton") }
		var btns []*popups.ModalButton
		if showCancel {
			btns = append(btns, st.cancel)
		}
		if showSubmit {
			btns = append(btns, st.submit)
		}
		// Slack's ConfirmationModal: autoFocus on submit, or on Cancel
		// with shouldAutoFocusCancel
		focus := st.submit
		if p.Bool("shouldAutoFocusCancel") || !showSubmit {
			focus = st.cancel
		}
		if !showCancel && focus == st.cancel {
			focus = nil
		}
		st.m = popups.NewModal(ph, popups.ModalOpts{
			Title: title,
			// Slack's ConfirmationModal titles are ModalTitleBar's h1 in
			// .c-sk-modal_title_bar__text, which wraps (client-boot Fj8r, H4iZ)
			TitleWrap: true,
			Buttons:   btns,
			AutoFocus: focus,
			OnClose:   func() { st.h.Call("onClose") },
			Body: func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min = image.Point{}
				th := thm()
				return children(gtx, st.h, colorsOf(th).Fg, 15, 400)
			},
		})
	}
	st.cancel.Label = p.StrOr("cancelButtonText", "Cancel")
	st.submit.Label = p.StrOr("submitButtonText", "Submit")
	if p.StrOr("submitButtonType", "primary") == "danger" {
		st.submit.Kind = popups.BtnDanger
	} else {
		st.submit.Kind = popups.BtnPrimary
	}
	if st.m.Dismissed() {
		return layout.Dimensions{}
	}
	var d layout.Dimensions
	slackc.DrawLayer(gtx, st, slackc.LayerSpec{Area: &st.area, Home: st.m.FocusHome(), Plugin: true}, func(gtx layout.Context) { d = st.m.Layout(gtx) })
	return d
}

func init() {
	add(tooltip, "Tooltip")
	add(popover, "Popover")
	add(confirmationModal, "ConfirmationModal")
}
