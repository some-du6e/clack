package fakeslack

// Code channels (Slack Code) fixtures, Size.CodeChannels: one channel per
// agent status, with context bar links and actions, published views
// (diff, html, block kit, canvas), artifact messages, an archived one and
// one the viewer isn't in. The property shapes follow the live #test
// channel (2026-11-08) and Slack's web client; see docs/CODE-CHANNELS.md.
// Synthetic data only.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Code channel fixture ids.
const (
	CodeAgentID       = "U0AGENT0001" // the agent's bot user
	CodeAgentBotID    = "B0AGENT0001"
	CodeWorkingID     = "C0CODE00001" // processing (the working chip)
	CodeWaitingID     = "C0CODE00002" // suspended: needs attention
	CodeDoneID        = "C0CODE00003" // closed: done
	CodeIdleID        = "C0CODE00004" // active, PR open
	CodeStaleID       = "C0CODE00005" // active, quiet for 5 days: stale
	CodeEmptyID       = "C0CODE00006" // active, code_channel {} (as #test): "No branch info"
	CodeArchivedID    = "C0CODE00007" // archived when the work was done
	CodePreviewID     = "C0CODE00008" // a code channel the viewer isn't in
	codeChannelRecord = "AC:9991336848048:852294937191:"
)

// CodeFixtures is the code channels' data.
type CodeFixtures struct {
	Agent    slack.User
	Convs    []slack.Conversation // the viewer's
	Others   []slack.Conversation // archived and preview
	History  map[string][]slack.Message
	Latest   map[string]string // each channel's latest ts (client.counts)
	Statuses map[string]string // the effective status each one should show
}

// CodeChannelFixtures builds the fixtures as of now.
func CodeChannelFixtures(now time.Time) CodeFixtures {
	ts := func(ago time.Duration, n int) string {
		return strconv.FormatInt(now.Add(-ago).Unix(), 10) + fmt.Sprintf(".%06d", n)
	}
	props := func(id, status, title string, cc map[string]any, extra map[string]any) json.RawMessage {
		sess := map[string]any{"status": status, "title": title, "agent_bot_user_ids": []any{},
			"encoded_agent_bot_user_ids": []string{CodeAgentID}}
		for k, v := range extra {
			sess[k] = v
		}
		b, _ := json.Marshal(map[string]any{
			"record_channel": map[string]any{"record_id": codeChannelRecord + id, "record_type": "agent_channel",
				"record_label": "Code Channel", "record_label_plural": "Code Channels"},
			"code_channel":  cc,
			"agent_session": sess,
		})
		return b
	}
	links := []map[string]any{
		{"key": "pr", "label": "PR #482", "icon": "branch", "url": "https://github.com/example/app/pull/482"},
		{"key": "preview", "label": "Preview deploy", "icon": "globe", "url": "https://example-app-git-fix.vercel.app"},
		{"key": "session", "label": "Session", "icon": "terminal", "url": "https://example.com/sessions/7"},
		{"key": "rerun", "item_type": "action", "label": "Re-run checks", "icon": "search"},
	}
	views := []map[string]any{
		{"view_id": "V0SUMMARY", "type": "block_kit", "name": "Summary", "content_version": 2},
		{"view_id": "V0PR", "type": "pull_request", "pr_url": "https://github.com/example/app/pull/482", "name": "#482"},
	}
	conv := func(id, name string, p json.RawMessage) slack.Conversation {
		return slack.Conversation{ID: id, Name: name, IsChannel: true, IsMember: true, Created: now.Add(-6 * 24 * time.Hour).Unix(),
			Creator: SelfID, NumMembers: 2, ContextTeamID: TeamID, Properties: p,
			Purpose: &slack.Purpose{Value: "Code channel"}}
	}
	f := CodeFixtures{
		Agent: slack.User{ID: CodeAgentID, Name: "codebot", RealName: "Codebot", IsBot: true, TeamID: TeamID,
			Profile: slack.Profile{DisplayName: "Codebot", RealName: "Codebot", BotID: CodeAgentBotID}},
		History: map[string][]slack.Message{}, Latest: map[string]string{}, Statuses: map[string]string{},
	}
	add := func(c slack.Conversation, latest string, status string) {
		f.Convs = append(f.Convs, c)
		f.Latest[c.ID] = latest
		f.Statuses[c.ID] = status
	}
	wCC := map[string]any{"repo": "example/app", "branch": "fix-login-redirect", "pr_status": "open", "ci_state": "pending",
		"pr_title": "Fix the login redirect loop", "context_bar_items": links, "agent_session_views": views,
		"agent_views":  []map[string]any{{"view_id": "V0HTML", "file_id": "F0HTML0001", "content_version": 1}},
		"diff_file_id": "F0DIFF0001", "diff_file_version": 3}
	add(conv(CodeWorkingID, "fix-login-redirect", props(CodeWorkingID, "processing", "Fix the login redirect loop", wCC,
		map[string]any{"date_status_processing_expire": now.Add(time.Hour).Unix()})), ts(time.Minute, 300), "processing")
	add(conv(CodeWaitingID, "add-dark-mode", props(CodeWaitingID, "suspended", "Add a dark mode toggle",
		map[string]any{"repo": "example/app", "branch": "dark-mode", "context_bar_items": links[:1]}, nil)), ts(20*time.Minute, 100), "suspended")
	add(conv(CodeDoneID, "upgrade-deps", props(CodeDoneID, "closed", "Upgrade dependencies",
		map[string]any{"repo": "example/app", "branch": "deps", "pr_status": "merged"}, nil)), ts(3*time.Hour, 100), "closed")
	add(conv(CodeIdleID, "refactor-api-client", props(CodeIdleID, "active", "Refactor the API client",
		map[string]any{"repo": "example/app", "branch": "api-client", "pr_status": "open", "context_bar_items": links[:2]}, nil)), ts(2*time.Hour, 100), "active")
	add(conv(CodeStaleID, "spike-search", props(CodeStaleID, "active", "Search spike",
		map[string]any{"repo": "example/app", "branch": "search-spike"}, nil)), ts(5*24*time.Hour, 100), "stale")
	add(conv(CodeEmptyID, "test", props(CodeEmptyID, "active", "test", map[string]any{}, nil)), ts(30*time.Minute, 100), "active")

	arch := conv(CodeArchivedID, "ship-onboarding", props(CodeArchivedID, "closed", "Ship onboarding", map[string]any{"repo": "example/app"}, nil))
	arch.IsArchived, arch.IsMember = true, false
	prev := conv(CodePreviewID, "flaky-test-hunt", props(CodePreviewID, "processing", "Hunt the flaky test",
		map[string]any{"repo": "example/app", "branch": "flaky", "context_bar_items": links}, map[string]any{"date_status_processing_expire": now.Add(time.Hour).Unix()}))
	prev.IsMember, prev.Creator = false, "U0FAKE00002"
	f.Others = []slack.Conversation{arch, prev}

	// the working channel's history: the request, the agent's plan and
	// artifacts (a diff as preformatted text, an unknown block type that
	// falls back), newest first
	agent := func(at time.Duration, n int, text string, blocks string) slack.Message {
		return slack.Message{Type: "message", TS: ts(at, n), User: CodeAgentID, BotID: CodeAgentBotID, Text: text, Blocks: json.RawMessage(blocks)}
	}
	rt := func(s string) string {
		b, _ := json.Marshal(s)
		return `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":` + string(b) + `}]}]}]`
	}
	diff := "--- a/src/auth/redirect.ts\n+++ b/src/auth/redirect.ts\n@@ -12,7 +12,9 @@\n-  if (next) return next\n+  if (next && !next.startsWith('/login')) {\n+    return next\n+  }\n   return '/home'"
	db, _ := json.Marshal(diff)
	f.History[CodeWorkingID] = []slack.Message{
		agent(time.Minute, 300, "Unsupported artifact", `[{"type":"code_diff","block_id":"d1","file":"src/auth/redirect.ts"}]`),
		agent(2*time.Minute, 200, diff, `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Here's the change so far:"}]},{"type":"rich_text_preformatted","elements":[{"type":"text","text":`+string(db)+`}]}]}]`),
		agent(3*time.Minute, 100, "Plan", `[{"type":"plan","block_id":"p1","title":"Fix the login redirect loop","tasks":[`+
			`{"task_id":"t1","title":"Reproduce the loop","status":"complete"},`+
			`{"task_id":"t2","title":"Guard the next parameter","status":"in_progress"},`+
			`{"task_id":"t3","title":"Add a regression test","status":"pending"}]}]`),
		{Type: "message", TS: ts(4*time.Minute, 100), User: SelfID, Text: "the login page keeps redirecting to itself, can you fix it?",
			Blocks: json.RawMessage(rt("the login page keeps redirecting to itself, can you fix it?"))},
	}
	for _, c := range f.Convs {
		if _, ok := f.History[c.ID]; !ok {
			f.History[c.ID] = []slack.Message{agent(time.Since(time.Unix(tsUnix(f.Latest[c.ID]), 0)), 100, "On it.", rt("On it."))}
			f.History[c.ID][0].TS = f.Latest[c.ID]
		}
	}
	return f
}

