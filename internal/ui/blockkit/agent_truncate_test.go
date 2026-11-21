package blockkit

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// TestAgentTaskTruncation: the Go port of Slack's BLFT
// truncateRichTextElements gives exactly what Slack's own module gives
// (testdata/agent_truncate.json: Slack's code run in node over these
// cases, maxChars 250, maxLines 3, block-level expand button).
func TestAgentTaskTruncation(t *testing.T) {
	b, err := os.ReadFile("testdata/agent_truncate.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cases []struct {
			Name     string          `json:"name"`
			Elements []any           `json:"elements"`
			MaxChars int             `json:"maxChars"`
			MaxLines int             `json:"maxLines"`
			Expected json.RawMessage `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Cases) < 10 {
		t.Fatalf("%d cases", len(f.Cases))
	}
	for _, c := range f.Cases {
		got := truncateRichTextElements(c.Elements, c.MaxChars, c.MaxLines)
		gb, _ := json.Marshal(got)
		var g, w any
		_ = json.Unmarshal(gb, &g)
		_ = json.Unmarshal(c.Expected, &w)
		if !reflect.DeepEqual(g, w) {
			t.Errorf("%s:\n got  %s\n want %s", c.Name, gb, c.Expected)
		}
	}
}

// TestAgentTaskContentCut: what the slim task draws collapsed: the cut
// block, the truncation element as an ellipsis, no expand section (Show
// more is drawn under it).
func TestAgentTaskContentCut(t *testing.T) {
	long := `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"` +
		`aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}]}`
	out, cut := truncateTaskContent(json.RawMessage(long))
	if !cut {
		t.Fatal("a 254-character detail isn't cut")
	}
	var blk struct {
		Elements []struct {
			Type     string `json:"type"`
			Elements []struct {
				Type, Text string
			} `json:"elements"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(out, &blk); err != nil {
		t.Fatal(err)
	}
	if len(blk.Elements) != 1 || len(blk.Elements[0].Elements) != 2 {
		t.Fatalf("cut block %s", out)
	}
	if e := blk.Elements[0].Elements; len(e[0].Text) != 250 || e[1].Type != "text" || e[1].Text != "\u2026" {
		t.Errorf("cut block %s", out)
	}
	short := `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Read the logs."}]}]}`
	if out, cut := truncateTaskContent(json.RawMessage(short)); cut || string(out) != short {
		t.Errorf("a short detail was cut: %s", out)
	}
}
