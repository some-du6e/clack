package desktop

// Windows' own reasons a toast it took shows no banner, or doesn't show at
// all (notifystate_windows.go; ui/notify_windows.go logs them next to the
// notification's decision line). Off Windows the state is unknown and the
// switches aren't there.

// The shell's notification states: SHQueryUserNotificationState's
// QUERY_USER_NOTIFICATION_STATE (shellapi.h), 0 when it can't say.
const (
	NotifyStateUnknown       = 0
	NotifyStateNotPresent    = 1 // QUNS_NOT_PRESENT: a screen saver, the screen locked, another user switched in
	NotifyStateBusy          = 2 // QUNS_BUSY: an app full screen (Focus Assist's full-screen rule)
	NotifyStateD3DFullScreen = 3 // QUNS_RUNNING_D3D_FULL_SCREEN: a Direct3D app (a game) full screen
	NotifyStatePresentation  = 4 // QUNS_PRESENTATION_MODE: presentation settings on
	NotifyStateAccepts       = 5 // QUNS_ACCEPTS_NOTIFICATIONS: banners show
	NotifyStateQuietTime     = 6 // QUNS_QUIET_TIME: the first hour after a first sign-in or an upgrade
	NotifyStateApp           = 7 // QUNS_APP: a Store app full screen
)

// NotifyStateWhy says why the shell holds banners in state s, "" when it
// doesn't (or can't say).
func NotifyStateWhy(s int) string {
	switch s {
	case NotifyStateNotPresent:
		return "the screen is locked, or a screen saver or another user has it"
	case NotifyStateBusy:
		return "an app is full screen"
	case NotifyStateD3DFullScreen:
		return "a game or another Direct3D app is full screen"
	case NotifyStatePresentation:
		return "presentation mode is on"
	case NotifyStateQuietTime:
		return "it's the quiet hour after a first sign-in or an upgrade"
	case NotifyStateApp:
		return "a Store app is full screen"
	}
	return ""
}

// The switches' readings (ToastSwitches): Windows' default when the value
// isn't there is on.
const (
	ToastSwitchUnset = -1
	ToastSwitchOff   = 0
	ToastSwitchOn    = 1
)
