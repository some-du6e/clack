//go:build windows

package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
	"golang.org/x/sys/windows"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

var (
	tbGDI32                  = windows.NewLazySystemDLL("gdi32.dll")
	tbSendMessage            = trayTestUser32.NewProc("SendMessageW")
	tbSetWindowPos           = trayTestUser32.NewProc("SetWindowPos")
	tbGetWindowRect          = trayTestUser32.NewProc("GetWindowRect")
	tbGetClientRect          = trayTestUser32.NewProc("GetClientRect")
	tbGetDpiForWindow        = trayTestUser32.NewProc("GetDpiForWindow")
	tbGetSystemMetricsForDpi = trayTestUser32.NewProc("GetSystemMetricsForDpi")
	tbIsZoomed               = trayTestUser32.NewProc("IsZoomed")
	tbGetDC                  = trayTestUser32.NewProc("GetDC")
	tbReleaseDC              = trayTestUser32.NewProc("ReleaseDC")
	tbCreateCompatibleDC     = tbGDI32.NewProc("CreateCompatibleDC")
	tbCreateCompatibleBitmap = tbGDI32.NewProc("CreateCompatibleBitmap")
	tbSelectObject           = tbGDI32.NewProc("SelectObject")
	tbBitBlt                 = tbGDI32.NewProc("BitBlt")
	tbGetDIBits              = tbGDI32.NewProc("GetDIBits")
	tbDeleteObject           = tbGDI32.NewProc("DeleteObject")
	tbDeleteDC               = tbGDI32.NewProc("DeleteDC")
)

const (
	tbWMNCHitTest     = 0x0084
	tbWMGetMinMaxInfo = 0x0024
	tbHTClient        = 1
	tbHTCaption       = 2
	tbHTMinButton     = 8
	tbHTMaxButton     = 9
	tbHTTop           = 12
	tbHTTopRight      = 14
	tbHTClose         = 20
)

type tbRect struct{ Left, Top, Right, Bottom int32 }

func (r tbRect) img() image.Rectangle {
	return image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom))
}

// tbOrigin is the client area's origin on the screen (ClientToScreen of
// a POINT: two int32s).
func tbOrigin(hwnd uintptr) image.Point {
	var p struct{ X, Y int32 }
	asTestClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	return image.Pt(int(p.X), int(p.Y))
}

// tbCapture reads screen rectangle r from the desktop (BitBlt of the
// screen DC, GetDIBits top-down BGRA).
func tbCapture(t *testing.T, r image.Rectangle) *image.RGBA {
	t.Helper()
	w, h := r.Dx(), r.Dy()
	sdc, _, _ := tbGetDC.Call(0)
	defer tbReleaseDC.Call(0, sdc)
	mdc, _, _ := tbCreateCompatibleDC.Call(sdc)
	defer tbDeleteDC.Call(mdc)
	bmp, _, _ := tbCreateCompatibleBitmap.Call(sdc, uintptr(w), uintptr(h))
	defer tbDeleteObject.Call(bmp)
	old, _, _ := tbSelectObject.Call(mdc, bmp)
	const srcCopy, captureBlt = 0x00CC0020, 0x40000000
	if ok, _, err := tbBitBlt.Call(mdc, 0, 0, uintptr(w), uintptr(h), sdc, uintptr(r.Min.X), uintptr(r.Min.Y), srcCopy|captureBlt); ok == 0 {
		t.Fatalf("BitBlt: %v", err)
	}
	tbSelectObject.Call(mdc, old)
	hdr := struct {
		Size                   uint32
		Width, Height          int32
		Planes, BitCount       uint16
		Compression, SizeImage uint32
		XPerMeter, YPerMeter   int32
		ClrUsed, ClrImportant  uint32
		_                      [4]uint32 // room for BITMAPINFO's colour table
	}{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	px := make([]byte, w*h*4)
	if n, _, err := tbGetDIBits.Call(mdc, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&px[0])), uintptr(unsafe.Pointer(&hdr)), 0); int(n) != h {
		t.Fatalf("GetDIBits: %d lines, %v", n, err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = px[i*4+2], px[i*4+1], px[i*4], 0xff
	}
	return img
}

