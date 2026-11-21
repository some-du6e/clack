package ui

// Reactions on a jump window's messages draw after the list left the
// conversation for old messages elsewhere and came back to the window
// (edit-latency's reactions round; thread-open-root's review: core kept
// the 8 windows loaded last across every conversation, a list's reads
// never counted as an access, and once core dropped the window the
// socket's reactions, the viewer's own too, found no message to change).
// The review's sequence: jump to an old message in a read channel, open
// old messages in other conversations (ten windows, as old search
// results), go back to the channel (read, so its window is kept), react.
// Someone's reaction over the socket, then the viewer's own through
// reactions.add and its echo. Synthetic data only.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/messages"
)

func TestJumpWindowReactionsAfterLeaving(t *testing.T) {
	e := longEnv(t)
	c := e.a.Core()
	c.SetRealtime(true)
	e.until("the socket", 20*time.Second, func() bool { return c.RTM != nil && c.RTM.Connected() })
	ch := longRead
	target := plainAt(t, e.fs, ch, 2500)
	e.openAtOld(t, ch, target)

	// old messages in the other long conversations, far apart: a window each
	for _, other := range []string{"C0FAKE00001", "C0FAKE00003"} {
		for _, back := range []int{400, 800, 1200, 1600, 2000} {
			ts := plainAt(t, e.fs, other, back)
			e.a.Navigate("c/" + other + "/m/" + ts)
			e.waitDrawn(t, fmt.Sprintf("%s's old message %d back", other, back), 30*time.Second, "", other, "",
				func(rows []messages.SenderView) bool { return lit(rows, ts) })
		}
	}
	// back to the channel: the window it kept
	e.openChannel(ch)
	e.waitDrawn(t, "the channel's window again", 20*time.Second, "", ch, "",
		func(rows []messages.SenderView) bool { return drewRow(rows, target) })

	shows := func(name string) bool {
		cv := e.channelView(ch)
		if cv == nil {
			return false
		}
		for _, r := range cv.ReactionRects() {
			if r.TS == target && r.Name == name {
				return true
			}
		}
		return false
	}
	who := e.fs.UserIDs()[3]
	e.fs.Push(map[string]any{"type": "reaction_added", "user": who, "reaction": "tada", "item_user": who,
		"item": map[string]any{"type": "message", "channel": ch, "ts": target}, "event_ts": fmt.Sprintf("%d.000500", time.Now().Unix())})
	e.until("someone's reaction on the window's message", 10*time.Second, func() bool { return shows("tada") })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.React(ctx, ch, target, "eyes", true); err != nil {
		t.Fatal(err)
	}
	e.until("the viewer's own reaction on the window's message", 10*time.Second, func() bool { return shows("eyes") })
}
