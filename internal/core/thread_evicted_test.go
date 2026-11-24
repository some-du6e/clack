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
	"testing"
	"time"

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
		Conv:     func(string) (bool, bool) { return true, true },
		WholeOK:  func(string) bool { return false },
		LastRead: func(string) string { return fmt.Sprintf("%d.000100", time.Now().Unix()) },
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
