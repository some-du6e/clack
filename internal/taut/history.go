package taut

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
)

// channelHistory as the plugins' patches leave it, for Clack's message
// lists (HistoryHidden).
//
// Slack's lists draw channelHistory[key]'s slices (a channel's, or a
// thread's under ch-threadTs), which a plugin's patch may filter: Slick's
// HideResolved takes resolved tickets out of help channels. Clack's lists
// draw core's messages, so they're told what the patches took out. Go
// names the keys its lists show (__taut.watchHistory), and JS tells it,
// whenever the store changes (each change a list's useSelector re-reads in
// Slack), which of each key's timestamps the patched history doesn't list
// (historyHidden, 03_redux.js). Pushed rather than asked for: what a patch
// reads is its own (HideResolved reads messages, reactions, channels and
// viewUiState), and a view cache staled by the slice it caches never
// heard of a ✅ landing on a ticket.
type historyView struct {
	mu      sync.Mutex
	read    map[string]int64   // key → when a list last asked (viewNow)
	hidden  map[string]*Hidden // key → what JS last told (absent: not yet; nil: nothing hidden)
	watched map[string]bool    // the keys JS watches, as last posted
	swept   int64              // when the watched keys were last looked over (viewNow)
	posting bool               // a watchHistory call is out
	again   bool               // another is to go when it's back
	posted  uint64             // watchHistory calls made; JS's tells carry the last it took
	// waiting: keys whose answer is owed by watchHistory call need, since
	// at (viewNow): a key just watched, or one a change Go made may move
	// (retell: a jump's highlight)
	waiting map[string]historyWait
}

type historyWait struct {
	need uint64
	at   int64
}

const (
	// historyIdle is how long a key no list asks for stays watched: a list
	// on screen asks in every frame it lays out, and one in an idle window
	// keeps its key (the window draws nothing to ask with until JS tells
	// it of a change, which brings a frame).
	historyIdle = int64(10 * time.Second)
	// historyWaitMax is how long a key's answer is waited for: past it
	// the list takes what it has (JS busy, or gone).
	historyWaitMax = int64(2 * time.Second)
)

// Hidden is the timestamps the plugins' patches took out of a history.
type Hidden struct{ ts map[string]bool }

// Has reports whether ts is one of them.
func (h *Hidden) Has(ts string) bool { return h != nil && h.ts[ts] }

// Len is how many there are.
func (h *Hidden) Len() int {
	if h == nil {
		return 0
	}
	return len(h.ts)
}

// HistoryHidden is what the plugins' patches took out of
// channelHistory[key] (key a channel's id, or ch-threadTs for a thread):
// nil for nothing, and the same *Hidden until it changes. ok is false
// while JS owes an answer for key (its first, or one after a change Go
// made that the patches may read: SetViewHighlight), for up to 2 s; the
// list draws what it has meanwhile, as a row draws its source until its
// view comes, but a jump waits. Never blocks, and cheap enough for every
// frame; the first ask for a key starts JS watching it.
func (t *Taut) HistoryHidden(key string) (h *Hidden, ok bool) {
	if !t.SlicePatched("channelHistory") {
		return nil, true
	}
	hv := &t.ui.history
	now := viewNow()
	hv.mu.Lock()
	hv.read[key] = now
	post := false
	if !hv.watched[key] || now-hv.swept > int64(time.Second) {
		post = hv.sweepLocked(now)
	}
	h, ok = hv.hidden[key], true
	if w, owed := hv.waiting[key]; owed {
		// a key watched anew waits too (sweepLocked), so one JS never told
		// of reads as nothing hidden once the wait is over
		if ok = now-w.at > historyWaitMax; ok {
			delete(hv.waiting, key)
		}
	}
	hv.mu.Unlock()
	if post {
		t.postHistoryWatch()
	}
	return h, ok
}

// sweepLocked makes the watched keys the ones a list asked for within
// historyIdle, letting the rest go (and what JS told of them: watched
// again, a key starts from nothing), and reports whether they changed.
// A key watched anew waits for the call that will name it (mu).
func (hv *historyView) sweepLocked(now int64) (changed bool) {
	hv.swept = now
	next := make(map[string]bool, len(hv.read))
	for k, at := range hv.read {
		if now-at > historyIdle {
			delete(hv.read, k)
			delete(hv.hidden, k)
			delete(hv.waiting, k)
			continue
		}
		next[k] = true
		if !hv.watched[k] {
			changed = true
			hv.waiting[k] = historyWait{hv.nextCallLocked(), now}
		}
	}
	if !changed && len(next) == len(hv.watched) {
		return false
	}
	hv.watched = next
	return true
}

