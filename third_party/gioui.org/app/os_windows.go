// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"errors"
	"fmt"
	"gioui.org/io/transfer"
	syscall "golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"image"
	"io"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
	"unsafe"

	"gioui.org/app/internal/windows"
	"gioui.org/op"
	"gioui.org/unit"
	gowindows "golang.org/x/sys/windows"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/widget"
)

type Win32ViewEvent struct {
	HWND uintptr
}

type window struct {
	hwnd syscall.Handle
	hdc  syscall.Handle
	w    *callbacks

	// cursorIn tracks whether the cursor was inside the window according
	// to the most recent WM_SETCURSOR.
	cursorIn bool
	cursor   syscall.Handle

	animating bool

	borderSize image.Point
	config     Config
	// frameDims stores the last seen window frame width and height.
	frameDims image.Point
	loop      *eventLoop
	drop      *dropTarget // Clack patch (CLACK_PATCHES.md §11)
	// configured: the first Configure (newWindow's, which shows the
	// window) ran. Clack patch (CLACK_PATCHES.md §20).
	configured bool
	// startedHidden: that Configure left the window hidden (StartHidden).
	// Clack patch (CLACK_PATCHES.md §23).
	startedHidden bool
	// hiddenShow is the show that Configure left out for a window started
	// hidden maximized or full screen (SW_SHOWMAXIMIZED), for the
	// ActionRaise that shows it. Clack patch (CLACK_PATCHES.md §25).
	hiddenShow int32
	// mousePID is the mouse's pointer ID, for a pointer.Leave from a
	// message that has none; ncPressed: a press on a caption button went
	// to the program. Clack patch (CLACK_PATCHES.md §22).
	mousePID  pointer.ID
	ncPressed bool
}

const _WM_WAKEUP = windows.WM_USER + iota

const copyDataURLType = 0xffffff00

type gpuAPI struct {
	priority    int
	initializer func(w *window) (context, error)
}

// drivers is the list of potential Context implementations.
var drivers []gpuAPI

// winMap maps win32 HWNDs to *windows.
var winMap sync.Map

// iconID is the ID of the icon in the resource file.
const iconID = 1

var resources struct {
	once sync.Once
	// handle is the module handle from GetModuleHandle.
	handle syscall.Handle
	// class is the Gio window class from RegisterClassEx.
	class uint16
	// cursor is the arrow cursor resource.
	cursor syscall.Handle
}

func osMain() {
	processURLEvent(startupURI())
	select {}
}

func newWindow(win *callbacks, options []Option) {
	done := make(chan struct{})
	go func() {
		// GetMessage and PeekMessage can filter on a window HWND, but
		// then thread-specific messages such as WM_QUIT are ignored.
		// Instead lock the thread so window messages arrive through
		// unfiltered GetMessage calls.
		//
		// Don't call UnLockOSThread, so that the Go runtime doesn't
		// reuse a thread with thread-specific settings such as DPI
		// awareness.
		runtime.LockOSThread()
		windows.SetThreadDpiAwarenessContext()
		setCaretBlinkFromOS() // Clack patch (CLACK_PATCHES.md §13)

		w := &window{
			w: win,
		}
		w.loop = newEventLoop(w.w, w.wakeup)
		w.w.SetDriver(w)
		// Clack patch (CLACK_PATCHES.md §25): a window placed by its
		// first options is created there.
		var place Config
		place.apply(unit.Metric{}, options)
		err := w.init(place)
		done <- struct{}{}
		if err != nil {
			w.ProcessEvent(DestroyEvent{Err: err})
			return
		}
		winMap.Store(w.hwnd, w)
		defer winMap.Delete(w.hwnd)
		w.Configure(options)
		w.ProcessEvent(Win32ViewEvent{HWND: uintptr(w.hwnd)})
		// Clack patches (CLACK_PATCHES.md §8, §23): a panel or a window
		// started hidden doesn't take the focus.
		if !w.config.panel && !w.startedHidden {
			windows.SetForegroundWindow(w.hwnd)
			windows.SetFocus(w.hwnd)
		}
		// Since the window class for the cursor is null,
		// set it here to show the cursor.
		w.SetCursor(pointer.CursorDefault)
		w.runLoop()
	}()
	<-done
}

// initResources initializes the resources global.
func initResources() error {
	hInst, err := windows.GetModuleHandle()
	if err != nil {
		return err
	}
	resources.handle = hInst
	c, err := windows.LoadCursor(windows.IDC_ARROW)
	if err != nil {
		return err
	}
	resources.cursor = c
	// Prefer an icon supplied at IDI_APPLICATION, which is where a
	// resource author puts an icon meant for the window and title bar.
	// Fall back to the first icon group for resources built without
	// one, which is the previous behavior. A binary with no icon
	// resources at all keeps an icon-less window class, as before.
	var icon syscall.Handle
	for _, id := range []uint32{windows.IDI_APPLICATION, iconID} {
		h, err := windows.LoadImage(hInst, id, windows.IMAGE_ICON, 0, 0, windows.LR_DEFAULTSIZE|windows.LR_SHARED)
		if err == nil {
			icon = h
			break
		}
	}

	appid, err := syscall.UTF16PtrFromString(ID)
	if err != nil {
		return err
	}

	wcls := windows.WndClassEx{
		CbSize:        uint32(unsafe.Sizeof(windows.WndClassEx{})),
		Style:         windows.CS_HREDRAW | windows.CS_VREDRAW | windows.CS_OWNDC,
		LpfnWndProc:   syscall.NewCallback(windowProc),
		HInstance:     hInst,
		HIcon:         icon,
		LpszClassName: appid,
	}
	cls, err := windows.RegisterClassEx(&wcls)
	if err != nil {
		return err
	}
	resources.class = cls
	return nil
}

const dwExStyle = windows.WS_EX_APPWINDOW | windows.WS_EX_WINDOWEDGE

func (w *window) init(place Config) error {
	var resErr error
	resources.once.Do(func() {
		resErr = initResources()
	})
	if resErr != nil {
		return resErr
	}
	const dwStyle = windows.WS_OVERLAPPEDWINDOW

	x, y, cx, cy := int32(windows.CW_USEDEFAULT), int32(windows.CW_USEDEFAULT), int32(windows.CW_USEDEFAULT), int32(windows.CW_USEDEFAULT)
	if b := place.bounds; place.boundsSet {
		// Clack patch (CLACK_PATCHES.md §25): created where Bounds puts
		// it, so it opens with that monitor's DPI; SetWindowPos onto a
		// monitor of another DPI would rescale it (WM_DPICHANGED).
		x, y, cx, cy = int32(b.Min.X), int32(b.Min.Y), int32(b.Dx()), int32(b.Dy())
	}
	hwnd, err := windows.CreateWindowEx(
		dwExStyle,
		resources.class,
		"",
		dwStyle|windows.WS_CLIPSIBLINGS|windows.WS_CLIPCHILDREN,
		x, y,
		cx, cy,
		0,
		0,
		resources.handle,
		0)
	if err != nil {
		return err
	}
	if err := windows.RegisterTouchWindow(hwnd, 0); err != nil {
		return err
	}
	if err := windows.EnableMouseInPointer(1); err != nil {
		return err
	}
	w.hdc, err = windows.GetDC(hwnd)
	if err != nil {
		windows.DestroyWindow(hwnd)
		return err
	}
	w.hwnd = hwnd
	w.registerFileDrop() // Clack patch (CLACK_PATCHES.md §11)
	return nil
}

