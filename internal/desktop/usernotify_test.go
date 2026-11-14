package desktop

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestUserNotificationEvent is NotificationCenterDelegate's mapping: the
// default action a click, the dismiss action a close; inside one of
// ShowUserNotification's categories REPLY_ACTION a reply (from a text
// input only), ACTION_<i> button i (integerValue's parse), CLOSE_ACTION a
// close; anything else nothing.
func TestUserNotificationEvent(t *testing.T) {
	for _, c := range []struct {
		category, action, text string
		input                  bool
		want                   UserNotificationEvent
		ok                     bool
	}{
		{"", unDefaultAction, "", false, UserNotificationEvent{ID: "n", Kind: "click", Index: -1}, true},
		{"CATEGORY_1", unDefaultAction, "", false, UserNotificationEvent{ID: "n", Kind: "click", Index: -1}, true},
		{"", unDismissAction, "", false, UserNotificationEvent{ID: "n", Kind: "close", Index: -1}, true},
		{"CATEGORY_1", "REPLY_ACTION", "on my way", true, UserNotificationEvent{ID: "n", Kind: "reply", Index: -1, Reply: "on my way"}, true},
		{"CATEGORY_1", "REPLY_ACTION", "", true, UserNotificationEvent{ID: "n", Kind: "reply", Index: -1}, true},
		{"CATEGORY_1", "REPLY_ACTION", "", false, UserNotificationEvent{}, false},
		{"CATEGORY_1", "ACTION_0", "", false, UserNotificationEvent{ID: "n", Kind: "action", Index: 0}, true},
		{"CATEGORY_1", "ACTION_12", "", false, UserNotificationEvent{ID: "n", Kind: "action", Index: 12}, true},
		{"CATEGORY_1", "ACTION_x", "", false, UserNotificationEvent{ID: "n", Kind: "action", Index: 0}, true},
		{"CATEGORY_1", "CLOSE_ACTION", "", false, UserNotificationEvent{ID: "n", Kind: "close", Index: -1}, true},
		{"CATEGORY_1", "OTHER", "", false, UserNotificationEvent{}, false},
		{"other", "ACTION_0", "", false, UserNotificationEvent{}, false},
		{"", "CLOSE_ACTION", "", false, UserNotificationEvent{}, false},
	} {
		got, ok := userNotificationEvent("n", c.category, c.action, c.text, c.input)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%s in %q: %+v %t, want %+v %t", c.action, c.category, got, ok, c.want, c.ok)
		}
	}
}

// TestUserNotificationJSON: the options travel to the .m side under
// Electron's names, the buttons as an array even when there are none.
func TestUserNotificationJSON(t *testing.T) {
	b, err := json.Marshal(UserNotification{ID: "i", Title: "t", Subtitle: "s", Body: "b", GroupID: "g", Actions: []string{"Join"},
		HasReply: true, ReplyPlaceholder: "p", CloseText: "Dismiss", Silent: true, Sound: "none"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"i","title":"t","subtitle":"s","body":"b","groupId":"g","actions":["Join"],"hasReply":true,"replyPlaceholder":"p","closeButtonText":"Dismiss","silent":true,"sound":"none"}`
	if string(b) != want {
		t.Errorf("got %s\nwant %s", b, want)
	}
	if !strings.Contains((&UserNotificationError{Domain: "UNErrorDomain", Code: 1, Desc: "Notifications are not allowed for this application"}).Error(), "UNErrorDomain 1") {
		t.Error("the error names its domain and code")
	}
}
