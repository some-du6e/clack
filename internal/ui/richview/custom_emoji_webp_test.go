package richview

import (
	"context"
	"errors"
	"image/color"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/imgcache/webptest"
	rt "github.com/some-du6e/clack/internal/richtext"
)

// A custom emoji in a message whose image is an animated WebP behind a
// .png URL (Slack serves :explodes: that way) draws its image through the
// real cache: it loads, the view stops waiting on it and draws it live
// (it plays). Before, the decode failed, the cache fetched it again every
// minute, and the emoji was a blank 22px gap that only its tooltip
// (from the emoji's name) gave away. Synthetic image only.
func TestCustomEmojiAnimatedWebPDraws(t *testing.T) {
	const u = "https://emoji.slack-edge.com/T1/explodes/0123456789abcdef.png"
	data := webptest.Anim(32, 32, 0,
		webptest.Solid(0, 0, 32, 32, 70, color.NRGBA{255, 80, 0, 255}),
		webptest.Solid(8, 8, 16, 16, 30, color.NRGBA{255, 220, 0, 200}))
	c := imgcache.New(func(_ context.Context, url string) ([]byte, error) {
		if url == u {
			return data, nil
		}
		return nil, errors.New("no such image")
	}, "", nil)
	d := doc(para(txt("boom "), rt.Span{Kind: rt.Emoji, ID: "explodes", Text: ":explodes:", URL: u}))
	hs := &harness{st: newTestStyle(), v: New(d, Options{}), h: &recHandler{}, w: 400}
	hs.st.Images = c
	hs.frame()
	fi := -1
	for i := range hs.v.d.frags {
		if f := &hs.v.d.frags[i]; f.kind == fImage && f.url == u {
			fi = i
		}
	}
	if fi < 0 {
		t.Fatal("no image frag for the custom emoji")
	}
	size := hs.v.d.frags[fi].size
	for dl := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		hs.frame()
		if _, ok := c.Get(u, size); ok {
			break
		}
		if c.Failed(u, size) || time.Now().After(dl) {
			t.Fatalf("the emoji never loaded (failed: %v)", c.Failed(u, size))
		}
	}
	for i := 0; i < 3; i++ {
		hs.frame()
	}
	if !c.IsAnimation(u, size) {
		t.Error("the emoji isn't an animation")
	}
	for _, p := range hs.v.pending {
		if p == ^int32(fi) {
			t.Fatal("the view still waits on the loaded emoji")
		}
	}
	live := false
	for _, i := range hs.v.live {
		live = live || int(i) == fi
	}
	if !live {
		t.Errorf("the emoji isn't drawn live (live %v): it wouldn't play", hs.v.live)
	}
}
