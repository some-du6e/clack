package ui

import (
	"context"
	"image"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

// originTracker learns a widget's window position from pointer events:
// Gio exposes no global transform, but a pass-through handler sees the same
// pointer event in local coordinates that the root tracker sees in window
// coordinates, so origin = window - local.
type originTracker struct {
	tag    int
	origin image.Point
	ok     bool
}

func (t *originTracker) track(gtx layout.Context, a *App) {
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &t.tag, Kinds: pointer.Move | pointer.Enter | pointer.Press | pointer.Drag})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok && a.ptrOK {
			t.origin = a.ptrWin.Sub(image.Pt(int(pe.Position.X+.5), int(pe.Position.Y+.5)))
			t.ok = true
		}
	}
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &t.tag)
}

// get returns the learned origin, or fallback until the pointer visits.
func (t *originTracker) get(fallback image.Point) image.Point {
	if t.ok {
		return t.origin
	}
	return fallback
}

// rootPointer records the window-coordinate pointer position (PointerPos).
// Registered last each frame, pass-through, over the whole window.
func (a *App) rootPointer(gtx layout.Context) {
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &a.ptrTag, Kinds: pointer.Move | pointer.Enter | pointer.Press | pointer.Drag | pointer.Release})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok {
			a.ptrWin = image.Pt(int(pe.Position.X+.5), int(pe.Position.Y+.5))
			a.ptrOK = true
		}
	}
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &a.ptrTag)
}

// PointerPos is the last pointer position in window coordinates (popup
// anchors; messages.PointerHost).
func (a *App) PointerPos() image.Point { return a.ptrWin }

// anchor returns p unless it's zero (unknown), else the pointer position.
func (a *App) anchor(p image.Point) image.Point {
	if p == (image.Point{}) {
		return a.ptrWin
	}
	return p
}

// typer is implemented by messages.ChannelView/ThreadView.
type typer interface{ Typing() string }

// originSetter covers the feature widgets with an exported Origin field
// (set through a small adapter in mount_features.go).
type originSetter interface{ SetOrigin(image.Point) }

// composerView is what the shell needs from composer.Composer.
type composerView interface {
	View
	SetOrigin(image.Point)
	SetOnEditLast(func())
	Done() bool
	Focus()
	Close()
}

// convo is one conversation surface: message list + composer, for a
// channel (ts == "") or a thread.
type convo struct {
	ch, ts   string
	list     View
	comp     composerView
	edit     composerView // Up-to-edit / "Edit message" composer, replaces comp
	org      originTracker
	compH    int
	compRect image.Rectangle // window coords (emoji picker anchor)
}

func (a *App) convoFor(ch, ts string) *convo {
	k := ch + "/" + ts
	if a.convos == nil {
		a.convos = map[string]*convo{}
	}
	if c := a.convos[k]; c != nil {
		return c
	}
	c := &convo{ch: ch, ts: ts}
	if ts == "" {
		c.list = a.mountChannel(ch)
	} else {
		c.list = a.mountThreadList(ch, ts)
	}
	if cv, ok := a.mountComposer(ch, ts).(composerView); ok {
		c.comp = cv
		c.comp.SetOnEditLast(func() { a.editLast(c) })
	}
	a.convos[k] = c
	a.convoOrder = append(a.convoOrder, k)
	if len(a.convoOrder) > 32 {
		old := a.convoOrder[0]
		a.convoOrder = a.convoOrder[1:]
		if oc := a.convos[old]; oc != nil && oc.comp != nil {
			oc.comp.Close() // saves the draft
		}
		delete(a.convos, old)
	}
	return c
}

// layoutConvo draws the list with the composer under it. fallback is the
// widget's window origin when known from layout (primary panel), used until
// the origin tracker has seen the pointer.
func (a *App) layoutConvo(gtx layout.Context, c *convo, fallback image.Point) layout.Dimensions {
	sz := gtx.Constraints.Max
	origin := c.org.get(fallback)
	c.org.track(gtx, a)

	comp := c.comp
	if c.edit != nil {
		if c.edit.Done() {
			c.edit = nil
			if c.comp != nil {
				c.comp.Focus()
			}
		} else {
			comp = c.edit
		}
	}
	compH := 0
	var call op.CallOp
	if comp != nil {
		comp.SetOrigin(origin.Add(image.Pt(0, sz.Y-c.compH)))
		m := op.Record(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Min: image.Pt(sz.X, 0), Max: image.Pt(sz.X, sz.Y*6/10)}
		d := comp.Layout(g)
		call = m.Stop()
		compH = d.Size.Y
		c.compH = compH
		top := origin.Add(image.Pt(0, sz.Y-compH))
		c.compRect = image.Rectangle{Min: top, Max: top.Add(image.Pt(sz.X, compH))}
	}
	listH := sz.Y - compH
	if s, ok := c.list.(originSetter); ok {
		s.SetOrigin(origin)
	}
	atExact(gtx, image.Point{}, image.Pt(sz.X, listH), func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
		return c.list.Layout(gtx)
	})
	if comp != nil {
		t := op.Offset(image.Pt(0, listH)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		t.Pop()
	}
	return layout.Dimensions{Size: sz}
}

// editLast opens the edit composer on the viewer's newest message (Up in
// an empty composer, like Slack).
func (a *App) editLast(c *convo) {
	if a.data == nil {
		return
	}
	svc := a.data.core
	self := a.model().SelfID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var msgs []core.Message
		if c.ts == "" {
			p, err := svc.History(ctx, c.ch, "")
			if err != nil {
				return
			}
			msgs = p.Messages
		} else {
			rs, err := svc.Replies(ctx, c.ch, c.ts)
			if err != nil {
				return
			}
			msgs = rs
		}
		for i := len(msgs) - 1; i >= 0; i-- {
			m := msgs[i]
			if m.User == self && !m.System && m.Subtype == "" {
				a.Post(func() { a.startEdit(c, m) })
				return
			}
		}
		a.Post(func() { a.Toast("You haven't sent anything here yet") })
	}()
}

// startEdit swaps the conversation's composer for an edit composer on m.
func (a *App) startEdit(c *convo, m core.Message) {
	if mkEditComposer == nil {
		return
	}
	c.edit = mkEditComposer(a, m, func() { a.Invalidate() })
	c.edit.Focus()
	a.Invalidate()
}

// editMessage starts editing m in its conversation (message menu "Edit").
func (a *App) editMessage(m core.Message) {
	c := a.convoFor(m.Channel, "")
	if m.ThreadTS != "" && m.ThreadTS != m.TS {
		c = a.convoFor(m.Channel, m.ThreadTS) // a reply: edit in the thread pane
	}
	a.startEdit(c, m)
}

var _ = f32.Point{}
