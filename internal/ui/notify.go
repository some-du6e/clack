package ui

import (
	"context"
	"time"

	"github.com/some-du6e/clack/internal/ui/huddleui"
	"github.com/some-du6e/clack/internal/ui/uipref"
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
func (a *App) Notify(title, body string) { a.notifyShow(title, body) }

// notifyMessage is a new message's desktop notification with its sound:
// Slack's desktopNotificationOptions hands the shell desktop_sound (a
// global notification pref) and the shell plays it with the banner,
// unless mute_sounds (canPlayMessageSound), Do Not Disturb (canPlaySound)
// or the value is "none".
func (a *App) notifyMessage(title, body string) {
	if !a.notifyShow(title, body) {
		return
	}
	c := a.Core()
	if c == nil || c.PrefBool("mute_sounds", false) {
		return
	}
	file := messageSound(c.GlobalNotifyString("desktop_sound"))
	go func() { // dnd.info the first time: off the UI goroutine
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if !c.DNDActive(ctx) {
			huddleui.PlaySlackSound(file)
		}
	}()
}

// messageSound is the desktop_sound file to play ("" unset: Slack's
// default, Knock Brush).
func messageSound(v string) string {
	if v == "" {
		return "knock_brush.mp3"
	}
	return v
}

func (a *App) notifyShow(title, body string) bool {
	if !a.notifyOn() || uipref.Get().NoDesktopPush { // Notifications > Desktop notifications
		return false
	}
	if uipref.Get().NoTextInNotifications { // Notifications: no message preview
		body = ""
	}
	if a.notify == nil {
		a.notify = newNotifier()
	}
	go func() { _ = a.notify.Notify(title, body) }()
	a.bounceDock() // mac_ssb_bounce
	return true
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
