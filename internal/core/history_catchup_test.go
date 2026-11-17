package core

import (
	"context"
	"encoding/json"
	"fmt"
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
// Slack resets its histories' reachedEnd on a connect that wasn't fast
// (CFM onConnected, resetReachedEnd), so a view reads the newest page
// again when it shows, and merges every page into its store by timestamp
// (1qrw addTimestamps). Once per latest: a latest
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
	p := a.historyLanded(t, ctx, ch)
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
	a.historyLanded(t, ctx, ch)
	// a latest the page can't show (a thread reply's, say): one read, then
	// the cache
	a.S.NoteMessage(ch, &slack.Message{TS: tsAfter(ts)}, false)
	for i := 0; i < 3; i++ {
		a.historyLanded(t, ctx, ch)
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
	// the cached window shows at once, the catch-up lands after
	if p, err := a.History(ctx, ch, ""); err != nil || pageHas(p, missed) {
		t.Fatalf("History waited for the catch-up (%v)", err)
	}
	p := a.historyLanded(t, ctx, ch)
	if !pageHas(p, missed) || !pageHas(p, nextTS) {
		t.Errorf("after the slow connect the window has the missed message %v and the next one %v", pageHas(p, missed), pageHas(p, nextTS))
	}
	if got := fs.HistoryReads(ch) - reads; got != 1 {
		t.Errorf("%d conversations.history reads after the slow connect, want 1", got)
	}
	// read once: cached until the next slow connect
	a.historyLanded(t, ctx, ch)
	if got := fs.HistoryReads(ch) - reads; got != 1 {
		t.Errorf("%d conversations.history reads after a second History, want 1", got)
	}
	rs := a.repliesLanded(t, ctx, ch, rootTS)
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
	a.historyLanded(t, ctx, ch)
	if got := fs.HistoryReads(ch) - reads; got != 1 {
		t.Errorf("%d conversations.history reads after a fast connect, want 1", got)
	}
}

// A catch-up that fails shows the cached window and is tried again by
// itself after catchUpRetry (not at the list's next 30 s reload); one that
// succeeds isn't tried again. (The client holds an identical failed
// request back for slack.BackoffBase, 1 s; catchUpRetry is 5 s.)
func TestHistoryCatchUpRetries(t *testing.T) {
	old := catchUpRetry
	catchUpRetry = slack.BackoffBase + 500*time.Millisecond
	t.Cleanup(func() { catchUpRetry = old })
	a, fs, l := rtApp(t)
	ch := fs.ChannelIDs()[4]
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := a.History(ctx, ch, ""); err != nil {
		t.Fatal(err)
	}
	missed, _ := fs.PostAppQuiet(ch, "B0CATCHUP3", "A0CATCHUP3", "Owl", "42141: Hi meta,")
	a.socketConnected(false)
	fs.SetChannelFault("conversations.history", ch, fakeslack.Fault{Error: "internal_error", Times: 1})
	n := fs.HistoryReads(ch)
	if p, err := a.History(ctx, ch, ""); err != nil || pageHas(p, missed) {
		t.Fatalf("History: %v, or it has the message before any read", err)
	}
	for dl := time.Now().Add(10 * time.Second); fs.HistoryReads(ch) == n; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("no catch-up read")
		}
	}
	time.Sleep(200 * time.Millisecond)
	if p, _ := a.History(ctx, ch, ""); pageHas(p, missed) {
		t.Fatal("the failed catch-up's page has the message")
	}
	// nothing reads History again: the retry comes by itself and says so
	l.wait(t, "the retried catch-up's history_caught_up", func(evs []Event) bool {
		for _, e := range evs {
			if e.Type == "history_caught_up" && e.Channel == ch {
				return true
			}
		}
		return false
	})
	if p, _ := a.History(ctx, ch, ""); !pageHas(p, missed) {
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
	counts("1794000000.000100", "1794000000.000100") // applied before any window: the baseline
	a.watch(ch, "")                                  // on screen: an updated reads it again, a history_invalid purges it
	a.historyLanded(t, ctx, ch)
	reads := fs.HistoryReads(ch)
	// the socket told of a reaction: its event_ts moves updated first
	a.advanceMutation(ch, "1794000001.000100", false)
	counts("1794000000.000100", "1794000001.000100")
	a.historyLanded(t, ctx, ch)
	if got := fs.HistoryReads(ch) - reads; got != 0 {
		t.Errorf("%d reads for a change the socket told, want 0", got)
	}
	// an edit it never told
	counts("1794000000.000100", "1794000002.000100")
	a.historyLanded(t, ctx, ch)
	a.historyLanded(t, ctx, ch)
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

// The viewer's failed (and unconfirmed) sends stay, with their Retry,
// through every re-read and purge, whatever the page's range: no page
// can hold a local copy, and Slack keeps them apart from the history
// slices. A catch-up after a wake dropped a failed send once anyone had
// posted after it, and a purge dropped them all.
func TestFailedSendSurvivesCatchUp(t *testing.T) {
	a, fs, _ := rtApp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	failedIn := func(ch string, p Page) bool {
		for _, m := range p.Messages {
			if m.Text == "on it" && m.Failed {
				return true
			}
		}
		return false
	}
	// read reads ch's newest window as a list does, after any catch-up
	// History started has landed
	read := func(ch string) Page {
		t.Helper()
		return a.historyLanded(t, ctx, ch)
	}
	sendFailed := func(ch string) {
		t.Helper()
		fs.SetFault("chat.postMessage", fakeslack.Fault{Error: "internal_error", Times: 1})
		if _, err := a.Send(ctx, SendOpts{Channel: ch, Text: "on it"}); err == nil {
			t.Fatal("the send didn't fail")
		}
		other := fs.UserIDs()[2]
		for i := 0; i < 3; i++ { // others post after it
			_, ev := fs.PostQuiet(ch, other, fmt.Sprintf("after the failed send %d", i))
			injectMessage(a, ev)
		}
	}

	t.Run("a re-read after a slow connect", func(t *testing.T) {
		ch := fs.ChannelIDs()[7]
		if _, err := a.History(ctx, ch, ""); err != nil {
			t.Fatal(err)
		}
		sendFailed(ch)
		a.socketConnected(false)
		if p := read(ch); !failedIn(ch, p) {
			t.Error("the failed send left the window after the catch-up re-read")
		}
	})

	t.Run("a purge", func(t *testing.T) {
		ch := fs.ChannelIDs()[8]
		counts := func(hi string) {
			a.applyMutationTimes(&slack.Counts{Channels: []slack.CountEntry{{ID: ch, HistoryInvalid: hi, Updated: "1794000000.000100"}}})
		}
		counts("1794000000.000100")
		if _, err := a.History(ctx, ch, ""); err != nil {
			t.Fatal(err)
		}
		sendFailed(ch)
		counts("1794000009.000100") // a deletion the socket missed: the history goes
		if p := read(ch); !failedIn(ch, p) {
			t.Error("the failed send went with the purged history")
		}
	})
}

// What the socket tells moves the counts' mutation timestamps (Slack's
// maybeAdvanceChannelUpdatedTimestamp and
// maybeAdvanceChannelHistoryInvalidTimestamp, from its reaction, edit and
// deletion handlers): the next counts, which carry each event's
// timestamp, read nothing and purge nothing for them. A change the socket
// never told still reads the window again.
func TestSocketEventsAdvanceMutations(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[9]
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	counts := func(hi, up string) {
		a.applyMutationTimes(&slack.Counts{Channels: []slack.CountEntry{{ID: ch, HistoryInvalid: hi, Updated: up}}})
	}
	counts("1794000000.000100", "1794000000.000100") // the baseline, the socket up (countsApplied)
	a.watch(ch, "")
	p := a.historyLanded(t, ctx, ch)
	if len(p.Messages) < 2 {
		t.Fatalf("the window has %d messages", len(p.Messages))
	}
	first, last := p.Messages[0], p.Messages[len(p.Messages)-1]
	reads := fs.HistoryReads(ch)
	inject := func(v map[string]any) {
		raw, _ := json.Marshal(v)
		chRaw, _ := json.Marshal(ch)
		a.Inject(slack.Event{Type: v["type"].(string), Channel: chRaw, Raw: raw})
	}
	inject(map[string]any{"type": "reaction_added", "user": fs.UserIDs()[1], "reaction": "eyes", "event_ts": "1794000001.000100",
		"item": map[string]any{"type": "message", "channel": ch, "ts": last.TS}})
	counts("1794000000.000100", "1794000001.000100")
	inject(map[string]any{"type": "message", "subtype": "message_changed", "channel": ch, "event_ts": "1794000002.000100",
		"message": map[string]any{"type": "message", "ts": last.TS, "user": last.User, "text": "edited, and the socket said so"}})
	counts("1794000000.000100", "1794000002.000100")
	inject(map[string]any{"type": "message", "subtype": "message_deleted", "channel": ch, "event_ts": "1794000003.000100", "deleted_ts": first.TS})
	counts("1794000003.000100", "1794000002.000100")
	if a.S.Timeline(ch) == nil {
		t.Fatal("a deletion the socket told purged the window")
	}
	a.historyLanded(t, ctx, ch)
	if got := fs.HistoryReads(ch) - reads; got != 0 {
		t.Errorf("%d reads for changes the socket told, want 0", got)
	}
	// control: an updated past what the socket told
	counts("1794000003.000100", "1794000004.000100")
	a.historyLanded(t, ctx, ch)
	if got := fs.HistoryReads(ch) - reads; got != 1 {
		t.Errorf("%d reads for an updated the socket never told, want 1", got)
	}
}

// An updated the socket never told reads the open conversation's
// scrollback again too, not only its newest page and threads: an edit in
// the older messages the list shows kept the copy from before it
// (refreshOlder).
func TestHistoryRefreshesScrollback(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[10]
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	counts := func(hi, up string) {
		a.applyMutationTimes(&slack.Counts{Channels: []slack.CountEntry{{ID: ch, HistoryInvalid: hi, Updated: up}}})
	}
	counts("1794000000.000100", "1794000000.000100")
	a.watch(ch, "")
	p := a.historyLanded(t, ctx, ch)
	if !p.HasMore || len(p.Messages) == 0 {
		t.Fatalf("the newest page: %d messages, HasMore %v", len(p.Messages), p.HasMore)
	}
	older, err := a.History(ctx, ch, p.Messages[0].TS) // the user scrolls back
	if err != nil || len(older.Messages) < 2 {
		t.Fatalf("the older page: %v, %d messages", err, len(older.Messages))
	}
	edited := older.Messages[len(older.Messages)/2].TS
	fs.EditQuiet(ch, edited, "edited where the user reads")
	counts("1794000000.000100", "1794000001.000100")
	p = a.historyLanded(t, ctx, ch)
	found := false
	for _, m := range p.Messages {
		if m.TS == edited {
			found = true
			if m.Text != "edited where the user reads" {
				t.Errorf("the scrollback's message reads %q: the edit Slack has didn't show", m.Text)
			}
		}
	}
	if !found {
		t.Error("the scrollback's message is gone from the window")
	}
}

// A catch-up that joins a read begun before the mark that asked for it
// reads once more: the joined page predates what the mark covers.
func TestHistoryCatchUpOnceMore(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[11]
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	counts := func(hi, up string) {
		a.applyMutationTimes(&slack.Counts{Channels: []slack.CountEntry{{ID: ch, HistoryInvalid: hi, Updated: up}}})
	}
	counts("1794000000.000100", "1794000000.000100")
	a.watch(ch, "")
	a.historyLanded(t, ctx, ch)
	// a refresh goes out and is slow to answer: the fake answers from what
	// it holds now
	fs.SetChannelFault("conversations.history", ch, fakeslack.Fault{ReplyDelay: 800 * time.Millisecond, Times: 1})
	n := fs.HistoryReads(ch)
	refreshed := make(chan error, 1)
	go func() {
		_, err := a.Refresh(ctx, ch)
		refreshed <- err
	}()
	for dl := time.Now().Add(5 * time.Second); fs.HistoryReads(ch) == n; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("the refresh never reached the fake")
		}
	}
	ts, _ := fs.PostQuiet(ch, fs.UserIDs()[1], "posted after the refresh went out")
	counts("1794000000.000100", "1794000001.000100") // the counts say the history changed
	select {
	case err := <-refreshed:
		t.Fatalf("the slow refresh answered (%v) before the catch-up could join it", err)
	default:
	}
	p := a.historyLanded(t, ctx, ch)
	if err := <-refreshed; err != nil {
		t.Fatal(err)
	}
	if !pageHas(p, ts) {
		t.Error("the catch-up ended with the page it joined, read before the change the counts named")
	}
}
