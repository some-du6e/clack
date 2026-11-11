//go:build windows && cgo

package huddle

import (
	"image"
	"testing"
	"time"

	"github.com/thesyncim/goh264"
)

// TestMFEncode: Media Foundation's H.264 encoder takes the test screen
// and goh264 decodes it; a forced keyframe carries its parameter sets.
// Windows Server images without Media Foundation skip.
func TestMFEncode(t *testing.T) {
	enc, err := newMFEncoder(encOpts{W: 1280, H: 720, FPS: 15, Bps: 1_200_000, Screen: true})
	if err != nil {
		t.Skip(err)
	}
	defer enc.Close()
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	dec := goh264.NewDecoder()
	var f CameraFrame
	start := time.Now()
	pics, keys := 0, 0
	for i := 0; i < 15; i++ {
		paintTestScreen(img, i)
		rgbaToNV12(&f, img)
		f.At = start.Add(time.Duration(i) * time.Second / 15)
		au, err := enc.Encode(&f, i == 0 || i == 8)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if au == nil {
			continue
		}
		if hasIDR(au) {
			keys++
			if !hasNAL(au, 7) {
				t.Errorf("keyframe %d without an SPS", i)
			}
		}
		frames, err := dec.DecodeFrames(au)
		if err != nil {
			t.Fatalf("decode %d: %v", i, err)
		}
		for _, fr := range frames {
			pics++
			if fr.Width != 1280 || fr.Height != 720 {
				t.Errorf("decoded %dx%d", fr.Width, fr.Height)
			}
		}
	}
	if pics < 8 || keys < 2 {
		t.Errorf("%d pictures, %d keyframes", pics, keys)
	}
}
