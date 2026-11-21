package richtext

import "testing"

// notificationTextOf is NotificationText for a message (red branches
// stub it with the drawn Doc's plain text, the old notification body).
func notificationTextOf(t *testing.T, msgJSON string) string {
	t.Helper()
	m := msg(t, msgJSON)
	return NotificationText(R, m, FromMessage(R, m), nil)
}

// searchTextOf is SearchText (red branches stub it with PlainText).
func searchTextOf(d Doc) string { return SearchText(d) }
