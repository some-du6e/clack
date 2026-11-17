package exif

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 40), G: uint8(y * 40), B: 0x80, A: 0xff})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// Every orientation reads back from the segment WithOrientation writes, in
// either byte order, and the JPEG still decodes; anything malformed is 1.
func TestOrientation(t *testing.T) {
	plain := testJPEG(t, 8, 4)
	if o := Orientation(plain); o != 1 {
		t.Errorf("plain JPEG: %d", o)
	}
	for o := 1; o <= 8; o++ {
		data := WithOrientation(plain, o)
		if got := Orientation(data); got != o {
			t.Errorf("orientation %d read as %d", o, got)
		}
		if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
			t.Errorf("orientation %d: the JPEG no longer decodes: %v", o, err)
		}
	}
	// little-endian ("II") TIFF, orientation 6
	ii := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 0x01, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	seg := append([]byte("Exif\x00\x00"), ii...)
	app1 := append([]byte{0xff, 0xe1, byte((len(seg) + 2) >> 8), byte(len(seg) + 2)}, seg...)
	le := append(append(append([]byte{}, plain[:2]...), app1...), plain[2:]...)
	if got := Orientation(le); got != 6 {
		t.Errorf("little-endian 6 read as %d", got)
	}
	for name, data := range map[string][]byte{
		"empty":        nil,
		"png":          []byte("\x89PNG\r\n\x1a\n...."),
		"truncated":    WithOrientation(plain, 6)[:10],
		"out of range": WithOrientation(plain, 9),
		"zero":         WithOrientation(plain, 0),
	} {
		if got := Orientation(data); got != 1 {
			t.Errorf("%s: %d, want 1", name, got)
		}
	}
}

// Source turns the stored image as each orientation shows it: 6 brings the
// bottom-left corner to the top-left (a quarter turn clockwise), 8 the
// top-right, 3 the bottom-right, 2 mirrors.
func TestSource(t *testing.T) {
	const w, h = 3, 2 // stored pixels are numbered y*w+x
	for _, c := range []struct {
		o     int
		shown [][]int // rows of the shown image
	}{
		{1, [][]int{{0, 1, 2}, {3, 4, 5}}},
		{2, [][]int{{2, 1, 0}, {5, 4, 3}}},
		{3, [][]int{{5, 4, 3}, {2, 1, 0}}},
		{4, [][]int{{3, 4, 5}, {0, 1, 2}}},
		{5, [][]int{{0, 3}, {1, 4}, {2, 5}}},
		{6, [][]int{{3, 0}, {4, 1}, {5, 2}}},
		{7, [][]int{{5, 2}, {4, 1}, {3, 0}}},
		{8, [][]int{{2, 5}, {1, 4}, {0, 3}}},
	} {
		sw, sh := w, h
		if Swaps(c.o) {
			sw, sh = h, w
		}
		if len(c.shown) != sh || len(c.shown[0]) != sw {
			t.Fatalf("orientation %d: table is %d×%d, want %d×%d", c.o, len(c.shown[0]), len(c.shown), sw, sh)
		}
		for y := 0; y < sh; y++ {
			for x := 0; x < sw; x++ {
				sx, sy := Source(c.o, w, h, x, y)
				if got := sy*w + sx; got != c.shown[y][x] {
					t.Errorf("orientation %d: shown (%d,%d) is stored pixel %d, want %d", c.o, x, y, got, c.shown[y][x])
				}
			}
		}
	}
}
