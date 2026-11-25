package blockkit

// The slim ToggleBar's status icon and caret sit as Slack's
// .toggleBarIconInline and .toggleBarCaretInline do (vertical-align
// middle in div.toggleBarTitleContainer, the 15px message's font): each
// box's middle half the 15px x-height over the title's baseline, the
// icon .5px higher (top -.5px). Clack centred both on the 19.5px line,
// about 2px too high (Karim, 2026-11-24: "this icon seems misaligned", a
// failed task's warning triangle above its title); then the icon on the
// 13px title's x-height, half a px low, its caret still on the line.
// Drawn, in each state, at 1x, 1.1x and 2x (Karim's Mac).

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func TestAgentToggleIconAlignsWithTitle(t *testing.T) {
	for _, name := range []string{"plan-error", "plan-complete", "plan-in-progress"} {
		for _, scale := range []float32{1, 1.1, 2} {
			t.Run(fmt.Sprintf("%s/%.1fx", name, scale), func(t *testing.T) {
				ln, ok := toggleLineInk(t, name, scale)
				if !ok {
					t.Fatal("no toggle line drawn")
				}
				// Slack's middles: half the 15px x-height (Lato's sxHeight,
				// 1013/2000 em) over the baseline, the icon .5px over that.
				// A box's top is a whole pixel, so its middle is within
				// half a pixel of Slack's (and the ink's offset in its box
				// is measured, not guessed: toggleInkOffset).
				mid := float64(ln.base) - 15*float64(scale)*1013/2000/2
				const tol = 0.55
				if want := mid - 0.5*float64(scale); math.Abs(ln.icon-want) > tol {
					t.Errorf("the icon's box middle is %.2f, Slack's %.2f (baseline %d): %.2fpx off", ln.icon, want, ln.base, ln.icon-want)
				}
				if !ln.hasCaret {
					t.Fatal("no caret after the title")
				}
				if math.Abs(ln.caret-mid) > tol {
					t.Errorf("the caret's box middle is %.2f, Slack's %.2f (baseline %d): %.2fpx off", ln.caret, mid, ln.base, ln.caret-mid)
				}
			})
		}
	}
}

// toggleInk is a slim toggle line's first line as drawn: the title's
// baseline (its letters' commonest ink bottom, plus one), and the icon's
// and caret's box middles (their ink's middle less the ink's offset in a
// box drawn alone), in px.
type toggleInk struct {
	base        int
	icon, caret float64
	hasCaret    bool
}

// inkRows is the first and last row of img with ink (at least min of 765
// off white) in columns x0 to x1 and rows y0 to y1, or -1s.
func inkRows(img *image.RGBA, x0, x1, y0, y1, min int) (lo, hi int) {
	lo, hi = -1, -1
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c := img.RGBAAt(x, y)
			if int(255-c.R)+int(255-c.G)+int(255-c.B) > min {
				if lo < 0 {
					lo = y
				}
				hi = y
				break
			}
		}
	}
	return lo, hi
}

// toggleInkOffset draws w (a box size dp tall) alone at the top left at
// scale and returns its ink's middle less the box's: what an ink middle
// is off its box's middle.
func toggleInkOffset(t *testing.T, scale, size float32, w layout.Widget) float64 {
	t.Helper()
	ops := new(op.Ops)
	paint.FillShape(ops, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Rect{Max: image.Pt(64, 64)}.Op())
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Now: time.Now().Add(time.Hour),
		Constraints: layout.Constraints{Max: image.Pt(64, 64)}}
	w(gtx)
	img := pixeltest.Render(t, 64, 64, ops)
	lo, hi := inkRows(img, 0, 64, 0, 64, 60)
	if lo < 0 {
		t.Fatal("the box drew no ink")
	}
	return float64(lo+hi+1)/2 - float64(dp(gtx, size))/2
}

