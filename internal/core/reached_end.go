package core

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Windows after a socket gap (store/reached_end.go has Slack's side).
//
// At every connect that isn't a fast reconnect, Slack's onConnected
// handler (boot-shared erkq, "Resetting reachedEnd") resets every
// channel's reachedEnd (client-boot 6Yed resetReachedEnd): the socket
// missed whatever was posted while it was away, from a few seconds of
// network blip to hours of a laptop asleep. The view on screen draws the
// slice it had and asks for what came after its newest message
// (AEjl requestNewer: conversations.history oldest=<newest> inclusive,
// limit 28); with more left than that, the newest view starts from the
// newest page. Clack reset nothing and read nothing: the open channel's
// list reloaded core's cached window, and the socket's next message went
// right after it, the hours between missing with nothing to say so
// (Karim's #clack, 2026-11-20: 6:54 PM, then 9:53 PM).
//
// So each window's end goes stale at a slow connect, the socket's
// messages past it wait (store.UpsertHeard), and catchUp reads the
// window forward from its end: the open conversation's at once, any
// other's the next time a view reads it (History returns the cached
// window at once, as Slack draws the outdated slice, and
// "history_caught_up" tells the lists when the read is in). The other
// conversations' unread state comes from client.counts after every
// connect (refreshCountsAfterGap).

// catchUpLimit is the page a catch-up reads forward: Slack's requestNewer
// leaves fetchHistory its default limit (rGEL determineDefaultHistoryLimit).
const catchUpLimit = 28

// catchUpRetries are a failed catch-up's waits before it tries again (the
// network often isn't back the moment the socket is). Var for tests.
var catchUpRetries = []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second}

// resetReachedEnd is the onConnected handler's reset at a connect that
// wasn't fast: every window's end goes stale, and the open
// conversation's is read forward at once (Slack's open view asks for
// newer messages as it draws).
func (a *App) resetReachedEnd() {
	n := a.S.ResetReachedEnd(a.trackedSends())
	if n == 0 {
		return
	}
	log.Printf("core: slow connect: resetting reachedEnd (%d windows)", n)
	a.mu.Lock()
	open := a.watchCh
	a.mu.Unlock()
	if open != "" {
		a.startCatchUp(open)
	}
}

// trackedSends is "ch/ts" of the viewer's sends core still tracks:
// pending and failed copies (local ts) and confirmed ones (Slack's ts,
// for two minutes after). None marks a window's end: a send goes in past
// the window whatever the socket missed, as the Web API works while the
// socket is dead (Karim's 9:53 PM message went out that way).
func (a *App) trackedSends() map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]bool{}
	for _, p := range a.pending {
		if !p.confirmed {
			out[p.opts.Channel+"/"+p.ts] = true
		} else if p.realTS != "" {
			out[p.opts.Channel+"/"+p.realTS] = true
		}
	}
	return out
}

// sendsIn is the ts of the viewer's sends in ch core still tracks
// (trackedSends): a newest page keeps them in the window
// (store.SetLatestReadAt).
func (a *App) sendsIn(ch string) map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]bool{}
	for _, p := range a.pending {
		if p.opts.Channel != ch {
			continue
		}
		if !p.confirmed {
			out[p.ts] = true
		} else if p.realTS != "" {
			out[p.realTS] = true
		}
	}
	return out
}

// catchingUp holds the conversations whose catch-up (startCatchUp) is
// running, per App: one at a time each.
var catchingUp sync.Map // catchUpKey → struct{}

type catchUpKey struct {
	a  *App
	ch string
}

// startCatchUp reads ch's window forward in the background when its end
// is stale (once at a time), trying again a few times while it fails.
func (a *App) startCatchUp(ch string) {
	if _, _, stale := a.S.EndOf(ch); !stale {
		return
	}
	k := catchUpKey{a, ch}
	if _, running := catchingUp.LoadOrStore(k, struct{}{}); running {
		return
	}
	go func() {
		done := a.runCatchUp(ch)
		catchingUp.Delete(k)
		if _, _, stale := a.S.EndOf(ch); done && stale {
			a.startCatchUp(ch) // the socket came back again as the read ended
		}
	}()
}

