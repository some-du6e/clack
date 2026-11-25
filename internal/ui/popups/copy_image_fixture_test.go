package popups

import (
	"errors"
	"image"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// Copy Image on the GIF picker's image (#emojibot, Karim 2026-11-24: "copying
// a image doesnt gif it right"): what the clipboard write gets, as Slack's
// desktop app's Chromium copies the <img> (clipimg/chromium.go). The
// fixture is fakeslack's GIF picker WebP: three frames, the first
// GIFPickerFirst.

const (
	gpImage = "https://media3.giphy.com/media/v1.Y2lkPTc5MGI3NjExYWJj/l0HlNQ03J5JxX6lva/giphy.webp?cid=790b7611abc&rid=giphy.webp&ct=g"
	gpQuery = "https%3A%2F%2Fmedia3.giphy.com%2Fmedia%2Fv1.Y2lkPTc5MGI3NjExYWJj%2Fl0HlNQ03J5JxX6lva%2Fgiphy.webp%3Fcid%3D790b7611abc%26rid%3Dgiphy.webp%26ct%3Dg"
	gpAlt   = "A test GIF"
)

func gpFetch(u string) ([]byte, error) {
	if u == gpImage {
		return fakeslack.GIFPickerWebP(220, 260), nil
	}
	return nil, errors.New("http 404")
}

// firstFrame fails unless img is the fixture's first frame: every pixel
// GIFPickerFirst.
func firstFrame(t *testing.T, img image.Image, what string) {
	t.Helper()
	if img.Bounds().Size() != image.Pt(220, 260) {
		t.Fatalf("%s copied a %v image, want the GIF's 220x260", what, img.Bounds().Size())
	}
	want := fakeslack.GIFPickerFirst
	for _, p := range []image.Point{{0, 0}, {110, 130}, {219, 259}} {
		r, g, b, a := img.At(p.X, p.Y).RGBA()
		if uint8(r>>8) != want.R || uint8(g>>8) != want.G || uint8(b>>8) != want.B || uint8(a>>8) != want.A {
			t.Fatalf("%s: pixel %v is %v, want the first frame's %v", what, p, img.At(p.X, p.Y), want)
		}
	}
}
