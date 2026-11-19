package standin

import (
	"bytes"
	"image"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// Every stand-in decodes, at the size of the original it replaces (the
// sizes the js files ask for are the originals'). With CLACK_STANDIN_OUT
// set, the pictures are written there to look at (the site workflow's
// demo job uploads them).
func TestStandins(t *testing.T) {
	out := os.Getenv("CLACK_STANDIN_OUT")
	if out != "" {
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cells := []Cell{{R: image.Rect(0, 0, 200, 178)}, {R: image.Rect(0, 192, 200, 370), Night: true}, {R: image.Rect(0, 378, 184, 532)}, {R: image.Rect(0, 546, 184, 700)}}
	pngs := []struct {
		name string
		b    []byte
		w, h int
	}{
		{"cards", Cards(200, 716, cells), 200, 716},
		{"cards-text", Cards(220, 406, []Cell{{R: image.Rect(0, 0, 220, 46), Text: true}, {R: image.Rect(0, 100, 220, 254)}, {R: image.Rect(0, 250, 220, 404), Night: true}}), 220, 406},
		{"search", Picture(318, 280, Search), 318, 280},
		{"glass", Picture(512, 348, Glass), 512, 348},
		{"folder", Picture(364, 292, Folder), 364, 292},
		{"group", Picture(88, 125, Group), 88, 125},
		{"blocked", Picture(130, 84, Blocked), 130, 84},
		{"avatar", Avatar(72), 72, 72},
		{"app-icon", AppIcon(256), 256, 256},
	}
	for _, c := range pngs {
		img, err := png.Decode(bytes.NewReader(c.b))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if b := img.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
			t.Errorf("%s is %v, want %dx%d", c.name, b, c.w, c.h)
		}
		if out != "" {
			if err := os.WriteFile(filepath.Join(out, c.name+".png"), c.b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, dark := range []bool{false, true} {
		b := Spinner(dark)
		g, err := gif.DecodeAll(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("spinner dark=%v: %v", dark, err)
		}
		if len(g.Image) != 8 || g.Config.Width != 80 || g.Config.Height != 80 {
			t.Errorf("spinner dark=%v: %d frames, %dx%d", dark, len(g.Image), g.Config.Width, g.Config.Height)
		}
		if out != "" {
			name := "spinner.gif"
			if dark {
				name = "spinner-dark.gif"
			}
			if err := os.WriteFile(filepath.Join(out, name), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}
