package composer

import (
	"image"
	"image/color"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The footer's narrow layout is Slack's TextyButtons and
// TextyButtonOverflow (gantry-v2-async-client-boot-shared, build
// 2cd9270e786c; the module that defines TextyButtonsDivider):
//
//   - TextyButtons measures its .c-texty_buttons div (offsetWidth, kept
//     current by a ResizeObserver) and passes it as containerOffsetWidth.
//     Under 65px the composer (Aa) button, under 105 the emoji button and
//     under 160 the mention button move from the row into the overflow's
//     children (ti, tr, ts).
//   - TextyButtonOverflow gets 15 child slots (React.Children.count counts
//     the false ones): those three, the clips fragment, the shortcuts
//     divider, Run shortcut, the file button and Slackbot's, Projects',
//     connectors', dictation's, email's, deep research's, model override's
//     and the submit button. With no real button among them it renders
//     them without dividers; with fewer than minButtonsForOverflow (default
//     5; MessageInput passes it only to Slackbot's composer) it renders
//     them all; otherwise it folds them behind "More options"
//     (ellipsis-horizontal-filled) when containerOffsetWidth is under
//     220 + (count − 5) × 36 = 580, or 220 when minButtonsForOverflow is
//     under 5.
//   - Folded, a click on ⋯ toggles .c-texty_buttons--overflow, a row of the
//     same buttons: position absolute, top −40px, right −36px of the
//     button's container, padding 4px, base-sec, radius 4, box-shadow 0 0 0
//     1px rgba(sk_foreground_low,.13), 0 1px 3px rgba(0,0,0,.08), dividers
//     display: none. A mousedown outside it and outside ⋯ closes it (not
//     inside a .c-popover__content), and so do the mention and Run shortcut
//     buttons (eH.current.close()).
//
// The toolbar's width is the footer's flex layout (client-boot-styles):
// .c-wysiwyg_container__footer is a flex row padded 0 4px 0 6px; the
// prefix (the + button, 28px) doesn't shrink; .c-wysiwyg_container__
// toolbar_buttons is flex: 1 with no inline padding, and .c-texty_buttons
// inside it has container: texty-buttons/inline-size, so its min-content
// is 0 and it takes all the room left; the suffix doesn't shrink and is
// padded 4px 2px 4px 4px around the send group (60px: send 32, its caret
// 28 at min-width) and the character count (gap 4px). With no prefix the
// footer renders .c-wysiwyg_container__spacer (flex: 100%), which takes
// that room instead and leaves the toolbar 0 wide: nothing folds. Measured
// in Chrome with those rules: a 686px box gives a 580px toolbar, so the
// footer folds below a 686px wide box (scratch/gap-w28/measure).

const (
	crOverflowDefault = 5  // TextyButtonOverflow's minButtonsForOverflow default (tm)
	crOverflowKids    = 15 // TextyButtons' child slots
)

// crFootMoves is where each of the first three buttons moves into the
// overflow: below this toolbar width (ti, tr, ts).
var crFootMoves = map[crBtn]int{crAa: 65, crEmoji: 105, crMention: 160}

// crMore is the overflow row's state (TextyButtonOverflow's l).
type crMore struct {
	open  bool
	shown bool   // drawn last frame (the ⋯ was)
	press uint64 // the press count last checked
	n     int    // buttons in the row last frame
	bg    int    // the row's own press tag (its padding doesn't fall through)
}

// toolbarWidth is .c-texty_buttons' width in a footer whose box content
// is inner px wide: the footer padding, the + prefix and the suffix
// (last frame's submit group, the send group until then) taken off.
func (c *chrome) toolbarWidth(gtx layout.Context, inner int) int {
	if c.cur.Hide&(1<<crAttach) != 0 {
		return 0 // no prefix: the spacer takes the room
	}
	suf := c.sufW
	if suf <= 0 {
		suf = gtx.Dp(32) + gtx.Dp(28)
	}
	// footer 6 + 4, prefix 28, suffix 4 + group + 2
	return max(inner-gtx.Dp(6+4)-gtx.Dp(28)-gtx.Dp(4+2)-suf, 0)
}

// moved reports whether id is in the overflow's children at this toolbar
// width (Aa, emoji and mention move in when it's narrow).
func (c *chrome) moved(id crBtn) bool {
	w, ok := crFootMoves[id]
	if !ok || c.f.eq <= 0 {
		return false
	}
	return c.f.eq < int(float32(w)*c.pxPerDpOr1()+.5)
}

func (c *chrome) pxPerDpOr1() float32 {
	if c.pxPerDp <= 0 {
		return 1
	}
	return c.pxPerDp
}

// inOverflow reports whether id is one of TextyButtonOverflow's children
// this frame.
func (c *chrome) inOverflow(id crBtn) bool {
	switch id {
	case crVideo, crAudio, crShortcut:
		return true
	case crAa, crEmoji, crMention:
		return c.moved(id)
	}
	return false
}

// overflowButtons are the overflow's real (non-divider) children, in
// order, that this composer shows.
func (c *chrome) overflowButtons(dst []crBtn) []crBtn {
	edit := c.cur.Mode == modeEdit
	for _, id := range crFootOverflow {
		if c.hidden(id) || !c.inOverflow(id) {
			continue
		}
		if edit && (id == crMention || id == crVideo || id == crAudio || id == crShortcut) {
			continue
		}
		dst = append(dst, id)
	}
	return dst
}

// foldFooter is TextyButtonOverflow's choice: fold behind ⋯ when there is
// a real button to fold, the slots reach minButtonsForOverflow and the
// toolbar is narrower than the threshold.
func (c *chrome) foldFooter() bool {
	if c.cur.Mode == modeEdit {
		return false // the edit composer's toolbar has nothing to fold
	}
	var buf [len(crFootOverflow)]crBtn
	if len(c.overflowButtons(buf[:0])) == 0 {
		return false
	}
	s := c.f.minOverflow
	if crOverflowKids < s {
		return false
	}
	f := 220
	if s >= crOverflowDefault {
		f = 220 + (crOverflowKids-crOverflowDefault)*36
	}
	return c.f.eq > 0 && float32(c.f.eq) < float32(f)*c.pxPerDpOr1()
}

// toggleMore is ⋯'s onClick.
func (c *chrome) toggleMore() {
	c.more.open = !c.more.open
	c.more.press = pressCount()
}

// moreRowRect is the row's rectangle for n buttons next to the ⋯ button
// at btn: right −36px and top −40px of its container (the button with its
// 2px block margins).
func crMoreRowRect(gtx layout.Context, btn image.Rectangle, n int) image.Rectangle {
	w := gtx.Dp(4)*2 + n*gtx.Dp(28) + max(n-1, 0)*gtx.Dp(4)
	h := gtx.Dp(4)*2 + gtx.Dp(28) + gtx.Dp(2)*2
	top := btn.Min.Y - gtx.Dp(2) - gtx.Dp(40)
	right := btn.Max.X + gtx.Dp(36)
	return image.Rect(right-w, top, right, top+h)
}

// moreOutside closes the row on a press outside it and outside ⋯ (the
// document mousedown listener), read at the start of a frame.
func (c *chrome) moreOutside(gtx layout.Context, st *chromeState) {
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &c.more.bg, Kinds: pointer.Press | pointer.Release}); !ok {
			break
		}
	}
	if !c.more.open || !c.more.shown {
		return
	}
	n := pressCount()
	if n == c.more.press {
		return
	}
	c.more.press = n
	at := lastPress()
	btn := c.rect[crFootMore].Add(st.Origin)
	row := crMoreRowRect(gtx, btn, c.more.n)
	if !at.In(btn) && !at.In(row) {
		c.more.open = false
	}
}

