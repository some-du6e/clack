package core

// Karim's #help thread (2026-11-24 22:31Z): a thread read once, a bot's
// two "Only visible to you" replies over the socket, then the message
// cache evicted the thread's root and replies (never its ephemerals). The
// pane opened on it again drew the ephemerals alone. Slack's thread
// history keeps the evicted timestamps and the pane fetches what the
// store misses by id (getMessageByTimestamp's fetchObjectWithId,
// messages.list). Synthetic data on internal/fakeslack.

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func TestThreadAfterEvictionLikeSlack(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[1]
	ctx := context.Background()
	base := time.Now().Add(-26 * time.Hour).Unix()
	at := func(i int) string { return fmt.Sprintf("%d.000100", base+int64(i)) }
	root := at(0)
	fs.PostAtQuiet(ch, root, "U0FAKE00001", "synthetic question")
	for i := 1; i <= 4; i++ {
		fs.ReplyAtQuiet(ch, root, at(i), "U0FAKE00002", fmt.Sprintf("synthetic answer %d", i))
	}
	sl, err := a.ThreadAt(ctx, ch, root, root)
	if err != nil || len(sl.Msgs) < 5 {
		t.Fatalf("control: the thread's first read: %d messages (%v)", len(sl.Msgs), err)
	}
	// the bot's ephemerals, over the socket, in the thread
	for i := 5; i <= 6; i++ {
		fs.Push(map[string]any{"type": "message", "channel": ch, "ts": at(i), "thread_ts": root, "user": "U0FAKE00002",
			"text": "synthetic only visible to you", "is_ephemeral": true})
	}
	waitFor(t, "the ephemerals in the thread", 5*time.Second, func() bool {
		th, _ := a.S.Thread(ch, root)
		n := 0
		for i := range th {
			if th[i].IsEphemeral {
				n++
			}
		}
		return n == 2
	})
	// the message cache's budget: a channel's worth of read messages over it
	big := make([]slack.Message, 3500)
	for i := range big {
		big[i] = slack.Message{TS: fmt.Sprintf("%d.000100", base+3600+int64(i)), User: "U0FAKE00001", Text: "synthetic filler"}
	}
	a.S.SetLatest("C0FILLER01", big, true)
	out := a.S.EvictMessages(store.EvictInput{Now: time.Now(), Accessed: map[string]time.Time{}, Keep: map[string]bool{}, Pending: map[string]bool{},
		Conv:    func(string) (bool, bool) { return true, true },
		WholeOK: func(string) bool { return false },
		// the thread's channel's window isn't read: it keeps its messages
		// (a timeline that loses every one starts over with its threads)
		LastRead: func(c string) string {
			if c == ch {
				return "0000000000.000000"
			}
			return fmt.Sprintf("%d.000100", time.Now().Unix())
		},
	})
	if out == nil {
		t.Fatal("control: nothing was evicted")
	}
	if th, _ := a.S.Thread(ch, root); len(th) != 2 {
		t.Fatalf("control: the evicted thread's cache holds %d messages, want its two ephemerals", len(th))
	}
	lists := fs.CallCount("messages.list")
	sl, err = a.ThreadAt(ctx, ch, root, root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string // threadMsgs' order: the root first, then the slice's replies
	for _, m := range threadMsgs(sl, root) {
		got = append(got, m.TS)
	}
	want := []string{root, at(1), at(2), at(3), at(4), at(5), at(6)}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("the pane opened again draws %v\nwant the root, the 4 replies and the 2 ephemerals in time order %v", got, want)
	}
	if fs.CallCount("messages.list") == lists {
		t.Error("the evicted replies weren't fetched by id (messages.list)")
	}
}

