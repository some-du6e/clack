package ui

// A jump's window is a slice of its own, as Slack's channelHistory slices
// are (client-boot 1qrw addTimestamps, invH insert, B0mF
// getTimestampsByChannelId; boot-shared K+Na MessagePane; build
// 2c0d02ca130f):
//
//   - the window is the list's own: a jump in the split view leaves the
//     main view of the same conversation at its newest messages;
//   - paging inside the window stays inside it: the conversation's newest
//     messages (core's timeline) never take a page of it, so "Latest
//     messages" draws them with no gap;
//   - the window's messages take live changes (a reaction on one), and a
//     new message joins the newest messages, drawn once the list is back
//     at them;
//   - entering the conversation again is Slack's determineTimestamps:
//     where it was left (the scrollmark, the window too) with nothing
//     new, its newest messages once it has unreads;
//   - a jump into a conversation not loaded yet loads only the window
//     (two calls), never the newest page first;
//   - after a reply-rooted pane hands on, the conversation highlights the
//     thread's root even when the window around the reply came first.
//
// Synthetic fakeslack history; nothing talks to Slack.

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// longEnv is a workspace whose channels C0FAKE00001…3 hold 3,000 messages
// each, with the thread header fixtures; its read channels are read to
// their newest message (ReadCursors), so they open at it.
func longEnv(t *testing.T) *perfEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.ThreadHeader, sz.ReadCursors = true, true
	sz.LongChannels, sz.LongHistory = 3, 3000
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	return e
}

// longRead is a long channel that starts read (fakeslack makes every
// tenth channel from the second unread, and core prefetches those).
const longRead = "C0FAKE00002"

// plainAt is the first message from the i-th newest back in ch that isn't
// a thread's parent.
func plainAt(t *testing.T, fs *fakeslack.Server, ch string, i int) string {
	t.Helper()
	for k := i; k < i+100; k++ {
		if m, ok := fs.HistoryAt(ch, k); ok && m.ThreadTS == "" {
			return m.TS
		}
	}
	t.Fatalf("no plain message %d back in %s", i, ch)
	return ""
}

func drewRow(rows []messages.SenderView, ts string) bool {
	for _, r := range rows {
		if r.TS == ts {
			return true
		}
	}
	return false
}

// openAtOld opens ch's newest messages, then the message far back:
// the window around it, the message drawn highlighted.
func (e *perfEnv) openAtOld(t *testing.T, ch, target string) {
	t.Helper()
	newest, _ := e.fs.HistoryAt(ch, 0)
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	e.a.Navigate("c/" + ch + "/m/" + target)
	e.waitDrawn(t, "the old message, highlighted", 30*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return lit(rows, target) })
}

// latestMessages clicks the conversation's "Latest messages" pill.
func (e *perfEnv) latestMessages(t *testing.T, ch string, at time.Duration) {
	t.Helper()
	var r image.Rectangle
	e.until("Latest messages", 10*time.Second, func() bool {
		if cv := e.channelView(ch); cv != nil {
			r = cv.LatestMessagesRect()
		}
		return !r.Empty()
	})
	e.clickAt(rectMid(r), at)
}

// TestJumpWindowIsTheListsOwn: the split view opened at a message far back
// shows the window around it, and the main view of the same conversation
// keeps its newest messages (Slack's panes each read their own startTs's
// slice). Clack swapped the conversation's one model for the window, so
// the main view jumped too.
func TestJumpWindowIsTheListsOwn(t *testing.T) {
	e := longEnv(t)
	ch := longRead
	newest, _ := e.fs.HistoryAt(ch, 0)
	target := plainAt(t, e.fs, ch, 2500)
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	e.a.OpenSplit("c/" + ch + "/m/" + target)
	e.waitDrawn(t, "the split view at the old message", 30*time.Second, paneSplit, ch, "", func(rows []messages.SenderView) bool {
		return lit(rows, target)
	})
	e.settle(300 * time.Millisecond)
	if rows := e.paneRows("", ch, ""); !drewRow(rows, newest.TS) || drewRow(rows, target) {
		t.Errorf("the main view moved with the split view's jump: it drew %s; want its newest message %s, not %s", drawnState(rows), newest.TS, target)
	}
}

