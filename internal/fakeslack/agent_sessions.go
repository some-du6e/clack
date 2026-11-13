package fakeslack

import (
	"net/url"
	"strconv"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Agent sessions for the rail's Agents & tools peek (Size.AgentSessions):
// ai.alpha.agents.sessions.list as Slack's client reads it (boot-shared
// p9pF fetchAgentSessions, XsVf AgentSessionsPeekList): "thread" sessions
// carry their root message (agent_session.title / agent_bot_user_ids /
// status, assistant_app_thread.title, latest_reply, last_read), "channel"
// sessions their channel_id, channel object and last_activity_ts;
// is_pinned and is_archived. Every name and text is made up, and the
// times are relative to now so the rows read "Updated 5 hours ago",
// "Updated 1 day ago"…
//
// Without Size.AgentSessions the method answers an empty list ("No agent
// conversations yet").

const (
	// AgentSessionsBotID is the sessions' agent (a bot user with a picture).
	AgentSessionsBotID = "U0FAKEAGSS"
	// AgentSessionsDMID is the agent's DM, where the thread sessions live.
	AgentSessionsDMID = "D0FAKEAGSS1"
	// AgentSessionsChannelID is the channel session's channel.
	AgentSessionsChannelID = "C0FAKEAGSS1"
)

// AgentSessionTS is thread session i's root ts (0: pinned "Plan the Q4
// launch", 1: the assistant-titled one, 2: working, 3: needs input, 4:
// titled from its text, 5: archived).
func AgentSessionTS(i int) string { return strconv.Itoa(1762900000+i*100) + ".000200" }

// agentSessionsUser is the sessions' agent.
func (s *Server) agentSessionsUser() slack.User {
	n := "atlas"
	return slack.User{ID: AgentSessionsBotID, TeamID: TeamID, Name: n, RealName: "Atlas Agent", IsBot: true, AgentStatus: "consistently_an_agent",
		Profile: slack.Profile{RealName: "Atlas Agent", Team: TeamID, Fields: []byte(`{}`), Image24: s.identityImg(24, n), Image32: s.identityImg(32, n),
			Image48: s.identityImg(48, n), Image72: s.identityImg(72, n), Image192: s.identityImg(192, n), Image512: s.identityImg(512, n)}}
}

// AgentSessionsNow pins the fixtures' clock (zero: time.Now).
var AgentSessionsNow func() time.Time

func agentSessionsNow() time.Time {
	if AgentSessionsNow != nil {
		return AgentSessionsNow()
	}
	return time.Now()
}

func tsAgo(now time.Time, d time.Duration) string {
	return strconv.FormatInt(now.Add(-d).Unix(), 10) + ".000100"
}

// agentSessions is sessions.list's sessions, Slack's order.
func (s *Server) agentSessions() []map[string]any {
	now := agentSessionsNow()
	root := func(i int, text string, session map[string]any, extra map[string]any) map[string]any {
		m := map[string]any{"type": "message", "ts": AgentSessionTS(i), "user": SelfID, "text": text, "channel": AgentSessionsDMID,
			"thread_ts": AgentSessionTS(i), "reply_count": 2, "agent_session": session}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	bots := []string{AgentSessionsBotID}
	thread := func(m map[string]any, pinned, archived bool) map[string]any {
		return map[string]any{"session_type": "thread", "thread": m, "is_pinned": pinned, "is_archived": archived}
	}
	return []map[string]any{
		thread(root(0, "Help me plan the Q4 launch", map[string]any{"title": "Plan the Q4 launch", "agent_bot_user_ids": bots, "status": "active"},
			map[string]any{"latest_reply": tsAgo(now, 5*time.Hour), "last_read": tsAgo(now, 5*time.Hour)}), true, false),
		{"session_type": "channel", "channel_id": AgentSessionsChannelID, "is_pinned": false, "is_archived": false,
			"last_activity_ts": tsAgo(now, 3*time.Hour),
			"channel": map[string]any{"id": AgentSessionsChannelID, "name": "agent-fix-login", "is_channel": true, "is_member": true,
				"properties": map[string]any{"agent_session": map[string]any{"encoded_agent_bot_user_ids": bots}}}},
		thread(root(1, "can you summarize this week?", map[string]any{"agent_bot_user_ids": bots, "status": "active",
			"agent_statuses": []map[string]any{{"agent_bot_user_id": AgentSessionsBotID, "icon_emoji": ":robot_face:"}}},
			map[string]any{"assistant_app_thread": map[string]any{"title": "Summarize the week"}, "latest_reply": tsAgo(now, 26*time.Hour),
				"last_read": tsAgo(now, 30*time.Hour)}), false, false),
		thread(root(2, "Draft the onboarding doc", map[string]any{"title": "Draft the onboarding doc", "agent_bot_user_ids": bots,
			"status": "processing", "date_status_processing_expire": now.Add(10 * time.Minute).Unix()},
			map[string]any{"latest_reply": tsAgo(now, 2*time.Minute)}), false, false),
		thread(root(3, "Book the offsite", map[string]any{"title": "Book the offsite", "agent_bot_user_ids": bots, "status": "suspended"},
			map[string]any{"latest_reply": tsAgo(now, 50*time.Minute)}), false, false),
		thread(root(4, "**Triage** the open bugs", map[string]any{"agent_bot_user_ids": bots, "status": "active"},
			map[string]any{"latest_reply": tsAgo(now, 4*24*time.Hour), "last_read": tsAgo(now, 4*24*time.Hour)}), false, false),
		thread(root(5, "An archived session", map[string]any{"title": "Archived", "agent_bot_user_ids": bots}, nil), false, true),
	}
}

// handleAgentSessions answers ai.alpha.agents.sessions.list and
// workflows.triggers.listRecentlyRunForUser (none without Size.Workflows).
func (s *Server) handleAgentSessions(method string, f url.Values) (any, bool) {
	if method == "workflows.triggers.listRecentlyRunForUser" {
		if !s.Size.Workflows {
			return ok("recently_run_triggers", []any{}, "recently_run_triggers_with_timestamps", []any{}), true
		}
		return s.recentlyRun(), true
	}
	switch method {
	case "agents.threads.pin", "agents.threads.unpin", "agents.threads.archive", "agents.threads.rename":
		// the session menu's writes (boot-vendors LSN0 / KDGJ / ek0Z /
		// d+ht): {channel, thread_ts (threads only), reason[, name]}
		if f.Get("channel") == "" {
			return fail("channel_not_found"), true
		}
		if method == "agents.threads.rename" && f.Get("name") == "" {
			return fail("invalid_name"), true
		}
		e := url.Values{"method": {method}}
		for k, v := range f {
			if k != "token" {
				e[k] = v
			}
		}
		s.convMu.Lock()
		s.sessionEdits = append(s.sessionEdits, e)
		s.convMu.Unlock()
		return ok(), true
	case "ai.alpha.agents.sessions.list":
	default:
		return nil, false
	}
	s.convMu.Lock()
	s.sessionReasons = append(s.sessionReasons, f.Get("reason"))
	edits := append([]url.Values(nil), s.sessionEdits...)
	s.convMu.Unlock()
	if !s.Size.AgentSessions {
		return ok("sessions", []any{}), true
	}
	return ok("sessions", applySessionEdits(s.agentSessions(), edits)), true
}

// applySessionEdits lays the pins, archives and renames so far over the
// fixtures: what the server would list now.
func applySessionEdits(ss []map[string]any, edits []url.Values) []map[string]any {
	for _, e := range edits {
		ch, ts := e.Get("channel"), e.Get("thread_ts")
		for _, m := range ss {
			id, rts := "", ""
			var root map[string]any
			if r, ok := m["thread"].(map[string]any); ok {
				root = r
				id, _ = r["channel"].(string)
				rts, _ = r["ts"].(string)
			} else {
				id, _ = m["channel_id"].(string)
			}
			if id != ch || rts != ts {
				continue
			}
			switch e.Get("method") {
			case "agents.threads.pin":
				m["is_pinned"] = true
			case "agents.threads.unpin":
				m["is_pinned"] = false
			case "agents.threads.archive":
				m["is_archived"] = true
			case "agents.threads.rename":
				if root != nil {
					if as, ok := root["agent_session"].(map[string]any); ok {
						as["title"] = e.Get("name")
					}
				}
			}
		}
	}
	return ss
}

// AgentSessionEdits are the agents.threads.* writes so far, each with its
// "method".
func (s *Server) AgentSessionEdits() []url.Values {
	s.convMu.Lock()
	defer s.convMu.Unlock()
	return append([]url.Values(nil), s.sessionEdits...)
}

// AgentSessionsReasons are the reasons ai.alpha.agents.sessions.list was
// called with, in order.
func (s *Server) AgentSessionsReasons() []string {
	s.convMu.Lock()
	defer s.convMu.Unlock()
	return append([]string(nil), s.sessionReasons...)
}

// RecentlyRunTriggers are workflows.triggers.listRecentlyRunForUser's
// triggers, newest run first (Size.Workflows).
var RecentlyRunTriggers = []string{TriggerDeploy, TriggerStandup, TriggerTimeOff}

// recentlyRun answers workflows.triggers.listRecentlyRunForUser:
// recently_run_triggers and recently_run_triggers_with_timestamps.
func (s *Server) recentlyRun() map[string]any {
	now := agentSessionsNow()
	var trig, stamped []map[string]any
	for i, id := range RecentlyRunTriggers {
		for _, t := range fakeTriggers {
			if t.id == id {
				j := t.json(s.URL)
				trig = append(trig, j)
				stamped = append(stamped, map[string]any{"timestamp": now.Add(-time.Duration(i+1) * time.Hour).Unix(), "trigger": j})
			}
		}
	}
	return ok("recently_run_triggers", trig, "recently_run_triggers_with_timestamps", stamped)
}
