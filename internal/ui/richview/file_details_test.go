package richview

import (
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// FileDetails' preview (Options.FileDetails, eg5B renderPreview): an
// image is MessageImageFile at its thumb's size (thumb_480, the caller's
// Thumb), shrunk to the width with its aspect kept, never upscaled, in a
// 1px bordered box at radius 8, with no FileActions bar (showActions off);
// the same file in a message is the gallery's tile with its bar.
func TestFileDetailsImagePreview(t *testing.T) {
	st := newTestStyle()
	f := &rt.File{ID: "F0DETAILS01", Name: "photo.png", IsImage: true, OrigW: 1600, OrigH: 900,
		Thumb: &rt.Image{URL: "https://files.example/photo_480.png", W: 480, H: 270}}
	doc := rt.Doc{Blocks: []rt.Block{{Kind: rt.FileBlock, File: f}}}
	for _, c := range []struct {
		w            int
		wantW, wantH int
	}{
		{600, 480, 270}, // wider than the thumb: its own size
		{268, 268, 150}, // a 300px sidebar's content: fitted, 270*268/480
	} {
		v := layoutAt(st, doc, Options{FileDetails: true, CompactFiles: true, NoOuterMargins: true}, c.w)
		if len(v.d.imgs) != 1 || v.d.imgs[0].url != f.Thumb.URL {
			t.Fatalf("width %d: images %+v, want the thumb", c.w, v.d.imgs)
		}
		if r := v.d.imgs[0].r; r.Min.X != 0 || r.Dx() != c.wantW || r.Dy() != c.wantH {
			t.Errorf("width %d: image %v, want %dx%d at the left", c.w, r, c.wantW, c.wantH)
		}
		if v.d.size.Y != c.wantH {
			t.Errorf("width %d: view %v tall, want the image's %d", c.w, v.d.size, c.wantH)
		}
		if len(v.d.bars) != 0 {
			t.Errorf("width %d: %d FileActions bars, want none (showActions false)", c.w, len(v.d.bars))
		}
		border := false
		for _, b := range v.d.boxes {
			if b.kind == bBorder && b.r == v.d.imgs[0].r && b.radius == 8 && b.bw == 1 {
				border = true
			}
		}
		if !border {
			t.Errorf("width %d: no 1px radius-8 border round the image (.p-file_image_thumbnail__wrapper)", c.w)
		}
	}
	if v := layoutAt(st, doc, Options{CompactFiles: true}, 600); len(v.d.bars) != 1 {
		t.Errorf("in a message the image has %d FileActions bars, want its gallery bar", len(v.d.bars))
	}
}
