// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"image"
	"sync"
	"time"

	syscall "golang.org/x/sys/windows"

	"gioui.org/app/internal/windows"
	"gioui.org/f32"
	"gioui.org/io/pointer"
)

// Clack patch (CLACK_PATCHES.md §22): HiddenTitleBar on Windows, as
// Electron 45 lays out a frameless window with a titleBarOverlay
// (shell/browser/ui/win/electron_desktop_window_tree_host_win.cc,
// shell/browser/ui/views/win_frame_view.cc) over Chromium's
// HWNDMessageHandler (ui/views/win/hwnd_message_handler.cc):
//
//   - The window keeps WS_OVERLAPPEDWINDOW, so DWM keeps its shadow,
//     rounded corners and Snap, and Windows its system menu and
//     animations. WM_NCCALCSIZE takes the caption: restored, the client
//     area is inset by the frame thickness on the left, right and bottom
//     (where the resize borders stay, outside the window's visible edge)
//     and not at the top; maximized, it is the monitor's work area, kept
//     2 px off an edge with an auto-hide taskbar (GetClientAreaInsets,
//     OnNCCalcSize).
//   - WM_NCHITTEST (WinFrameView::NonClientHitTest): the caption buttons'
//     areas are HTMINBUTTON, HTMAXBUTTON (Snap Layouts) and HTCLOSE; the
//     top frame-thickness band of a restored, resizable window HTTOP
//     (HTTOPLEFT/HTTOPRIGHT in the corners); ActionMove areas HTCAPTION.
//     Points in the resize borders outside the client area go to
//     DefWindowProc.
//   - Pointer input over the caption buttons arrives as WM_NCPOINTER*
//     messages. It is delivered to the program as client pointer events
//     (Chromium routes the non-client mouse messages to the caption
//     button views); a press there takes the capture, as a client press
//     does, so the rest of the click comes as client messages. The press
//     is not passed on: DefWindowProc would paint classic caption buttons
//     and run the button itself. Moves over the caption and the borders
//     reach the program too (hover ends over them), and still go on to
//     DefWindowProc, as do presses on the caption and the borders (move,
//     resize, double-click, the system menu).
//   - The pointer leaving the window from the caption (WM_POINTERLEAVE,
//     WM_NCMOUSELEAVE with the cursor no longer over the window) is a
//     pointer.Leave.

// _WM_CLACK_FRAME asks the window to recompute its non-client area (an
// auto-hide taskbar appeared or went away while it was maximized).
const _WM_CLACK_FRAME = windows.WM_USER + 0x60

// hiddenTitle reports whether the window has a hidden title bar now: the
// option on a decorated window with its caption style (not full screen).
func (w *window) hiddenTitle() bool {
	if !w.config.hiddenTitleBar || !w.config.Decorated {
		return false
	}
	return windows.GetWindowLong(w.hwnd, windows.GWL_STYLE)&windows.WS_CAPTION != 0
}

// frameThickness is GetResizableFrameThicknessFromMonitorInPixels for the
// window: SM_CXSIZEFRAME plus SM_CXPADDEDBORDER at its DPI (the window has
// WS_CAPTION, so no pixel comes off).
func (w *window) frameThickness() int {
	dpi := windows.GetWindowDPI(w.hwnd)
	return windows.GetSystemMetricsForDpi(windows.SM_CXSIZEFRAME, dpi) +
		windows.GetSystemMetricsForDpi(windows.SM_CXPADDEDBORDER, dpi)
}

// hiddenTitleFrame is the window size around a client area of a restored
// window with a hidden title bar: the borders on the left, right and bottom.
func (w *window) hiddenTitleFrame() image.Point {
	ft := w.frameThickness()
	return image.Pt(2*ft, ft)
}