// nextCallLocked is the number of the next watchHistory call to go: the
// one after the call out now, if one is (mu).
func (hv *historyView) nextCallLocked() uint64 { return hv.posted + 1 }

// postHistoryWatch tells JS the keys to watch, one call out at a time:
// what changes meanwhile goes when it's back. Every call has JS tell
// every key it names (watchHistory).
func (t *Taut) postHistoryWatch() {
	hv := &t.ui.history
	hv.mu.Lock()
	if hv.posting {
		hv.again = true
		hv.mu.Unlock()
		return
	}
	keys := make([]string, 0, len(hv.watched))
	for k := range hv.watched {
		keys = append(keys, k)
	}
	hv.posting = true
	hv.posted++
	n := hv.posted
	hv.mu.Unlock()
	slices.Sort(keys)
	t.mu.Lock()
	rt := t.rt
	t.mu.Unlock()
	done := func(err error) {
		hv.mu.Lock()
		hv.posting = false
		again := hv.again
		hv.again = false
		if err != nil {
			// not posted (the runtime busy past the timeout, or gone): the
			// next ask posts them again
			clear(hv.watched)
		}
		hv.mu.Unlock()
		if again {
			t.postHistoryWatch()
		}
	}
	if rt == nil {
		done(jsrt.ErrClosed)
		return
	}
	// on the in-order lane with the mirror's invalidates: JS reads the
	// history behind every change queued before it
	ctx, cancel := context.WithTimeout(jsrt.InOrder(context.Background()), 3*time.Second)
	rt.rt.CallThen(ctx, "__taut.watchHistory", func(_ json.RawMessage, err error) {
		cancel()
		done(err)
	}, keys, float64(n))
}

// retell has JS tell key again, after a change Go queued for it that the
// patches may read (SetViewHighlight's viewUiState): until it has,
// HistoryHidden answers not ok. The mirror's queue is flushed first, so
// the call runs behind the change.
func (t *Taut) retellHistory(key string) {
	if !t.SlicePatched("channelHistory") {
		return
	}
	hv := &t.ui.history
	hv.mu.Lock()
	watched := hv.watched[key]
	if watched {
		hv.waiting[key] = historyWait{hv.nextCallLocked(), viewNow()}
	}
	hv.mu.Unlock()
	if watched {
		t.mirror.send()
		t.postHistoryWatch()
	}
}

// tell takes what JS says the patches took out of the keys it watches
// (only the keys that changed, but every key after a watchHistory call),
// as of call seq, and has the UI draw again.
func (hv *historyView) tell(t *Taut, changed map[string][]string, seq uint64) {
	moved := false
	hv.mu.Lock()
	for k, tss := range changed {
		if !hv.watched[k] {
			continue // let go meanwhile
		}
		var h *Hidden
		if len(tss) > 0 {
			h = &Hidden{ts: make(map[string]bool, len(tss))}
			for _, ts := range tss {
				h.ts[ts] = true
			}
		}
		if old, had := hv.hidden[k]; !had || !sameHidden(old, h) {
			hv.hidden[k] = h
			moved = true
		}
	}
	for k, w := range hv.waiting {
		if _, told := changed[k]; told && seq >= w.need {
			delete(hv.waiting, k)
			moved = true
		}
	}
	hv.mu.Unlock()
	if f := t.opts.UI.Invalidate; moved && f != nil {
		f()
	}
}

// reset forgets the keys JS watched and what it told: a new runtime
// (restartAfterCrash) watches nothing until the lists ask again.
func (hv *historyView) reset() {
	hv.mu.Lock()
	defer hv.mu.Unlock()
	clear(hv.hidden)
	clear(hv.watched)
	clear(hv.waiting)
	hv.swept = 0
}

func (hv *historyView) init() {
	hv.read = map[string]int64{}
	hv.hidden = map[string]*Hidden{}
	hv.watched = map[string]bool{}
	hv.waiting = map[string]historyWait{}
}

func sameHidden(a, b *Hidden) bool {
	if a.Len() != b.Len() {
		return false
	}
	if a == nil || b == nil {
		return true // both empty
	}
	for ts := range a.ts {
		if !b.ts[ts] {
			return false
		}
	}
	return true
}

// historyKeys lists the keys JS watches (tests).
func (t *Taut) historyKeys() []string {
	hv := &t.ui.history
	hv.mu.Lock()
	defer hv.mu.Unlock()
	keys := make([]string, 0, len(hv.watched))
	for k := range hv.watched {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
