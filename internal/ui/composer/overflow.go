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
// TextyButtonOverflow (gantry-v2-async-client-boot-shared; module VIJ/ in
// build 35b42de8b870, the same code in 1f2d42be66b2, 2026-11-20):
//
//   - TextyButtons measures its .c-texty_buttons div (offsetWidth, kept
//     current by a ResizeObserver) and passes it as containerOffsetWidth.
//     Under 65px the composer (Aa) button, under 105 the emoji button and
//     under 160 the mention button move from the row into the overflow's
//     children (td, tc, tu); unfolded they still draw where they were.
//   - Its measureInlineButtons (a layout effect, run again when a child
//     resizes or the children change) passes inlineButtonsWidth: from the
//     div's left edge to the right edge of its last button or divider,
//     rounded, measured only while nothing is folded (it returns at once
//     when the div holds a .c-texty_buttons__overflow_container), so it's
//     the buttons' own width in a row, kept while folded. Both are set
//     before the first paint: the first render, with containerOffsetWidth
//     still null, folds nothing.
//   - TextyButtonOverflow folds its children behind "More options"
//     (ellipsis-horizontal-filled) when one of them is a real button (not
//     a divider), there are at least minButtonsForOverflow of them (5;
//     MessageInput passes 2 only to Slackbot's composer) and
//     containerOffsetWidth < (inlineButtonsWidth ?? 220 + (count − 5) ×
//     36): the buttons fold only when they don't fit. React.Children.count
//     counts the false slots, so Slack's own children are 15 (Tasks' and
//     Test as member's are arrays, empty when off). Builds before
//     35b42de8b870 had no inlineButtonsWidth and folded below 580px.
//   - Taut's composer.addButton (app/slack/composer/buttons.tsx, Taut
//     3.7.2) patches TextyButtonOverflow where a button is registered for
//     the composer's kind: its children become [...Children.toArray
//     (children), ...native buttons, ...submit, ...end buttons], so the
//     plugin buttons draw after Slack's in the toolbar, before the send
//     group, and fold into the row with them. toArray drops the false
//     slots, so there the count is the slots that render plus the plugin
//     buttons: with one or two it stays under 5 until the narrow moves
//     add the rest, and a toolbar narrower than its buttons (137px, and
//     30 more per plugin button) doesn't fold. The footer is a flex row
//     with no clip (.c-texty_buttons is only container: inline-size), so
//     [/] and the plugin buttons run under the send group, which is later
//     in the tree and position: relative, and takes their clicks once the
//     composer has text (disabled, it has pointer-events: none). Clack
//     counts the plugin buttons on top of Slack's 15 slots instead: the
//     toolbar folds whenever its buttons don't fit, as Slack's own rule
//     means it to.
//   - Folded, a click on ⋯ toggles .c-texty_buttons--overflow, a row of the
//     same children: position absolute, top −40px, right −36px of the
//     button's container, padding 4px, base-sec, radius 4, box-shadow 0 0 0
//     1px rgba(sk_foreground_low,.13), 0 1px 3px rgba(0,0,0,.08), dividers
//     display: none. While it's open the "More options" tooltip has
//     tipClassName display_none; every tooltip (delay 500, zIndex
//     above_fs) is in the top layer, above the row. A mousedown outside it
//     and outside ⋯ closes it (not inside a .c-popover__content), and so
//     do the mention and Run shortcut buttons (eU.current.close()).
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
// that room instead and leaves the toolbar 0 wide: nothing folds.

const (
	crOverflowDefault = 5  // TextyButtonOverflow's minButtonsForOverflow default (tH)
	crOverflowKids    = 15 // TextyButtons' child slots
)

// crFootMoves is where each of the first three buttons moves into the
// overflow: below this toolbar width (td, tc, tu).
var crFootMoves = map[crBtn]int{crAa: 65, crEmoji: 105, crMention: 160}

