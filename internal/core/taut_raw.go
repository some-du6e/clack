package core

// Additions for internal/taut (Taut plugin compatibility). Kept in their own
// file so the backend's files stay as they are; the only edit elsewhere is
// the a.tapRaw(ev) call at the top of handle (events.go).

import (
	"sync"
	"sync/atomic"

	"github.com/some-du6e/clack/internal/slack"
)

// rawTaps fans the realtime gateway's raw events out to SubscribeRaw
// subscribers. nil until the first subscriber, so without plugins the cost
// is one atomic load per event. A channel leaves subs only under mu, which
// every tap holds, and whoever takes it out closes it: its stop, or
// Release, which takes the whole set (subs nil).
type rawTaps struct {
	mu   sync.Mutex
	subs map[chan slack.Event]struct{}
}

type rawTapPtr = atomic.Pointer[rawTaps]

var rawTapIndex = newPerApp() // *App → *rawTapPtr

func (a *App) rawTapPtr() *rawTapPtr {
	if p, ok := rawTapIndex.Load(a); ok {
		return p.(*rawTapPtr)
	}
	p, _ := rawTapIndex.LoadOrStore(a, new(rawTapPtr))
	return p.(*rawTapPtr)
}

// tapRaw hands ev to raw subscribers, never blocking the event pump: a
// subscriber that falls more than its buffer behind loses events.
func (a *App) tapRaw(ev slack.Event) {
	p, ok := rawTapIndex.Load(a)
	if !ok {
		return
	}
	t := p.(*rawTapPtr).Load()
	if t == nil {
		return
	}
	t.mu.Lock()
	for c := range t.subs {
		select {
		case c <- ev:
		default:
		}
	}
	t.mu.Unlock()
}

// SubscribeRaw delivers every realtime event as the gateway sent it (Type,
// Subtype and the full Raw payload), before core applies it, so Taut's
// rtm.on sees exactly what Slack's web client would. Call cancel to stop.
func (a *App) SubscribeRaw() (<-chan slack.Event, func()) {
	ptr := a.rawTapPtr()
	c := make(chan slack.Event, 1024)
	t := ptr.Load()
	if t == nil {
		ptr.CompareAndSwap(nil, &rawTaps{subs: map[chan slack.Event]struct{}{}})
		t = ptr.Load()
	}
	if t == nil { // Release took the set meanwhile: the App is done
		close(c)
		return c, func() {}
	}
	t.mu.Lock()
	if t.subs == nil || a.released.Load() { // likewise, or Release is under way
		close(c)
	} else {
		t.subs[c] = struct{}{}
	}
	t.mu.Unlock()
	var once sync.Once
	return c, func() {
		once.Do(func() {
			// whoever takes c out closes it, under the lock every tap
			// holds: a stop before Release used to leave its consumer's
			// range loop, and what it kept, waiting for good
			t.mu.Lock()
			if _, live := t.subs[c]; live {
				delete(t.subs, c)
				close(c)
			}
			t.mu.Unlock()
		})
	}
}

// MessageFromSlack builds the UI message for a Slack-shaped message, the
// same conversion core applies to history. internal/taut uses it to turn a
// plugin's patched view of a message (patchSlice('messages'),
// patchMessageText, injectMessages) back into what the UI draws.
func (a *App) MessageFromSlack(ch string, m *slack.Message) Message { return a.message(ch, m) }

// MemberCountCached is the people count core has for ch (edge users/counts,
// the header's number), without a network call; ok is false if unknown.
func (a *App) MemberCountCached(ch string) (n int, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	n, ok = a.memberN[ch]
	return n, ok
}

// AppCountCached is how many apps core knows are in ch (edge users/counts
// `apps`, Slack's membershipCounts app_count).
func (a *App) AppCountCached(ch string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.memberA[ch]
}

// GuestCountCached is how many of MemberCountCached's people are guests
// (edge users/counts `guests`, Slack's restricted_member_count).
func (a *App) GuestCountCached(ch string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.memberG[ch]
}

// SavedState reports whether ch/ts is in the viewer's Saved / To-dos.
func (a *App) SavedState(ch, ts string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.saved[ch+"/"+ts]
}

// ServeOffline marks the app ready without Boot: Ready, DataReady,
// Snapshot and the rest answer from what the store already holds. For
// packages built on core that run without a network (internal/taut's
// plugin tests, demos). Call it at most once, instead of Boot.
func (a *App) ServeOffline() {
	a.markDataReady()
	close(a.ready)
}

// ServeLookups runs, for an app ServeOffline serves, the background
// lookups Boot starts for what rendering couldn't name (resolveLoop:
// channels, bots, canvases' files.info), until StopTraffic. Tests that
// draw what the store learns late use it. Call it at most once.
func (a *App) ServeLookups() { go a.resolveLoop(a.bg) }

// Inject applies one realtime event as if the gateway had sent it (raw
// subscribers first, then core's own handling and its typed events). For
// offline use and tests; the live gateway goes through the same path.
func (a *App) Inject(ev slack.Event) { a.handle(ev) }
