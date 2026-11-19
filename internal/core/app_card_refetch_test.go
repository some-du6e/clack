package core

// A stale app card is refetched without forgetting it: while
// apps.profile.get is in flight, and after it fails, CachedAppCard still
// answers with the last good card (Slack keeps the stored app profile
// while it refetches), so the Tasks tab rule never reads the agent as not
// an agent and an open Tasks tab doesn't fall back to Messages.

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

func TestAppCardKeptWhileRefetching(t *testing.T) {
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
	deadline := time.Now().Add(5 * time.Second)
	for {
		c := a.appCards()
		c.mu.Lock()
		waiting := c.m[k] != nil && c.m[k].wait != nil
		c.mu.Unlock()
		if waiting || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got, ok := a.CachedAppCard(fakeslack.AgentTasksUserID, ""); !ok || !got.AgentApp {
		t.Errorf("while the card refetches: cached %+v %v, want the last card (an agent)", got, ok)
	}
	close(gate)
	<-done
	// a refetch that fails
	expire()
	fs.SetHook(func(m string, _ url.Values) (any, bool) {
		if m == "apps.profile.get" {
			return map[string]any{"ok": false, "error": "internal_error"}, true
		}
		return nil, false
	})
	if _, err := a.AppCardOf(ctx, fakeslack.AgentTasksUserID, ""); err == nil {
		t.Errorf("the failing refetch answered no error")
	}
	if got, ok := a.CachedAppCard(fakeslack.AgentTasksUserID, ""); !ok || !got.AgentApp {
		t.Errorf("after a failed refetch: cached %+v %v, want the last card", got, ok)
	}
	fs.SetHook(nil)
}