// layoutMoreRow draws the open overflow row above everything (deferred),
// placed from the ⋯ button drawn at r (row-local).
func (c *chrome) layoutMoreRow(gtx layout.Context, r image.Rectangle) {
	c.more.shown = true
	if !c.more.open {
		return
	}
	var buf [len(crFootOverflow)]crBtn
	ids := c.overflowButtons(buf[:0])
	c.more.n = len(ids)
	if len(ids) == 0 {
		return
	}
	th := c.f.h.Theme()
	row := crMoreRowRect(gtx, r, len(ids))
	m := op.Record(gtx.Ops)
	v := css.SlackVars(th.Dark)
	bg, _ := css.ParseColor(v["--dt_color-base-sec"])
	theme.SetColor(th.Tokens, &bg, "var(--dt_color-base-sec)")
	ring, ok := css.ParseColor("rgb(" + v["--sk_foreground_low"] + ")")
	if !ok {
		ring = th.C.Fg
	}
	ring.A = 33 // .13
	rad := gtx.Dp(4)
	theme.Shadow(gtx, row.Add(image.Pt(0, gtx.Dp(1))), rad, gtx.Dp(3), color.NRGBA{A: 20})
	px := max(1, gtx.Dp(1))
	theme.RRect(gtx, row.Inset(-px), rad+px, ring)
	theme.RRect(gtx, row, rad, bg)
	func() {
		defer clip.Rect(row).Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &c.more.bg)
	}()
	// the buttons draw on the row's own background
	pal := c.fpal
	pal.bg = bg
	saved, savedBtn := c.fpal, c.f.btn
	c.fpal, c.f.btn = pal, slackc.Styled{} // not the ⋯ button's own CSS
	bs := gtx.Dp(28)
	x := row.Min.X + gtx.Dp(4)
	y := row.Min.Y + (row.Dy()-bs)/2
	for _, id := range ids {
		c.footButton(gtx, th, &c.fpal, &c.cur, id, image.Rect(x, y, x+bs, y+bs))
		x += bs + gtx.Dp(4)
	}
	c.fpal, c.f.btn = saved, savedBtn
	op.Defer(gtx.Ops, m.Stop())
}
