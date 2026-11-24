package views

import "testing"

// TestViewContexts: each page's previews read and write Slack's
// expandables under the view context Slack's page provides its list
// (MessageContainerContext; expandable's view contexts): the Activity
// page's ACTIVITY_PAGE, Threads' THREADS_VIEW, Unreads' UNREAD_VIEW and
// the To-dos page's SAVED_FOR_LATER_PAGE. The Saved page (BookmarksPage),
// the Pins tab and its peek have no provider, so their keys carry none.
func TestViewContexts(t *testing.T) {
	app, _ := vaFake(t)
	h := newKBHost(app, false)
	for _, c := range []struct {
		view, got, want string
	}{
		{"Activity", NewActivity(h).viewCtx, "activity-page"},
		{"Threads", NewThreads(h).viewCtx, "Threads"},
		{"Unreads", NewUnreads(h).viewCtx, "Punreads"},
		{"To-dos", NewLater(h).viewCtx, "saved-for-later-page"},
		{"Saved", NewSaved(h).viewCtx, ""},
		{"Pins tab", NewChannelPins(h, "C1").viewCtx, ""},
		{"Pins peek", NewChannelPinsPeek(h, "C1").viewCtx, ""},
	} {
		if c.got != c.want {
			t.Errorf("%s: view context %q, want %q", c.view, c.got, c.want)
		}
	}
}
