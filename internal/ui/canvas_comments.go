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
	"image"

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

// SecondaryOrigin is views.SecondaryOriginer: the pane's window origin.
func (a *App) SecondaryOrigin() image.Point { return a.secondaryOrigin }

// canvasViews are the live views of canvas file: the side panel's, the
// full view's and the channel tabs'.
func (a *App) canvasViews(file string) []*canvasview.View {
	var out []*canvasview.View
	add := func(v View) {
		if cv, ok := v.(*canvasview.View); ok && cv.ID() == file {
			out = append(out, cv)
		}
	}
	add(a.secondary.view)
	if p := a.secondary.prev; p != nil {
		add(p.view)
	}
	add(a.views[Route("docs/"+file)])
	if m, ok := canvasTabs.Load(a); ok {
		for _, v := range m.(map[string]*canvasview.View) {
			add(v)
		}
	}
	return out
}

// CanvasPermalink is popups.CanvasMentions: the canvas's permalink, from
// a view that loaded it, else the last files.info read.
func (a *App) CanvasPermalink(file string) string {
	for _, v := range a.canvasViews(file) {
		if d := v.Doc(); d != nil && d.Info.Permalink != "" {
			return d.Info.Permalink
		}
	}
	if c := a.Core(); c != nil {
		if i, ok := c.KnownCanvasInfo(file); ok {
			return i.Permalink
		}
	}
	return ""
}

// CanvasMentionRemovable is popups.CanvasMentions (Remove mention).
func (a *App) CanvasMentionRemovable(file, section, channel string) bool {
	for _, v := range a.canvasViews(file) {
		if v.MentionRemovable(section, channel) {
			return true
		}
	}
	return false
}

// RemoveCanvasMention is popups.CanvasMentions (Remove mention): one view
// saves; the others follow the document_update.
func (a *App) RemoveCanvasMention(file, section, channel string) {
	for _, v := range a.canvasViews(file) {
		if v.MentionRemovable(section, channel) {
			v.RemoveMention(section, channel)
			return
		}
	}
}
