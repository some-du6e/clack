// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

package cocoatest

/*
#cgo CFLAGS: -Werror -Wno-deprecated-declarations -fobjc-arc -x objective-c
#cgo LDFLAGS: -framework AppKit

#include <stdint.h>
#include <stdlib.h>
#include <CoreGraphics/CoreGraphics.h>

typedef struct {
	CGFloat x, y, w, h;
} cocoatest_rect;

typedef struct {
	int fullSizeContent, titlebarTransparent, titleHidden, tabbingDisallowed;
	int buttonsHidden[3];
	cocoatest_rect buttons[3];
	cocoatest_rect container;
	cocoatest_rect frame;
	cocoatest_rect content;
	cocoatest_rect opaqueForMove;
	int hasOpaqueForMove;
	int visible, zoomed, miniaturized, fullScreen, key, appActive;
	char title[256];
} cocoatest_window;

void cocoatest_state(uintptr_t view, cocoatest_window *s);
int cocoatest_hit(uintptr_t view, CGFloat x, CGFloat y, char *cls, int n);
void cocoatest_post_mouse(uintptr_t view, int down, CGFloat x, CGFloat y, int clicks);
void cocoatest_press(uintptr_t view, int button);
void cocoatest_order_front(uintptr_t view);
void cocoatest_set_double_click(const char *v);
int cocoatest_reopen(int visible, int *handled);
void cocoatest_post_did_become_active(void);
int cocoatest_key_equivalent(const char *key, int mods);
void cocoatest_on_main(uintptr_t h);
void cocoatest_track(uintptr_t view);
void cocoatest_alive(int *view, int *window);
*/
import "C"

import (
	"runtime/cgo"
	"unsafe"
)

// Rect is a frame in window points, its origin at the window's top left.
type Rect struct{ X, Y, W, H float64 }

// Button is one of the window's standard buttons.
type Button int

const (
	Close Button = iota
	Minimize
	Zoom
)

// Window is what AppKit holds for a window, from its content view.
type Window struct {
	FullSizeContent     bool // NSWindowStyleMaskFullSizeContentView
	TitlebarTransparent bool
	TitleHidden         bool
	TabbingDisallowed   bool
	ButtonsHidden       [3]bool // Close, Minimize, Zoom
	Buttons             [3]Rect
	// TitlebarContainer is the buttons' title bar container (their
	// superview's superview, as Electron's WindowButtonsProxy finds it).
	TitlebarContainer Rect
	Frame             Rect // the window's size (X, Y zero)
	Content           Rect // the content view's frame in the window
	// OpaqueForMove is the view's _opaqueRectForWindowMoveWhenInTitlebar
	// (in the view, its origin at the top left) when AppKit has the hook.
	OpaqueForMove    Rect
	HasOpaqueForMove bool
	Visible          bool
	Zoomed           bool
	Miniaturized     bool
	FullScreen       bool
	Key              bool // the key window
	AppActive        bool // NSApp is the active app
	Title            string
}

func rect(r C.cocoatest_rect) Rect {
	return Rect{float64(r.x), float64(r.y), float64(r.w), float64(r.h)}
}

// State reads the window of view (an NSView, Gio's AppKitViewEvent.View).
// Main thread.
func State(view uintptr) Window {
	var s C.cocoatest_window
	C.cocoatest_state(C.uintptr_t(view), &s)
	w := Window{
		FullSizeContent:     s.fullSizeContent != 0,
		TitlebarTransparent: s.titlebarTransparent != 0,
		TitleHidden:         s.titleHidden != 0,
		TabbingDisallowed:   s.tabbingDisallowed != 0,
		TitlebarContainer:   rect(s.container),
		Frame:               rect(s.frame),
		Content:             rect(s.content),
		OpaqueForMove:       rect(s.opaqueForMove),
		HasOpaqueForMove:    s.hasOpaqueForMove != 0,
		Visible:             s.visible != 0,
		Zoomed:              s.zoomed != 0,
		Miniaturized:        s.miniaturized != 0,
		FullScreen:          s.fullScreen != 0,
		Key:                 s.key != 0,
		AppActive:           s.appActive != 0,
		Title:               C.GoString(&s.title[0]),
	}
	for i := range w.Buttons {
		w.ButtonsHidden[i] = s.buttonsHidden[i] != 0
		w.Buttons[i] = rect(s.buttons[i])
	}
	return w
}

