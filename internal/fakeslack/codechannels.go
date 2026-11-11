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
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/slack"
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
	CodeQuietID       = "C0CODE00009" // active, read and quiet for 9 days: code_channels_auto_close closes it
	codeChannelRecord = "AC:9991336848048:852294937191:"

	// the working channel's views
	CodeHTMLViewID   = "V0HTML"
	CodeHTMLFileID   = "F0HTML0001"
	CodePlanCanvasID = "F0PLAN0001"
	CodePRURL        = "https://github.com/example/app/pull/482"
)

func withTabz(props json.RawMessage, tabz []map[string]any) json.RawMessage {
	var m map[string]any
	_ = json.Unmarshal(props, &m)
	m["tabz"] = tabz
	b, _ := json.Marshal(m)
	return b
}

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
		{"view_id": "V0PR", "type": "pull_request", "pr_url": CodePRURL, "name": "#482"},
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
		"agent_views":  []map[string]any{{"view_id": CodeHTMLViewID, "file_id": CodeHTMLFileID, "content_version": 1}},
		"diff_file_id": "F0DIFF0001", "diff_file_version": 3,
		"plan_canvases": []map[string]any{{"canvas_id": CodePlanCanvasID, "date_tabbed": now.Add(-time.Hour).Unix()}}}
	working := conv(CodeWorkingID, "fix-login-redirect", props(CodeWorkingID, "processing", "Fix the login redirect loop", wCC,
		map[string]any{"date_status_processing_expire": now.Add(time.Hour).Unix()}))
	working.Properties = withTabz(working.Properties, []map[string]any{ // the channel tabs a code channel's views are (pX9p)
		{"id": "code", "type": "code", "label": "Code"},
		{"id": CodeHTMLViewID, "type": "agent_view", "label": "Preview"},
		{"id": "Ct" + CodePlanCanvasID, "type": "canvas", "label": "", "data": map[string]any{"file_id": CodePlanCanvasID}},
	})
	add(working, ts(time.Minute, 300), "processing")
	add(conv(CodeWaitingID, "add-dark-mode", props(CodeWaitingID, "suspended", "Add a dark mode toggle",
		map[string]any{"repo": "example/app", "branch": "dark-mode", "context_bar_items": links[:1]}, nil)), ts(20*time.Minute, 100), "suspended")
	add(conv(CodeDoneID, "upgrade-deps", props(CodeDoneID, "closed", "Upgrade dependencies",
		map[string]any{"repo": "example/app", "branch": "deps", "pr_status": "merged"}, nil)), ts(3*time.Hour, 100), "closed")
	add(conv(CodeIdleID, "refactor-api-client", props(CodeIdleID, "active", "Refactor the API client",
		map[string]any{"repo": "example/app", "branch": "api-client", "pr_status": "open", "context_bar_items": links[:2]}, nil)), ts(2*time.Hour, 100), "active")
	add(conv(CodeStaleID, "spike-search", props(CodeStaleID, "active", "Search spike",
		map[string]any{"repo": "example/app", "branch": "search-spike"}, nil)), ts(5*24*time.Hour, 100), "stale")
	add(conv(CodeEmptyID, "test", props(CodeEmptyID, "active", "test", map[string]any{}, nil)), ts(30*time.Minute, 100), "active")
	add(conv(CodeQuietID, "old-prototype", props(CodeQuietID, "active", "Prototype the importer",
		map[string]any{"repo": "example/app", "branch": "importer"}, nil)), ts(9*24*time.Hour, 100), "stale")

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
	slug := func(tab string) string { // a rich_text channel element with a tab_id: a channel-tab slug
		return `{"type":"channel","channel_id":"` + CodeWorkingID + `","tab_id":"` + tab + `"}`
	}
	viewAdded := agent(30*time.Second, 400, "Codebot added views", `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[`+
		`{"type":"text","text":"Views: "},`+slug("code")+`,{"type":"text","text":" "},`+slug(CodeHTMLViewID)+`]}]}]`)
	viewAdded.Subtype = "agent_view_added"
	f.History[CodeWorkingID] = []slack.Message{
		viewAdded,
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
	// the plan canvas (a plan_canvases view)
	cs := s.canvasStore()
	cs.mu.Lock()
	now := time.Now().Unix()
	cs.byID[CodePlanCanvasID] = &FakeCanvas{ID: CodePlanCanvasID, Title: "Plan: fix the login redirect", Creator: CodeAgentID, // no Channel: canvas.go would rewrite the channel's properties
		Created: now - 3600, Updated: now - 600, Access: map[string]string{}, Doc: canvas.Doc{Blocks: canvas.ParseMarkdown(
			"# Plan\n\nThe login page sends signed-in users back to `/login`.\n\n- [x] Reproduce the loop\n- [ ] Guard the `next` parameter\n- [ ] Add a regression test", nil)}}
	cs.order = append(cs.order, CodePlanCanvasID)
	cs.mu.Unlock()
}

