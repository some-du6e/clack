//go:build windows

package huddle

import (
	"image"
	"testing"
	"time"
)

// TestGDIShares lists the runner's monitors and windows and grabs the
// first monitor (GitHub's Windows runners have a desktop session).
func TestGDIShares(t *testing.T) {
	b := gdiShares{}
	l, err := b.List(ShareScreen, ShareThumbSize)
	if err != nil || len(l) == 0 {
		t.Skipf("no monitors: %v", err)
	}
	t.Logf("screens: %+v", l[0].Bounds)
	if l[0].Thumb == nil || l[0].Thumb.Bounds().Dx() > 346 || l[0].Thumb.Bounds().Dy() > 260 {
		t.Errorf("thumbnail %v", l[0].Thumb)
	}
	w, err := b.List(ShareWindow, ShareThumbSize)
	t.Logf("%d windows (%v)", len(w), err)
	c, err := b.Open(l[0], false, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan image.Point, 1)
	if err := c.Start(func(f *CameraFrame) {
		select {
		case got <- image.Pt(f.W, f.H):
		default:
		}
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	select {
	case sz := <-got:
		ww, hh := capSize(l[0].Bounds.Dx(), l[0].Bounds.Dy())
		if sz != image.Pt(ww, hh) {
			t.Errorf("frame %v, want %dx%d", sz, ww, hh)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no frame")
	}
	// the border window comes and goes without taking anything down
	showBorder(l[0])
	time.Sleep(300 * time.Millisecond)
	hideBorder()
}
