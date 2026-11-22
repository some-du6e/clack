package richtext

import "testing"

// notificationTextOf is a file-less message's notification text:
// NotificationMessageText, else NotificationAttachmentText (red branches
// stub it with the drawn Doc's plain text, the old notification body).
func notificationTextOf(t *testing.T, msgJSON string) string {
	t.Helper()
	m := msg(t, msgJSON)
	if s := NotificationMessageText(R, m, nil); s != "" {
		return s
	}
	return NotificationAttachmentText(R, m, nil)
}

// searchTextOf is SearchText (red branches stub it with PlainText).
func searchTextOf(d Doc) string { return SearchText(d) }