// codeLatest is a code channel fixture's latest message ts.
func (s *Server) codeLatest(ch string) (string, bool) {
	if s.codeFx == nil {
		return "", false
	}
	l, ok := s.codeFx.Latest[ch]
	return l, ok
}

// isCodeFixture reports whether ch is one of the viewer's code channels.
func (s *Server) isCodeFixture(ch string) bool {
	if s.codeFx == nil {
		return false
	}
	for _, c := range s.codeFx.Convs {
		if c.ID == ch {
			return true
		}
	}
	return false
}

// CodeDiffFixture is client.codeChannels.getDiff's diff for the working
// channel: a modified, an added, a renamed and a generated file, and one
// past the patch renderer's 100-line page.
func CodeDiffFixture() map[string]any {
	var long strings.Builder
	long.WriteString("@@ -0,0 +1,130 @@\n")
	for i := 1; i <= 130; i++ {
		long.WriteString(fmt.Sprintf("+export const route%03d = { path: '/r/%d', auth: true } // generated\n", i, i))
	}
	files := []map[string]any{
		{"filename": "src/auth/redirect.ts", "status": "modified", "additions": 5, "deletions": 2, "patch": "@@ -9,10 +9,13 @@ import { session } from './session'\n \n export function nextPath(next?: string): string {\n   // where to go after signing in\n-  if (next) return next\n-  return '/home'\n+  if (next && !next.startsWith('/login')) {\n+    return next\n+  }\n+  const fallback = session.home ?? '/home'\n+  return fallback\n }\n \n export const MAX_REDIRECTS = 3"},
		{"filename": "src/auth/redirect.test.ts", "status": "added", "additions": 9, "deletions": 0, "patch": "@@ -0,0 +1,9 @@\n+import { nextPath } from './redirect'\n+\n+describe('nextPath', () => {\n+  it(\"ignores /login\", () => {\n+    expect(nextPath('/login?next=/login')).toBe('/home')\n+  })\n+  it('keeps other paths', () => expect(nextPath('/settings')).toBe('/settings'))\n+})\n+"},
		{"filename": "src/auth/loop.ts", "previous_filename": "src/legacy/loginLoop.ts", "status": "renamed", "additions": 1, "deletions": 1, "patch": "@@ -1,4 +1,4 @@\n-// TODO: remove the legacy loop guard\n+// the loop guard, now next to the redirect\n export function isLoop(path: string): boolean {\n   return path === '/login'\n }"},
		{"filename": "src/routes/generated.ts", "status": "added", "additions": 130, "deletions": 0, "patch": strings.TrimSuffix(long.String(), "\n")},
		{"filename": "package-lock.json", "status": "modified", "additions": 2, "deletions": 2, "is_generated": true, "patch": "@@ -1,6 +1,6 @@\n {\n   \"name\": \"example-app\",\n-  \"version\": \"1.4.0\",\n+  \"version\": \"1.4.1\",\n   \"lockfileVersion\": 3,\n-  \"requires\": false\n+  \"requires\": true\n }"},
	}
	return map[string]any{"files": files, "truncated": false, "total_commits": 3, "base_branch": "main", "head_branch": "fix-login-redirect"}
}

