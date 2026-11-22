package core

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// The socket events' network lookups run off the event pump, in the
// order the events came, as the Activity events' do (activity_later.go):
// users.prefs after a pref_change (Clack's own frecency saves come back
// as one about once a minute) or a search_frecency or search_recents,
// the sections after a sections event, dnd.info after the viewer's
// dnd_updated, a saved item's message, the conversation a membership
// event names, the bot an assistant status names, and the conversation of
// a message core doesn't know yet. On the pump, every message behind one
// of them waited for its round trip and reached the lists late, in a
// clump (Karim: messages show late). Slack's socket handler takes each
// event as it comes and fetches what it needs without holding up the
// next. What an event writes to the store happens on the pump as before;
// only the lookup, and what it announces, come after.

// laterQueue runs its tasks one after another, in the order they were
// queued, on a worker that runs while it isn't empty.
type laterQueue struct {
	mu      sync.Mutex
	tasks   []func(ctx context.Context)
	running bool
}

// runLater runs f after every task queued on q before it, off the
// caller's goroutine, with a context that ends with the session (or after
// 15 s, as the pump's).
func (a *App) runLater(q *laterQueue, f func(ctx context.Context)) {
	q.mu.Lock()
	q.tasks = append(q.tasks, f)
	start := !q.running
	q.running = true
	q.mu.Unlock()
	if start {
		go a.laterWork(q)
	}
}

func (a *App) laterWork(q *laterQueue) {
	for {
		q.mu.Lock()
		if len(q.tasks) == 0 {
			q.running = false
			q.tasks = nil
			q.mu.Unlock()
			return
		}
		f := q.tasks[0]
		q.tasks[0] = nil
		q.tasks = q.tasks[1:]
		q.mu.Unlock()
		if a.bg.Err() != nil {
			continue // the session ended: nothing to announce
		}
		ctx, cancel := context.WithTimeout(a.bg, 15*time.Second)
		f(ctx)
		cancel()
	}
}

// pumpLater runs f after every task queued before it with pumpLater, off
// the event pump.
func (a *App) pumpLater(f func(ctx context.Context)) { a.runLater(&a.pumpQ, f) }

// convLater runs f after the earlier held events of ch, off the event
// pump: each conversation's held messages wait for its own lookup, not
// for the other socket events' (pumpLater's queue: a users.prefs, a
// sections read).
func (a *App) convLater(ch string, f func(ctx context.Context)) {
	a.convHoldMu.Lock()
	if a.convQs == nil {
		a.convQs = map[string]*laterQueue{}
	}
	q := a.convQs[ch]
	if q == nil {
		q = &laterQueue{}
		a.convQs[ch] = q
	}
	a.convHoldMu.Unlock()
	a.runLater(q, f)
}

// holdForConv reports whether a message of ch waits for a lookup of its
// conversation (one core doesn't know), or for earlier messages of ch
// that do: they go out in order (releaseConv after each).
func (a *App) holdForConv(ch string) bool {
	a.convHoldMu.Lock()
	defer a.convHoldMu.Unlock()
	if a.convHold[ch] == 0 {
		if _, known := a.S.Conv(ch); known || a.knownNonMember(ch) {
			return false
		}
	}
	if a.convHold == nil {
		a.convHold = map[string]int{}
	}
	a.convHold[ch]++
	return true
}

// holdBehindConv reports whether ch has events waiting for its lookup
// (holdForConv), counting the caller's among them when it does: an edit,
// a deletion, a lock or a thread's replies then go out after the messages
// they change, in the socket's order (releaseConv after it), as Slack's
// preprocessRtmMessage holds each of a conversation's message events until
// the conversation is fetched. With none waiting it applies at once,
// whether core knows ch or not.
func (a *App) holdBehindConv(ch string) bool {
	a.convHoldMu.Lock()
	defer a.convHoldMu.Unlock()
	if a.convHold[ch] == 0 {
		return false
	}
	a.convHold[ch]++
	return true
}

// releaseConv is a held event of ch done: a message announced, or a held
// change applied (holdBehindConv).
func (a *App) releaseConv(ch string) {
	a.convHoldMu.Lock()
	defer a.convHoldMu.Unlock()
	if a.convHold[ch]--; a.convHold[ch] > 0 {
		return
	}
	delete(a.convHold, ch)
	if q := a.convQs[ch]; q != nil {
		q.mu.Lock()
		idle := len(q.tasks) == 0
		q.mu.Unlock()
		if idle {
			delete(a.convQs, ch) // its worker ends with its last task
		}
	}
}

// dndUpdateLoads reports whether a dnd_updated event is the viewer's, so
// dnd.info is read after it (onDNDUpdated's).
func (a *App) dndUpdateLoads(raw json.RawMessage) bool {
	var ev struct {
		User   string     `json:"user"`
		Status *DNDStatus `json:"dnd_status"`
	}
	return json.Unmarshal(raw, &ev) == nil && ev.Status != nil && (ev.User == "" || ev.User == a.S.SelfID())
}
