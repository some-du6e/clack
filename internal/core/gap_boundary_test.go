package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// noHoleIn fails when a message in posted that falls inside the page's
// range (its oldest to its newest message from Slack) is missing from it:
// a silent hole the list never fills.
func noHoleIn(t *testing.T, what string, p Page, posted []string) {
	t.Helper()
	held := map[string]bool{}
	first, last := "", ""
	for _, m := range p.Messages {
		if m.Pending || m.Failed || m.Ephemeral {
			continue
		}
		held[m.TS] = true
		if first == "" || store.TSLess(m.TS, first) {
			first = m.TS
		}
		if last == "" || store.TSLess(last, m.TS) {
			last = m.TS
		}
	}
	var missing []string
	for _, ts := range posted {
		if !store.TSLess(ts, first) && !store.TSLess(last, ts) && !held[ts] {
			missing = append(missing, ts)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%s: %d messages missing inside the window (%s .. %s), the first %s", what, len(missing), first, last, missing[0])
	}
}

// postQuiet posts n messages to ch on the fake, unpushed (the socket was
// down), and returns their ts.
func postQuiet(fs *fakeslack.Server, ch, user, label string, n int) []string {
	var out []string
	for i := 0; i < n; i++ {
		ts, _ := fs.PostQuiet(ch, user, fmt.Sprintf("%s %d", label, i))
		out = append(out, ts)
	}
	return out
}

// postLive posts n messages to ch on the fake and hands their events to
// core, as the socket brings them.
func postLive(a *App, fs *fakeslack.Server, ch, user, label string, n int) []string {
	var out []string
	for i := 0; i < n; i++ {
		ts, ev := fs.PostQuiet(ch, user, fmt.Sprintf("%s %d", label, i))
		injectMessage(a, ev)
		out = append(out, ts)
	}
	return out
}

// The boundary of what a window holds from Slack is the window's own, as
// Slack's slices keep it (resetReachedEnd, then the socket's messages a
// slice of their own until a page intersects it): two wakes with the
// socket's traffic between them leave the window behind its first gap,
// and the page after the second starts it over instead of joining across
// the first gap. One global mark of the last slow connect, read against a
// ring of live records, joined it: the lunch messages were missing for
// good between yesterday's and the afternoon's.
func TestCatchUpAfterTwoSlowConnects(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[12]
	user := fs.UserIDs()[1]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a.historyLanded(t, ctx, ch)
	var posted []string
	a.socketConnected(false) // the lunch sleep's wake
	posted = append(posted, postQuiet(fs, ch, user, "lunch", 12)...)
	posted = append(posted, postLive(a, fs, ch, user, "afternoon", 70)...)
	a.socketConnected(false) // the meeting's wake
	posted = append(posted, postQuiet(fs, ch, user, "meeting", 8)...)
	noHoleIn(t, "after the second wake", a.historyLanded(t, ctx, ch), posted)
}

// The same with one wake, once the socket's records rolled out of the
// live ring (a busy Grid fills it in well under an hour).
func TestCatchUpAfterLiveRecordsRollOver(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch, other := fs.ChannelIDs()[13], fs.ChannelIDs()[14]
	user := fs.UserIDs()[1]
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a.historyLanded(t, ctx, ch)
	var posted []string
	a.socketConnected(false)
	posted = append(posted, postQuiet(fs, ch, user, "while asleep", 12)...)
	posted = append(posted, postLive(a, fs, ch, user, "after the wake", 70)...)
	postLive(a, fs, other, user, "elsewhere", 1100) // the rest of the workspace
	noHoleIn(t, "after the records rolled over", a.historyLanded(t, ctx, ch), posted)
}

// A window caught up after its wake that the socket kept current joins
// the next page, whatever came over the socket since: a fast reconnect
// that misses one message is no gap. It started the window over and the
// list dropped the scrollback its reader was on.
func TestFastReconnectJoins(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[15]
	user := fs.UserIDs()[1]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a.historyLanded(t, ctx, ch)
	a.socketConnected(false) // the morning's wake, caught up at once
	a.historyLanded(t, ctx, ch)
	before := a.S.Timeline(ch)
	oldest := before.Msgs[0].TS
	postLive(a, fs, ch, user, "this morning", 60)
	a.socketConnected(true) // a brief drop: a fast reconnect
	missed := postQuiet(fs, ch, user, "during the drop", 1)
	fs.SetCountLatest(ch, missed[0])
	a.refreshCounts(ctx) // the counts name it: the list catches up
	p := a.historyLanded(t, ctx, ch)
	if !pageHas(p, missed[0]) {
		t.Fatal("the catch-up never read the missed message")
	}
	after := a.S.Timeline(ch)
	if after.Epoch != before.Epoch {
		t.Errorf("the window started over (epoch %d, was %d): a fast reconnect left no gap", after.Epoch, before.Epoch)
	}
	if !pageHas(p, oldest) {
		t.Error("the window lost its scrollback")
	}
}

// An older page out when the window starts over (a gap of more than a
// page after a wake) belongs to the old window: it's dropped, not put in
// front of the new one, which left a hole between the two that paging
// back never filled.
func TestOlderPageAcrossRestart(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[16]
	user := fs.UserIDs()[1]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p := a.historyLanded(t, ctx, ch)
	if !p.HasMore || len(p.Messages) == 0 {
		t.Fatalf("the window: %d messages, HasMore %v", len(p.Messages), p.HasMore)
	}
	before := p.Messages[0].TS
	fs.SetChannelFault("conversations.history", ch, fakeslack.Fault{ReplyDelay: 1500 * time.Millisecond, Times: 1})
	n := fs.HistoryReads(ch)
	older := make(chan error, 1)
	go func() { // the reader scrolls back: the older page is out
		_, err := a.History(ctx, ch, before)
		older <- err
	}()
	for dl := time.Now().Add(5 * time.Second); fs.HistoryReads(ch) == n; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("the older read never reached the fake")
		}
	}
	a.socketConnected(false)
	posted := postQuiet(fs, ch, user, "overnight", 60)
	epoch := a.S.Timeline(ch).Epoch
	a.historyLanded(t, ctx, ch)
	if tl := a.S.Timeline(ch); tl == nil || tl.Epoch == epoch {
		t.Fatal("the window didn't start over")
	}
	if err := <-older; err != nil {
		t.Fatal(err)
	}
	p, err := a.History(ctx, ch, "")
	if err != nil {
		t.Fatal(err)
	}
	noHoleIn(t, "after the old window's older page landed", p, posted)
}

