package ui

// After a socket gap the open channel's list draws every message, like
// Slack's (core/reached_end_test.go has the window's side): Karim's
// #clack on α.112 (2026-11-20) drew 6:54 PM and then 9:53 PM, with three
// hours of messages between missing and nothing to say so. The socket
// had come back on a slow connect, the list reloaded core's cached window,
// and the socket's next message went right after it. Slack resets every
// channel's reachedEnd at a slow connect (boot-shared erkq, client-boot
// 6Yed) and the open view reads newer messages from its end (AEjl
// requestNewer). Here the socket drops, messages arrive in the open
// channel and another, the socket comes back and one more message comes
// live: the list must hold every message Slack has from its oldest to the
// newest, and the other channel's row must be unread. Read back to an old
// message first (a jump pages back until it holds it), the list keeps it
// too. Synthetic data only.

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

func TestSocketGapListDrawsEveryMessage(t *testing.T) {
	t.Run("at the newest", func(t *testing.T) { socketGapList(t, gapAtNewest) })
	t.Run("read back to an old message", func(t *testing.T) { socketGapList(t, gapJump) })
	t.Run("own send during the newest read", func(t *testing.T) { socketGapList(t, gapOwnSend) })
}

type gapMode int

const (
	gapAtNewest gapMode = iota
	gapJump
	// more than a page missed (the window starts over at the newest
	// page), and the viewer's own send goes out and is confirmed while
	// that page is out: the list keeps it (the review's blocker: core's
	// new window dropped it, and the list with it)
	gapOwnSend
)

func socketGapList(t *testing.T, mode gapMode) {
	jump := mode == gapJump
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.CountsFollowPosts(true) // client.counts sees what the test posts, as Slack's does
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	c := e.a.data.core
	ids := fs.ChannelIDs()
	ch, other := ids[2], ""
	for _, id := range ids[3:] {
		if cv := c.Count(id); !cv.Unread && cv.Mentions == 0 {
			other = id
			break
		}
	}
	if other == "" {
		t.Fatal("no read channel to post into")
	}
	e.openChannel(ch)
	e.until("the channel's history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.toLatest()
	c.SetRealtime(true)
	e.until("the socket", 20*time.Second, func() bool { return c.RTM != nil && c.RTM.Connected() && fs.Sockets() > 0 })
	v := e.ownSendList(ch)
	holds := func(ts string) bool {
		for _, x := range v.LoadedTS() {
			if x == ts {
				return true
			}
		}
		return false
	}
	user, n := fs.UserIDs()[4], 0
	post := func(in, what string) string {
		n++
		ts, ev := fs.PostQuiet(in, user, fmt.Sprintf("synthetic message %d: %s", n, what))
		fs.Push(ev)
		return ts
	}
	var target string
	if jump {
		target = fs.HistoryTS(ch)[2]
		v.Highlight(target)
		e.until("the list paged back to the message jumped to", 30*time.Second, func() bool { return holds(target) && v.JumpPending() == "" })
	}
	before := post(ch, "before the gap")
	e.until("the message before the gap", 10*time.Second, func() bool { return holds(before) })

	fs.DropSockets()
	e.until("the client to see its socket go", 10*time.Second, func() bool { return !c.RTM.Connected() })
	var must []string
	gaps := 6
	if mode == gapOwnSend {
		gaps = 60
	}
	for i := 0; i < gaps; i++ {
		ts := post(ch, "while the socket was away")
		if i >= gaps-6 {
			must = append(must, ts)
		}
	}
	post(other, "in another channel while the socket was away")
	var release func()
	held := make(chan struct{})
	if mode == gapOwnSend {
		// hold the catch-up's newest-page read (no oldest, no latest)
		var once, rel sync.Once
		out := make(chan struct{})
		release = func() { rel.Do(func() { close(out) }) }
		fs.SetHook(func(method string, form url.Values) (any, bool) {
			if method == "conversations.history" && form.Get("channel") == ch && form.Get("oldest") == "" && form.Get("latest") == "" {
				once.Do(func() { close(held); <-out })
			}
			return nil, false
		})
		t.Cleanup(func() { release(); fs.SetHook(nil) })
	}
	e.until("the socket back", 20*time.Second, func() bool { return c.RTM.Connected() && fs.Sockets() > 0 })
	if mode == gapOwnSend {
		e.until("the catch-up's newest read", 15*time.Second, func() bool {
			select {
			case <-held:
				return true
			default:
				return false
			}
		})
		m, err := c.Send(context.Background(), core.SendOpts{Channel: ch, Text: "synthetic: mine, sent while the newest page was out"})
		if err != nil {
			t.Fatal(err)
		}
		e.until("the list drawing the send", 10*time.Second, func() bool { return holds(m.TS) })
		release()
		must = append(must, m.TS)
	}
	must = append(must, post(ch, "after the reconnect"))
	if target != "" {
		must = append(must, target)
	}

	missing := func() (miss []string) {
		have := map[string]bool{}
		lt := v.LoadedTS()
		for _, ts := range lt {
			have[ts] = true
		}
		all := fs.HistoryTS(ch)
		from := len(all)
		for i, ts := range all {
			if have[ts] {
				from = i
				break
			}
		}
		seen := map[string]bool{}
		for _, ts := range append(append([]string(nil), all[from:]...), must...) {
			if !have[ts] && !seen[ts] {
				seen[ts] = true
				miss = append(miss, ts)
			}
		}
		return miss
	}
	dl := time.Now().Add(20 * time.Second)
	for {
		miss := missing()
		if len(miss) == 0 {
			break
		}
		if time.Now().After(dl) {
			t.Fatalf("after the gap the list (%d messages) lacks %d of Slack's messages from its oldest to the newest: %s",
				len(v.LoadedTS()), len(miss), strings.Join(miss, " "))
		}
		e.frame()
		time.Sleep(2 * time.Millisecond)
	}
	e.until("the other channel unread", 15*time.Second, func() bool {
		cv := e.a.model().conv(other)
		return cv != nil && cv.Unread
	})
}
