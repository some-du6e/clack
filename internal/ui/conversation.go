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

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/composer"
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
			composer.NotePointer(a.ptrWin) // the editors' context menus open here
			if pe.Kind == pointer.Press {
				composer.NotePress() // a click may move the focus (editor_focus.go)
			}
			a.navButtons(pe)
			a.railPeekPointer(pe)  // the More peek's safe area (rail_peek.go)
			a.tabPeekPointer(pe)   // the tab peeks' (rail_tab_peek.go)
			a.focusPeekPointer(pe) // the focus mode peek's (focus_peek.go)
			a.splitPointer(pe)     // click modifiers, the split view's focus (split.go)
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

// layoutComposerIn lays comp out with its plugin components in slackc
// pane pane (composer.LayoutIn); a composer without one lays out as it is.
func layoutComposerIn(comp composerView, gtx layout.Context, pane string) layout.Dimensions {
	if p, ok := comp.(interface {
		LayoutIn(layout.Context, string) layout.Dimensions
	}); ok {
		return p.LayoutIn(gtx, pane)
	}
	return comp.Layout(gtx)
}

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
	jp       *joinPane       // the join pane / archived bar in place of the composer (joinpane.go)
	banner   *joinBanner     // the channel join banner over the composer (joinbanner.go)
	bannerH  int             // its height last frame
	post     core.Posting    // posting restrictions, cached at postAt
	postAt   time.Time
	// zeroSince is when the composer started laying out to no height (zero:
	// it has its height); rebuiltAt when it was last replaced for that
	// (composer_guard.go).
	zeroSince, rebuiltAt time.Time
	// focusKnown: a navigation here wanted the composer focused while the
	// channel's membership was unknown (footPending); the first frame that
	// knows it gives the composer the focus, if it has one
	focusKnown bool
}

func (a *App) convoFor(ch, ts string) *convo {
	k := ch + "/" + ts
	if a.convos == nil {
		a.convos = map[string]*convo{}
	}
	if c := a.convos[k]; c != nil {
		a.convoOrder = lruTouch(a.convoOrder, k) // view_lru.go
		return c
	}
	c := &convo{ch: ch, ts: ts}
	if ts == "" {
		c.list = a.mountChannel(ch)
	} else {
		c.list = a.mountThreadList(ch, ts)
	}
	a.attachComposer(c)
	a.convos[k] = c
	a.convoOrder = append(a.convoOrder, k)
	if len(a.convoOrder) > maxConvos {
		old := a.convoOrder[0] // the least recently used
		a.convoOrder = a.convoOrder[1:]
		if oc := a.convos[old]; oc != nil && oc.comp != nil {
			oc.comp.Close() // saves the draft
		}
		delete(a.convos, old)
		noteEvicted("convo", old)
	}
	return c
}

// attachComposer mounts c's composer and wires it to c's list.
func (a *App) attachComposer(c *convo) {
	cv, ok := a.mountComposer(c.ch, c.ts).(composerView)
	if !ok {
		return
	}
	c.comp = cv
	c.comp.SetOnEditLast(func() { a.editLast(c) })
	if fl, ok := c.comp.(interface{ SetOnFocusList(func()) }); ok {
		fl.SetOnFocusList(func() { a.focusList(c) })
	}
	if sn, ok := c.comp.(interface{ SetOnSend(func(string, bool)) }); ok {
		sn.SetOnSend(func(cid string, files bool) { jumpToOwnSend(c, cid, files) })
	}
}