// CodePRFixture is client.codeChannels.getPullRequest's answer for #482.
func CodePRFixture() map[string]any {
	now := time.Now().Unix()
	comment := func(id, kind string, at int64, login, body string, anchor map[string]any) map[string]any {
		c := map[string]any{"id": id, "comment_kind": kind, "review_state": "COMMENTED", "timestamp": at,
			"comment": map[string]any{"value": body}, "sender": map[string]any{"login": login}}
		if anchor != nil {
			c["review_anchor"] = anchor
		}
		return c
	}
	return map[string]any{
		"pull_request": map[string]any{"title": "Fix the login redirect loop", "state": "open", "review_decision": "changes_requested", "has_conflicts": false,
			"body": "Signed-in users who opened /login were sent back to /login forever. `nextPath` now ignores `/login` and falls back to the session's home.\n\nCloses #470."},
		"diff":     CodeDiffFixture(),
		"revision": "9f2c1e7",
		"comments": []map[string]any{
			comment("c1", "issue_comment", now-7200, "octo-reviewer", "Thanks! Could we keep the old fallback for users without a home?", nil),
			comment("c2", "review_comment", now-3600, "octo-reviewer", "Nit: `fallback` can be inlined.", map[string]any{"path": "src/auth/redirect.ts", "line": 16, "side": "RIGHT"}),
			comment("c3", "review_summary", now-3500, "octo-reviewer", "One nit, otherwise looks good.", nil),
		},
	}
}

// handleCodeChannels answers the code channel methods.
func (s *Server) handleCodeChannels(method string, f url.Values) (any, bool) {
	switch method {
	case "client.codeChannels.action", "agents.sessions.stop", "client.codeChannels.getBlockKitView",
		"client.codeChannels.getDiff", "client.codeChannels.getPullRequest", "client.codeChannels.getView",
		"recordChannels.open":
	case "conversations.close": // closing a code channel (closeChannelApi); DMs fall through
		if !s.isCodeFixture(f.Get("channel")) {
			return nil, false
		}
	default:
		return nil, false
	}
	s.code.mu.Lock()
	s.code.calls = append(s.code.calls, CodeCall{Method: method, Args: f})
	s.code.mu.Unlock()
	switch method {
	case "client.codeChannels.getBlockKitView":
		blocks := json.RawMessage(`[{"type":"header","text":{"type":"plain_text","text":"Summary"}},` +
			`{"type":"section","text":{"type":"mrkdwn","text":"*5 files changed*, 2 checks passing"}},` +
			`{"type":"divider"},` +
			`{"type":"section","fields":[{"type":"mrkdwn","text":"*Branch*\nfix-login-redirect"},{"type":"mrkdwn","text":"*Tests*\n42 passed"}]},` +
			`{"type":"actions","elements":[{"type":"button","action_id":"rerun","text":{"type":"plain_text","text":"Re-run checks"}}]}]`)
		return ok("blocks", blocks, "service_id", CodeAgentBotID, "app_id", "A0AGENT0001"), true
	case "client.codeChannels.getDiff":
		if f.Get("channel_id") != CodeWorkingID {
			return ok("diff", map[string]any{"files": []any{}, "truncated": false, "base_branch": "main"}), true
		}
		return ok("diff", CodeDiffFixture()), true
	case "client.codeChannels.getPullRequest":
		if f.Get("pr_url") != CodePRURL {
			return map[string]any{"ok": false, "error": "invalid_pr_url"}, true
		}
		pr := CodePRFixture()
		return ok("pull_request", pr["pull_request"], "diff", pr["diff"], "comments", pr["comments"], "revision", pr["revision"]), true
	case "recordChannels.open": // openRecordChannel: the channel, open again
		for _, c := range s.codeFx.Convs {
			if c.ID == f.Get("channel") {
				ch := map[string]any{}
				b, _ := json.Marshal(c)
				_ = json.Unmarshal(b, &ch)
				ch["is_open"] = true
				return ok("channel", ch), true
			}
		}
		return map[string]any{"ok": false, "error": "channel_not_found"}, true
	case "client.codeChannels.getView":
		if f.Get("view_id") != CodeHTMLViewID {
			return map[string]any{"ok": false, "error": "view_not_found"}, true
		}
		return ok("html_content", "<!doctype html><h1>Login redirect report</h1><script>alert(1)</script>", "content_version", 1,
			"csp", map[string]any{"resource_domains": []any{}}), true
	}
	return ok(), true
}
