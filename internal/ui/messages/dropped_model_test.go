package messages

// A list keeps its model when the hub lets it go (40 newer
// conversations): out of the hub it takes none of core's events, its
// "evicted" ones among them. Core's message cache meanwhile resets a
// conversation left alone for an hour (store/evict.go): its window is the
// newest page again, the messages between it and what was there before
// only on the server. Taken back, the model merged that page into what it
// had: the list showed what Karim read an hour ago and then the newest
// page, the unread messages between them missing, and paging back
// started before the oldest of what he'd read, so they never loaded.
// Synthetic data on internal/fakeslack.

import (
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestDroppedModelTakesCoresWindow(t *testing.T) {
	app, fs, _ := shotsApp(t)
	h := coreHost{app: app, th: theme.New(false)}
	const ch = "C0GAPHIST1"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "busy", IsChannel: true, IsMember: true, NumMembers: 40})
	// the server's history: what Karim read, 20 he hasn't, the newest 10
	post := func(text string) slack.Message {
		ts, _ := fs.PostQuiet(ch, "UANA00001", text)
		return slack.Message{Type: "message", TS: ts, User: "UANA00001", Text: text}
	}
	var read, unread, newest []slack.Message
	for i := 0; i < 5; i++ {
		read = append(read, post(fmt.Sprintf("read %d", i)))
	}
	for i := 0; i < 20; i++ {
		unread = append(unread, post(fmt.Sprintf("unread %d", i)))
	}
	for i := 0; i < 10; i++ {
		newest = append(newest, post(fmt.Sprintf("newest %d", i)))
	}
	app.S.SetLatest(ch, read, false)
	v := NewChannelView(h, ch)
	frame := frameFn(v)
	until(t, 5*time.Second, "what Karim read", frame, func() bool { return itemHeight(v.s, read[4].TS) > 0 })

	// 40 other conversations: the hub lets the list's model go
	base := time.Now().Add(-time.Hour).Unix()
	for i := 0; i < maxModels; i++ {
		oc := fmt.Sprintf("C0GAPOTH%02d", i)
		ts := fmt.Sprintf("%d.000100", base+int64(i))
		app.S.PutConv(slack.Conversation{ID: oc, Name: fmt.Sprintf("other-%d", i), IsChannel: true, IsMember: true, NumMembers: 5})
		app.S.SetLatest(oc, []slack.Message{{Type: "message", TS: ts, User: "UANA00001", Text: "hello " + oc}}, false)
		ov := NewChannelView(h, oc)
		until(t, 5*time.Second, oc+" drawn", frameFn(ov), func() bool { return itemHeight(ov.s, ts) > 0 })
	}
	hb := v.s.hb
	hb.mu.Lock()
	held := hb.chans[ch] == v.s.m
	hb.mu.Unlock()
	if held {
		t.Fatal("the hub still keeps the list's model after 40 other conversations")
	}

	// core resets the conversation meanwhile: its window is the newest
	// page, the unread messages before it on the server only
	app.S.SetLatest(ch, newest, true)

	// Karim opens it again
	until(t, 5*time.Second, "the newest messages", frame, func() bool { return itemHeight(v.s, newest[9].TS) > 0 })
	for i := 0; i < 20; i++ {
		frame()
		time.Sleep(5 * time.Millisecond)
	}
	// it draws no gap: what Karim read only with the unread messages after
	// it (the list may have paged back on its own to fill the pane, from
	// the newest page down: what it pages in is whole)
	if r, u := listHas(v, read), listHas(v, unread); r > 0 && u < len(unread) {
		t.Errorf("the list draws %d of the messages Karim read and %d of the %d unread ones after them: a gap above the newest page", r, u, len(unread))
	}
	// scrolled up, it pages back and draws every unread message, in order
	after := append(append([]slack.Message(nil), unread...), newest...)
	scrollUp(v, frame, after, 5*time.Second)
	drawnInOrder(t, v, frame, after)
}
