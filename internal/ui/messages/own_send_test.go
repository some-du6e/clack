package messages

// Your own new message (W43). Slack's sendMessage thunk (client-boot
// n6uE) dispatches jumpToTimestamp to a top-level message's pending copy
// wherever the pane was, and BaseMessageList anchors it ANCHOR_OFFSET
// (64px) under the list's top within the content: a message that fits
// sits at the bottom, a taller one shows its top 64px down
// (placeSendJump). A message with files goes through sendDraftFiles,
// which doesn't jump. The thread pane's reply box is its list's last row,
// so a reply goes out with the list at its end (ThreadView.JumpToOwnSend).
// Someone else's message moves neither list while you read back, and
// with the newest messages loaded (reachedEnd) there is no "Latest
// messages" pill; at the bottom the list follows them (stickToBottom).

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// ownSendEnv is fakeslack's long channel in a 900 x 700 list, loaded.
func ownSendEnv(t *testing.T) (*flingEnv, *core.App, string) {
	t.Helper()
	const ch = "C0FAKE00001"
	e := newFlingEnv(t, ch, 1)
	until(t, 10*time.Second, "the channel", e.frame, func() bool { return e.v.s.snap.loaded && len(e.v.s.items) > 3 })
	for i := 0; i < 5; i++ {
		e.frame()
	}
	return e, e.v.h.Core(), ch
}

// readBack wheels the list up through the history until the newest
// message is well off screen, then lets the scroll settle.
func (e *flingEnv) readBack(t *testing.T) {
	t.Helper()
	for i := 0; i < 3; i++ {
		e.wheel(6, -250)
		e.frame()
	}
	key, y, still := "", 0, 0
	for i := 0; i < 200 && still < 5; i++ {
		e.frame()
		if k, ky := e.firstMsg(); k == key && ky == y {
			still++
		} else {
			key, y, still = k, ky, 0
		}
	}
	if !e.v.s.list.Position.BeforeEnd {
		t.Fatal("the wheel didn't scroll the list up")
	}
	if _, ok := e.v.NewestRowBottom(); ok {
		t.Fatal("the newest message is still on screen")
	}
}

// ownSendTS is a ts after everything the list holds.
func ownSendTS(s *listState) string {
	now := time.Now()
	ts := fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)
	if n := s.newestTS(); n != "" && !tsLess(n, ts) {
		sec, _, _ := strings.Cut(n, ".")
		v, _ := strconv.ParseInt(sec, 10, 64)
		ts = fmt.Sprintf("%d.%06d", v+1, 100)
	}
	return ts
}

// ownSendInject has core apply someone else's message as the socket
// would bring it (threadTS "": a channel message).
func ownSendInject(t *testing.T, app *core.App, ch, threadTS, ts, text string) {
	t.Helper()
	m := map[string]any{"type": "message", "channel": ch, "user": "U0FAKE00007", "text": text, "ts": ts, "event_ts": ts}
	if threadTS != "" {
		m["thread_ts"] = threadTS
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	app.Inject(slack.Event{Type: "message", Raw: raw, Channel: json.RawMessage(strconv.Quote(ch))})
}

// newestCID is the client_msg_id of the newest message in the list.
func newestCID(s *listState) string {
	if n := s.newestItem(); n >= 0 && s.items[n].idx < len(s.snap.msgs) {
		return s.snap.msgs[s.items[n].idx].m.ClientMsgID
	}
	return ""
}

// ownSend sends text as the viewer under client_msg_id cid (core.Send
// blocks until the fake answers); the error arrives on the channel.
func ownSend(app *core.App, o core.SendOpts) <-chan error {
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := app.Send(ctx, o)
		done <- err
	}()
	return done
}

// Sending from a channel read back in its history: the list jumps to the
// message and stays on it once Slack confirms it; a message with files
// doesn't move it.
func TestOwnSendJumpsToTheMessage(t *testing.T) {
	e, app, ch := ownSendEnv(t)
	e.readBack(t)
	s := e.v.s

	key, y := e.firstMsg()      // what's on screen: a page landing above keeps it there
	e.v.JumpToOwnSend("", true) // sendDraftFiles: no jumpToTimestamp
	e.frame()
	e.frame()
	if k, ky := e.firstMsg(); k != key || ky != y || !s.list.Position.BeforeEnd || s.sendCID != "" {
		t.Errorf("a message with files moved the list: %s at %d, before %s at %d (armed %q)", k, ky, key, y, s.sendCID)
	}

	const cid = "0a6f3c2e-0000-4000-8000-0000000000a1"
	e.v.JumpToOwnSend(cid, false)
	done := ownSend(app, core.SendOpts{Channel: ch, Text: "sent while reading history", ClientMsgID: cid})
	until(t, 10*time.Second, "the jump to the sent message", e.frame, func() bool { return s.sendCID == "" && newestCID(s) == cid })
	at := func(what string) {
		t.Helper()
		e.frame()
		e.frame()
		if s.list.Position.BeforeEnd {
			t.Errorf("%s: the list isn't at its end (%+v)", what, s.list.Position)
		}
		if got := newestCID(s); got != cid {
			t.Errorf("%s: the newest message is %q, not the one sent", what, got)
		}
		bottom, ok := e.v.NewestRowBottom()
		if want := e.size.Y - 24; !ok || bottom != want { // the bottom spacer under it (space.150)
			t.Errorf("%s: the sent message ends at %d (on screen %v), want %d", what, bottom, ok, want)
		}
	}
	at("pending")
	if err := <-done; err != nil {
		t.Fatalf("send: %v", err)
	}
	until(t, 10*time.Second, "the confirmed message", e.frame, func() bool {
		n := s.newestItem()
		return n >= 0 && !s.snap.msgs[s.items[n].idx].m.Pending && newestCID(s) == cid
	})
	at("confirmed")
}

