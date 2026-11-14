package ui

// Canvas comments in the secondary pane (docs/CANVASES.md "Comments";
// views/canvas_comments.go draws the stream, messages/thread_comment.go
// the thread). Slack's canvas More actions "Show all threads" navigates
// the secondary container to toQuipCommentStream (or, with the stream
// showing, "Hide all threads" closes it); a row of the stream is
// showQuipCommentThread, the comment thread in the same container. Slack
// lets a canvas sit beside either, and either beside the other (yzh0's
// sibling views), so:
//
//   - with the canvas in the main view (its channel tab or the full
//     view), the stream replaces whatever the pane showed;
//   - with the canvas itself in the pane (the side panel), the stream
//     takes the pane and the canvas becomes its back stack;
//   - a thread opened from the stream keeps the stream as its back stack
//     (FlexpaneBackButton).

import (
	"github.com/some-du6e/clack/internal/ui/canvasview"
	"github.com/some-du6e/clack/internal/ui/views"
)

// CanvasCommentsOpen reports whether the secondary pane shows canvas
// fileID's comment stream.
func (a *App) CanvasCommentsOpen(fileID string) bool {
	if !a.secondary.open() {
		return false
	}
	s, ok := a.secondary.view.(*views.CanvasComments)
	return ok && s.File() == fileID
}

// ToggleCanvasComments is the More actions item: Show all threads, or
// Hide all threads while the stream is open (closeSecondary).
func (a *App) ToggleCanvasComments(fileID string) {
	if a.CanvasCommentsOpen(fileID) {
		if p := a.secondary.prev; p != nil {
			if cv, ok := p.view.(*canvasview.View); ok && cv.ID() == fileID { // back to the canvas the stream covered
				a.GoSecondaryBack()
				return
			}
		}
		a.CloseSecondary()
		return
	}
	a.OpenCanvasComments(fileID, "")
}

// OpenCanvasComments shows canvas fileID's comment stream (filter "" =
// Most recent) in the secondary pane.
func (a *App) OpenCanvasComments(fileID, filter string) {
	cur := a.secondary
	var prev *secondaryState
	if cur.open() {
		if cv, ok := cur.view.(*canvasview.View); ok && cv.ID() == fileID {
			p := cur
			prev = &p
		}
		if cur.kind == secThread {
			a.leaveThreadRoute(cur.channel, cur.ts)
		}
	}
	w := cur.width
	if prev != nil && w < 520 { // the side panel's width (OpenCanvas)
		w = 520
	}
	a.secondary = secondaryState{kind: secDetails, width: w, prev: prev}
	a.secondary.view = views.NewCanvasComments(a, fileID, filter)
	a.Invalidate()
}

// OpenCommentThread is views.CommentThreadOpener: the comment thread at
// ts in file channel ch, the stream it was opened from as its back stack.
func (a *App) OpenCommentThread(file, ch, ts string) {
	cur := a.secondary
	var prev *secondaryState
	if cur.open() {
		p := cur
		prev = &p
	}
	a.secondary = secondaryState{kind: secThread, channel: ch, ts: ts, width: cur.width, prev: prev}
	a.secondary.view = a.mountThread(ch, ts)
	a.Invalidate()
}
