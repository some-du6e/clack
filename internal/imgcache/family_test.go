package imgcache

// Karim, 2026-11-23 (α.146): opening a bot's App pane from its message,
// "the pfp takes a bit to load. should no loading bc it alr there from the
// message". The pane asks for the icon's 192px image, the message drew its
// 72px one: one picture at two sizes. A size the cache doesn't hold yet
// draws at once from the largest size of the same picture it holds, scaled,
// and the exact image loads and replaces it. Synthetic URLs.

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
	"time"
)

func familyPNG(px int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, px, px))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	img.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func TestFamilySizeDrawsAtOnce(t *testing.T) {
	const (
		small    = "https://avatars.slack-edge.com/2026-11-22/1234567_abcdef0123_72.png"
		big      = "https://avatars.slack-edge.com/2026-11-22/1234567_abcdef0123_512.png"
		stranger = "https://avatars.slack-edge.com/2026-11-22/7654321_fedcba9876_512.png"
		original = "https://avatars.slack-edge.com/2026-11-22/1234567_abcdef0123_original.png"
		pasted   = "https://avatars.slack-edge.com/2026-11-22/1234567_abcdef0123_blurred.png"
	)
	var mu sync.Mutex
	hold := make(chan struct{}) // the larger images' bytes, held
	asked := map[string]int{}
	fetch := func(ctx context.Context, u string) ([]byte, error) {
		mu.Lock()
		asked[u]++
		mu.Unlock()
		if u != small {
			select {
			case <-hold:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return familyPNG(512), nil
		}
		return familyPNG(72), nil
	}
	c := New(fetch, "", nil)
	defer close(hold)
	for dl := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, ok := c.Get(small, 72); ok {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("the 72px image never loaded")
		}
	}
	// the 512px image of the same picture: the 72px one, at once
	if _, ok := c.Get(big, 144); !ok {
		t.Fatal("the 512px size draws nothing on its first Get while the 72px one is held: a loading box")
	}
	for dl := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		mu.Lock()
		n := asked[big]
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("the sharp size was never asked for: the scaled copy would stay")
		}
	}
	// another picture isn't the same picture
	if _, ok := c.Get(stranger, 144); ok {
		t.Error("another picture's 512px image drew from this one's 72px")
	}
	// the full-size viewer's image_original: the picture held, at once
	if _, ok := c.Get(original, 144); !ok {
		t.Error("image_original draws nothing while the 72px size of its picture is held")
	}
	if familyKey(pasted) != "" {
		t.Error("a name that isn't a size counted as one")
	}
	if k := familyKey("https://ca.slack-edge.com/T0SYNTH01-U0SYNTH01-0123abcd-72"); k == "" || k != familyKey("https://ca.slack-edge.com/T0SYNTH01-U0SYNTH01-0123abcd-512") {
		t.Errorf("ca.slack-edge.com's sizes aren't one picture: %q", k)
	}
}
