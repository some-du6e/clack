package richtext

import (
	"strings"
	"testing"
)

// Synthetic fixtures only.

// agentRes is the test resolver with share_prompt agents.
type agentRes struct {
	fakeRes
	agents map[string]agentFx
}

type agentFx struct {
	name, avatar string
	agentforce   bool
}

func (r agentRes) PromptAgent(id string) (name, avatar string, agentforce, ok bool) {
	a, ok := r.agents[id]
	return a.name, a.avatar, a.agentforce, ok
}

// A share_prompt's keys in either spelling (toCamelCaseProps), its agent
// as the resolver has it, and prompt_rich_text kept as blocks with the
// block itself for the send. SharePromptButton draws it only for an
// agent it knows with a picture that's an agent (Clack knows Agentforce
// agents only).
func TestSharePromptDecode(t *testing.T) {
	r := agentRes{agents: map[string]agentFx{
		"UAF": {name: "Pipeline Agent", avatar: "https://img.test/a.png", agentforce: true},
		"UNO": {name: "Plain Bot", avatar: "https://img.test/b.png"},
		"UNA": {name: "No Picture", agentforce: true},
	}}
	build := func(block string) *SharePrompt {
		t.Helper()
		d := FromMessage(r, msg(t, `{"ts":"1.0","text":"fallback text","blocks":[`+block+`]}`))
		if len(d.Blocks) != 1 || d.Blocks[0].Kind != SharePromptBlock || d.Blocks[0].Prompt == nil {
			t.Fatalf("%s: %s", block, dump(d.Blocks))
		}
		return d.Blocks[0].Prompt
	}
	for _, b := range []string{
		`{"type":"share_prompt","block_id":"sp","label":"Weekly pipeline","prompt":"Summarise this week","prompt_id":"P1","app_id":"A1","bot_user_id":"UAF","is_slackbot":false}`,
		`{"type":"share_prompt","block_id":"sp","label":"Weekly pipeline","prompt":"Summarise this week","promptId":"P1","appId":"A1","botUserId":"UAF","isSlackbot":false}`,
	} {
		p := build(b)
		if p.Label != "Weekly pipeline" || p.Prompt != "Summarise this week" || p.PromptID != "P1" || p.AppID != "A1" || p.BotUserID != "UAF" || p.IsSlackbot {
			t.Errorf("%s: %+v", b, p)
		}
		if !p.Shown || p.AgentName != "Pipeline Agent" || p.AgentAvatar != "https://img.test/a.png" {
			t.Errorf("%s: shown %v, agent %q %q; want the Agentforce agent shown", b, p.Shown, p.AgentName, p.AgentAvatar)
		}
	}
	for _, c := range []struct{ name, bot string }{
		{"not an agent", "UNO"},
		{"an agent without a picture", "UNA"},
		{"an agent nobody has", "U0NOSUCH"},
		{"no bot_user_id", ""},
	} {
		if p := build(`{"type":"share_prompt","prompt":"x","bot_user_id":"` + c.bot + `"}`); p.Shown {
			t.Errorf("%s: shown", c.name)
		}
	}
	rich := `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Summarise "},{"type":"text","text":"this week","style":{"bold":true}}]}]}`
	p := build(`{"type":"share_prompt","prompt":"Summarise this week","prompt_rich_text":` + rich + `,"bot_user_id":"UAF"}`)
	if len(p.Rich) != 1 || p.Rich[0].Kind != Paragraph || len(p.Rich[0].Spans) != 2 || p.Rich[0].Spans[1].Style&Bold == 0 {
		t.Errorf("rich text: %s", dump(p.Rich))
	}
	if !strings.Contains(string(p.RichRaw), `"rich_text_section"`) {
		t.Errorf("RichRaw %s: want the block itself", p.RichRaw)
	}
	// a prompt_rich_text that isn't a rich_text block: the prompt's text
	if p := build(`{"type":"share_prompt","prompt":"x","prompt_rich_text":{"type":"section"},"bot_user_id":"UAF"}`); len(p.Rich) != 0 || len(p.RichRaw) != 0 {
		t.Errorf("a section as prompt_rich_text: rich %s, raw %s", dump(p.Rich), p.RichRaw)
	}
}

// A share_prompt's plain text (notifications: convertStringTitledBlock-
// ToPlainText) is its label.
func TestSharePromptNotifyText(t *testing.T) {
	r := agentRes{agents: map[string]agentFx{"UAF": {name: "Pipeline Agent", avatar: "https://img.test/a.png", agentforce: true}}}
	d := FromMessage(r, msg(t, `{"ts":"1.0","text":"fallback text","blocks":[{"type":"share_prompt","label":"Weekly pipeline","prompt":"Summarise this week","bot_user_id":"UAF"}]}`))
	if got := notifyBlock(&d.Blocks[0], nil); got != "Weekly pipeline" {
		t.Errorf("notification text %q, want the label", got)
	}
}

// Mistyped fields don't fail the block: a field of another type reads as
// absent (an object label, a number prompt or agent, a string
// prompt_rich_text), and with no agent to look up the card draws nothing,
// as SharePromptButton returns null before it reads the label.
func TestSharePromptMistypedFields(t *testing.T) {
	r := agentRes{agents: map[string]agentFx{"UAF": {name: "Pipeline Agent", avatar: "https://img.test/a.png", agentforce: true}}}
	d := FromMessage(r, msg(t, `{"ts":"1.0","text":"fallback text","blocks":[{"type":"share_prompt","label":{"x":1},"prompt":7,"prompt_id":["p"],"bot_user_id":5,"is_slackbot":"yes","prompt_rich_text":"x"}]}`))
	if len(d.Blocks) != 1 || d.Blocks[0].Kind != SharePromptBlock || d.Blocks[0].Prompt == nil {
		t.Fatalf("blocks %s, want the share_prompt", dump(d.Blocks))
	}
	p := d.Blocks[0].Prompt
	if p.Label != "" || p.Prompt != "" || p.PromptID != "" || p.BotUserID != "" || len(p.Rich) != 0 || len(p.RichRaw) != 0 {
		t.Errorf("prompt %+v: want each mistyped field absent", p)
	}
	if !p.IsSlackbot || p.Shown {
		t.Errorf("is_slackbot %v shown %v: want a truthy string read as true, and nothing drawn", p.IsSlackbot, p.Shown)
	}
}
