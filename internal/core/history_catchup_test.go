package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
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
	ch := fs.ChannelIDs()[2] // read (boot prefetches the unread ones)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := a.History(ctx, ch, ""); err != nil {
		t.Fatal(err)
	}
	reads := func() int { return fs.HistoryReads(ch) } // ch's pages only
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

func pageHas(p Page, ts string) bool {
	for _, m := range p.Messages {
		if m.TS == ts {
			return true
		}
	}
	return false
}

// injectMessage brings ts of ch (posted to the fake with PostQuiet) over
// the socket's path, as its event would.
func injectMessage(a *App, ev map[string]any) {
	raw, _ := json.Marshal(ev)
	chRaw, _ := json.Marshal(ev["channel"])
	a.Inject(slack.Event{Type: "message", Channel: chRaw, Raw: raw})
}

// A slow connect (no reconnect_url: every Mac wake) makes every window
// and thread read before it read Slack again once, however many messages
// the socket brought since, as Slack's onConnected handler resets every
// channel's reachedEnd. Comparing the window's newest with the counts'
// latest missed the gap for good once one more message came.
func TestHistoryRereadsAfterSlowConnect(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[3]
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := a.History(ctx, ch, ""); err != nil {
		t.Fatal(err)
	}
	root := fs.UserIDs()[1]
	rootTS, rootEv := fs.PostQuiet(ch, root, "a thread")
	injectMessage(a, rootEv)
	first := fs.PostReplyQuiet(ch, rootTS, root, "first")
	if _, err := a.Replies(ctx, ch, rootTS); err != nil {
		t.Fatal(err)
	}
	// the socket is down: a message and a reply it never brings
	missed, _ := fs.PostAppQuiet(ch, "B0CATCHUP2", "A0CATCHUP2", "Owl", "42140: Hi meta,")
	missedReply := fs.PostReplyQuiet(ch, rootTS, root, "while the socket was down")
	// back with a slow connect; a message comes over the socket at once
	a.socketConnected(false)
	nextTS, nextEv := fs.PostQuiet(ch, root, "after the reconnect")
	injectMessage(a, nextEv)
	if l, _ := a.S.Count(ch); l.Latest != nextTS {
		t.Fatalf("the counts' latest is %s, want the socket's %s", l.Latest, nextTS)
	}
	reads := fs.HistoryReads(ch)
	p, err := a.History(ctx, ch, "")
	if err != nil {
		t.Fatal(err)
	}
	if !pageHas(p, missed) || !pageHas(p, nextTS) {
		t.Errorf("after the slow connect the window has the missed message %v and the next one %v", pageHas(p, missed), pageHas(p, nextTS))
	}
	if got := fs.HistoryReads(ch) - reads; got != 1 {
		t.Errorf("%d conversations.history reads after the slow connect, want 1", got)
	}
	// read once: cached until the next slow connect
	if _, err := a.History(ctx, ch, ""); err != nil {
		t.Fatal(err)
	}
	if got := fs.HistoryReads(ch) - reads; got != 1 {
		t.Errorf("%d conversations.history reads after a second History, want 1", got)
	}
	rs, err := a.Replies(ctx, ch, rootTS)
	if err != nil {
		t.Fatal(err)
	}
	has := func(ts string) bool {
		for _, m := range rs {
			if m.TS == ts {
				return true
			}
		}
		return false
	}
	if !has(first) || !has(missedReply) {
		t.Errorf("after the slow connect the thread has its first reply %v and the missed one %v", has(first), has(missedReply))
	}
	// a fast connect (reconnect_url) misses nothing: no read
	a.socketConnected(true)
	if _, err := a.History(ctx, ch, ""); err != nil {
		t.Fatal(err)
	}
	if got := fs.HistoryReads(ch) - reads; got != 1 {
		t.Errorf("%d conversations.history reads after a fast connect, want 1", got)
	}
}

