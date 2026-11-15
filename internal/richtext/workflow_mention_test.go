package richtext

import (
	"encoding/json"
	"testing"
)

// Workflow mentions (jp/6 WorkflowMention with Po6V
// getWorkflowSlugAttributes). Synthetic fixtures only, in the shape of a
// rich_text workflow_mention element (keys only).

func TestWorkflowMentionVariants(t *testing.T) {
	t.Cleanup(func() { SetWorkflowSlugs(nil) })
	trig := map[string]WorkflowSlug{
		"Ft0OK": {Title: "Run\u00a0deploy\u00ad"},
		"Ft0UN": {},
		"Ft0NO": {Title: "Secret", Error: "access_denied"},
		"Ft0GO": {Error: "trigger_not_found"},
	}
	SetWorkflowSlugs(func(id string) (WorkflowSlug, bool) {
		s, ok := trig[id]
		return s, ok
	})
	el := func(id string) string {
		return `{"type":"workflow_mention","function_trigger_id":"` + id + `","workflow_id":"Wf0X","text":"Element text","url":"https://slack.com/shortcuts/` + id + `/abc"}`
	}
	r := &fakeRes{}
	cases := []struct {
		id, text, icon string
		off            bool
	}{
		{"Ft0OK", "Run deploy", "play", false},
		{"Ft0UN", "Element text", "play", false}, // no workflow title: the element's text
		{"Ft0NO", "Secret", "lock", true},
		{"Ft0GO", "Deleted workflow", "trash", true},
	}
	for _, c := range cases {
		sp := slugOf(t, FromMessage(r, lrMessage(el(c.id))))
		if sp.Text != c.text || sp.Icon != c.icon || sp.Off != c.off || sp.ID != c.id || sp.URL != "https://slack.com/shortcuts/"+c.id+"/abc" {
			t.Errorf("%s: %q %q off %v id %q url %q, want %q %q %v", c.id, sp.Text, sp.Icon, sp.Off, sp.ID, sp.URL, c.text, c.icon, c.off)
		}
		if ps := sp.PartSpans(); len(ps) != 1 || ps[0].Text != c.text {
			t.Errorf("%s: parts %+v", c.id, ps)
		}
	}
	// not loaded yet: nothing drawn (isLoading)
	for _, b := range FromMessage(r, lrMessage(el("Ft0NEW"))).Blocks {
		for _, s := range b.Spans {
			if s.Kind == Canvas || s.Text == "Element text" {
				t.Errorf("an unloaded trigger drew %+v", s)
			}
		}
	}
	// without the ui's resolver: the element's text
	SetWorkflowSlugs(nil)
	if got := PlainText(FromMessage(r, lrMessage(el("Ft0OK")))); got != "Element text" {
		t.Errorf("no resolver: %q", got)
	}
}

func TestWorkflowMentionTrigger(t *testing.T) {
	blocks := json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` +
		`{"type":"link","url":"https://example.com"},` +
		`{"type":"workflow_mention","function_trigger_id":"Ft0A","url":"https://slack.com/shortcuts/Ft0A/x"}]}]}]`)
	if id := WorkflowMentionTrigger(blocks, "https://slack.com/shortcuts/Ft0A/x"); id != "Ft0A" {
		t.Errorf("mention: %q", id)
	}
	if id := WorkflowMentionTrigger(blocks, "https://example.com"); id != "" {
		t.Errorf("a plain link: %q", id)
	}
	if id := WorkflowMentionTrigger(nil, "x"); id != "" {
		t.Errorf("no blocks: %q", id)
	}
}

func TestSanitizeSlugText(t *testing.T) {
	if got := sanitizeSlugText("\u200bA\u2028B C\u00a0D\u00ade\ufeff"); got != "A\nB C De" {
		t.Errorf("%q", got)
	}
}
