package messages

// Screenshots of the hover toolbar's Add reaction… button under the
// pointer, light and dark, through a real router and Gio's GPU renderer
// (MESSAGES_AUDIT_SHOTS_DIR, the messages-audit-shots workflow), on
// context_test.go's synthetic channel. Frames are taken at fixed times
// after the pointer arrives: at rest, 0, 20, 40 and 80 ms (the tilt's 80 ms
// transition), at 560 and 1100 ms (the tooltip's 60 ms delay and its
// second-long fade), and Reply in thread at 80 ms.
//
// This file uses only what the ui branch already had, so the workflow also
// runs it on a checkout of ui with TOOLBAR_SHOTS_TAG=before: the
// toolbar-hover-before-* shots are the old drawing, toolbar-hover-after-*
// this branch's. Nothing talks to Slack.

import (
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func TestToolbarHoverShots(t *testing.T) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("MESSAGES_AUDIT_SHOTS_DIR not set (the messages-audit-shots workflow)")
	}
	tag := os.Getenv("TOOLBAR_SHOTS_TAG")
	if tag == "" {
		tag = "after"
	}
	const w, h, zoom = 900, 700, 3
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		e := newCtxEnv(t)
		th := theme.New(dark)
		e.h.coreHost.th = th
		s := e.v.s
		frame := func(now time.Time) {
			e.ops.Reset()
			e.now = now
			paint.FillShape(&e.ops, th.C.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
			gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h)), Now: now, Source: e.r.Source()}
			e.v.Layout(gtx)
			e.r.Frame(&e.ops)
		}
		move := func(p f32.Point, now time.Time) {
			e.clock += 10 * time.Millisecond
			e.r.Queue(pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: e.clock})
			frame(now)
		}
		button := func(b int) f32.Point {
			for k, i := range s.tbBtns {
				if i == b {
					bs, pad := int(mt.toolbarBtn), int(mt.toolbarPad)
					return f32.Pt(float32(s.tbRect.Min.X+pad+(len(s.quick.names)+k)*bs+bs/2+s.origin.X),
						float32(s.tbRect.Min.Y+pad+bs/2+s.origin.Y))
				}
			}
			t.Fatalf("%s: no toolbar button %d (%v)", mode, b, s.tbBtns)
			return f32.Point{}
		}
		var shots []*image.RGBA
		shoot := func(name string) {
			img := pixeltest.Render(t, w, h, &e.ops)
			r := image.Rect(s.tbRect.Min.X-70, s.tbRect.Min.Y-54, s.tbRect.Max.X+12, s.tbRect.Max.Y+10).Add(s.origin).Intersect(img.Bounds())
			out := image.NewRGBA(image.Rect(0, 0, r.Dx()*zoom, r.Dy()*zoom))
			for y := 0; y < out.Bounds().Dy(); y++ {
				for x := 0; x < out.Bounds().Dx(); x++ {
					out.Set(x, y, img.At(r.Min.X+x/zoom, r.Min.Y+y/zoom))
				}
			}
			savePNG(t, filepath.Join(dir, "toolbar-hover-"+tag+"-"+mode+"-"+name+".png"), out)
			shots = append(shots, out)
		}

		for k := 0; k < 3; k++ {
			e.frame()
		}
		now := e.now.Add(time.Second)
		for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
			if s.items[i].key == tsCtxLink && s.heights[i] > 0 {
				move(f32.Pt(300, float32(s.pos[i]+s.heights[i]-4+s.origin.Y)), now)
			}
		}
		frame(now.Add(20 * time.Millisecond))
		if s.tbFor < 0 {
			t.Fatalf("%s: no toolbar", mode)
		}
		frame(now.Add(40 * time.Millisecond))
		shoot("rest")

		t0 := e.now.Add(time.Second)
		move(button(tbAddReact), t0)
		shoot("0ms")
		for _, ms := range []int{20, 40, 80, 560, 1100} {
			frame(t0.Add(time.Duration(ms) * time.Millisecond))
			shoot(map[int]string{20: "20ms", 40: "40ms", 80: "80ms", 560: "tip-560ms", 1100: "tip-1100ms"}[ms])
		}
		t1 := e.now.Add(time.Second)
		move(button(tbReply), t1)
		frame(t1.Add(80 * time.Millisecond))
		shoot("reply-80ms")

		// all of them in one strip, top to bottom
		sw, sh := 0, 0
		for _, im := range shots {
			sw, sh = max(sw, im.Bounds().Dx()), sh+im.Bounds().Dy()+zoom*2
		}
		strip := image.NewRGBA(image.Rect(0, 0, sw, sh))
		y := 0
		for _, im := range shots {
			draw.Draw(strip, im.Bounds().Add(image.Pt(0, y)), im, image.Point{}, draw.Src)
			y += im.Bounds().Dy() + zoom*2
		}
		savePNG(t, filepath.Join(dir, "toolbar-hover-"+tag+"-"+mode+"-strip.png"), strip)
	}
}
