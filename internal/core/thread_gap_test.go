package core

import (
	"context"
	"testing"
	"time"
)

// TestSocketGapThreadAtItsEnd: a thread read to its end goes stale at the
// slow connect in the same reset as the channels' windows (Slack's
// onConnected resetReachedEnd({}), every key); the socket's reply after
// the connect makes a slice of its own, and the pane's read past the old
// end (loadPost) joins it and the reply posted while the socket was away.
// Synthetic data only.
func TestSocketGapThreadAtItsEnd(t *testing.T) {
	e := newGapEnv(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := e.live("a thread's root")
	e.fs.PostReplyQuiet(e.ch, root, e.user, "a synthetic reply")
	sl, err := e.a.ThreadAt(ctx, e.ch, root, root)
	if err != nil {
		t.Fatal(err)
	}
	if !sl.ReachedEnd || len(sl.Msgs) != 2 {
		t.Fatalf("the open thread: %d messages, reachedEnd %v", len(sl.Msgs), sl.ReachedEnd)
	}
	ups := e.ups()
	e.fs.DropSockets()
	e.waitDown(1)
	away := e.fs.PostReplyQuiet(e.ch, root, e.user, "a reply while the socket was away")
	e.waitUp(ups + 1)
	// core resets before it tells anyone of the connection (waitUp saw
	// it); the open channel's own catch-up may be done already
	sl = e.a.ThreadSliceAt(e.ch, root, root)
	if sl.ReachedEnd || !sl.PrevReachedEnd {
		t.Fatalf("after the slow connect: reachedEnd %v prevReachedEnd %v; want the thread's end stale with the channel's", sl.ReachedEnd, sl.PrevReachedEnd)
	}
	// the socket's reply after the connect
	live := e.fs.PostReplyQuiet(e.ch, root, e.user, "a reply after the connect")
	e.fs.Push(map[string]any{"type": "message", "channel": e.ch, "user": e.user, "text": "a reply after the connect", "ts": live,
		"event_ts": live, "thread_ts": root, "client_msg_id": "gap-" + live})
	dl := time.Now().Add(10 * time.Second)
	for {
		th, _ := e.a.S.Thread(e.ch, root)
		if findTS(th, live) != nil {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("the store never took the socket's reply")
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, m := range e.a.ThreadSliceAt(e.ch, root, root).Msgs {
		if m.TS == live || m.TS == away {
			t.Fatalf("reply %s is in the pane's slice before its read past the stale end", m.TS)
		}
	}
	// the pane at its end reads forward from the old end
	if err := e.a.ThreadNewer(ctx, e.ch, root, sl.End); err != nil {
		t.Fatal(err)
	}
	sl = e.a.ThreadSliceAt(e.ch, root, root)
	got := map[string]bool{}
	for _, m := range sl.Msgs {
		got[m.TS] = true
	}
	if !sl.ReachedEnd || !got[away] || !got[live] {
		t.Errorf("after the read: reachedEnd %v, the gap's reply %v, the socket's %v; want both joined at the end", sl.ReachedEnd, got[away], got[live])
	}
}