// A page carrying the confirmed copy of a send whose reply was lost
// confirms it: the failed copy, with its Retry, doesn't come back once the
// message leaves the newest page. The page only hid the local copy.
func TestPageConfirmsFailedSend(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[17]
	user := fs.UserIDs()[1]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a.historyLanded(t, ctx, ch)
	// Slack stores the post, but its reply is lost (and so is the echo)
	fs.OnPost = func(f url.Values, _ map[string]any) {
		fs.PostQuietCID(f.Get("channel"), fakeslack.SelfID, f.Get("text"), f.Get("client_msg_id"))
	}
	fs.SetFault("chat.postMessage", fakeslack.Fault{Error: "internal_error", AfterHandle: true, Times: 1})
	if _, err := a.Send(ctx, SendOpts{Channel: ch, Text: "on it"}); err == nil {
		t.Fatal("the send didn't fail")
	}
	copies := func(p Page) (sent, failed int) {
		for _, m := range p.Messages {
			if m.Text != "on it" {
				continue
			}
			if m.Failed {
				failed++
			} else if !m.Pending {
				sent++
			}
		}
		return
	}
	a.socketConnected(false) // the wake after the drop: the catch-up reads Slack's copy
	if sent, failed := copies(a.historyLanded(t, ctx, ch)); sent != 1 || failed != 0 {
		t.Fatalf("after the catch-up: %d sent and %d failed copies, want 1 and 0", sent, failed)
	}
	postQuiet(fs, ch, user, "later", 55)
	a.socketConnected(false)
	if sent, failed := copies(a.historyLanded(t, ctx, ch)); failed != 0 {
		t.Errorf("once Slack's copy left the newest page: %d sent and %d failed copies; the send Slack had came back as failed", sent, failed)
	}
}

// An updated the socket didn't tell marks every cached window stale, on
// screen or not, as Slack's prJX does (only a history_invalid purges):
// the conversation opens on its cache and catches up behind it. Every
// window but the last History caller's was purged, and opened on a
// network read.
func TestUpdatedMarksOffScreenStale(t *testing.T) {
	a, fs, _ := rtApp(t)
	off, on := fs.ChannelIDs()[18], fs.ChannelIDs()[19]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	counts := func(ch, hi, up string) {
		a.applyMutationTimes(&slack.Counts{Channels: []slack.CountEntry{{ID: ch, HistoryInvalid: hi, Updated: up}}})
	}
	base := "1794000000.000100"
	counts(off, base, base)
	counts(on, base, base)
	a.historyLanded(t, ctx, off)
	a.historyLanded(t, ctx, on) // the last History caller
	counts(off, base, "1794000001.000100")
	if a.S.Timeline(off) == nil {
		t.Fatal("an updated purged the window off screen")
	}
	reads := fs.HistoryReads(off)
	p := a.historyLanded(t, ctx, off)
	if len(p.Messages) == 0 {
		t.Fatal("no messages")
	}
	if got := fs.HistoryReads(off) - reads; got < 1 {
		t.Errorf("%d reads: the stale window wasn't caught up when shown", got)
	}
}