// runCatchUp is startCatchUp's tries; it reports whether the last one
// read what it went for.
func (a *App) runCatchUp(ch string) bool {
	for try := 0; ; try++ {
		ctx, cancel := context.WithTimeout(a.bg, time.Minute)
		err := a.catchUp(ctx, ch)
		cancel()
		if err == nil {
			return true
		}
		if a.bg.Err() != nil || a.C.SignedOut() || try >= len(catchUpRetries) {
			if a.bg.Err() == nil {
				log.Printf("history %s: catch-up: %v", ch, err)
			}
			return false
		}
		select {
		case <-a.bg.Done():
			return false
		case <-time.After(catchUpRetries[try]):
		}
		if _, _, stale := a.S.EndOf(ch); !stale {
			return true // a newest read caught it up meanwhile
		}
	}
}

// catchUp reads ch's window forward from the end it had when its end went
// stale, the page Slack's open view asks for: a page that leaves nothing
// newer joins the window, and what the socket brought past the old end
// with it. With more left than a page, the newest page starts the window
// over, as Slack's newest view does (requestHistory({}) on an empty
// latest slice). A window with no message from Slack to read forward from
// takes the newest page too. A page read across another slow connect
// leaves the end stale (what that gap missed is after it), and the
// window is read forward again from the page's end.
func (a *App) catchUp(ctx context.Context, ch string) error {
	return a.onceCtx(ctx, "catchup:"+ch, func() error {
		t0 := time.Now()
		for round := 0; round < 3; round++ {
			from, gen, stale := a.S.EndOf(ch)
			if !stale {
				return nil
			}
			if from == "" {
				break
			}
			pins, subs, live := a.S.PinSeq(), a.S.SubSeq(), a.S.LiveSeq()
			p, err := a.C.History(ctx, ch, slack.HistoryOpts{Oldest: from, Inclusive: true, Limit: catchUpLimit})
			if err != nil {
				return err
			}
			joined, ok := a.S.CatchUp(ch, from, gen, p.Messages, p.HasMore, live)
			a.S.SyncPins(ch, pins, p.Messages) // a pin event newer than the answer (store/pins.go)
			a.S.SyncSubs(ch, subs, p.Messages) // the roots' thread subscriptions (store/thread_subs.go)
			a.announceReads()                  // a revision the store held another of (read_changes.go)
			a.fillStubs(ch, p.Messages)
			if !ok {
				return nil // a newest read caught the window up meanwhile, or it went
			}
			if joined {
				log.Printf("history %s: caught up (%d messages from the end, %s)", ch, len(p.Messages), time.Since(t0).Round(time.Millisecond))
				a.emit(Event{Type: "history_caught_up", Channel: ch})
				return nil
			}
			if p.HasMore {
				break
			}
		}
		// more than a page behind: the newest page, which joins what the
		// socket brought past the old end (store SetLatestRead)
		if err := a.loadLatest(ctx, ch); err != nil {
			return err
		}
		log.Printf("history %s: more than a page behind; started over at the newest page (%s)", ch, time.Since(t0).Round(time.Millisecond))
		a.emit(Event{Type: "history_caught_up", Channel: ch})
		return nil
	})
}

// fillStubs completes the file stubs among msgs (files.info), as a
// newest page's are.
func (a *App) fillStubs(ch string, msgs []slack.Message) {
	for i := range msgs {
		if needsFileInfo(&msgs[i]) {
			go a.fillFiles(ch, msgs[i].TS)
		}
	}
}

// IsAhead reports whether ch/ts is a message waiting past the stale end
// of ch's window (store.UpsertHeard): the lists don't draw it until the
// window is read forward to it ("history_caught_up").
func (a *App) IsAhead(ch, ts string) bool { return a.S.IsAhead(ch, ts) }
