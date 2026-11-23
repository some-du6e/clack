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
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
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
		// a plan whose tasks' details run past Slack's 3 lines / 250
		// characters (TaskItem cuts them, with Show more)
		"plan-long-details": {map[string]any{"type": "plan", "block_id": "p5", "title": "Thinking completed", "tasks": []any{
			agentTask("t1", "Read the failing tests", "complete", map[string]any{"details": agentRich(AgentLongDetail)}),
			agentTask("t2", "Traced the clock", "complete", map[string]any{"details": agentRich("Opened the fixture.\nFound the start date.\nIt is a year early.\nEvery relative date shifts.\nFixed the seed.")}),
			agentTask("t3", "Ran them again", "complete", map[string]any{"details": agentRich("All green.")}),
		}}},
		// a plan an app updates with chat.update (the same block, as the
		// plan posts in the account's channels are): its first task
		// running; then done, with details, and a second one running; and
		// collapsed, its title grown past a line
		"plan-grow-1": {map[string]any{"type": "plan", "block_id": "p6", "title": "Checking the release", "tasks": []any{
			agentTask("t1", "Read the changelog", "in_progress", nil)}}},
		"plan-grow-2": {map[string]any{"type": "plan", "block_id": "p6", "title": "Checking the release", "tasks": []any{
			agentTask("t1", "Read the changelog", "complete", map[string]any{"details": agentRich("Three fixes and one new setting.")}),
			agentTask("t2", "Run the smoke tests", "in_progress", nil)}}},
		"plan-grow-title": {map[string]any{"type": "plan", "block_id": "p6", "title": "Checking the release: the changelog, the smoke tests, the migration notes and the rollout plan for every region", "tasks": []any{
			agentTask("t1", "Read the changelog", "in_progress", nil)}}},
		// a plan whose tasks carry icons (TaskIcon draws a known one for a
		// task neither running nor failed): a rocket, a bug, one running
		// and one failed (their status wins), a lightbulb (Slack's
		// emoji-objects), a name Block Kit doesn't know and a bare string
		"plan-icons": {map[string]any{"type": "plan", "block_id": "p7", "title": "Shipping the release", "tasks": []any{
			agentTask("t1", "Built the release", "complete", map[string]any{"icon": map[string]any{"name": "rocket"}}),
			agentTask("t2", "Filed the regression", "pending", map[string]any{"icon": map[string]any{"name": "bug"}}),
			agentTask("t3", "Checking the mirrors", "in_progress", map[string]any{"icon": map[string]any{"name": "globe"}}),
			agentTask("t4", "Tagging failed", "error", map[string]any{"icon": map[string]any{"name": "flag"}}),
			agentTask("t5", "Wrote the notes", "complete", map[string]any{"icon": map[string]any{"name": "lightbulb"}}),
			agentTask("t6", "Cleaned up", "complete", map[string]any{"icon": map[string]any{"name": "not-an-icon"}}),
			agentTask("t7", "Shared it", "complete", map[string]any{"icon": "share"}),
		}}},
		// a plan task's header with its caret: a one-line title of x-height
		// letters only, and one that wraps
		"plan-caret": {map[string]any{"type": "plan", "block_id": "p8", "title": "Reviewed", "tasks": []any{
			agentTask("t1", "run some scans", "complete", map[string]any{"details": agentRich("All clear.")})}}},
		"plan-caret-wrap": {map[string]any{"type": "plan", "block_id": "p9", "title": "Reviewed", "tasks": []any{
			agentTask("t1", "Read every failing test, traced the clock in the fixture, checked the seed and ran the suite again", "complete",
				map[string]any{"details": agentRich("All clear.")})}}},
		// a plan whose update puts a task ahead of the others (the same
		// block, chat.update)
		"plan-follow-1": {map[string]any{"type": "plan", "block_id": "p10", "title": "Checking the release", "tasks": []any{
			agentTask("t1", "Read the changelog", "complete", map[string]any{"details": agentRich("Three fixes.")}),
			agentTask("t2", "Ran the smoke tests", "complete", map[string]any{"details": agentRich("All green.")})}}},
		"plan-follow-2": {map[string]any{"type": "plan", "block_id": "p10", "title": "Checking the release", "tasks": []any{
			agentTask("t0", "Fetched the branch", "complete", map[string]any{"details": agentRich("Up to date.")}),
			agentTask("t1", "Read the changelog", "complete", map[string]any{"details": agentRich("Three fixes.")}),
			agentTask("t2", "Ran the smoke tests", "complete", map[string]any{"details": agentRich("All green.")})}}},
		// an MCP task's card while it runs, and once it is done (same
		// block: the stream's update)
		"task-card-mcp-running": {map[string]any{"type": "task_card", "block_id": "mcp_task_2", "task_id": "k6", "title": "Querying the warehouse", "status": "in_progress", "output": agentRich("Reading the orders table.")}},
		"task-card-mcp-done":    {map[string]any{"type": "task_card", "block_id": "mcp_task_2", "task_id": "k6", "title": "Querying the warehouse", "status": "complete", "output": agentRich("Read 1,204 orders.")}},
		"reply-feedback": append([]any{map[string]any{"type": "rich_text", "block_id": "r1", "elements": []any{
			map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": "Sure, here's the answer. "},
				map[string]any{"type": "emoji", "name": "sweat_smile", "unicode": "1f605"}}}}}}, AgentFeedback()...),
	}
}