// TestJumpWindowPagesWithinItself: scrolling up inside the window loads
// the page before it (requestOlder: latest its start, inclusive, 28) into
// the window only; core's newest messages are as they were, and "Latest
// messages" draws them with nothing of the window beside them. Clack
// prepended the window's older page to the conversation's newest
// messages, leaving 2,400 messages missing between them with no gap.
func TestJumpWindowPagesWithinItself(t *testing.T) {
	e := longEnv(t)
	ch := longRead
	newest, _ := e.fs.HistoryAt(ch, 0)
	target := plainAt(t, e.fs, ch, 2500)
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	tl := e.a.Core().S.Timeline(ch)
	if tl == nil || len(tl.Msgs) == 0 {
		t.Fatalf("no newest messages in core for %s", ch)
	}
	start, n := tl.Msgs[0].TS, len(tl.Msgs)
	e.a.Navigate("c/" + ch + "/m/" + target)
	e.waitDrawn(t, "the old message, highlighted", 30*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return lit(rows, target) })
	calls := e.recordCalls("conversations.history")
	older := func() int {
		k := 0
		for _, c := range calls.of("conversations.history", ch, "latest") {
			if c != "latest=" {
				k++
			}
		}
		return k
	}
	deadline := time.Now().Add(30 * time.Second)
	for older() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("scrolling up the window asked for no older page: %q", calls.of("conversations.history", ch, "latest", "oldest", "limit"))
		}
		e.scroll(e.listPoint(), -3000)
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
	e.settle(800 * time.Millisecond)
	if tl := e.a.Core().S.Timeline(ch); tl == nil || len(tl.Msgs) != n || tl.Msgs[0].TS != start {
		got := "none"
		if tl != nil && len(tl.Msgs) > 0 {
			got = fmt.Sprintf("%d messages from %s", len(tl.Msgs), tl.Msgs[0].TS)
		}
		t.Errorf("core's newest messages after paging the window: %s; want them as they were, %d from %s", got, n, start)
	}
	e.latestMessages(t, ch, 2*time.Second)
	e.waitDrawn(t, "the newest messages again", 15*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	e.settle(300 * time.Millisecond)
	for _, r := range e.paneRows("", ch, "") {
		if slackTSBefore(r.TS, start) {
			t.Errorf("the newest messages drew %s, from before their first message %s: a page of the window joined them", r.TS, start)
			break
		}
	}
}

// slackTSBefore compares Slack timestamps.
func slackTSBefore(a, b string) bool {
	x, _ := strconv.ParseFloat(a, 64)
	y, _ := strconv.ParseFloat(b, 64)
	return x < y
}

// TestJumpWindowTakesLiveChanges: a message in the window takes another
// member's reaction (core holds the window's messages, so the event finds
// it), and a new message joins the newest messages: not the window, and
// drawn once "Latest messages" goes back to them. Clack kept the window
// outside core's store, so the reaction never showed.
func TestJumpWindowTakesLiveChanges(t *testing.T) {
	e := longEnv(t)
	ch := longRead
	target := plainAt(t, e.fs, ch, 2500)
	e.openAtOld(t, ch, target)
	who := e.fs.UserIDs()[3]
	raw, _ := json.Marshal(map[string]any{"type": "reaction_added", "user": who, "reaction": "tada",
		"item": map[string]any{"type": "message", "channel": ch, "ts": target}, "event_ts": fmt.Sprintf("%d.000200", time.Now().Unix())})
	e.a.Core().Inject(slack.Event{Type: "reaction_added", Raw: raw})
	e.until("the reaction on the old message", 10*time.Second, func() bool {
		cv := e.channelView(ch)
		if cv == nil {
			return false
		}
		for _, r := range cv.ReactionRects() {
			if r.TS == target && r.Name == "tada" {
				return true
			}
		}
		return false
	})
	ts := fmt.Sprintf("%d.000300", time.Now().Unix())
	raw, _ = json.Marshal(map[string]any{"type": "message", "channel": ch, "user": who, "text": "synthetic live message", "ts": ts})
	e.a.Core().Inject(slack.Event{Type: "message", Channel: json.RawMessage(strconv.Quote(ch)), Raw: raw})
	e.settle(500 * time.Millisecond)
	if rows := e.paneRows("", ch, ""); drewRow(rows, ts) {
		t.Errorf("the new message joined the window: %s", drawnState(rows))
	}
	e.latestMessages(t, ch, 2*time.Second)
	e.waitDrawn(t, "the new message among the newest", 15*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, ts) })
}

