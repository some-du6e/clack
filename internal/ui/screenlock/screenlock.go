// Package screenlock reports the screen locking and unlocking, for
// Preferences > Audio & video "When your screen saver starts or computer
// locks… Automatically leave huddles" (huddles_disconnect_on_lock).
//
// Slack's desktop app keeps the lock state in its main process
// (app.systemLockedStatus, SET_SYSTEM_LOCKED), and the renderer's
// HuddleLocker (gantry boot-render) leaves the huddle with terminal state
// "DeviceLocked" when it turns from false to true while the pref is on.
// The per-OS sources here:
//
//   - macOS: the com.apple.screenIsLocked / com.apple.screenIsUnlocked
//     distributed notifications (the lock screen, and the screen saver
//     when it asks for a password).
//   - Windows: WM_WTSSESSION_CHANGE (WTS_SESSION_LOCK / UNLOCK) to a
//     message-only window registered with WTSRegisterSessionNotification.
//   - Linux: systemd-logind's Lock and Unlock signals and LockedHint
//     property on this process's session (system bus), and the
//     ActiveChanged signal of org.freedesktop.ScreenSaver and
//     org.gnome.ScreenSaver (session bus: the screen saver starting).
//
// Slack's own Linux build defines SET_SYSTEM_LOCKED but never dispatches
// it (its main.bundle has no lock source), so there the pref does
// nothing; Clack's Linux watcher makes it do what it says.
package screenlock

import (
	"errors"
	"sync"
)

// ErrUnsupported: no lock source on this platform or build.
var ErrUnsupported = errors.New("screenlock: not supported here")

// Watch calls f(true) when the screen locks (or the screen saver starts)
// and f(false) when it unlocks, one call at a time on a goroutine of its
// own, until stop. Repeated states are reported once.
func Watch(f func(locked bool)) (stop func(), err error) {
	ch := make(chan bool, 16)
	done := make(chan struct{})
	go func() {
		var last *bool
		for {
			select {
			case <-done:
				return
			case l := <-ch:
				if last != nil && *last == l {
					continue
				}
				last = &l
				f(l)
			}
		}
	}()
	send := func(l bool) { // any goroutine (Linux has more than one source)
		select {
		case ch <- l:
		case <-done:
		}
	}
	inner, err := watchFn(send)
	if err != nil {
		close(done)
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			inner()
			close(done)
		})
	}, nil
}

// watchFn is the platform's watch (tests swap it).
var watchFn = watch
