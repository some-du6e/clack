// Package nativemenu pops up a context menu the operating system draws,
// the way Electron's Menu.popup does for Slack's text-field menu
// (buildMenuForTextInput: spelling suggestions, Add to Dictionary,
// Search with Google, Cut, Copy, Paste).
//
// Electron (shell/browser/api/electron_api_menu_mac.mm) runs an NSMenu
// with popUpMenuPositioningItem:atLocation:inView: on macOS, at the
// pointer, with the view's height minus y as the location. On Windows
// and Linux it runs views::MenuRunner (electron_api_menu_views.cc,
// CONTEXT_MENU | HAS_MNEMONICS), a menu Chromium paints itself. Clack
// uses NSMenu on macOS and the system's own popup menu (TrackPopupMenuEx)
// on Windows; elsewhere Supported reports false and callers keep their
// drawn menu.
//
// Menus run asynchronously on the window's thread (AppKit's main thread,
// the Win32 window thread), never inside a frame: Popup returns at once
// and done is called, from another goroutine, with the picked item's
// index or -1.
package nativemenu

import (
	"image"
	"sync"

	"gioui.org/io/event"
)

// Item is one menu row.
type Item struct {
	Label    string
	Hint     string // the accelerator text drawn right-aligned (shortcut.Format), Windows
	Key      string // macOS key equivalent with Command ("x"); "" for none
	Sep      bool   // a separator (Label ignored)
	Disabled bool
}

var (
	mu     sync.Mutex
	view   uintptr      // the main window's HWND or NSView
	runner func(func()) // runs f on the window's thread (app.Window.Run), Windows
	scale  float32      = 1
)

// NoteEvent records the main window's native handle from its view event
// (app.Win32ViewEvent, app.AppKitViewEvent). The ui root passes every
// main-window event.
func NoteEvent(ev event.Event) { noteEvent(ev) }

// SetRunner gives the package the main window's Run, which the Windows
// menu needs: TrackPopupMenuEx must run on the thread that owns the
// window.
func SetRunner(f func(func())) {
	mu.Lock()
	runner = f
	mu.Unlock()
}

// Supported reports whether Popup can show a native menu now.
func Supported() bool {
	mu.Lock()
	defer mu.Unlock()
	return supported()
}

// Popup shows items at pt, in the main window's pixels (top-left
// origin). It reports false, without calling done, when there's no
// native menu; otherwise done gets the picked index or -1 when the menu
// closed without a pick.
func Popup(items []Item, pt image.Point, done func(int)) bool {
	mu.Lock()
	v, run := view, runner
	ok := supported()
	mu.Unlock()
	if !ok || len(items) == 0 {
		return false
	}
	go popup(v, run, items, pt, done)
	return true
}

// Fake replaces the platform menu (tests): it gets the items and the
// point and returns the pick.
var Fake func(items []Item, pt image.Point) int
