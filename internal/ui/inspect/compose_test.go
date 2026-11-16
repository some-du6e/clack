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

// The pasted image: the shot (the element and its margin), the element
// outlined (a halo, the outline's colour, a faint wash inside), and under
// a band of the same colour the report's panel with its text.
func TestComposeOutlineAndPanel(t *testing.T) {
	shot := image.NewRGBA(image.Rect(0, 0, 640, 240))
	draw.Draw(shot, shot.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	rect := image.Rect(100, 50, 200, 100)
	img, err := Compose(shot, rect, 1, testReport().Lines())
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 640 || b.Dy() <= 240+100 {
		t.Fatalf("composed size %v: want 640 wide (the shot, wider than the panel's minimum), the shot plus a panel", b)
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
		for x := 0; x < 640; x++ {
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

// A small element's image is as wide as the panel's minimum, no wider:
// the shot at its own size, centred, panel colour beside it, and the
// panel under it.
func TestComposeSmallElement(t *testing.T) {
	shot := image.NewRGBA(image.Rect(0, 0, 100, 70))
	draw.Draw(shot, shot.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	img, err := Compose(shot, image.Rect(24, 24, 76, 46), 1, testReport().Lines())
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() != PanelMinWidth || b.Dy() <= 70 {
		t.Fatalf("composed size %v: want the panel's minimum width %d, the shot plus a panel", b, PanelMinWidth)
	}
	white, first := 0, -1
	for x := 0; x < b.Dx(); x++ {
		if img.RGBAAt(x, 5) == (color.RGBA{0xff, 0xff, 0xff, 0xff}) {
			white++
			if first < 0 {
				first = x
			}
		}
	}
	if white != 100 || first != (PanelMinWidth-100)/2 {
		t.Errorf("the shot's row: %d px from x %d, want its 100 px centred at %d", white, first, (PanelMinWidth-100)/2)
	}
	pb := color.RGBA{R: panelBg.R, G: panelBg.G, B: panelBg.B, A: 0xff}
	if c := img.RGBAAt(5, 5); c != pb {
		t.Errorf("beside the shot %v, want the panel's colour", c)
	}
}

// The margin around the element is blurred past reading (what the shot
// caught of the neighbours: 1 px stripes, sharper than any text), the
// element stays sharp.
func TestComposeBlursTheMargin(t *testing.T) {
	shot := image.NewRGBA(image.Rect(0, 0, 640, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 640; x++ {
			c := color.RGBA{A: 0xff}
			if x%2 == 0 {
				c = color.RGBA{0xff, 0xff, 0xff, 0xff}
			}
			shot.SetRGBA(x, y, c)
		}
	}
	elem := image.Rect(200, 40, 280, 80)
	img, err := Compose(shot, elem, 1, testReport().Lines())
	if err != nil {
		t.Fatal(err)
	}
	luma := func(x, y int) int { c := img.RGBAAt(x, y); return (int(c.R) + int(c.G) + int(c.B)) / 3 }
	abs := func(v int) int { return max(v, -v) }
	sharp := 0
	for y := 0; y < 120; y++ {
		for x := 0; x+1 < 640; x++ {
			p := image.Pt(x, y)
			if p.In(elem.Inset(-12)) {
				continue // the element, its outline, and the blur's edge next to it
			}
			if abs(luma(x, y)-luma(x+1, y)) > 12 {
				sharp++
			}
		}
	}
	if sharp > 0 {
		t.Errorf("%d margin px still differ from their neighbour by more than 12: the margin isn't blurred", sharp)
	}
	if d := abs(luma(240, 60) - luma(241, 60)); d < 150 {
		t.Errorf("inside the element neighbours differ by %d: the element was blurred too", d)
	}
}

// The crop: the element and CropMargin dp around it, inside the window,
// at ShotDensity px per dp (never below the window's own), so a reaction
// pill's image is a sliver of the window's width.
func TestCropFor(t *testing.T) {
	win := image.Pt(2880, 1800) // 1440x900 dp at 2x
	pill := image.Rect(1000, 800, 1100, 848)
	crop, k := cropFor(pill, win, 2)
	if want := pill.Inset(-2 * CropMargin); crop != want || k != 2 {
		t.Fatalf("pill crop %v at %g, want %v at 2", crop, k, want)
	}
	if crop.Dx()*10 > win.X {
		t.Errorf("the pill's crop is %d px of the window's %d: more than the element", crop.Dx(), win.X)
	}
	if got, want := inShot(pill, crop, k), image.Rect(96, 96, 296, 192); got != want {
		t.Errorf("the pill in its shot %v, want %v", got, want)
	}
	if got := ShotSize(crop, k); got != image.Pt(392, 288) {
		t.Errorf("the pill's shot is %v, want 392x288", got)
	}
	// at 1x the crop is drawn 4 times as dense
	if _, k := cropFor(image.Rect(500, 400, 550, 424), image.Pt(1440, 900), 1); k != 4 {
		t.Errorf("1x density %g, want 4", k)
	}
	// at the window's edge: clamped
	if crop, _ := cropFor(image.Rect(0, 10, 40, 40), win, 2); crop != image.Rect(0, 0, 88, 88) {
		t.Errorf("corner crop %v, want 0,0-88,88", crop)
	}
	// the whole window: no denser than fits MaxShotSide, never below 1
	if crop, k := cropFor(image.Rectangle{Max: win}, win, 2); crop != (image.Rectangle{Max: win}) || k != max(MaxShotSide/float32(2880), 1) {
		t.Errorf("window crop %v at %g", crop, k)
	}
	if _, k := cropFor(image.Rectangle{Max: image.Pt(5120, 2880)}, image.Pt(5120, 2880), 2); k != 1 {
		t.Errorf("a 5K window's density %g, want 1", k)
	}
	if crop, _ := cropFor(image.Rect(3000, 0, 3100, 40), win, 2); !crop.Empty() {
		t.Errorf("an element outside the window cropped to %v", crop)
	}
}

// A rect the user blurred in the review is baked into the copy and
// keeps nothing of what was under it: two shots that differ only inside
// the rect (one has "text" there, one doesn't) compose to the very same
// image, the blurred margin next to it included, while away from it the
// element and its margin are as without the rect.
func TestComposeRedactKeepsNothingInside(t *testing.T) {
	elem := image.Rect(60, 40, 520, 200)
	mark := image.Rect(60, 70, 300, 130) // inside the element, at its edge: someone's name
	a := noiseShot(image.Pt(600, 240), 1)
	b := image.NewRGBA(a.Bounds())
	draw.Draw(b, b.Bounds(), a, image.Point{}, draw.Src)
	draw.Draw(b, mark, image.NewUniform(color.RGBA{0x20, 0x40, 0x60, 0xff}), image.Point{}, draw.Src)
	lines := testReport().Lines()
	ia, err := Compose(a, elem, 1, lines, mark)
	if err != nil {
		t.Fatal(err)
	}
	ib, err := Compose(b, elem, 1, lines, mark)
	if err != nil {
		t.Fatal(err)
	}
	if ia.Bounds() != ib.Bounds() {
		t.Fatalf("sizes %v and %v", ia.Bounds(), ib.Bounds())
	}
	at := image.Pt((ia.Bounds().Dx()-600)/2, 0)
	for y := 0; y < 240; y++ {
		for x := 0; x < 600; x++ {
			if p, q := ia.RGBAAt(x+at.X, y+at.Y), ib.RGBAAt(x+at.X, y+at.Y); p != q {
				t.Fatalf("at %d,%d the image shows %v over one shot and %v over the other: what was under the rect comes through", x, y, p, q)
			}
		}
	}
	plain, err := Compose(a, elem, 1, lines)
	if err != nil {
		t.Fatal(err)
	}
	changed := 0
	for y := 0; y < 240; y++ {
		for x := 0; x < 600; x++ {
			p := image.Pt(x, y)
			if p.In(mark.Inset(-20)) {
				continue // the rect, and the margin's blur next to it
			}
			if ia.RGBAAt(x+at.X, y+at.Y) != plain.RGBAAt(x+at.X, y+at.Y) {
				changed++
			}
		}
	}
	if changed > 0 {
		t.Errorf("%d px outside the blurred rect changed", changed)
	}
	// and it is blurred: no sharp edges left in it (the noise had them
	// everywhere)
	sharp := 0
	for y := mark.Min.Y + 2; y < mark.Max.Y-2; y++ {
		for x := mark.Min.X + 2; x < mark.Max.X-3; x++ {
			p, q := ia.RGBAAt(x+at.X, y+at.Y), ia.RGBAAt(x+1+at.X, y+at.Y)
			if absI(int(p.R)-int(q.R)) > 8 {
				sharp++
			}
		}
	}
	if sharp > 0 {
		t.Errorf("%d px in the blurred rect still differ from their neighbour by more than 8", sharp)
	}
}

// Rects over rects: each is filled from what's outside all of them, so
// nothing under one leaks into another; rects past the shot are clipped;
// a rect over all of it is one flat colour.
func TestRedactOverlapAndEdges(t *testing.T) {
	a := noiseShot(image.Pt(200, 120), 2)
	b := noiseShot(image.Pt(200, 120), 3)
	draw.Draw(b, image.Rect(0, 0, 200, 120), a, image.Point{}, draw.Src)
	draw.Draw(b, image.Rect(40, 30, 130, 90), noiseShot(image.Pt(90, 60), 9), image.Point{}, draw.Src)
	marks := []image.Rectangle{image.Rect(40, 30, 100, 90), image.Rect(80, 20, 130, 100)}
	Redact(a, a.Bounds(), marks, 1)
	Redact(b, b.Bounds(), marks, 1)
	for y := 0; y < 120; y++ {
		for x := 0; x < 200; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				t.Fatalf("at %d,%d the overlapping rects show what was under them", x, y)
			}
		}
	}
	c := noiseShot(image.Pt(50, 50), 4)
	Redact(c, c.Bounds(), []image.Rectangle{image.Rect(-10, -10, 80, 80)}, 1)
	if p, q := c.RGBAAt(0, 0), c.RGBAAt(49, 49); p != q || p != (color.RGBA{0x80, 0x80, 0x80, 0xff}) {
		t.Errorf("a rect over the whole shot: %v and %v, want flat grey", p, q)
	}
}

// A dragged region's shot is all element: drawn as it was, no blur and
// no outline.
func TestComposeRegionAsIs(t *testing.T) {
	shot := noiseShot(image.Pt(700, 160), 5)
	img, err := Compose(shot, shot.Bounds(), 1, testReport().Lines())
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 160; y++ {
		for x := 0; x < 700; x++ {
			if img.RGBAAt(x, y) != shot.RGBAAt(x, y) {
				t.Fatalf("at %d,%d the region's image differs from its shot", x, y)
			}
		}
	}
}