// update handles changes done by the user, and updates the configuration.
// It reads the window style and size/position and updates w.config.
// If anything has changed it emits a ConfigEvent to notify the application.
func (w *window) update() {
	p := windows.GetWindowPlacement(w.hwnd)
	if !p.IsMinimized() {
		r := windows.GetWindowRect(w.hwnd)
		cr := windows.GetClientRect(w.hwnd)
		w.config.Size = image.Point{
			X: int(cr.Right - cr.Left),
			Y: int(cr.Bottom - cr.Top),
		}
		w.frameDims = image.Point{
			X: int(r.Right - r.Left),
			Y: int(r.Bottom - r.Top),
		}.Sub(w.config.Size)
	}

	// Clack patch (CLACK_PATCHES.md §25): where the window is.
	w.config.bounds, w.config.display = w.placement()

	w.borderSize = image.Pt(
		windows.GetSystemMetrics(windows.SM_CXSIZEFRAME),
		windows.GetSystemMetrics(windows.SM_CYSIZEFRAME),
	)
	style := windows.GetWindowLong(w.hwnd, windows.GWL_STYLE)
	switch {
	case p.IsMaximized() && style&windows.WS_OVERLAPPEDWINDOW != 0:
		w.config.Mode = Maximized
	case p.IsMaximized():
		w.config.Mode = Fullscreen
	default:
		w.config.Mode = Windowed
	}
	w.ProcessEvent(ConfigEvent{Config: w.config})
	w.draw(true)
}

