package ui

import (
	"github.com/some-du6e/yoloslackgoclient/internal/ui/huddleui"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/uipref"
)

// notifier posts desktop notifications (D-Bus on Linux, no-op elsewhere).
type notifier interface {
	Notify(title, body string) error
	// NotifyActions is Notify with buttons: actions is key/label pairs;
	// on gets the key of the one clicked.
	NotifyActions(title, body string, actions []string, on func(key string)) error
}

type nopNotifier struct{}

func (nopNotifier) Notify(string, string) error { return nil }
func (nopNotifier) NotifyActions(string, string, []string, func(string)) error {
	return nil
}

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

// NotifyActions shows a huddle notification with Slack's buttons (the
// incoming invite's Join / Decline, the missed one's View), if enabled.
// With message previews off it says only PlainBody, like Slack's
// shouldHideNotificationText. Any goroutine.
func (a *App) NotifyActions(n huddleui.Notification) {
	a.Post(func() { a.notifyActions(n) })
}

func (a *App) notifyActions(n huddleui.Notification) {
	if !a.notifyOn() {
		return
	}
	body := n.Body
	if uipref.Get().NoTextInNotifications && n.PlainBody != "" {
		body = n.PlainBody
	}
	if a.notify == nil {
		a.notify = newNotifier()
	}
	var keys []string
	for _, ac := range n.Actions {
		keys = append(keys, ac.Key, ac.Label)
	}
	acts := n.Actions
	go func() {
		_ = a.notify.NotifyActions(n.Title, body, keys, func(key string) {
			for _, ac := range acts {
				if ac.Key == key && ac.Do != nil {
					ac.Do()
				}
			}
		})
	}()
}
