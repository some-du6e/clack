package ui

import "github.com/some-du6e/yoloslackgoclient/internal/ui/uipref"

// notifier posts desktop notifications (D-Bus on Linux, no-op elsewhere).
type notifier interface {
	Notify(title, body string) error
}

type nopNotifier struct{}

func (nopNotifier) Notify(string, string) error { return nil }

// Notify shows a desktop notification if enabled.
func (a *App) Notify(title, body string) {
	if !a.notifyOn() || uipref.Get().NoDesktopPush { // Notifications > Desktop notifications
		return
	}
	if uipref.Get().NoTextInNotifications { // Notifications: no message preview
		body = ""
	}
	if a.notify == nil {
		a.notify = newNotifier()
	}
	go func() { _ = a.notify.Notify(title, body) }()
}