func windowProc(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	win, exists := winMap.Load(hwnd)
	if !exists {
		return windows.DefWindowProc(hwnd, msg, wParam, lParam)
	}

	w := win.(*window)
	if clackMsgTrace != nil { // Clack patch (CLACK_PATCHES.md §22): tests
		clackMsgTrace(hwnd, msg, wParam, lParam)
	}

	switch msg {
	case windows.WM_UNICHAR:
		if wParam == windows.UNICODE_NOCHAR {
			// Tell the system that we accept WM_UNICHAR messages.
			return windows.TRUE
		}
		fallthrough
	case windows.WM_CHAR:
		if r := rune(wParam); unicode.IsPrint(r) {
			w.w.EditorInsert(string(r))
		}
		// The message is processed.
		return windows.TRUE
	case windows.WM_DPICHANGED:
		// Windows provides a rectangle scaled for the new monitor DPI.
		r := (*windows.Rect)(unsafe.Pointer(lParam))
		windows.SetWindowPos(
			w.hwnd,
			0,
			r.Left,
			r.Top,
			r.Right-r.Left,
			r.Bottom-r.Top,
			windows.SWP_NOACTIVATE|windows.SWP_NOZORDER,
		)
		return 0
	case windows.WM_ERASEBKGND:
		// Avoid flickering between GPU content and background color.
		return windows.TRUE
	case windows.WM_KEYDOWN, windows.WM_KEYUP, windows.WM_SYSKEYDOWN, windows.WM_SYSKEYUP:
		if n, ok := convertKeyCode(wParam); ok {
			e := key.Event{
				Name:      n,
				Modifiers: getModifiers(),
				State:     key.Press,
			}
			if msg == windows.WM_KEYUP || msg == windows.WM_SYSKEYUP {
				e.State = key.Release
			}

			w.ProcessEvent(e)

			if (wParam == windows.VK_F10) && (msg == windows.WM_SYSKEYDOWN || msg == windows.WM_SYSKEYUP) {
				// Reserve F10 for ourselves, and don't let it open the system menu. Other Windows programs
				// such as cmd.exe and graphical debuggers also reserve F10.
				return 0
			}
		}
	case windows.WM_POINTERDOWN, windows.WM_POINTERUP, windows.WM_POINTERUPDATE, windows.WM_POINTERCAPTURECHANGED:
		pid := getPointerIDwParam(wParam)
		pi, err := windows.GetPointerInfo(uint32(pid))
		if err != nil {
			panic(err)
		}
		switch msg {
		case windows.WM_POINTERDOWN:
			windows.SetCapture(w.hwnd)
		case windows.WM_POINTERUP:
			windows.ReleaseCapture()
		}

		kind := pointer.Move
		switch pi.ButtonChangeType {
		case windows.POINTER_CHANGE_FIRSTBUTTON_DOWN, windows.POINTER_CHANGE_SECONDBUTTON_DOWN, windows.POINTER_CHANGE_THIRDBUTTON_DOWN, windows.POINTER_CHANGE_FOURTHBUTTON_DOWN, windows.POINTER_CHANGE_FIFTHBUTTON_DOWN:
			kind = pointer.Press
		case windows.POINTER_CHANGE_FIRSTBUTTON_UP, windows.POINTER_CHANGE_SECONDBUTTON_UP, windows.POINTER_CHANGE_THIRDBUTTON_UP, windows.POINTER_CHANGE_FOURTHBUTTON_UP, windows.POINTER_CHANGE_FIFTHBUTTON_UP:
			kind = pointer.Release
		}

		if (pi.PointerFlags&windows.POINTER_FLAG_CANCELED != 0) || (msg == windows.WM_POINTERCAPTURECHANGED) {
			kind = pointer.Cancel
		}

		w.pointerUpdate(pi, pid, kind, lParam)
	case windows.WM_CANCELMODE:
		w.ProcessEvent(pointer.Event{
			Kind: pointer.Cancel,
		})
	case windows.WM_SETFOCUS:
		w.config.Focused = true
		w.ProcessEvent(ConfigEvent{Config: w.config})
	case windows.WM_KILLFOCUS:
		w.config.Focused = false
		w.ProcessEvent(ConfigEvent{Config: w.config})
	case windows.WM_NCHITTEST:
		if w.hiddenTitle() { // Clack patch (CLACK_PATCHES.md §22)
			x, y := coordsFromlParam(lParam)
			np := windows.Point{X: int32(x), Y: int32(y)}
			windows.ScreenToClient(w.hwnd, &np)
			return w.hiddenTitleHitTest(int(np.X), int(np.Y), lParam)
		}
		if w.config.Decorated {
			// Let the system handle it.
			break
		}
		x, y := coordsFromlParam(lParam)
		np := windows.Point{X: int32(x), Y: int32(y)}
		windows.ScreenToClient(w.hwnd, &np)
		return w.hitTest(int(np.X), int(np.Y))
	case windows.WM_CLOSE:
		e := &ClosingEvent{}
		w.ProcessEvent(e)
		if e.abort {
			return 0
		}
	case windows.WM_POINTERWHEEL:
		w.scrollEvent(wParam, lParam, false, getModifiers())
	case windows.WM_POINTERHWHEEL:
		w.scrollEvent(wParam, lParam, true, getModifiers())
	case wmDropFiles: // Clack patch (CLACK_PATCHES.md §11)
		w.dropFiles(wParam)
		return 0
	case windows.WM_DESTROY:
		w.revokeFileDrop() // Clack patch (CLACK_PATCHES.md §11)
		w.ProcessEvent(Win32ViewEvent{})
		w.ProcessEvent(DestroyEvent{})
		w.w = nil
		if w.hdc != 0 {
			windows.ReleaseDC(w.hdc)
			w.hdc = 0
		}
		// The system destroys the HWND for us.
		w.hwnd = 0
		windows.PostQuitMessage(0)
		return 0
	case windows.WM_NCCALCSIZE:
		if w.hiddenTitle() {
			// Clack patch (CLACK_PATCHES.md §22): lParam is the
			// NCCALCSIZE_PARAMS whose first rectangle is the window's
			// (wParam TRUE), or that rectangle (FALSE).
			w.hiddenTitleCalcSize((*windows.Rect)(unsafe.Pointer(lParam)))
			return 0
		}
		if w.config.Decorated {
			// Let Windows handle decorations.
			break
		}
		// No client areas; we draw decorations ourselves.
		if wParam != 1 {
			return 0
		}
		// lParam contains an NCCALCSIZE_PARAMS for us to adjust.
		place := windows.GetWindowPlacement(w.hwnd)
		if !place.IsMaximized() {
			// Nothing do adjust.
			return 0
		}
		// Adjust window position to avoid the extra padding in maximized
		// state. See https://devblogs.microsoft.com/oldnewthing/20150304-00/?p=44543.
		// Note that trying to do the adjustment in WM_GETMINMAXINFO is ignored by Windows.
		szp := (*windows.NCCalcSizeParams)(unsafe.Pointer(lParam))
		mi, ok := windows.GetMonitorInfo(szp.Rgrc[0], windows.MONITOR_DEFAULTTONULL)
		if !ok {
			return 0
		}
		szp.Rgrc[0] = mi.WorkArea
		return 0
	case windows.WM_PAINT:
		w.draw(true)
	case windows.WM_STYLECHANGED:
		w.update()
	case windows.WM_WINDOWPOSCHANGED:
		w.update()
		return 0
	case windows.WM_GETMINMAXINFO:
		mm := (*windows.MinMaxInfo)(unsafe.Pointer(lParam))

		var frameDims image.Point
		if w.config.Decorated {
			frameDims = w.frameDims
		}
		if w.hiddenTitle() {
			// Clack patch (CLACK_PATCHES.md §22): the restored frame,
			// also while maximized (frameDims is the maximized one then)
			frameDims = w.hiddenTitleFrame()
		}
		if p := w.config.MinSize; p.X > 0 || p.Y > 0 {
			p = p.Add(frameDims)
			mm.PtMinTrackSize = windows.Point{
				X: int32(p.X),
				Y: int32(p.Y),
			}
		}
		if p := w.config.MaxSize; p.X > 0 || p.Y > 0 {
			p = p.Add(frameDims)
			mm.PtMaxTrackSize = windows.Point{
				X: int32(p.X),
				Y: int32(p.Y),
			}
		}
		return 0
	case windows.WM_NCPOINTERUPDATE, windows.WM_NCPOINTERDOWN, windows.WM_NCPOINTERUP:
		// Clack patch (CLACK_PATCHES.md §22): the caption buttons' input
		// reaches the program's widgets.
		if w.hiddenTitle() && w.hiddenTitlePointer(msg, wParam, lParam) {
			return 0
		}
	case windows.WM_NCLBUTTONDOWN, windows.WM_NCLBUTTONDBLCLK, windows.WM_NCLBUTTONUP:
		// Clack patch (CLACK_PATCHES.md §22): never DefWindowProc's
		// caption-button loop for the program's buttons.
		if w.hiddenTitle() && w.hiddenTitleMouse(msg, wParam, lParam) {
			return 0
		}
	case windows.WM_POINTERLEAVE:
		if w.hiddenTitle() { // Clack patch (CLACK_PATCHES.md §22)
			pid := getPointerIDwParam(wParam)
			if pi, err := windows.GetPointerInfo(uint32(pid)); err == nil && pi.PointerType == windows.PT_MOUSE {
				w.hiddenTitleLeave(pid)
			}
		}
	case windows.WM_NCMOUSELEAVE:
		if w.hiddenTitle() { // Clack patch (CLACK_PATCHES.md §22)
			w.hiddenTitleLeave(w.mousePID)
		}
	case _WM_CLACK_FRAME:
		if w.hiddenTitle() { // Clack patch (CLACK_PATCHES.md §22)
			w.refreshFrame()
		}
		return 0
	case windows.WM_SETCURSOR:
		w.cursorIn = (lParam & 0xffff) == windows.HTCLIENT
		if w.cursorIn {
			windows.SetCursor(w.cursor)
			return windows.TRUE
		}
	case _WM_WAKEUP:
		w.loop.Wakeup()
		w.loop.FlushEvents()
	case windows.WM_IME_STARTCOMPOSITION:
		imc := windows.ImmGetContext(w.hwnd)
		if imc == 0 {
			return windows.TRUE
		}
		defer windows.ImmReleaseContext(w.hwnd, imc)
		w.updateIMEWindows(imc)
		return windows.TRUE
	case windows.WM_IME_COMPOSITION:
		imc := windows.ImmGetContext(w.hwnd)
		if imc == 0 {
			return windows.TRUE
		}
		defer windows.ImmReleaseContext(w.hwnd, imc)
		defer w.updateIMEWindows(imc)
		state := w.w.EditorState()
		if lParam&windows.GCS_RESULTSTR != 0 {
			// RESULTSTR is committed text. Keep it separate from COMPSTR so a
			// preedit update never looks like a commit.
			rng := imeRange(state)
			result := windows.ImmGetCompositionString(imc, windows.GCS_RESULTSTR)
			start := rng.Start
			w.w.EditorReplace(rng, result)
			end := start + utf8.RuneCountInString(result)
			w.w.SetComposingRegion(key.Range{Start: -1, End: -1})
			w.w.SetEditorSelection(key.Range{Start: end, End: end})
			if lParam&windows.GCS_COMPSTR == 0 {
				return windows.TRUE
			}
			state = w.w.EditorState()
		}
		if lParam&windows.GCS_COMPSTR != 0 {
			// COMPSTR is still preedit text, so keep the composing range alive.
			rng := imeRange(state)
			replacement := windows.ImmGetCompositionString(imc, windows.GCS_COMPSTR)
			end := rng.Start + utf8.RuneCountInString(replacement)
			w.w.EditorReplace(rng, replacement)
			state = w.w.EditorState()
			comp := key.Range{
				Start: rng.Start,
				End:   end,
			}
			if lParam&windows.GCS_DELTASTART != 0 {
				start := windows.ImmGetCompositionValue(imc, windows.GCS_DELTASTART)
				comp.Start = state.RunesIndex(state.UTF16Index(comp.Start) + start)
			}
			w.w.SetComposingRegion(comp)
			pos := end
			if lParam&windows.GCS_CURSORPOS != 0 {
				rel := windows.ImmGetCompositionValue(imc, windows.GCS_CURSORPOS)
				pos = state.RunesIndex(state.UTF16Index(rng.Start) + rel)
			}
			w.w.SetEditorSelection(key.Range{Start: pos, End: pos})
			return windows.TRUE
		}
		if lParam&(windows.GCS_DELTASTART|windows.GCS_CURSORPOS) == 0 || state.compose.Start == -1 {
			return windows.TRUE
		}
		// Some composition messages only move the IME cursor or clause start.
		rng := normRange(state.compose)
		comp := rng
		if lParam&windows.GCS_DELTASTART != 0 {
			start := windows.ImmGetCompositionValue(imc, windows.GCS_DELTASTART)
			comp.Start = state.RunesIndex(state.UTF16Index(comp.Start) + start)
			w.w.SetComposingRegion(comp)
		}
		if lParam&windows.GCS_CURSORPOS != 0 {
			rel := windows.ImmGetCompositionValue(imc, windows.GCS_CURSORPOS)
			pos := state.RunesIndex(state.UTF16Index(rng.Start) + rel)
			w.w.SetEditorSelection(key.Range{Start: pos, End: pos})
		}
		return windows.TRUE
	case windows.WM_IME_ENDCOMPOSITION:
		w.w.SetComposingRegion(key.Range{Start: -1, End: -1})
		return windows.TRUE
	case windows.WM_COPYDATA:
		data := (*windows.CopyDataStruct)(unsafe.Pointer(lParam))
		switch data.DwData {
		case copyDataURLType:
			if schemesURI == "" {
				return windows.TRUE
			}

			uri := syscall.UTF16PtrToString((*uint16)(unsafe.Pointer(data.LpData)))
			if processURLEvent(uri) {
				w.Perform(system.ActionRaise)
			}
			return windows.TRUE
		}
	}

	return windows.DefWindowProc(hwnd, msg, wParam, lParam)
}

// getModifiers reports the currently pressed modifier keys.
// GetKeyState uses the high-order bit for the pressed state:
// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getkeystate
func getModifiers() key.Modifiers {
	var kmods key.Modifiers
	if windows.GetKeyState(windows.VK_LWIN) < 0 || windows.GetKeyState(windows.VK_RWIN) < 0 {
		kmods |= key.ModSuper
	}
	if windows.GetKeyState(windows.VK_MENU) < 0 {
		kmods |= key.ModAlt
	}
	if windows.GetKeyState(windows.VK_CONTROL) < 0 {
		kmods |= key.ModCtrl
	}
	if windows.GetKeyState(windows.VK_SHIFT) < 0 {
		kmods |= key.ModShift
	}
	return kmods
}

