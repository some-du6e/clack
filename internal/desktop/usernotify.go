package desktop

import (
	"fmt"
	"strings"
)

// macOS notifications through UNUserNotificationCenter, the way Electron
// shows Slack desktop's (shell/browser/notifications/mac:
// cocoa_notification.mm, notification_center_delegate.mm and
// notification_presenter_mac.mm; Slack's side is ui/notify_mac.go):
//
//   - the title, subtitle and body as given; the group id is the thread
//     identifier Notification Center groups by;
//   - silent: no sound; otherwise the default sound, or the sound file
//     named;
//   - a reply box, buttons or a close button make a category keyed by its
//     action string ("CATEGORY_" and the string's NSString hash):
//     REPLY_ACTION, a text input titled "Reply" with the reply placeholder;
//     ACTION_<i> for each button ("Show" for one without text); CLOSE_ACTION
//     with the close button's text; and the custom dismiss action, so a
//     dismissal is reported too;
//   - the center's delegate shows banners while Clack is frontmost (list,
//     banner and sound) and reports a click (the default action), a reply,
//     a button and a close (the close button or a dismissal);
//   - authorization is asked for alerts, sounds and badges once, before
//     the first notification (NotificationPresenterMac's constructor).
//
// ci.yml's macos job (internal/desktop/usernotifyprobe) showed that an
// ad-hoc signed Clack.app gets all of this on macOS 26: macOS puts up its
// permission prompt, provisional authorization is granted at once,
// notifications are delivered, and a rebuilt binary (a new cdhash, as each
// update has) keeps the answer. A bundle whose signature doesn't cover it
// (the Go linker's signature alone, Info.plist not bound) gets
// UNErrorDomain 1 (UNErrorCodeNotificationsNotAllowed), and a binary
// outside an app bundle can't use the center at all.

// UserNotification is one notification: the Electron Notification options
// CocoaNotification::Show reads.
type UserNotification struct {
	ID               string   `json:"id"` // the request's identifier ("": a new UUID)
	Title            string   `json:"title"`
	Subtitle         string   `json:"subtitle"`
	Body             string   `json:"body"`
	GroupID          string   `json:"groupId"` // the thread identifier ("": none)
	Actions          []string `json:"actions"` // the buttons' texts, ACTION_0…
	HasReply         bool     `json:"hasReply"`
	ReplyPlaceholder string   `json:"replyPlaceholder"`
	CloseText        string   `json:"closeButtonText"` // CLOSE_ACTION's text ("": none)
	Silent           bool     `json:"silent"`
	Sound            string   `json:"sound"` // "" the default sound
}

// UserNotificationEvent is what the user did with a notification.
type UserNotificationEvent struct {
	ID    string // the notification's identifier
	Kind  string // click, reply, action or close
	Index int    // action: the button (ACTION_<i>); -1 otherwise
	Reply string // reply: the text typed
}

// UserNotificationAuth is the notification settings' authorizationStatus.
type UserNotificationAuth int

const (
	AuthNotDetermined UserNotificationAuth = iota
	AuthDenied
	AuthAuthorized
	AuthProvisional
)

func (s UserNotificationAuth) String() string {
	switch s {
	case AuthNotDetermined:
		return "not determined"
	case AuthDenied:
		return "denied"
	case AuthAuthorized:
		return "authorized"
	case AuthProvisional:
		return "provisional"
	}
	return fmt.Sprintf("status %d", int(s))
}

// UserNotificationError is an NSError from the center.
type UserNotificationError struct {
	Domain string
	Code   int
	Desc   string
}

func (e *UserNotificationError) Error() string {
	return fmt.Sprintf("%s %d: %s", e.Domain, e.Code, e.Desc)
}

// errNoUserNotifications: this build or process has no Notification Center.
var errNoUserNotifications = &UserNotificationError{Domain: "Clack", Code: -1, Desc: "UNUserNotificationCenter needs Clack.app on macOS"}

// The response's action identifiers for a click and a dismissal
// (UNNotificationDefaultActionIdentifier, UNNotificationDismissActionIdentifier).
const (
	unDefaultAction = "com.apple.UNNotificationDefaultActionIdentifier"
	unDismissAction = "com.apple.UNNotificationDismissActionIdentifier"
)

// userNotificationEvent is NotificationCenterDelegate's
// didReceiveNotificationResponse: the default action is a click, the
// dismiss action a close, and in one of the categories ShowUserNotification
// makes REPLY_ACTION is the reply (from a text input), ACTION_<i> button i
// and CLOSE_ACTION a close. Anything else reports nothing.
func userNotificationEvent(id, category, action, text string, textInput bool) (UserNotificationEvent, bool) {
	ev := UserNotificationEvent{ID: id, Index: -1}
	switch {
	case action == unDefaultAction:
		ev.Kind = "click"
	case action == unDismissAction:
		ev.Kind = "close"
	case !strings.HasPrefix(category, "CATEGORY_"):
		return ev, false
	case action == "REPLY_ACTION":
		if !textInput {
			return ev, false
		}
		ev.Kind, ev.Reply = "reply", text
	case strings.HasPrefix(action, "ACTION_"):
		ev.Kind, ev.Index = "action", nsIntegerValue(strings.TrimPrefix(action, "ACTION_"))
	case action == "CLOSE_ACTION":
		ev.Kind = "close"
	default:
		return ev, false
	}
	return ev, true
}

// nsIntegerValue is NSString's integerValue: leading whitespace, an
// optional sign, then decimal digits; 0 without any.
func nsIntegerValue(s string) int {
	s = strings.TrimLeft(s, " \t\n\r\v\f")
	neg := false
	if s != "" && (s[0] == '-' || s[0] == '+') {
		neg, s = s[0] == '-', s[1:]
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' || n > (1<<31)/10 {
			break
		}
		n = n*10 + int(r-'0')
	}
	if neg {
		return -n
	}
	return n
}