// AgentLongDetail is plan-long-details' first task's details: past the
// 250 characters a slim task shows before Show more.
const AgentLongDetail = "Read the three failing tests and the stack traces from the last CI run, then traced each failure back to the helper " +
	"that builds the fake workspace: the fixture's clock starts a year early, so every relative date in the assertions is off by " +
	"twelve months, which is why only the tests that print a date fail and the rest of the suite stays green."

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

// AgentStoppedReply is the agent's reply whose stream the viewer stopped:
// ai_context result_status stopped_by_user (Slack's StoppedLabel).
func AgentStoppedReply(ts string) map[string]any {
	m := AgentMessage(ts, []any{map[string]any{"type": "rich_text", "block_id": "sr", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": "Here is the start of an answer"}}}}}},
		"completed")
	m["ai_context"] = map[string]any{"result_status": "stopped_by_user", "stopped_by_user_id": SelfID}
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

var agentRecs = newPerServer() // *Server → *agentRec

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
	case "bots.info":
		if s.Size.AppPlans && (f.Get("bot") == AppPlanBotID || strings.Contains(f.Get("bots"), AppPlanBotID)) {
			return map[string]any{"ok": true, "bot": appPlanBot(), "bots": []slack.BotProfile{appPlanBot()}}, true
		}
		return nil, false
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

// ---- a custom app's plan post (synthetic) ----

// AppPlanChannelID is the channel AppPlanMessages post in.
const (
	AppPlanChannelID = "C0FAKEPLAN01"
	AppPlanBotID     = "B0FAKEPLAN01"
	AppPlanAppID     = "A0FAKEPLAN01"
	AppPlanUserID    = "U0FAKEPLAN01"
	// AppPlanBotName is the app's name: one U+202E (RIGHT-TO-LEFT
	// OVERRIDE), as a real custom app had it.
	AppPlanBotName = "‮"
)

// AppPlanPlan is a plan block of every task status, each task's details
// rich text (bold, code, a link).
func AppPlanPlan() map[string]any {
	link := map[string]any{"type": "link", "url": "https://example.com/build/42", "text": "build 42"}
	code := map[string]any{"type": "text", "text": "go vet", "style": map[string]bool{"code": true}}
	return map[string]any{"type": "plan", "block_id": "plan_mixed_01", "title": "Checking the release", "tasks": []any{
		agentTask("1", "Read the changelog", "complete", map[string]any{"details": agentRich("Three fixes, ", bold("one"), " breaking change.")}),
		agentTask("2", "Run the checks", "error", map[string]any{"details": agentRich(code, " failed in ", link, ".")}),
		agentTask("3", "Compare with the last release", "in_progress", map[string]any{"details": agentRich("Reading the diff.")}),
		agentTask("4", "Write the notes", "pending", nil),
	}}
}

// AppPlanMessages are a custom app's top-level posts, newest first, in
// the shape such an app sends them (chat.postMessage, then
// chat.update): user, bot_id, app_id, bot_profile, edited, a short
// fallback text, and blocks:
//
//   - "plan": one plan block, two complete tasks with rich_text details
//     whose rich_text carries its own block_id (the shape of the post
//     Clack drew blank, 2026-11-15)
//   - "mixed": AppPlanPlan
//   - "unknown": a block type no Slack client knows, then rich text
//   - "unknown-only": only the unknown block
func AppPlanMessages() (msgs []slack.Message, names []string) {
	details := func(s string) map[string]any {
		d := agentRich(s)
		d["block_id"] = "dt" + s[:2]
		return d
	}
	posts := []struct {
		name, text string
		blocks     []any
	}{
		{"plan", "Looked over the open issues", []any{map[string]any{"type": "plan", "block_id": "plan_k8f3m2q9x1z7w4b6v0",
			"title": "Looked over the open issues", "tasks": []any{
				agentTask("t01", "Listed the issues opened this week", "complete", map[string]any{"details": details("Nine new issues, four of them about the composer losing focus after a paste.")}),
				agentTask("t02", "Grouped them by the part of the app they touch", "complete", map[string]any{"details": details("Composer: four. Sidebar: three. Search: two. None needs a release on its own.")}),
			}}}},
		{"mixed", "Checking the release", []any{AppPlanPlan()}},
		{"unknown", "A block from the future", []any{map[string]any{"type": "hologram", "block_id": "h1", "beams": 3},
			map[string]any{"type": "rich_text", "block_id": "r1", "elements": []any{map[string]any{"type": "rich_text_section",
				"elements": []any{map[string]any{"type": "text", "text": "Text after the hologram."}}}}}}},
		{"unknown-only", "Only the fallback text", []any{map[string]any{"type": "hologram", "block_id": "h2"}}},
	}
	base := 1794761000
	for i, p := range posts {
		raw, _ := json.Marshal(p.blocks)
		ts := fmt.Sprintf("%d.%06d", base+i*60, 100)
		m := slack.Message{Type: "message", TS: ts, User: AppPlanUserID, BotID: AppPlanBotID, Text: p.text, Blocks: raw,
			Edited:     &slack.Edited{User: AppPlanUserID, TS: fmt.Sprintf("%d.000000", base+i*60+5)},
			BotProfile: &slack.BotProfile{ID: AppPlanBotID, Name: AppPlanBotName, AppID: AppPlanAppID, UserID: AppPlanUserID, TeamID: TeamID}}
		msgs = append([]slack.Message{m}, msgs...)
		names = append([]string{p.name}, names...)
	}
	return msgs, names
}

// addAppPlans adds #plans, the app's bot user and AppPlanMessages
// (Size.AppPlans).
func (s *Server) addAppPlans() {
	s.convs = append(s.convs, slack.Conversation{ID: AppPlanChannelID, Name: "plans", IsChannel: true, IsMember: true,
		Created: 1700000000, Creator: SelfID, NumMembers: 2, ContextTeamID: TeamID})
	s.users = append(s.users, slack.User{ID: AppPlanUserID, TeamID: TeamID, Name: "eth", IsBot: true,
		Profile: slack.Profile{DisplayName: "eth", RealName: "eth", Team: TeamID}})
	msgs, _ := AppPlanMessages()
	s.hist[AppPlanChannelID] = msgs
}

// appPlanBot is bots.info's answer for the app.
func appPlanBot() slack.BotProfile {
	return slack.BotProfile{ID: AppPlanBotID, Name: AppPlanBotName, AppID: AppPlanAppID, UserID: AppPlanUserID, TeamID: TeamID}
}

// PushAppPlanLive posts a plan as the app does live: the message (its
// tasks in progress), then chat.update's message_changed (all complete,
// edited). It returns the message's ts.
func (s *Server) PushAppPlanLive() string {
	plan := func(st1, st2 string) []any {
		return []any{map[string]any{"type": "plan", "block_id": "plan_live_00000000001", "title": "Read the new issues", "tasks": []any{
			agentTask("t01", "Listed the issues", st1, map[string]any{"details": agentRich("Two new issues.")}),
			agentTask("t02", "Labelled them", st2, map[string]any{"details": agentRich("Both are composer bugs.")}),
		}}}
	}
	ts, _ := s.PostQuiet(AppPlanChannelID, AppPlanUserID, "Read the new issues")
	msg := func(blocks []any, edited bool) map[string]any {
		m := map[string]any{"type": "message", "ts": ts, "user": AppPlanUserID, "bot_id": AppPlanBotID, "app_id": AppPlanAppID,
			"team": TeamID, "text": "Read the new issues", "blocks": blocks, "channel": AppPlanChannelID,
			"bot_profile": map[string]any{"id": AppPlanBotID, "app_id": AppPlanAppID, "user_id": AppPlanUserID, "name": AppPlanBotName,
				"team_id": TeamID, "deleted": false, "updated": 1794000000, "icons": map[string]any{}}}
		if edited {
			m["edited"] = map[string]any{"user": AppPlanUserID, "ts": ts}
		}
		return m
	}
	first, done := plan("in_progress", "pending"), plan("complete", "complete")
	s.histMu.Lock()
	if h := s.hist[AppPlanChannelID]; len(h) > 0 && h[0].TS == ts {
		raw, _ := json.Marshal(done)
		h[0].BotID, h[0].Blocks = AppPlanBotID, raw
		h[0].Edited = &slack.Edited{User: AppPlanUserID, TS: ts}
		h[0].BotProfile = &slack.BotProfile{ID: AppPlanBotID, Name: AppPlanBotName, AppID: AppPlanAppID, UserID: AppPlanUserID, TeamID: TeamID}
	}
	s.histMu.Unlock()
	ev := msg(first, false)
	ev["event_ts"] = ts
	s.Push(ev)
	time.Sleep(300 * time.Millisecond)
	s.Push(map[string]any{"type": "message", "subtype": "message_changed", "channel": AppPlanChannelID, "hidden": true,
		"ts": ts, "event_ts": ts, "message": msg(done, true), "previous_message": msg(first, false)})
	return ts
}
