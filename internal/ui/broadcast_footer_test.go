package ui

import (
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// newerRects are the open channel's "View newer replies" links by
// broadcast ts, as drawn last frame.
func (e *perfEnv) newerRects(ch string) map[string]image.Rectangle {
	out := map[string]image.Rectangle{}
	cv := e.a.convos[ch+"/"]
	if cv == nil {
		return out
	}
	l, ok := cv.list.(channelList)
	if !ok {
		return out
	}
	for _, r := range l.NewerRepliesRects() {
		out[r.TS] = r.R
	}
	return out
}

// Slack's ThreadBroadcastFooter: a broadcast whose thread went on after
// it reads "View newer replies" under it; one that is still the thread's
// latest reply doesn't, until a newer reply comes over the socket
// (message_replied). A click opens the thread at the broadcast.
func TestBroadcastNewerReplies(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ids := fs.UserIDs()
	ch := fs.ChannelIDs()[0]
	root1, _ := fs.PostQuiet(ch, ids[1], "first thread")
	b1 := fs.PostBroadcastQuiet(ch, root1, ids[2], "broadcast one")
	root2, _ := fs.PostQuiet(ch, ids[1], "second thread")
	b2 := fs.PostBroadcastQuiet(ch, root2, ids[2], "broadcast two")
	fs.PostReplyQuiet(ch, root1, ids[3], "after the first broadcast")

	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	go e.a.Core().SetRealtime(true)
	e.until("the socket", 15*time.Second, func() bool { return e.a.data.m.Connected && fs.Sockets() > 0 })
	e.openChannel(ch)
	e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.toLatest()
	e.until("b1's footer", 10*time.Second, func() bool { _, ok := e.newerRects(ch)[b1]; return ok })
	e.settle(500 * time.Millisecond)
	rs := e.newerRects(ch)
	if _, ok := rs[b2]; ok {
		t.Errorf("the thread's latest reply shows View newer replies")
	}
	r1 := rs[b1]
	if r1.Dy() <= 0 || r1.Dx() <= 0 {
		t.Fatalf("b1's footer box %v", r1)
	}

	// live: a reply after b2 arrives over the socket
	fs.PostReplyQuiet(ch, root2, ids[3], "after the second broadcast")
	if !fs.PushMessageReplied(ch, root2) {
		t.Fatal("no root2 to push")
	}
	e.until("b2's footer", 10*time.Second, func() bool { _, ok := e.newerRects(ch)[b2]; return ok })

	// a click opens the thread at the broadcast
	e.settle(300 * time.Millisecond)
	r1 = e.newerRects(ch)[b1]
	e.click(r1.Min.Add(image.Pt(r1.Dx()/2, r1.Dy()/2)))
	e.frame()
	e.frame()
	e.until("the thread", 10*time.Second, func() bool {
		s := e.a.secondary
		return s.kind == secThread && s.channel == ch && s.ts == root1
	})
	if at := e.a.secondary.at; at != b1 {
		t.Errorf("the thread opened at %q, want the broadcast %s", at, b1)
	}
}