// tbSave writes img to CLACK_TITLEBAR_SHOTS/name.png.
func tbSave(t *testing.T, name string, img image.Image) {
	t.Helper()
	dir := os.Getenv("CLACK_TITLEBAR_SHOTS")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Log(err)
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Log(err)
		return
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Log(err)
	}
	t.Logf("screenshot %s", f.Name())
}

func tbLuma(c color.RGBA) int { return (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000 }

func tbNear(a, b color.RGBA, tol int) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol
}

// TestTitleBarWindow is App.Run's own window on a real Windows desktop
// (CLACK_WINDOWS_TITLEBAR_TESTS=1, ci's windows-applog job), signed in to
// internal/fakeslack, as Slack's frameless window (gap 2, W34):
//
//  1. No OS caption: the client area starts at the window's top, the
//     frame thickness left, right and below; WM_GETMINMAXINFO keeps
//     Slack's 668×400 client minimum.
//  2. Windows' hit-test codes: Electron's caption buttons (HTMINBUTTON,
//     HTMAXBUTTON for Snap Layouts, HTCLOSE) at their measured places,
//     the nav's empty space HTCAPTION, its widgets and the content
//     HTCLIENT, the top edge HTTOP.
//  3. The pixels (the window's top strip, light and dark, saved to
//     CLACK_TITLEBAR_SHOTS): the three symbols in the theme's symbol
//     colour over the nav, close red under the pointer, maximize's
//     hover darker.
//  4. SendInput: maximize and restore, a double-click on the nav
//     maximizing and restoring, close hiding the window to the tray.
func TestTitleBarWindow(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_TITLEBAR_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_TITLEBAR_TESTS not set")
	}
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	cache := filepath.Join(home, "cache")
	hwnds := make(chan uintptr, 1)
	var frames atomic.Int64
	images := imgcache.New(fs.Fetch, "", nil)
	a := New(Options{ConfigDir: home, NoRealtime: true, Started: time.Now(), OnWindowEvent: func(e event.Event) {
		switch e := e.(type) {
		case app.Win32ViewEvent:
			if e.HWND != 0 {
				select {
				case hwnds <- e.HWND:
				default:
				}
			}
		case app.FrameEvent:
			frames.Add(1)
		}
	}}, images)
	a.Attach(core.New(slack.New(fs.Creds()), store.New(cache)), cache)
	a.SetTray(&fakeTray{})
	done := make(chan error, 1)
	go func() { done <- a.Run() }()
	t.Cleanup(func() {
		a.Quit()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Log("Run didn't return after Quit")
		}
	})
	var hwnd uintptr
	select {
	case hwnd = <-hwnds:
	case <-time.After(60 * time.Second):
		t.Fatal("no Win32ViewEvent")
	}
	on := func(f func()) {
		t.Helper()
		ran := make(chan struct{})
		a.Post(func() { f(); close(ran) })
		select {
		case <-ran:
		case err := <-done:
			t.Fatalf("Run returned (%v)", err)
		case <-time.After(20 * time.Second):
			t.Fatal("posted work didn't run")
		}
	}
	until := func(what string, ok func() bool) {
		t.Helper()
		for dl := time.Now().Add(30 * time.Second); !ok(); time.Sleep(50 * time.Millisecond) {
			if time.Now().After(dl) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	settle := func() {
		f := frames.Load()
		until("frames", func() bool { return frames.Load() > f+3 })
		time.Sleep(400 * time.Millisecond)
	}
	until("the first frames", func() bool { return frames.Load() > 2 })
	until("core ready", func() (ok bool) { on(func() { ok = !a.data.booted.IsZero() }); return })
	until("the reveal", func() (ok bool) { on(func() { ok = a.rv.done }); return })
	time.Sleep(2 * time.Second) // boot settles

	dpi, _, _ := tbGetDpiForWindow.Call(hwnd)
	s := float32(dpi) / 96
	metric := func(i uintptr) int32 { r, _, _ := tbGetSystemMetricsForDpi.Call(i, dpi); return int32(r) }
	ft := metric(32) + metric(92) // SM_CXSIZEFRAME + SM_CXPADDEDBORDER
	// a window that fits the runner's screen
	cx, _, _ := asTestGetSystemMetrics.Call(0)
	cy, _, _ := asTestGetSystemMetrics.Call(1)
	ww, wh := min(int(cx)-40, 1200), min(int(cy)-60, 760)
	tbSetWindowPos.Call(hwnd, 0, 16, 16, uintptr(ww), uintptr(wh), 0x0004|0x0010) // SWP_NOZORDER|SWP_NOACTIVATE
	settle()
	asTestSetForeground.Call(hwnd)

	var wr, cr tbRect
	tbGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
	tbGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
	org := tbOrigin(hwnd)
	t.Logf("dpi %d, frame thickness %d, window %v, client origin %v size %dx%d, screen %dx%d", dpi, ft, wr.img(), org, cr.Right, cr.Bottom, cx, cy)

	// 1. the frame
	if int32(org.Y) != wr.Top {
		t.Errorf("the client area starts %d px under the window's top: the OS caption is still there", int32(org.Y)-wr.Top)
	}
	if l, r, b := int32(org.X)-wr.Left, wr.Right-(int32(org.X)+cr.Right), wr.Bottom-(int32(org.Y)+cr.Bottom); l != ft || r != ft || b != ft {
		t.Errorf("borders left %d right %d bottom %d, want the frame thickness %d", l, r, b, ft)
	}
	var mmi struct{ Reserved, MaxSize, MaxPosition, MinTrack, MaxTrack struct{ X, Y int32 } }
	tbSendMessage.Call(hwnd, tbWMGetMinMaxInfo, 0, uintptr(unsafe.Pointer(&mmi)))
	px := func(dp float32) int32 { return int32(dp*s + .5) }
	if mmi.MinTrack.X != px(mainMinW)+2*ft || mmi.MinTrack.Y != px(mainMinH)+ft {
		t.Errorf("minimum track size %dx%d, want the 668x400 client (%dx%d) plus the frame", mmi.MinTrack.X, mmi.MinTrack.Y, px(mainMinW)+2*ft, px(mainMinH)+ft)
	}

	// 2. hit tests
	W := int(cr.Right)
	var g wcoGeo
	var pill image.Rectangle
	on(func() { g, pill = a.wco.geo, a.searchPill })
	if want := wcoGeometry(W, s, 39, false); g != want {
		t.Errorf("the caption buttons at %+v, want Electron's %+v", g.bounds, want.bounds)
	}
	t.Logf("caption buttons %v, backgrounds %v, symbols %v, stroke %d; search %v", g.bounds, g.bg, g.sym, g.stroke, pill)
	hit := func(p image.Point) uintptr {
		sx, sy := org.X+p.X, org.Y+p.Y
		r, _, _ := tbSendMessage.Call(hwnd, tbWMNCHitTest, 0, uintptr(uint16(sx))|uintptr(uint16(sy))<<16)
		return r
	}
	emptyNav := image.Pt((int(76*s)+pill.Min.X-int(99*s))/2, int(20*s))
	for _, c := range []struct {
		what string
		p    image.Point
		want uintptr
	}{
		{"minimize", wcoCentre(g.bounds[0]), tbHTMinButton},
		{"maximize", wcoCentre(g.bounds[1]), tbHTMaxButton},
		{"close", wcoCentre(g.bounds[2]), tbHTClose},
		{"the nav's empty space", emptyNav, tbHTCaption},
		{"the search box", wcoCentre(pill), tbHTClient},
		{"the app menu button", image.Pt(int(21*s), int(20*s)), tbHTClient},
		{"the content", image.Pt(W/2, int(300*s)), tbHTClient},
		{"the top edge", image.Pt(W/2, 1), tbHTTop},
		{"the top edge over close", image.Pt(W-2, 0), tbHTTopRight},
	} {
		if got := hit(c.p); got != c.want {
			t.Errorf("WM_NCHITTEST over %s (%v): %d, want %d", c.what, c.p, got, c.want)
		}
	}

	// 3. the pixels
	strip := func() *image.RGBA {
		return tbCapture(t, image.Rect(org.X, org.Y, org.X+W, org.Y+int(56*s)))
	}
	at := func(img *image.RGBA, p image.Point) color.RGBA { return img.RGBAAt(p.X, p.Y) }
	checkSymbols := func(img *image.RGBA, mode string, sym color.RGBA) {
		t.Helper()
		nav := at(img, image.Pt(g.bg[1].Min.X+int(3*s), g.bg[1].Min.Y+int(3*s)))
		if mode == "light" && tbLuma(at(img, image.Pt(W/2, 3))) > 0xc0 {
			t.Errorf("%s: the window's top is light (%v): an OS caption?", mode, at(img, image.Pt(W/2, 3)))
		}
		for i, p := range []image.Point{
			{g.sym[0].Min.X + g.sym[0].Dx()/2, g.sym[0].Min.Y + g.sym[0].Dy()/2}, // minimize's line
			{g.sym[1].Min.X + g.sym[1].Dx()/2, g.sym[1].Min.Y},                   // maximize's top edge
			{g.sym[2].Min.X + g.sym[2].Dx()/2, g.sym[2].Min.Y + g.sym[2].Dy()/2}, // close's crossing
		} {
			c := at(img, p)
			if !tbNear(c, sym, 0x50) || tbLuma(c)-tbLuma(nav) < 60 {
				t.Errorf("%s: symbol %d at %v is %v over the nav's %v, want ~%v", mode, i, p, c, nav, sym)
			}
		}
	}
	light := strip()
	tbSave(t, "titlebar-light", light)
	checkSymbols(light, "light", color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})

	// close hovered: Windows' red; maximize hovered: darker
	bgAt := func(g wcoGeo, i int) image.Point { return image.Pt(g.bg[i].Min.X+int(3*s), g.bg[i].Min.Y+int(3*s)) }
	maxBg, closeBg := bgAt(g, 1), bgAt(g, 2)
	scr := func(p image.Point) image.Point { return image.Pt(org.X+p.X, org.Y+p.Y) }
	asSend(t, asMouse(scr(wcoCentre(g.bounds[2])), 0))
	time.Sleep(500 * time.Millisecond)
	hc := strip()
	tbSave(t, "titlebar-light-close-hover", hc)
	if c := at(hc, closeBg); !tbNear(c, color.RGBA{R: 0xe8, G: 0x11, B: 0x23, A: 0xff}, 12) {
		t.Errorf("close hovered: %v, want #E81123", c)
	}
	asSend(t, asMouse(scr(wcoCentre(g.bounds[1])), 0))
	time.Sleep(1500 * time.Millisecond) // long enough for Windows 11's Snap Layouts flyout
	hm := strip()
	tbSave(t, "titlebar-light-maximize-hover", hm)
	tbSave(t, "titlebar-snap-layouts", tbCapture(t, image.Rect(org.X+W/2, org.Y, org.X+W, org.Y+int(360*s))))
	if before, after := tbLuma(at(light, maxBg)), tbLuma(at(hm, maxBg)); after >= before {
		t.Errorf("maximize hovered: luma %d, not darker than %d (black at 0x1A over a light frame)", after, before)
	}
	if c := at(hm, closeBg); tbNear(c, color.RGBA{R: 0xe8, G: 0x11, B: 0x23, A: 0xff}, 30) {
		t.Errorf("close still red (%v) once the pointer left it", c)
	}

	// 4. maximize, restore
	zoomed := func() bool { r, _, _ := tbIsZoomed.Call(hwnd); return r != 0 }
	click := func(p image.Point) {
		q := scr(p)
		asSend(t, asMouse(q, 0), asMouse(q, asLeftDown), asMouse(q, asLeftUp))
	}
	click(wcoCentre(g.bounds[1]))
	until("maximized by the maximize button", zoomed)
	settle()
	tbGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
	org = tbOrigin(hwnd)
	W = int(cr.Right)
	var gm wcoGeo
	on(func() { gm = a.wco.geo })
	if want := wcoGeometry(W, s, 39, true); gm != want {
		t.Errorf("maximized: the caption buttons at %v, want %v", gm.bounds, want.bounds)
	}
	tbSave(t, "titlebar-light-maximized", tbCapture(t, image.Rect(org.X, org.Y, org.X+W, org.Y+int(56*s))))
	if got := hit(image.Pt(W/2, 1)); got != tbHTCaption {
		t.Errorf("maximized: the top edge is %d, want HTCAPTION", got)
	}
	if got := hit(wcoCentre(gm.bounds[1])); got != tbHTMaxButton {
		t.Errorf("maximized: restore is %d, want HTMAXBUTTON", got)
	}
	click(wcoCentre(gm.bounds[1]))
	until("restored by the restore button", func() bool { return !zoomed() })
	settle()
	tbGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
	org = tbOrigin(hwnd)
	W = int(cr.Right)
	on(func() { g, pill = a.wco.geo, a.searchPill })

	// a double-click on the nav: Windows maximizes, and restores
	emptyNav = image.Pt((int(76*s)+pill.Min.X-int(99*s))/2, int(20*s))
	dbl := func() {
		q := scr(emptyNav)
		asSend(t, asMouse(q, 0), asMouse(q, asLeftDown), asMouse(q, asLeftUp), asMouse(q, asLeftDown), asMouse(q, asLeftUp))
	}
	dbl()
	until("maximized by a double-click on the nav", zoomed)
	settle()
	tbGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
	org = tbOrigin(hwnd)
	on(func() { pill = a.searchPill })
	emptyNav = image.Pt((int(76*s)+pill.Min.X-int(99*s))/2, int(20*s))
	dbl()
	until("restored by a double-click on the nav", func() bool { return !zoomed() })
	settle()
	tbGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
	org = tbOrigin(hwnd)
	W = int(cr.Right)
	on(func() { g = a.wco.geo })

	// dark: the dark theme's top_nav_text, #D1D2D3
	on(func() { a.th.SetDark(true); a.Invalidate() })
	asSend(t, asMouse(scr(image.Pt(W/2, int(300*s))), 0))
	settle()
	dark := strip()
	tbSave(t, "titlebar-dark", dark)
	checkSymbols(dark, "dark", color.RGBA{R: 0xd1, G: 0xd2, B: 0xd3, A: 0xff})
	maxBg = bgAt(g, 1)
	asSend(t, asMouse(scr(wcoCentre(g.bounds[1])), 0))
	time.Sleep(500 * time.Millisecond)
	dm := strip()
	tbSave(t, "titlebar-dark-maximize-hover", dm)
	if before, after := tbLuma(at(dark, maxBg)), tbLuma(at(dm, maxBg)); after <= before {
		t.Errorf("dark: maximize hovered luma %d, not lighter than %d (white at 0x1A over a dark frame)", after, before)
	}

	// close: the window goes to the tray (runFromTray), Raise brings it back
	visible := func() bool { r, _, _ := trayTestIsVisible.Call(hwnd); return r != 0 }
	click(wcoCentre(g.bounds[2]))
	until("hidden to the tray by the close button", func() bool { return !visible() })
	if r, _, _ := trayTestIsWindow.Call(hwnd); r == 0 {
		t.Fatal("the close button destroyed the window")
	}
	a.Raise()
	until("shown again by Raise", visible)
}
