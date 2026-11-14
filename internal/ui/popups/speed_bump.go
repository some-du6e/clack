package popups

// Slack Kit's ModalSpeedBump (Z8hx), rendered by a ModalFooter with
// renderSpeedBump (1QG1) while the modal's speed bump is on
// (forceSpeedBumpEnabled / enableSpeedBump): closing the modal (the
// close button, Escape, a click outside, a ModalCloseButton) shows it
// instead (zUGg's closeModal). c-modal_speed_bump veils the whole card
// (rgba(primary background, .7), z-index 2) and its
// c-modal_speed_bump_content sheet sits at the bottom: base-pry, a 1px
// otl-ter line on top, radius 0 0 8px 8px, padding 20px 24px. The
// text_wrapper (padding-bottom 24px) holds the heading
// (c-modal_speed_bump__heading: 18px black, line-height 1.3) over the
// description, or without a heading "**Discard changes?** Closing this
// window will cancel any unsaved changes."; then the button row
// (justify-content flex-end, gap 12px): Cancel (outline, autofocused)
// and Continue (danger). Escape on it cancels it.

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// SpeedBump is a modal's speed bump for an owner outside popups: wire
// the modal's CloseGuard to Guard, Over to Layout and FocusStops to
// Stops (while it's up).
type SpeedBump struct {
	Heading, Description string
	Keep, Continue       *ModalButton
	// Dirty turns it on (forceSpeedBumpEnabled); OnContinue is the
	// continue button's onContinue (it closes the modal itself).
	Dirty      func() bool
	OnContinue func()

	m       *Modal
	up      bool
	focused bool
	veil    int
}

// NewSpeedBump makes the speed bump for m's owner: keep is
// cancelButtonLabel, cont continueButtonLabel (danger).
func NewSpeedBump(heading, description, keep, cont string, dirty func() bool, onContinue func()) *SpeedBump {
	return &SpeedBump{Heading: heading, Description: description,
		Keep: &ModalButton{Label: keep, Kind: BtnOutline}, Continue: &ModalButton{Label: cont, Kind: BtnDanger},
		Dirty: dirty, OnContinue: onContinue}
}

// Attach ties the speed bump to its modal.
func (sb *SpeedBump) Attach(m *Modal) { sb.m = m }

// Up reports the speed bump is showing.
func (sb *SpeedBump) Up() bool { return sb.up }

// Guard is the modal's CloseGuard: on the speed bump a close cancels it;
// with changes the speed bump comes up instead of the close.
func (sb *SpeedBump) Guard() bool {
	switch {
	case sb.up:
		sb.up = false
		return false
	case sb.Dirty != nil && sb.Dirty():
		sb.up, sb.focused = true, false
		return false
	}
	return true
}

// Stops are the focus stops while it's up (nil otherwise).
func (sb *SpeedBump) Stops() []event.Tag {
	if !sb.up {
		return nil
	}
	return []event.Tag{&sb.Keep.click, &sb.Continue.click}
}

// Layout is the modal's Over while the speed bump is up.
func (sb *SpeedBump) Layout(gtx layout.Context) {
	if !sb.up || sb.m == nil {
		return
	}
	if !sb.focused {
		sb.focused = true
		sb.m.focusTo(gtx, &sb.Keep.click, true)
	}
	speedBumpSheet(gtx, sb.m.h, sb.m, &sb.veil, sb.Keep, sb.Continue, sb.KeepEditing, sb.Discard, sb.Heading, []cnRun{{s: sb.Description}})
}

// KeepEditing is the cancel button: back to the modal.
func (sb *SpeedBump) KeepEditing() { sb.up = false }

// Discard is the continue button: onContinue.
func (sb *SpeedBump) Discard() {
	sb.up = false
	if sb.OnContinue != nil {
		sb.OnContinue()
	}
}

// speedBumpSheet draws c-modal_speed_bump over the card (gtx.Constraints.Max):
// the veil, then the sheet with heading (none: runs alone, the default
// text) over runs and the buttons; keep and cont call their funcs.
func speedBumpSheet(gtx layout.Context, h Host, m *Modal, veil *int, keep, cont *ModalButton, onKeep, onCont func(), heading string, runs []cnRun) {
	th := h.Theme()
	cl := colorsOf(th)
	if keep.click.Clicked(gtx) {
		onKeep()
		return
	}
	if cont.click.Clicked(gtx) {
		onCont()
		return
	}
	sz := gtx.Constraints.Max
	func() {
		rad := dp(gtx, 8)
		defer clip.RRect{Rect: image.Rectangle{Max: sz}, NE: rad, NW: rad, SE: rad, SW: rad}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, veil)
		for {
			if _, ok := gtx.Event(pointer.Filter{Target: veil, Kinds: pointer.Press | pointer.Release | pointer.Scroll,
				ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}}); !ok {
				break
			}
		}
		fill(gtx, image.Rectangle{Max: sz}, theme.WithAlpha(cl.ModalBg, 179))
	}()
	px, py := dp(gtx, 24), dp(gtx, 20)
	iw := sz.X - 2*px
	call, td := record(loose(gtx, image.Pt(iw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		y := 0
		var hd layout.Dimensions
		if heading != "" {
			hc, d := record(loose(gtx, image.Pt(iw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Max.X = iw
				return para(gtx, th, 18, 18*1.3, theme.Black, cl.Fg, heading, 0)
			})
			macroAt(gtx, hc, image.Point{})
			hd, y = d, d.Size.Y
		}
		bc, bd := record(loose(gtx, image.Pt(iw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return cnRuns(gtx, th, cl, runs, 15, 22, cl.Fg, iw)
		})
		macroAt(gtx, bc, image.Pt(0, y))
		return layout.Dimensions{Size: image.Pt(max(hd.Size.X, bd.Size.X), y+bd.Size.Y)}
	})
	bh := dp(gtx, 36)
	sheetH := py + td.Size.Y + dp(gtx, 24) + bh + py
	top := sz.Y - sheetH
	sheet := image.Rect(0, top, sz.X, sz.Y)
	rad := dp(gtx, 8)
	func() {
		defer clip.RRect{Rect: sheet, SE: rad, SW: rad}.Push(gtx.Ops).Pop()
		fill(gtx, sheet, BasePryOf(th))
	}()
	fill(gtx, image.Rect(0, top, sz.X, top+border1(gtx)), theme.OtlTer(th))
	macroAt(gtx, call, image.Pt(px, top+py))
	by := top + py + td.Size.Y + dp(gtx, 24)
	x := sz.X - px
	for _, b := range []*ModalButton{cont, keep} {
		ring := m.ringOn(gtx, &b.click)
		c, d := record(loose(gtx, image.Pt(sz.X, bh)), func(gtx layout.Context) layout.Dimensions {
			return b.draw(gtx, th, b.Kind, ring, grpNone)
		})
		x -= d.Size.X
		macroAt(gtx, c, image.Pt(x, by))
		b.rect = image.Rectangle{Min: image.Pt(x, by), Max: image.Pt(x, by).Add(d.Size)}.Add(m.rect.Min)
		x -= dp(gtx, 12)
	}
}
