package imgcache

import (
	"context"
	"image"
	"image/color"
	"testing"
)

// An animated WebP decodes (DecodeBudget: a copy of the image, Copy
// Image) to its first frame on its canvas, as playback draws it first and
// Chromium's copyImageAt copies the frame shown: synthWebP's red over
// rows 0-5, the rest clear.
func TestDecodeAnimWebPFirstFrame(t *testing.T) {
	var img image.Image
	err := WithBudget(context.Background(), func(b *Budget) error {
		var err error
		img, err = DecodeBudget(synthWebP(0), MaxPixels, MaxSide, b)
		return err
	})
	if err != nil {
		t.Fatalf("an animated WebP doesn't decode: %v", err)
	}
	if img.Bounds().Size() != image.Pt(8, 8) {
		t.Fatalf("decoded %v, want the 8x8 canvas", img.Bounds().Size())
	}
	at := func(x, y int) color.RGBA {
		r, g, b, a := img.At(x, y).RGBA()
		return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
	}
	for p, want := range map[image.Point]color.RGBA{{0, 0}: {255, 0, 0, 255}, {7, 5}: {255, 0, 0, 255}, {3, 3}: {255, 0, 0, 255}, {0, 7}: {}, {7, 7}: {}} {
		if g := at(p.X, p.Y); !nearRGBA(g, want, 2) {
			t.Errorf("pixel %v %v, want %v", p, g, want)
		}
	}
	if _, err := decodeWithinSide(synthWebP(0), 4); err == nil {
		t.Error("an 8x8 animation decoded under a 4px side limit")
	}
}

// decodeWithinSide is DecodeBudget with a side limit of side px.
func decodeWithinSide(data []byte, side int) (image.Image, error) {
	var img image.Image
	err := WithBudget(context.Background(), func(b *Budget) error {
		var err error
		img, err = DecodeBudget(data, side*side, side, b)
		return err
	})
	return img, err
}