// hiddenTitleCalcSize is WM_NCCALCSIZE for a hidden title bar: r is the
// proposed window rectangle, turned into the client rectangle.
func (w *window) hiddenTitleCalcSize(r *windows.Rect) {
	if windows.GetWindowPlacement(w.hwnd).IsMaximized() {
		// Windows puts a maximized window's frame off the monitor's
		// edges; the client area is the work area.
		mi, ok := windows.GetMonitorInfo(*r, windows.MONITOR_DEFAULTTONULL)
		if !ok {
			return
		}
		*r = mi.WorkArea
		// An auto-hide taskbar on an edge: stay 2 px off it so the
		// window isn't taken for a full-screen one, which keeps the
		// taskbar from showing (Chromium's kAutoHideTaskbarThicknessPx;
		// a top edge moves the bottom up, as Chromium does for a
		// system-drawn frame).
		mon := windows.MonitorFromWindow(w.hwnd, windows.MONITOR_DEFAULTTONULL)
		edges := w.autoHideEdges(mon)
		const thick = 2
		if edges&(1<<windows.ABE_LEFT) != 0 {
			r.Left += thick
		}
		if edges&(1<<windows.ABE_TOP) != 0 {
			r.Bottom--
		}
		if edges&(1<<windows.ABE_RIGHT) != 0 {
			r.Right -= thick
		}
		if edges&(1<<windows.ABE_BOTTOM) != 0 {
			r.Bottom -= thick
		}
		return
	}
	ft := int32(w.frameThickness())
	r.Left += ft
	r.Right -= ft
	r.Bottom -= ft
}

// hiddenTitleHitTest is WM_NCHITTEST for a hidden title bar at client
// point (x, y). lParam is the message's.
func (w *window) hiddenTitleHitTest(x, y int, lParam uintptr) uintptr {
	if x < 0 || y < 0 || x >= w.config.Size.X || y >= w.config.Size.Y {
		// the resize borders outside the client area
		return windows.DefWindowProc(w.hwnd, windows.WM_NCHITTEST, 0, lParam)
	}
	topBand, corner := 0, 0
	style := windows.GetWindowLong(w.hwnd, windows.GWL_STYLE)
	if w.config.Mode == Windowed && style&windows.WS_THICKFRAME != 0 {
		// WinFrameView: the frame's top border thickness, and
		// kResizeCornerWidth (16 DIPs) less the side border.
		topBand = w.frameThickness()
		dpi := windows.GetWindowDPI(w.hwnd)
		corner = (16*dpi+48)/96 - windows.GetSystemMetricsForDpi(windows.SM_CXSIZEFRAME, dpi)
	}
	act, ok := w.w.ActionAt(f32.Pt(float32(x), float32(y)))
	switch captionHit(image.Pt(x, y), w.config.Size, w.config.Mode == Fullscreen, topBand, corner, act, ok) {
	case captionMove:
		return windows.HTCAPTION
	case captionMinimize:
		return windows.HTMINBUTTON
	case captionMaximize:
		return windows.HTMAXBUTTON
	case captionClose:
		return windows.HTCLOSE
	case captionTop:
		return windows.HTTOP
	case captionTopLeft:
		return windows.HTTOPLEFT
	case captionTopRight:
		return windows.HTTOPRIGHT
	}
	return windows.HTCLIENT
}

// hiddenTitlePointer handles a WM_NCPOINTER* message (it reports whether
// the message is done with; false lets DefWindowProc have it).
func (w *window) hiddenTitlePointer(msg uint32, wParam, lParam uintptr) bool {
	pid := getPointerIDwParam(wParam)
	pi, err := windows.GetPointerInfo(uint32(pid))
	if err != nil || pi.PointerType != windows.PT_MOUSE {
		// touch and pen: the system's (DefWindowProc turns them into
		// mouse input, which runs the caption buttons)
		return false
	}
	hit := (wParam >> 16) & 0xffff
	button := hit == windows.HTMINBUTTON || hit == windows.HTMAXBUTTON || hit == windows.HTCLOSE
	switch msg {
	case windows.WM_NCPOINTERUPDATE:
		kind := pointer.Move
		if pi.PointerFlags&windows.POINTER_FLAG_CANCELED != 0 {
			kind = pointer.Cancel
		}
		w.ncPointerEvent(pi, pid, kind, lParam)
		return false // the system tracks the caption too (Snap Layouts)
	case windows.WM_NCPOINTERDOWN:
		if !button {
			return false // move, resize, double-click, the system menu
		}
		windows.SetCapture(w.hwnd)
		w.ncPressed = true
		w.ncPointerEvent(pi, pid, pointer.Press, lParam)
		return true
	case windows.WM_NCPOINTERUP:
		// over a button, or the end of a press a button took (the
		// program saw the press: it gets the release wherever it is)
		if !button && !w.ncPressed {
			return false
		}
		w.ncPressed = false
		windows.ReleaseCapture()
		w.ncPointerEvent(pi, pid, pointer.Release, lParam)
		return true
	}
	return false
}