// updateIMEWindows keeps the Windows IME popup near the text being edited.
func (w *window) updateIMEWindows(imc syscall.Handle) {
	sel := w.w.EditorState().Selection
	top := sel.Transform.Transform(sel.Caret.Pos.Add(f32.Pt(0, -sel.Caret.Ascent)))
	base := sel.Transform.Transform(sel.Caret.Pos)
	bottom := sel.Transform.Transform(sel.Caret.Pos.Add(f32.Pt(0, sel.Caret.Descent)))

	itop := image.Pt(int(top.X+.5), int(top.Y+.5))
	ibase := image.Pt(int(base.X+.5), int(base.Y+.5))
	ibottom := image.Pt(int(bottom.X+.5), int(bottom.Y+.5))
	if ibottom.Y <= itop.Y {
		ibottom.Y = itop.Y + 1
	}
	exclude := windows.Rect{
		Left:   int32(ibase.X),
		Top:    int32(itop.Y),
		Right:  int32(ibase.X + 1),
		Bottom: int32(ibottom.Y),
	}
	x, y := ibottom.X, ibottom.Y
	if !sel.CompositionBounds.Empty() {
		exclude = transformRect(sel.Transform, sel.CompositionBounds)
		x = int(exclude.Left)
		y = int(exclude.Bottom)
	}
	windows.ImmSetCompositionWindow(imc, x, y)
	windows.ImmSetCandidateWindow(imc, x, y, exclude)
}

// transformRect maps a local rectangle to window coordinates. Transform all
// corners because an affine transform may flip or rotate the rectangle.
func transformRect(t f32.Affine2D, r image.Rectangle) windows.Rect {
	p0 := t.Transform(f32.Pt(float32(r.Min.X), float32(r.Min.Y)))
	p1 := t.Transform(f32.Pt(float32(r.Max.X), float32(r.Min.Y)))
	p2 := t.Transform(f32.Pt(float32(r.Max.X), float32(r.Max.Y)))
	p3 := t.Transform(f32.Pt(float32(r.Min.X), float32(r.Max.Y)))

	minX := min(min(p0.X, p1.X), min(p2.X, p3.X))
	minY := min(min(p0.Y, p1.Y), min(p2.Y, p3.Y))
	maxX := max(max(p0.X, p1.X), max(p2.X, p3.X))
	maxY := max(max(p0.Y, p1.Y), max(p2.Y, p3.Y))
	left := int32(math.Floor(float64(minX)))
	top := int32(math.Floor(float64(minY)))
	right := int32(math.Ceil(float64(maxX)))
	bottom := int32(math.Ceil(float64(maxY)))
	if right <= left {
		right = left + 1
	}
	if bottom <= top {
		bottom = top + 1
	}
	return windows.Rect{
		Left:   left,
		Top:    top,
		Right:  right,
		Bottom: bottom,
	}
}

// hitTest returns the non-client area hit by the point, needed to
// process WM_NCHITTEST.
func (w *window) hitTest(x, y int) uintptr {
	if w.config.Mode == Windowed {
		// Check for resize handle before system actions; otherwise it can be impossible to
		// resize a custom-decorations window when the system move area is flush with the
		// edge of the window.
		top := y <= w.borderSize.Y
		bottom := y >= w.config.Size.Y-w.borderSize.Y
		left := x <= w.borderSize.X
		right := x >= w.config.Size.X-w.borderSize.X
		switch {
		case top && left:
			return windows.HTTOPLEFT
		case top && right:
			return windows.HTTOPRIGHT
		case bottom && left:
			return windows.HTBOTTOMLEFT
		case bottom && right:
			return windows.HTBOTTOMRIGHT
		case top:
			return windows.HTTOP
		case bottom:
			return windows.HTBOTTOM
		case left:
			return windows.HTLEFT
		case right:
			return windows.HTRIGHT
		}
	}
	p := f32.Pt(float32(x), float32(y))
	if a, ok := w.w.ActionAt(p); ok && a == system.ActionMove {
		return windows.HTCAPTION
	}
	return windows.HTCLIENT
}

func (w *window) pointerUpdate(pi windows.PointerInfo, pid pointer.ID, kind pointer.Kind, lParam uintptr) {
	if !w.config.Focused {
		windows.SetFocus(w.hwnd)
	}

	src := pointer.Touch
	if pi.PointerType == windows.PT_MOUSE {
		src = pointer.Mouse
		w.mousePID = pid // Clack patch (CLACK_PATCHES.md §22)
	}

	x, y := coordsFromlParam(lParam)
	np := windows.Point{X: int32(x), Y: int32(y)}
	windows.ScreenToClient(w.hwnd, &np)
	p := f32.Point{X: float32(np.X), Y: float32(np.Y)}
	w.ProcessEvent(pointer.Event{
		Kind:      kind,
		Source:    src,
		Position:  p,
		PointerID: pid,
		Buttons:   getPointerButtons(pi),
		Time:      windows.GetMessageTime(),
		Modifiers: getModifiers(),
	})
}

func coordsFromlParam(lParam uintptr) (int, int) {
	x := int(int16(lParam & 0xffff))
	y := int(int16((lParam >> 16) & 0xffff))
	return x, y
}

func (w *window) scrollEvent(wParam, lParam uintptr, horizontal bool, kmods key.Modifiers) {
	pid := getPointerIDwParam(wParam)
	pi, err := windows.GetPointerInfo(uint32(pid))
	if err != nil {
		panic(err)
	}

	x, y := coordsFromlParam(lParam)
	// The WM_MOUSEWHEEL coordinates are in screen coordinates, in contrast
	// to other mouse events.
	np := windows.Point{X: int32(x), Y: int32(y)}
	windows.ScreenToClient(w.hwnd, &np)
	p := f32.Point{X: float32(np.X), Y: float32(np.Y)}
	delta := int16(wParam >> 16)
	// Clack patch 9: whole notches (multiples of WHEEL_DELTA) animate; a
	// precision touchpad's or a free-spinning wheel's smaller deltas
	// don't. Chromium can't tell them apart either (crbug.com/545234,
	// web_input_event_builders_win.cc); its precision touchpads come
	// through DirectManipulation instead, which Gio doesn't use.
	notched := delta != 0 && delta%120 == 0
	dist := float32(delta) * w.wheelPixelsPerDelta(horizontal, kmods == key.ModShift)
	var sp f32.Point
	if horizontal {
		sp.X = dist
	} else {
		// support horizontal scroll (shift + mousewheel)
		if kmods == key.ModShift {
			sp.X = -dist
		} else {
			sp.Y = -dist
		}
	}
	w.ProcessEvent(pointer.Event{
		Kind:      pointer.Scroll,
		Source:    pointer.Mouse,
		Position:  p,
		Buttons:   getPointerButtons(pi),
		Scroll:    sp,
		Notched:   notched,
		Modifiers: kmods,
		Time:      windows.GetMessageTime(),
	})
}

