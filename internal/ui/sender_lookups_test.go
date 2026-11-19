package ui

// A socket message from someone the client never fetched, in a channel
// nothing on screen shows, asks users/info for no one (α.110's review).
// Core announces it with the sender as the store has them: Slack's
// messageReceived asks for no one, and its rows ask as they draw. Read
// through getMemberById, every such sender went to users/info, and the
// answer's "names" made the UI read every conversation on screen again (a
// resnapshot, the open channel's history converted afresh, every row
// resolved again) for people nothing drew. Top-level messages and thread
// replies. (TestIdleFrames can't tell this apart: a message elsewhere
// moves that channel's latest, and a reply makes core refresh the counts,
// each a frame of its own.) Synthetic data only.

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

func TestSocketSendersAskedForNoOne(t *testing.T) {
	if os.Getenv("CLACK_LATENCY_TESTS") == "" {
		t.Skip("CLACK_LATENCY_TESTS not set: flake-check's sender-lookups job runs this")
	}
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	c := e.a.data.core
	c.SetRealtime(true)
	e.until("the socket", 20*time.Second, func() bool { return c.RTM != nil && c.RTM.Connected() })
	chs := fs.ChannelIDs()
	ch, other := chs[0], chs[1]
	e.openChannel(ch)
	e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.settle(2 * time.Second)

	evs, stop := c.Subscribe()
	defer stop()
	var mu sync.Mutex
	named := map[string]bool{}
	go func() {
		for ev := range evs {
			if ev.Type == "names" {
				mu.Lock()
				for _, u := range ev.Users {
					named[u] = true
				}
				mu.Unlock()
			}
		}
	}()
	var senders []string
	for i := 0; i < 8; i++ {
		u := fmt.Sprintf("U0NOSENDR%02d", i)
		senders = append(senders, u)
		ts := fmt.Sprintf("%d.%06d", time.Now().Unix(), 200000+i)
		ev := map[string]any{"type": "message", "channel": other, "user": u, "ts": ts, "text": "a synthetic message nobody here reads"}
		if i%2 == 1 {
			ev["thread_ts"] = "1762500000.000100" // a reply in a thread nobody opened
		}
		fs.Push(ev)
		time.Sleep(100 * time.Millisecond)
	}
	e.settle(3 * time.Second) // the lazy fetcher asks within a round trip

	mu.Lock()
	defer mu.Unlock()
	for _, u := range senders {
		if named[u] || c.S.Nonexistent(u) {
			t.Errorf("%s, a sender nothing on screen shows, was asked for: users/info answered, and its \"names\" made the UI read everything on screen again", u)
		}
	}
}
