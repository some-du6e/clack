package tflite_test

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/some-du6e/clack/internal/huddle/bgblur"
)

// The fixed inputs of the reference test, 256×144 RGB PNGs:
// ref-person.png (bgblur.SyntheticPerson) and ref-noise.png (xorshift
// noise over a gradient). TFLITE_WRITE_REF=1 rewrites them.
func refInputs() map[string]*image.RGBA {
	person, _ := bgblur.SyntheticPerson(256, 144)
	noise := image.NewRGBA(image.Rect(0, 0, 256, 144))
	s := uint32(2463534242)
	for y := 0; y < 144; y++ {
		for x := 0; x < 256; x++ {
			var c [3]uint8
			for k := range c {
				s ^= s << 13
				s ^= s >> 17
				s ^= s << 5
				c[k] = uint8((int(s>>24) + x + y*k) / 2)
			}
			noise.SetRGBA(x, y, color.RGBA{c[0], c[1], c[2], 255})
		}
	}
	return map[string]*image.RGBA{"ref-person": person, "ref-noise": noise}
}

func TestWriteRefInputs(t *testing.T) {
	if os.Getenv("TFLITE_WRITE_REF") == "" {
		t.Skip("TFLITE_WRITE_REF not set")
	}
	for name, img := range refInputs() {
		f, err := os.Create(filepath.Join("testdata", name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
