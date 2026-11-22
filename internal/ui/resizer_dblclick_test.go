package ui

import (
	"image"
	"testing"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// A double click on a column edge resets its width (BaseResizer's
// onReset, Chromium's dblclick), and a drag ends the click run: taking the
// edge again right after dragging it starts a new drag, however slow the
// platform's double-click time. In Chromium a double click can't follow a
// drag (Windows ends the run when the pointer leaves its box, Linux and
// AppKit count a press only near the last one), so Clack's handles count
// presses with gesture.ClickRun, the rule gesture.Click counts by, and end
// the run on a drag past its slop (Gio patch 48).
func TestResizerDragEndsDoubleClick(t *testing.T) {
	e, _ := previewEnv(t)
	e.frame()
	a := e.a
	s := &a.sidebar
	rail := int(theme.RailW)
	y := a.primaryOrigin.Y + 200
	w0 := s.width

	// a drag 100px right in one move, let go
	edge := rail + w0
	e.rzPress(image.Pt(edge, y))
	e.rzMove(image.Pt(edge+100, y))
	e.rzRelease(image.Pt(edge+100, y))
	if s.width != w0+100 || s.mode != "manual" {
		t.Fatalf("dragged 100px right: %d (%q), want %d manual", s.width, s.mode, w0+100)
	}

	// at once, the edge where it is now: a new drag, not a double click
	at := rail + s.width
	e.rzPress(image.Pt(at, y))
	if !s.rz.dragging() {
		t.Fatalf("the press right after a drag was a double click: the edge reset to %d (%q), no drag", s.width, s.mode)
	}
	e.rzMove(image.Pt(at-40, y))
	e.rzRelease(image.Pt(at-40, y))
	if s.width != w0+60 || s.mode != "manual" {
		t.Errorf("the second drag, 40px back: %d (%q), want %d manual", s.width, s.mode, w0+60)
	}

	// a double click where it stands still resets it
	at = rail + s.width
	for range 2 {
		e.rzPress(image.Pt(at, y))
		e.rzRelease(image.Pt(at, y))
	}
	e.frame()
	if s.width != w0 || s.mode != "auto" {
		t.Errorf("double click: %d (%q), want %d auto", s.width, s.mode, w0)
	}
}
