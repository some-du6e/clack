package richtext

// Slack's agent blocks (2025–2026): plan, task_card and context_actions
// (feedback_buttons, icon_button). They become one Agent block whose
// single Button names the block (Kind = block type, BlockID); the
// message's element host (internal/ui/blockkit, agent.go) draws and runs
// it. Without a host, richview lays out Spans: the plain-text fallback
// Slack's own previews use (plan title, task titles).

import (
	"encoding/json"
	"strings"
)

// bkAgent is the agent part of a block (embedded in bk).
type bkAgent struct {
	Tasks   []bk   `json:"tasks"`   // plan
	TaskID  string `json:"task_id"` // plan task, task_card
	Status  string `json:"status"`  // pending | in_progress | complete | error
	Details *bk    `json:"details"` // rich_text
	Output  *bk    `json:"output"`  // rich_text
	Sources []bk   `json:"sources"` // {type: url, url, text}
}

// UnmarshalJSON takes a text object or a bare string: plan and
// task_card titles are strings, and one string title must not fail
// the whole blocks array (Slack shows the block; we used to drop
// every block and fall back to the message text).
func (t *textObj) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*t = textObj{Type: "plain_text", Text: s}
		return nil
	}
	type plain textObj
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*t = textObj(p)
	return nil
}

// AgentFirst marks (Button.Style) an agent block that is the first block
// of its list: Slack's .firstBlock drops the slim plan's top margin.
const AgentFirst = "first"

// agentBlock converts plan / task_card / context_actions.
func (c *builder) agentBlock(out []Block, b *bk) []Block {
	btn := Button{Kind: b.Type, BlockID: b.BlockID}
	if len(out) == 0 {
		btn.Style = AgentFirst
	}
	blk := Block{Kind: Agent, Buttons: []Button{btn}}
	switch b.Type {
	case "plan":
		title := ""
		if b.Title != nil {
			title = b.Title.Text
		}
		if title == "" {
			title = planTitle(b.Tasks)
		}
		blk.Spans = plainSpans(c.r, title)
	case "task_card":
		if b.Title != nil {
			blk.Spans = plainSpans(c.r, b.Title.Text)
		}
	case "context_actions":
		if len(b.Elements) == 0 {
			return out
		}
	}
	return append(out, blk)
}

// planTitle is Slack's getPlanTitle for a plan without a title: the last
// in-progress task's title, else the last titled task's.
func planTitle(ts []bk) string {
	last, prog := "", ""
	for i := range ts {
		t := ""
		if ts[i].Title != nil {
			t = strings.TrimSpace(ts[i].Title.Text)
		}
		if t == "" {
			continue
		}
		last = t
		if ts[i].Status == "in_progress" {
			prog = t
		}
	}
	if prog != "" {
		return prog
	}
	return last
}
