package demo

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/emoji"
)

// TestEmojiSheet draws the first 480 standard emoji (the picker's order)
// as the demo shows them, into one sheet (CLACK_DEMO_SHOTS only), and
// checks every one draws.
func TestEmojiSheet(t *testing.T) {
	list := emoji.StandardList()
	if len(list) > 480 {
		list = list[:480]
	}
	const px, cols = 48, 24
	rows := (len(list) + cols - 1) / cols
	sheet := image.NewRGBA(image.Rect(0, 0, cols*(px+4), rows*(px+4)))
	draw.Draw(sheet, sheet.Bounds(), image.White, image.Point{}, draw.Src)
	for i, e := range list {
		u, ok := emoji.StandardURL(e.Names[0], "google", "large")
		if !ok {
			continue
		}
		b, ok := StandardEmoji(strings.Replace(u, ".png", "@2x.png", 1))
		if !ok {
			b, ok = StandardEmoji(u)
		}
		if !ok {
			t.Errorf("%s: not drawn", e.Names[0])
			continue
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("%s: %v", e.Names[0], err)
		}
		x, y := (i%cols)*(px+4), (i/cols)*(px+4)
		r := image.Rect(x, y, x+px, y+px)
		// nearest-neighbour down to px (the drawings are 64px)
		ib := img.Bounds()
		for yy := 0; yy < px; yy++ {
			for xx := 0; xx < px; xx++ {
				sheet.Set(r.Min.X+xx, r.Min.Y+yy, img.At(ib.Min.X+xx*ib.Dx()/px, ib.Min.Y+yy*ib.Dy()/px))
			}
		}
	}
	dir := os.Getenv("CLACK_DEMO_SHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, "demo-emoji-sheet.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, sheet); err != nil {
		t.Fatal(err)
	}
}
