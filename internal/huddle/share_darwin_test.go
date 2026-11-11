//go:build darwin && cgo

package huddle

import (
	"errors"
	"testing"
)

// TestSCKList lists screens and windows through ScreenCaptureKit. CI's
// macOS runners don't let a test binary record the screen (TCC), so there
// it checks the answer is the permission error Slack's banner is for.
func TestSCKList(t *testing.T) {
	b := scShares{}
	if !b.supported() {
		t.Skip("no ScreenCaptureKit (macOS < 12.3)")
	}
	l, err := b.List(ShareScreen, ShareThumbSize)
	if errors.Is(err, ErrShareDenied) {
		if ScreenAccessAllowed() {
			t.Error("listing denied while CGPreflightScreenCaptureAccess says yes")
		}
		t.Skipf("Screen Recording isn't allowed here: %v", err)
	}
	if err != nil || len(l) == 0 {
		t.Fatalf("screens: %v %+v", err, l)
	}
	t.Logf("screen %s %q %v thumb %v", l[0].ID, l[0].Name, l[0].Bounds, l[0].Thumb != nil)
	if _, err := b.List(ShareWindow, ShareThumbSize); err != nil {
		t.Errorf("windows: %v", err)
	}
}
