package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/key"
)

// A video in the media viewer after one Tab: M still mutes (Slack binds
// it on the document), and Space goes to the focused control rather than
// closing the viewer (Slack's Space skips a BUTTON target, so the button
// takes it). Clack's first Tab stop was the header's close: Space closed
// the viewer and M did nothing.
func TestVideoViewerKeysAfterTab(t *testing.T) {
	viewerVideos(t)
	e := filesEnv(t, image.Pt(1440, 900), false)
	vvOpenGallery(t, e)
	e.r.MoveFocus(key.FocusForward) // Tab
	for range 3 {
		e.frame()
	}
	st, _ := viewerState(e)
	muted := st.muted
	e.key("M", 0)
	e.until("M toggles the mute with a control focused", 5*time.Second, func() bool { st, _ := viewerState(e); return st.muted != muted })
	e.key(key.NameSpace, 0)
	e.settle(300 * time.Millisecond)
	if lb := vvLightbox(e); lb == nil {
		t.Error("Space with a control focused closed the viewer")
	}
}