// TestEnteringConversationAfterJump is Slack's determineTimestamps for a
// navigation with no message to start at: with nothing new the
// conversation opens where it was left (MessagePane's startTs falls back
// to the channel's scrollmark), the window too; once it has unreads the
// scrollmark goes and its newest messages show, the new one in them.
// Clack kept the window either way, the new message never drawn.
func TestEnteringConversationAfterJump(t *testing.T) {
	e := longEnv(t)
	ch, other := longRead, fakeslack.ThreadPrivateID
	target := plainAt(t, e.fs, ch, 2500)
	e.openAtOld(t, ch, target)
	e.openChannel(other)
	e.settle(300 * time.Millisecond)
	e.openChannel(ch)
	e.settle(500 * time.Millisecond)
	if cv := e.channelView(ch); cv == nil || !cv.InWindow() || !drewRow(e.paneRows("", ch, ""), target) {
		t.Errorf("entered with nothing new: drew %s; want where it was left, the window at %s", drawnState(e.paneRows("", ch, "")), target)
	}
	e.openChannel(other)
	e.settle(300 * time.Millisecond)
	ts := fmt.Sprintf("%d.000400", time.Now().Unix())
	raw, _ := json.Marshal(map[string]any{"type": "message", "channel": ch, "user": e.fs.UserIDs()[3], "text": "synthetic new message", "ts": ts})
	e.a.Core().Inject(slack.Event{Type: "message", Channel: json.RawMessage(strconv.Quote(ch)), Raw: raw})
	e.until("the conversation unread", 10*time.Second, func() bool { return e.a.Core().Count(ch).Unread })
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages, the new one in them", 15*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, ts) })
	if cv := e.channelView(ch); cv != nil && cv.InWindow() {
		t.Errorf("entered with unreads: still the window")
	}
}

// TestColdJumpLoadsOnlyTheWindow: a jump to a message far back in a
// conversation this session never loaded is Slack's pane mounted at
// startTs: requestAround, two conversations.history calls, and the newest
// page is neither fetched nor drawn first. Clack fetched and drew the
// newest page, then sent the two calls: three requests, and a flash of
// the wrong page.
func TestColdJumpLoadsOnlyTheWindow(t *testing.T) {
	e := longEnv(t)
	e.coldJump(t, nil)
}

// TestColdJumpRefreshedBeforeFirstFrame: core's names landing between the
// navigation and the list's first frame (refreshVisible) don't load the
// newest page for the jump's list: it still loads only the window. The
// refresh loaded the newest page, then the window: three calls, and the
// newest page drawn first.
func TestColdJumpRefreshedBeforeFirstFrame(t *testing.T) {
	e := longEnv(t)
	e.coldJump(t, e.a.refreshVisible)
}

// coldJump is TestColdJumpLoadsOnlyTheWindow's jump and checks, with
// between run after the navigation, before the first frame.
func (e *perfEnv) coldJump(t *testing.T, between func()) {
	t.Helper()
	ch := "C0FAKE00003" // read: not prefetched
	if e.a.Core().S.Timeline(ch) != nil {
		t.Fatalf("%s's history is loaded already", ch)
	}
	newest, _ := e.fs.HistoryAt(ch, 0)
	target := plainAt(t, e.fs, ch, 500)
	calls := e.recordCalls("conversations.history")
	e.a.Navigate("c/" + ch + "/m/" + target)
	if between != nil {
		between()
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		rows := e.paneRows("", ch, "")
		if drewRow(rows, newest.TS) {
			t.Fatalf("the newest page was drawn first: %s", drawnState(rows))
		}
		if lit(rows, target) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the old message never drew highlighted: %s", drawnState(rows))
		}
		e.frame()
		time.Sleep(time.Millisecond)
	}
	got := calls.of("conversations.history", ch, "latest", "oldest", "inclusive", "limit")
	slices.Sort(got)
	want := []string{
		"latest= oldest=" + target + " inclusive=true limit=28",
		"latest=" + target + " oldest= inclusive=true limit=28",
	}
	if !slices.Equal(got, want) {
		t.Errorf("conversations.history for the jump: %q\nwant only the window around the message: %q", got, want)
	}
}

// TestHandOverHighlightsRootAfterWindow: a slack://…thread_ts=<reply>
// link for a reply far back has the conversation load the window around
// the reply's time first (highlightRoot with the reply's ts, a message the
// channel never holds, so that jump gives up); once the pane learns the
// reply's thread (conversations.replies, answered after the window) and
// hands on, the conversation highlights the thread's root. Clack lost the
// root highlight when the window landed first.
func TestHandOverHighlightsRootAfterWindow(t *testing.T) {
	e := longEnv(t)
	ch := longRead
	root := plainAt(t, e.fs, ch, 2500)
	sec, _, _ := strings.Cut(root, ".")
	n, _ := strconv.ParseInt(sec, 10, 64)
	reply := strconv.FormatInt(n+1, 10) + ".000700" // in the thread only, never the channel's history
	who := e.fs.UserIDs()[2]
	newest, _ := e.fs.HistoryAt(ch, 0)
	e.openChannel(ch)
	e.waitDrawn(t, "the newest messages", 20*time.Second, "", ch, "", func(rows []messages.SenderView) bool { return drewRow(rows, newest.TS) })
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "conversations.replies" || f.Get("channel") != ch || f.Get("ts") != reply && f.Get("ts") != root {
			return nil, false
		}
		time.Sleep(1500 * time.Millisecond) // after the window's two calls
		return map[string]any{"ok": true, "has_more": false, "messages": []any{
			map[string]any{"type": "message", "ts": root, "thread_ts": root, "user": who, "text": "synthetic old root", "reply_count": 1,
				"latest_reply": reply, "reply_users": []string{who}},
			map[string]any{"type": "message", "ts": reply, "thread_ts": root, "user": who, "text": "synthetic old reply"}}}, true
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	e.a.OpenURL("slack://channel?team=" + fakeslack.TeamID + "&id=" + ch + "&thread_ts=" + reply)
	e.waitDrawn(t, "the reply's thread, at it", 20*time.Second, "", ch, root, func(rows []messages.SenderView) bool {
		return len(rows) == 2 && rows[0].TS == root && lit(rows, reply)
	})
	e.waitDrawn(t, "the root highlighted in the conversation", 15*time.Second, "", ch, "", func(rows []messages.SenderView) bool {
		return lit(rows, root)
	})
}

