package views

import (
	"image"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"

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

// notePress records e's context presses (the body laid out last frame).
func (b *base) notePress(gtx C, e *richEntry) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &e.ctx, Kinds: pointer.Press})
		if !ok {
			return
		}
		if pe, ok := ev.(pointer.Event); ok && ctxclick.Press(pe) {
			b.press = bodyPress{v: e.v, p: pe.Position, at: gtx.Now}
		}
	}
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
