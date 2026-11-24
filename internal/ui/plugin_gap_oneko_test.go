package ui

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/gif"
	"math"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestPluginGapOneko: Slick's oneko from the Library, in the whole client.
// Its cat is a 32px div in document.body (position: fixed, pointer-events:
// none, image-rendering: pixelated) whose background-image is the sprite
// sheet, a data: GIF Slick's build inlined, a frame picked by
// background-position.
//
//   - The cat runs to the pointer: it comes to rest within 48px of it, as
//     oneko idles.
//   - It's drawn there: its box isn't what's under it (the frame with the
//     cat hidden), and it shows one of the sprite's 32px frames, pixel for
//     pixel where the frame is opaque. slackc decodes the sprite from the
//     URL's own text, as Chromium does.
func TestPluginGapOneko(t *testing.T) {
	e := gapStart(t, pluginlib.Slick, "oneko", nil)
	if s := e.scale; s != 0 && s != 1 {
		t.Fatalf("the bench runs at %v px per dp; this test reads the cat at 1", s)
	}
	type cat struct {
		Left, Top float64
		Bg        string
		In        bool
	}
	read := func() (c cat) {
		s := e.js(`(() => {
  const el = document.getElementById('oneko');
  if (!el) return '{}';
  return JSON.stringify({Left: parseFloat(el.style.left), Top: parseFloat(el.style.top), Bg: el.style.backgroundImage, In: document.body.contains(el)});
})()`)
		_ = json.Unmarshal([]byte(s), &c)
		return c
	}
	e.until("the cat in the document", 20*time.Second, func() bool { return read().In })

	// the pointer over the message list: the cat runs to it and rests
	p := e.listPoint()
	e.move(f32.Pt(float32(p.X), float32(p.Y)))
	near := func(c cat) bool { // its left and top are its centre less 16
		return math.Hypot(c.Left+16-float64(p.X), c.Top+16-float64(p.Y)) < 48
	}
	var c cat
	t.Logf("the pointer at %v; the cat at %.1f,%.1f", p, read().Left, read().Top)
	e.until("the cat at rest by the pointer", 30*time.Second, func() bool {
		c = read()
		if !near(c) {
			return false
		}
		e.settle(300 * time.Millisecond)
		c2 := read()
		return c2.Left == c.Left && c2.Top == c.Top
	})

	sprite := onekoSprite(t, c.Bg)
	// the frame with the cat, then with it hidden; the cat stays put
	var with, without *image.RGBA
	for try := 0; ; try++ {
		before := read()
		e.settle(300 * time.Millisecond)
		with = pixeltest.Render(t, e.size.X, e.size.Y, &e.ops)
		if after := read(); after.Left == before.Left && after.Top == before.Top {
			c = after
			break
		}
		if try == 5 {
			t.Fatal("the cat kept moving while it was drawn")
		}
	}
	e.js(`document.getElementById('oneko').style.display = 'none'; 0`)
	e.settle(500 * time.Millisecond)
	without = pixeltest.Render(t, e.size.X, e.size.Y, &e.ops)
	e.js(`document.getElementById('oneko').style.display = ''; 0`)

	// in the frame's px: its CSS px (1 a px here), under Gio's own title
	// bar when there is one
	at := image.Pt(int(math.Floor(c.Left)), int(math.Floor(c.Top))).Add(e.deco)
	box := image.Rectangle{Min: at, Max: at.Add(image.Pt(32, 32))}
	changed := 0
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if with.RGBAAt(x, y) != without.RGBAAt(x, y) {
				changed++
			}
		}
	}
	if changed == 0 {
		t.Fatalf("the cat's box %v is the same with the cat hidden: the cat isn't drawn (its background-image %.40s…)", box, c.Bg)
	}
	best, bestAt := 0.0, image.Point{}
	for fy := 0; fy+32 <= sprite.Bounds().Dy(); fy += 32 {
		for fx := 0; fx+32 <= sprite.Bounds().Dx(); fx += 32 {
			for oy := -1; oy <= 1; oy++ { // its left and top aren't whole px
				for ox := -1; ox <= 1; ox++ {
					if r := frameMatch(with, box.Min.Add(image.Pt(ox, oy)), sprite, image.Pt(fx, fy)); r > best {
						best, bestAt = r, image.Pt(fx, fy)
					}
				}
			}
		}
	}
	t.Logf("the cat's box %v: %d px differ from the frame without it; best match the sprite's frame at %v (%.0f%% of its opaque px)", box, changed, bestAt, best*100)
	if best < 0.9 {
		t.Errorf("the cat's box shows none of the sprite's frames: at best %.0f%% of a frame's opaque px", best*100)
	}
}

// onekoSprite decodes the sprite sheet from the cat's background-image,
// url("data:image/gif;base64,…").
func onekoSprite(t *testing.T, bg string) image.Image {
	t.Helper()
	u := strings.TrimSuffix(strings.TrimPrefix(bg, "url("), ")")
	u = strings.Trim(u, `"'`)
	_, b64, ok := strings.Cut(u, ";base64,")
	if !strings.HasPrefix(u, "data:image/gif") || !ok {
		t.Fatalf("the cat's background-image isn't a base64 data: GIF: %.60s", bg)
	}
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	img, err := gif.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if sz := img.Bounds().Size(); sz.X%32 != 0 || sz.Y%32 != 0 {
		t.Fatalf("the sprite is %v, not whole 32px frames", sz)
	}
	return img
}

// frameMatch is the share of the sprite frame at f's opaque px that the
// frame drew at at, within a small difference per channel.
func frameMatch(drawn *image.RGBA, at image.Point, sprite image.Image, f image.Point) float64 {
	match, opaque := 0, 0
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			r, g, b, a := sprite.At(f.X+x, f.Y+y).RGBA()
			if a != 0xffff {
				continue
			}
			opaque++
			d := drawn.RGBAAt(at.X+x, at.Y+y)
			if near8(d.R, r) && near8(d.G, g) && near8(d.B, b) {
				match++
			}
		}
	}
	if opaque == 0 {
		return 0
	}
	return float64(match) / float64(opaque)
}

func near8(v uint8, c uint32) bool {
	d := int(v) - int(c>>8)
	return d >= -8 && d <= 8
}
