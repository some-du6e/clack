package ui

// A canvas's hidden file channel (core canvas_channel.go) never opens as a
// channel: Slack's jump to one of its messages is isSpaceship (wc0P
// jumpToMessage → lFpV openDocWithThreadTs → zsuR openDoc), the canvas at a
// mention's section. Karim (α.102) clicked a canvas mention and got "a limbo
// esque channel where the canvas contents are messages". Every way into a
// conversation (a route, the secondary column, a thread, the split view, the
// Activity panes) opens the canvas instead once core knows the conversation
// is a canvas's.

import (
	"sync"

	"github.com/some-du6e/clack/internal/ui/canvasview"
	"github.com/some-du6e/clack/internal/ui/views"
)

var _ views.CanvasOpener = (*App)(nil)

// canvasFocusOf is a full view's section to focus once its page is made
// (OpenCanvasAt before the route's view exists). UI goroutine only.
var canvasFocusOf sync.Map // *App -> map[string]string (file id -> section id)

// OpenCanvasAt is views.CanvasOpener: the canvas viewer on fileID, focused
// on sectionID ("" for the top; canvasview FocusSection, as openDoc's
// focusSection), in the side panel or, with full, the full view ("docs/<F…>",
// openDoc's navigation in Primary).
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
	m, _ := canvasFocusOf.Load(a)
	pending, _ := m.(map[string]string)
	if pending == nil {
		pending = map[string]string{}
		canvasFocusOf.Store(a, pending)
	}
	pending[fileID] = sectionID
}

// takeCanvasFocus is the section a full view of fileID opens at (once).
func (a *App) takeCanvasFocus(fileID string) string {
	m, ok := canvasFocusOf.Load(a)
	if !ok {
		return ""
	}
	pending := m.(map[string]string)
	sec := pending[fileID]
	delete(pending, fileID)
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

// canvasInstead opens the canvas when ch is a canvas's file channel, at
// the section of the message ts points at when it is a mention, in the
// side panel or (full) the full view. It reports whether it did.
func (a *App) canvasInstead(ch, ts string, full bool) bool {
	c := a.Core()
	if c == nil || ch == "" {
		return false
	}
	fid, ok := c.CanvasOfChannel(ch)
	if !ok {
		return false
	}
	a.OpenCanvasAt(fid, c.MentionSection(ch, ts), full)
	return true
}
