package core

import (
	"context"
	"testing"
)

// fetchAgentSessions (p9pF): ai.alpha.agents.sessions.list with reason
// recent_sessions_sidebar; archived sessions are left out; a thread
// session is its root (title, agent, identity override), a channel
// session its channel; getAgentSessionTitle's order.
func TestAgentSessions(t *testing.T) {
	a, f := testApp(t)
	f.reply["ai.alpha.agents.sessions.list"] = `{"ok":true,"sessions":[
	 {"session_type":"thread","is_pinned":true,"thread":{"type":"message","ts":"1700000000.000100","channel":"DAG1","user":"U1","text":"hi","latest_reply":"1700000500.000100","last_read":"1700000400.000100",
	   "agent_session":{"title":"Plan it","agent_bot_user_ids":["UBOT1"],"status":"active","agent_statuses":[{"agent_bot_user_id":"UBOT1","icon_emoji":":robot_face:"}]}}},
	 {"session_type":"channel","channel_id":"CAG1","last_activity_ts":"1700000600.000100","channel":{"id":"CAG1","name":"agent-work","is_channel":true,"properties":{"agent_session":{"encoded_agent_bot_user_ids":["UBOT2"]}}}},
	 {"session_type":"thread","thread":{"type":"message","ts":"1700000001.000100","channel":"DAG1","text":"x","assistant_app_thread":{"title":"From the assistant"},"agent_session":{"agent_bot_user_ids":["UBOT1"]}}},
	 {"session_type":"thread","thread":{"type":"message","ts":"1700000002.000100","channel":"DAG1","subtype":"huddle_thread","text":"","agent_session":{"agent_statuses":{"weird":"shape"}}}},
	 {"session_type":"thread","thread":{"type":"message","ts":"1700000003.000100","channel":"DAG1","text":""}},
	 {"session_type":"thread","is_archived":true,"thread":{"type":"message","ts":"1700000004.000100","channel":"DAG1","text":"gone"}},
	 {"session_type":"channel","channel_id":""}
	]}`
	ss, err := a.AgentSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v := f.last("ai.alpha.agents.sessions.list"); v.Get("reason") != "recent_sessions_sidebar" {
		t.Errorf("reason %q", v.Get("reason"))
	}
	if len(ss) != 5 {
		t.Fatalf("%d sessions, want 5 (no archived, no channel without an id)", len(ss))
	}
	plain := func(m *Message) string { return m.Text }
	names := map[string]string{"DAG1": "kite", "CAG1": "agent-work"}
	name := func(ch string) string { return names[ch] }
	s := ss[0]
	if s.Kind != "thread" || !s.Pinned || s.BotUserID != "UBOT1" || s.IconEmoji != ":robot_face:" || s.RootLastRead != "1700000400.000100" {
		t.Errorf("thread session %+v", s)
	}
	if s.Title(plain, name) != "Plan it" || s.ActivityTS() != "1700000500.000100" {
		t.Errorf("title %q activity %q", s.Title(plain, name), s.ActivityTS())
	}
	c := ss[1]
	if c.Kind != "channel" || c.Channel != "CAG1" || c.BotUserID != "UBOT2" || c.Title(plain, name) != "agent-work" || c.ActivityTS() != "1700000600.000100" {
		t.Errorf("channel session %+v title %q", c, c.Title(plain, name))
	}
	if got := ss[2].Title(plain, name); got != "From the assistant" {
		t.Errorf("assistant title %q", got)
	}
	// an agent_statuses shape Clack doesn't know doesn't break the message
	if got := ss[3].Title(plain, name); got != "Huddle in #kite" {
		t.Errorf("huddle title %q", got)
	}
	if got := ss[4].Title(plain, name); got != "Untitled session" {
		t.Errorf("empty title %q", got)
	}
}

// fetchAndUpsertRecentlyRunTriggers (h9ME): listRecentlyRunForUser's
// recently_run_triggers in order, known to Trigger afterwards.
func TestRecentlyRunTriggers(t *testing.T) {
	a, f := testApp(t)
	f.reply["workflows.triggers.listRecentlyRunForUser"] = `{"ok":true,"recently_run_triggers":[
	 {"id":"Ft2","type":"shortcut","name":"Second","workflow":{"workflow_id":"Wf2","title":"W2","app":{"icons":{"image_36":"https://x/36.png"}}}},
	 {"id":"Ft1","type":"scheduled","name":"","workflow":{"workflow_id":"Wf1","title":"Weekly","description":"Every Monday"}}],
	 "recently_run_triggers_with_timestamps":[{"timestamp":1700000000,"trigger":{"id":"Ft2"}}]}`
	ts, err := a.RecentlyRunTriggers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v := f.last("workflows.triggers.listRecentlyRunForUser"); v.Get("reason") != "fetch_and_upsert_recently_run_triggers" {
		t.Errorf("reason %q", v.Get("reason"))
	}
	if len(ts) != 2 || ts[0].ID != "Ft2" || ts[1].ID != "Ft1" || ts[0].Icon != "https://x/36.png" || ts[1].Title() != "Weekly" || ts[1].Desc() != "Every Monday" {
		t.Fatalf("triggers %+v", ts)
	}
	if _, ok := a.Interactive().Trigger("Ft1"); !ok {
		t.Error("the trigger store doesn't know Ft1")
	}
}
