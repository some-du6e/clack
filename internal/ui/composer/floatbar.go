package composer

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/icons"
)

// The floating formatting bar (boot-shared ZGif TextyFloatingFormattingBar
// over bpQI buildComposer; placement cj0/ calculateToolbarPosition): with
// the formatting toolbar hidden (Aa off, msg_input_sticky_composer) and
// text selected (shouldToolbarShow: not while composing, not with the
// autocomplete open, not for a selection of nothing but blanks), the same
// buttons float 5px above the selection: centred on it when it is wider
// than the bar (unless it spans the whole editor, then from its left);
// narrower, centred while it sits in the editor's middle half, else
// ending at its end when there is room, else from its start; never past
// 10px from the right edge. Escape hides it until the selection changes.
// Look (.p-texty_floating_formatting_bar, .p-composer__body--floating,
// .p-composer__button--floating): #1a1d21, radius 8, padding 2, a 1px
// rgba(232,232,232,.25) ring and 0 1px 3px rgba(0,0,0,.08); 28px buttons
// with 2px margins and radius 4, white icons, hover rgba(255,255,255,.07),
// pressed rgba(232,232,232,.13), disabled 30% white; separators 1×20
// rgba(255,255,255,.25) with 4px margins.

type floatBar struct {
	btns   [len(crFmtOrder)]widget.Clickable
	hidLo  int // the selection Escape hid the bar for (hidHi > hidLo)
	hidHi  int
	shown  bool
	acts   []chromeAction // clicks, for handleActions (chrome coordinates)
	origin image.Point    // the bar's top-left in composer coordinates, last layout
}

var (
	fbBg      = color.NRGBA{R: 0x1a, G: 0x1d, B: 0x21, A: 0xff}
	fbRing    = color.NRGBA{R: 0xe8, G: 0xe8, B: 0xe8, A: 0x40}
	fbShadow  = color.NRGBA{A: 0x14}
	fbHover   = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x13}
	fbPressed = color.NRGBA{R: 0xe8, G: 0xe8, B: 0xe8, A: 0x21}
	fbIcon    = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	fbOff     = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x4d}
	fbSep     = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x40}
)

// floatBarWanted is shouldToolbarShow with TextyFormattingBar's gate.
func (c *Composer) floatBarWanted(gtx layout.Context) (lo, hi int, ok bool) {
	if c.cs.ShowFormatting || c.ed.plainText || !c.ed.Focused(gtx) || c.ac.Open() {
		return 0, 0, false
	}
	lo, hi = c.ed.Selection()
	if lo > hi {
		lo, hi = hi, lo
	}
	if lo == hi {
		return 0, 0, false
	}
	d := c.ed.Doc()
	blank := true
	for i := lo; i < hi && i < d.Len(); i++ {
		if d.Attrs[i].Atom != nil || strings.TrimSpace(string(d.Runes[i])) != "" {
			blank = false
			break
		}
	}
	if blank {
		return 0, 0, false
	}
	if c.fbar.hidHi > c.fbar.hidLo && lo == c.fbar.hidLo && hi == c.fbar.hidHi {
		return 0, 0, false // Escape hid it for this selection
	}
	return lo, hi, true
}

// floatEscape is the bar's capture-phase Escape: it hides the bar.
func (c *Composer) floatEscape() bool {
	if !c.fbar.shown {
		return false
	}
	lo, hi := c.ed.Selection()
	if lo > hi {
		lo, hi = hi, lo
	}
	c.fbar.hidLo, c.fbar.hidHi, c.fbar.shown = lo, hi, false
	c.h.Invalidate()
	return true
}

// floatBarPos is calculateToolbarPosition (top placement) in composer
// coordinates: sel is the selection's box, edLeft/edW the editor's, maxX
// the right edge the bar keeps 10px from.
func floatBarPos(sel image.Rectangle, edLeft, edW, tw, th, maxX int) image.Point {
	m := sel.Min.X
	top := sel.Min.Y - th - 5
	x := m
	rel := m - edLeft
	switch sw := sel.Dx(); {
	case sw >= tw:
		if sw != edW {
			x = m + (sw-tw)/2
		}
	case rel > edW/4 && sel.Max.X-edLeft < edW*3/4:
		x = m - (tw-sw)/2
	case rel > tw:
		x = m - (tw - sw)
	}
	if over := maxX - 10 - x - tw; over < 0 {
		x += over
	}
	return image.Pt(x, top)
}

// layoutFloatBar draws the bar (deferred, over the message list) when it
// shows; org is the editor's origin in composer coordinates.
func (c *Composer) layoutFloatBar(gtx layout.Context, org image.Point) {
	for i := range c.fbar.btns {
		for c.fbar.btns[i].Clicked(gtx) {
			id := crFmtOrder[i]
			if !c.ui.disabled(&c.cs, id) {
				// handleActions adds the context bar's height to anchors
				c.fbar.acts = append(c.fbar.acts, c.ui.actionFor(id, c.fbar.btnRect(gtx, i).Sub(image.Pt(0, c.top))))
			}
		}
	}
	lo, hi, ok := c.floatBarWanted(gtx)
	c.fbar.shown = ok
	if !ok {
		return
	}
	r1, r2 := c.ed.PosRect(lo), c.ed.PosRect(hi)
	edW := c.ed.width
	if edW <= 0 {
		edW = gtx.Constraints.Max.X
	}
	sel := image.Rect(r1.Min.X, r1.Min.Y, r2.Max.X, r2.Max.Y)
	if r2.Min.Y > r1.Min.Y { // more than one line: the lines' full width
		sel = image.Rect(0, r1.Min.Y, edW, r2.Max.Y)
	}
	sel = sel.Add(org)
	tw, th := c.fbar.size(gtx)
	at := floatBarPos(sel, org.X, edW, tw, th, gtx.Constraints.Max.X)
	m := op.Record(gtx.Ops)
	off := op.Offset(at).Push(gtx.Ops)
	c.fbar.draw(gtx, &c.cs, c)
	off.Pop()
	call := m.Stop()
	op.Defer(gtx.Ops, call)
	c.fbar.origin = at
}

