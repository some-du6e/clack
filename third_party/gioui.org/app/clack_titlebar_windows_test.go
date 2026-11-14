// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package app

import (
	"encoding/binary"
	"image"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	syscall "golang.org/x/sys/windows"

	"gioui.org/app/internal/windows"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"
)

// Clack patch (CLACK_PATCHES.md §22) on a real Windows desktop
// (CLACK_WINDOWS_TITLEBAR_TESTS, ci's windows-applog job): the frame
// without a caption, the hit-test codes, and the caption buttons' input
// reaching the program's widgets through Windows' own input path
// (SendInput, which moves the runner's real cursor).

var testSendMessage = testUser32.NewProc("SendMessageW")

func titleBarTests(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("opens windows")
	}
	if os.Getenv("CLACK_WINDOWS_TITLEBAR_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_TITLEBAR_TESTS not set")
	}
}

const (
	captionTestH    = 40 // dp
	captionTestBtnW = 46 // dp
)

// captionTestWindow is a Gio window that draws a caption: an ActionMove
// area across its top captionTestH, a plain button inside it (100..160 dp),
// and three buttons at the right declared ActionMinimize, ActionMaximize
// (ActionUnmaximize while maximized) and ActionClose. Each button is a
// widget.Clickable whose clicks and hover the test reads; nothing performs
// the actions.
type captionTestWindow struct {
	w      *Window
	hwnd   syscall.Handle
	frames atomic.Int64
	mode   atomic.Int32
	clicks chan string
	hover  [4]atomic.Bool // the plain button, minimize, maximize, close
	mu     sync.Mutex
	r      [4]image.Rectangle // their bounds, client px
	size   image.Point
	done   chan struct{}
}

var captionTestNames = [4]string{"client", "minimize", "maximize", "close"}

func newCaptionTestWindow(t *testing.T, opts ...Option) *captionTestWindow {
	t.Helper()
	c := &captionTestWindow{w: new(Window), clicks: make(chan string, 32), done: make(chan struct{})}
	c.w.Option(append([]Option{Title("Clack title bar test"), Size(640, 400)}, opts...)...)
	hwnds := make(chan syscall.Handle, 1)
	go func() {
		defer close(c.done)
		var ops op.Ops
		var btns [4]widget.Clickable
		for {
			switch e := c.w.Event().(type) {
			case Win32ViewEvent:
				if e.HWND != 0 {
					select {
					case hwnds <- syscall.Handle(e.HWND):
					default:
					}
				}
			case ConfigEvent:
				c.mode.Store(int32(e.Config.Mode))
			case FrameEvent:
				gtx := NewContext(&ops, e)
				for i := range btns {
					for btns[i].Clicked(gtx) {
						c.clicks <- captionTestNames[i]
					}
				}
				w, ch, bw := e.Size.X, gtx.Dp(captionTestH), gtx.Dp(captionTestBtnW)
				area := clip.Rect{Max: image.Pt(w, ch)}.Push(gtx.Ops)
				system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
				area.Pop()
				maxAct := system.ActionMaximize
				if WindowMode(c.mode.Load()) == Maximized {
					maxAct = system.ActionUnmaximize
				}
				acts := [4]system.Action{0, system.ActionMinimize, maxAct, system.ActionClose}
				var r [4]image.Rectangle
				r[0] = image.Rect(gtx.Dp(100), 0, gtx.Dp(160), ch)
				for i := 1; i < 4; i++ {
					r[i] = image.Rect(w-(4-i)*bw, 0, w-(3-i)*bw, ch)
				}
				for i := range btns {
					off := op.Offset(r[i].Min).Push(gtx.Ops)
					bg := gtx
					bg.Constraints = layout.Exact(r[i].Size())
					act := acts[i]
					btns[i].Layout(bg, func(gtx layout.Context) layout.Dimensions {
						if act != 0 {
							system.ActionInputOp(act).Add(gtx.Ops)
						}
						return layout.Dimensions{Size: gtx.Constraints.Max}
					})
					off.Pop()
					c.hover[i].Store(btns[i].Hovered())
				}
				c.mu.Lock()
				c.r, c.size = r, e.Size
				c.mu.Unlock()
				// keep drawing: hover changes show up in the next frame
				gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(30 * time.Millisecond)})
				e.Frame(gtx.Ops)
				c.frames.Add(1)
			case DestroyEvent:
				return
			}
		}
	}()
	select {
	case c.hwnd = <-hwnds:
	case <-time.After(30 * time.Second):
		t.Fatal("no Win32ViewEvent")
	}
	waitUntil(t, "the first frame", func() bool { return c.frames.Load() > 0 })
	// a known place on the primary monitor, clear of its edges
	windows.SetWindowPos(c.hwnd, 0, 60, 60, 0, 0, windows.SWP_NOSIZE|windows.SWP_NOZORDER)
	f := c.frames.Load()
	waitUntil(t, "a frame after the move", func() bool { return c.frames.Load() > f+2 })
	return c
}