// wheelPixelsPerDelta converts WM_MOUSEWHEEL's WHEEL_DELTA units to
// pixels the way Chromium does (WebMouseWheelEventBuilder::Build,
// ui/events/blink/web_input_event_builders_win.cc): a notch scrolls
// SPI_GETWHEELSCROLLLINES lines (3 if it can't be read) of
// kScrollbarPixelsPerLine, 100/3 dp, so 100 dp at the default; a
// horizontal notch SPI_GETWHEELSCROLLCHARS characters (1) of the same
// width. Shift turns the vertical wheel horizontal (Gio's convention)
// and keeps its lines. "One screen at a time" (WHEEL_PAGESCROLL) scrolls
// by Blink's page step, 87.5% of the scroller's height
// (kMinFractionToStepWhenPaging, cc/input/scroll_utils.h), here of the
// window's.
func (w *window) wheelPixelsPerDelta(horizontal, shift bool) float32 {
	const (
		wheelDelta     = 120
		pixelsPerLine  = 100.0 / 3.0
		pageFraction   = 0.875
		defaultLines   = 3
		defaultChars   = 1
		pageScrollUint = windows.WHEEL_PAGESCROLL
	)
	ppdp := configForDPI(windows.GetWindowDPI(w.hwnd)).PxPerDp
	if horizontal {
		chars := windows.SystemParametersUint(windows.SPI_GETWHEELSCROLLCHARS, defaultChars)
		return float32(chars) * pixelsPerLine * ppdp / wheelDelta
	}
	lines := windows.SystemParametersUint(windows.SPI_GETWHEELSCROLLLINES, defaultLines)
	if lines == pageScrollUint {
		size := w.config.Size.Y
		if shift {
			size = w.config.Size.X
		}
		return float32(size) * pageFraction / wheelDelta
	}
	return float32(lines) * pixelsPerLine * ppdp / wheelDelta
}

// Adapted from https://blogs.msdn.microsoft.com/oldnewthing/20060126-00/?p=32513/
func (w *window) runLoop() {
	msg := new(windows.Msg)
loop:
	for {
		anim := w.animating
		p := windows.GetWindowPlacement(w.hwnd)
		// Clack patch (CLACK_PATCHES.md §13): a hidden window draws
		// nothing either, like a minimized one; showing it paints it.
		if anim && !p.IsMinimized() && windows.IsWindowVisible(w.hwnd) {
			// Clack patch (CLACK_PATCHES.md §27): take the message, or
			// draw. A PeekMessage(PM_NOREMOVE) that saw one followed by
			// GetMessage could block: after a caption button's pointer up
			// the peek reported a message GetMessage never returned, and
			// the window drew nothing until the next unrelated input.
			if !windows.PeekMessage(msg, 0, 0, 0, windows.PM_REMOVE) {
				w.draw(false)
				continue
			}
			if msg.Message == windows.WM_QUIT {
				break loop
			}
		} else {
			switch ret := windows.GetMessage(msg, 0, 0, 0); ret {
			case -1:
				panic(errors.New("GetMessage failed"))
			case 0:
				// WM_QUIT received.
				break loop
			}
		}
		windows.TranslateMessage(msg)
		windows.DispatchMessage(msg)
	}
}

func (w *window) EditorStateChanged(old, new editorState) {
	imc := windows.ImmGetContext(w.hwnd)
	if imc == 0 {
		return
	}
	defer windows.ImmReleaseContext(w.hwnd, imc)
	if old.Selection.Caret != new.Selection.Caret ||
		old.Selection.Transform != new.Selection.Transform ||
		old.Selection.CompositionBounds != new.Selection.CompositionBounds {
		w.updateIMEWindows(imc)
	}
	if shouldCancelComposition(old, new) {
		windows.ImmNotifyIME(imc, windows.NI_COMPOSITIONSTR, windows.CPS_CANCEL, 0)
	}
}

func (w *window) SetAnimating(anim bool) {
	w.animating = anim
}

func (w *window) ProcessEvent(e event.Event) {
	w.w.ProcessEvent(e)
	w.loop.FlushEvents()
}

func (w *window) Event() event.Event {
	return w.loop.Event()
}

func (w *window) Invalidate() {
	w.loop.Invalidate()
}

func (w *window) Run(f func()) {
	w.loop.Run(f)
}

func (w *window) Frame(frame *op.Ops) {
	w.loop.Frame(frame)
}

func (w *window) wakeup() {
	if err := windows.PostMessage(w.hwnd, _WM_WAKEUP, 0, 0); err != nil {
		panic(err)
	}
}

func (w *window) draw(sync bool) {
	if w.config.Size.X == 0 || w.config.Size.Y == 0 {
		return
	}
	dpi := windows.GetWindowDPI(w.hwnd)
	cfg := configForDPI(dpi)
	w.ProcessEvent(frameEvent{
		FrameEvent: FrameEvent{
			Now:    time.Now(),
			Size:   w.config.Size,
			Metric: cfg,
		},
		Sync: sync,
	})
}

func (w *window) NewContext() (context, error) {
	sort.Slice(drivers, func(i, j int) bool {
		return drivers[i].priority < drivers[j].priority
	})
	var errs []string
	for _, b := range drivers {
		ctx, err := b.initializer(w)
		if err == nil {
			return ctx, nil
		}
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("NewContext: failed to create a GPU device, tried: %s", strings.Join(errs, ", "))
	}
	return nil, errors.New("NewContext: no available GPU drivers")
}

func (w *window) ReadClipboard() {
	w.readClipboard()
}

func (w *window) readClipboard() (cerr error) {
	defer func() {
		if cerr != nil {
			w.processDataEvent("")
		}
	}()

	if err := windows.OpenClipboard(w.hwnd); err != nil {
		return err
	}
	defer windows.CloseClipboard()
	mem, err := windows.GetClipboardData(windows.CF_UNICODETEXT)
	if err != nil {
		return err
	}
	ptr, err := windows.GlobalLock(mem)
	if err != nil {
		return err
	}
	defer windows.GlobalUnlock(mem)
	content := gowindows.UTF16PtrToString((*uint16)(unsafe.Pointer(ptr)))
	w.processDataEvent(content)
	return nil
}

func (w *window) processDataEvent(content string) {
	w.ProcessEvent(transfer.DataEvent{
		Type: "application/text",
		Open: func() io.ReadCloser {
			return io.NopCloser(strings.NewReader(content))
		},
	})
}