// The ephemerals arriving while the thread's first replies read is out:
// the root, the replies and the ephemerals all draw, in time order.
func TestThreadEphemeralsDuringLoadLikeSlack(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[1]
	ctx := context.Background()
	base := time.Now().Add(-2 * time.Hour).Unix()
	at := func(i int) string { return fmt.Sprintf("%d.000100", base+int64(i)) }
	root := at(0)
	fs.PostAtQuiet(ch, root, "U0FAKE00001", "synthetic question")
	for i := 1; i <= 4; i++ {
		fs.ReplyAtQuiet(ch, root, at(i), "U0FAKE00002", fmt.Sprintf("synthetic answer %d", i))
	}
	fs.SetFault("conversations.replies", fakeslack.Fault{Delay: time.Second, Times: 1})
	n := fs.CallCount("conversations.replies")
	done := make(chan ThreadSlice, 1)
	go func() {
		sl, _ := a.ThreadAt(ctx, ch, root, root)
		done <- sl
	}()
	waitFor(t, "the replies read out", 5*time.Second, func() bool { return fs.CallCount("conversations.replies") > n })
	for i := 5; i <= 6; i++ {
		fs.Push(map[string]any{"type": "message", "channel": ch, "ts": at(i), "thread_ts": root, "user": "U0FAKE00002",
			"text": "synthetic only visible to you", "is_ephemeral": true})
	}
	var sl ThreadSlice
	select {
	case sl = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the thread's read never ended")
	}
	waitFor(t, "the ephemerals in the slice", 5*time.Second, func() bool {
		sl = a.ThreadSliceAt(ch, root, root)
		return len(threadMsgs(sl, root)) == 7
	})
	var got []string
	for _, m := range threadMsgs(sl, root) {
		got = append(got, m.TS)
	}
	if want := []string{root, at(1), at(2), at(3), at(4), at(5), at(6)}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("the pane draws %v\nwant %v", got, want)
	}
}

// evictedThread is a thread of n replies (text of size bytes each) on the
// fake, read once, one ephemeral of the bot's held, then evicted by the
// message cache: its channel's window keeps the root (unread), the
// thread's cache the ephemeral alone, its slice every timestamp.
func evictedThread(t *testing.T, a *App, fs *fakeslack.Server, n, size int) (ch, root string, at func(int) string) {
	t.Helper()
	ch = fs.ChannelIDs()[1]
	base := time.Now().Add(-26 * time.Hour).Unix()
	at = func(i int) string { return fmt.Sprintf("%d.%06d", base+int64(i/1000), 100+i%1000) }
	root = at(0)
	fs.PostAtQuiet(ch, root, "U0FAKE00001", "synthetic question")
	text := strings.Repeat("synthetic answer ", max(1, size/17))
	rootMsg := slack.Message{Type: "message", TS: root, ThreadTS: root, User: "U0FAKE00001", Text: "synthetic question", ReplyCount: n}
	msgs := []slack.Message{rootMsg}
	for i := 1; i <= n; i++ {
		fs.ReplyAtQuiet(ch, root, at(i), "U0FAKE00002", text)
		msgs = append(msgs, slack.Message{Type: "message", TS: at(i), ThreadTS: root, User: "U0FAKE00002", Text: text})
	}
	// the whole thread read once (its history one slice, both ends), and
	// the channel's window with the root
	a.S.SetThread(ch, root, msgs)
	a.S.SetLatest(ch, []slack.Message{rootMsg}, false)
	a.S.AddReply(ch, slack.Message{TS: at(n + 1), ThreadTS: root, User: "U0FAKE00002", Text: "synthetic only visible to you", IsEphemeral: true})
	big := make([]slack.Message, 3500)
	for i := range big {
		big[i] = slack.Message{TS: fmt.Sprintf("%d.000100", base+3600+int64(i)), User: "U0FAKE00001", Text: "synthetic filler"}
	}
	a.S.SetLatest("C0FILLER01", big, true)
	a.S.EvictMessages(store.EvictInput{Now: time.Now(), Accessed: map[string]time.Time{}, Keep: map[string]bool{}, Pending: map[string]bool{},
		Conv:    func(string) (bool, bool) { return true, true },
		WholeOK: func(string) bool { return false },
		LastRead: func(c string) string {
			if c == ch {
				return "0000000000.000000"
			}
			return fmt.Sprintf("%d.000100", time.Now().Unix())
		},
	})
	if th, _ := a.S.Thread(ch, root); len(th) != 1 || !th[0].IsEphemeral {
		t.Fatalf("control: the evicted thread's cache holds %d messages, want its ephemeral", len(th))
	}
	return ch, root, at
}

