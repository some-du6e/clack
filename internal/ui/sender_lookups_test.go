package ui

// Socket events about people the client never fetched, in conversations
// nothing on screen shows, ask users/info for no one (α.110's reviews).
// Core announces a message with its sender as the store has them: Slack's
// messageReceived asks for no one, and its rows ask as they draw. A typing
// record goes out with the name the store has, and the composer's typing
// line asks for its typers as it draws (MessagePaneInput maps them
// through getMemberById as it renders); core asked for a typer in any
// watched conversation, the last two threads opened among them even once
// their pane closed. Every such ask's users/info answer brought "names",
// and the UI read every conversation on screen again (a resnapshot, the
// open channel's history converted afresh, every row resolved again) for
// people nothing drew. Top-level messages, thread replies, and typing in
// a thread whose pane closed. (TestIdleFrames can't tell this apart: a
// message elsewhere moves that channel's latest, and a reply makes core
// refresh the counts, each a frame of its own.) Synthetic data only.

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

func TestSocketSendersAskedForNoOne(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	chs, ids := fs.ChannelIDs(), fs.UserIDs()
	ch, other := chs[0], chs[1]
	// a thread in the open channel, opened and closed below: still watched
	root, _ := fs.PostQuiet(ch, ids[2], "a synthetic thread root")
	fs.PostReplyQuiet(ch, root, ids[3], "a synthetic reply")
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	c := e.a.data.core
	c.SetRealtime(true)
	e.until("the socket", 20*time.Second, func() bool { return c.RTM != nil && c.RTM.Connected() })
	e.openChannel(ch)
	e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.a.OpenThread(ch, root)
	e.until("the thread pane", 20*time.Second, func() bool {
		tv := e.threadView(ch, root)
		if tv == nil {
			return false
		}
		_, _, ok := tv.RowText(root)
		return ok
	})
	e.a.CloseSecondary()
	e.settle(2 * time.Second)

	evs, stop := c.Subscribe()
	defer stop()
	var mu sync.Mutex
	named := map[string]bool{}
	announced := map[string]bool{}
	go func() {
		for ev := range evs {
			mu.Lock()
			switch ev.Type {
			case "names":
				for _, u := range ev.Users {
					named[u] = true
				}
			case "message":
				announced[ev.TS] = true
			case "typing":
				announced["typing "+ev.UserID] = true
			}
			mu.Unlock()
		}
	}()
	var senders, sent []string
	for i := 0; i < 8; i++ {
		u := fmt.Sprintf("U0NOSENDR%02d", i)
		senders = append(senders, u)
		ts := fmt.Sprintf("%d.%06d", time.Now().Unix(), 200000+i)
		sent = append(sent, ts)
		ev := map[string]any{"type": "message", "channel": other, "user": u, "ts": ts, "text": "a synthetic message nobody here reads"}
		if i%2 == 1 {
			ev["thread_ts"] = "1762500000.000100" // a reply in a thread nobody opened
		}
		fs.Push(ev)
		time.Sleep(100 * time.Millisecond)
	}
	// someone never fetched types in the thread whose pane closed
	const typer = "U0NOTYPER00"
	fs.Push(map[string]any{"type": "user_typing", "channel": ch, "thread_ts": root, "user": typer})
	e.settle(3 * time.Second) // the lazy fetcher asks within a round trip

	mu.Lock()
	defer mu.Unlock()
	for _, ts := range sent {
		if !announced[ts] {
			t.Fatalf("core never announced the message %s: the test checks nothing", ts)
		}
	}
	if !announced["typing "+typer] {
		t.Fatal("core never announced the typing: the test checks nothing")
	}
	for _, u := range append(senders, typer) {
		if named[u] || c.S.Nonexistent(u) {
			t.Errorf("%s, someone nothing on screen shows, was asked for: users/info answered, and its \"names\" made the UI read everything on screen again", u)
		}
	}
}
