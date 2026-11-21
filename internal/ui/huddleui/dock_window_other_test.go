//go:build !windows

package huddleui

import "testing"

// closeHuddleWindowLikeYou closes the huddle window through its own close
// (Gio's ActionClose, as the X11 and Wayland close requests end up).
func closeHuddleWindowLikeYou(t *testing.T, w *window) {
	t.Helper()
	w.close()
}