// A catch-up that fails shows the cached window and is tried again at a
// read after catchUpRetry; one that succeeds isn't tried again. (The
// client holds an identical failed request back for slack.BackoffBase,
// 1 s; catchUpRetry is 5 s.)
func TestHistoryCatchUpRetries(t *testing.T) {
	old := catchUpRetry
	catchUpRetry = slack.BackoffBase + 500*time.Millisecond
	t.Cleanup(func() { catchUpRetry = old })
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[4]
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := a.History(ctx, ch, ""); err != nil {
		t.Fatal(err)
	}
	missed, _ := fs.PostAppQuiet(ch, "B0CATCHUP3", "A0CATCHUP3", "Owl", "42141: Hi meta,")
	a.socketConnected(false)
	fs.SetFault("conversations.history", fakeslack.Fault{Error: "internal_error", Times: 1})
	p, err := a.History(ctx, ch, "")
	if err != nil {
		t.Fatalf("a failed catch-up failed the read: %v", err)
	}
	if pageHas(p, missed) {
		t.Fatal("the failed catch-up's page has the message")
	}
	if p, _ = a.History(ctx, ch, ""); pageHas(p, missed) {
		t.Error("read again before catchUpRetry")
	}
	time.Sleep(catchUpRetry + 100*time.Millisecond)
	if p, _ = a.History(ctx, ch, ""); !pageHas(p, missed) {
		t.Error("the catch-up wasn't tried again after catchUpRetry")
	}
}

// client.counts' history_invalid and updated (Slack's channel mutation
// timestamps): past the ones last applied, history_invalid purges the
// window and its threads, updated marks it stale (read again once);
// the socket's own changes move them first, so what it told reads
// nothing (maybePurgeChannelHistoryAfterMutations,
// maybeAdvanceChannelUpdatedTimestamp).
func TestHistoryCountsMutations(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[5]
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	counts := func(hi, up string) {
		a.applyMutationTimes(&slack.Counts{Channels: []slack.CountEntry{{ID: ch, HistoryInvalid: hi, Updated: up}}})
	}
	counts("1794000000.000100", "1794000000.000100") // applied before any window
	if _, err := a.History(ctx, ch, ""); err != nil {
		t.Fatal(err)
	}
	reads := fs.HistoryReads(ch)
	// the socket told of a reaction: its event_ts moves updated first
	a.advanceMutation(ch, "1794000001.000100", false)
	counts("1794000000.000100", "1794000001.000100")
	if _, err := a.History(ctx, ch, ""); err != nil {
		t.Fatal(err)
	}
	if got := fs.HistoryReads(ch) - reads; got != 0 {
		t.Errorf("%d reads for a change the socket told, want 0", got)
	}
	// an edit it never told
	counts("1794000000.000100", "1794000002.000100")
	if _, err := a.History(ctx, ch, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.History(ctx, ch, ""); err != nil {
		t.Fatal(err)
	}
	if got := fs.HistoryReads(ch) - reads; got != 1 {
		t.Errorf("%d reads for an updated past the one applied, want 1", got)
	}
	// a deletion it never told: the window goes
	counts("1794000003.000100", "1794000002.000100")
	if a.S.Timeline(ch) != nil {
		t.Error("a history_invalid past the one applied left the window")
	}
}

// A socket message from someone core doesn't hold asks for nobody (Slack's
// messageReceived fetches nobody): its conversion reads the store without
// asking, so no users/info goes out and no "names" event resnapshots the
// sidebar, reloads the lists and invalidates the plugins' members for a
// message nothing draws. The rows that draw it ask for its people
// (lazy_users.go).
func TestSocketMessageAsksForNobody(t *testing.T) {
	a, fs, l := rtApp(t)
	ch := fs.ChannelIDs()[6]
	sender, mentioned := fakeslack.SynthMemberID(900), fakeslack.SynthMemberID(901)
	time.Sleep(300 * time.Millisecond) // boot's own lookups and their events
	asked, n0 := fs.CallCount("edge/users/info"), len(l.all())
	ts, ev := fs.PostQuiet(ch, sender, "hi <@"+mentioned+">")
	injectMessage(a, ev)
	l.wait(t, "the message event", func(evs []Event) bool {
		for _, e := range evs {
			if e.Type == "message" && e.TS == ts {
				return true
			}
		}
		return false
	})
	time.Sleep(500 * time.Millisecond) // the member fetcher's tick and batch, were anything asked
	if got := fs.CallCount("edge/users/info") - asked; got != 0 {
		t.Errorf("%d users/info calls for a socket message nothing draws", got)
	}
	for _, e := range l.all()[n0:] {
		if e.Type == "names" {
			t.Errorf("a %q event after a socket message nothing draws", e.Type)
			break
		}
	}
}
