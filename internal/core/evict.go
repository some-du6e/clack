package core

import (
	"context"
	"log"
	"time"

	"github.com/some-du6e/clack/internal/store"
)

// Slack's bounded message cache (store/evict.go): every EvictEvery core
// applies Slack's budget to the cached conversations and threads and emits
// an "evicted" event for each it trimmed, so the message lists and the
// plugins' state mirror drop the same messages. Slack checks about hourly,
// when a message is read after the interval; Clack checks more often,
// with the same budget and the same choice of messages, so a session's
// memory comes back within minutes of leaving a conversation instead of
// growing for an hour.
var EvictEvery = 2 * time.Minute

// evictOnScreen: a conversation read this recently is kept whole, as if on
// screen (a list on screen refreshes its conversation every 30 s, the
// split view shows a second one): one left a few minutes ago comes back
// where it was. Slack checks only hourly, which keeps the same messages.
const evictOnScreen = 10 * time.Minute

// noteAccess records that ch (or "ch/thread_ts") was read now (Slack's
// recordHit on every message read; the comparator and the whole-history
// reset look at when).
func (a *App) noteAccess(key string) {
	a.mu.Lock()
	if a.accessed == nil {
		a.accessed = map[string]time.Time{}
	}
	a.accessed[key] = time.Now()
	a.mu.Unlock()
}

func (a *App) evictLoop(ctx context.Context) {
	t := time.NewTicker(EvictEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		a.EvictMessages()
		if a.files != nil {
			a.files.sweep(time.Now().Add(-fileIdle))
		}
	}
}

// EvictMessages applies Slack's message cache budget now (evictLoop runs
// it every EvictEvery) and reports what went.
func (a *App) EvictMessages() []store.Evicted {
	now := time.Now()
	in := store.EvictInput{Now: now, Accessed: map[string]time.Time{}, Keep: map[string]bool{}, Pending: map[string]bool{}}
	a.mu.Lock()
	for k, at := range a.accessed {
		if now.Sub(at) > store.EvictRecent+time.Hour {
			delete(a.accessed, k) // forgetAccessesBefore
			continue
		}
		in.Accessed[k] = at
		if now.Sub(at) < evictOnScreen {
			in.Keep[k] = true
		}
	}
	if a.watchCh != "" {
		in.Keep[a.watchCh] = true
	}
	for _, th := range a.watchTh {
		in.Keep[th.ch+"/"+th.ts] = true
	}
	for id, p := range a.pending {
		if !p.confirmed {
			in.Pending[id] = true
		}
	}
	a.mu.Unlock()
	in.Conv = func(ch string) (known, member bool) {
		if _, ok := a.S.Conv(ch); ok {
			return true, true
		}
		_, ok := a.S.ExtConv(ch)
		return ok, false
	}
	in.WholeOK = func(ch string) bool {
		// isUncountedChannelEvictable: no mentions waiting (DMs count
		// every unread message as one)
		c, ok := a.S.Conv(ch)
		if !ok {
			return false
		}
		e, _ := a.S.Count(ch)
		if e.MentionCount > 0 || (c.IsIM || c.IsMPIM) && e.HasUnreads {
			return false
		}
		return true
	}
	in.LastRead = func(ch string) string {
		e, _ := a.S.Count(ch)
		return e.LastRead
	}
	out := a.S.EvictMessages(in)
	if len(out) == 0 {
		return nil
	}
	n := 0
	for _, e := range out {
		ev := Event{Type: "evicted", Channel: e.Channel, ThreadTS: e.Thread, TS: e.Oldest}
		a.emit(ev)
		n++
	}
	log.Printf("core: message cache: trimmed %d conversations and threads to Slack's budget", n)
	return out
}
