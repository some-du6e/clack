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

// readRootPointer consumes the root handler's pointer events at the START
// of a frame, so origin trackers laid out later in the frame pair each local
// event with the same event's window position.
func (a *App) readRootPointer(gtx layout.Context) {
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &a.ptrTag, Kinds: pointer.Move | pointer.Enter | pointer.Press | pointer.Drag | pointer.Release})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok {
			a.ptrWin = image.Pt(int(pe.Position.X+.5), int(pe.Position.Y+.5))
			a.ptrOK = true
			a.navButtons(pe)
			a.globalPointer(gtx, pe)
		}
	}
}

// rootPointer registers the window-wide pass-through handler (last each
// frame, so it sits above everything without blocking it).
func (a *App) rootPointer(gtx layout.Context) {
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
	fitH     int             // thread content height the composer was placed under (0: bottom)
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
	// Slack top-aligns a short thread: the reply composer sits right under
	// the last reply instead of at the pane's bottom (last frame's height).
	fit := 0
	if ch, ok := c.list.(interface{ ContentHeight() int }); ok && c.ts != "" {
		if h := ch.ContentHeight(); h > 0 && h < sz.Y-c.compH {
			fit = h
		}
	}
	compH := 0
	var call op.CallOp
	if comp != nil {
		// Edit composers draw just the box: give them the same side and
		// bottom margins as the normal composer (20px channel, 16px thread).
		mx, mb := 0, 0
		if comp == c.edit {
			mx = gtx.Dp(20)
			if c.ts != "" {
				mx = gtx.Dp(16)
			}
			mb = gtx.Dp(24)
		}
		compY := sz.Y - c.compH
		if fit > 0 {
			compY = fit
		}
		comp.SetOrigin(origin.Add(image.Pt(mx, compY)))
		a.appDMRoadblock(c, comp)
		m := op.Record(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Min: image.Pt(sz.X-2*mx, 0), Max: image.Pt(sz.X-2*mx, sz.Y*6/10)}
		st := op.Offset(image.Pt(mx, 0)).Push(gtx.Ops)
		d := comp.Layout(g)
		st.Pop()
		call = m.Stop()
		compH = d.Size.Y + mb
		c.compH = compH
		compY = sz.Y - compH
		if fit > 0 && fit < compY {
			compY = fit
		}
		top := origin.Add(image.Pt(0, compY))
		c.compRect = image.Rectangle{Min: top, Max: top.Add(image.Pt(sz.X, compH))}
	}
	listH := sz.Y - compH
	if fit > 0 && fit < listH {
		listH = fit
	}
	if fit != c.fitH { // the thread grew or shrank: settle on the next frame
		c.fitH = fit
		a.Invalidate()
	}
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
	// ui-messages edits inside the row (pale-yellow row, Cancel/Save).
	if e, ok := c.list.(interface{ EditLast() bool }); ok && e.EditLast() {
		return
	}
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
	if e, ok := c.list.(interface{ Edit(string) }); ok {
		e.Edit(m.TS) // inline in the row
		return
	}
	a.startEdit(c, m)
}

var _ = f32.Point{}

// refreshVisible re-reads the conversations on screen (after core resolved
// names in the background).
func (a *App) refreshVisible() {
	type refresher interface{ Refresh() }
	want := map[string]bool{}
	if ch := a.router.Current().Channel(); ch != "" {
		want[ch+"/"] = true
	}
	if a.secondary.kind == secThread {
		want[a.secondary.channel+"/"+a.secondary.ts] = true
	}
	for k, c := range a.convos {
		if !want[k] {
			continue
		}
		if r, ok := c.list.(refresher); ok {
			r.Refresh()
		}
	}
}
