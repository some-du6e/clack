package slackc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// A plugin element whose background-image is a data: URL (Slick's oneko:
// its sprite, inlined by Slick's build, a frame picked by
// background-position, pixelated) is drawn, decoded from the URL's own
// text as Chromium decodes it: the box shows the frame background-position
// picks and nothing of the sprite around it, and the image cache's
// fetcher is never asked. An <img> whose src is a data: URL is drawn at
// the image's size.
func TestBackgroundDataImage(t *testing.T) {
	// a sprite of two 16×16 frames, red then blue
	sprite := image.NewNRGBA(image.Rect(0, 0, 32, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			c := color.NRGBA{R: 255, A: 255}
			if x >= 16 {
				c = color.NRGBA{B: 255, A: 255}
			}
			sprite.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, sprite); err != nil {
		t.Fatal(err)
	}
	u := "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
	src, _ := json.Marshal(u)
	var fetched atomic.Int32
	im := imgcache.New(func(context.Context, string) ([]byte, error) {
		fetched.Add(1)
		return nil, errors.New("no network in this test")
	}, "", nil)
	defer im.Close()
	prev := env()
	t.Cleanup(func() { Init(prev) })
	Init(Env{Theme: theme.New(false), Images: im})
	withStyles(t, func(n *Node, s *css.Style) {
		switch n.ID {
		case "cat":
			s.Position = css.PosFixed
			s.Left, s.Top = px(10), px(10)
			s.Width, s.Height = px(16), px(16)
			s.Background.Image = u
			s.Background.Position = "-16px 0px"
			s.Pixelated = true
			s.PointerEvents = false
		case "pic":
			s.Position = css.PosFixed
			s.Left, s.Top = px(40), px(10)
		}
	})
	const w, h = 80, 40
	windowSize = image.Pt(w, h)
	deliverOverlay(OverlayBody, mustTree(t, `[{"t":"div","k":1,"p":{"id":"cat"}},{"t":"img","k":2,"p":{"id":"pic","src":`+string(src)+`}}]`))
	defer deliverOverlay(OverlayBody, nil)
	frame := func() *op.Ops {
		gtx := testGtx(w, h)
		LayoutOverlays(gtx)
		return gtx.Ops
	}
	page := imgcache.PageURL(u)
	deadline := time.Now().Add(10 * time.Second)
	for {
		frame()
		if _, ok := im.Get(page, 0); ok {
			break
		}
		if im.Failed(page, 0) || time.Now().After(deadline) {
			t.Fatalf("the sprite didn't load (failed %v)", im.Failed(page, 0))
		}
		time.Sleep(5 * time.Millisecond)
	}
	frame() // laid out with the image's size
	if n := fetched.Load(); n != 0 {
		t.Errorf("the fetcher was asked %d times, want never", n)
	}
	overlays.Lock()
	v := overlays.views[OverlayBody]
	overlays.Unlock()
	if v == nil {
		t.Fatal("no body overlay view")
	}
	if p := byID(v, "pic"); p == nil {
		t.Error("no <img> box")
	} else if p.w != 32 || p.h != 16 {
		t.Errorf("the <img>'s box is %vx%v, want the image's 32x16", p.w, p.h)
	}

	img := pixeltest.Render(t, w, h, frame())
	blue, red := color.RGBA{B: 255, A: 255}, color.RGBA{R: 255, A: 255}
	for _, c := range []struct {
		p    image.Point
		want color.RGBA
		what string
	}{
		{image.Pt(11, 11), blue, "the box's top left"},
		{image.Pt(24, 24), blue, "the box's bottom right"},
		{image.Pt(42, 12), red, "the <img>'s left half"},
		{image.Pt(70, 24), blue, "the <img>'s right half"},
	} {
		if got := img.RGBAAt(c.p.X, c.p.Y); got != c.want {
			t.Errorf("%s at %v: %v, want %v", c.what, c.p, got, c.want)
		}
	}
	for _, p := range []image.Point{{8, 8}, {28, 18}, {18, 28}, {36, 18}} {
		if got := img.RGBAAt(p.X, p.Y); got == blue || got == red {
			t.Errorf("at %v, outside both boxes: %v, the sprite drawn past them", p, got)
		}
	}
}
