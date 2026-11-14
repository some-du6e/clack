// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"image"

	"gioui.org/unit"
)

// Clack patch (CLACK_PATCHES.md §25): a window's place on the screen and
// the displays it can go on, for Slack's mainWindowSettings, which reopens
// the main window where it was, on a display that is still connected.
// Electron's BrowserWindow takes x, y, width and height and reports
// getBounds(); its screen module lists the displays (getAllDisplays,
// getPrimaryDisplay) and finds the one a window is on
// (getDisplayMatching).

// Bounds places the window. r is its frame, the OS frame included, in the
// screen coordinates Display and Config.Bounds use too:
//
//   - Windows: physical pixels of the virtual screen, the primary
//     monitor's top left at (0, 0); the frame is GetWindowRect's, the
//     invisible resize borders included. Given with the window's first
//     options, the window is created there, so it starts with the DPI of
//     the monitor it opens on (a move there afterwards would rescale it
//     through WM_DPICHANGED).
//   - macOS: points from the top left of the primary screen (the one with
//     the menu bar), y down, as Electron flips an NSWindow's frame. A size
//     under the window's minimum is raised to it, as Electron's setBounds
//     does: AppKit's setFrame:display: doesn't check it.
//
// Like Size, it sets the mode to Windowed. A Maximized or Fullscreen
// option after it maximizes or fills the display the window was placed
// on, and a maximized window restores to r. Given later, it moves and
// sizes a windowed window; on Windows a maximized or minimized window is
// restored first, on macOS a full-screen window ignores it. Windows and
// macOS implement it; elsewhere the window opens where the OS puts it.
func Bounds(r image.Rectangle) Option {
	return func(_ unit.Metric, cnf *Config) {
		cnf.Mode = Windowed
		cnf.bounds = r
		cnf.boundsSet = !r.Empty()
	}
}

// Bounds is the window's frame on the screen, in the Bounds option's
// coordinates (Clack patch, CLACK_PATCHES.md §25): in a ConfigEvent, where
// the window is, maximized and full screen included. It is empty while
// the window isn't on the screen (minimized, its place being the
// taskbar's or the Dock's; hidden with Window.Hide or StartHidden) and
// where the platform doesn't say (other than Windows and macOS). Options
// applied to a Config set it to the Bounds option's rectangle.
func (c Config) Bounds() image.Rectangle { return c.bounds }

// Display is the display the window is on (Clack patch, CLACK_PATCHES.md
// §25): the one with the biggest part of it, Electron's
// screen.getDisplayMatching(window.getBounds()). It is the zero Display
// whenever Bounds is empty.
func (c Config) Display() Display { return c.display }

// Display is a connected display (Clack patch, CLACK_PATCHES.md §25), as
// Electron's screen module describes one.
type Display struct {
	// Bounds is the whole display and WorkArea the part windows maximize
	// to: without the taskbar and the other app bars on Windows, without
	// the menu bar and the Dock on macOS. Both are in the Bounds option's
	// screen coordinates.
	Bounds, WorkArea image.Rectangle
	// Scale is the display's screen units per DIP, the unit Electron's
	// bounds and sizes are in: its effective DPI over 96 on Windows, 1 on
	// macOS (whose screen coordinates are points).
	Scale float32
	// Primary is Windows' primary monitor, whose top left is the screen's
	// origin, or macOS's screen with the menu bar.
	Primary bool
}

// Displays lists the connected displays (Clack patch, CLACK_PATCHES.md
// §25): EnumDisplayMonitors' monitors on Windows, NSScreen's screens on
// macOS (the primary first). It is nil where Gio can't say (Linux, the
// mobile platforms, the browser), where windows can't be placed either.
// Any goroutine, but not while the window's event handling holds AppKit's
// main thread (macOS: a frame or another window event being handled; the
// list is read there); Config.Display is the display a window is on.
func Displays() []Display { return displays() }