func (w *window) Configure(options []Option) {
	dpi := windows.GetWindowDPI(w.hwnd)
	metric := configForDPI(dpi)
	cnf := w.config
	cnf.apply(metric, options)
	w.config.Title = cnf.Title
	w.config.Decorated = cnf.Decorated
	w.config.MinSize = cnf.MinSize
	w.config.MaxSize = cnf.MaxSize
	w.config.panel = cnf.panel
	w.config.hiddenTitleBar = cnf.hiddenTitleBar // Clack patch (CLACK_PATCHES.md §22)
	windows.SetWindowText(w.hwnd, cnf.Title)

	style := windows.GetWindowLong(w.hwnd, windows.GWL_STYLE)
	var showMode int32
	var x, y, width, height int32
	swpStyle := uintptr(windows.SWP_FRAMECHANGED)
	if cnf.panel {
		// Clack patch (CLACK_PATCHES.md §8): a panel shows without the
		// focus, as Chromium's inactive show does.
		swpStyle |= windows.SWP_NOACTIVATE
	}
	if cnf.TopMost == w.config.TopMost {
		// Don't change the z-order if TopMost didn't change.
		swpStyle |= windows.SWP_NOZORDER
	}
	hwndAfter := windows.HWND_NOTOPMOST
	if cnf.TopMost {
		hwndAfter = windows.HWND_TOPMOST
	}
	w.config.TopMost = cnf.TopMost
	winStyle := uintptr(windows.WS_OVERLAPPEDWINDOW)
	style &^= winStyle
	switch cnf.Mode {
	case Minimized:
		style |= winStyle
		swpStyle |= windows.SWP_NOMOVE | windows.SWP_NOSIZE
		showMode = windows.SW_SHOWMINIMIZED

	case Maximized:
		style |= winStyle
		swpStyle |= windows.SWP_NOMOVE | windows.SWP_NOSIZE
		showMode = windows.SW_SHOWMAXIMIZED

	case Windowed:
		style |= winStyle
		showMode = windows.SW_SHOWNORMAL
		// Get target for client area size.
		width = int32(cnf.Size.X)
		height = int32(cnf.Size.Y)
		// Get the current window size and position.
		wr := windows.GetWindowRect(w.hwnd)
		x = wr.Left
		y = wr.Top
		if cnf.Decorated && cnf.hiddenTitleBar {
			// Clack patch (CLACK_PATCHES.md §22): the borders on the
			// left, right and bottom, no caption.
			f := w.hiddenTitleFrame()
			width += int32(f.X)
			height += int32(f.Y)
		} else if cnf.Decorated {
			// Compute client size and position. Note that the client size is
			// equal to the window size when we are in control of decorations.
			r := windows.Rect{
				Right:  width,
				Bottom: height,
			}
			windows.AdjustWindowRectEx(&r, uint32(style), 0, dwExStyle)
			width = r.Right - r.Left
			height = r.Bottom - r.Top
		} else {
			// Enable drop shadows when we draw decorations.
			windows.DwmExtendFrameIntoClientArea(w.hwnd, windows.Margins{-1, -1, -1, -1})
		}
		if b := cnf.bounds; cnf.boundsSet {
			// Clack patch (CLACK_PATCHES.md §25): the frame Bounds asks
			// for, in place of the size and the current position. A
			// maximized or minimized window is restored first, without
			// the focus: ShowWindow(SW_SHOWNORMAL) below would otherwise
			// put its restored rectangle back over this one (Chromium's
			// SetBounds takes WS_MAXIMIZE off for the same reason).
			x, y, width, height = int32(b.Min.X), int32(b.Min.Y), int32(b.Dx()), int32(b.Dy())
			if w.configured && windows.IsWindowVisible(w.hwnd) && (windows.IsIconic(w.hwnd) || windows.GetWindowPlacement(w.hwnd).IsMaximized()) {
				windows.ShowWindow(w.hwnd, windows.SW_SHOWNOACTIVATE)
			}
		}

	case Fullscreen:
		swpStyle |= windows.SWP_NOMOVE | windows.SWP_NOSIZE
		showMode = windows.SW_SHOWMAXIMIZED
	}

	// Disable the maximize button if MaxSize is set.
	if cnf.MaxSize != (image.Point{}) {
		style &^= windows.WS_MAXIMIZEBOX
		// Disable window resizing if MinSize and MaxSize are equal.
		if cnf.MinSize == cnf.MaxSize {
			style &^= windows.WS_THICKFRAME
		}
	}

	// Clack patch (CLACK_PATCHES.md §20): options that leave the mode
	// alone (a title, a minimum size) don't show a hidden window or
	// restore a minimized one, as Electron's setTitle and
	// setMinimumSize don't (SetWindowText, the size limits). Without
	// this, ShowWindow(SW_SHOWNORMAL) below brought a window hidden to
	// the tray back each time its title changed ("N new items"), and
	// restored a minimized one. A hidden window still takes its new size;
	// a minimized one keeps its iconic place (GetWindowRect is the icon's).
	if w.configured && cnf.Mode == w.config.Mode {
		hidden, iconic := !windows.IsWindowVisible(w.hwnd), windows.IsIconic(w.hwnd)
		if hidden || iconic {
			if hidden && !iconic {
				windows.SetWindowPos(w.hwnd, hwndAfter, x, y, width, height, swpStyle|windows.SWP_NOACTIVATE)
			}
			windows.SetWindowLong(w.hwnd, windows.GWL_STYLE, style)
			return
		}
	}
	// Clack patch (CLACK_PATCHES.md §23): the first Configure of a window
	// made with StartHidden places and styles it but doesn't show it.
	hide := !w.configured && cnf.startHidden
	w.configured = true
	if hide {
		swpStyle |= windows.SWP_NOACTIVATE
	}

	// Note: these invocation all trigger the windows callback method which may process a pending system.ActionCenter
	// action, so SetWindowPos should come first so as to not "overwrite" system.ActionCenter.
	windows.SetWindowPos(w.hwnd, hwndAfter, x, y, width, height, swpStyle)
	windows.SetWindowLong(w.hwnd, windows.GWL_STYLE, style)
	if hide {
		w.startedHidden = true
		if showMode == windows.SW_SHOWMAXIMIZED {
			// Clack patch (CLACK_PATCHES.md §25): started hidden maximized
			// or full screen, it shows that way when ActionRaise shows it.
			w.hiddenShow = showMode
		}
		return
	}
	w.hiddenShow = 0
	if cnf.panel && showMode == windows.SW_SHOWNORMAL {
		showMode = windows.SW_SHOWNOACTIVATE // Clack patch (CLACK_PATCHES.md §8)
	}
	windows.ShowWindow(w.hwnd, showMode)
}

func (w *window) WriteClipboard(mime string, s []byte) {
	w.writeClipboard(string(s))
}

func (w *window) writeClipboard(s string) error {
	if err := windows.OpenClipboard(w.hwnd); err != nil {
		return err
	}
	defer windows.CloseClipboard()
	if err := windows.EmptyClipboard(); err != nil {
		return err
	}
	u16, err := gowindows.UTF16FromString(s)
	if err != nil {
		return err
	}
	n := len(u16) * int(unsafe.Sizeof(u16[0]))
	mem, err := windows.GlobalAlloc(n)
	if err != nil {
		return err
	}
	ptr, err := windows.GlobalLock(mem)
	if err != nil {
		windows.GlobalFree(mem)
		return err
	}
	u16v := unsafe.Slice((*uint16)(ptr), len(u16))
	copy(u16v, u16)
	windows.GlobalUnlock(mem)
	if err := windows.SetClipboardData(windows.CF_UNICODETEXT, mem); err != nil {
		windows.GlobalFree(mem)
		return err
	}
	return nil
}

func (w *window) SetCursor(cursor pointer.Cursor) {
	c, err := loadCursor(cursor)
	if err != nil {
		c = resources.cursor
	}
	w.cursor = c
	if w.cursorIn {
		windows.SetCursor(w.cursor)
	}
}

// windowsCursor contains mapping from pointer.Cursor to an IDC.
var windowsCursor = [...]uint16{
	pointer.CursorDefault:                  windows.IDC_ARROW,
	pointer.CursorNone:                     0,
	pointer.CursorText:                     windows.IDC_IBEAM,
	pointer.CursorVerticalText:             windows.IDC_IBEAM,
	pointer.CursorPointer:                  windows.IDC_HAND,
	pointer.CursorCrosshair:                windows.IDC_CROSS,
	pointer.CursorAllScroll:                windows.IDC_SIZEALL,
	pointer.CursorColResize:                windows.IDC_SIZEWE,
	pointer.CursorRowResize:                windows.IDC_SIZENS,
	pointer.CursorGrab:                     windows.IDC_SIZEALL,
	pointer.CursorGrabbing:                 windows.IDC_SIZEALL,
	pointer.CursorNotAllowed:               windows.IDC_NO,
	pointer.CursorWait:                     windows.IDC_WAIT,
	pointer.CursorProgress:                 windows.IDC_APPSTARTING,
	pointer.CursorNorthWestResize:          windows.IDC_SIZENWSE,
	pointer.CursorNorthEastResize:          windows.IDC_SIZENESW,
	pointer.CursorSouthWestResize:          windows.IDC_SIZENESW,
	pointer.CursorSouthEastResize:          windows.IDC_SIZENWSE,
	pointer.CursorNorthSouthResize:         windows.IDC_SIZENS,
	pointer.CursorEastWestResize:           windows.IDC_SIZEWE,
	pointer.CursorWestResize:               windows.IDC_SIZEWE,
	pointer.CursorEastResize:               windows.IDC_SIZEWE,
	pointer.CursorNorthResize:              windows.IDC_SIZENS,
	pointer.CursorSouthResize:              windows.IDC_SIZENS,
	pointer.CursorNorthEastSouthWestResize: windows.IDC_SIZENESW,
	pointer.CursorNorthWestSouthEastResize: windows.IDC_SIZENWSE,
	// Clack patch 15: the panning cursors, if Chromium's images
	// (clack_pancursor_windows.go) fail to load.
	pointer.CursorMiddlePanning:           windows.IDC_SIZEALL,
	pointer.CursorMiddlePanningVertical:   windows.IDC_SIZEALL,
	pointer.CursorMiddlePanningHorizontal: windows.IDC_SIZEALL,
	pointer.CursorNorthPanning:            windows.IDC_SIZENS,
	pointer.CursorNorthEastPanning:        windows.IDC_SIZENESW,
	pointer.CursorEastPanning:             windows.IDC_SIZEWE,
	pointer.CursorSouthEastPanning:        windows.IDC_SIZENWSE,
	pointer.CursorSouthPanning:            windows.IDC_SIZENS,
	pointer.CursorSouthWestPanning:        windows.IDC_SIZENESW,
	pointer.CursorWestPanning:             windows.IDC_SIZEWE,
	pointer.CursorNorthWestPanning:        windows.IDC_SIZENWSE,
}

