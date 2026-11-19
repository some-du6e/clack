package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
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
			// a held press: the one frame that delivers it (a window gets
			// no other while the button is down)
			pt := f32.Pt(float32(mid.X), float32(mid.Y))
			e.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt})
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
// theme (gray, brightness 6): #f8f8f8 with a #1d1c1d moon in light mode,
// #e0e0e0 with #101214 in dark, not the main pane's colours; hovered,
// :hover's blended surf-inv-pry-hover layer over the base-pry (#eae9ea,
// #e2e2e2). Drawn and sampled: the disc beside the moon, the moon's ink.
func TestAccountThemeFocusButtonActive(t *testing.T) {
	for _, c := range []struct {
		dark            bool
		fill, ink, hovd color.NRGBA
	}{
		{false, tcRGB(0xf8f8f8), tcRGB(0x1d1c1d), tcRGB(0xeae9ea)},
		{true, tcRGB(0xe0e0e0), tcRGB(0x101214), tcRGB(0xe2e2e2)},
	} {
		a, ca, _ := fakeApp(t, map[string]any{"focus_mode_expiration_ts": time.Now().Add(90 * time.Minute).Unix()})
		ca.SetExperimentGroup("silent_sidebar", "on")
		ft, ok := a.focusTheme()
		if !ok || !a.focusModeActive() {
			t.Fatal("focus mode isn't on")
		}
		theme.CachedFields(ft, c.dark, true) // restyle's FieldsAsync then has them at once
		a.th.Dark = c.dark
		a.th.Refresh()
		w := newTCWin(t, a, func(gtx layout.Context) { a.focusButton(gtx, image.Pt(8, 8), 36) })
		w.later(300 * time.Millisecond)
		img := w.shot()
		if got := img.RGBAAt(tcDisc.X, tcDisc.Y); !tcNear(got, c.fill, 2) {
			t.Errorf("dark=%v: the active focus button is %s; Slack theme-base-pry %s (the gray silent theme's rail scope)", c.dark, tcHex(tcNRGBA(got)), tcHex(c.fill))
		}
		if k, off, peak := tcAlong(img, tcIcon, c.fill, c.ink); k < .85 || k > 1.1 || off > 16 {
			t.Errorf("dark=%v: the moon's ink peaks at %s (%.2f of the way to Slack's theme-content-ter %s)", c.dark, tcHex(peak), k, tcHex(c.ink))
		}
		w.ptr(pointer.Move, tcMid)
		w.later(300 * time.Millisecond)
		if got := w.shot().RGBAAt(tcDisc.X, tcDisc.Y); !tcNear(got, c.hovd, 1) || tcNear(got, c.fill, 0) { // the layer moves the dark discs by 1-2 levels only
			t.Errorf("dark=%v: the hovered active focus button is %s; Slack's :hover layer over theme-base-pry %s", c.dark, tcHex(tcNRGBA(got)), tcHex(c.hovd))
		}
	}
}

