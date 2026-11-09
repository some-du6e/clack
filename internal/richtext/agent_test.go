package richtext

import "testing"

// The shape of an agent's "thinking" message (types only, synthetic
// text): a plan with a string title and tasks whose details/output are
// rich_text. Before agent.go the string title failed the whole blocks
// array and the message showed its text fallback.
func TestAgentPlanBlock(t *testing.T) {
	d := blocksMsg(t, `[{"type":"plan","block_id":"p1","title":"Thinking completed","tasks":[
		{"task_id":"t1","title":"Read","status":"complete","details":{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"d"}]}]}},
		{"task_id":"t2","title":"Write","status":"complete","output":{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"o","style":{"bold":true}}]}]}}]}]`)
	if len(d.Blocks) != 1 || d.Blocks[0].Kind != Agent {
		t.Fatalf("blocks = %+v", d.Blocks)
	}
	b := d.Blocks[0]
	if len(b.Buttons) != 1 || b.Buttons[0].Kind != "plan" || b.Buttons[0].BlockID != "p1" || b.Buttons[0].Style != AgentFirst {
		t.Errorf("button = %+v", b.Buttons)
	}
	if got := SpansText(b.Spans); got != "Thinking completed" {
		t.Errorf("fallback %q", got)
	}
}

// An untitled plan falls back to Slack's getPlanTitle: the last
// in-progress task, else the last titled one.
func TestAgentPlanTitleFallback(t *testing.T) {
	d := blocksMsg(t, `[{"type":"plan","block_id":"p","tasks":[{"task_id":"a","title":"One","status":"in_progress"},{"task_id":"b","title":"Two","status":"pending"}]}]`)
	if got := SpansText(d.Blocks[0].Spans); got != "One" {
		t.Errorf("in-progress title %q", got)
	}
	d = blocksMsg(t, `[{"type":"plan","block_id":"p","tasks":[{"task_id":"a","title":"One","status":"complete"},{"task_id":"b","title":"Two","status":"complete"}]}]`)
	if got := SpansText(d.Blocks[0].Spans); got != "Two" {
		t.Errorf("last title %q", got)
	}
}

// The reply: rich_text, a context line and context_actions with
// feedback_buttons. Every block survives; context_actions is an Agent
// slot that isn't first.
func TestAgentReplyBlocks(t *testing.T) {
	d := blocksMsg(t, `[{"type":"rich_text","block_id":"r","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"hi "},{"type":"emoji","name":"sweat_smile","unicode":"1f605"}]}]},
		{"type":"context","block_id":"c","elements":[{"type":"mrkdwn","text":"_done in 6.6s_","verbatim":false}]},
		{"type":"context_actions","block_id":"f","elements":[{"type":"feedback_buttons","action_id":"fb",
			"positive_button":{"text":{"type":"plain_text","text":"Good","emoji":true},"accessibility_label":"good","value":"up"},
			"negative_button":{"text":{"type":"plain_text","text":"Bad","emoji":true},"accessibility_label":"bad","value":"down"}}]},
		{"type":"task_card","block_id":"k","task_id":"x","title":"Card","status":"error"}]`)
	kinds := []BlockKind{}
	for _, b := range d.Blocks {
		kinds = append(kinds, b.Kind)
	}
	want := []BlockKind{Paragraph, Context, Agent, Agent}
	if len(kinds) != len(want) {
		t.Fatalf("kinds %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds %v, want %v", kinds, want)
		}
	}
	if bt := d.Blocks[2].Buttons[0]; bt.Kind != "context_actions" || bt.BlockID != "f" || bt.Style == AgentFirst {
		t.Errorf("context_actions button %+v", bt)
	}
	if bt := d.Blocks[3].Buttons[0]; bt.Kind != "task_card" || SpansText(d.Blocks[3].Spans) != "Card" {
		t.Errorf("task_card %+v %q", bt, SpansText(d.Blocks[3].Spans))
	}
}

// A bare string where a text object is expected no longer drops the
// message's other blocks.
func TestStringTitleKeepsBlocks(t *testing.T) {
	d := blocksMsg(t, `[{"type":"image","image_url":"https://img.test/a.png","alt_text":"a","title":"plain title"},{"type":"divider"}]`)
	if len(d.Blocks) != 2 || d.Blocks[0].Image == nil || d.Blocks[0].Image.Title != "plain title" {
		t.Fatalf("blocks %+v", d.Blocks)
	}
}
