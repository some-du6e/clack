package fakeslack

// Agent / AI-app fixtures (synthetic; shapes from Slack's docs and web
// client, nothing copied from a real workspace): every plan / task_card
// state, context_actions with feedback_buttons and icon_button, and a
// streaming sequence (chat.startStream → appendStream → stopStream as
// the realtime events a client sees), plus assistant.threads.setStatus /
// setSuggestedPrompts events. blocks.actions calls are recorded.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"
)

const (
	AgentChannelID = "C0FAKEAGENT1"
	AgentBotID     = "B0FAKEAGENT1"
	AgentAppID     = "A0FAKEAGENT1"
	AgentUserID    = "U0FAKEAGENT1"
	AgentThreadTS  = "1762520000.000100"
)

func agentRich(parts ...any) map[string]any {
	var els []any
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			els = append(els, map[string]any{"type": "text", "text": v})
		default:
			els = append(els, v)
		}
	}
	return map[string]any{"type": "rich_text", "elements": []any{map[string]any{"type": "rich_text_section", "elements": els}}}
}

func bold(s string) map[string]any {
	return map[string]any{"type": "text", "text": s, "style": map[string]bool{"bold": true}}
}

func agentTask(id, title, status string, extra map[string]any) map[string]any {
	t := map[string]any{"task_id": id, "title": title, "status": status}
	for k, v := range extra {
		t[k] = v
	}
	return t
}

// AgentFeedback is the reply footer an agent sends: a context line and
// context_actions with feedback_buttons and an icon_button.
func AgentFeedback() []any {
	return []any{
		map[string]any{"type": "context", "block_id": "ctx1", "elements": []any{map[string]any{"type": "mrkdwn", "text": "_done in 6.6s_"}}},
		map[string]any{"type": "context_actions", "block_id": "fb1", "elements": []any{
			map[string]any{"type": "feedback_buttons", "action_id": "feedback",
				"positive_button": map[string]any{"text": map[string]any{"type": "plain_text", "text": "Good response", "emoji": true},
					"accessibility_label": "Mark this response as good", "value": "good-feedback"},
				"negative_button": map[string]any{"text": map[string]any{"type": "plain_text", "text": "Bad response", "emoji": true},
					"accessibility_label": "Mark this response as bad", "value": "bad-feedback"}},
			map[string]any{"type": "icon_button", "action_id": "regenerate", "icon": "refresh", "value": "regen-1",
				"text": map[string]any{"type": "plain_text", "text": "Regenerate"}},
		}},
	}
}

// AgentFixtures are named agent messages, one per block and state.
func AgentFixtures() map[string][]any {
	src := []any{
		map[string]any{"type": "url", "url": "https://example.com/docs/plan", "text": "Plan docs"},
		map[string]any{"type": "url", "url": "https://example.com/wiki/tasks", "text": "Tasks wiki"},
	}
	thinking := []any{
		agentTask("t1", "Read the thread", "complete", map[string]any{"details": agentRich("Two replies, one mention.")}),
		agentTask("t2", "Draft a reply", "complete", map[string]any{"output": agentRich(bold("Reply: "), "on it, what's up?")}),
	}
	return map[string][]any{
		"plan-complete":      {map[string]any{"type": "plan", "block_id": "p1", "title": "Thinking completed", "tasks": thinking}},
		"plan-complete-open": {map[string]any{"type": "plan", "block_id": "p1", "title": "Thinking completed", "tasks": thinking}},
		"plan-in-progress": {map[string]any{"type": "plan", "block_id": "p2", "title": "Thinking…", "tasks": []any{
			agentTask("t1", "Search the docs", "complete", map[string]any{"sources": src}),
			agentTask("t2", "Compare the options", "in_progress", nil),
			agentTask("t3", "Write the answer", "pending", nil),
		}}},
		"plan-error": {map[string]any{"type": "plan", "block_id": "p3", "title": "Couldn't finish", "tasks": []any{
			agentTask("t1", "Fetch the page", "complete", nil),
			agentTask("t2", "Parse the table", "error", map[string]any{"details": agentRich("The page timed out.")}),
		}}},
		"plan-untitled": {map[string]any{"type": "plan", "block_id": "p4", "tasks": []any{
			agentTask("t1", "Look things up", "complete", nil),
			agentTask("t2", "Summarise what I found", "in_progress", nil),
		}}},
		"task-card-pending":     {map[string]any{"type": "task_card", "block_id": "c1", "task_id": "k1", "title": "Queue the build", "status": "pending"}},
		"task-card-in-progress": {map[string]any{"type": "task_card", "block_id": "c2", "task_id": "k2", "title": "Running the tests", "status": "in_progress", "details": agentRich("go test ./...")}},
		"task-card-complete": {map[string]any{"type": "task_card", "block_id": "c3", "task_id": "k3", "title": "Searched the web", "status": "complete",
			"details": agentRich("Looked for Block Kit plan blocks."), "output": agentRich("Found ", bold("2"), " pages."), "sources": src}},
		"task-card-error": {map[string]any{"type": "task_card", "block_id": "c4", "task_id": "k4", "title": "Deploy failed", "status": "error", "output": agentRich("exit status 1")}},
		"task-card-mcp":   {map[string]any{"type": "task_card", "block_id": "mcp_task_1", "task_id": "k5", "title": "Called a tool", "status": "complete", "output": agentRich("42 rows")}},
		"reply-feedback": append([]any{map[string]any{"type": "rich_text", "block_id": "r1", "elements": []any{
			map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": "Sure, here's the answer. "},
				map[string]any{"type": "emoji", "name": "sweat_smile", "unicode": "1f605"}}}}}}, AgentFeedback()...),
	}
}