// A press on the control strip's focus and + buttons shows :active while
// it's held: theme-base-pry with theme-content-ter, hovered :hover's layer
// over it (the default theme's rail scope: #efdef6 and #39063a light,
// #e4d0ec and #200221 dark), and dragged off the button, the :active disc
// without the layer (#f9edff, #e3ceeb), as Chrome keeps :active until
// mouseup. A window gets the frame that delivers the press and then only
// the frames the app asks for: the press's own frame must start the
// 125 ms transition and ask for the next one (the buttons read the
// Clickable before its update and missed the press; dropping the
// transition's InvalidateCmd leaves the held button on the hover tile).
// The focus button's tooltip asks for frames of its own, so the + one is
// the strict case.
func TestAccountThemeControlStripPress(t *testing.T) {
	for _, b := range []struct {
		name string
		draw func(a *App, gtx layout.Context)
	}{
		{"focus", func(a *App, gtx layout.Context) { a.focusButton(gtx, image.Pt(8, 8), 36) }},
		{"+", func(a *App, gtx layout.Context) { a.createButton(gtx, image.Pt(8, 8), 36) }},
	} {
		for _, c := range []struct {
			dark            bool
			fill, ink, rest color.NRGBA
		}{
			{false, tcRGB(0xefdef6), tcRGB(0x39063a), tcRGB(0xf9edff)},
			{true, tcRGB(0xe4d0ec), tcRGB(0x200221), tcRGB(0xe3ceeb)},
		} {
			a, _, _ := fakeApp(t, nil)
			a.th.Dark = c.dark
			a.th.Refresh()
			w := newTCWin(t, a, func(gtx layout.Context) { b.draw(a, gtx) })
			w.ptr(pointer.Move, tcMid)
			w.later(300 * time.Millisecond)
			w.later(300 * time.Millisecond) // hovered, settled
			w.run()
			w.ptr(pointer.Press, tcMid) // held from here
			if !w.soon(20 * time.Millisecond) {
				t.Errorf("%s dark=%v: the press's frame asks for no other, so the held press never shows", b.name, c.dark)
			}
			w.run()
			img := w.shot()
			if got := img.RGBAAt(tcDisc.X, tcDisc.Y); !tcNear(got, c.fill, 2) {
				t.Errorf("%s dark=%v: held, the button is %s; Slack's :active (hovered) %s", b.name, c.dark, tcHex(tcNRGBA(got)), tcHex(c.fill))
			}
			if k, off, peak := tcAlong(img, tcIcon, c.fill, c.ink); k < .8 || k > 1.1 || off > 16 {
				t.Errorf("%s dark=%v: held, its icon's ink peaks at %s (%.2f of the way to Slack's theme-content-ter %s)", b.name, c.dark, tcHex(peak), k, tcHex(c.ink))
			}
			w.drag(image.Pt(50, 50)) // off the button, still held
			w.run()
			w.later(300 * time.Millisecond)
			if got := w.shot().RGBAAt(tcDisc.X, tcDisc.Y); !tcNear(got, c.rest, 1) {
				t.Errorf("%s dark=%v: held and dragged off, the button is %s; Slack keeps :active, theme-base-pry %s", b.name, c.dark, tcHex(tcNRGBA(got)), tcHex(c.rest))
			}
		}
	}
}

// The peek cards' header rule (.p-peek_card__header: box-shadow 0 1px 0 0
// rgba(var(--sk_foreground_low), .13)) is Slack Kit's 232,232,232 under
// .sk-client-theme--dark (209,210,211 is --sk_primary_foreground) and
// 29,28,29 in light mode.
func TestPeekHeaderRule(t *testing.T) {
	for _, c := range []struct {
		dark bool
		want color.NRGBA
	}{
		{false, color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0x21}},
		{true, color.NRGBA{R: 0xe8, G: 0xe8, B: 0xe8, A: 0x21}},
	} {
		a, _, _ := fakeApp(t, nil)
		a.th.Dark = c.dark
		a.th.Refresh()
		if got := a.agentsPal().rule; got != c.want {
			t.Errorf("dark=%v: the peek header's rule is %v; Slack rgba(var(--sk_foreground_low),.13) %v", c.dark, got, c.want)
		}
	}
}

// The control strip test buttons' geometry: a 36px button at (8, 8) in a
// 52x52 window; tcDisc is inside its disc left of the icon, tcIcon the
// icon's box (with room for the hover's scale and tilt).
var (
	tcMid  = image.Pt(26, 26)
	tcDisc = image.Pt(13, 26)
	tcIcon = image.Rect(15, 15, 37, 37)
)

