package ui

import (
	"image"
	"math"
	"slices"
	"testing"
	"time"

	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/icons"
)

// Karim, α.165 on macOS (a 1920pt window at zoom +1, 1745 CSS px): with
// the sidebar hidden (Cmd+Shift+D) the top nav had no back arrow, and the
// history button's left half looked dotted. Slack's TopNav lays its left
// container out from the window alone: .p-ia4_top_nav__native_ui_spacer
// (24px + 52px / zoom, the traffic lights' room) and a flex basis of
// topNavLeftContainer.width = rail + the sidebar's automatic width - 76
// (useCalculateMainWindowClientLayout), whatever the sidebar's state, with
// back, forward and the clock (HistoryButtons draws SvgIcon "clock",
// 20px) at its end, 26px boxes 32px apart, 9px before the search. Clack
// put the search at the rail's edge with the sidebar hidden and the
// buttons right of nothing: back at x -29, forward and history under the
// traffic lights; and it drew the "history" glyph (Slack's, with three
// dots on its left) where Slack draws the clock.
func TestTopNavSidebarHidden(t *testing.T) {
	e := newPerfEnv(t, false)
	e.waitReady()
	e.a.macNav = true
	size, scale := e.size, e.scale
	t.Cleanup(func() {
		e.a.macNav, e.a.sidebar.hidden, e.a.prefs.Zoom = false, false, 0
		e.size, e.scale = size, scale
		icons.DrawHook = nil
	})
	for _, base := range []float32{1, 2} { // a 1x display and a Retina one
		for _, z := range []int{-1, 0, 1, 2} {
			zf := zoomFactors[z-zoomMin]
			e.a.prefs.Zoom = z
			e.scale = base * zf
			e.size = image.Pt(int(1920*base), int(1064*base))
			m := unit.Metric{PxPerDp: e.scale, PxPerSp: e.scale}
			css := 1920 / float64(zf)
			var shown [3]image.Rectangle
			for _, hidden := range []bool{false, true} {
				e.a.sidebar.hidden = hidden
				e.settle(150 * time.Millisecond)
				var drawn []string
				icons.DrawHook = func(n string) { drawn = append(drawn, n) }
				e.frame()
				icons.DrawHook = nil
				what := map[bool]string{false: "shown", true: "hidden"}[hidden]
				tag := func() string {
					return "base " + ftoa(base) + ", zoom " + itoa(z) + ", sidebar " + what
				}
				btns, pill := e.a.navBtns, e.a.searchPill
				// Slack's order and spacing: back, forward, clock, 26px boxes
				// at a 32px pitch, vertically centred in the 40px nav
				for i, r := range btns {
					if r.Dx() != m.Dp(26) || r.Dy() != m.Dp(26) || r.Min.Y != (m.Dp(40)-m.Dp(26))/2 {
						t.Errorf("%s: button %d is %v, want %dpx square, centred", tag(), i, r, m.Dp(26))
					}
					if i > 0 && r.Min.X-btns[i-1].Min.X != m.Dp(26)+m.Dp(6) {
						t.Errorf("%s: buttons %d and %d at %d and %d, want %dpx apart", tag(), i-1, i, btns[i-1].Min.X, r.Min.X, m.Dp(26)+m.Dp(6))
					}
				}
				gap := 9 // the left container's :after shrunk to ~3.9px, margin_right_25, the clock's margin
				if css > wcoWideW {
					gap = 13 // the container grows: its :after keeps 8px
				}
				if d := pill.Min.X - btns[2].Max.X; d != m.Dp(unit.Dp(gap)) {
					t.Errorf("%s: the clock ends %dpx before the search (%v), want %d", tag(), d, pill, m.Dp(unit.Dp(gap)))
				}
				if css <= wcoWideW {
					// the search: the spacer, then rail + clamp(.24 × (window -
					// rail - 4), 180, 450) - 76, in CSS px
					ex := math.Max(180, math.Min(450, .24*(math.Max(css, 694)-74)))
					want := float64(m.Dp(24)) + 52*float64(base) + (70+ex-76)*float64(e.scale)
					if math.Abs(float64(pill.Min.X)-want) > 1.5 {
						t.Errorf("%s: the search starts at %d px, want Slack's %.1f", tag(), pill.Min.X, want)
					}
				}
				// the traffic lights: the close button 12pt in, the three 52pt
				// wide (they don't zoom)
				if lights := int(math.Ceil(64 * float64(base))); btns[0].Min.X < lights {
					t.Errorf("%s: back at %v, under the traffic lights (they end at %d px)", tag(), btns[0], lights)
				}
				if hidden && btns != shown {
					t.Errorf("%s: the buttons at %v, with the sidebar shown at %v; want them unmoved", tag(), btns, shown)
				}
				shown = btns
				// the history button draws Slack's clock (drawn right to left)
				if !slices.ContainsFunc(windows3(drawn), func(w [3]string) bool { return w == [3]string{"clock", "forward", "back"} }) {
					t.Errorf("%s: the nav drew %v, want clock, forward, back", tag(), drawn)
				}
			}
		}
	}
}

// windows3 is every run of three consecutive names.
func windows3(s []string) [][3]string {
	var out [][3]string
	for i := 0; i+3 <= len(s); i++ {
		out = append(out, [3]string{s[i], s[i+1], s[i+2]})
	}
	return out
}

func ftoa(f float32) string {
	if f == float32(int(f)) {
		return itoa(int(f))
	}
	return itoa(int(f)) + "." + itoa(int(f*10)%10)
}