// AgentMessage wraps fixture blocks as the agent's thread reply.
func AgentMessage(ts string, blocks []any, streaming string) map[string]any {
	m := map[string]any{"type": "message", "ts": ts, "user": AgentUserID, "bot_id": AgentBotID, "app_id": AgentAppID,
		"text": "agent reply", "thread_ts": AgentThreadTS, "blocks": blocks, "channel": AgentChannelID,
		"bot_profile": map[string]any{"id": AgentBotID, "app_id": AgentAppID, "user_id": AgentUserID, "name": "agent", "team_id": TeamID}}
	if streaming != "" {
		m["streaming_state"] = streaming
	}
	return m
}

// AgentStream is a streamed reply as realtime events, in order: status
// set, startStream (plan in progress), appendStream ×2, stopStream
// (completed, feedback footer), status cleared.
func AgentStream(ts string) []map[string]any {
	plan := func(s1, s2 string) map[string]any {
		return map[string]any{"type": "plan", "block_id": "sp", "title": "Thinking…", "tasks": []any{
			agentTask("t1", "Read the thread", s1, nil), agentTask("t2", "Draft a reply", s2, nil)}}
	}
	text := func(s string) map[string]any {
		return map[string]any{"type": "rich_text", "block_id": "st", "elements": []any{map[string]any{"type": "rich_text_section",
			"elements": []any{map[string]any{"type": "text", "text": s}}}}}
	}
	changed := func(m map[string]any) map[string]any {
		return map[string]any{"type": "message", "subtype": "message_changed", "channel": AgentChannelID, "message": m, "ts": ts, "hidden": true}
	}
	status := func(s string) map[string]any {
		return map[string]any{"type": "ai_assistant_status", "channel_id": AgentChannelID, "thread_ts": AgentThreadTS,
			"bot_user_id": AgentUserID, "status": s, "status_type": "typing", "loading_messages": []string{"Reading…", "Writing…"}}
	}
	done := plan("complete", "complete")
	done["title"] = "Thinking completed"
	final := append([]any{done, text("Hello! Here's the whole answer.")}, AgentFeedback()...)
	first := AgentMessage(ts, []any{plan("in_progress", "pending")}, "in_progress")
	return []map[string]any{
		status("is thinking…"),
		first,
		changed(AgentMessage(ts, []any{plan("complete", "in_progress"), text("Hello!")}, "in_progress")),
		changed(AgentMessage(ts, []any{plan("complete", "in_progress"), text("Hello! Here's the")}, "in_progress")),
		changed(AgentMessage(ts, final, "completed")),
		status(""),
		{"type": "ai_assistant_threads_suggested_prompts", "channel_id": AgentChannelID, "thread_ts": AgentThreadTS,
			"bot_user_id": AgentUserID, "title": "Try these", "prompts": []any{
				map[string]any{"title": "Summarise", "message": "Summarise this thread"},
				map[string]any{"title": "Next steps", "message": "What should I do next?"}}},
	}
}

// PlayAgentStream pushes AgentStream to every socket, gap apart.
func (s *Server) PlayAgentStream(ts string, gap time.Duration) {
	for i, ev := range AgentStream(ts) {
		if i > 0 && gap > 0 {
			time.Sleep(gap)
		}
		s.Push(ev)
	}
}

// ---- blocks.actions recorder ----

type agentRec struct {
	mu      sync.Mutex
	actions []url.Values
}

var agentRecs sync.Map // *Server → *agentRec

func (s *Server) agentRec() *agentRec {
	v, _ := agentRecs.LoadOrStore(s, &agentRec{})
	return v.(*agentRec)
}

// BlockActions returns every blocks.actions call's form, in order.
func (s *Server) BlockActions() []url.Values {
	r := s.agentRec()
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]url.Values(nil), r.actions...)
}

// handleAgents answers blocks.actions (recorded) and the agent channel's
// history and replies (the fixtures, oldest first in replies).
func (s *Server) handleAgents(method string, f url.Values) (any, bool) {
	switch method {
	case "blocks.actions":
		r := s.agentRec()
		r.mu.Lock()
		r.actions = append(r.actions, f)
		r.mu.Unlock()
		return ok(), true
	case "conversations.history", "conversations.replies":
		if f.Get("channel") != AgentChannelID {
			return nil, false
		}
		names := []string{"plan-complete", "plan-in-progress", "plan-error", "plan-untitled", "task-card-pending",
			"task-card-in-progress", "task-card-complete", "task-card-error", "task-card-mcp", "reply-feedback"}
		fx := AgentFixtures()
		msgs := []any{map[string]any{"type": "message", "ts": AgentThreadTS, "user": SelfID, "text": "hey agent", "thread_ts": AgentThreadTS,
			"reply_count": len(names)}}
		for i, n := range names {
			msgs = append(msgs, AgentMessage(fmt.Sprintf("1762520%03d.000200", i+1), fx[n], "completed"))
		}
		if method == "conversations.history" {
			msgs = msgs[:1]
		}
		b, _ := json.Marshal(msgs)
		return ok("messages", json.RawMessage(b), "has_more", false), true
	}
	return nil, false
}
