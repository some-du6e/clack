package ui

// A canvas's hidden file channel (core canvas_channel.go) never opens as a
// channel: Slack's jump to one of its messages is isSpaceship (wc0P
// jumpToMessage → lFpV openDocWithThreadTs → zsuR openDoc), the canvas at a
// mention's section or with a comment's thread. Karim (α.102) clicked a
// canvas mention and got "a limbo esque channel where the canvas contents
// are messages". Every jump into the conversation opens the canvas instead
// once core knows the conversation is a canvas's: a route to a message or
// a thread (Navigate), the secondary column, the split view, the Activity
// panes. A comment thread itself still opens in the pane
// (OpenCommentThread, OpenThreadAt).
//
// The jump reads what it opens from a message (core CanvasJump). When the
// store doesn't hold it, messages.list brings it first, as lFpV does, and
// the canvas opens when it answers. A file channel whose canvas files.info
// hasn't named yet waits for the "names" that brings the file id
// (canvasJumpNames), and opens the conversation after all if the file
// turns out to be no canvas (a list's). Any navigation in between drops a
// jump that is still waiting.

import (
	"context"
	"log"
	"sync"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/canvasview"
	"github.com/some-du6e/clack/internal/ui/views"
)

var _ views.CanvasOpener = (*App)(nil)

// canvasJumpState is the App's jumps into canvases. UI goroutine only.
type canvasJumpState struct {
	// gen counts jumps and navigations: a jump that waited for its
	// canvas or its message opens only while gen is still its own
	gen     uint64
	waiting map[string]canvasJumpWait // file id -> the jump waiting for its files.info
	// focus is a full view's section to focus once its page is made
	// (OpenCanvasAt before the route's view exists)
	focus map[string]string
	// pane is the Activity persistent detail panel's canvas (CanvasPane)
	pane *canvasview.View
	// bypass is set while a waiting jump falls back to the conversation
	bypass bool
}

// canvasJumpWait is a jump waiting for its canvas's files.info.
type canvasJumpWait struct {
	gen            uint64
	ch, thread, ts string
	where          views.CanvasWhere
	fallback       func() // the conversation itself, when the file is no canvas
}

var canvasJumps sync.Map // *App -> *canvasJumpState

func (a *App) canvasJumpsOf() *canvasJumpState {
	if v, ok := canvasJumps.Load(a); ok {
		return v.(*canvasJumpState)
	}
	v, _ := canvasJumps.LoadOrStore(a, &canvasJumpState{waiting: map[string]canvasJumpWait{}, focus: map[string]string{}})
	return v.(*canvasJumpState)
}

// canvasNavigated drops the canvas jumps still waiting (any navigation).
func (a *App) canvasNavigated() { a.canvasJumpsOf().gen++ }

// OpenCanvasAt is the canvas viewer on fileID, focused on sectionID (""
// for the top; canvasview FocusSection, as openDoc's focusSection), in the
// side panel or, with full, the full view ("docs/<F…>", openDoc's
// navigation in Primary).
func (a *App) OpenCanvasAt(fileID, sectionID string, full bool) {
	if fileID == "" {
		return
	}
	if !full {
		a.OpenCanvas(fileID)
		if v, ok := a.secondary.view.(*canvasview.View); ok && v.ID() == fileID && sectionID != "" {
			v.FocusSection(sectionID)
		}
		return
	}
	r := Route("docs/" + fileID)
	a.Navigate(string(r))
	if sectionID == "" {
		return
	}
	if v, ok := a.views[r].(*canvasview.View); ok {
		v.FocusSection(sectionID)
		return
	}
	a.canvasJumpsOf().focus[fileID] = sectionID
}

// takeCanvasFocus is the section a full view of fileID opens at (once).
func (a *App) takeCanvasFocus(fileID string) string {
	st := a.canvasJumpsOf()
	sec := st.focus[fileID]
	delete(st.focus, fileID)
	return sec
}

// CanvasShown is views.CanvasOpener: the secondary column shows canvas
// fileID.
func (a *App) CanvasShown(fileID string) bool {
	if a.secondary.kind == secNone || a.secondary.hidden {
		return false
	}
	v, ok := a.secondary.view.(*canvasview.View)
	return ok && v.ID() == fileID
}

// OpenCanvasJump is views.CanvasOpener: Activity's jump into canvas file
// channel ch, at its thread or message ts.
func (a *App) OpenCanvasJump(ch, thread, ts string, where views.CanvasWhere) {
	a.canvasInstead(ch, thread, ts, where, nil)
}

