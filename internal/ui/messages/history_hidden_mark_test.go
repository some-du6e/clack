package messages

import (
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// hiddenMarkEnv is a channel of 40 messages on the fake, unread from
// lastRead to its newest, whose newest messages a plugin's patch hides
// (Slick's BetterBlock, the newest messages' author blocked); the list
// opens on it, marking read itself unless preview.
func hiddenMarkEnv(t *testing.T, preview bool, hide func(tss []string) (lastRead string, hidden []string)) (*winEnv, *fakeslack.Server, *core.App) {
	t.Helper()
	app, fs, _ := shotsApp(t)
	ch := winEndChan
	app.S.PutConv(slack.Conversation{ID: ch, Name: "archive-walk", IsChannel: true, IsMember: true, NumMembers: 12})
	e := &winEnv{h: &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}}
	base := time.Now().Add(-7 * time.Hour).Unix()
	for i := 0; i < 40; i++ {
		ts := fmt.Sprintf("%d.000100", base+int64(i)*90)
		fs.PostQuietAt(ch, "UBEN00001", fmt.Sprintf("message %d", i), ts)
		e.tss = append(e.tss, ts)
	}
	lastRead, hidden := hide(e.tss)
	app.S.SetCounts(&slack.Counts{Channels: []slack.CountEntry{{ID: ch, LastRead: lastRead, Latest: e.newest(), HasUnreads: true}}})
	historyHiddenHook = func(key string) (*taut.Hidden, bool) {
		if key != ch {
			return nil, true
		}
		return taut.HiddenOf(hidden...), true
	}
	t.Cleanup(func() { historyHiddenHook = nil })
	e.v = NewChannelView(e.h, ch)
	e.v.Preview = preview
	e.rt = newRouted(e.v, 900, 700)
	return e, fs, app
}

// The unread banner's ✕ over a list whose newest messages a plugin hides
// marks the channel's real newest message, as Slack's
// markMostRecentMsgRead marks getLatestTs (the channel's latest or the
// slice's last real message), not the newest one drawn: with the read
// cursor before the newest drawn message, and with it past it, where
// marking the newest drawn would move the cursor back and turn read
// messages unread on every client.
func TestHiddenNewestBannerMarksLatest(t *testing.T) {
	for _, c := range []struct {
		name   string
		cursor int // the read cursor's message, from the newest (1 = the newest)
	}{
		{"cursor before the newest drawn", 6},
		{"cursor past the newest drawn", 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			var lastDrawn string
			e, fs, _ := hiddenMarkEnv(t, true, func(tss []string) (string, []string) {
				n := len(tss)
				lastDrawn = tss[n-3]
				return tss[n-c.cursor], []string{tss[n-1], tss[n-2]}
			})
			newest := e.newest()
			e.await(t, "the banner over the list without its two newest messages", func() bool {
				return e.v.s.ub.text != "" && e.drawn(lastDrawn) && !e.drawn(newest)
			})
			e.rt.press(centre(e.v.s.ub.closeR), true)
			until(t, 3*time.Second, "the ✕'s conversations.mark", e.rt.frame, func() bool { return fs.Marked(winEndChan) != "" })
			if got := fs.Marked(winEndChan); got != newest {
				t.Errorf("the ✕ marked %s, want the newest message %s (the newest drawn is %s)", got, newest, lastDrawn)
			}
		})
	}
}

// Under "Mark as read: immediately" the list marks the channel read as it
// shows, newest messages a plugin hides included: before HideResolved's
// patched history they were, as Slack's are (its mark reads the slice's
// last real message, not the last row drawn).
func TestHiddenNewestMarkedImmediately(t *testing.T) {
	var prev store.Prefs
	if p := uipref.Get(); p != nil {
		prev = *p
	}
	uipref.Set(store.Prefs{MarkReadImmediately: true})
	t.Cleanup(func() { uipref.Set(prev) })
	e, fs, _ := hiddenMarkEnv(t, false, func(tss []string) (string, []string) {
		n := len(tss)
		return tss[n-6], []string{tss[n-1]}
	})
	newest := e.newest()
	until(t, 10*time.Second, "the list's conversations.mark", e.rt.frame, func() bool { return fs.Marked(winEndChan) != "" })
	e.frames(2 * time.Second) // any mark after the first
	if got := fs.Marked(winEndChan); got != newest {
		t.Errorf("marked read to %s, want the newest message %s (hidden from the list)", got, newest)
	}
}
