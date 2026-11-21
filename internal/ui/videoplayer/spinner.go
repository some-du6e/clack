package videoplayer

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"
	"time"

	"gioui.org/op/paint"
	"golang.org/x/image/vector"
)

// The pill's spinner while a video loads: Slack Kit's InfiniteSpinner
// (s6So), a CSS animation that costs Slack no page layout. Clack draws it
// as a live image (Gio patch 35) of frames rasterised once per size, and
// the controller asks for a quiet frame (the last frame's ops drawn again,
// no layout) every frame interval while some tile loads: no op.InvalidateCmd
// mid-layout (which loses the window's other input), and nothing at all
// for a tile that isn't trying to play.

const (
	spinCycle  = 1200 * time.Millisecond // a turn
	spinFrames = 72                      // its frames (60 a second)
	spinTick   = spinCycle / spinFrames
)

var (
	spinMu    sync.Mutex
	spinCache = map[int][]paint.ImageOp{}
)

// spinImages are the spinner's frames at d pixels.
func spinImages(d int) []paint.ImageOp {
	spinMu.Lock()
	defer spinMu.Unlock()
	if fs, ok := spinCache[d]; ok {
		return fs
	}
	fs := make([]paint.ImageOp, spinFrames)
	for i := range fs {
		fs[i] = paint.NewImageOp(spinFrame(d, float64(i)/spinFrames))
	}
	spinCache[d] = fs
	return fs
}

// spinFrame is the spinner at t (0..1 of a turn), d pixels square: the
// 8/78 stroke on the 35/78 ring, the track white at 64, two quarter arcs
// white, eased as the Kit's keyframes are.
func spinFrame(d int, t float64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, d, d))
	sw := float64(d) * 8 / 78
	rr := float64(d) * 35 / 78
	c := float64(d) / 2
	white := image.NewUniform(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	track := image.NewUniform(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 64})
	var z vector.Rasterizer
	// the track: the ring, its inner circle wound the other way
	z.Reset(d, d)
	circle(&z, c, c, rr+sw/2, false)
	circle(&z, c, c, rr-sw/2, true)
	z.Draw(img, img.Bounds(), track, image.Point{})
	q := 1.5 * math.Pi
	for _, a0 := range []float64{
		q + 2*math.Pi*cubicBezierY(.25, .29, .54, .86, t),
		q + 2*math.Pi*cubicBezierY(.41, .24, .64, .69, t),
	} {
		z.Reset(d, d)
		roundArc(&z, c, c, rr, sw, a0, math.Pi/2)
		z.DrawOp = draw.Over
		z.Draw(img, img.Bounds(), white, image.Point{})
	}
	return img
}

// circle adds a circle of radius r around (cx, cy), clockwise unless ccw.
func circle(z *vector.Rasterizer, cx, cy, r float64, ccw bool) {
	const n = 64
	for i := 0; i <= n; i++ {
		a := 2 * math.Pi * float64(i) / n
		if ccw {
			a = -a
		}
		x, y := float32(cx+r*math.Cos(a)), float32(cy+r*math.Sin(a))
		if i == 0 {
			z.MoveTo(x, y)
		} else {
			z.LineTo(x, y)
		}
	}
	z.ClosePath()
}

// roundArc adds the stroke of the arc of radius r from a0 sweeping s,
// w wide with round ends (as Gio's clip.Stroke draws it).
func roundArc(z *vector.Rasterizer, cx, cy, r, w, a0, s float64) {
	const n = 32
	ro, ri := r+w/2, r-w/2
	pt := func(rad, a float64) (float32, float32) {
		return float32(cx + rad*math.Cos(a)), float32(cy + rad*math.Sin(a))
	}
	x, y := pt(ro, a0)
	z.MoveTo(x, y)
	for i := 1; i <= n; i++ { // the outer edge
		z.LineTo(pt(ro, a0+s*float64(i)/n))
	}
	end := func(a float64, out bool) { // a half circle round the end
		ex, ey := cx+r*math.Cos(a), cy+r*math.Sin(a)
		for i := 1; i < n; i++ {
			b := a + math.Pi*float64(i)/n
			if !out {
				b = a + math.Pi + math.Pi*float64(i)/n
			}
			z.LineTo(float32(ex+w/2*math.Cos(b)), float32(ey+w/2*math.Sin(b)))
		}
	}
	end(a0+s, true)
	for i := n; i >= 0; i-- { // the inner edge, back
		z.LineTo(pt(ri, a0+s*float64(i)/n))
	}
	end(a0, false)
	z.ClosePath()
}

// spinLive is a tile's spinner, live (paint.LiveImage): the frame of now,
// from when it started.
type spinLive struct {
	frames []paint.ImageOp
	t0     time.Time
}

func (s *spinLive) Image() paint.ImageOp {
	i := int(time.Since(s.t0)%spinCycle/spinTick) % len(s.frames)
	return s.frames[i]
}

// spinningLocked reports whether some tile draws its spinner and wants to
// play: then the controller asks a quiet frame every spinTick.
func (c *Controller) spinningLocked() bool {
	for _, t := range c.tiles {
		if t.loading && t.h != nil && !t.viewer {
			return true
		}
	}
	return false
}

// spinLocked keeps the spinner's frames coming while one spins, and stops
// them when none does.
func (c *Controller) spinLocked() {
	if c.spinOn || !c.spinningLocked() || c.env.FrameReady == nil {
		return
	}
	c.spinOn = true
	var tick func()
	tick = func() {
		c.mu.Lock()
		if !c.spinningLocked() {
			c.spinOn = false
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
		c.env.FrameReady()
		time.AfterFunc(spinTick, tick)
	}
	time.AfterFunc(spinTick, tick)
}