// A message taller than the list: jumpToTimestamp's anchor puts its top
// ANCHOR_OFFSET (64px) under the list's top, not its bottom at the end.
func TestOwnSendTallMessageShowsItsTop(t *testing.T) {
	e, app, ch := ownSendEnv(t)
	e.readBack(t)
	s := e.v.s
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, fmt.Sprintf("line %d of a long paste", i))
	}
	const cid = "0a6f3c2e-0000-4000-8000-0000000000a2"
	e.v.JumpToOwnSend(cid, false)
	done := ownSend(app, core.SendOpts{Channel: ch, Text: strings.Join(lines, "\n"), ClientMsgID: cid})
	until(t, 10*time.Second, "the jump to the sent message", e.frame, func() bool { return s.sendCID == "" && newestCID(s) == cid })
	e.frame()
	e.frame()
	n := s.newestItem()
	if h := s.heights[n]; h <= e.size.Y {
		t.Fatalf("the sent message is %d px tall, not taller than the %d px list", h, e.size.Y)
	}
	if y, ok := e.yOf(s.items[n].key); !ok || y != sendJumpOffset {
		t.Errorf("the sent message's top at %d (on screen %v), want %d (ANCHOR_OFFSET)", y, ok, sendJumpOffset)
	}
	if !s.list.Position.BeforeEnd {
		t.Errorf("the list went to its end, past the message's top")
	}
	if err := <-done; err != nil {
		t.Fatalf("send: %v", err)
	}
}

// Someone else's message while you read back keeps your place and draws
// no "Latest messages" (the newest page is loaded); at the bottom the list
// follows it.
func TestOthersMessagesKeepThePlace(t *testing.T) {
	e, app, ch := ownSendEnv(t)
	e.readBack(t)
	s := e.v.s
	key, y := e.firstMsg()
	ts := ownSendTS(s)
	ownSendInject(t, app, ch, "", ts, "a message from someone else")
	until(t, 10*time.Second, "their message", e.frame, func() bool { return s.newestTS() == ts })
	e.frame()
	e.frame()
	if k, ky := e.firstMsg(); k != key || ky != y {
		t.Errorf("their message moved the list: %s at %d, before %s at %d", k, ky, key, y)
	}
	if !s.list.Position.BeforeEnd {
		t.Errorf("their message took the list to its end")
	}
	if _, ok := e.v.NewestRowBottom(); ok {
		t.Errorf("their message is on screen")
	}
	if !s.latestR.Empty() {
		t.Errorf("\"Latest messages\" drew at %v with the newest messages loaded", s.latestR)
	}

	// at the bottom: stickToBottom
	e.v.ScrollToBottom()
	for i := 0; i < 3; i++ {
		e.frame()
	}
	ts2 := ownSendTS(s)
	ownSendInject(t, app, ch, "", ts2, "and another")
	until(t, 10*time.Second, "the next message", e.frame, func() bool { return s.newestTS() == ts2 })
	e.frame()
	e.frame()
	if s.list.Position.BeforeEnd {
		t.Errorf("at the bottom, their message left the list short of its end")
	}
	if bottom, ok := e.v.NewestRowBottom(); !ok || bottom != e.size.Y-24 {
		t.Errorf("their message ends at %d (on screen %v), want %d", bottom, ok, e.size.Y-24)
	}
}

// The thread pane opens a long thread you don't follow at its root;
// someone else's reply leaves it there, your reply takes the list to its
// end with the reply in view.
func TestOwnReplyGoesToTheEnd(t *testing.T) {
	app, _ := auditEnv(t)
	root := fakeslack.ThreadsAuditLong
	tv, _, frame := auditPane(t, app, root, false, 320)
	s := tv.s
	if p := s.list.Position; p.First != 0 || p.Offset != 0 {
		t.Fatalf("the thread opened at %+v, not at its root", p)
	}
	if _, ok := tv.NewestRowBottom(); ok {
		t.Fatal("the newest reply is on screen with the pane at the root")
	}
	ts := ownSendTS(s)
	ownSendInject(t, app, fakeslack.ThreadsAuditChannel, root, ts, "a reply from someone else")
	until(t, 10*time.Second, "their reply", frame, func() bool { return s.newestTS() == ts })
	frame()
	frame()
	if p := s.list.Position; p.First != 0 || p.Offset != 0 {
		t.Errorf("their reply moved the pane to %+v", p)
	}

	const cid = "0a6f3c2e-0000-4000-8000-0000000000a3"
	tv.JumpToOwnSend(cid, false)
	done := ownSend(app, core.SendOpts{Channel: fakeslack.ThreadsAuditChannel, ThreadTS: root, Text: "my reply", ClientMsgID: cid})
	until(t, 10*time.Second, "the reply", frame, func() bool { return newestCID(s) == cid })
	frame()
	frame()
	if s.list.Position.BeforeEnd {
		t.Errorf("the reply left the pane short of its end (%+v)", s.list.Position)
	}
	if _, ok := tv.NewestRowBottom(); !ok {
		t.Errorf("the reply isn't on screen")
	}
	if err := <-done; err != nil {
		t.Fatalf("send: %v", err)
	}
}
