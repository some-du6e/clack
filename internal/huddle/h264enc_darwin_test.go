//go:build darwin && cgo

package huddle

import (
	"bytes"
	"testing"
	"time"

	"github.com/thesyncim/goh264"
)

// TestVTEncode: VideoToolbox encodes synthetic NV12 frames headless (no
// camera, no TCC prompt) into baseline H.264 with SPS/PPS on keyframes, a
// keyframe on request, and pictures goh264 (the receive side) decodes.
func TestVTEncode(t *testing.T) {
	const w, h = 640, 360
	enc, err := newVTEncoder(encOpts{W: w, H: h, FPS: 15, Bps: 800_000})
	if err != nil {
		t.Fatalf("VideoToolbox: %v", err)
	}
	defer enc.Close()
	cam := NewTestCamera(w, h, 15).(*testCamera)
	dec := goh264.NewDecoder()
	start := time.Now()
	var out, decoded, keys int
	var keyBytes, pBytes int
	for n := 0; n < 45; n++ {
		f := cam.render(n)
		f.At = start.Add(time.Duration(n) * time.Second / 15)
		au, err := enc.Encode(f, n == 30)
		if err != nil {
			t.Fatalf("frame %d: %v", n, err)
		}
		if au == nil {
			continue
		}
		out++
		if !bytes.HasPrefix(au, []byte{0, 0, 0, 1}) {
			t.Fatalf("frame %d: not Annex B: % x", n, au[:min(8, len(au))])
		}
		if hasIDR(au) {
			keys++
			keyBytes = len(au)
			if au[4]&0x1f != 7 {
				t.Errorf("frame %d: keyframe doesn't start with an SPS (NAL type %d)", n, au[4]&0x1f)
			} else if au[5] != 66 {
				t.Errorf("frame %d: profile_idc %d, want 66 (baseline)", n, au[5])
			}
		} else {
			pBytes = len(au)
		}
		if (n == 0 || n == 30) && !hasIDR(au) {
			t.Errorf("frame %d: no keyframe", n)
		}
		frames, err := dec.DecodeFrames(au)
		if err != nil {
			t.Fatalf("frame %d: decode: %v", n, err)
		}
		for _, p := range frames {
			decoded++
			if p.Width != w || p.Height != h {
				t.Fatalf("decoded %dx%d", p.Width, p.Height)
			}
			// the white bar on the left survives encoding
			if y := p.Y[(p.CropTop+40)*p.YStride+p.CropLeft+10]; y < 200 {
				t.Errorf("frame %d: luma %d in the white bar", n, y)
			}
		}
	}
	t.Logf("VideoToolbox: %d/45 frames out, %d decoded, %d keyframes; keyframe %d bytes, P %d bytes", out, decoded, keys, keyBytes, pBytes)
	if out < 40 || decoded < 40 {
		t.Errorf("only %d frames out, %d decoded", out, decoded)
	}
	if keys < 2 {
		t.Errorf("%d keyframes, want the first and the forced one", keys)
	}
	enc.SetBitrate(300_000)
}