func tsUnix(ts string) int64 {
	for i := 0; i < len(ts); i++ {
		if ts[i] == '.' {
			ts = ts[:i]
			break
		}
	}
	n, _ := strconv.ParseInt(ts, 10, 64)
	return n
}

// codeCalls records the code channel API calls (CodeCalls).
type codeCalls struct {
	mu    sync.Mutex
	calls []CodeCall
}

// CodeCall is one call to a code channel method.
type CodeCall struct {
	Method string
	Args   url.Values
}

// CodeCalls returns the code channel calls so far.
func (s *Server) CodeCalls() []CodeCall {
	s.code.mu.Lock()
	defer s.code.mu.Unlock()
	return append([]CodeCall(nil), s.code.calls...)
}

// addCodeChannels adds the fixtures (Size.CodeChannels).
func (s *Server) addCodeChannels() {
	f := CodeChannelFixtures(time.Now())
	s.codeFx = &f
	s.convs = append(s.convs, f.Convs...)
	s.others = append(s.others, f.Others...)
	for id, h := range f.History {
		s.hist[id] = h
	}
}

// handleCodeChannels answers the code channel methods.
func (s *Server) handleCodeChannels(method string, f url.Values) (any, bool) {
	switch method {
	case "client.codeChannels.action", "agents.sessions.stop", "client.codeChannels.getBlockKitView":
	default:
		return nil, false
	}
	s.code.mu.Lock()
	s.code.calls = append(s.code.calls, CodeCall{Method: method, Args: f})
	s.code.mu.Unlock()
	if method == "client.codeChannels.getBlockKitView" {
		blocks := json.RawMessage(`[{"type":"header","text":{"type":"plain_text","text":"Summary"}},` +
			`{"type":"section","text":{"type":"mrkdwn","text":"*3 files changed*, 2 checks passing"}}]`)
		return ok("blocks", blocks, "service_id", CodeAgentBotID, "app_id", "A0AGENT0001"), true
	}
	return ok(), true
}
