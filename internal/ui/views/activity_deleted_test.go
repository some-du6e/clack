package views

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// Activity's rows for messages that are no more, as Slack's item
// components draw them: a mention's or a reaction's row renders null once
// its message is non-existent (messages.list answered without it:
// UnknownFetchManager's isNonExistent), deleted (message_deleted:
// "Remove activity item") or a tombstone (message_changed); a thread's
// row whose latest reply is gone stays with no body. No row is a
// loading skeleton for ever.

// Fetches that fail (an API error for the channel, a 5xx), every one of
// core's tries, delete nothing: the mention's and the thread's rows stay,
// with no body and no shimmer frame after frame (core gave up:
// MessageGaveUp), and no change says the messages are gone, so Saved and
// Threads keep them too.
func TestActivityFailedFetchKeepsTheRows(t *testing.T) {
	for _, f := range []fakeslack.Fault{{Error: "channel_not_found"}, {Status: 500}} {
		e := newLmEnv(t, lmOpts{latest: true, setup: func(e *lmEnv) {
			e.app.SetRefetchBackoff(func(int) time.Duration { return 5 * time.Millisecond })
			e.fs.SetFault("messages.list", f)
		}})
		e.e.until(t, "the rows", func() bool { return e.shown(lmMentionKey) && e.shown(lmThreadKey) && !e.v.data.busy() })
		e.e.until(t, "core giving up", func() bool {
			return e.app.MessageGaveUp(lmChan, lmMention) && e.app.MessageGaveUp(lmChan, lmLatest)
		})
		for i := 0; i < 5; i++ {
			e.frame()
		}
		shimmers := e.v.shimmers
		for i := 0; i < 10; i++ {
			e.frame()
		}
		if n := e.v.shimmers - shimmers; n != 0 {
			t.Errorf("%+v: %d loading shimmers in 10 frames after core gave up: a row loads for ever", f, n)
		}
		if h := e.rowDrawnH(lmMentionKey); h <= 0 {
			t.Errorf("%+v: the mention whose fetches failed is a %dpx row: a failure hid it", f, h)
		}
		if h := e.rowH(lmThreadKey); h <= 0 {
			t.Errorf("%+v: the thread's row is %dpx; it stays with no body", f, h)
		}
		if got := e.drawn(lmLatest); got != "" {
			t.Errorf("%+v: the thread's row drew %q for a reply it couldn't fetch", f, got)
		}
		for _, ts := range []string{lmMention, lmLatest} {
			if c, ok := e.app.MessageChange(lmChan, ts); ok && c.Deleted {
				t.Errorf("%+v: %s, whose fetches failed, is gone", f, ts)
			}
		}
	}
}

// activityInject delivers a socket event to v's core (the views' fake
// opens no socket).
func activityInject(h *vaHost, ev map[string]any) {
	ev["ts"], ev["event_ts"] = "1762709900.000100", "1762709900.000100"
	raw, _ := json.Marshal(ev)
	sub, _ := ev["subtype"].(string)
	typ, _ := ev["type"].(string)
	ch, _ := json.Marshal(ev["channel"])
	h.app.Inject(slack.Event{Type: typ, Subtype: sub, Channel: ch, Raw: raw})
}

// activityListed reports whether the page lists and draws the entry key.
func activityListed(v *Activity, key string) bool {
	p := actPage(v)
	for li, i := range v.vis {
		if i >= 0 && i < len(p.Items) && actKey(&p.Items[i]) == key {
			h, ok := v.stk.h[li]
			if li == 0 {
				h -= v.stk.lead
			}
			return !ok || h > 0
		}
	}
	return false
}

// Live: the reaction row's message deleted, and the mention's turned into
// a tombstone: each row goes in the next frames and doesn't come back.
func TestActivityLiveDeletedReactionAndTombstone(t *testing.T) {
	v, h, e, _ := vaFakeActivity(t, false)
	e.size.Y = 2400
	e.frame()
	ch := fakeslack.ViewsChannel
	const reaction, reactionTS = "fa-reaction", "1762700500.000100"
	const mention, mentionTS = "fa-at-user", "1762700100.000100"
	e.until(t, "the rows", func() bool { return activityListed(v, reaction) && activityListed(v, mention) })
	activityInject(h, map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true, "channel": ch,
		"deleted_ts": reactionTS, "previous_message": map[string]any{"type": "message", "ts": reactionTS, "user": fakeslack.SelfID, "text": "a synthetic message people liked"}})
	e.until(t, "the reaction row gone", func() bool { return !activityListed(v, reaction) })
	prev := map[string]any{"type": "message", "ts": mentionTS, "user": "U0FAKE00001", "text": "<@" + fakeslack.SelfID + "> can you look at the synthetic plan?"}
	activityInject(h, map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "channel": ch,
		"message":          map[string]any{"type": "message", "subtype": "tombstone", "ts": mentionTS, "user": "USLACKBOT", "text": "This message was deleted."},
		"previous_message": prev})
	e.until(t, "the tombstoned mention gone", func() bool { return !activityListed(v, mention) })
	for i := 0; i < 10; i++ {
		e.frame()
		if activityListed(v, reaction) || activityListed(v, mention) {
			t.Fatalf("frame %d: a deleted message's row came back", i)
		}
	}
}
