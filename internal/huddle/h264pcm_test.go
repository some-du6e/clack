package huddle

import (
	"testing"

	"github.com/thesyncim/goh264"
)

// TestPCMEncoderRoundTrip: the I_PCM/P_Skip stream decodes (goh264, the
// same decoder the receive side uses) to exactly the test pattern, with a
// keyframe on request and skipped macroblocks for what didn't change.
func TestPCMEncoderRoundTrip(t *testing.T) {
	for _, sz := range [][2]int{{320, 180}, {176, 144}, {50, 34}} {
		w, h := sz[0], sz[1]
		cam := NewTestCamera(w, h, 15).(*testCamera)
		enc, err := newPCMEncoder(w, h)
		if err != nil {
			t.Fatal(err)
		}
		dec := goh264.NewDecoder()
		var keySize, pSize int
		for n := 0; n < 12; n++ {
			f := cam.render(n)
			au, err := enc.Encode(f, n == 7)
			if err != nil {
				t.Fatal(err)
			}
			isKey := hasIDR(au)
			if isKey != (n == 0 || n == 7) {
				t.Fatalf("%dx%d frame %d: keyframe %v", w, h, n, isKey)
			}
			if isKey {
				keySize = len(au)
			} else {
				pSize = len(au)
			}
			frames, err := dec.DecodeFrames(au)
			if err != nil {
				t.Fatalf("%dx%d frame %d: decode: %v", w, h, n, err)
			}
			if len(frames) != 1 {
				t.Fatalf("%dx%d frame %d: %d pictures out", w, h, n, len(frames))
			}
			p := frames[0]
			if p.Width != w || p.Height != h {
				t.Fatalf("%dx%d frame %d: decoded %dx%d", w, h, n, p.Width, p.Height)
			}
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					if got, want := p.Y[(p.CropTop+y)*p.YStride+p.CropLeft+x], f.Y[y*w+x]; got != want {
						t.Fatalf("%dx%d frame %d: Y(%d,%d) = %d, want %d", w, h, n, x, y, got, want)
					}
				}
			}
			for y := 0; y < h/2; y++ {
				for x := 0; x < w/2; x++ {
					o := (p.CropTop/2+y)*p.CStride + p.CropLeft/2 + x
					if p.Cb[o] != f.UV[y*w+2*x] || p.Cr[o] != f.UV[y*w+2*x+1] {
						t.Fatalf("%dx%d frame %d: chroma (%d,%d) = %d,%d, want %d,%d", w, h, n, x, y, p.Cb[o], p.Cr[o], f.UV[y*w+2*x], f.UV[y*w+2*x+1])
					}
				}
			}
		}
		if w >= 176 && pSize*3 > keySize {
			t.Errorf("%dx%d: P picture %d bytes vs keyframe %d: unchanged macroblocks weren't skipped", w, h, pSize, keySize)
		}
		t.Logf("%dx%d: keyframe %d bytes, P picture %d bytes", w, h, keySize, pSize)
	}
}

func TestExpGolomb(t *testing.T) {
	var b bitWriter
	for _, v := range []uint32{0, 1, 2, 3, 7, 254, 1 << 20} {
		b.reset()
		b.ue(v)
		b.trailing()
		// decode it back
		bits := b.done()
		pos := 0
		bit := func() uint32 { r := uint32(bits[pos/8]>>(7-pos%8)) & 1; pos++; return r }
		z := 0
		for bit() == 0 {
			z++
		}
		x := uint32(1)
		for i := 0; i < z; i++ {
			x = x<<1 | bit()
		}
		if x-1 != v {
			t.Errorf("ue(%d) read back %d", v, x-1)
		}
	}
}
