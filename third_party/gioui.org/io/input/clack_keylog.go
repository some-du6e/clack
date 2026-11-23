// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"slices"

	"gioui.org/io/event"
	"gioui.org/io/key"
)

// Clack patch 28 (CLACK_PATCHES.md): the key presses a window's router
// was given, whoever took them. Gio hands a key event to one Event call,
// the first whose filters match, so code that watches for a sequence of
// keys other widgets act on either takes the keys from them or never sees
// them. Clack's Konami code (↑ ↑ ↓ ↓ ← → ← → B A turns its inspect tool
// on) is typed while the composer has the focus and takes the arrows and
// the letters.

// keyLogMax bounds the presses a router keeps between reads.
const keyLogMax = 32

type keyLog struct {
	presses []key.Event
	read    bool // a Source called KeyPresses since the last frame
	on      bool // one did in the frame before: log
	replay  bool // the router queues events again (state replay)
	wake    bool // a press was kept since KeyLogged last answered
}

// KeyPresses returns the key presses (key.Events with State key.Press,
// but not the modifier keys' own: key.NameShift, …) the router was given
// since the last call or the last frame, oldest first, the last 32 at
// most, and forgets them. It doesn't matter which widget took them, or
// whether any did: the other readers get them as before. The router
// keeps the presses only while a Source calls KeyPresses every frame (so
// from the frame after the first call), and the window draws a frame for
// each one it keeps (KeyLogged). A disabled or HoverOnly Source returns
// none.
func (s Source) KeyPresses() []key.Event {
	if !s.Enabled() || s.hoverOnly {
		return nil
	}
	l := &s.r.keyLog
	l.read = true
	if len(l.presses) == 0 {
		return nil
	}
	p := slices.Clone(l.presses)
	clear(l.presses)
	l.presses = l.presses[:0]
	return p
}

// noteKey keeps e for KeyPresses if it is a press, a Source reads them
// and the router hasn't seen e before.
func (q *Router) noteKey(e event.Event) {
	l := &q.keyLog
	ke, ok := e.(key.Event)
	if !ok || ke.State != key.Press || !l.on || l.replay {
		return
	}
	switch ke.Name {
	case key.NameShift, key.NameCtrl, key.NameAlt, key.NameSuper, key.NameCommand:
		return
	}
	if len(l.presses) == keyLogMax {
		copy(l.presses, l.presses[1:])
		l.presses = l.presses[:keyLogMax-1]
	}
	l.presses = append(l.presses, ke)
	l.wake = true
}

// KeyLogged reports whether the router kept a key press for KeyPresses
// since the last call or the last frame. The window asks after every
// event and draws a frame for it even when no filter matched the key,
// which still counts as not handled (WakeupTime): Gio still moves the
// focus for an unhandled Tab, and macOS still gets an unhandled key's
// command (doCommandBySelector:).
func (q *Router) KeyLogged() bool {
	w := q.keyLog.wake
	q.keyLog.wake = false
	return w
}

// frameKeyLog starts a frame: the presses no Source read go, and the
// router logs while one read them this frame.
func (q *Router) frameKeyLog() {
	l := &q.keyLog
	clear(l.presses)
	l.presses = l.presses[:0]
	l.on, l.read = l.read, false
	l.wake = false
}

// requeue queues events the router was already given (a state replay):
// KeyPresses has them already.
func (q *Router) requeue(events ...event.Event) {
	q.replayed += len(events) // Clack patch 61 (tests)
	q.keyLog.replay = true
	defer func() { q.keyLog.replay = false }()
	q.Queue(events...)
}
