package imgcache

import (
	"bytes"
	"image"
	"image/draw"
	"image/gif"
	"time"

	"gioui.org/op/paint"
)

// Animated GIFs (custom emoji, mostly) play like they do in Slack. Every
// frame is composed on the GIF's canvas (frames may cover only part of it
// and say how to dispose of themselves), scaled to the requested size and
// kept; Get returns the frame for the current time and arranges a repaint
// when the next one is due. Only small sizes animate (emoji, avatars):
// a large GIF stays its first frame, so a long image channel can't fill the
// memory budget with frames.

const (
	// MaxAnimPx is the largest requested size that animates.
	MaxAnimPx = 160
	// maxFrames caps the frames kept per image (later ones are dropped).
	maxFrames = 200
	// minDelay: browsers play 0/10 ms frame delays at 100 ms.
	minDelay = 20 * time.Millisecond
)

type anim struct {
	frames []paint.ImageOp
	ends   []time.Duration // cumulative: frame i shows until ends[i]
	total  time.Duration
	start  time.Time
}

// frameAt returns the frame to show at now and when the next one is due.
func (a *anim) frameAt(now time.Time) (int, time.Time) {
	el := now.Sub(a.start)
	if el < 0 {
		el = 0
	}
	t := el % a.total
	i := 0
	for i < len(a.ends)-1 && t >= a.ends[i] {
		i++
	}
	return i, now.Add(a.ends[i] - t)
}

func isGIF(data []byte) bool { return bytes.HasPrefix(data, []byte("GIF8")) }

// decodeGIF composes the frames of an (already size-checked) GIF on its
// canvas and scales each to max px. With one=true only the first frame.
func decodeGIF(data []byte, max int, one bool) ([]*image.RGBA, []time.Duration, error) {
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	w, h := g.Config.Width, g.Config.Height
	if len(g.Image) > 0 && (w <= 0 || h <= 0) {
		b := g.Image[0].Bounds()
		w, h = b.Max.X, b.Max.Y
	}
	if w <= 0 || h <= 0 || w > MaxSide || h > MaxSide || w*h > MaxPixels {
		return nil, nil, ErrTooBig
	}
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	var prev *image.RGBA
	var out []*image.RGBA
	var delays []time.Duration
	for i, fr := range g.Image {
		disp := byte(0)
		if i < len(g.Disposal) {
			disp = g.Disposal[i]
		}
		if disp == gif.DisposalPrevious {
			prev = cloneRGBA(canvas)
		}
		draw.Draw(canvas, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
		out = append(out, Scale(cloneRGBA(canvas), max))
		d := minDelay
		if i < len(g.Delay) {
			if v := time.Duration(g.Delay[i]) * 10 * time.Millisecond; v > 10*time.Millisecond {
				d = v
			} else {
				d = 100 * time.Millisecond
			}
		}
		delays = append(delays, d)
		if one || len(out) >= maxFrames {
			break
		}
		switch disp {
		case gif.DisposalBackground:
			draw.Draw(canvas, fr.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			if prev != nil {
				copy(canvas.Pix, prev.Pix)
			}
		}
	}
	if len(out) == 0 {
		return nil, nil, ErrTooBig
	}
	return out, delays, nil
}

func cloneRGBA(m *image.RGBA) *image.RGBA {
	c := image.NewRGBA(m.Rect)
	copy(c.Pix, m.Pix)
	return c
}

// Animated reports whether the ready image at url/sizePx animates (callers
// that record their drawing draw it every frame instead).
func (c *Cache) Animated(url string, sizePx int) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key{url, sizePx}]; ok {
		return el.Value.(*entry).anim != nil
	}
	return false
}

// wakeLocked repaints the window at t (the earliest of the pending wakes).
func (c *Cache) wakeLocked(t time.Time) {
	now := time.Now()
	if !c.wakeAt.IsZero() && c.wakeAt.After(now) && !t.Before(c.wakeAt) {
		return
	}
	c.wakeAt = t
	d := t.Sub(now)
	if c.wakeT == nil {
		c.wakeT = time.AfterFunc(d, c.fireWake)
	} else {
		c.wakeT.Reset(d)
	}
}

func (c *Cache) fireWake() {
	c.mu.Lock()
	c.wakeAt = time.Time{}
	inv := c.invalidate
	c.mu.Unlock()
	inv()
}
