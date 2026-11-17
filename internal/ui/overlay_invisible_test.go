package ui

import (
	"testing"

	"gioui.org/layout"
)

type testOverlay struct{ dismissed, invisible bool }

func (o *testOverlay) Layout(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }
func (o *testOverlay) Dismissed() bool                             { return o.dismissed }
func (o *testOverlay) Invisible() bool                             { return o.invisible }

// popups' runFor keeps an invisible pump in the overlay list while a
// closed modal's request finishes: up to 3 minutes for a profile photo's
// upload (2 for Save and Remove). Such a pump must not count as an open
// overlay: it held the composer's focus after a channel switch, thread
// read marks (UserAttention) and the split view's focus and hover.
func TestInvisiblePumpIsNoOverlay(t *testing.T) {
	a := &App{focused: true}
	a.overlays = []Overlay{&testOverlay{invisible: true}}
	if a.overlayOpen() {
		t.Error("an invisible pump counts as an open overlay: the composer can't take the focus")
	}
	if !a.UserAttention() {
		t.Error("an invisible pump takes the user's attention: open threads aren't marked read")
	}
	if a.visibleOverlays() {
		t.Error("an invisible pump holds the split view's focus and hover")
	}

	a.overlays = append(a.overlays, &testOverlay{})
	if !a.overlayOpen() || a.UserAttention() || !a.visibleOverlays() {
		t.Error("a visible overlay next to the pump no longer counts")
	}
	a.overlays = []Overlay{&testOverlay{dismissed: true}}
	if a.overlayOpen() || !a.UserAttention() {
		t.Error("a dismissed overlay counts as open")
	}
	if !a.visibleOverlays() {
		t.Error("a dismissed overlay still drawing no longer holds the split view")
	}
}
