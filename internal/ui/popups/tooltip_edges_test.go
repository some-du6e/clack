package popups

// Every tooltip goes through theme.PlaceTooltip (theme/tooltip_place.go):
// a tooltip on a button at each window edge and corner stays 3px inside
// the window (BaseTooltip's allowance), takes the other side when it
// doesn't fit above (tryOppositePosition), and keeps its arrow on the
// button. With TOOLTIP_SHOTS_DIR set (composer-audit-shots) the window is
// saved light and dark at 2x with a tooltip at every edge.

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// edgeTip is a 28px button laid out at its window position with a
// tooltip, its origin known as the pointer would teach it.
type edgeTip struct {
	name  string
	at    image.Point // the button's top-left (dp)
	side  Side
	label string
}

func edgeTips(w, h int) []edgeTip {
	return []edgeTip{
		{"top-left", image.Pt(0, 0), Above, "Add a bookmark"},
		{"top", image.Pt(w/2-14, 0), Above, "Search in this channel"},
		{"top-right", image.Pt(w-28, 0), Above, "More actions"},
		{"left", image.Pt(0, h/2-14), Above, "Close the sidebar"},
		{"right", image.Pt(w-28, h/2-14), Above, "Open the details pane"},
		{"bottom-left", image.Pt(0, h-28), Above, "Attach a file"},
		{"bottom-right", image.Pt(w-28, h-28), Below, "Send now"},
		{"right side", image.Pt(w-28, h/2+40), Right, "Huddle"},
	}
}

// layoutEdgeTips draws the buttons and their tooltips in a w×h dp window
// at scale.
func layoutEdgeTips(t *testing.T, th *theme.Theme, ops *op.Ops, w, h int, scale float32, sts []TipState) []theme.TipPlace {
	t.Helper()
	win := image.Pt(int(float32(w)*scale), int(float32(h)*scale))
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Constraints: layout.Exact(win), Now: time.Now()}
	paint.FillShape(ops, th.C.Bg, clip.Rect{Max: win}.Op())
	defer func(s func() image.Point) { theme.WindowSize = s }(theme.WindowSize)
	theme.WindowSize = func() image.Point { return win }
	var out []theme.TipPlace
	for i, e := range edgeTips(w, h) {
		st := &sts[i]
		st.NoDelay = true
		org := image.Pt(gtx.Dp(unit.Dp(e.at.X)), gtx.Dp(unit.Dp(e.at.Y)))
		st.org.Set(org)
		bs := gtx.Dp(28)
		tr := op.Offset(org).Push(ops)
		paint.FillShape(ops, th.C.SidebarHover, clip.Rect{Max: image.Pt(bs, bs)}.Op())
		Tooltip(gtx, th, st, true, image.Pt(bs, bs), e.side, e.label, "")
		tr.Pop()
		pl := st.last
		pl.Box = pl.Box.Add(org)
		pl.Arrow.C = pl.Arrow.C.Add(org)
		out = append(out, pl)
	}
	return out
}

func TestTooltipKeepsInsideTheWindow(t *testing.T) {
	const w, h = 480, 320
	th := theme.New(false)
	sts := make([]TipState, len(edgeTips(w, h)))
	pls := layoutEdgeTips(t, th, new(op.Ops), w, h, 1, sts)
	in := image.Rect(3, 3, w-3, h-3) // keepInBounds: allowanceX / allowanceY 3
	for i, e := range edgeTips(w, h) {
		pl := pls[i]
		btn := image.Rectangle{Min: e.at, Max: e.at.Add(image.Pt(28, 28))}
		if !pl.Box.In(in) {
			t.Errorf("%s: tooltip %v leaves the window (%v)", e.name, pl.Box, in)
		}
		if e.side == Above || e.side == Below {
			if c := (btn.Min.X + btn.Max.X) / 2; pl.Arrow.C.X != c {
				t.Errorf("%s: the arrow at x %d, want the button's centre %d", e.name, pl.Arrow.C.X, c)
			}
		}
		switch e.name {
		case "top-left", "top", "top-right":
			// no room above: under the button, 8px down, the arrow on top
			if pl.Pos != theme.TipBottom || pl.Box.Min.Y != btn.Max.Y+8 || pl.Arrow.C.Y != pl.Box.Min.Y {
				t.Errorf("%s: %s at %v, arrow %v; want under the button (tryOppositePosition)", e.name, pl.Pos, pl.Box, pl.Arrow.C)
			}
		case "left", "right", "bottom-left":
			if pl.Pos != theme.TipTop || pl.Box.Max.Y != btn.Min.Y-8 || pl.Arrow.C.Y != pl.Box.Max.Y {
				t.Errorf("%s: %s at %v, arrow %v; want 8px over the button", e.name, pl.Pos, pl.Box, pl.Arrow.C)
			}
		case "bottom-right":
			// Below with no room under it: above
			if pl.Pos != theme.TipTop || pl.Box.Max.Y != btn.Min.Y-8 {
				t.Errorf("%s: %s at %v; want over the button", e.name, pl.Pos, pl.Box)
			}
		case "right side":
			// "right" never flips: shifted back inside, over the button
			if pl.Pos != theme.TipRight || pl.Box.Max.X != w-3 {
				t.Errorf("%s: %s at %v; want kept 3px inside the right edge", e.name, pl.Pos, pl.Box)
			}
		}
		switch e.name {
		case "top-left", "left", "bottom-left":
			if pl.Box.Min.X != 3 {
				t.Errorf("%s: box left %d, want 3 (shifted, not clipped)", e.name, pl.Box.Min.X)
			}
		case "top-right", "right", "bottom-right":
			if pl.Box.Max.X != w-3 {
				t.Errorf("%s: box right %d, want %d", e.name, pl.Box.Max.X, w-3)
			}
		}
	}
}

func TestTooltipEdgeShots(t *testing.T) {
	dir := os.Getenv("TOOLTIP_SHOTS_DIR")
	if dir == "" {
		t.Skip("TOOLTIP_SHOTS_DIR not set")
	}
	const w, h = 480, 320
	for _, dark := range []bool{false, true} {
		th := theme.New(dark)
		ops := new(op.Ops)
		sts := make([]TipState, len(edgeTips(w, h)))
		layoutEdgeTips(t, th, ops, w, h, 2, sts)
		img := pixeltest.Render(t, w*2, h*2, ops)
		f, err := os.Create(filepath.Join(dir, "tooltip-edges-"+map[bool]string{false: "light", true: "dark"}[dark]+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
