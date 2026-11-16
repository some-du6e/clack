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
