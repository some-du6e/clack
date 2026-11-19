package core

import (
	"context"
	"log"
	"time"

	"github.com/some-du6e/clack/internal/logtag"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// A conversation opened at a message it doesn't hold, the way Slack's
// message pane does (boot-shared, build 2c0d02ca130f): a jump
// (jumpToThreadReply's startTs, toMessage's) mounts the pane with startTs,
// and its first load is requestHistory({latest: startTs, oldest:
// startTs}). fetchHistory sends that as two conversations.history calls
// at once, the page that ends at the message and the page that starts at
// it, inclusive, 28 each (QJKZ determineDefaultHistoryLimit), however far
// from the newest the message is: two requests, never a walk back from
// the newest page. The window's ends are the calls' has_more
// (hasMoreStart, hasMoreEnd): a window short of the newest message is
// Slack's reachedEnd false, which shows "Latest messages", loads its
// neighbours as it scrolls (requestOlder {latest: its start}, requestNewer
// {oldest: its end}, inclusive, 28 each) and never marks the conversation
// read.
//
// The window is a slice of Slack's channelHistory (store/windows.go): core
// keeps it as one of the conversation's windows, apart from the timeline
// (the newest messages), so its messages take live changes and its pages
// never write the timeline; the slice joins the timeline only once they
// meet (or it reaches the newest message), as Slack's slices do. A jump
// back to a message a window holds is the window again, with no request
// (getTimestampsByChannelId's slice for startTs).

// Around is a window of a conversation's history.
type Around struct {
	Messages []Message // ascending
	HasOlder bool      // older messages exist (hasMoreStart)
	HasNewer bool      // the window stops short of the newest message (hasMoreEnd)
	// Merged: the window met the conversation's newest messages (or
	// reached the newest) and core joined it to the timeline, which holds
	// it now
	Merged bool
}

// AroundLimit is each call's page (determineDefaultHistoryLimit).
const AroundLimit = 28

// HistoryAround is the window around message ts of ch: the window core
// holds that has it, else the page ending at it and the page starting at
// it, kept as one of the conversation's windows.
func (a *App) HistoryAround(ctx context.Context, ch, ts string) (Around, error) {
	a.noteAccess(ch) // a window read is the conversation in use (evict.go)
	if w, ok := a.S.WindowAt(ch, ts); ok {
		return a.around(ch, w), nil
	}
	t0 := time.Now()
	pins, subs, live := a.S.PinSeq(), a.S.SubSeq(), a.S.LiveSeq()
	type answer struct {
		p   *slack.HistoryPage
		err error
	}
	older, newer := make(chan answer, 1), make(chan answer, 1)
	go func() {
		p, err := a.C.History(ctx, ch, slack.HistoryOpts{Latest: ts, Inclusive: true, Limit: AroundLimit})
		older <- answer{p, err}
	}()
	go func() {
		p, err := a.C.History(ctx, ch, slack.HistoryOpts{Oldest: ts, Inclusive: true, Limit: AroundLimit})
		newer <- answer{p, err}
	}()
	o, n := <-older, <-newer
	log.Printf("history %s: around %s in %s", logtag.ID(ch), logtag.ID(ts), time.Since(t0).Round(time.Millisecond))
	if o.err != nil {
		return Around{}, o.err
	}
	if n.err != nil {
		return Around{}, n.err
	}
	msgs := mergeHistory(o.p.Messages, n.p.Messages)
	return a.addWindow(ch, msgs, ts, ts, o.p.HasMore, n.p.HasMore, pins, subs, live), nil
}

// WindowOlder is a window's requestOlder: the page ending at its start,
// added to it.
func (a *App) WindowOlder(ctx context.Context, ch, start string) (Around, error) {
	a.noteAccess(ch)
	t0 := time.Now()
	pins, subs, live := a.S.PinSeq(), a.S.SubSeq(), a.S.LiveSeq()
	p, err := a.C.History(ctx, ch, slack.HistoryOpts{Latest: start, Inclusive: true, Limit: AroundLimit})
	log.Printf("history %s: older than %s in %s", logtag.ID(ch), logtag.ID(start), time.Since(t0).Round(time.Millisecond))
	if err != nil {
		return Around{}, err
	}
	return a.addWindow(ch, mergeHistory(p.Messages, nil), "", start, p.HasMore, true, pins, subs, live), nil
}

// WindowNewer is a window's requestNewer: the page starting at its end,
// added to it; the window joins the timeline once the page meets it.
func (a *App) WindowNewer(ctx context.Context, ch, end string) (Around, error) {
	a.noteAccess(ch)
	t0 := time.Now()
	pins, subs, live := a.S.PinSeq(), a.S.SubSeq(), a.S.LiveSeq()
	p, err := a.C.History(ctx, ch, slack.HistoryOpts{Oldest: end, Inclusive: true, Limit: AroundLimit})
	log.Printf("history %s: newer than %s in %s", logtag.ID(ch), logtag.ID(end), time.Since(t0).Round(time.Millisecond))
	if err != nil {
		return Around{}, err
	}
	return a.addWindow(ch, mergeHistory(p.Messages, nil), end, "", true, p.HasMore, pins, subs, live), nil
}

// addWindow keeps a stretch of ch's history from start to end ("" the
// page's own ends) as a window (store.AddWindow), read by requests begun
// at LiveSeq live, with the pins and the thread subscriptions events
// changed while it was out, and its files looked up, as the newest page's
// are (fetchLatest). A window that joined the timeline reports what the
// timeline has before it (Slack's foreword shows only at reachedStart).
func (a *App) addWindow(ch string, msgs []slack.Message, start, end string, hasOlder, hasNewer bool, pins, subs, live uint64) Around {
	a.S.SyncPins(ch, pins, msgs) // a pin event newer than the answer, over it (store/pins.go)
	a.S.SyncSubs(ch, subs, msgs) // the roots' thread subscriptions (store/thread_subs.go)
	w, merged := a.S.AddWindow(ch, msgs, start, end, hasOlder, hasNewer, live)
	for i := range msgs {
		if needsFileInfo(&msgs[i]) {
			go a.fillFiles(ch, msgs[i].TS)
		}
	}
	a.SubscribePresence(rowUsers(msgs))
	if merged {
		older := hasOlder
		if t := a.S.Timeline(ch); t != nil {
			older = t.HasMore
		}
		return Around{Messages: a.timeline(ch, w.Msgs), HasOlder: older, Merged: true}
	}
	return a.around(ch, w)
}

// HoldWindow keeps the window of ch holding ts while a list shows it
// (store.HoldWindow): the cap on windows and the conversation's eviction
// never drop it, so the socket's reactions and edits keep finding its
// messages. release lets it go.
func (a *App) HoldWindow(ch, ts string) (release func()) { return a.S.HoldWindow(ch, ts) }

// around is window w as Around.
func (a *App) around(ch string, w store.Window) Around {
	return Around{Messages: a.timeline(ch, w.Msgs), HasOlder: w.HasOlder, HasNewer: true}
}

// mergeHistory is two pages as one, each message once, ascending.
func mergeHistory(x, y []slack.Message) []slack.Message {
	seen := make(map[string]bool, len(x)+len(y))
	out := make([]slack.Message, 0, len(x)+len(y))
	for _, page := range [][]slack.Message{x, y} {
		for _, m := range page {
			if !seen[m.TS] {
				seen[m.TS] = true
				out = append(out, m)
			}
		}
	}
	store.SortAsc(out)
	return out
}
