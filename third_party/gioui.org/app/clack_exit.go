// SPDX-License-Identifier: Unlicense OR MIT

package app

// TerminateEvent is a Clack patch (CLACK_PATCHES.md §58): the OS asked
// the program to quit. macOS sends it for AppKit's terminate: (the Dock's
// Quit, log out, restart and shut down, a quit Apple Event such as
// osascript's): a program ranging over Events gets it while AppKit waits
// (NSTerminateLater) and quits as its own Quit does, the process ending
// when the program exits. A program that doesn't range over Events is
// terminated at once, as before. Other systems don't send it.
type TerminateEvent struct{}

func (TerminateEvent) ImplementsEvent() {}

// X11ConnectionLost is a Clack patch (CLACK_PATCHES.md §59): when set,
// it's called when the connection to the X server breaks (Xlib's IO
// error handler: the X session ended, the server stopped or was killed),
// before the process exits with status 1 as Xlib's default handler does.
// Xlib's handler must not return, so f should only record why and flush,
// briefly, on whatever goroutine Xlib's caller is.
var X11ConnectionLost func()
