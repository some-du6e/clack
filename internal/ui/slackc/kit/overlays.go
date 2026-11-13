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
	st.org.track(gtx, image.Rectangle{Max: d.Size})
	if !hovered || !h.HasElement("tip") {
		st.on = false
		return d
	}
	if !st.on {
		st.on = true
		st.since = gtx.Now
	}
	delay := popups.TipDelay
	if ms, ok := h.Props.Num("delay"); ok && ms >= 0 {
		delay = time.Duration(ms) * time.Millisecond
	}
	if wait := st.since.Add(delay); gtx.Now.Before(wait) {
		gtx.Execute(op.InvalidateCmd{At: wait})
		return d
	}
	th := thm()
	cl := colorsOf(th)
	o := theme.BaseTooltip(gtx, tipPosition(h.Props.StrOr("position", "top")))
	if v, ok := h.Props.Num("offsetX"); ok {
		o.OffsetX = dp(gtx, float32(v))
	}
	if v, ok := h.Props.Num("offsetY"); ok {
		o.OffsetY = dp(gtx, float32(v))
	}
	padX, padY := dp(gtx, 12), dp(gtx, 8)
	maxW := dp(gtx, 320)
	content, cs := record(loose(gtx, image.Pt(maxW, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return textOrElement(gtx, h, "tip", th, 13, 18, theme.Bold, cl.TooltipText, 0)
	})
	box := image.Pt(cs.Size.X+2*padX, cs.Size.Y+2*padY)
	var bounds image.Rectangle
	if st.org.ok {
		bounds = theme.WindowBounds(st.org.get())
	}
	pl := theme.PlaceTooltip(gtx, image.Rectangle{Max: d.Size}, box, bounds, o)
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
	return d
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
		st.layer.reset()
		return layout.Dimensions{}
	}
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
}

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
			Title:     title,
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
	return st.m.Layout(gtx)
}

func init() {
	add(tooltip, "Tooltip")
	add(popover, "Popover")
	add(confirmationModal, "ConfirmationModal")
}