// toggleLineInk draws fixture name's message on white at scale and reads
// its slim toggle bar's first line.
func toggleLineInk(t *testing.T, name string, scale float32) (ln toggleInk, ok bool) {
	t.Helper()
	h := &agentHost{th: theme.New(false)} // no core: slim
	m := agentMsg(t, name)
	me := MessageElements(h, &m)
	v := richview.New(m.Doc, richview.Options{Elements: me})
	st := styleFor(h.th, nil)
	const w, hgt = 520, 300
	var r input.Router
	var ops *op.Ops
	var slot image.Rectangle
	now := time.Now().Add(time.Hour)
	for i := 0; i < 3; i++ {
		ops = new(op.Ops)
		paint.FillShape(ops, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Rect{Max: image.Pt(w, hgt)}.Op())
		now = now.Add(100 * time.Millisecond)
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Now: now,
			Constraints: layout.Constraints{Max: image.Pt(w, hgt)}, Source: r.Source()}
		v.Layout(gtx, st, &surfHandler{h: h})
		r.Frame(ops)
		if sl := v.ElementSlots(); len(sl) > 0 {
			slot = sl[0]
		}
	}
	if slot.Empty() {
		return ln, false
	}
	img := pixeltest.Render(t, w, hgt, ops)
	px := func(v float32) int { return int(v*scale + 0.5) }
	if dir := os.Getenv("BK_AGENT_SHOTS_DIR"); dir != "" { // the agents area's shot
		if f, err := os.Create(filepath.Join(dir, fmt.Sprintf("agent-toggle-icon-%s-%.1fx.png", name, scale))); err == nil {
			_ = png.Encode(f, img.SubImage(slot.Inset(-px(8))))
			_ = f.Close()
		}
	}
	y0, y1 := slot.Min.Y, slot.Min.Y+px(19.5)
	inked := func(x, y, min int) bool {
		c := img.RGBAAt(x, y)
		return int(255-c.R)+int(255-c.G)+int(255-c.B) > min
	}

	// the icon: its ink (the spinner's faint track too: its arcs alone
	// turn), less its ink's offset in its box
	pal := agentColors(h.th)
	status, isz := StatusError, float32(13)
	switch name {
	case "plan-complete":
		status = StatusComplete
	case "plan-in-progress":
		status, isz = StatusInProgress, 12
	}
	lo, hi := inkRows(img, slot.Min.X, slot.Min.X+px(12), y0, y1, 60)
	if lo < 0 {
		return ln, false
	}
	off := toggleInkOffset(t, scale, isz, func(gtx C) D { return statusIcon(gtx, pal, status, true) })
	ln.icon = float64(lo+hi+1)/2 - float64(slot.Min.Y) - off

	// the title: the commonest bottom of its letters' ink is the row over
	// its baseline; the caret is the last run of inked columns, past a gap
	// wider than any between the title's words
	bots := map[int]int{}
	var cols []int
	for x := slot.Min.X + px(20); x < slot.Max.X; x++ {
		b0 := -1
		for y := y0; y < y1; y++ {
			if inked(x, y, 150) {
				b0 = y
			}
		}
		if b0 >= 0 {
			bots[b0]++
		}
		for y := y0; y < y1; y++ {
			if inked(x, y, 60) {
				cols = append(cols, x)
				break
			}
		}
	}
	if len(bots) == 0 {
		return ln, false
	}
	best, n := -1, -1
	for k, c := range bots {
		if c > n || c == n && k < best {
			best, n = k, c
		}
	}
	ln.base = best + 1 - slot.Min.Y
	if len(cols) > 0 {
		c1 := len(cols) - 1
		c0 := c1
		for c0 > 0 && cols[c0]-cols[c0-1] <= px(1) {
			c0--
		}
		if c0 > 0 && cols[c0]-cols[c0-1] > px(6) {
			lo, hi := inkRows(img, cols[c0], cols[c1]+1, y0, y1, 60)
			// caret-right or caret-down: whichever's ink is as wide
			wid := cols[c1] - cols[c0] + 1
			best := math.Inf(1)
			for _, name := range []string{"caret-right", "caret-down"} {
				cw := inkCols(t, scale, func(gtx C) D { return agentIcon(gtx, name, 13, pal.ter) })
				if d := math.Abs(float64(cw - wid)); d < best {
					best = d
					ln.caret = float64(lo+hi+1)/2 - float64(slot.Min.Y) - toggleInkOffset(t, scale, 13, func(gtx C) D { return agentIcon(gtx, name, 13, pal.ter) })
				}
			}
			ln.hasCaret = true
		}
	}
	return ln, true
}

// inkCols is how many columns w's ink spans, drawn alone at scale.
func inkCols(t *testing.T, scale float32, w layout.Widget) int {
	t.Helper()
	ops := new(op.Ops)
	paint.FillShape(ops, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Rect{Max: image.Pt(64, 64)}.Op())
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Now: time.Now().Add(time.Hour),
		Constraints: layout.Constraints{Max: image.Pt(64, 64)}}
	w(gtx)
	img := pixeltest.Render(t, 64, 64, ops)
	x0, x1 := -1, -1
	for x := 0; x < 64; x++ {
		for y := 0; y < 64; y++ {
			c := img.RGBAAt(x, y)
			if int(255-c.R)+int(255-c.G)+int(255-c.B) > 60 {
				if x0 < 0 {
					x0 = x
				}
				x1 = x
				break
			}
		}
	}
	return x1 - x0 + 1
}