// The first counts of a launch are the baseline for a window already
// read: nothing is read again or dropped for them.
func TestFirstCountsAreBaselineForReadWindow(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[20]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a.historyLanded(t, ctx, ch)
	reads := fs.HistoryReads(ch)
	a.applyMutationTimes(&slack.Counts{Channels: []slack.CountEntry{{ID: ch, HistoryInvalid: "1794000005.000100", Updated: "1794000005.000100"}}})
	if a.S.Timeline(ch) == nil {
		t.Fatal("the first counts purged a window read before them")
	}
	a.historyLanded(t, ctx, ch)
	if got := fs.HistoryReads(ch) - reads; got != 0 {
		t.Errorf("%d reads for the first counts of a launch, want 0", got)
	}
}

// channel_history_changed drops the cached history of the conversation
// (Slack truncates it), and the next counts read nothing for it.
func TestChannelHistoryChangedPurges(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[21]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	base := "1794000000.000100"
	a.applyMutationTimes(&slack.Counts{Channels: []slack.CountEntry{{ID: ch, HistoryInvalid: base, Updated: base}}})
	a.historyLanded(t, ctx, ch)
	a.Inject(rawEventFor(ch, map[string]any{"type": "channel_history_changed", "latest": "1794000002.000100", "ts": "1794000002.000100", "event_ts": "1794000002.000200"}))
	if a.S.Timeline(ch) != nil {
		t.Error("channel_history_changed left the cached history")
	}
	a.historyLanded(t, ctx, ch)
	reads := fs.HistoryReads(ch)
	a.applyMutationTimes(&slack.Counts{Channels: []slack.CountEntry{{ID: ch, HistoryInvalid: "1794000002.000200", Updated: base}}})
	if a.S.Timeline(ch) == nil {
		t.Error("the counts after the event purged again")
	}
	a.historyLanded(t, ctx, ch)
	if got := fs.HistoryReads(ch) - reads; got != 0 {
		t.Errorf("%d reads for a change the event told, want 0", got)
	}
}

// A socket message past the gap of a window behind a slow connect carries
// the epoch of the view past the gap (Event.Epoch), the one History's page
// has while the catch-up is out: the list switches to that slice as the
// message comes, whether its reload after the wake ran before it or not.
// It switched only when a reload came after the message, and drew the
// pre-gap messages straight above it otherwise. A message with no gap
// carries none.
func TestPostGapMessageCarriesGapEpoch(t *testing.T) {
	a, fs, l := rtApp(t)
	ch := fs.ChannelIDs()[23]
	user := fs.UserIDs()[1]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a.historyLanded(t, ctx, ch)
	plain := postLive(a, fs, ch, user, "before the wake", 1)[0]
	if ev := messageEvent(t, l, plain); ev.Epoch != 0 {
		t.Errorf("a message with no gap carries epoch %x", ev.Epoch)
	}
	fs.SetChannelFault("conversations.history", ch, fakeslack.Fault{ReplyDelay: 3 * time.Second, Times: 1}) // the catch-up stays out
	a.socketConnected(false)                                                                                // a wake
	postQuiet(fs, ch, user, "while asleep", 3)
	past := postLive(a, fs, ch, user, "after the wake", 1)[0]
	ev := messageEvent(t, l, past)
	if ev.Epoch&GapView == 0 {
		t.Fatalf("the message past the gap carries epoch %x, no GapView", ev.Epoch)
	}
	p, err := a.History(ctx, ch, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Epoch != ev.Epoch {
		t.Errorf("History's page while the catch-up is out has epoch %x, the message's event %x", p.Epoch, ev.Epoch)
	}
	if len(p.Messages) != 1 || p.Messages[0].TS != past {
		t.Errorf("the page past the gap holds %d messages, want the one after the wake", len(p.Messages))
	}
	a.landed(t, ch)
}

func rawEventFor(ch string, v map[string]any) slack.Event {
	raw, _ := json.Marshal(v)
	cj, _ := json.Marshal(ch)
	return slack.Event{Type: v["type"].(string), Channel: cj, Raw: raw}
}
