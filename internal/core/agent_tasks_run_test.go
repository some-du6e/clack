package core

// Run task now posts the prompt as Slack does (EAAO runPromptFor3pAgent →
// prepareAndSendMessage): rich_text blocks, a string prompt as one block
// of literal text and a rich text prompt's blocks unchanged (its
// mentions, channels, links and emoji kept), and no text, link_names or
// unfurl_links (Xhlw sendMessage's arguments).

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

func TestAgentTasksRunNowBlocks(t *testing.T) {
	a, fs := agentTasksApp(t, nil)
	ctx := context.Background()
	st := fs.AgentTasks()
	rich := []any{map[string]any{"type": "rich_text", "elements": []any{map[string]any{"type": "rich_text_section", "elements": []any{
		map[string]any{"type": "text", "text": "Summarize "},
		map[string]any{"type": "channel", "channel_id": "C0FAKE0001"},
		map[string]any{"type": "text", "text": " for "},
		map[string]any{"type": "user", "user_id": fakeslack.SelfID},
		map[string]any{"type": "text", "text": " and check "},
		map[string]any{"type": "link", "url": "https://example.com/status", "text": "the status page"},
		map[string]any{"type": "text", "text": " "},
		map[string]any{"type": "emoji", "name": "rocket"},
	}}}}}
	st.Set(func(s *fakeslack.AgentTasksState) {
		for _, tk := range s.Tasks {
			if tk.ID == fakeslack.TaskDigest {
				tk.PromptRich = rich
			}
		}
	})
	page, err := a.AgentTasks(ctx, fakeslack.AgentTasksAppID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var brief, digest AgentTask
	for _, tk := range page.Tasks {
		switch tk.WorkflowID {
		case fakeslack.TaskBriefing:
			brief = tk
		case fakeslack.TaskDigest:
			digest = tk
		}
	}
	for _, tk := range []AgentTask{brief, digest} {
		if _, _, err := a.RunAgentTaskNow(ctx, fakeslack.AgentTasksUserID, tk); err != nil {
			t.Fatal(err)
		}
	}
	var posts []map[string]string
	for _, c := range st.AgentTaskCalls() {
		if c.Method == "chat.postMessage" {
			posts = append(posts, map[string]string{"blocks": c.Form.Get("blocks"), "text": c.Form.Get("text")})
			for _, k := range []string{"text", "link_names", "unfurl_links"} {
				if c.Form.Has(k) {
					t.Errorf("chat.postMessage has %s=%q: Slack's sendMessage posts the blocks alone", k, c.Form.Get(k))
				}
			}
		}
	}
	if len(posts) != 2 {
		t.Fatalf("chat.postMessage calls: %d, want 2", len(posts))
	}
	decode := func(s string) any {
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			t.Errorf("blocks %q: %v", s, err)
		}
		return v
	}
	wantString := []any{map[string]any{"type": "rich_text", "elements": []any{map[string]any{"type": "rich_text_section",
		"elements": []any{map[string]any{"type": "text", "text": brief.Prompt}}}}}}
	if got := decode(posts[0]["blocks"]); !reflect.DeepEqual(got, decode(mustJSON(t, wantString))) {
		t.Errorf("string prompt: blocks %s, want one rich_text block of its text", posts[0]["blocks"])
	}

	if got := decode(posts[1]["blocks"]); !reflect.DeepEqual(got, decode(mustJSON(t, rich))) {
		t.Errorf("rich text prompt: blocks %s, want the prompt's blocks unchanged", posts[1]["blocks"])
	}

}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