// CanvasPane is views.CanvasOpener: canvas fileID in the Activity
// persistent detail panel's own pane. Slack's openDoc puts the canvas in
// Primary and leaves the inbox in the Sidebar.
func (a *App) CanvasPane(fileID string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions { return a.activityCanvas(fileID).Layout(gtx) }
}

// activityCanvas is the Activity pane's canvas view on fileID: the one it
// has, or a new one for another canvas.
func (a *App) activityCanvas(fileID string) *canvasview.View {
	st := a.canvasJumpsOf()
	if st.pane == nil || st.pane.ID() != fileID {
		st.pane = a.newCanvasView(fileID, canvasview.Full)
	}
	return st.pane
}

// canvasInstead opens the canvas when ch is a canvas's file channel, as
// Slack opens a jump to its message ts or thread (core CanvasJump): a
// mention at its section, a comment with its comment thread in the pane
// (OpenCommentThread; the canvas is its back stack when the canvas is in
// the pane too). where says where the canvas goes. A jump that waits for
// files.info calls fallback (the conversation, guards off) if the file
// is no canvas. It reports whether the jump is the canvas's.
func (a *App) canvasInstead(ch, thread, ts string, where views.CanvasWhere, fallback func()) bool {
	c := a.Core()
	st := a.canvasJumpsOf()
	if c == nil || ch == "" || st.bypass {
		return false
	}
	at := thread
	if at == "" {
		at = ts
	}
	fid, ok, wait := c.CanvasOfJump(ch, at)
	switch {
	case wait:
		st.gen++
		st.waiting[fid] = canvasJumpWait{gen: st.gen, ch: ch, thread: thread, ts: ts, where: where, fallback: fallback}
		return true
	case !ok:
		return false
	}
	a.canvasJump(fid, ch, thread, ts, where)
	return true
}

// canvasJump opens canvas fid where a jump to ch's thread or message ts
// says, from the message the store holds, else once messages.list brought
// it. Nothing opens if it can't be had, as in Slack.
func (a *App) canvasJump(fid, ch, thread, ts string, where views.CanvasWhere) {
	c := a.Core()
	st := a.canvasJumpsOf()
	st.gen++
	if sec, comment, known := c.CanvasJump(ch, thread, ts); known {
		a.openCanvasTarget(fid, ch, sec, comment, where)
		return
	}
	gen := st.gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		sec, comment, err := c.CanvasJumpFetch(ctx, ch, thread, ts)
		a.Post(func() {
			if a.canvasJumpsOf().gen != gen || a.Core() != c {
				return // another jump or a navigation since, or another account
			}
			if err != nil {
				log.Printf("canvas %s: the jump's message: %v", fid, err)
				return
			}
			a.openCanvasTarget(fid, ch, sec, comment, where)
		})
	}()
}

// openCanvasTarget opens canvas fid at section sec where says, with
// comment thread comment (its root's ts) of file channel ch beside it.
func (a *App) openCanvasTarget(fid, ch, sec, comment string, where views.CanvasWhere) {
	switch where {
	case views.CanvasPane:
		if v := a.activityCanvas(fid); sec != "" {
			v.FocusSection(sec)
		}
		a.Invalidate()
	case views.CanvasFull:
		a.OpenCanvasAt(fid, sec, true)
	default:
		a.OpenCanvasAt(fid, sec, false)
	}
	if comment != "" {
		a.OpenCommentThread(fid, ch, comment)
	}
}

// canvasJumpNames resumes the jumps that waited for their canvas's
// files.info ("names" with the file id): the canvas opens, or, when the
// file is no canvas after all, the conversation the jump asked for.
func (a *App) canvasJumpNames(ids []string) {
	st := a.canvasJumpsOf()
	if len(st.waiting) == 0 || a.Core() == nil {
		return
	}
	for _, id := range ids {
		w, ok := st.waiting[id]
		if !ok {
			continue
		}
		delete(st.waiting, id)
		if w.gen != st.gen {
			continue // another jump or a navigation since
		}
		at := w.thread
		if at == "" {
			at = w.ts
		}
		switch fid, ok, wait := a.Core().CanvasOfJump(w.ch, at); {
		case ok:
			a.canvasJump(fid, w.ch, w.thread, w.ts, w.where)
		case wait:
			st.waiting[id] = w // still unknown: the next "names"
		case w.fallback != nil:
			st.bypass = true
			w.fallback()
			st.bypass = false
		}
	}
}
