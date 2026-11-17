package theme

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// The workspace as the window draws it (app.go layoutShell: the light
// shadow or the dark outline, the sidebar, the panel's fill), with nothing
// over the panel, under the transforms Clack draws under besides a
// whole-pixel offset: a scale, an inspect shot's (inspect.Shoot: the crop's
// corner to the origin, ×3200/side, here the density of Karim's 1630 px
// rows) and a fraction of a pixel (animations). The new way against the
// old: there every op must be drawn whole, as the old way draws it, so
// every pixel must come out the same. A split of the panel into abutting
// fills, or of the rings into bands of clips, blends the same colour twice
// along its seams there (the review of 71acc910: up to 25% of the
// background through the panel, notches in the outline).
func TestFrameCostSeams(t *testing.T) {
	if os.Getenv("CLACK_PIXEL_TESTS") == "" {
		t.Skip("CLACK_PIXEL_TESTS not set")
	}
	const w, h = 1100, 700
	k := float32(3200.0 / 1630)
	crop := image.Rect(300, 30, 1050, 520)
	for _, dark := range []bool{false, true} {
		for _, tr := range []struct {
			name string
			a    f32.Affine2D
			out  image.Point
		}{
			{"scaled by 1.37", f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(1.37, 1.37)), image.Pt(int(math.Ceil(w*1.37)), int(math.Ceil(h*1.37)))},
			{"as an inspect shot", f32.Affine2D{}.Offset(f32.Pt(-float32(crop.Min.X), -float32(crop.Min.Y))).Scale(f32.Point{}, f32.Pt(k, k)), image.Pt(int(float32(crop.Dx())*k+.5), int(float32(crop.Dy())*k+.5))},
			{"at a fraction of a pixel", f32.Affine2D{}.Offset(f32.Pt(0.4, 0.7)), image.Pt(w, h)},
		} {
			name := fmt.Sprintf("%s, %s", map[bool]string{false: "light", true: "dark"}[dark], tr.name)
			old := seamShot(t, dark, tr.a, tr.out, true)
			cur := seamShot(t, dark, tr.a, tr.out, false)
			n, m, first := 0, 0, image.Point{}
			for y := 0; y < tr.out.Y; y++ {
				for x := 0; x < tr.out.X; x++ {
					a, b := old.RGBAAt(x, y), cur.RGBAAt(x, y)
					d := max(seamAbs(a.R, b.R), seamAbs(a.G, b.G), seamAbs(a.B, b.B), seamAbs(a.A, b.A))
					if d > 0 {
						if n == 0 || d > m {
							first = image.Pt(x, y)
						}
						n++
						m = max(m, d)
					}
				}
			}
			if n > 0 {
				t.Errorf("%s: %d px differ from the old way, by up to %d levels; the worst at %v is %v, the old way's %v (a seam)", name, n, m, first, cur.RGBAAt(first.X, first.Y), old.RGBAAt(first.X, first.Y))
			} else {
				t.Logf("%s: the same as the old way (%d px)", name, tr.out.X*tr.out.Y)
			}
		}
	}
}

func seamAbs(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// seamShot draws the workspace the old way (legacy) or the new under tr
// into an out-sized window.
func seamShot(t *testing.T, dark bool, tr f32.Affine2D, out image.Point, legacy bool) *image.RGBA {
	t.Helper()
	SetLegacyFrameCost(legacy)
	gpu.ClackLegacyClips(legacy)
	defer SetLegacyFrameCost(false)
	defer gpu.ClackLegacyClips(false)
	th := New(dark)
	sz := image.Pt(1100, 700)
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(sz)}
	st := op.Affine(tr).Push(gtx.Ops)
	chrome := color.NRGBA{R: 0x3f, G: 0x0e, B: 0x40, A: 0xff}
	Fill(gtx, image.Rectangle{Max: sz}, chrome)
	ws := image.Rect(gtx.Dp(RailW), gtx.Dp(TopNavH), sz.X-gtx.Dp(PanelInset), sz.Y-gtx.Dp(PanelInset))
	rad := gtx.Dp(PanelRadius)
	primary := image.Rect(ws.Min.X+gtx.Dp(260), ws.Min.Y, ws.Max.X, ws.Max.Y)
	if !dark {
		Shadow(gtx, ws.Add(image.Pt(-gtx.Dp(2), -gtx.Dp(2))), rad, gtx.Dp(8), th.C.PanelShadow)
	}
	Fill(gtx, image.Rect(ws.Min.X, ws.Min.Y, primary.Min.X, ws.Max.Y), Over(th.C.SidebarBg, chrome))
	func() {
		rr := clip.RRect{Rect: primary, NE: rad, SE: rad}
		defer rr.Push(gtx.Ops).Pop()
		FillPanel(gtx, rr, th.C.Bg)
	}()
	if b := th.C.WorkspaceBorder; b.A > 0 {
		Border(gtx, ws, rad, gtx.Dp(1), Over(b, chrome))
	}
	st.Pop()
	win, err := pixeltest.NewWindow(out.X, out.Y)
	if err != nil {
		t.Fatalf("headless GPU: %v", err)
	}
	defer win.Release()
	for range 2 {
		if err := win.Frame(&ops); err != nil {
			t.Fatal(err)
		}
	}
	img := image.NewRGBA(image.Rectangle{Max: out})
	if err := win.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	return img
}
