package ui

import "github.com/some-du6e/clack/internal/desktop"

// newNotifier is macOS's Notification Center (notify_mac.go) for a
// Clack.app. A binary run outside one (go run, ci's clack-darwin-arm64)
// can't use UNUserNotificationCenter, which aborts the process there, so it
// shows none.
func newNotifier() notifier {
	if !desktop.UserNotificationsAvailable() {
		return nopNotifier{}
	}
	return newMacNotifier(desktopUserNotifications{})
}