// size is the bar's width and height (2px padding, 2px button margins).
func (b *floatBar) size(gtx layout.Context) (w, h int) {
	pad, mg, btn := gtx.Dp(2), gtx.Dp(2), gtx.Dp(28)
	w = 2 * pad
	for i, id := range crFmtOrder {
		if crFmtSep[i] {
			w += gtx.Dp(4)*2 + max(gtx.Dp(1), 1)
		}
		bw := btn
		if id == crPara {
			bw = gtx.Dp(44)
		}
		w += 2*mg + bw
	}
	return w, 2*pad + 2*mg + btn
}

// btnRect is button i's box in composer coordinates (the ¶ menu's anchor).
func (b *floatBar) btnRect(gtx layout.Context, i int) image.Rectangle {
	pad, mg, btn := gtx.Dp(2), gtx.Dp(2), gtx.Dp(28)
	x := pad
	for j, id := range crFmtOrder {
		if crFmtSep[j] {
			x += gtx.Dp(4)*2 + max(gtx.Dp(1), 1)
		}
		bw := btn
		if id == crPara {
			bw = gtx.Dp(44)
		}
		x += mg
		if j == i {
			return image.Rect(x, pad+mg, x+bw, pad+mg+btn).Add(b.origin)
		}
		x += bw + mg
	}
	return image.Rectangle{}
}

func (b *floatBar) draw(gtx layout.Context, st *chromeState, c *Composer) {
	w, h := b.size(gtx)
	r := image.Rect(0, 0, w, h)
	rad := gtx.Dp(8)
	// box-shadow: 0 1px 3px rgba(0,0,0,.08), then the 1px ring
	paint.FillShape(gtx.Ops, fbShadow, clip.UniformRRect(r.Add(image.Pt(0, gtx.Dp(1))).Inset(-gtx.Dp(1)), rad+gtx.Dp(1)).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, fbRing, clip.UniformRRect(r.Inset(-max(gtx.Dp(1), 1)), rad+1).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, fbBg, clip.UniformRRect(r, rad).Op(gtx.Ops))
	pad, mg, btn := gtx.Dp(2), gtx.Dp(2), gtx.Dp(28)
	x := pad
	for i, id := range crFmtOrder {
		if crFmtSep[i] {
			sx := x + gtx.Dp(4)
			sy := (h - gtx.Dp(20)) / 2
			paint.FillShape(gtx.Ops, fbSep, clip.Rect{Min: image.Pt(sx, sy), Max: image.Pt(sx+max(gtx.Dp(1), 1), sy+gtx.Dp(20))}.Op())
			x += gtx.Dp(4)*2 + max(gtx.Dp(1), 1)
		}
		bw := btn
		if id == crPara {
			bw = gtx.Dp(44)
		}
		x += mg
		off := op.Offset(image.Pt(x, pad+mg)).Push(gtx.Ops)
		disabled := c.ui.disabled(st, id)
		pressed := crPressed(st, id)
		cl := &b.btns[i]
		cl.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(bw, btn)
			br := image.Rectangle{Max: sz}
			switch {
			case disabled:
			case pressed && cl.Hovered():
				fillRRect(gtx, br, gtx.Dp(4), color.NRGBA{R: 0xe8, G: 0xe8, B: 0xe8, A: 0x33})
			case pressed:
				fillRRect(gtx, br, gtx.Dp(4), fbPressed)
			case cl.Hovered():
				fillRRect(gtx, br, gtx.Dp(4), fbHover)
			}
			col := fbIcon
			if disabled {
				col = fbOff
			} else {
				pointer.CursorPointer.Add(gtx.Ops)
			}
			icon := crInfo[id].icon
			if id == crPara {
				icon = crParaIcon(st.Block)
			}
			is := gtx.Dp(18)
			ix := (btn - is) / 2
			if id == crPara {
				ix = gtx.Dp(8) // .p-composer__menu-trigger padding-left 8px
			}
			io := op.Offset(image.Pt(ix, (btn-is)/2)).Push(gtx.Ops)
			if icon != nil {
				icon(gtx, unit.Dp(18), col)
			}
			io.Pop()
			if id == crPara {
				co := op.Offset(image.Pt(ix+is+gtx.Dp(2), (btn-gtx.Dp(12))/2)).Push(gtx.Ops)
				icons.Get("chevron-down")(gtx, unit.Dp(12), col)
				co.Pop()
			}
			return layout.Dimensions{Size: sz}
		})
		off.Pop()
		x += bw + mg
	}
}

// floatBarKey hooks the bar's Escape into the editor's key intercept.
func (c *Composer) floatBarKey(next func(key.Name, key.Modifiers) bool) func(key.Name, key.Modifiers) bool {
	return func(name key.Name, mods key.Modifiers) bool {
		if name == key.NameEscape && mods == 0 && c.floatEscape() {
			return true
		}
		return next != nil && next(name, mods)
	}
}
