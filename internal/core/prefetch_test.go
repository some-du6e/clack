package core

import (
	"context"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Boot's prefetch loads unread conversations three at a time. One queued
// behind the other three must not refetch a channel opened (History) while
// it waited: its page, read from Slack after the open, replaced the cached
// one and wiped a message sent in between
// (TestRealtimeSendEchoOneMessage/echo_before_reply, CI 2026-11-11).
func TestPrefetchSkipsLoadedWhileQueued(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	// boot's own prefetch first: every unread conversation loaded
	waitForms(t, "boot's prefetch", 1, func() int {
		var unread []string
		a.S.RLock()
		for id, e := range a.S.Counts {
			if _, ok := a.S.Convs[id]; ok && (e.HasUnreads || e.MentionCount > 0) && !a.S.Muted[id] {
				unread = append(unread, id)
			}
		}
		a.S.RUnlock()
		for _, id := range unread {
			if a.S.Timeline(id) == nil {
				return 0
			}
		}
		return 1
	})
	// four read channels turn unread: three with mentions go first and are
	// held at the fake, the fourth queues behind them
	held := []string{"C0FAKE00002", "C0FAKE00003", "C0FAKE00004"}
	target := "C0FAKE00005"
	a.S.Lock()
	for i, id := range append(append([]string(nil), held...), target) {
		if _, ok := a.S.Convs[id]; !ok {
			a.S.Unlock()
			t.Fatalf("%s isn't a member channel", id)
		}
		a.S.Counts[id] = &slack.CountEntry{ID: id, HasUnreads: true, MentionCount: 3 - i, LastRead: "1762400000.000000", Latest: "1762516800.000000"}
	}
	a.S.Unlock()
	for _, id := range append(append([]string(nil), held...), target) {
		if a.S.Timeline(id) != nil {
			t.Fatalf("%s already loaded", id)
		}
	}
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	var mu sync.Mutex
	calls := map[string]int{}
	var holding atomic.Int32
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "conversations.history" {
			return nil, false
		}
		ch := f.Get("channel")
		mu.Lock()
		calls[ch]++
		mu.Unlock()
		for _, h := range held {
			if ch == h {
				holding.Add(1)
				<-gate
			}
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })

	done := make(chan struct{})
	go func() { defer close(done); a.prefetch(ctx) }()
	// three fetches held: the loop has seen the target unloaded and waits
	// for a slot
	waitForms(t, "the held fetches", 3, func() int { return int(holding.Load()) })
	if _, err := a.History(ctx, target, ""); err != nil {
		t.Fatal(err)
	}
	text := "sent while the prefetch waited"
	if _, err := a.Send(ctx, SendOpts{Channel: target, Text: text}); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("prefetch never finished")
	}
	mu.Lock()
	n := calls[target]
	mu.Unlock()
	if n != 1 {
		t.Errorf("%s history fetched %d times, want 1 (History's)", target, n)
	}
	if got := countWhere(timelineOf(a, target), func(m *slack.Message) bool { return m.Text == text }); got != 1 {
		t.Errorf("%d copies of the message sent after the open, want 1", got)
	}
}
