package core

import (
	"context"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Synthetic fixtures only.

// A cached channel's History doesn't wait for the header's side reads:
// with edge users/counts held back, the page comes at once, the count
// isn't known yet, and when users/counts answers a "member_count" event
// carries the conversation with its count (the header repaints on it).
func TestHistoryCachedSkipsMemberCountWait(t *testing.T) {
	a, f := testApp(t)
	hold := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	t.Cleanup(release) // before the fake server's Close, which waits for the held call
	f.mu.Lock()
	f.reply["counts"] = `{"ok":true,"counts":{"people":42,"members":42,"apps":0,"guests":1}}`
	f.hooks["counts"] = func(url.Values) { <-hold }
	f.mu.Unlock()
	a.S.SetLatest("C1", []slack.Message{{TS: "1.000001", User: "U1", Text: "a"}, {TS: "1.000002", User: "U2", Text: "b"}}, false)
	evs, stop := a.Subscribe()
	defer stop()

	type result struct {
		p   Page
		err error
	}
	done := make(chan result, 1)
	go func() {
		p, err := a.History(context.Background(), "C1", "")
		done <- result{p, err}
	}()
	select {
	case r := <-done:
		if r.err != nil || len(r.p.Messages) != 2 {
			t.Fatalf("history: %+v %v", r.p, r.err)
		}
	case <-time.After(3 * time.Second):
		release()
		t.Fatal("a cached History waited for users/counts")
	}
	if n, ok := a.MemberCountCached("C1"); ok {
		t.Fatalf("count %d before users/counts answered", n)
	}
	called := func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, m := range f.calls {
			if m == "counts" {
				return true
			}
		}
		return false
	}
	for deadline := time.Now().Add(2 * time.Second); !called(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("no users/counts read for the header")
		}
	}

	release()
	ev := next(t, evs, "member_count")
	if ev.Channel != "C1" || ev.Conv == nil || ev.Conv.NumMembers != 42 {
		t.Fatalf("member_count event: %+v (conv %+v)", ev, ev.Conv)
	}
	if c, _ := a.Conv("C1"); c.NumMembers != 42 {
		t.Errorf("Conv(C1).NumMembers = %d, want users/counts' 42", c.NumMembers)
	}
	if g := a.GuestCountCached("C1"); g != 1 {
		t.Errorf("guests %d, want 1", g)
	}

	// known now: the next open starts no read, and a read that brings the
	// same count announces nothing
	n := 0
	f.mu.Lock()
	for _, m := range f.calls {
		if m == "counts" {
			n++
		}
	}
	f.mu.Unlock()
	if _, err := a.History(context.Background(), "C1", ""); err != nil {
		t.Fatal(err)
	}
	a.loadMemberCount(context.Background(), "C1")
	f.mu.Lock()
	m := 0
	for _, c := range f.calls {
		if c == "counts" {
			m++
		}
	}
	f.mu.Unlock()
	if m != n+1 {
		t.Errorf("%d users/counts reads after the count was known, want only the explicit one", m-n)
	}
	quiet := time.After(150 * time.Millisecond)
drain:
	for {
		select {
		case ev := <-evs:
			if ev.Type == "member_count" {
				t.Errorf("an unchanged count was announced: %+v", ev)
			}
		case <-quiet:
			break drain
		}
	}
	a.Shutdown() // waits for the side reads
}

// Shutdown waits for a side read History left running, and no new one
// starts once the session is over.
func TestHistorySideReadsEndWithTheSession(t *testing.T) {
	a, f := testApp(t)
	started := make(chan struct{})
	var once sync.Once
	f.mu.Lock()
	f.hooks["counts"] = func(url.Values) { once.Do(func() { close(started) }) }
	f.mu.Unlock()
	a.S.SetLatest("C1", []slack.Message{{TS: "1.000001", User: "U1", Text: "a"}}, false)
	if _, err := a.History(context.Background(), "C1", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("no side read started")
	}
	a.Shutdown()
	if !a.side.done {
		t.Error("Shutdown left side reads open")
	}
	if count, conv := a.historySide("C1"); count != nil || conv != nil {
		t.Error("a side read started after Shutdown")
	}
}

