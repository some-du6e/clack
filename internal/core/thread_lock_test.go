package core

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// Locked threads (thread_lock.go): the root's is_locked reaches
// core.Message and isThreadLocked; the socket's message_locked turns it
// on and off on the stored root (messageLocked, locallyUpdateThreadLock)
// and ignores events without a boolean is_locked, a thread_ts, a known
// channel or a stored root.
func TestThreadLocked(t *testing.T) {
	a, _ := testApp(t)
	const root, r1, open = "1.000001", "1.000002", "2.000001"
	var m slack.Message
	if err := json.Unmarshal([]byte(`{"type":"message","ts":"1.000001","thread_ts":"1.000001","user":"U1","text":"a synthetic root",
		"reply_count":1,"is_locked":true,"locked_by_team_id":"T1"}`), &m); err != nil {
		t.Fatal(err)
	}
	if !m.IsLocked || m.LockedByTeamID != "T1" {
		t.Fatalf("decoded is_locked %v, locked_by_team_id %q", m.IsLocked, m.LockedByTeamID)
	}
	a.S.SetThread("C1", root, []slack.Message{m, {TS: r1, ThreadTS: root, User: "U2", Text: "one"}})
	a.S.SetThread("C1", open, []slack.Message{{TS: open, ThreadTS: open, User: "U1", Text: "another root", ReplyCount: 1}})
	if v := a.message("C1", &m); !v.Locked || v.LockedByTeam != "T1" {
		t.Errorf("core.Message locked %v by %q", v.Locked, v.LockedByTeam)
	}
	for _, c := range []struct {
		ch, ts string
		want   bool
	}{
		{"C1", root, true}, {"C1", open, false}, {"C1", "", false}, {"", root, false}, {"C1", "9.000001", false},
	} {
		if got := a.ThreadLocked(c.ch, c.ts); got != c.want {
			t.Errorf("ThreadLocked(%q, %q) = %v, want %v", c.ch, c.ts, got, c.want)
		}
	}

	evs, stop := a.Subscribe()
	defer stop()
	lock := func(fields map[string]any) {
		fields["subtype"] = "message_locked"
		a.handle(rtm("message", fields))
	}
	lock(map[string]any{"channel": "C1", "thread_ts": open, "is_locked": true, "locked_by_team_id": "T1"})
	if e := next(t, evs, "update"); e.Msg.TS != open || !e.Msg.Locked || e.Msg.LockedByTeam != "T1" {
		t.Errorf("message_locked: update %s locked %v by %q", e.Msg.TS, e.Msg.Locked, e.Msg.LockedByTeam)
	}
	if !a.ThreadLocked("C1", open) {
		t.Error("message_locked didn't lock the stored root")
	}
	lock(map[string]any{"channel": "C1", "thread_ts": root, "is_locked": false})
	if e := next(t, evs, "update"); e.Msg.TS != root || e.Msg.Locked {
		t.Errorf("unlock: update %s locked %v", e.Msg.TS, e.Msg.Locked)
	}
	if a.ThreadLocked("C1", root) {
		t.Error("is_locked false left the thread locked")
	}
	// messageLocked's guards: a boolean is_locked and a thread_ts; the
	// channel and the root must be known
	lock(map[string]any{"channel": "C1", "thread_ts": root, "is_locked": "true"})
	lock(map[string]any{"channel": "C1", "thread_ts": root})
	lock(map[string]any{"channel": "C1", "is_locked": true})
	lock(map[string]any{"channel": "C9", "thread_ts": root, "is_locked": true})
	lock(map[string]any{"channel": "C1", "thread_ts": "9.000001", "is_locked": true})
	noUpdates(t, evs)
	if a.ThreadLocked("C1", root) {
		t.Error("an event messageLocked ignores locked the thread")
	}
}

// noUpdates fails on any update left in evs (the guarded events above).
func noUpdates(t *testing.T, evs <-chan Event) {
	t.Helper()
	for {
		select {
		case e := <-evs:
			if e.Type == "update" {
				t.Errorf("an ignored message_locked emitted an update for %s", e.TS)
			}
		default:
			return
		}
	}
}

// useThreadLockedCopy's feat_flag_message_hide_replies read: "on" only.
func TestHideRepliesExperiment(t *testing.T) {
	a, _ := testApp(t)
	for _, c := range []struct {
		group string
		want  bool
	}{{"on", true}, {"off", false}, {"control", false}, {"", false}} {
		a.setExperiments(map[string]string{ExpHideReplies: c.group})
		if got := a.HideRepliesOn(); got != c.want {
			t.Errorf("group %q: HideRepliesOn %v, want %v", c.group, got, c.want)
		}
	}
}