// Hit is what a click at a window point lands on.
type Hit int

const (
	HitNone    Hit = iota // nothing in the window (nil)
	HitContent            // the window's content view
	HitButton             // one of the window's standard buttons
	HitOther              // another view (the title bar's own)
)

// HitTest is the view a click at (x, y) (window points from the top
// left) goes to: the window's frame view's hitTest:, as NSWindow's
// sendEvent: finds a mouse-down's target, and that view's class. Main
// thread.
func HitTest(view uintptr, x, y float64) (Hit, string) {
	var cls [128]C.char
	h := C.cocoatest_hit(C.uintptr_t(view), C.CGFloat(x), C.CGFloat(y), &cls[0], C.int(len(cls)))
	return Hit(h), C.GoString(&cls[0])
}

// PostMouse queues a left button press (down) or release at (x, y)
// (window points from the top left) with the click count, for the run
// loop to deliver through NSApp sendEvent:. Main thread.
func PostMouse(view uintptr, down bool, x, y float64, clicks int) {
	C.cocoatest_post_mouse(C.uintptr_t(view), cint(down), C.CGFloat(x), C.CGFloat(y), C.int(clicks))
}

// Press clicks one of the window's standard buttons (performClick:), as
// a click on it does. Main thread.
func Press(view uintptr, b Button) { C.cocoatest_press(C.uintptr_t(view), C.int(b)) }

// OrderFront activates the app and makes the window key and front without
// going through Gio (what Clack's Dock reopen does). Main thread.
func OrderFront(view uintptr) { C.cocoatest_order_front(C.uintptr_t(view)) }

// SetDoubleClickAction sets AppleActionOnDoubleClick in the app's own
// defaults (over the global one), or removes it when set is false. Main
// thread.
func SetDoubleClickAction(v string, set bool) {
	if !set {
		C.cocoatest_set_double_click(nil)
		return
	}
	cs := C.CString(v)
	defer C.free(unsafe.Pointer(cs))
	C.cocoatest_set_double_click(cs)
}

// Reopen calls the app delegate's
// applicationShouldHandleReopen:hasVisibleWindows:, as a click on the
// Dock icon does. handled is false when the delegate has no such method.
// Main thread.
func Reopen(hasVisibleWindows bool) (ret, handled bool) {
	var h C.int
	r := C.cocoatest_reopen(cint(hasVisibleWindows), &h)
	return r != 0, h != 0
}

// PostDidBecomeActive posts NSApplicationDidBecomeActiveNotification, as
// AppKit does when the app comes to the front (⌘Tab, the Dock). Main
// thread.
func PostDidBecomeActive() { C.cocoatest_post_did_become_active() }

// Modifier flags for KeyEquivalent.
const (
	Cmd   = 1
	Shift = 2
	Opt   = 4
	Ctrl  = 8
)

// KeyEquivalent offers the main menu a key down for key with mods, as
// NSApp does for a key press with ⌘, and reports whether a menu item took
// it. Main thread.
func KeyEquivalent(key string, mods int) bool {
	cs := C.CString(key)
	defer C.free(unsafe.Pointer(cs))
	return C.cocoatest_key_equivalent(cs, C.int(mods)) != 0
}

// Track keeps weak references to view and its window, for Alive. Main
// thread.
func Track(view uintptr) { C.cocoatest_track(C.uintptr_t(view)) }

// Alive reports whether the view and the window Track last saw still
// exist (their weak references aren't nil). Main thread.
func Alive() (view, window bool) {
	var v, w C.int
	C.cocoatest_alive(&v, &w)
	return v != 0, w != 0
}

// OnMain runs f on the main thread from the main dispatch queue (the run
// loop app.Main runs) and waits for it. Not from the main thread.
func OnMain(f func()) {
	done := make(chan struct{})
	h := cgo.NewHandle(func() {
		defer close(done)
		f()
	})
	C.cocoatest_on_main(C.uintptr_t(h))
	<-done
}

//export cocoatestRun
func cocoatestRun(h C.uintptr_t) {
	hd := cgo.Handle(h)
	f := hd.Value().(func())
	hd.Delete()
	f()
}

func cint(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