// ncPointerEvent is pointerUpdate for a non-client message, without taking
// the focus on a move.
func (w *window) ncPointerEvent(pi windows.PointerInfo, pid pointer.ID, kind pointer.Kind, lParam uintptr) {
	if kind == pointer.Press && !w.config.Focused {
		windows.SetFocus(w.hwnd)
	}
	w.mousePID = pid
	x, y := coordsFromlParam(lParam)
	np := windows.Point{X: int32(x), Y: int32(y)}
	windows.ScreenToClient(w.hwnd, &np)
	w.ProcessEvent(pointer.Event{
		Kind:      kind,
		Source:    pointer.Mouse,
		Position:  f32.Point{X: float32(np.X), Y: float32(np.Y)},
		PointerID: pid,
		Buttons:   getPointerButtons(pi),
		Time:      windows.GetMessageTime(),
		Modifiers: getModifiers(),
	})
}

// hiddenTitleLeave delivers pointer.Leave once the cursor has left the
// window: WM_POINTERLEAVE and WM_NCMOUSELEAVE also come as it crosses
// between the caption and the client area (Chromium's
// HandleMouseEventInternal checks WindowFromPoint the same way).
func (w *window) hiddenTitleLeave(pid pointer.ID) {
	if windows.GetCapture() == w.hwnd {
		return
	}
	p, ok := windows.GetCursorPos()
	if !ok || windows.WindowFromPoint(p) == w.hwnd {
		return
	}
	np := p
	windows.ScreenToClient(w.hwnd, &np)
	w.ProcessEvent(pointer.Event{
		Kind:      pointer.Leave,
		Source:    pointer.Mouse,
		Position:  f32.Point{X: float32(np.X), Y: float32(np.Y)},
		PointerID: pid,
		Time:      windows.GetMessageTime(),
	})
}

// autoHide caches the auto-hide taskbar edges of each monitor:
// SHAppBarMessage calls into the shell, so it runs off the window thread
// (Chromium's GetAppbarAutohideEdges does the same) and a change asks the
// window for a new frame.
var autoHide struct {
	mu    sync.Mutex
	edges map[syscall.Handle]autoHideEdges
}

type autoHideEdges struct {
	mask    uint32
	checked time.Time
	busy    bool
}

// autoHideEdges is the monitor's cached edges (bit ABE_* set: an auto-hide
// taskbar there), refreshed in the background when older than a second.
func (w *window) autoHideEdges(mon syscall.Handle) uint32 {
	if mon == 0 {
		return 0
	}
	autoHide.mu.Lock()
	defer autoHide.mu.Unlock()
	if autoHide.edges == nil {
		autoHide.edges = map[syscall.Handle]autoHideEdges{}
	}
	e := autoHide.edges[mon]
	if !e.busy && time.Since(e.checked) > time.Second {
		e.busy = true
		autoHide.edges[mon] = e
		hwnd := w.hwnd
		go func(old uint32) {
			var mask uint32
			for _, edge := range []uint32{windows.ABE_LEFT, windows.ABE_TOP, windows.ABE_RIGHT, windows.ABE_BOTTOM} {
				if windows.AutoHideTaskbar(mon, edge) {
					mask |= 1 << edge
				}
			}
			autoHide.mu.Lock()
			autoHide.edges[mon] = autoHideEdges{mask: mask, checked: time.Now()}
			autoHide.mu.Unlock()
			if mask != old {
				windows.PostMessage(hwnd, _WM_CLACK_FRAME, 0, 0)
			}
		}(e.mask)
	}
	return e.mask
}

// refreshFrame recomputes the non-client area (_WM_CLACK_FRAME).
func (w *window) refreshFrame() {
	windows.SetWindowPos(w.hwnd, 0, 0, 0, 0, 0,
		windows.SWP_FRAMECHANGED|windows.SWP_NOMOVE|windows.SWP_NOSIZE|
			windows.SWP_NOZORDER|windows.SWP_NOACTIVATE|windows.SWP_NOOWNERZORDER)
}
