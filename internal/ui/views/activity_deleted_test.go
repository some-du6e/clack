package views

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// Activity's rows for messages that are no more, as Slack's item
// components draw them: a mention's or a reaction's row renders null once
// its message is non-existent (messages.list had none, or refused the
// fetch: UnknownFetchManager's isNonExistent), deleted (message_deleted:
// "Remove activity item") or a tombstone (message_changed); a thread's
// row whose latest reply is gone stays with no body. No row is a
// loading skeleton for ever.

// A fetch Slack refuses (an API error for the channel) leaves no row on
// its loading bar: the mention renders null, the thread whose latest
// reply it couldn't bring draws no body; no shimmer frame after frame.
func TestActivityRefusedFetchLeavesNoSkeleton(t *testing.T) {
	e := newLmEnv(t, lmOpts{latest: true, setup: func(e *lmEnv) {
		e.fs.SetFault("messages.list", fakeslack.Fault{Error: "channel_not_found"})
	}})
	e.e.until(t, "the rows", func() bool { return e.shown(lmMentionKey) && e.shown(lmThreadKey) && !e.v.data.busy() })
	for i := 0; i < 5; i++ {
		e.frame()
	}
	shimmers := e.v.shimmers
	for i := 0; i < 10; i++ {
		e.frame()
	}
	if n := e.v.shimmers - shimmers; n != 0 {
		t.Errorf("%d loading shimmers in 10 frames after Slack refused the fetch: a row loads for ever", n)
	}
	if h := e.rowH(lmMentionKey); h != 0 {
		t.Errorf("the mention whose fetch Slack refused is a %dpx row; Slack renders it null", h)
	}
	if h := e.rowH(lmThreadKey); h <= 0 {
		t.Errorf("the thread's row is %dpx; it stays with no body", h)
	}
	if got := e.drawn(lmLatest); got != "" {
		t.Errorf("the thread's row drew %q for a reply there is none of", got)
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