// layoutConvo draws the list with the composer under it. fallback is the
// widget's window origin when known from layout (primary panel), used until
// the origin tracker has seen the pointer.
//
// The composer (or the footer in its place) lays out first, and the list
// gets the height it leaves. Slack top-aligns a short thread, its reply box
// right under the last reply instead of at the pane's bottom: where that is
// is known once the list has laid out in this frame, so the box goes there
// after the list, which is clipped to the thread it drew. Every decision
// is this frame's: a decision from the last frame's list left a thread's
// reply box out of the frame that drew its replies, and with nothing else
// to draw, the window drew no other frame until some input came.
func (a *App) layoutConvo(gtx layout.Context, c *convo, fallback image.Point) layout.Dimensions {
	sz := gtx.Constraints.Max
	origin := c.org.get(fallback)
	c.org.track(gtx, a)

	// the list's copy of the conversation for this frame, the one it then
	// draws (messages.ThreadView.Prepare): the reply box goes with it
	if p, ok := c.list.(interface{ Prepare(layout.Context) }); ok {
		p.Prepare(gtx)
	}
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
	// ThreadsError is the thread pane's only content: no reply input
	// (messages.ThreadView.HidesComposer)
	hc, _ := c.list.(interface{ HidesComposer() bool })
	if hc != nil && hc.HidesComposer() {
		comp = nil
	}
	// the composer's (or footer's) window origin while it lays out (its
	// popups, rects): where the last frame put it; the list says where it
	// goes in this frame
	guess := sz.Y - c.compH
	if c.fitH > 0 {
		guess = c.fitH
	}
	compH, mx, bannerH := 0, 0, 0
	var call op.CallOp
	foot := a.footerFor(c)
	if foot != footPending && c.focusKnown {
		// the navigation's focus, held while membership was unknown
		// (focusAfterNavigate): MessagePaneInput's onFocusTransition once
		// the input mounts
		c.focusKnown = false
		if foot == footNone && c.comp != nil && c.edit == nil && a.router.Current().Channel() == c.ch && !a.overlayOpen() {
			c.comp.Focus()
		}
	}
	switch foot {
	case footNone:
	case footPending:
		comp = nil // membership unknown: neither the composer nor a footer yet
	default:
		// a preview or an archived channel: Slack's footer, no composer
		comp = nil
		m := op.Record(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Min: image.Pt(sz.X, 0), Max: image.Pt(sz.X, sz.Y*6/10)}
		d := a.layoutFooter(g, c, foot, origin.Add(image.Pt(0, guess)))
		call = m.Stop()
		compH = d.Size.Y
		c.compH = compH
	}
	if comp != nil {
		// Edit composers draw just the box: give them the same side and
		// bottom margins as the normal composer (20px channel, 16px thread).
		mb := 0
		if comp == c.edit {
			mx = gtx.Dp(20)
			if c.ts != "" {
				mx = gtx.Dp(16)
			}
			mb = gtx.Dp(24)
		}
		comp.SetOrigin(origin.Add(image.Pt(mx, guess)))
		a.composerRoadblock(c, comp)
		m := op.Record(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Min: image.Pt(sz.X-2*mx, 0), Max: image.Pt(sz.X-2*mx, sz.Y*6/10)}
		st := op.Offset(image.Pt(mx, 0)).Push(gtx.Ops)
		// in its list's slackc pane: the plugin roots are this
		// conversation's, never another place's composer for it
		pane := ""
		if p, ok := c.list.(interface{ PaneID() string }); ok {
			pane = p.PaneID()
		}
		d := layoutComposerIn(comp, g, pane)
		if a.composerHeight(gtx, g, c, comp, d.Size.Y) { // no height: a new composer (composer_guard.go)
			comp = c.comp
			comp.SetOrigin(origin.Add(image.Pt(mx, guess)))
			a.composerRoadblock(c, comp)
			d = layoutComposerIn(comp, g, pane)
		}
		st.Pop()
		call = m.Stop()
		compH = d.Size.Y + mb
		c.compH = compH
		// the channel join banner sits over the composer (message pane
		// input chrome); the list gives up its height. Channels only, so
		// no short thread moves it.
		bm := op.Record(gtx.Ops)
		bd := a.layoutJoinBanner(gtx, c, origin.Add(image.Pt(0, sz.Y-compH-c.bannerH)))
		bcall := bm.Stop()
		if bd.Size.Y > 0 {
			inner := call
			m := op.Record(gtx.Ops)
			bcall.Add(gtx.Ops)
			t := op.Offset(image.Pt(0, bd.Size.Y)).Push(gtx.Ops)
			inner.Add(gtx.Ops)
			t.Pop()
			call = m.Stop()
			compH += bd.Size.Y
		}
		bannerH = bd.Size.Y
		if c.bannerH != bd.Size.Y {
			c.bannerH = bd.Size.Y
			a.Invalidate()
		}
	}
	// the list in what the composer leaves
	availH := sz.Y - compH
	if s, ok := c.list.(originSetter); ok {
		s.SetOrigin(origin)
	}
	lm := op.Record(gtx.Ops)
	atExact(gtx, image.Point{}, image.Pt(sz.X, availH), c.list.Layout)
	lcall := lm.Stop()
	// a thread that fits over its box ends where the list drew its last
	// reply in this frame (messages.ThreadView.ContentHeight): the list is
	// clipped there and the box goes under it
	fit := 0
	if ch, ok := c.list.(interface{ ContentHeight() int }); ok && c.ts != "" {
		if h := ch.ContentHeight(); h > 0 && h < availH {
			fit = h
		}
	}
	c.fitH = fit
	listH := availH
	if fit > 0 {
		listH = fit
	}
	cl := clip.Rect{Max: image.Pt(sz.X, listH)}.Push(gtx.Ops)
	lcall.Add(gtx.Ops)
	cl.Pop()
	compY := listH + bannerH // the composer's top (the footer's: no banner)
	if comp != nil {
		comp.SetOrigin(origin.Add(image.Pt(mx, compY))) // BoxRect, popups
		top := origin.Add(image.Pt(0, compY))
		c.compRect = image.Rectangle{Min: top, Max: top.Add(image.Pt(sz.X, compH-bannerH))}
	}
	switch {
	case comp != nil || (foot != footNone && foot != footPending):
		t := op.Offset(image.Pt(0, listH)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		t.Pop()
		if compY != guess {
			// it laid out where it was (the thread loaded, grew or shrank,
			// the composer's height changed): again, so what it anchors
			// in window coordinates is where it is
			a.Invalidate()
		}
	case foot == footNone && c.comp != nil && hc != nil && !hc.HidesComposer():
		a.Invalidate() // the thread's box shows now (its list changed under the decision)
	}
	// Slack's FileDragDrop around the pane: its drop zone, and the drop
	// overlay over list and composer while files hover it (drop_overlay.go)
	a.dropZone(c, image.Rectangle{Min: origin, Max: origin.Add(sz)}, foot)
	a.layoutDropOverlay(gtx, c, sz)
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

// focusList moves keyboard focus to the conversation's message list with
// the newest message selected (Up in the composer with Accessibility's
// "Move focus to the message list"); leaving it focuses the composer.
func (a *App) focusList(c *convo) {
	if l, ok := c.list.(interface{ FocusNewest(func()) bool }); ok {
		l.FocusNewest(func() {
			if c.comp != nil {
				c.comp.Focus()
			}
		})
	}
}

// jumpToOwnSend is the conversation's composer sending a message (its
// OnSend): the list beside it, in the same view container, moves to it
// the way Slack's does (messages.ChannelView.JumpToOwnSend for the
// sendMessage thunk's jumpToTimestamp, ThreadView.JumpToOwnSend for the
// thread pane, whose reply box is its list's last row).
func jumpToOwnSend(c *convo, cid string, files bool) {
	if l, ok := c.list.(interface{ JumpToOwnSend(string, bool) }); ok {
		l.JumpToOwnSend(cid, files)
	}
}

// FocusMessage gives channel ch's message list the keyboard focus at
// message ts once it has it (views.MessageFocuser: the Threads view's
// Enter, transitionFocusTo MESSAGE_PANE_MESSAGE_LIST); leaving the list
// goes back to the composer, as focusList's does.
func (a *App) FocusMessage(ch, ts string) {
	c := a.convoFor(ch, "")
	if l, ok := c.list.(interface{ FocusMessage(string, func()) }); ok {
		l.FocusMessage(ts, func() {
			if c.comp != nil {
				c.comp.Focus()
			}
		})
	}
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
	// the split view's (split.go)
	if top := a.splitTop(); top != "" && a.pane == "" {
		if c := a.split.pane.convos[top.Channel()+"/"+top.Thread()]; c != nil {
			if r, ok := c.list.(refresher); ok {
				r.Refresh()
			}
		}
	}
}
