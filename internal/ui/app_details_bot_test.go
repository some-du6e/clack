package ui

// App details for another bot (review of thread-header-hover-3): with
// App details open for a user-less bot, a click on another user-less bot
// opens that one's; with a bot user's open, its member's message posted
// with another bot opens that bot's. OpenAppDetails returned early when
// the open pane's user matched (both "" for user-less bots).

import (
	"testing"
)

func TestAppDetailsOtherBotOpensItsOwn(t *testing.T) {
	a := New(Options{ConfigDir: t.TempDir(), NoRealtime: true}, nil)
	for _, c := range []struct {
		name   string
		user   string
		b1, b2 string
	}{
		{"user-less bots", "", "B0FIRST001", "B0SECOND01"},
		{"a bot user's own and its message's bot", "U0BOTUSER1", "B0PROFILE1", "B0MESSAGE1"},
	} {
		a.OpenAppDetails(c.user, c.b1)
		first := a.secondary.view
		a.OpenAppDetails(c.user, c.b1)
		if a.secondary.view != first {
			t.Errorf("%s: the same app's App details again made a new view", c.name)
		}
		a.OpenAppDetails(c.user, c.b2)
		if a.secondary.view == first {
			t.Errorf("%s: App details for %s kept %s's view", c.name, c.b2, c.b1)
		}
		a.CloseSecondary()
	}
}
