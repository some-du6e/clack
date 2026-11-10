// Package globalkey registers Slack's OS-wide mute shortcut, Mod+Shift+
// Space (Cmd on macOS, Ctrl elsewhere), for Preferences > Audio & video
// "When Slack is in the background: Allow keyboard shortcut to mute"
// (huddles_global_mute).
//
// Slack's desktop main process maps the toggle_mute feature to Electron's
// globalShortcut "CommandOrControl+Shift+Space" (main.bundle ua); the
// renderer turns it on while a huddle is STARTED and the pref is on
// (maybeToggleGlobalMuteShortcut, globalMuteShortcut.enable/disable) and a
// press runs toggleSelfMicrophone. The per-OS registrations here are what
// Electron's globalShortcut uses:
//
//   - macOS: Carbon RegisterEventHotKey (no accessibility permission).
//   - Windows: RegisterHotKey with MOD_NOREPEAT on a thread of its own.
//   - Linux: XGrabKey on the X root window, also with Caps Lock and Num
//     Lock held. Wayland has no API for an app to grab a key outside its
//     own windows (Electron's globalShortcut doesn't work there either):
//     under a Wayland session Register returns ErrWayland.
package globalkey

import "errors"

var (
	// ErrUnsupported: no global shortcuts on this platform or build.
	ErrUnsupported = errors.New("globalkey: global shortcuts not supported here")
	// ErrWayland: the session is Wayland, which has no global key grabs.
	ErrWayland = errors.New("globalkey: Wayland doesn't let apps grab keys outside their windows")
	// ErrTaken: another app holds the shortcut.
	ErrTaken = errors.New("globalkey: another app holds the shortcut")
)

// Shortcut is the shortcut's label as Slack shows it.
const Shortcut = "Mod+Shift+Space"

// Register grabs Mod+Shift+Space for the whole desktop; onPress runs on a
// goroutine of its own (post to the UI goroutine) for each press. stop
// releases it.
func Register(onPress func()) (stop func(), err error) { return register(onPress) }
