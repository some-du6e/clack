package core

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// notify-relatives part 3: what a message the socket brings does to its
// conversation's sidebar row, as Slack's countUnreadMessages (EJMC)
// counts it (docs/NOTIFY-RULES.md §7): bold when
// canCountAsUnreadForCurrentUser (qxvW) says so, with the system
// messages of system_messages_redesign_m1, and a mention badge when
// shouldMessageBadgeChannel (eqVu) does. Synthetic people and text only.

// TestSidebarMarksLikeSlack pushes one message at a time into a
// conversation whose row starts read, and reads the row's unread state
// and mention count after it.
func TestSidebarMarksLikeSlack(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	chs := fs.ChannelIDs()
	ch, quiet, every := chs[2], chs[4], chs[5]
	prefs, _ := json.Marshal(map[string]any{
		"global": map[string]any{"global_desktop": "mentions_dms", "global_mpdm_desktop": "mentions_dms", "global_keywords": "synthkeyword"},
		"channels": map[string]any{
			quiet: map[string]any{"suppress_at_channel": true},
			every: map[string]any{"badge_all_unreads": true},
		},
	})
	fs.SetPrefs(map[string]any{"all_notifications_prefs": string(prefs)})
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	evs, stop := a.Subscribe()
	t.Cleanup(stop)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.SetRealtime(true)
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "connection" && ev.Connected })
	log := &eventLog{}
	go func() {
		for ev := range evs {
			log.add(ev)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := a.RefreshPrefs(ctx); err != nil {
		t.Fatal(err)
	}
	if !a.NotifyPrefs(quiet).SuppressAtChannel || !a.NotifyPrefs(every).BadgeAllUnreads || len(a.S.HighlightWords()) == 0 {
		t.Fatalf("setup: the prefs didn't take (suppress_at_channel %v, badge_all_unreads %v, keywords %q)",
			a.NotifyPrefs(quiet).SuppressAtChannel, a.NotifyPrefs(every).BadgeAllUnreads, a.S.HighlightWords())
	}
	fs.Push(map[string]any{"type": "subteam_self_added", "subteam_id": fakeslack.WorkspaceGroupID})
	until(t, "the viewer's user group", func() bool { return a.S.InUsergroup(fakeslack.WorkspaceGroupID) })
	presence := func(p string) {
		fs.Push(map[string]any{"type": "presence_change", "user": fakeslack.SelfID, "presence": p})
		until(t, "the viewer "+p, func() bool {
			a.S.RLock()
			defer a.S.RUnlock()
			return a.S.Presence[fakeslack.SelfID] == p
		})
	}
	sender := fs.UserIDs()[7]
	me := "<@" + fakeslack.SelfID + "> "
	group := "<!subteam^" + fakeslack.WorkspaceGroupID + "> "
	// a ts of the clock's: the session began before it (start_ms)
	stamp := func(at time.Time) string { return fmt.Sprintf("%d.%06d", at.Unix(), at.Nanosecond()/1000) }
	// row reads the conversation as read, pushes ev and returns its row
	// once the socket's events behind it are in
	row := func(c string, ev map[string]any) Count {
		t.Helper()
		a.S.Lock()
		a.S.Counts[c] = &slack.CountEntry{ID: c, LastRead: "1000000000.000100", Latest: "1000000000.000100"}
		a.S.Unlock()
		ev["type"], ev["channel"] = "message", c
		if _, ok := ev["ts"]; !ok {
			ev["ts"] = stamp(time.Now())
		}
		ev["event_ts"] = ev["ts"]
		if _, ok := ev["user"]; !ok && ev["subtype"] != "bot_message" {
			ev["user"] = sender
		}
		fs.Push(ev)
		flush(t, fs, log)
		return a.Count(c)
	}
	for _, tc := range []struct {
		name     string
		ch       string
		ev       map[string]any
		before   func()
		unread   bool
		mentions int
	}{
		{"a mention of the viewer's user group", ch, map[string]any{"text": group + "a synthetic heads-up for the crew"}, nil, true, 1},
		{"a mention of the viewer in an attachment", ch, map[string]any{"text": "", "attachments": []any{
			map[string]any{"fallback": "a synthetic request", "text": me + "a synthetic review request"}}}, nil, true, 1},
		{"@here while the viewer is away", ch, map[string]any{"text": "<!here> a synthetic ping"}, func() { presence("away") }, true, 0},
		{"@here while the viewer is active", ch, map[string]any{"text": "<!here> another synthetic ping"}, func() { presence("active") }, true, 1},
		{"@here older than the session", ch, map[string]any{"text": "<!here> a synthetic ping from before", "ts": stamp(time.Now().Add(-2 * time.Hour))}, nil, true, 0},
		{"a keyword in a bot's message", ch, map[string]any{"subtype": "bot_message", "bot_id": "B0SYNTHBOT1", "username": "synthbot",
			"text": "synthkeyword went up"}, nil, true, 0},
		{"a keyword in a workflow's message", ch, map[string]any{"subtype": "bot_message", "bot_id": "B0SYNTHWFB1",
			"bot_profile": map[string]any{"id": "B0SYNTHWFB1", "name": "synth workflow", "is_workflow_bot": true},
			"text":        "synthkeyword is due"}, nil, true, 1},
		{"a keyword in a person's message", ch, map[string]any{"text": "the synthkeyword report"}, nil, true, 1},
		{"a plain message", ch, map[string]any{"text": "a synthetic plain line"}, nil, true, 0},
		{"@channel where it's suppressed", quiet, map[string]any{"text": "<!channel> a synthetic announcement"}, nil, true, 0},
		{"a user group where @channel is suppressed", quiet, map[string]any{"text": group + "a synthetic call for the crew"}, nil, true, 1},
		{"someone's join", ch, map[string]any{"subtype": "channel_join", "text": "<@" + sender + "> has joined the channel"}, nil, false, 0},
		{"a topic change", ch, map[string]any{"subtype": "channel_topic", "topic": "a synthetic topic",
			"text": "set the channel topic: a synthetic topic"}, nil, false, 0},
		{"a huddle's thread", ch, map[string]any{"subtype": "huddle_thread", "text": ""}, nil, false, 0},
		{"a plain message where every message badges", every, map[string]any{"text": "a synthetic plain line"}, nil, true, 1},
		{"the viewer added by someone", ch, map[string]any{"subtype": "channel_join", "user": fakeslack.SelfID, "inviter": sender,
			"text": me + "has joined the channel"}, nil, true, 1},
	} {
		if tc.before != nil {
			tc.before()
		}
		if got := row(tc.ch, tc.ev); got.Unread != tc.unread || got.Mentions != tc.mentions {
			t.Errorf("%s: unread %v with %d mentions, want %v with %d", tc.name, got.Unread, got.Mentions, tc.unread, tc.mentions)
		}
	}
	// read anywhere: the server's channel_marked clears the row
	read := stamp(time.Now())
	row(ch, map[string]any{"text": me + "a synthetic question", "ts": read})
	fs.Push(map[string]any{"type": "channel_marked", "channel": ch, "ts": read, "unread_count_display": 0, "mention_count_display": 0})
	flush(t, fs, log)
	if c := a.Count(ch); c.Unread || c.Mentions != 0 {
		t.Errorf("read elsewhere (channel_marked): unread %v with %d mentions", c.Unread, c.Mentions)
	}
}

// until polls cond for up to 10 s.
func until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for dl := time.Now().Add(10 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}