func (c *captionTestWindow) rects() ([4]image.Rectangle, image.Point) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.r, c.size
}

func (c *captionTestWindow) close(t *testing.T) {
	t.Helper()
	windows.PostMessage(c.hwnd, windows.WM_CLOSE, 0, 0)
	select {
	case <-c.done:
	case <-time.After(15 * time.Second):
		t.Error("the window didn't close")
	}
}

func (c *captionTestWindow) zoomed() bool {
	r, _, _ := testIsZoomed.Call(uintptr(c.hwnd))
	return r != 0
}

// screen maps a client point to the screen.
func (c *captionTestWindow) screen(p image.Point) windows.Point {
	s := windows.Point{X: int32(p.X), Y: int32(p.Y)}
	testClientToScreen.Call(uintptr(c.hwnd), uintptr(unsafe.Pointer(&s)))
	return s
}

// hit is WM_NCHITTEST at client point p, as Windows asks it.
func (c *captionTestWindow) hit(p image.Point) uintptr {
	s := c.screen(p)
	lp := uintptr(uint16(s.X)) | uintptr(uint16(s.Y))<<16
	r, _, _ := testSendMessage.Call(uintptr(c.hwnd), windows.WM_NCHITTEST, 0, lp)
	return r
}

// thickness is the frame thickness the patch insets by.
func (c *captionTestWindow) thickness() int {
	dpi := windows.GetWindowDPI(c.hwnd)
	return windows.GetSystemMetricsForDpi(windows.SM_CXSIZEFRAME, dpi) + windows.GetSystemMetricsForDpi(windows.SM_CXPADDEDBORDER, dpi)
}

func centre(r image.Rectangle) image.Point { return r.Min.Add(r.Max).Div(2) }

var hitNames = map[uintptr]string{
	windows.HTCLIENT: "HTCLIENT", windows.HTCAPTION: "HTCAPTION", windows.HTMINBUTTON: "HTMINBUTTON",
	windows.HTMAXBUTTON: "HTMAXBUTTON", windows.HTCLOSE: "HTCLOSE", windows.HTTOP: "HTTOP",
	windows.HTTOPLEFT: "HTTOPLEFT", windows.HTTOPRIGHT: "HTTOPRIGHT", windows.HTLEFT: "HTLEFT",
	windows.HTRIGHT: "HTRIGHT", windows.HTBOTTOM: "HTBOTTOM", windows.HTBOTTOMLEFT: "HTBOTTOMLEFT",
	windows.HTBOTTOMRIGHT: "HTBOTTOMRIGHT", windows.HTNOWHERE: "HTNOWHERE", windows.HTBORDER: "HTBORDER",
}

func hitName(h uintptr) string {
	if n, ok := hitNames[h]; ok {
		return n
	}
	return "HT?"
}

// The frame: no caption above the client area, the frame thickness on the
// left, right and bottom, the client size the one asked for; a window
// without the option keeps its caption.
func TestHiddenTitleBarFrame(t *testing.T) {
	titleBarTests(t)
	c := newCaptionTestWindow(t, HiddenTitleBar(true))
	defer c.close(t)
	plain := newCaptionTestWindow(t)
	defer plain.close(t)

	ft := int32(c.thickness())
	wr := windows.GetWindowRect(c.hwnd)
	cr := windows.GetClientRect(c.hwnd)
	org := c.screen(image.Point{})
	t.Logf("hidden: window %v, client origin %v size %dx%d, frame thickness %d, dpi %d", wr, org, cr.Right, cr.Bottom, ft, windows.GetWindowDPI(c.hwnd))
	if org.Y != wr.Top {
		t.Errorf("the client area starts %d px below the window's top: a caption is left", org.Y-wr.Top)
	}
	if l, r, b := org.X-wr.Left, wr.Right-(org.X+cr.Right), wr.Bottom-(org.Y+cr.Bottom); l != ft || r != ft || b != ft {
		t.Errorf("borders left %d right %d bottom %d, want %d", l, r, b, ft)
	}
	dpi := windows.GetWindowDPI(c.hwnd)
	if w, h := int(cr.Right), int(cr.Bottom); w != (640*dpi+48)/96 || h != (400*dpi+48)/96 {
		t.Errorf("client %dx%d, want 640x400 dp at %d dpi", w, h, dpi)
	}

	pwr := windows.GetWindowRect(plain.hwnd)
	porg := plain.screen(image.Point{})
	t.Logf("plain: window %v, client origin %v", pwr, porg)
	if porg.Y-pwr.Top <= ft {
		t.Errorf("the plain window's client area is %d px below its top: it lost its caption", porg.Y-pwr.Top)
	}
}

