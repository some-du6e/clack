package core

// A stale app card is refetched without forgetting it: while
// apps.profile.get is in flight, and after it fails, the last good card
// still answers (Slack keeps the stored app profile while it refetches,
// and its refetcher swallows a failed refetch and marks the app fresh), so
// the Tasks tab rule never reads the agent as not an agent and an open
// Tasks tab doesn't fall back to Messages.

import (
	"context"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

func TestAgentAppCardKeptWhileRefetching(t *testing.T) {
	a, fs := agentTasksApp(t, nil)
	ctx := context.Background()
	card, err := a.AppCardOf(ctx, fakeslack.AgentTasksUserID, "")
	if err != nil || !card.AgentApp {
		t.Fatalf("first fetch: %+v %v", card, err)
	}
	k := a.appCardKey(fakeslack.AgentTasksUserID, "")
	expire := func() {
		c := a.appCards()
		c.mu.Lock()
		c.m[k].at = time.Now().Add(-2 * appCardTTL)
		c.mu.Unlock()
	}
	// what the Tasks tab rule and the badges read: the agent's card
	kept := func(when string) {
		t.Helper()
		if got, ok := a.CachedAppCard(fakeslack.AgentTasksUserID, ""); !ok || !got.AgentApp {
			t.Errorf("%s: cached %+v %v, want the last card (an agent)", when, got, ok)
		}
		if got, ok, known := a.AppCardSettled(fakeslack.AgentTasksUserID, ""); !ok || !known || !got.AgentApp {
			t.Errorf("%s: settled %+v ok %v known %v, want the last card, known (the Tasks tab rule reads it)", when, got, ok, known)
		}
	}
	// the refetch held in flight
	expire()
	gate := make(chan struct{})
	fs.SetHook(func(m string, _ url.Values) (any, bool) {
		if m == "apps.profile.get" {
			<-gate
		}
		return nil, false
	})
	done := make(chan struct{})
	go func() { _, _ = a.AppCardOf(ctx, fakeslack.AgentTasksUserID, ""); close(done) }()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		c := a.appCards()
		c.mu.Lock()
		waiting := c.m[k] != nil && c.m[k].wait != nil
		c.mu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the stale card's refetch never started")
		}
	}
	kept("while the card refetches")
	close(gate)
	<-done
	// a refetch that fails: Slack's refetcher swallows the error and keeps
	// the stored app, fresh again
	expire()
	var asked atomic.Int32
	fs.SetHook(func(m string, _ url.Values) (any, bool) {
		if m == "apps.profile.get" {
			asked.Add(1)
			return map[string]any{"ok": false, "error": "internal_error"}, true
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	got, err := a.AppCardOf(ctx, fakeslack.AgentTasksUserID, "")
	if err != nil || !got.AgentApp {
		t.Errorf("the failing refetch answered %+v %v, want the last card and no error", got, err)
	}
	kept("after a failed refetch")
	if _, err := a.AppCardOf(ctx, fakeslack.AgentTasksUserID, ""); err != nil {
		t.Errorf("the next read: %v", err)
	}
	if n := asked.Load(); n != 1 {
		t.Errorf("apps.profile.get asked %d times for the failed refetch and the read after it, want 1 (the kept card is fresh again)", n)
	}
}
