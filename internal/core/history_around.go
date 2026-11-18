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
// Slack's reachedEnd false, which shows "Latest messages", loads newer
// pages at its end (requestNewer: {oldest: the window's newest}) and
// never marks the conversation read. None of it goes in the store's
// timeline, which holds the newest window only.

// Around is a window of a conversation's history.
type Around struct {
	Messages []Message // ascending
	HasOlder bool      // older messages exist (hasMoreStart)
	HasNewer bool      // the window stops short of the newest message (hasMoreEnd)
}

// AroundLimit is each call's page (determineDefaultHistoryLimit).
const AroundLimit = 28

// HistoryAround is the window around message ts of ch: the page ending
// at it and the page starting at it.
func (a *App) HistoryAround(ctx context.Context, ch, ts string) (Around, error) {
	t0 := time.Now()
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
	a.SubscribePresence(rowUsers(msgs))
	return Around{Messages: a.timeline(ch, msgs), HasOlder: o.p.HasMore, HasNewer: n.p.HasMore}, nil
}

// HistoryNewer is requestNewer for a window short of the newest message:
// the page just after after.
func (a *App) HistoryNewer(ctx context.Context, ch, after string) (Around, error) {
	t0 := time.Now()
	p, err := a.C.History(ctx, ch, slack.HistoryOpts{Oldest: after, Limit: 100})
	log.Printf("history %s: newer than %s in %s", logtag.ID(ch), logtag.ID(after), time.Since(t0).Round(time.Millisecond))
	if err != nil {
		return Around{}, err
	}
	msgs := mergeHistory(p.Messages, nil)
	a.SubscribePresence(rowUsers(msgs))
	return Around{Messages: a.timeline(ch, msgs), HasNewer: p.HasMore}, nil
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
