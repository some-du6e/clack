package core

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// A conversation's newest window core holds that ends before the
// conversation's latest message is read again from Slack when a view
// reads it: a message the socket never brought (it was down, or its
// event came while a page read before it was on its way) would otherwise
// stay missing from every list for the session, as Owl's did from Karim's
// #meta (α.102) while Activity, which fetches its own items, showed it.
// Slack's views refetch the histories a slow reconnect reset
// (fetchAndApplyReconnectCounts, resetChannelsWithFetchedHistory) and
// merge every page into the store by timestamp. Once per latest: a latest
// the newest page can't show (a reply's, a deleted message's) costs one
// read, not one per frame.
func TestHistoryCatchesUpWithLatest(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[1]
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := a.History(ctx, ch, ""); err != nil {
		t.Fatal(err)
	}
	reads := func() int64 { return fs.CallCount("conversations.history") }
	n0 := reads()
	// posted while the socket was down: the fake has it, core doesn't;
	// the counts read after the reconnect name it the latest
	ts, _ := fs.PostAppQuiet(ch, "B0CATCHUP1", "A0CATCHUP1", "Owl", "42110: Hi meta,")
	a.S.NoteMessage(ch, &slack.Message{TS: ts, BotID: "B0CATCHUP1"}, false)
	p, err := a.History(ctx, ch, "")
	if err != nil {
		t.Fatal(err)
	}
	has := func(p Page) bool {
		for _, m := range p.Messages {
			if m.TS == ts {
				return true
			}
		}
		return false
	}
	if !has(p) {
		t.Errorf("the newest window core returned ends at %s, before the conversation's latest %s: not read again", p.Messages[len(p.Messages)-1].TS, ts)
	}
	if got := reads() - n0; got != 1 {
		t.Errorf("%d conversations.history reads for the catch-up, want 1", got)
	}
	// up to date now: cached again
	n1 := reads()
	if _, err := a.History(ctx, ch, ""); err != nil {
		t.Fatal(err)
	}
	// a latest the page can't show (a thread reply's, say): one read, then
	// the cache
	a.S.NoteMessage(ch, &slack.Message{TS: tsAfter(ts)}, false)
	for i := 0; i < 3; i++ {
		if _, err := a.History(ctx, ch, ""); err != nil {
			t.Fatal(err)
		}
	}
	if got := reads() - n1; got != 1 {
		t.Errorf("%d conversations.history reads for an up-to-date window and then a latest no page holds, want 1", got)
	}
}
