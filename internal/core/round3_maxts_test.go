package core

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// A message the typed decode refuses (here a files field that isn't a
// list) was still heard: its ts goes into the notifications' maxTs, read
// on its own, so the desktop_notification about it that follows isn't
// dropped as 19.
func TestMaxTSFromAMessageTheDecodeRefuses(t *testing.T) {
	a, _ := testApp(t)
	const ts = "1762600000.000100"
	a.handle(rtm("message", map[string]any{"channel": "C1", "ts": ts, "user": "U1", "text": "synthetic", "files": "not a list"}))
	if got := a.S.NotificationsMaxTS(); got != ts {
		t.Errorf("after a message the typed decode refused maxTs is %q, want its ts %s", got, ts)
	}
}

// An optimistic send's local copy carries the local clock's ts, which can
// run ahead of the server's: maxTs keeps the server's newest until the
// send is confirmed, then takes the server's ts for it.
func TestOptimisticSendKeepsMaxTS(t *testing.T) {
	a, f := testApp(t)
	const server, sent = "1700000000.000100", "1700000001.000200"
	a.S.SetLatest("C1", []slack.Message{{Type: "message", TS: server, User: "U1", Text: "synthetic"}}, false)
	f.mu.Lock()
	f.reply["chat.postMessage"] = `{"ok":true,"channel":"C1","ts":"` + sent + `","message":{"type":"message","user":"USELF","text":"a synthetic reply","ts":"` + sent + `"}}`
	f.mu.Unlock()
	release, started := gateOnce(f, "chat.postMessage")
	defer release()
	done := make(chan error, 1)
	go func() {
		_, err := a.Send(context.Background(), SendOpts{Channel: "C1", Text: "a synthetic reply"})
		done <- err
	}()
	waitFor(t, "the send out", 3*time.Second, started)
	if got := a.S.NotificationsMaxTS(); got != server {
		t.Errorf("with the send's local copy in, maxTs is %q, want the server's newest %s (the copy's ts is the local clock's)", got, server)
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := a.S.NotificationsMaxTS(); got != sent {
		t.Errorf("once the send was confirmed maxTs is %q, want the server's ts for it %s", got, sent)
	}
}

// The Threads view's subscriptions.thread.getView answer doesn't put an
// older threads state back over an update_thread_state that came while
// it was out.
func TestThreadsViewKeepsNewerThreadState(t *testing.T) {
	a, f := testApp(t)
	// the backstop read the event asks for stays off (thread_state_test.go)
	a.mu.Lock()
	hold := time.AfterFunc(time.Hour, func() {})
	a.countsTimer = hold
	a.mu.Unlock()
	t.Cleanup(func() { hold.Stop() })
	f.mu.Lock()
	f.reply["subscriptions.thread.getView"] = `{"ok":true,"threads":[],"has_more":false,"total_unread_replies":0,"threads_state":{"has_unreads":false,"mention_count":0}}`
	f.mu.Unlock()
	release, started := gateOnce(f, "subscriptions.thread.getView")
	defer release()
	done := make(chan error, 1)
	go func() {
		_, err := a.Threads(context.Background(), "", 20)
		done <- err
	}()
	waitFor(t, "getView out", 3*time.Second, started)
	a.handle(rtm("update_thread_state", map[string]any{"has_unreads": true, "mention_count": 1,
		"unread_count_by_channel": map[string]int{"C1": 1}, "org_wide": a.IsEnterprise()}))
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if st := a.ThreadsState(); !st.Unread || st.Mentions != 1 {
		t.Errorf("the Threads view's answer, requested before the update_thread_state, put its older state back: %+v, want the event's unread with 1 mention", st)
	}
}
