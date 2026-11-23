package views

import (
	"image"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// A right-click on a message's body in a view that draws messages the
// way Slack's message lists do (the Threads view: duay's thread list,
// behind should_show_message_contextmenu like the channel's) opens what
// the channel's list opens there (popups.ContentMenu): the desktop
// shell's image and link menus, a mention's, a file's, a v3 card's, a
// code block's; anywhere else the message's own actions menu. Each body
// sees its presses through a pass-through tag (bodyParent); the view's
// message decides after its body laid out (contentMenu).

// bodyTag is a message body's context-press target.
type bodyTag struct{ _ byte }

// bodyPress is the last context press on a body: its view and where.
type bodyPress struct {
	v  *richview.View
	p  f32.Point
	at time.Time // the frame it came in (gtx.Now)
}

// notePress records e's context presses (the body laid out last frame),
// and the frame of a primary press on one of its links, mentions,
// buttons or images (ctlPress).
func (b *base) notePress(gtx C, e *richEntry) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &e.ctx, Kinds: pointer.Press})
		if !ok {
			return
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		if ctxclick.Press(pe) {
			b.press = bodyPress{v: e.v, p: pe.Position, at: gtx.Now}
		} else if pe.Buttons.Contain(pointer.ButtonPrimary) && e.v.HitAt(pe.Position).Kind != richview.HitNone {
			b.ctlPress = gtx.Now
		}
	}
}

// A row that opens on a click, in Slack a container with onClick around
// the message: a press on something inside it that handles its own click
// may not open the row too.
//
//   - Activity's rows are ControlledClick (boot-shared): a click whose
//     target is inside an A, BUTTON or INPUT (or a message editor or
//     select) up to the row does nothing, so a link, a mention, a Block
//     Kit button and a reaction pill click only themselves.
//   - The Pins tab's rows (p-embedded_generic_pins__container) are an
//     UnstyledButton with a plain onClick: a link's click (BaseMrkdwnLink,
//     no stopPropagation) opens the link and the pin; a reaction pill's
//     doesn't (Reaction's click is preventDefault + stopPropagation).
//
// The marks are the frame a press came in (widget.Press.Start is that
// frame's gtx.Now too): ctlPress a press on a body's link, mention,
// button or image (notePress), pillPress one on a reaction pill.

// pressedOn reports whether c's latest press is the one marked at.
func pressedOn(c *widget.Clickable, at time.Time) bool {
	if at.IsZero() {
		return false
	}
	h := c.History()
	return len(h) > 0 && h[len(h)-1].Start.Equal(at)
}

// bodyParent draws the body (call, laid out at size) inside its tag's
// area: the tag is the body's parent, so the body's own areas are hit
// first and keep their cursors (richview's text I-beam, its links'
// hand), and the tag, pass-through, still sees every press on the body
// without taking it from what is under it.
func bodyParent(gtx C, tag *bodyTag, size image.Point, call op.CallOp) {
	defer clip.Rect(image.Rectangle{Max: size}).Push(gtx.Ops).Pop()
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, tag)
	pass.Pop()
	call.Add(gtx.Ops)
}

// In a row Slack makes cursor: pointer the body inherits it, so its text
// shows the hand, not the I-beam: Activity's items
// (.p-activity_ia4_page__item:hover, .activity_row_content_container),
// Saved and Later's (.p-saved_item--message, savedItemWrapper) and the
// Pins tab's (.p-embedded_generic_pins__container
// .p-embedded_message_unfurl__container). Elsewhere (the Threads and
// Unreads views, canvas comments, the Activity pane) it is the message
// list's: the I-beam over text, the hand over links.

// handBody lays w out with every body in it under the row's hand.
func (b *base) handBody(gtx C, w W) D {
	b.hand++
	defer func() { b.hand-- }()
	return w(gtx)
}

// handOver puts the hand over e's body (size) when it is drawn in a
// handBody row: a pass-through area on top, so the body's areas still
// take every event and only the cursor is the row's.
func (b *base) handOver(gtx C, e *richEntry, size image.Point) {
	if b.hand <= 0 {
		return
	}
	defer clip.Rect(image.Rectangle{Max: size}).Push(gtx.Ops).Pop()
	pointer.CursorPointer.Add(gtx.Ops)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &e.hand)
	pass.Pop()
}

// contentMenu is the menu for this frame's context press on m's body, at
// at (window coordinates); ok is false when the press wasn't on the body
// (or was where the message's own menu opens).
func (b *base) contentMenu(gtx C, ph popups.Host, at image.Point, m core.Message) (popups.Overlay, bool) {
	p := b.press
	b.press = bodyPress{}
	if p.v == nil || !p.at.Equal(gtx.Now) {
		return nil, false
	}
	return popups.ContentMenu(ph, at, p.v, p.p, m, hostHandler{b.h}.OpenURL)
}
