package ui

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Draw-site checks for the account theme's colours: what the client
// paints, sampled off the rendered frame, against Slack's values (Slack's
// tokens for the scope, from Slack's own code: theme
// TestThemeInkMatchesSlack's table; Slack desktop's preload for the app
// menu). They need a GPU: CI runs them in prefs-colors.yml with
// CLACK_PIXEL_TESTS=1; elsewhere they skip.

const (
	tcPickerIA = `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":2,"sidebarInverted":false,"useCustomHex":true}`
)

func tcHex(c color.NRGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// tcNear reports whether the drawn pixel is within tol per channel of
// want (opaque).
func tcNear(got color.RGBA, want color.NRGBA, tol int) bool {
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return d(got.R, want.R) <= tol && d(got.G, want.G) <= tol && d(got.B, want.B) <= tol
}

func tcNRGBA(c color.RGBA) color.NRGBA { return color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0xff} }

// tcGPU gives the env a headless GPU window (skipping without one, unless
// CLACK_PIXEL_TESTS asks for it).
func tcGPU(t *testing.T, e *perfEnv) {
	t.Helper()
	if e.gpu != nil {
		return
	}
	w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
	if err != nil {
		if os.Getenv("CLACK_PIXEL_TESTS") != "" {
			t.Fatalf("headless GPU: %v", err)
		}
		t.Skipf("no headless GPU (%v); CI runs this with CLACK_PIXEL_TESTS=1", err)
	}
	e.gpu = w
}

func (e *perfEnv) tcShot(t *testing.T) *image.RGBA {
	t.Helper()
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	return img
}

// The shell's own lines and boxes under the account theme, drawn: the
// sidebar's pagesDivider (rgba(var(--sk_foreground_low), .13), Slack Kit's
// 232,232,232 under .sk-client-theme--dark and --light-inverted-sidebar,
// 29,28,29 at a light root), the sidebar filter box (.c-filter_input--themed:
// theme-surf-inv-sec over the sidebar; its 1px theme-otl-inv-pry border
// inside the 28px box, over that fill, so the box's top row is the
// border's colour) and the
// app menu button (Slack desktop's window-chrome-app-menu: white at .15 on
// :hover and .2 on :active in every theme, not the history buttons'
// theme-surf-inv-pry / -inv-sec tiles). Karim's #FED4BE as Slack's picker
// stores it has a light sidebar: dark lines, a white box with a dark edge.
func TestAccountThemeDrawSites(t *testing.T) {
	sz := fakeslack.Small
	sz.Unreads = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	tcGPU(t, e)
	e.waitReady()
	white := func(a uint8) color.NRGBA { return color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: a} }
	for _, c := range []struct {
		name, ia            string
		dark                bool
		rule, boxBg, boxEdg color.NRGBA // Slack's sk_foreground_low .13, surf-inv-sec, otl-inv-pry
	}{
		{"karim picker light", tcPickerIA, false,
			color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0x21}, white(0xe6), color.NRGBA{R: 0x41, G: 0x28, B: 0x1a, A: 0x21}},
		{"default light", "", false,
			color.NRGBA{R: 0xe8, G: 0xe8, B: 0xe8, A: 0x21}, color.NRGBA{R: 0xf9, G: 0xed, B: 0xff, A: 0x1c}, color.NRGBA{R: 0xf9, G: 0xed, B: 0xff, A: 0x38}},
		{"default dark", "", true,
			color.NRGBA{R: 0xe8, G: 0xe8, B: 0xe8, A: 0x21}, color.NRGBA{R: 0x10, G: 0x12, B: 0x14, A: 0x99}, color.NRGBA{R: 0xef, G: 0xe1, B: 0xf5, A: 0x38}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e.useAccountTheme(c.ia, c.dark) // theme_contrast_shots_test.go
			e.openChannel(fakeslack.JoinBannerID)
			e.movePtr(e.listPoint())
			e.settle(300 * time.Millisecond)
			a := e.a
			s := &a.sidebar
			gtx := e.context()
			w := gtx.Dp(unit.Dp(s.width))
			img := e.tcShot(t)

			// the pagesDivider: the first divider row, its line 12px down
			y := -1
			for p, i := range s.kb.order {
				if i < len(s.rows) && s.rows[i].kind == sbDivider && p < len(s.kb.rowOff) {
					y = s.origin.Y + s.listTop + s.kb.rowOff[p] - s.scrollTop() + gtx.Dp(12)
					break
				}
			}
			if y < 0 {
				t.Fatal("no pagesDivider row in the sidebar")
			}
			if got, want := img.RGBAAt(s.origin.X+w/2, y), theme.Over(c.rule, a.sbBase); !tcNear(got, want, 2) {
				t.Errorf("pagesDivider %s; Slack rgba(sk_foreground_low,.13) %v over the sidebar %s = %s", tcHex(tcNRGBA(got)), c.rule, tcHex(a.sbBase), tcHex(want))
			}

			// the filter box (sidebar.go: 8px in, under the header, 28px)
			if a.sbFilterOn() {
				hh := gtx.Dp(49)
				bx, by := s.origin.X+gtx.Dp(8), s.origin.Y+hh
				if got, want := img.RGBAAt(bx+gtx.Dp(100), by+gtx.Dp(3)), theme.Over(c.boxBg, a.sbBase); !tcNear(got, want, 2) {
					t.Errorf("filter box fill %s; Slack theme-surf-inv-sec %v over the sidebar = %s", tcHex(tcNRGBA(got)), c.boxBg, tcHex(want))
				}
				fill := theme.Over(c.boxBg, a.sbBase)
				if got, want := img.RGBAAt(bx+gtx.Dp(100), by), theme.Over(c.boxEdg, fill); !tcNear(got, want, 2) {
					t.Errorf("filter box border (its top row) %s; Slack theme-otl-inv-pry %v over the box's fill %s = %s", tcHex(tcNRGBA(got)), c.boxEdg, tcHex(fill), tcHex(want))
				}
				// the row above is the sidebar's, as the one above that (the
				// gradient moves less than a level in 2px): no half of a
				// stroke across the edge
				if got, sb := img.RGBAAt(bx+gtx.Dp(100), by-1), img.RGBAAt(bx+gtx.Dp(100), by-3); !tcNear(got, tcNRGBA(sb), 2) {
					t.Errorf("above the filter box %s; Slack's border stays inside the box: the sidebar %s", tcHex(tcNRGBA(got)), tcHex(tcNRGBA(sb)))
				}
			} else {
				t.Log("sidebar_text_filter off for the fake account: no filter box")
			}

			// the app menu button (Linux and Windows): its corner pixel at
			// rest, hovered and pressed
			if !hasAppMenuButton() {
				return
			}
			b := gtx.Dp(26)
			org := image.Pt(gtx.Dp(8), (gtx.Dp(theme.TopNavH)-b)/2)
			mid, probe := org.Add(image.Pt(b/2, b/2)), org.Add(image.Pt(gtx.Dp(2), gtx.Dp(2)))
			rest := tcNRGBA(img.RGBAAt(probe.X, probe.Y))
			e.movePtr(mid)
			e.settle(200 * time.Millisecond)
			if got, want := e.tcShot(t).RGBAAt(probe.X, probe.Y), theme.Over(white(0x26), rest); !tcNear(got, want, 2) {
				t.Errorf("app menu :hover tile %s over %s; Slack desktop rgba(255,255,255,.15) = %s", tcHex(tcNRGBA(got)), tcHex(rest), tcHex(want))
			}
			pt := f32.Pt(float32(mid.X), float32(mid.Y))
			e.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt})
			e.frame()
			e.frame()
			if got, want := e.tcShot(t).RGBAAt(probe.X, probe.Y), theme.Over(white(0x33), rest); !tcNear(got, want, 2) {
				t.Errorf("app menu :active tile %s over %s; Slack desktop rgba(255,255,255,.2) = %s", tcHex(tcNRGBA(got)), tcHex(rest), tcHex(want))
			}
			e.r.Queue(pointer.Event{Kind: pointer.Cancel, Source: pointer.Mouse, Position: pt}) // no click: the menu stays shut
			e.movePtr(e.listPoint())
			e.settle(200 * time.Millisecond)
		})
	}
	e.a.applyAccountPrefs(core.Prefs{})
}

