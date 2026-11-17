// SPDX-License-Identifier: Unlicense OR MIT

package headless

import (
	"image"
	"image/color"
	"image/draw"
	"testing"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// liveTwo is a LiveImage of two solid images, showing cur.
type liveTwo struct {
	ops [2]paint.ImageOp
	cur int
}

func (l *liveTwo) Image() paint.ImageOp { return l.ops[l.cur] }

// Clack patch 35: the same ops drawn twice draw a live image as it is at
// each frame, on a real GPU context.
func TestLiveImageFrames(t *testing.T) {
	w, release := newTestWindow(t)
	defer release()
	solid := func(c color.RGBA) paint.ImageOp {
		m := image.NewRGBA(image.Rect(0, 0, 16, 16))
		draw.Draw(m, m.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
		return paint.NewImageOp(m)
	}
	red, blue := color.RGBA{R: 0xff, A: 0xff}, color.RGBA{B: 0xff, A: 0xff}
	l := &liveTwo{ops: [2]paint.ImageOp{solid(red), solid(blue)}}
	var ops op.Ops
	paint.Fill(&ops, color.NRGBA{G: 0xff, A: 0xff}) // the background
	st := op.Offset(image.Pt(4, 4)).Push(&ops)
	cl := clip.Rect(image.Rect(0, 0, 16, 16)).Push(&ops)
	paint.NewLiveImageOp(l).Add(&ops)
	paint.PaintOp{}.Add(&ops)
	cl.Pop()
	st.Pop()
	at := func() color.RGBA {
		t.Helper()
		if err := w.Frame(&ops); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rectangle{Max: w.Size()})
		if err := w.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		return img.RGBAAt(10, 10)
	}
	if got := at(); got != red {
		t.Errorf("first frame: %v, want the live image's red", got)
	}
	l.cur = 1
	if got := at(); got != blue {
		t.Errorf("the same ops again after it moved on: %v, want blue", got)
	}
}