func tcRGB(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

// tcWin runs a's frames through a router at a clock the test moves, as a
// window gets them: a frame per delivered event, then only the ones the
// app asks for (the router's wakeup, read after every frame as a window
// reads it).
type tcWin struct {
	t      *testing.T
	a      *App
	size   image.Point
	r      input.Router
	ops    op.Ops
	now    time.Time
	draw   func(gtx layout.Context)
	wake   time.Time // the frame the last one asked for, while wakeOK
	wakeOK bool
}

// newTCWin lays draw out over the chrome's base colour in a 52x52 window
// and runs its first frame.
func newTCWin(t *testing.T, a *App, draw func(gtx layout.Context)) *tcWin {
	t.Helper()
	w := &tcWin{t: t, a: a, size: image.Pt(52, 52), now: time.Now(), draw: draw}
	a.winSize = w.size
	w.frame()
	return w
}

func (w *tcWin) frame() {
	w.ops.Reset()
	gtx := layout.Context{Ops: &w.ops, Now: w.now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(w.size), Source: w.r.Source()}
	paint.FillShape(&w.ops, w.a.chromeBase(image.Pt(w.size.X/2, w.size.Y/2)), clip.Rect{Max: w.size}.Op())
	w.draw(gtx)
	w.r.Frame(&w.ops)
	w.wake, w.wakeOK = w.r.WakeupTime()
}

// soon reports whether the last frame asked for another within d.
func (w *tcWin) soon(d time.Duration) bool { return w.wakeOK && !w.wake.After(w.now.Add(d)) }

// run runs the frames the app asks for, each at its wakeup and at least
// 16 ms after the last, until it asks for none within 50 ms (or 120).
func (w *tcWin) run() {
	for i := 0; i < 120 && w.soon(50*time.Millisecond); i++ {
		next := w.now.Add(16 * time.Millisecond)
		if w.wake.After(next) {
			next = w.wake
		}
		w.now = next
		w.frame()
	}
}

// ptr queues a mouse event at p (a press with the primary button) and runs
// the frame that delivers it.
func (w *tcWin) ptr(kind pointer.Kind, p image.Point) {
	ev := pointer.Event{Kind: kind, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))}
	if kind == pointer.Press {
		ev.Buttons = pointer.ButtonPrimary
	}
	w.r.Queue(ev)
	w.frame()
}

// drag moves the pointer to p with the primary button held (the router
// makes it a Drag; a queued pointer.Drag panics) and runs that frame.
func (w *tcWin) drag(p image.Point) {
	w.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(float32(p.X), float32(p.Y))})
	w.frame()
}

// later runs a frame d on (the transitions the last one started done).
func (w *tcWin) later(d time.Duration) {
	w.now = w.now.Add(d)
	w.frame()
}

func (w *tcWin) shot() *image.RGBA { return pixeltest.Render(w.t, w.size.X, w.size.Y, &w.ops) }

// tcAlong is the most-covered ink pixel in r (the one farthest from bg)
// placed on the line from bg to ink: a is how far along it lies (1: ink,
// 0: nothing drawn), off its distance from the line in levels.
func tcAlong(img *image.RGBA, r image.Rectangle, bg, ink color.NRGBA) (a, off float64, peak color.NRGBA) {
	ab := func(v int) int {
		if v < 0 {
			return -v
		}
		return v
	}
	far := -1
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if d := ab(int(c.R)-int(bg.R)) + ab(int(c.G)-int(bg.G)) + ab(int(c.B)-int(bg.B)); d > far {
				far, peak = d, tcNRGBA(c)
			}
		}
	}
	dv := [3]float64{float64(ink.R) - float64(bg.R), float64(ink.G) - float64(bg.G), float64(ink.B) - float64(bg.B)}
	pv := [3]float64{float64(peak.R) - float64(bg.R), float64(peak.G) - float64(bg.G), float64(peak.B) - float64(bg.B)}
	dd := dv[0]*dv[0] + dv[1]*dv[1] + dv[2]*dv[2]
	if dd == 0 {
		return 0, 0, peak
	}
	a = (pv[0]*dv[0] + pv[1]*dv[1] + pv[2]*dv[2]) / dd
	var res float64
	for i := range pv {
		e := pv[i] - a*dv[i]
		res += e * e
	}
	return a, math.Sqrt(res), peak
}