func loadCursor(cursor pointer.Cursor) (syscall.Handle, error) {
	switch cursor {
	case pointer.CursorDefault:
		return resources.cursor, nil
	case pointer.CursorNone:
		return 0, nil
	default:
		if h, ok := loadPanCursor(cursor); ok { // Clack patch 15
			return h, nil
		}
		return windows.LoadCursor(windowsCursor[cursor])
	}
}

func (w *window) ShowTextInput(show bool) {}

func (w *window) SetInputHint(_ key.InputHint) {}

func (w *window) HDC() syscall.Handle {
	return w.hdc
}

func (w *window) HWND() (syscall.Handle, int, int) {
	return w.hwnd, w.config.Size.X, w.config.Size.Y
}

func (w *window) Perform(acts system.Action) {
	walkActions(acts, func(a system.Action) {
		switch a {
		case system.ActionCenter:
			if w.config.Mode != Windowed {
				break
			}
			r := windows.GetWindowRect(w.hwnd)
			dx := r.Right - r.Left
			dy := r.Bottom - r.Top
			// Center in the usable area of the current monitor.
			mi, ok := windows.GetMonitorInfo(r, windows.MONITOR_DEFAULTTONULL)
			if !ok {
				break
			}
			area := mi.WorkArea
			x := (area.Right + area.Left - dx) / 2
			y := (area.Bottom + area.Top - dy) / 2
			windows.SetWindowPos(w.hwnd, 0, x, y, dx, dy, windows.SWP_NOZORDER|windows.SWP_FRAMECHANGED)
		case system.ActionRaise:
			// Clack patch (CLACK_PATCHES.md §13): a window hidden with
			// Window.Hide comes back the way Electron's BrowserWindow.show
			// shows it (Chromium's HWNDMessageHandler::Show for
			// kNormal): SW_SHOW when it was maximized, so it stays
			// maximized, SW_SHOWNORMAL otherwise.
			if !windows.IsWindowVisible(w.hwnd) {
				cmd := int32(windows.SW_SHOWNORMAL)
				if w.config.Mode == Maximized {
					cmd = windows.SW_SHOW
				}
				if s := w.hiddenShow; s != 0 {
					// Clack patch (CLACK_PATCHES.md §25): a window started
					// hidden maximized or full screen (StartHidden and
					// one of those modes) shows in it.
					cmd, w.hiddenShow = s, 0
				}
				windows.ShowWindow(w.hwnd, cmd)
			}
			// A minimized window has to be restored before it can be
			// brought to the front; SetForegroundWindow on its own
			// leaves it minimized, so raising an iconified window did
			// nothing at all.
			if windows.IsIconic(w.hwnd) {
				windows.ShowWindow(w.hwnd, windows.SW_RESTORE)
			}
			windows.SetForegroundWindow(w.hwnd)
			windows.SetWindowPos(w.hwnd, windows.HWND_TOP, 0, 0, 0, 0,
				windows.SWP_NOMOVE|windows.SWP_NOSIZE|windows.SWP_SHOWWINDOW)
		case system.ActionClose:
			windows.PostMessage(w.hwnd, windows.WM_CLOSE, 0, 0)
		}
	})
}

// hide is Window.Hide (Clack patch, CLACK_PATCHES.md §13): Chromium's
// HWNDMessageHandler::Hide, which Electron's BrowserWindow.hide calls.
// SetWindowPos rather than ShowWindow(SW_HIDE), which would activate
// another window itself while the activation may be changing.
func (w *window) hide() bool {
	windows.SetWindowPos(w.hwnd, 0, 0, 0, 0, 0,
		windows.SWP_HIDEWINDOW|windows.SWP_NOACTIVATE|windows.SWP_NOMOVE|
			windows.SWP_NOOWNERZORDER|windows.SWP_NOSIZE|windows.SWP_NOZORDER)
	return !windows.IsWindowVisible(w.hwnd)
}

// setCaretBlinkFromOS is Chromium's OsSettingsProviderWin::CaretBlinkInterval
// (Clack patch, CLACK_PATCHES.md §13): GetCaretBlinkTime, the default
// when it fails (0), no blinking for INFINITE. Read as a window opens:
// Windows has no notification for the setting, and Chromium reads it
// when it builds a renderer's preferences.
func setCaretBlinkFromOS() {
	switch t := windows.GetCaretBlinkTime(); t {
	case 0:
	case windows.INFINITE:
		widget.SetCaretBlinkInterval(0)
	default:
		widget.SetCaretBlinkInterval(time.Duration(t) * time.Millisecond)
	}
}

func convertKeyCode(code uintptr) (key.Name, bool) {
	if '0' <= code && code <= '9' || 'A' <= code && code <= 'Z' {
		return key.Name(rune(code)), true
	}
	if windows.VK_NUMPAD0 <= code && code <= windows.VK_NUMPAD9 {
		return key.Name('0' + rune(code-windows.VK_NUMPAD0)), true
	}
	var r key.Name

	switch code {
	case windows.VK_ADD:
		r = "+"
	case windows.VK_SUBTRACT:
		r = "-"
	case windows.VK_MULTIPLY:
		r = "*"
	case windows.VK_DIVIDE:
		r = "/"
	case windows.VK_DECIMAL:
		r = "."
	case windows.VK_ESCAPE:
		r = key.NameEscape
	case windows.VK_LEFT:
		r = key.NameLeftArrow
	case windows.VK_RIGHT:
		r = key.NameRightArrow
	case windows.VK_RETURN:
		r = key.NameReturn
	case windows.VK_UP:
		r = key.NameUpArrow
	case windows.VK_DOWN:
		r = key.NameDownArrow
	case windows.VK_HOME:
		r = key.NameHome
	case windows.VK_END:
		r = key.NameEnd
	case windows.VK_BACK:
		r = key.NameDeleteBackward
	case windows.VK_DELETE:
		r = key.NameDeleteForward
	case windows.VK_PRIOR:
		r = key.NamePageUp
	case windows.VK_NEXT:
		r = key.NamePageDown
	case windows.VK_F1:
		r = key.NameF1
	case windows.VK_F2:
		r = key.NameF2
	case windows.VK_F3:
		r = key.NameF3
	case windows.VK_F4:
		r = key.NameF4
	case windows.VK_F5:
		r = key.NameF5
	case windows.VK_F6:
		r = key.NameF6
	case windows.VK_F7:
		r = key.NameF7
	case windows.VK_F8:
		r = key.NameF8
	case windows.VK_F9:
		r = key.NameF9
	case windows.VK_F10:
		r = key.NameF10
	case windows.VK_F11:
		r = key.NameF11
	case windows.VK_F12:
		r = key.NameF12
	case windows.VK_TAB:
		r = key.NameTab
	case windows.VK_SPACE:
		r = key.NameSpace
	case windows.VK_OEM_1:
		r = ";"
	case windows.VK_OEM_PLUS:
		r = "+"
	case windows.VK_OEM_COMMA:
		r = ","
	case windows.VK_OEM_MINUS:
		r = "-"
	case windows.VK_OEM_PERIOD:
		r = "."
	case windows.VK_OEM_2:
		r = "/"
	case windows.VK_OEM_3:
		r = "`"
	case windows.VK_OEM_4:
		r = "["
	case windows.VK_OEM_5, windows.VK_OEM_102:
		r = "\\"
	case windows.VK_OEM_6:
		r = "]"
	case windows.VK_OEM_7:
		r = "'"
	case windows.VK_CONTROL:
		r = key.NameCtrl
	case windows.VK_SHIFT:
		r = key.NameShift
	case windows.VK_MENU:
		r = key.NameAlt
	case windows.VK_LWIN, windows.VK_RWIN:
		r = key.NameSuper
	default:
		return "", false
	}
	return r, true
}

