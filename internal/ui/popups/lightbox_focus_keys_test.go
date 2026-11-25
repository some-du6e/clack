package popups

import (
	"testing"

	"gioui.org/io/key"
)

// One Tab in the media viewer (Gio's unhandled Tab: the next focusable
// after the viewer's own key target) must leave the viewer's keys
// working, as Slack's do: its first stop isn't the media column's close
// (Slack's is the uploader's name; the close is the media group's last),
// ZoomControl's + = - 0 work with any widget but its own focused, and
// Space goes to a focused button (it clicks it, it doesn't close the
// viewer). Clack kept the focus on the header's close after one Tab, where
// Space or Return closed the viewer and + = - 0 did nothing.
func TestMediaViewerKeysAfterTab(t *testing.T) {
	e := newMVDEnv(t, "treatment", mvdGallery)
	e.frames(3)
	e.r.MoveFocus(key.FocusForward) // Tab
	e.frames(3)
	src := e.r.Source()
	if src.Focused(&e.lb.closeBtn) {
		t.Fatal("one Tab put the focus on the media column's close (Slack's last stop in the media group)")
	}
	if src.Focused(e.lb) {
		t.Fatal("Tab didn't move the focus off the viewer: the test proves nothing")
	}
	z := e.lb.zoom
	e.press("+")
	if e.lb.zoom <= z {
		t.Errorf("+ with a toolbar button focused: zoom %v, want above %v", e.lb.zoom, z)
	}
	e.press("0")
	if e.lb.zoom != 1 {
		t.Errorf("0 with a toolbar button focused: zoom %v, want 1", e.lb.zoom)
	}
	e.press(key.NameSpace)
	e.press(key.NameReturn)
	if e.lb.Dismissed() {
		t.Error("Space and Return on the focused toolbar button closed the viewer")
	}
}