// A caller that joins a member-count read in flight waits only as long as
// its own ctx allows (a send's @channel check has 10 s), and the side
// reads Close waits for end when the session's context does, whoever owns
// the read they joined.
func TestMemberCountWaitersKeepTheirContext(t *testing.T) {
	a, f := testApp(t)
	hold := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	t.Cleanup(release)
	f.mu.Lock()
	f.reply["counts"] = `{"ok":true,"counts":{"people":7,"members":7}}`
	f.hooks["counts"] = func(url.Values) { <-hold }
	f.mu.Unlock()
	owner := make(chan struct{})
	go func() { // a caller without a deadline owns the read (a popup's)
		defer close(owner)
		a.loadMemberCount(context.Background(), "C1")
	}()
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		a.mu.Lock()
		_, out := a.inflight["mc:C1"]
		a.mu.Unlock()
		if out {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the owner's users/counts read never started")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		a.loadMemberCount(ctx, "C1")
	}()
	select {
	case <-joined:
	case <-time.After(3 * time.Second):
		release()
		<-joined
		<-owner
		t.Fatal("a joining caller with a 150 ms context waited for another caller's read")
	}
	if _, ok := a.MemberCountCached("C1"); ok {
		t.Error("the count is known before users/counts answered")
	}

	// History's side read joins the same read; sign-out cancels the
	// session's context and Close returns without the owner's answer
	a.S.SetLatest("C1", []slack.Message{{TS: "1.000001", User: "U1", Text: "a"}}, false)
	if _, err := a.History(context.Background(), "C1", ""); err != nil {
		t.Fatal(err)
	}
	a.StopTraffic()
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		_ = a.Close()
	}()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		release()
		<-closed
		t.Fatal("Close waited for another caller's users/counts read")
	}
	release()
	<-owner
}

// The sidebar's press prefetch only fills the store: the conversation on
// screen (watch, what the poll refetches and typing's lookups read) stays
// the open one, and the open's History then reads the page from memory.
func TestPrefetchHistoryLeavesTheOpenConversation(t *testing.T) {
	a, f := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "C2", Name: "random", IsChannel: true, IsMember: true})
	f.mu.Lock()
	f.reply["conversations.history"] = `{"ok":true,"messages":[{"ts":"7.000001","user":"U2","text":"hi"}],"has_more":false}`
	f.mu.Unlock()
	a.S.SetLatest("C1", []slack.Message{{TS: "1.000001", User: "U1", Text: "a"}}, false)
	if _, err := a.History(context.Background(), "C1", ""); err != nil {
		t.Fatal(err)
	}
	if !a.PrefetchHistory("C2") {
		t.Fatal("no prefetch for a conversation without a page")
	}
	for deadline := time.Now().Add(3 * time.Second); a.S.Timeline("C2") == nil; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the prefetch never filled C2's page")
		}
	}
	if !a.watching("C1", "") || a.watching("C2", "") {
		a.mu.Lock()
		w := a.watchCh
		a.mu.Unlock()
		t.Errorf("the prefetch moved the conversation on screen to %q", w)
	}
	if a.PrefetchHistory("C2") {
		t.Error("a second prefetch for a page core holds")
	}
	reads := func() int {
		f.mu.Lock()
		defer f.mu.Unlock()
		n := 0
		for _, m := range f.calls {
			if m == "conversations.history" {
				n++
			}
		}
		return n
	}
	before := reads()
	if p, err := a.History(context.Background(), "C2", ""); err != nil || len(p.Messages) != 1 {
		t.Fatalf("history after the prefetch: %+v %v", p, err)
	}
	if n := reads(); n != before {
		t.Errorf("History read C2 again after the prefetch (%d → %d)", before, n)
	}
	a.Shutdown()
}

// Close returns promptly while a view's History is slow: the press
// prefetch skips a conversation whose history is loading (d7's loading)
// instead of joining the view's read, and History's side read that joined
// a lookup another caller owns (a popup's, with no deadline) stops waiting
// when sign-out cancels core's background context.
func TestCloseDoesNotWaitOnSlowReads(t *testing.T) {
	a, f := testApp(t)
	hold := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	t.Cleanup(release)
	f.mu.Lock()
	f.reply["conversations.history"] = `{"ok":true,"messages":[{"ts":"7.000001","user":"U2","text":"hi"}],"has_more":false}`
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"D9","is_im":true,"user":"U2"}}`
	f.hooks["conversations.history"] = func(url.Values) { <-hold }
	f.hooks["conversations.info"] = func(url.Values) { <-hold }
	f.mu.Unlock()
	a.S.PutConv(slack.Conversation{ID: "C2", Name: "random", IsChannel: true, IsMember: true})
	inflight := func(key string) {
		t.Helper()
		for deadline := time.Now().Add(3 * time.Second); !a.inFlight(key); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%s never went out", key)
			}
		}
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	defer release()
	wg.Add(2)
	go func() { // a view's History, under the view's own 45 s context
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_, _ = a.History(ctx, "C2", "")
	}()
	inflight("h:C2")
	if a.PrefetchHistory("C2") {
		t.Error("a press prefetched a conversation whose history is loading")
	}
	go func() { // a popup's lookup with no deadline
		defer wg.Done()
		a.ResolveConv(context.Background(), "D9")
	}()
	inflight("ci:D9")
	a.S.SetLatest("D9", []slack.Message{{TS: "1.000001", User: "U2", Text: "a"}}, false)
	if _, err := a.History(context.Background(), "D9", ""); err != nil { // cached: its side read joins the lookup
		t.Fatal(err)
	}
	a.StopTraffic()
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		_ = a.Close()
	}()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		release()
		<-closed
		t.Fatal("Close waited for reads another caller owns")
	}
}
