package blockkit

// The slim ToggleBar's status icon sits as Slack's .toggleBarIconInline
// does (vertical-align middle, top -.5px): its middle half the title's
// x-height over the baseline. Clack centred it on the 19.5px line, about
// 2px too high (Karim, 2026-11-24: "this icon seems misaligned", a failed
// task's warning triangle above its title). Drawn, in each state, at zoom
// 0 and +1 (1.1x).

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
		for _, scale := range []float32{1, 1.1} {
			t.Run(fmt.Sprintf("%s/%.1fx", name, scale), func(t *testing.T) {
				icon, text, ok := toggleLineInk(t, name, scale)
				if !ok {
					t.Fatal("no toggle line drawn")
				}
				if d := math.Abs(icon - text); d > float64(scale) {
					t.Errorf("the icon's middle is %.1fpx from the title's x-height middle (icon %.1f, text %.1f), want within 1dp", d, icon, text)
				}
			})
		}
	}
}

// toggleLineInk draws fixture name's message on white at scale and returns
// the slim toggle bar's first line's icon middle (its ink's) and the
// title's x-height middle (the commonest top and bottom of its letters'
// ink: the x-height and the baseline), in px.
func toggleLineInk(t *testing.T, name string, scale float32) (icon, text float64, ok bool) {
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
		return 0, 0, false
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
	ink := func(x, y, min int) bool {
		c := img.RGBAAt(x, y)
		return int(255-c.R)+int(255-c.G)+int(255-c.B) > min
	}
	inked := func(x, y int) bool { return ink(x, y, 150) }
	lo, hi := -1, -1
	for y := y0; y < y1; y++ {
		for x := slot.Min.X; x < slot.Min.X+px(12); x++ {
			if ink(x, y, 60) { // the spinner's faint track too: its arcs alone turn
				if lo < 0 {
					lo = y
				}
				hi = y
			}
		}
	}
	if lo < 0 {
		return 0, 0, false
	}
	tops, bots := map[int]int{}, map[int]int{}
	for x := slot.Min.X + px(20); x < min(slot.Max.X, slot.Min.X+px(200)); x++ {
		t0, b0 := -1, -1
		for y := y0; y < y1; y++ {
			if inked(x, y) {
				if t0 < 0 {
					t0 = y
				}
				b0 = y
			}
		}
		if t0 >= 0 {
			tops[t0]++
			bots[b0]++
		}
	}
	mode := func(m map[int]int) int {
		best, n := -1, -1
		for k, c := range m {
			if c > n || c == n && k < best {
				best, n = k, c
			}
		}
		return best
	}
	if len(tops) == 0 {
		return 0, 0, false
	}
	return float64(lo+hi+1) / 2, float64(mode(tops)+mode(bots)+1) / 2, true
}