// crPlugPlacements are Taut's composer.addButton placements in the order
// its TextyButtonOverflow patch appends them.
var crPlugPlacements = [...]string{"native", "end"}

// crMore is the overflow row's state (TextyButtonOverflow's m).
type crMore struct {
	open  bool
	shown bool   // drawn last frame (the ⋯ was)
	press uint64 // the press count last checked
	w     int    // the row's content width last drawn: its buttons and gaps
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

// crToolbarLeft is where .c-texty_buttons starts in the footer row: the
// footer's 6px padding and the 28px prefix.
func crToolbarLeft(gtx layout.Context) int { return gtx.Dp(6) + gtx.Dp(28) }

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

// tautKind is the kind of composer Taut's ComposerContext gives this one
// (composerContext): a field (RichTextInputElement's, no viewContext) is
// "other".
func (c *chrome) tautKind() string {
	st := &c.cur
	switch {
	case st.Mode == modeEdit:
		return "edit"
	case st.Mode == modeField:
		return "other"
	case st.ThreadTS != "":
		return "thread"
	}
	return "channel"
}

// plugButtons is how many composer.addButton buttons Taut puts among this
// composer's toolbar children (0 without any, or without Taut).
func (c *chrome) plugButtons() int {
	if !slackc.Patched(tautButtons) {
		return 0
	}
	return currentTaut().ComposerButtonsFor(c.tautKind())
}

// overflowKids is TextyButtonOverflow's React.Children.count and whether
// one of the children is a real button: Slack's 15 slots, false ones
// included, and Taut's plugin buttons after them, each a real child
// whatever its render returns. (Taut's own patch counts only the slots
// that render: see above for why Clack doesn't copy that.)
func (c *chrome) overflowKids() (n int, real bool) {
	var buf [len(crFootOverflow)]crBtn
	real = len(c.overflowButtons(buf[:0])) > 0 || c.f.plugBtns > 0
	return crOverflowKids + c.f.plugBtns, real
}

// foldFooter is TextyButtonOverflow's choice: fold behind ⋯ when one of
// the children is a real button, they reach minButtonsForOverflow and the
// toolbar is narrower than the buttons' own width in a row.
func (c *chrome) foldFooter() bool {
	if c.cur.Mode == modeEdit || c.cur.Mode == modeField {
		return false // the edit composer's toolbar has nothing to fold
	}
	n, real := c.overflowKids()
	if !real || n < c.f.minOverflow || c.f.eq <= 0 {
		return false
	}
	return c.f.eq < c.foldAt(n)
}

// foldAt is the toolbar width (px) the footer folds below for n children:
// inlineButtonsWidth as TextyButtons last measured it, before that the
// width Slack's buttons this composer shows take in a row, and with no
// button at all TextyButtonOverflow's 220 + (n − 5) × 36.
func (c *chrome) foldAt(n int) int {
	if c.inlineW > 0 {
		return c.inlineW
	}
	if c.f.inlineGuess > 0 {
		return c.f.inlineGuess
	}
	f := 220
	if c.f.minOverflow >= crOverflowDefault && n > crOverflowDefault {
		f = 220 + (n-crOverflowDefault)*36
	}
	return int(float32(f)*c.pxPerDpOr1() + .5)
}

// inlineGuess is inlineButtonsWidth before TextyButtons first measures
// it (Slack measures it before its first paint): from the toolbar's left
// edge to the right edge of the last of Slack's buttons and dividers
// this composer shows, laid out in a row with crRow's margins. ts is
// .c-texty_buttons' style (layoutTextyButtons): a button plugin CSS hides
// there (SlimMessageBox's emoji) takes no room, as the guess would
// otherwise overshoot, fold the toolbar and never let it be measured.
func (c *chrome) inlineGuess(gtx layout.Context, ts slackc.Styled) int {
	x, right := 0, 0
	for _, it := range crRow {
		if it.div == 0 && (it.id == crAttach || it.id == crFootMore) {
			continue // the prefix, and the overflow's own button
		}
		w := gtx.Dp(28)
		if it.div != 0 {
			if !c.dividerOn(it.div) || ts.Child(crDividerClass, 0).Hidden() {
				continue
			}
			w = max(gtx.Dp(1), 1)
		} else if !c.inlineOn(it.id) || ts.Child(crBtnClass[it.id], c.btnMods(it.id)).Hidden() {
			continue
		}
		x += gtx.Dp(it.l)
		right = x + w
		x = right + gtx.Dp(it.r)
	}
	return right
}

// toggleMore is ⋯'s onClick.
func (c *chrome) toggleMore() {
	c.more.open = !c.more.open
	c.more.press = pressCount()
}

// crMoreRowRect is the row's rectangle for n buttons next to the ⋯
// button at btn.
func crMoreRowRect(gtx layout.Context, btn image.Rectangle, n int) image.Rectangle {
	return crMoreRowRectW(gtx, btn, n*gtx.Dp(28)+max(n-1, 0)*gtx.Dp(4))
}

// crMoreRowRectW is the row's rectangle for content w px wide next to the
// ⋯ button at btn: right −36px and top −40px of its container (the button
// with its 2px block margins), padding 4px.
func crMoreRowRectW(gtx layout.Context, btn image.Rectangle, w int) image.Rectangle {
	w += gtx.Dp(4) * 2
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
	row := crMoreRowRectW(gtx, btn, c.more.w)
	if !at.In(btn) && !at.In(row) {
		c.more.open = false
	}
}

// plugRender records Taut's composer.addButton buttons for placement pl
// laid out at the origin, as wide as they are (an empty tree takes no
// room, whatever the row's constraints).
func (c *chrome) plugRender(gtx layout.Context, pl string) (op.CallOp, layout.Dimensions) {
	g := gtx
	g.Constraints.Min = image.Point{}
	m := op.Record(gtx.Ops)
	d := slackc.Render(g, tautButtons, c.tautButtonsProps(pl), noNative)
	return m.Stop(), d
}

// layoutMoreRow draws the open overflow row, placed from the ⋯ button
// drawn at r (row-local): Slack's folded buttons, then Taut's. It draws
// in the footer's own ops, over the box's content and under every
// tooltip (they're deferred), as the absolutely positioned row sits under
// Slack's top-layer tooltips.
func (c *chrome) layoutMoreRow(gtx layout.Context, r image.Rectangle) {
	c.more.shown = true
	if !c.more.open {
		return
	}
	var buf [len(crFootOverflow)]crBtn
	ids := c.overflowButtons(buf[:0])
	var plug [len(crPlugPlacements)]crSuf
	np := 0
	if c.f.plugBtns > 0 {
		for _, pl := range crPlugPlacements {
			if call, d := c.plugRender(gtx, pl); d.Size.X > 0 {
				plug[np] = crSuf{call: call, size: d.Size}
				np++
			}
		}
	}
	bs, gap := gtx.Dp(28), gtx.Dp(4)
	k := len(ids) + np
	if k == 0 {
		c.more.w = 0
		return
	}
	cw := len(ids)*bs + (k-1)*gap
	for _, s := range plug[:np] {
		cw += s.size.X
	}
	c.more.w = cw
	th := c.f.h.Theme()
	row := crMoreRowRectW(gtx, r, cw)
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
	x := row.Min.X + gap
	y := row.Min.Y + (row.Dy()-bs)/2
	for _, id := range ids {
		c.footButton(gtx, th, &c.fpal, &c.cur, id, image.Rect(x, y, x+bs, y+bs))
		x += bs + gap
	}
	c.fpal, c.f.btn = saved, savedBtn
	for _, s := range plug[:np] {
		t := op.Offset(image.Pt(x, row.Min.Y+(row.Dy()-s.size.Y)/2)).Push(gtx.Ops)
		s.call.Add(gtx.Ops)
		t.Pop()
		x += s.size.X + gap
	}
}
