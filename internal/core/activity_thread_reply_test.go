package core

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// Karim, 2026-11-12: every reply in a thread they were in showed in
// Activity twice, as a mention (@) and as the thread. The server pushes a
// thread_reply entry per reply next to the thread's thread_v2 bundle;
// Slack's activityUpdatedV2 drops every type outside its J list, and
// getActivityIconType draws an unknown type with the @ glyph.

func threadV2Entry(ch, thread, latest string) map[string]any {
	return map[string]any{"type": "activity", "subtype": "activity_updated", "entry": map[string]any{
		"key": "thread_v2-" + ch + "-" + thread, "feed_ts": latest, "is_unread": true,
		"item": map[string]any{"type": "thread_v2", "bundle_info": map[string]any{"payload": map[string]any{
			"thread_entry": map[string]any{"channel_id": ch, "thread_ts": thread, "latest_ts": latest, "unread_msg_count": 1}}}}}}
}

func threadReplyEntry(ch, thread, ts, author string) map[string]any {
	return map[string]any{"type": "activity", "subtype": "activity_updated", "entry": map[string]any{
		"key": "thread_reply-" + ch + "-" + ts, "feed_ts": ts, "is_unread": true,
		"item": map[string]any{"type": "thread_reply", "message": map[string]any{
			"ts": ts, "channel": ch, "thread_ts": thread, "author_user_id": author, "is_broadcast": false}}}}
}

func TestActivityThreadReplyNotAMention(t *testing.T) {
	for _, tc := range []struct {
		name            string
		started         bool // the viewer's own thread, else one they follow
		replyEntryFirst bool
	}{
		{"own thread, thread_reply first", true, true},
		{"own thread, thread_v2 first", true, false},
		{"followed thread, thread_reply first", false, true},
		{"followed thread, thread_v2 first", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, fs, log := rtApp(t)
			ch := fixtureCh
			p, _, _ := threadFixture(a, ch)
			if tc.started {
				a.S.Mutate(ch, p.TS, func(m *slack.Message) { m.User = fakeslack.SelfID; m.Subscribed = true })
			} else {
				a.S.Mutate(ch, p.TS, func(m *slack.Message) { m.Subscribed = true })
			}
			before := a.Count(ch).Mentions
			// no mention of the viewer, but the channel's name and a word
			// like it, as in Karim's reply
			r := slack.Message{TS: fmtTS(time.Now().Unix() - 30), ThreadTS: p.TS, User: "U0FAKE00003",
				Text: "Clack does this, and clack does that"}
			fs.Push(map[string]any{"type": "message", "channel": ch, "ts": r.TS, "thread_ts": p.TS, "user": r.User,
				"text": r.Text, "parent_user_id": map[bool]string{true: fakeslack.SelfID, false: p.User}[tc.started], "event_ts": r.TS})
			evs := []map[string]any{threadReplyEntry(ch, p.TS, r.TS, r.User), threadV2Entry(ch, p.TS, r.TS)}
			if !tc.replyEntryFirst {
				evs[0], evs[1] = evs[1], evs[0]
			}
			for _, ev := range evs {
				fs.Push(ev)
			}
			flush(t, fs, log)
			// let the activity items hydrate (activityItems fetches the messages)
			log.wait(t, "the thread_v2 item", func(evs []Event) bool {
				for _, ev := range evs {
					if ev.Type == "activity" && ev.Activity != nil && ev.Activity.Type == "thread_v2" {
						return true
					}
				}
				return false
			})
			flush(t, fs, log)
			rows := 0
			for _, ev := range log.all() {
				switch {
				case ev.Type == "activity" && ev.Activity != nil:
					rows++
					if ev.Activity.Type != "thread_v2" {
						t.Errorf("a realtime %q item reached the views", ev.Activity.Type)
					}
				case ev.Type == "activity" && ev.Name == "activity_updated":
					t.Errorf("an activity_updated event without its item reached the views")
				case ev.Type == "message" && ev.TS == r.TS:
					if ev.Mention {
						t.Error("the reply counts as a mention")
					}
					if ev.Msg == nil || a.MentionsViewerInReply(ev.Msg) {
						t.Error("the reply would notify as a mention (replyNotifies)")
					}
				}
			}
			if rows != 1 {
				t.Errorf("%d realtime activity rows, want 1 (the thread)", rows)
			}
			if got := a.Count(ch).Mentions; got != before {
				t.Errorf("mention count %d → %d for a reply without a mention", before, got)
			}
		})
	}
}

// The control: a reply that @-mentions the viewer is a mention (at_user,
// with the thread_v2 next to it, as Slack lists both).
func TestActivityThreadReplyWithMention(t *testing.T) {
	a, fs, log := rtApp(t)
	ch := fixtureCh
	p, _, _ := threadFixture(a, ch)
	r := slack.Message{TS: fmtTS(time.Now().Unix() - 30), ThreadTS: p.TS, User: "U0FAKE00003", Text: "hey <@" + fakeslack.SelfID + ">"}
	fs.Push(map[string]any{"type": "message", "channel": ch, "ts": r.TS, "thread_ts": p.TS, "user": r.User, "text": r.Text, "event_ts": r.TS})
	fs.Push(map[string]any{"type": "activity", "subtype": "activity_updated", "entry": map[string]any{
		"key": "at_user-" + r.TS, "feed_ts": r.TS, "is_unread": true,
		"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": r.TS, "channel": ch, "thread_ts": p.TS, "author_user_id": r.User}}}})
	fs.Push(threadV2Entry(ch, p.TS, r.TS))
	log.wait(t, "both items", func(evs []Event) bool {
		n := 0
		for _, ev := range evs {
			if ev.Type == "activity" && ev.Activity != nil {
				n++
			}
		}
		return n == 2
	})
	for _, ev := range log.all() {
		if ev.Type == "message" && ev.TS == r.TS && (!ev.Mention || ev.Msg == nil || !a.MentionsViewerInReply(ev.Msg)) {
			t.Error("an @-mention in a reply isn't a mention")
		}
	}
}
