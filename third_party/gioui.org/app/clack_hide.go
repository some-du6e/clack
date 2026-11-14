// SPDX-License-Identifier: Unlicense OR MIT

package app

// Clack patch (CLACK_PATCHES.md §13): hiding a window, for Slack's
// runFromTray. Slack's main process answers a click on the close button
// with preventDefault and BrowserWindow.hide (MAIN_WINDOW_CLOSE), so the
// app keeps running in the notification area; the tray icon brings the
// window back with show. Gio can already abort the close
// (ClosingEvent.Abort); Hide is the hide.

// hider is a driver that can hide its window.
type hider interface {
	hide() bool
}

// Hide hides the window without destroying it, on the window's thread,
// and reports whether it did: Windows (Chromium's
// HWNDMessageHandler::Hide) and macOS (Electron's NativeWindowMac::Hide,
// orderOut:; a full-screen window leaves full screen first, CLACK_PATCHES.md
// §23); elsewhere it does nothing and returns false. The window keeps
// delivering events but draws no frames while hidden;
// Perform(system.ActionRaise) shows it again (on macOS so does any other
// order-front). It may be called while handling a ClosingEvent, before
// calling Abort or after.
func (w *Window) Hide() bool {
	if w.driver == nil {
		return false
	}
	ok := false
	w.Run(func() {
		if h, is := w.driver.(hider); is {
			ok = h.hide()
		}
	})
	return ok
}
