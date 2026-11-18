package core

import (
	"context"
	"encoding/json"
)

// The socket events' network lookups run off the event pump, in the
// order the events came, as the Activity events' do (activity_later.go):
// users.prefs after a pref_change (Clack's own frecency saves come back
// as one about once a minute), the sections after a sections event,
// dnd.info after dnd_updated, the saved item's message, the conversation a
// membership event names, the bot an assistant status names, and the
// conversation of a message core doesn't know yet. Every message behind
// one of them waited for its round trip, and reached the lists late and
// in a clump (msg-latency). What the event writes to the store happens on
// the pump as before; the lookup and what it announces come after.

// pumpLater runs f after every task queued before it with pumpLater, off
// the caller's goroutine, with a context that ends with the session (or
// after 15 s, as the pump's).
func (a *App) pumpLater(f func(ctx context.Context)) { a.inOrder(&a.pumpQ, f) }

// convLater runs f after the earlier held messages of ch, off the caller's
// goroutine: each conversation's held messages wait for its own lookup,
// not for the other socket events' (pumpLater's queue: a users.prefs, a
// sections read).
func (a *App) convLater(ch string, f func(ctx context.Context)) {
	a.convHoldMu.Lock()
	if a.convQs == nil {
		a.convQs = map[string]*actQueue{}
	}
	q := a.convQs[ch]
	if q == nil {
		q = &actQueue{}
		a.convQs[ch] = q
	}
	a.convHoldMu.Unlock()
	a.inOrder(q, f)
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

// holdBehindConv reports whether ch has messages waiting for its lookup
// (holdForConv), counting the caller's event among them when it does: an
// edit or a deletion then goes out after them, in the socket's order
// (releaseConv after it). With none waiting it applies at once, whether
// core knows ch or not.
func (a *App) holdBehindConv(ch string) bool {
	a.convHoldMu.Lock()
	defer a.convHoldMu.Unlock()
	if a.convHold[ch] == 0 {
		return false
	}
	a.convHold[ch]++
	return true
}

// releaseConv is a held message of ch announced (or a held change
// applied: holdBehindConv).
func (a *App) releaseConv(ch string) {
	a.convHoldMu.Lock()
	if a.convHold[ch]--; a.convHold[ch] <= 0 {
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
	a.convHoldMu.Unlock()
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