// WM_NCHITTEST: the buttons, the caption, the top band, the client, the
// borders; maximized, no band and the restore button; a window without
// the option answers for its own caption and ignores the action areas.
func TestHiddenTitleBarHitTest(t *testing.T) {
	titleBarTests(t)
	c := newCaptionTestWindow(t, HiddenTitleBar(true))
	defer c.close(t)
	r, size := c.rects()
	ft := c.thickness()
	ch := r[1].Dy()
	check := func(what string, p image.Point, want uintptr) {
		t.Helper()
		if got := c.hit(p); got != want {
			t.Errorf("%s at %v: %s, want %s", what, p, hitName(got), hitName(want))
		}
	}
	check("minimize", centre(r[1]), windows.HTMINBUTTON)
	check("maximize", centre(r[2]), windows.HTMAXBUTTON)
	check("close", centre(r[3]), windows.HTCLOSE)
	check("a button in the caption", centre(r[0]), windows.HTCLIENT)
	check("the caption", image.Pt(size.X/2, ch/2), windows.HTCAPTION)
	check("under the caption", image.Pt(size.X/2, ch+50), windows.HTCLIENT)
	check("the top edge", image.Pt(size.X/2, 1), windows.HTTOP)
	check("the top-left corner", image.Pt(2, 2), windows.HTTOPLEFT)
	check("close over the top edge", image.Pt(size.X-3, 1), windows.HTCLOSE)
	check("just under the top band", image.Pt(size.X/2, ft), windows.HTCAPTION)
	check("the left border", image.Pt(-ft/2, size.Y/2), windows.HTLEFT)
	check("the right border", image.Pt(size.X+ft/2, size.Y/2), windows.HTRIGHT)
	check("the bottom border", image.Pt(size.X/2, size.Y+ft/2), windows.HTBOTTOM)
	check("the bottom-right corner", image.Pt(size.X+ft/2, size.Y+ft/2), windows.HTBOTTOMRIGHT)

	// maximized: the caption runs to the top, and the restore button
	c.w.Perform(system.ActionMaximize)
	waitUntil(t, "maximized", func() bool { return c.zoomed() && WindowMode(c.mode.Load()) == Maximized })
	f := c.frames.Load()
	waitUntil(t, "a maximized frame", func() bool { return c.frames.Load() > f+2 })
	r, size = c.rects()
	check("maximized: the top edge", image.Pt(size.X/2, 1), windows.HTCAPTION)
	check("maximized: restore", centre(r[2]), windows.HTMAXBUTTON)
	check("maximized: close at the corner", image.Pt(size.X-2, 1), windows.HTCLOSE)
	org := c.screen(image.Point{})
	mi, ok := windows.GetMonitorInfo(windows.GetWindowRect(c.hwnd), windows.MONITOR_DEFAULTTONULL)
	if ok {
		wa := mi.WorkArea
		t.Logf("maximized: client origin %v size %v, work area %v", org, size, wa)
		// within 2 px (an auto-hide taskbar's edge stays clear)
		if d := func(a, b int32) int32 {
			if a > b {
				return a - b
			}
			return b - a
		}; d(org.X, wa.Left) > 2 || d(org.Y, wa.Top) > 2 || d(org.X+int32(size.X), wa.Right) > 2 || d(org.Y+int32(size.Y), wa.Bottom) > 2 {
			t.Errorf("maximized client %v+%v is not the work area %v", org, size, wa)
		}
	}
	c.w.Perform(system.ActionUnmaximize)
	waitUntil(t, "restored", func() bool { return !c.zoomed() && WindowMode(c.mode.Load()) == Windowed })

	// without the option: Windows' own caption, the action areas unused
	plain := newCaptionTestWindow(t)
	defer plain.close(t)
	pr, psize := plain.rects()
	pwr := windows.GetWindowRect(plain.hwnd)
	porg := plain.screen(image.Point{})
	capY := -int(porg.Y-pwr.Top) / 2 // the middle of its caption, client coordinates
	if got := plain.hit(image.Pt(psize.X/2, capY)); got != windows.HTCAPTION {
		t.Errorf("the plain window's caption: %s, want HTCAPTION", hitName(got))
	}
	for i, want := range []uintptr{windows.HTCLIENT, windows.HTCLIENT, windows.HTCLIENT} {
		if got := plain.hit(centre(pr[i+1])); got != want {
			t.Errorf("the plain window's %s area: %s, want %s", captionTestNames[i+1], hitName(got), hitName(want))
		}
	}
	if got := plain.hit(image.Pt(psize.X/2, pr[1].Dy()/2)); got != windows.HTCLIENT {
		t.Errorf("the plain window's ActionMove area: %s, want HTCLIENT", hitName(got))
	}
}

