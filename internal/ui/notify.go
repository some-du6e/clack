package ui

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/huddleui"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// notifier posts desktop notifications (D-Bus on Linux, WinRT toasts on
// Windows, Notification Center on macOS, no-op elsewhere).
type notifier interface {
	Notify(title, body string) error
	// NotifyActions is Notify with buttons: actions is key/label pairs;
	// on gets the key of the one clicked.
	NotifyActions(title, body string, actions []string, on func(key string)) error
}

// noteNotifier is a notifier that shows the whole notification (the
// Windows toast: its workspace line, its buttons, where a click goes).
type noteNotifier interface {
	NotifyNote(n *desktopNote) error
}

type nopNotifier struct{}

func (nopNotifier) Notify(string, string) error { return nil }
func (nopNotifier) NotifyActions(string, string, []string, func(string)) error {
	return nil
}

// nsGrowl is the namespace of Slack's notification strings (gaJC's eA).
var nsGrowl = i18n.NS("growl")

// Notify shows a desktop notification if enabled.
func (a *App) Notify(title, body string) { a.notifyShow(title, body, nil) }

// notifyMessage is a new message's desktop notification with its sound:
// Slack's desktopNotificationOptions hands the shell desktop_sound (a
// global notification pref) and the shell plays it with the banner,
// unless mute_sounds (canPlayMessageSound), Do Not Disturb (canPlaySound)
// or the value is "none". n is the payload for a notifier that shows it
// (messageNote, made on the UI goroutine before the showNotification
// thunk ran).
func (a *App) notifyMessage(title, body string, n *desktopNote) {
	if !a.notifyShow(title, body, n) {
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

func (a *App) notifyShow(title, body string, n *desktopNote) bool {
	if !a.notifyOn() || uipref.Get().NoDesktopPush { // Notifications > Desktop notifications
		return false
	}
	if uipref.Get().NoTextInNotifications { // Notifications: no message preview
		body = ""
	}
	if a.notify == nil {
		a.notify = newNotifier()
	}
	if nn, ok := a.notify.(noteNotifier); ok {
		if n == nil {
			n = a.plainNote()
		}
		n.Title, n.Body = title, body
		go func() { _ = nn.NotifyNote(n) }()
	} else {
		go func() { _ = a.notify.Notify(title, body) }()
	}
	a.bounceDock()        // mac_ssb_bounce
	a.notificationShown() // Windows: the frame flash (windowFlashBehavior)
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
	a.bounceDock() // showHuddleNotification and showHuddleMissedNotification: maybeBounceDockIcon
	body := n.Body
	if uipref.Get().NoTextInNotifications && n.PlainBody != "" {
		body = n.PlainBody
	}
	if a.notify == nil {
		a.notify = newNotifier()
	}
	if nn, ok := a.notify.(noteNotifier); ok && n.Channel != "" {
		dn := a.huddleNote(n)
		dn.Body = body
		go func() { _ = nn.NotifyNote(dn) }()
		a.notificationShown()
		return
	}
	var keys []string
	for _, ac := range n.Actions {
		keys = append(keys, ac.Key, ac.Label)
	}
	acts := n.Actions
	a.notificationShown()
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

// ---- the payloads (UI goroutine) ----

// noteTeam is the signed-in team's id and name (teamId, and the
// shell's win32.workspaceName).
func (a *App) noteTeam() (id, name string) {
	c := a.Core()
	if c == nil {
		return "", ""
	}
	c.S.RLock()
	defer c.S.RUnlock()
	return c.S.TeamID, c.S.TeamName
}

// noteActionsOn is desktop_notification_actions (awVE's Ky): Slack's
// notification ids, groups and translated buttons.
func (a *App) noteActionsOn() bool {
	c := a.Core()
	return c != nil && c.Experiment("desktop_notification_actions") == "on"
}

// plainNote is a notification with nothing to open (App.Notify).
func (a *App) plainNote() *desktopNote {
	team, ws := a.noteTeam()
	n := &desktopNote{Team: team, Workspace: ws}
	n.ID = shellNoteID(team, "")
	n.Activated = func(act toastActivation) { a.Post(func() { a.toastActivated(n, act) }) }
	return n
}

// messageNote is desktopNotificationOptions for a message in ch (ts, in
// the thread at thread when it's a reply).
func (a *App) messageNote(ch, ts, thread string) *desktopNote {
	team, ws := a.noteTeam()
	// silent: the webapp plays the sound (notifyMessage); hasReply: the
	// message wasn't one of yours that failed to send
	n := &desktopNote{Team: team, Workspace: ws, Channel: ch, Msg: ts, ThreadTS: thread, HasReply: true, Silent: true}
	if a.noteActionsOn() {
		n.ID = buildNotificationID(team, noteTypeMessage, messageNotificationPayload(ch, ts, thread)...)
		n.GroupID = buildNotificationID(team, noteTypeMessage, ch)
	} else {
		n.ID = shellNoteID(team, ts)
	}
	n.Activated = func(act toastActivation) { a.Post(func() { a.toastActivated(n, act) }) }
	return n
}

// huddleNote is desktopHuddleNotificationOptions (with
// sharedHuddleNotificationOptions' text, which huddleui wrote) for an
// invite or a missed one.
func (a *App) huddleNote(h huddleui.Notification) *desktopNote {
	team, ws := a.noteTeam()
	now := time.Now()
	// no reply box; silent: both come from an invite (notifIsFromInvite),
	// which rings in its own window
	n := &desktopNote{Team: team, Workspace: ws, Channel: h.Channel, Subtitle: h.Subtitle, Msg: huddleNotificationTag(h.Channel, now), Silent: true}
	typ := noteTypeHuddle
	if h.Missed {
		// showHuddleMissedNotification's title; an invite has none
		// (sharedHuddleNotificationOptions; notifyBrowser passes title "")
		typ, n.Title = noteTypeHuddleMissed, h.Title
	}
	if a.noteActionsOn() {
		call := h.Call
		if call == "" {
			call = "t" + strconv.FormatInt(now.UnixMilli(), 10) // notificationDedupKey
		}
		n.ID = buildNotificationID(team, typ, h.Channel, call)
	} else {
		n.ID = shellNoteID(team, n.Msg)
	}
	for _, b := range h.Buttons {
		n.Buttons = append(n.Buttons, noteButton{Content: b.Label, LaunchURI: b.URI, Do: b.Do})
	}
	n.Activated = func(act toastActivation) { a.Post(func() { a.toastActivated(n, act) }) }
	return n
}

// ---- clicks (UI goroutine) ----

// toastActivated is handleToastActivation for one of Clack's toasts, and
// the macOS shell's routing of a notification's events (notify_mac.go):
// the notification itself (notificationClicked), its reply box
// (replyToNotification), a button (its launch URI as a deep link, else the
// webapp's handleNotificationAction; Clack runs the button's own action).
// A click and a link bring the window to the front, as notifyBrowser's
// onclick (window.focus) and the shell's deep-link handler do; a reply and
// a button without a link leave it where it is.
func (a *App) toastActivated(n *desktopNote, act toastActivation) {
	raise := func() {
		if a.winLive.Load() != nil { // a click only comes while the window is up
			a.Raise()
		}
	}
	switch act.Type {
	case "notification":
		raise()
		a.notificationClicked(act.Channel, act.MessageID, act.ThreadTS, act.NotificationID)
	case "reply":
		a.replyFromNotification(act)
	case "action":
		if isSlackURI(act.LaunchURI) { // every Windows button that activates Clack has one
			raise()
		}
		if act.Index >= 0 && act.Index < len(n.Buttons) && n.Buttons[act.Index].Do != nil {
			n.Buttons[act.Index].Do()
			return
		}
		if isSlackURI(act.LaunchURI) {
			a.OpenURL(act.LaunchURI)
		}
	}
}

// notificationClicked is the webapp's notificationClicked (boot-shared
// rFJr) for what Clack notifies about: a huddle's notification opens its
// conversation (the msg starts "huddle", or the id says so); a message's
// opens its conversation, in the thread for a reply.
func (a *App) notificationClicked(ch, msg, thread, id string) {
	if _, typ, payload, truncated := parseNotificationID(id); ch == "" && !truncated && typ == noteTypeMessage && len(payload) > 0 {
		// parseMessageNotificationRoute: the id names the conversation
		ch = payload[0]
		if len(payload) > 2 && thread == "" {
			thread = payload[2]
		}
	}
	if ch == "" {
		return
	}
	if strings.HasPrefix(msg, "huddle") {
		a.Navigate("c/" + ch)
		return
	}
	route := "c/" + ch
	if thread != "" && slackTSRE.MatchString(thread) {
		route += "/t/" + thread
	}
	a.Navigate(route)
}

// replyFromNotification is delegate_replyToNotification (boot-render): the
// text typed into a notification's reply box goes to its conversation (the
// one its id names when the payload doesn't), in the thread when the
// notification was for a reply, through the composer's send. The window
// stays where it is.
func (a *App) replyFromNotification(act toastActivation) {
	ch, thread := act.Channel, act.ThreadTS
	if ch == "" && act.NotificationID != "" {
		if _, typ, payload, truncated := parseNotificationID(act.NotificationID); !truncated && typ == noteTypeMessage && len(payload) > 0 {
			ch = payload[0]
			if thread == "" && len(payload) > 2 {
				thread = payload[2]
			}
		}
	}
	c := a.Core()
	if c == nil || ch == "" {
		return
	}
	text := act.Reply
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := composer.SendText(ctx, c, ch, thread, text); err != nil {
			log.Printf("notify: the reply typed into a notification wasn't sent: %v", err)
		}
	}()
}
