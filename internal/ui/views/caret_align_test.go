package views

// The caret in views' inputs against their placeholders and text
// (caret-align, Karim 2026-11-20: channel details' "Add agents and apps
// to #clack" modal drew its search's caret from near the input's top
// padding down to the placeholder's x-height). The rules are Blink's
// (theme/carettest). Pixels: CI runs these with CLACK_PIXEL_TESTS=1 on
// llvmpipe, WARP and Metal (flake-check only=caret-align).

import (
	"fmt"
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/carettest"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// caretShooter lays out draw (at 20,20 in a w×h px frame over bg) with
// ed focused, two frames, and renders the last.
func caretShooter(t *testing.T, sc float32, w, h int, ed *widget.Editor, bg func() color.NRGBA, draw func(gtx C)) func() *image.RGBA {
	var r input.Router
	var ops op.Ops
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: sc, PxPerSp: sc},
			Constraints: layout.Exact(image.Pt(w, h)), Now: time.Now()}
		if !gtx.Focused(ed) {
			gtx.Execute(key.FocusCmd{Tag: ed})
		}
		theme.Fill(gtx, image.Rectangle{Max: image.Pt(w, h)}, bg())
		st := op.Offset(image.Pt(20, 20)).Push(gtx.Ops)
		gtx.Constraints = layout.Constraints{Max: image.Pt(w-40, h-40)}
		draw(gtx)
		st.Pop()
		r.Frame(&ops)
	}
	return func() *image.RGBA {
		frame()
		frame()
		frame()
		return pixeltest.Render(t, w, h, &ops)
	}
}

func TestCaretAlignViewsInputs(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, sc := range []float32{1, 1.5, 2} {
			mode := "light"
			if dark {
				mode = "dark"
			}
			t.Run(fmt.Sprintf("%s-%gx", mode, sc), func(t *testing.T) {
				e := newCDTestEnv(t, &cdTestServer{}, "C1")
				th := theme.New(dark)
				sh := theme.NewShaper()
				th.SetShapers(sh, sh)
				e.h.th = th
				d := e.d
				d.base.frame() // the palette the backgrounds read
				dp := func(v float32) int { return int(v*sc + .5) }
				w, h := dp(480), dp(120)

				// the agents and apps invite modal's picker (Karim's
				// capture): AddToChannelInitialScreen under AgentsApps
				a := &cdAddPeople{agents: true, q: newSearchIn(nsChannelInvite, "Search agents and apps"),
					tokClk: map[string]*widget.Clickable{}, resClk: map[string]*widget.Clickable{}, members: map[string]bool{}}
				var pick D
				shot := caretShooter(t, sc, w, h, &a.q.ed, func() color.NRGBA { return d.p.Surface }, func(gtx C) {
					d.base.frame()
					gtx.Constraints.Max.X = dp(440)
					pick = d.addPeoplePicker(gtx, a)
				})
				shot() // sizes pick
				in := dp(3)
				box := image.Rect(20, 20, 20+dp(440), 20+max(pick.Size.Y, dp(44))).Inset(in)
				// the editor's row: 28dp inside the 6/8dp inset
				row := image.Rect(20+dp(8), 20+dp(6), 20+dp(440)-dp(8), 20+dp(6)+dp(28))
				carettest.Check(t, carettest.Field{Name: "agents picker", Ed: &a.q.ed, Shot: shot, Box: box,
					Size: 15, Scale: sc, Placeholder: a.q.placeholder(), Th: th, LineBox: row})

				// channel settings' posting permissions picker
				// (cdPeoplePick: "Select people…", a 40dp field)
				var k cdPeoplePick
				k.init(nsChanOptions.T("Select people…"))
				shot = caretShooter(t, sc, w, h, &k.q.ed, func() color.NRGBA { return d.p.Surface }, func(gtx C) {
					d.base.frame()
					gtx.Constraints.Max.X = dp(440)
					pick = k.layout(gtx, d)
				})
				shot()
				box = image.Rect(20, 20, 20+dp(440), 20+max(pick.Size.Y, dp(40))).Inset(in)
				carettest.Check(t, carettest.Field{Name: "posting picker", Ed: &k.q.ed, Shot: shot, Box: box,
					Size: 15, Scale: sc, Placeholder: k.q.placeholder(), Th: th, LineBox: row})

				// the views' search box (kit.go searchBox: Find members,
				// the directories, Files, huddles)
				s := newSearch("Search all files")
				shot = caretShooter(t, sc, w, h, &s.ed, func() color.NRGBA { return d.p.Bg }, func(gtx C) {
					d.base.frame()
					gtx.Constraints.Max.X = dp(440)
					d.searchBox(gtx, s, 36)
				})
				box = image.Rect(20+dp(10)+dp(18), 20, 20+dp(440), 20+dp(36)).Inset(in)
				carettest.Check(t, carettest.Field{Name: "search box", Ed: &s.ed, Shot: shot, Box: box,
					Size: 15, Scale: sc, Placeholder: s.placeholder(), Th: th})
			})
		}
	}
}