func configForDPI(dpi int) unit.Metric {
	const inchPrDp = 1.0 / 96.0
	ppdp := float32(dpi) * inchPrDp
	return unit.Metric{
		PxPerDp: ppdp,
		PxPerSp: ppdp,
	}
}

func (Win32ViewEvent) implementsViewEvent() {}
func (Win32ViewEvent) ImplementsEvent()     {}
func (w Win32ViewEvent) Valid() bool {
	return w != (Win32ViewEvent{})
}

// LOWORD (minwindef.h)
func loWord(val uint32) uint16 {
	return uint16(val & 0xFFFF)
}

// GET_POINTERID_WPARAM (winuser.h)
func getPointerIDwParam(wParam uintptr) pointer.ID {
	return pointer.ID(loWord(uint32(wParam)))
}

func getPointerButtons(pi windows.PointerInfo) pointer.Buttons {
	var btns pointer.Buttons

	if pi.PointerFlags&windows.POINTER_FLAG_FIRSTBUTTON != 0 {
		btns |= pointer.ButtonPrimary
	} else {
		btns &^= pointer.ButtonPrimary
	}
	if pi.PointerFlags&windows.POINTER_FLAG_SECONDBUTTON != 0 {
		btns |= pointer.ButtonSecondary
	} else {
		btns &^= pointer.ButtonSecondary
	}
	if pi.PointerFlags&windows.POINTER_FLAG_THIRDBUTTON != 0 {
		btns |= pointer.ButtonTertiary
	} else {
		btns &^= pointer.ButtonTertiary
	}
	if pi.PointerFlags&windows.POINTER_FLAG_FOURTHBUTTON != 0 {
		btns |= pointer.ButtonQuaternary
	} else {
		btns &^= pointer.ButtonQuaternary
	}
	if pi.PointerFlags&windows.POINTER_FLAG_FIFTHBUTTON != 0 {
		btns |= pointer.ButtonQuinary
	} else {
		btns &^= pointer.ButtonQuinary
	}

	return btns
}

// schemesURI is a list of schemes, comma separated, that must be
// defined using -X compiler ldflag, that used in gogio.
var schemesURI string

func init() {
	if schemesURI == "" {
		return
	}

	currentSchemes := strings.Split(schemesURI, ",")
	oldSchemes := registeredSchemes(ID)

	for _, s := range currentSchemes {
		for i, o := range oldSchemes {
			if s == o {
				oldSchemes = append(oldSchemes[:i], oldSchemes[i+1:]...)
				break
			}
		}
	}

	if len(oldSchemes) > 0 {
		go unregisterSchemes(ID, oldSchemes)
	}

	if len(currentSchemes) == 0 {
		return
	}

	// On Windows, launching the app using a URI will start a new instance of the app,
	// a new window. That behavior, by default, doesn't align with iOS/Android/macOS, where
	// the deeplink sends the event to the running app (if any). We are emulating it.
	if hwnd, _ := windows.FindWindow(ID); hwnd != 0 {
		if u := startupURI(); u != "" {
			broadcastURI(hwnd, u)
		}
		os.Exit(0)
		return
	}

	go registerSchemes(ID, currentSchemes)
}

func startupURI() string {
	if len(os.Args) == 3 && os.Args[1] == "-gio_launch_url" {
		return os.Args[2]
	}
	return ""
}

func processURLEvent(rawurl string) bool {
	if rawurl == "" {
		return false
	}

	evt, err := newURLEvent(rawurl)
	if err != nil {
		return false
	}

	for _, scheme := range strings.Split(schemesURI, ",") {
		if strings.EqualFold(scheme, evt.URL.Scheme) {
			processGlobalEvent(evt)
			return true
		}
	}

	return false
}

func broadcastURI(hwnd syscall.Handle, uri string) {
	data, err := syscall.UTF16FromString(uri)
	if err != nil {
		return // Only happens if uri contains NULL character.
	}

	pinner := new(runtime.Pinner)
	defer pinner.Unpin()
	pinner.Pin(unsafe.Pointer(unsafe.SliceData(data)))

	msg := &windows.CopyDataStruct{
		DwData: copyDataURLType,
		CbData: uint32(len(data) * int(unsafe.Sizeof(data[0]))),
		LpData: uintptr(unsafe.Pointer(unsafe.SliceData(data))),
	}
	pinner.Pin(unsafe.Pointer(msg))

	// SendMessage blocks until the message is processed.
	windows.SendMessage(hwnd, windows.WM_COPYDATA, 0, uintptr(unsafe.Pointer(msg)))
}

func registeredSchemes(appid string) []string {
	meta, err := registry.OpenKey(registry.CURRENT_USER, `Software\\`+appid, registry.ALL_ACCESS)
	if err != nil {
		return nil
	}
	defer meta.Close()

	schemes, _, _ := meta.GetStringsValue("URISchemes")
	return schemes
}

func registerSchemes(appid string, schemes []string) error {
	reg := func(scheme string) error {
		key, existent, err := registry.CreateKey(registry.CURRENT_USER, `Software\\Classes\\`+scheme, registry.ALL_ACCESS)
		if err != nil {
			return err
		}
		defer key.Close()

		if existent {
			// Check if the existent key belongs to the current application
			id, _, err := key.GetStringValue("appid")
			if err != nil || id != appid {
				return fmt.Errorf("scheme %s already registered by another application", scheme)
			}
		}

		path, err := os.Executable()
		if err != nil {
			return err
		}

		if err = key.SetStringValue("", "URL:"+scheme+" Protocol"); err != nil {
			return err
		}
		if err = key.SetStringValue("URL Protocol", ""); err != nil {
			return err
		}
		if err = key.SetStringValue("appid", appid); err != nil {
			return err
		}

		icon, _, err := registry.CreateKey(key, `DefaultIcon`, registry.ALL_ACCESS)
		if err != nil {
			return err
		}
		defer icon.Close()

		if err = icon.SetStringValue("", `"`+path+`",1`); err != nil {
			return err
		}

		cmd, _, err := registry.CreateKey(key, `shell\\open\\command`, registry.ALL_ACCESS)
		if err != nil {
			return err
		}
		defer cmd.Close()

		if err = cmd.SetStringValue("", `"`+path+`" -gio_launch_url "%1"`); err != nil {
			return err
		}

		return nil
	}

	for _, scheme := range schemes {
		if scheme == "" {
			continue // just in case
		}
		if err := reg(scheme); err != nil {
			return err
		}
	}

	meta, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\\`+appid, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer meta.Close()

	if err = meta.SetStringsValue("URISchemes", schemes); err != nil {
		return err
	}

	return nil
}

func unregisterSchemes(appid string, schemes []string) {
	classes, err := registry.OpenKey(registry.CURRENT_USER, `Software\\Classes`, registry.ALL_ACCESS)
	if err != nil {
		return
	}
	defer classes.Close()

	for _, scheme := range schemes {
		if scheme == "" {
			continue // just in case
		}

		key, err := registry.OpenKey(classes, scheme, registry.ALL_ACCESS)
		if err != nil {
			continue
		}

		id, _, err := key.GetStringValue("appid")
		if err == nil && id != appid {
			continue
		}

		for _, k := range []string{`DefaultIcon`, `shell\\open\\command`, `shell\\open`, `shell`} {
			registry.DeleteKey(key, k)
		}

		if err := key.Close(); err != nil {
			continue
		}

		registry.DeleteKey(classes, scheme)
	}
}