// The focus mode button while focus mode is on is
// .p-control_strip__circle_button--active: the rail scope's
// theme-base-pry with theme-content-ter under focus mode's silent sidebar
// theme (gray, brightness 6): #f8f8f8 in light mode, #e0e0e0 in dark,
// not the main pane's background. Drawn and sampled beside its moon.
func TestAccountThemeFocusButtonActive(t *testing.T) {
	for _, c := range []struct {
		dark bool
		want color.NRGBA
	}{{false, color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf8, A: 0xff}}, {true, color.NRGBA{R: 0xe0, G: 0xe0, B: 0xe0, A: 0xff}}} {
		a, ca, _ := fakeApp(t, map[string]any{"focus_mode_expiration_ts": time.Now().Add(90 * time.Minute).Unix()})
		ca.SetExperimentGroup("silent_sidebar", "on")
		ft, ok := a.focusTheme()
		if !ok || !a.focusModeActive() {
			t.Fatal("focus mode isn't on")
		}
		theme.CachedFields(ft, c.dark, true) // restyle's FieldsAsync then has them at once
		a.th.Dark = c.dark
		a.th.Refresh()
		cs := 36
		win := image.Pt(52, 52)
		a.winSize = win
		img := tcDraw(t, a, win, nil, func(gtx layout.Context) { a.focusButton(gtx, image.Pt(8, 8), cs) })
		if got := img.RGBAAt(8+6, 8+cs/2); !tcNear(got, c.want, 2) {
			t.Errorf("dark=%v: the active focus button is %s; Slack theme-base-pry %s (the gray silent theme's rail scope)", c.dark, tcHex(tcNRGBA(got)), tcHex(c.want))
		}
	}
}

// tcDraw lays draw out over the chrome's base colour in win (the pointer
// at ptr when not nil, a few frames for its hover), then renders the
// last frame.
func tcDraw(t *testing.T, a *App, win image.Point, ptr *image.Point, draw func(gtx layout.Context)) *image.RGBA {
	t.Helper()
	var r input.Router
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(win), Source: r.Source()}
		paint.FillShape(ops, a.chromeBase(image.Pt(win.X/2, win.Y/2)), clip.Rect{Max: win}.Op())
		draw(gtx)
		r.Frame(ops)
	}
	frame()
	if ptr != nil {
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(ptr.X), float32(ptr.Y))})
	}
	frame()
	frame()
	return pixeltest.Render(t, win.X, win.Y, ops)
}