// mouseInput is one absolute mouse INPUT at screen point p (flags:
// MOUSEEVENTF_*).
func mouseInput(p windows.Point, flags uint32) testInput {
	cx, _, _ := testGetSystemMetrics.Call(0) // SM_CXSCREEN
	cy, _, _ := testGetSystemMetrics.Call(1)
	in := testInput{typ: 0} // INPUT_MOUSE
	binary.LittleEndian.PutUint32(in.u[0:], uint32(int64(p.X)*65535/int64(max(cx, 1))))
	binary.LittleEndian.PutUint32(in.u[4:], uint32(int64(p.Y)*65535/int64(max(cy, 1))))
	binary.LittleEndian.PutUint32(in.u[12:], flags|0x8000) // MOUSEEVENTF_ABSOLUTE
	return in
}

const (
	mouseMove = 0x1
	mouseDown = 0x2
	mouseUp   = 0x4
)

func (c *captionTestWindow) expectClick(t *testing.T, want string) {
	t.Helper()
	select {
	case got := <-c.clicks:
		if got != want {
			t.Errorf("clicked %s, want %s", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Errorf("no click on %s", want)
	}
}

func (c *captionTestWindow) expectNoClick(t *testing.T, what string) {
	t.Helper()
	select {
	case got := <-c.clicks:
		t.Errorf("%s: a click on %s", what, got)
	case <-time.After(700 * time.Millisecond):
	}
}

// The caption buttons through SendInput: hovering one is a hover of its
// widget, the cursor leaving the window from it ends it, a click is the
// widget's click (and Windows doesn't run the button: the window doesn't
// maximize or close), a press dragged off a button clicks nothing, a
// button in the caption is the program's, and a double-click on the
// caption maximizes the window (Windows').
func TestHiddenTitleBarButtonsInput(t *testing.T) {
	titleBarTests(t)
	c := newCaptionTestWindow(t, HiddenTitleBar(true))
	defer c.close(t)
	testSetForegroundWindow.Call(uintptr(c.hwnd))
	r, _ := c.rects()
	wr := windows.GetWindowRect(c.hwnd)

	sendInput(t, []testInput{mouseInput(c.screen(centre(r[3])), mouseMove)})
	waitUntil(t, "close hovered", c.hover[3].Load)
	sendInput(t, []testInput{mouseInput(c.screen(centre(r[2])), mouseMove)})
	waitUntil(t, "maximize hovered, close not", func() bool { return c.hover[2].Load() && !c.hover[3].Load() })
	// out of the window over its top edge: the hover ends
	sendInput(t, []testInput{mouseInput(windows.Point{X: wr.Right - 70, Y: wr.Top - 30}, mouseMove)})
	waitUntil(t, "no button hovered once the cursor left", func() bool {
		return !c.hover[1].Load() && !c.hover[2].Load() && !c.hover[3].Load()
	})

	for _, i := range []int{2, 3, 1, 0} {
		p := c.screen(centre(r[i]))
		sendInput(t, []testInput{mouseInput(p, mouseMove), mouseInput(p, mouseDown), mouseInput(p, mouseUp)})
		c.expectClick(t, captionTestNames[i])
		time.Sleep(600 * time.Millisecond) // past the double-click time between buttons
	}
	if c.zoomed() {
		t.Error("the maximize button's click maximized the window: Windows ran the button")
	}
	if r, _, _ := testIsWindow.Call(uintptr(c.hwnd)); r == 0 {
		t.Fatal("the close button's click closed the window: Windows ran the button")
	}
	if windows.IsIconic(c.hwnd) {
		t.Error("the minimize button's click minimized the window: Windows ran the button")
	}

	// pressed on maximize, released under the caption: no click
	from, to := c.screen(centre(r[2])), c.screen(image.Pt(centre(r[2]).X, r[2].Max.Y+80))
	sendInput(t, []testInput{mouseInput(from, mouseMove), mouseInput(from, mouseDown)})
	time.Sleep(100 * time.Millisecond)
	sendInput(t, []testInput{mouseInput(to, mouseMove)})
	time.Sleep(100 * time.Millisecond)
	sendInput(t, []testInput{mouseInput(to, mouseUp)})
	c.expectNoClick(t, "a press dragged off maximize")
	time.Sleep(600 * time.Millisecond)

	// a double-click on the caption: Windows maximizes the window
	_, size := c.rects()
	p := c.screen(image.Pt(size.X/2, r[1].Dy()/2))
	sendInput(t, []testInput{mouseInput(p, mouseMove), mouseInput(p, mouseDown), mouseInput(p, mouseUp), mouseInput(p, mouseDown), mouseInput(p, mouseUp)})
	waitUntil(t, "maximized by a double-click on the caption", c.zoomed)
	c.expectNoClick(t, "the double-click on the caption")
}
