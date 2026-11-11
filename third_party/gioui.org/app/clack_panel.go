// SPDX-License-Identifier: Unlicense OR MIT

package app

import "gioui.org/unit"

// Clack patch (CLACK_PATCHES.md §8): panel windows, for Slack's huddle
// mini panel. Slack opens that window hidden as an Electron BrowserWindow
// with type "panel" (macOS) and acceptFirstMouse, then shows it with
// showInactive. Panel does what Electron does for those:
//
//   - The window first shows without taking the focus from the window
//     that has it: orderFrontRegardless on macOS, SW_SHOWNOACTIVATE and
//     SWP_NOACTIVATE on Windows, and on X11 _NET_WM_USER_TIME 0 before the
//     map, as Chromium's X11Window::Map(inactive) does. A Wayland
//     compositor decides for itself, as it does for Chromium.
//   - On macOS the window is Electron's ElectronNSPanel: an NSWindow whose
//     style mask always has NSWindowStyleMaskNonactivatingPanel, so a
//     click on it doesn't activate the app, and whose collection
//     behaviour always has CanJoinAllSpaces and FullScreenAuxiliary, at
//     NSFloatingWindowLevel. Its view takes the first mouse down
//     (acceptsFirstMouse), so one click presses a button in it while
//     another app is active.
//
// Panel only takes effect when the window is created.
func Panel(enabled bool) Option {
	return func(_ unit.Metric, cnf *Config) {
		cnf.panel = enabled
	}
}
