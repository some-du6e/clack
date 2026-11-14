package inspect

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// The pasted image: the shot as it was, the element outlined (a halo, the
// outline's colour, a faint wash inside), and under a band of the same
// colour the report's panel with its text.
func TestComposeOutlineAndPanel(t *testing.T) {
	shot := image.NewRGBA(image.Rect(0, 0, 400, 240))
	draw.Draw(shot, shot.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	rect := image.Rect(100, 50, 200, 100)
	img, err := Compose(shot, rect, 1, testReport().Lines())
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 400 || b.Dy() <= 240+100 {
		t.Fatalf("composed size %v: want 400 wide, the shot plus a panel", b)
	}
	at := func(x, y int) color.RGBA { return img.RGBAAt(x, y) }
	oc := color.RGBA{R: outlineCol.R, G: outlineCol.G, B: outlineCol.B, A: 0xff}
	for _, p := range []image.Point{{98, 70}, {99, 70}, {150, 48}, {201, 70}, {150, 101}} {
		if c := at(p.X, p.Y); c != oc {
			t.Errorf("outline pixel %v is %v, want %v", p, c, oc)
		}
	}
	if c := at(150, 75); c == oc || c.G > 245 || c.R != 0xff {
		t.Errorf("inside the element %v: want the outline's faint wash", c)
	}
	if c := at(20, 20); c != (color.RGBA{0xff, 0xff, 0xff, 0xff}) {
		t.Errorf("away from the element the shot changed: %v", c)
	}
	if c := at(10, 241); c != oc {
		t.Errorf("the band under the shot is %v, want the outline's colour", c)
	}
	pb := color.RGBA{R: panelBg.R, G: panelBg.G, B: panelBg.B, A: 0xff}
	if c := at(2, img.Bounds().Max.Y-2); c != pb {
		t.Errorf("the panel's background %v, want %v", c, pb)
	}
	ink := 0
	for y := 244; y < img.Bounds().Max.Y; y++ {
		for x := 0; x < 400; x++ {
			if c := at(x, y); int(c.R)+int(c.G)+int(c.B) > 500 {
				ink++
			}
		}
	}
	if ink < 500 {
		t.Errorf("the panel has %d light pixels: no text drawn", ink)
	}
	if dir := os.Getenv("INSPECT_SHOTS_DIR"); dir != "" {
		f, err := os.Create(filepath.Join(dir, "compose-synthetic.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
}

// Long values wrap inside the panel's value column, a word wider than the
// column breaks inside it.
func TestWrap(t *testing.T) {
	if err := loadFonts(); err != nil {
		t.Fatal(err)
	}
	lines := []Line{{"classes", "div.c-message_kit__reaction_bar < div.c-message_kit__message < div.c-virtual_list__item < div.c-message_list.c-virtual_list < div.p-message_pane"},
		{"element", "button.averyveryveryveryveryveryveryveryveryverylongclassnamewithoutanyspacesatallthatkeepsgoing"}}
	shot := image.NewRGBA(image.Rect(0, 0, 360, 40))
	img, err := Compose(shot, image.Rect(0, 0, 10, 10), 1, lines)
	if err != nil {
		t.Fatal(err)
	}
	// two keys, each value on two lines or more: taller than four rows
	if h := img.Bounds().Dy() - 40; h < 2*16+4*17 {
		t.Errorf("panel %dpx tall: the long values didn't wrap", h)
	}
}
