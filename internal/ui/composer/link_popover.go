package composer

import (
	"image"
	"image/color"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ComposerEditLinkPopover (boot-shared l9Hb, build 83aff18a1847): a click
// in a link in the composer (maybeHandleLinkClick), or Ctrl+Shift+U with
// the caret in one (openTextyLinkFormBasedOnSelection), selects the link
// and opens EditLinkCoachmark over it, a DeprecatedBaseCoachmark (hVjr)
// anchored on the <a>: orientation "top" (or "bottom" when the link is
// within 200px of the window's top), centerOnArrow, offsetY -5, the arrow
// at 10%, 50% or 90% of the card by where the link sits. In it the link's
// text in bold (overflow_ellipsis), its URL as a link under it
// (.p-composer_edit_link_popover__url: margin-top 4px, break-word, at
// most 9rem), and the footer: "Show full link" for a truncated link or
// "Shorten link" for a long bare one (truncateUrl), then Edit (outline)
// and Remove (danger). The close button floats at the top right.
//
// Remove takes the link off (formatText unlink) and leaves the caret at
// the end of the selection the click left; Edit opens the link dialog
// over the link; the third button swaps the text for the URL or its
// shortened form and the popover stays. Esc, the close button and a
// press outside the card close it, the selection the click left put back
// (closeCoachmark). Its overlay is transparent (overlayStyle zIndex 1012
// over the inline backgroundColor transparent).
//
// Card (.c-coachmark): 400px, sk_primary_background, radius 8, padding
// 20px 24px 24px, the 1px rgba(sk_foreground_low,.13) ring and 0 18px 48px
// rgba(0,0,0,.08); its arrow is cmdK's (16px :after, margin-left -12px).
// The close button is a 36px icon button (c-icon_button--size_medium)
// with the 20px close icon, floated right at top -8px, right -12px: the
// text and the URL are BFCs beside it, 36px narrower. The footer is
// margin-top 20px, its buttons medium (36px, min-width 80px), Edit
// margin-right 12px.

type linkPopover struct {
	c *Composer

	alo, ahi       int // the anchor element's runes
	text, url      string
	truncated      bool // isTruncated
	canTruncate    bool // canBeTruncated
	initLo, initHi int  // initialRange
	lo, hi         int  // adjustedRange
	editable       bool

	done                   bool
	closeBtn, edit, remove widget.Clickable
	tert, urlLink          widget.Clickable
	scrim, card            int
	rect                   image.Rectangle // the card, window px, last layout
}

// linkPopoverTap sees each popover as it opens (tests).
var linkPopoverTap func(*linkPopover)

// openEditLinkPopover is openTextyEditLinkModal.
func (c *Composer) openEditLinkPopover(alo, ahi int, text, url string, initLo, initHi, lo, hi int, editable bool) {
	p := &linkPopover{c: c, alo: alo, ahi: ahi, text: text, url: url, initLo: initLo, initHi: initHi, lo: lo, hi: hi, editable: editable}
	p.truncated = text != "" && url != "" && truncatedLinkText(text, url)
	p.canTruncate = !p.truncated && url == text && truncateURL(url) != url
	if linkPopoverTap != nil {
		linkPopoverTap(p)
	}
	c.h.ShowOverlay(p)
}

// linkClicked is TextyInput.onLinkClick: the link's range selected (no
// floating bar), the popover over the <a>.
func (c *Composer) linkClicked(_ layout.Context, lc linkClick) {
	d := c.ed.Doc()
	s := d.String(lc.lo, lc.hi)
	c.selectForLink(lc.lo, lc.hi)
	url := ""
	if lc.lo < lc.hi {
		url = d.Attrs[lc.lo].Link
	}
	c.openEditLinkPopover(lc.el.lo, lc.el.hi, s, url, lc.initLo, lc.initHi, lc.lo, lc.hi, !containsObj(s))
}

func containsObj(s string) bool {
	for _, r := range s {
		if r == ObjRune {
			return true
		}
	}
	return false
}

func (p *linkPopover) Dismissed() bool { return p.done }

// Dismiss is closeCoachmark: closeModal, the editor focused, the
// selection the click left put back.
func (p *linkPopover) Dismiss() {
	if p.done {
		return
	}
	p.done = true
	c := p.c
	c.ed.SetSelection(p.initLo, p.initHi)
	c.Focus()
}

// close ends the popover without touching the selection (Edit, Remove).
func (p *linkPopover) close() {
	p.done = true
	p.c.h.Invalidate()
}

// onRemove is ComposerEditLinkPopover's onRemove.
func (p *linkPopover) onRemove() {
	p.close()
	p.c.ed.unlinkRangeCaret(p.lo, p.hi, max(p.initLo, p.initHi))
	p.c.Focus()
}

// onEdit is ComposerEditLinkPopover's onEdit: the link dialog over the
// link (openTextyLinkFormModal).
func (p *linkPopover) onEdit() {
	p.close()
	p.c.openLinkForm(linkForm{lo: p.lo, hi: p.hi, text: p.text, url: p.url, editable: p.editable})
}

// onToggleTruncation is the third button: the text becomes the URL (a
// truncated link) or its truncateUrl form, linked, the caret after it.
func (p *linkPopover) onToggleTruncation() {
	t := p.url
	if !p.truncated {
		t = truncateURL(p.url)
	}
	p.c.ed.replaceRange(p.lo, p.hi, t, Attr{Link: p.url}, p.lo+len([]rune(t)))
	n := len([]rune(t))
	p.hi, p.ahi = p.lo+n, p.alo+n
}

// tertiary is the third button's label, or "" when there's none.
func (p *linkPopover) tertiary() string {
	switch {
	case p.canTruncate:
		return messageInputNS.T("Shorten link")
	case p.truncated:
		return messageInputNS.T("Show full link")
	}
	return ""
}

// Rect is the card as last laid out (window px).
func (p *linkPopover) Rect() image.Rectangle { return p.rect }

// linkPop geometry, dp.
const (
	lpW      = 400
	lpPadT   = 20
	lpPadX   = 24
	lpPadB   = 24
	lpLineH  = 22
	lpClose  = 36
	lpBtnH   = 36
	lpOffY   = -5
	lpFootMT = 20
)

// anchor is the <a>'s getBoundingClientRect in the window.
func (p *linkPopover) anchor() (image.Rectangle, bool) {
	c := p.c
	d := c.ed.Doc()
	if p.alo < 0 || p.ahi > d.Len() || p.ahi <= p.alo {
		return image.Rectangle{}, false
	}
	r := c.ed.rangeBox(p.alo, p.ahi)
	if r.Empty() {
		return image.Rectangle{}, false
	}
	return r.Add(c.Origin.Add(c.ui.InputOffset()).Add(image.Pt(0, c.top))), true
}

// arrowPos is EditLinkCoachmark's arrowPosition (percent) for the
// anchor's centre in a window w px wide (clientWidth − 10).
func arrowPos(gtx layout.Context, a image.Rectangle, win int) int {
	w := float64(win - gtx.Dp(10))
	cx := float64(a.Min.X+a.Max.X) / 2
	px := func(v float64) float64 { return v * float64(gtx.Metric.PxPerDp) }
	if w < px(560) {
		k := 50 - 10*jsRound((w/2-cx)/px(400)*100/10)
		return int(min(max(k, 10), 90))
	}
	switch {
	case cx+px(400)-px(40) < w:
		return 10
	case cx+px(200) < w:
		return 50
	}
	return 90
}

// jsRound is Math.round (halves up).
func jsRound(v float64) float64 {
	f := float64(int64(v))
	if v < 0 && f != v {
		f--
	}
	if v-f >= .5 {
		return f + 1
	}
	return f
}

func (p *linkPopover) Layout(gtx layout.Context) layout.Dimensions {
	if p.done {
		return layout.Dimensions{}
	}
	c := p.c
	if c == nil || c.done || !c.drawnAt.Equal(gtx.Now) {
		// gone with its MessageInput: the composer wasn't drawn
		p.done = true
		return layout.Dimensions{}
	}
	a, ok := p.anchor()
	if !ok {
		return layout.Dimensions{}
	}
	th := c.h.Theme()
	pal := cmdKColors(th)
	win := gtx.Constraints.Max

	// the buttons first: a click this frame acts before anything is drawn
	switch {
	case p.closeBtn.Clicked(gtx):
		p.Dismiss()
	case p.remove.Clicked(gtx):
		p.onRemove()
	case p.edit.Clicked(gtx):
		p.onEdit()
	case p.tert.Clicked(gtx):
		p.onToggleTruncation()
	case p.urlLink.Clicked(gtx):
		linkOpenURL(p.url)
	}
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &p.scrim, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Press {
			p.Dismiss() // shouldCloseOnOverlayClick
		}
	}
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &p.card, Kinds: pointer.Press | pointer.Release}); !ok {
			break
		}
	}
	if p.done {
		return layout.Dimensions{}
	}

	// the overlay: fixed, inset 0, transparent
	func() {
		defer clip.Rect{Max: win}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &p.scrim)
	}()

	// the card's size: the text line, the URL wrapped beside the close
	// button, the footer
	innerW := gtx.Dp(lpW - 2*lpPadX)
	besideW := innerW - gtx.Dp(lpClose)
	urlStyle := textStyle{Size: 15, LineHeight: lpLineH, Color: th.C.Link}
	urlRec, urlDims := crRecWrap(gtx, th, besideW, urlStyle, p.url)
	urlH := min(urlDims.Size.Y, gtx.Dp(144)) // max-height 9rem, overflow auto
	h := gtx.Dp(lpPadT) + gtx.Dp(lpLineH) + gtx.Dp(4) + urlH + gtx.Dp(lpFootMT) + gtx.Dp(lpBtnH) + gtx.Dp(lpPadB)
	sz := image.Pt(gtx.Dp(lpW), h)

	// placement: "top" (getComputedStyles: left + width/2, top − height
	// + offsetY, then translate(−arrow%, −100%)) or "bottom" (top +
	// height + 20 + offsetY, translate(−arrow%, 0))
	k := arrowPos(gtx, a, win.X)
	left := (a.Min.X+a.Max.X)/2 - int(float32(sz.X)*float32(k)/100+.5)
	var card image.Rectangle
	top := a.Min.Y >= gtx.Dp(200)
	if top {
		y := a.Min.Y - a.Dy() + gtx.Dp(lpOffY)
		card = image.Rect(left, y-sz.Y, left+sz.X, y)
	} else {
		y := a.Max.Y + gtx.Dp(20) + gtx.Dp(lpOffY)
		card = image.Rect(left, y, left+sz.X, y+sz.Y)
	}
	p.rect = card

	rad := gtx.Dp(8)
	theme.Shadow(gtx, card.Add(image.Pt(0, gtx.Dp(18))), rad, gtx.Dp(24), pal.shadow)
	ring := max(1, gtx.Dp(1))
	theme.RRect(gtx, card.Inset(-ring), rad+ring, pal.ring)
	theme.RRect(gtx, card, rad, pal.bg)
	linkPopArrow(gtx, card, k, top, pal)
	func() {
		defer clip.Rect(card).Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &p.card)
	}()

	x0, y := card.Min.X+gtx.Dp(lpPadX), card.Min.Y+gtx.Dp(lpPadT)
	// the close button: float right, relative top −8px right −12px
	cb := image.Rect(x0+innerW-gtx.Dp(lpClose)+gtx.Dp(12), y-gtx.Dp(8), x0+innerW+gtx.Dp(12), y-gtx.Dp(8)+gtx.Dp(lpClose))
	p.iconButton(gtx, th, cb, pal)
	// the text, bold, one line
	crText(gtx, th.Shaper, x0, y, besideW, textStyle{Size: 15, LineHeight: lpLineH, Weight: theme.Bold, Color: pal.fg, MaxLines: 1}, p.text)
	y += gtx.Dp(lpLineH) + gtx.Dp(4)
	// the URL, a link
	func() {
		r := image.Rect(x0, y, x0+besideW, y+urlH)
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		defer op.Offset(r.Min).Push(gtx.Ops).Pop()
		urlRec.Add(gtx.Ops)
		cl := clip.Rect{Max: image.Pt(min(urlDims.Size.X, besideW), urlH)}.Push(gtx.Ops)
		p.urlLink.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: urlDims.Size} })
		pointer.CursorPointer.Add(gtx.Ops)
		cl.Pop()
	}()
	y += urlH + gtx.Dp(lpFootMT)
	// the footer: [tertiary] … [Edit] 12px [Remove]
	right := x0 + innerW
	rw := p.button(gtx, th, &p.remove, popups.BtnDanger, messageInputNS.T("Remove"), right, y)
	right -= rw + gtx.Dp(12)
	p.button(gtx, th, &p.edit, popups.BtnOutline, messageInputNS.T("Edit"), right, y)
	if t := p.tertiary(); t != "" {
		st := textStyle{Size: 15, LineHeight: lpLineH, Color: th.C.Link}
		ty := y + (gtx.Dp(lpBtnH)-gtx.Dp(lpLineH))/2
		d := crText(gtx, th.Shaper, x0, ty, innerW/2, st, t)
		func() {
			defer clip.Rect(image.Rect(x0, ty, x0+d.Size.X, ty+d.Size.Y)).Push(gtx.Ops).Pop()
			defer op.Offset(image.Pt(x0, ty)).Push(gtx.Ops).Pop()
			p.tert.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return d })
			pointer.CursorPointer.Add(gtx.Ops)
		}()
	}
	return layout.Dimensions{Size: win}
}

