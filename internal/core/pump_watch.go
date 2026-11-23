package core

import (
	"log"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/logtag"
	"github.com/some-du6e/clack/internal/slack"
)

// The event pump's watchdog. Karim's α.136 logged nothing at all from
// 01:57Z to 02:42Z on 2026-11-22: no event handled, no socket line, a
// reply's toast lost, and no way to tell a handler that hung from a
// socket that went quiet (slack rtm.go's watchdog takes that one). Now an
// event the pump has been on for pumpStallAfter, or a task one of the
// queues behind it has been on (the lookups', the Activity events', a held
// conversation's), is logged once, with what it is and how many wait
// behind it, and every goroutine's stack follows, the stalled one's first:
// what the handler is blocked on. Once it's done, how long it took. An
// idle pump and idle queues log nothing. Vars for tests.
var pumpStallAfter, pumpWatchEvery atomic.Int64 // nanoseconds

func init() {
	pumpStallAfter.Store(int64(30 * time.Second))
	pumpWatchEvery.Store(int64(5 * time.Second))
}

// setPumpTimers sets the stall's threshold and how often the watchdog
// looks, and returns the undo: for tests.
func setPumpTimers(after, every time.Duration) (restore func()) {
	a, e := pumpStallAfter.Swap(int64(after)), pumpWatchEvery.Swap(int64(every))
	return func() { pumpStallAfter.Store(a); pumpWatchEvery.Store(e) }
}

// pumpTask is what a pump or a queue is on: the event (the pump's), or
// none (a queue's task), and since when, on the wall clock.
type pumpTask struct {
	ev slack.Event
	at slack.ClockReading
}

// what names the task for the log: the event's type and subtype and its
// conversation's tag, never its text.
func (t *pumpTask) what() string {
	if t.ev.Type == "" {
		return "a task"
	}
	s := "a " + t.ev.Type
	if t.ev.Subtype != "" {
		s += "/" + t.ev.Subtype
	}
	s += " event"
	if ch := chanID(t.ev.Channel); ch != "" {
		s += " of " + logtag.ID(ch)
	}
	return s
}

// tracked runs f as the task t marks in on, and logs it once it's done
// when it took longer than pumpStallAfter.
func tracked(on *atomic.Pointer[pumpTask], where string, t *pumpTask, f func()) {
	on.Store(t)
	f()
	on.Store(nil)
	if took := slack.ReadClock().WallSince(t.at); took >= time.Duration(pumpStallAfter.Load()) {
		log.Printf("core: %s took %s on %s", where, took.Round(100*time.Millisecond), t.what())
	}
}

// pumpWatch looks at the pump and its queues every pumpWatchEvery until
// done, and logs each task stalled past pumpStallAfter once.
func (a *App) pumpWatch(done <-chan struct{}) {
	t := time.NewTicker(time.Duration(pumpWatchEvery.Load()))
	defer t.Stop()
	told := map[*pumpTask]bool{}
	for {
		select {
		case <-done:
			return
		case <-t.C:
		}
		after := time.Duration(pumpStallAfter.Load())
		now := slack.ReadClock()
		look := func(where string, on *atomic.Pointer[pumpTask], waiting int, stack string) {
			w := on.Load()
			if w == nil || told[w] {
				return
			}
			age := now.WallSince(w.at)
			if age < after {
				return
			}
			told[w] = true
			log.Printf("core: %s has been on %s for %s, %d waiting behind it; the goroutines follow", where, w.what(), age.Round(time.Second), waiting)
			if !a.pumpDumps.Log("core: the goroutines", stack) {
				log.Printf("core: (the goroutines were dumped less than %s ago)", a.pumpDumps.Every)
			}
		}
		look("the event pump", &a.pumpOn, len(a.RTM.Events), "core.(*App).pump(")
		look("the lookups' queue", &a.pumpQ.on, a.pumpQ.waiting(), "core.(*App).laterWork(")
		a.actLaterMu.Lock()
		acts := len(a.actLaterQ)
		a.actLaterMu.Unlock()
		look("the Activity events' queue", &a.actOn, acts, "core.(*App).activityWorker(")
		a.convHoldMu.Lock()
		convs := make(map[string]*laterQueue, len(a.convQs))
		for ch, q := range a.convQs {
			convs[ch] = q
		}
		a.convHoldMu.Unlock()
		for ch, q := range convs {
			look(logtag.ID(ch)+"'s held events' queue", &q.on, q.waiting(), "core.(*App).laterWork(")
		}
		if len(told) > 256 { // the stalls are long over: start over
			clear(told)
		}
	}
}
