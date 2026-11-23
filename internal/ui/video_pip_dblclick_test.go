package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/pointer"
)

// The mini player's handles: a double click puts its default width back
// (onDoubleClick), and a drag ends the click run, so taking a handle again
// right after dragging it is a new drag, however slow the platform's
// double-click time (gesture.ClickRun, as the column edges count: Gio
// patch 48).
func TestPipDragEndsDoubleClick(t *testing.T) {
	viewerVideos(t)
	e := filesEnv(t, image.Pt(1440, 900), false)
	var clk time.Duration
	_, p := pipDock(t, e, &clk)
	w := image.Pt(p.rect.Min.X+3, p.rect.Min.Y+p.rect.Dy()/2) // the left side's handle (top-right: s, w, sw)
	clk += time.Second
	pipPress(e, w, pointer.Move, clk)
	pipPress(e, w, pointer.Press, clk+10*time.Millisecond)
	pipPress(e, w.Sub(image.Pt(100, 0)), pointer.Move, clk+30*time.Millisecond)
	pipPress(e, w.Sub(image.Pt(100, 0)), pointer.Release, clk+50*time.Millisecond)
	q, _ := pipShown(e)
	if q.rect.Dx() != 460 {
		t.Fatalf("the left side dragged 100px out: %d wide, want 460", q.rect.Dx())
	}
	// at once, the handle where it is now: no double click
	w = image.Pt(q.rect.Min.X+3, q.rect.Min.Y+q.rect.Dy()/2)
	pipPress(e, w, pointer.Press, clk+150*time.Millisecond)
	pipPress(e, w, pointer.Release, clk+170*time.Millisecond)
	if q, _ := pipShown(e); q.rect.Dx() != 460 {
		t.Errorf("a press right after the drag: %d wide, want 460 (it's no double click)", q.rect.Dx())
	}
	// a double click on it, past the double-click time of that press,
	// resets it
	for i := range 2 {
		at := clk + 1500*time.Millisecond + time.Duration(i)*80*time.Millisecond
		pipPress(e, w, pointer.Press, at)
		pipPress(e, w, pointer.Release, at+20*time.Millisecond)
	}
	if q, _ := pipShown(e); q.rect.Dx() != 360 {
		t.Errorf("after a double click on a handle it's %d wide, want 360 again", q.rect.Dx())
	}
}