// button draws a medium Slack button with its right edge at right, top y,
// and returns its width.
func (p *linkPopover) button(gtx layout.Context, th *theme.Theme, clk *widget.Clickable, kind popups.BtnKind, label string, right, y int) int {
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(gtx.Dp(lpW), gtx.Dp(lpBtnH))}
	d := popups.Button(g, th, clk, kind, popups.BtnMedium, label, "", 80)
	call := m.Stop()
	defer op.Offset(image.Pt(right-d.Size.X, y)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
	return d.Size.X
}

// iconButton draws the close button in r: a hover wash, the 20px close
// icon in the middle.
func (p *linkPopover) iconButton(gtx layout.Context, th *theme.Theme, r image.Rectangle, pal cmdKPal) {
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	sz := r.Size()
	p.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if p.closeBtn.Hovered() {
			theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(8), th.C.BgHover)
		}
		is := gtx.Dp(20)
		func() {
			defer op.Offset(image.Pt((sz.X-is)/2, (sz.Y-is)/2)).Push(gtx.Ops).Pop()
			icons.Get("close")(gtx, unit.Dp(20), th.C.FgMuted)
		}()
		pointer.CursorPointer.Add(gtx.Ops)
		return layout.Dimensions{Size: sz}
	})
}

// linkPopArrow draws the coachmark's :before and :after at arrow% of the
// card, under it ("top") or over it ("bottom").
func linkPopArrow(gtx layout.Context, card image.Rectangle, k int, top bool, pal cmdKPal) {
	cx := card.Min.X + int(float32(card.Dx())*float32(k)/100+.5)
	tri := func(half int, col color.NRGBA) {
		x0 := cx - gtx.Dp(cmdKArrowX) - (half - gtx.Dp(cmdKArrow))
		var path clip.Path
		path.Begin(gtx.Ops)
		if top {
			path.MoveTo(layout.FPt(image.Pt(x0, card.Max.Y)))
			path.LineTo(layout.FPt(image.Pt(x0+2*half, card.Max.Y)))
			path.LineTo(layout.FPt(image.Pt(x0+half, card.Max.Y+half)))
		} else {
			path.MoveTo(layout.FPt(image.Pt(x0, card.Min.Y)))
			path.LineTo(layout.FPt(image.Pt(x0+2*half, card.Min.Y)))
			path.LineTo(layout.FPt(image.Pt(x0+half, card.Min.Y-half)))
		}
		path.Close()
		paint.FillShape(gtx.Ops, col, clip.Outline{Path: path.End()}.Op())
	}
	tri(gtx.Dp(cmdKArrow+1), pal.ring)
	tri(gtx.Dp(cmdKArrow), pal.bg)
}

// crRecWrap records txt wrapped to w px, for placing after measuring.
func crRecWrap(gtx layout.Context, th *theme.Theme, w int, s textStyle, txt string) (op.CallOp, layout.Dimensions) {
	m := op.Record(gtx.Ops)
	d := crText(gtx, th.Shaper, 0, 0, w, s, txt)
	return m.Stop(), d
}