func sliceHas(a *App, ch, root, ts string) bool {
	sl, _ := a.S.ThreadSliceAt(ch, root, root)
	return slices.Contains(sl.TSs, ts)
}

// A reply deleted while its thread is evicted leaves the thread's history
// (setMessageDeleted passes threadTs on), and one the server no longer has
// (deleted unheard) leaves it at the first answer that lacks it (Slack's
// non-existent object): neither is asked for again. Two panes opening at
// once share one fetch.
func TestThreadEvictedDeletesLikeSlack(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch, root, at := evictedThread(t, a, fs, 4, 20)
	ctx := context.Background()
	// r2 deleted while evicted, with the event
	fs.DeleteReplyQuiet(ch, root, at(2))
	pushDeleteReply(fs, ch, slack.Message{TS: at(2), ThreadTS: root, User: "U0FAKE00002", Text: "synthetic answer"},
		map[string]any{"type": "message", "ts": root, "thread_ts": root, "user": "U0FAKE00001", "text": "synthetic question", "reply_count": 3})
	waitFor(t, "the deleted reply out of the thread's history", 5*time.Second, func() bool { return !sliceHas(a, ch, root, at(2)) })
	// r3 deleted on the server unheard
	fs.DeleteReplyQuiet(ch, root, at(3))
	fs.SetFault("messages.list", fakeslack.Fault{Delay: 300 * time.Millisecond, Times: 1})
	lists := fs.CallCount("messages.list")
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ { // two panes at once: one fetch
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = a.ThreadAt(ctx, ch, root, root) }()
	}
	wg.Wait()
	if n := fs.CallCount("messages.list") - lists; n != 1 {
		t.Errorf("two ThreadAt at once sent %d messages.list, want one", n)
	}
	if sliceHas(a, ch, root, at(3)) {
		t.Error("the reply the answer lacked is still in the thread's history")
	}
	lists = fs.CallCount("messages.list")
	if _, err := a.ThreadAt(ctx, ch, root, root); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if n := fs.CallCount("messages.list") - lists; n != 0 {
		t.Errorf("a reopen asked messages.list %d more times for replies that are gone", n)
	}
	var got []string
	for _, m := range threadMsgs(a.ThreadSliceAt(ch, root, root), root) {
		got = append(got, m.TS)
	}
	if want := []string{root, at(1), at(4), at(5)}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("the pane draws %v, want %v", got, want)
	}
}

// A big evicted thread's replies are fetched into the thread's own cache
// a page at a time, not the fetched-by-id cache the other views share
// (2,048 messages, 4 MB): a 5 MB thread no longer flushes Activity's
// copies, nor its own first replies.
func TestThreadEvictedBigFetchLikeSlack(t *testing.T) {
	a, fs, _ := rtApp(t)
	// an Activity row's copy, fetched by id
	other := fs.ChannelIDs()[2]
	ots := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	a.S.PutFetched(other, []slack.Message{{TS: ots, User: "U0FAKE00001", Text: "synthetic activity row"}}, a.S.LiveSeq())
	const n = 250
	ch, root, at := evictedThread(t, a, fs, n, 20000) // 250 × 20 KB
	lists := fs.CallCount("messages.list")
	if _, err := a.ThreadAt(context.Background(), ch, root, root); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "every reply back in the thread's cache", 20*time.Second, func() bool {
		th, _ := a.S.Thread(ch, root)
		return len(th) >= n+1
	})
	if calls := fs.CallCount("messages.list") - lists; calls < n/threadFillPage {
		t.Errorf("%d replies came in %d messages.list calls, want pages of %d", n, calls, threadFillPage)
	}
	if has := a.S.FetchedHas(other, []string{ots}); !has[0] {
		t.Error("the big thread's fetch flushed another view's fetched copy")
	}
	if has := a.S.FetchedHas(ch, []string{at(1), at(n)}); has[0] || has[1] {
		t.Error("the thread's replies went into the shared fetched-by-id cache")
	}
}