// TestJumpWindowKeepsReactionsAfterOtherJumps: a window a list shows
// stays core's while the list shows it, however many jumps elsewhere come
// after (Slack's slices stay until their own channel's
// truncateChannelHistory): a reaction on its message still shows. Core
// kept the 8 windows loaded last across every conversation, so nine old
// search results opened elsewhere dropped the shown window, and the
// socket's reactions (the viewer's own too) found no message to change.
func TestJumpWindowKeepsReactionsAfterOtherJumps(t *testing.T) {
	e := longEnv(t)
	ch := longRead
	target := plainAt(t, e.fs, ch, 2500)
	e.openAtOld(t, ch, target)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, other := range []string{"C0FAKE00001", "C0FAKE00003"} {
		for _, back := range []int{400, 800, 1200, 1600, 2000} { // windows far apart: one each
			if _, err := e.a.Core().HistoryAround(ctx, other, plainAt(t, e.fs, other, back)); err != nil {
				t.Fatalf("a window in %s: %v", other, err)
			}
		}
	}
	e.settle(200 * time.Millisecond)
	who := e.fs.UserIDs()[3]
	raw, _ := json.Marshal(map[string]any{"type": "reaction_added", "user": who, "reaction": "tada",
		"item": map[string]any{"type": "message", "channel": ch, "ts": target}, "event_ts": fmt.Sprintf("%d.000500", time.Now().Unix())})
	e.a.Core().Inject(slack.Event{Type: "reaction_added", Raw: raw})
	e.until("the reaction on the window's message after ten jumps elsewhere", 10*time.Second, func() bool {
		cv := e.channelView(ch)
		if cv == nil {
			return false
		}
		for _, r := range cv.ReactionRects() {
			if r.TS == target && r.Name == "tada" {
				return true
			}
		}
		return false
	})
}

// TestWindowNewerFailureDoesNotBlockOlder: a window's requestNewer that
// keeps failing backs off on its own (Slack's requestHistory limits each
// request: {oldest} and {latest} apart), and never holds up scrolling up
// in the window. Clack counted a newer failure as an older one, so each
// failed page at the window's end pushed the older page's retry out
// again, and asked for the newer page every second.
func TestWindowNewerFailureDoesNotBlockOlder(t *testing.T) {
	e := longEnv(t)
	ch := longRead
	target := plainAt(t, e.fs, ch, 2500)
	e.openAtOld(t, ch, target)
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "conversations.history" && f.Get("channel") == ch && f.Get("oldest") != "" && f.Get("latest") == "" {
			return map[string]any{"ok": false, "error": "synthetic_failure"}, true // every newer page fails
		}
		return nil, false
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	calls := e.recordCalls("conversations.history")
	newer := func() int { // newer pages asked so far
		k := 0
		for _, c := range calls.of("conversations.history", ch, "oldest", "latest") {
			if c != "oldest= latest=" && strings.HasSuffix(c, " latest=") {
				k++
			}
		}
		return k
	}
	older := func() int {
		k := 0
		for _, c := range calls.of("conversations.history", ch, "latest") {
			if c != "latest=" {
				k++
			}
		}
		return k
	}
	// to the window's end: its newer page fails, again and again
	for dl := time.Now().Add(15 * time.Second); newer() < 1; {
		if time.Now().After(dl) {
			t.Fatalf("no newer page asked at the window's end: %q", calls.of("conversations.history", ch, "latest", "oldest"))
		}
		e.scroll(e.listPoint(), 3000)
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
	e.settle(6 * time.Second) // the window's end in view all along
	if n := newer(); n > 4 {
		t.Errorf("%d newer pages asked in about 6 s with each one failing; want the retries to back off", n)
	}
	before := older()
	for dl := time.Now().Add(4 * time.Second); older() == before; {
		if time.Now().After(dl) {
			t.Fatalf("after the failed newer pages, scrolling up asked for no older page in 4 s")
		}
		e.scroll(e.listPoint(), -3000)
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
}
