package ui

import "testing"

// The App counts the times the viewer's attention went (AttentionLost,
// for the views that compare the attention of the frames they draw): the
// window losing the focus (a ConfigEvent, a hide to the notification
// area), and an overlay that counts opening over the focused window. A
// loss while unfocused, an overlay over an unfocused window or an
// invisible one (the popups pump) counts nothing.
func TestAttentionLostCounts(t *testing.T) {
	a := &App{}
	check := func(what string, want uint64) {
		t.Helper()
		if got := a.AttentionLost(); got != want {
			t.Errorf("%s: AttentionLost %d, want %d", what, got, want)
		}
	}
	a.setFocused(true)
	check("the window focused", 0)
	a.setFocused(false)
	check("the focus lost", 1)
	a.setFocused(false)
	check("a loss while unfocused", 1)
	a.ShowOverlay(&testOverlay{})
	check("an overlay over the unfocused window", 1)
	a.overlays, a.overlayPane = nil, nil
	a.setFocused(true)
	a.ShowOverlay(&testOverlay{invisible: true})
	check("an invisible overlay", 1)
	a.ShowOverlay(&testOverlay{})
	check("an overlay over the focused window", 2)
	a.overlays, a.overlayPane = nil, nil
	a.loseFocus() // closeToTray's hide
	check("a hide to the notification area", 3)
}
