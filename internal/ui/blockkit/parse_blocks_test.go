package blockkit

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestParseBlocksPartByPart: a block that doesn't decode (a section whose
// text is a string) drops only itself, and a container's callout keeps its
// button among the nested blocks.
func TestParseBlocksPartByPart(t *testing.T) {
	bs := ParseBlocks(json.RawMessage(`[
		{"type":"section","block_id":"a","text":{"type":"mrkdwn","text":"one"}},
		{"type":"section","block_id":"bad","text":"not an object"},
		{"type":"container","block_id":"c","child_blocks":[
			{"type":"callout","block_id":"co","child_blocks":[
				{"type":"actions","block_id":"act","elements":[{"type":"button","action_id":"go","text":{"type":"plain_text","text":"Go"}}]}]}]}]`))
	var ids []string
	for _, b := range bs {
		ids = append(ids, b.BlockID)
	}
	if strings.Join(ids, ",") != "a,c" {
		t.Fatalf("top-level blocks %v, want a and c", ids)
	}
	if _, ok := find(bs, "act", "go"); !ok {
		t.Errorf("the callout's button inside the container wasn't found")
	}
}

// TestParseBlocksNestingCap: blocks inside blocks are followed maxNesting
// levels down, as richtext follows fallbacks; deeper ones are dropped.
func TestParseBlocksNestingCap(t *testing.T) {
	inner := `{"type":"actions","block_id":"act","elements":[{"type":"button","action_id":"go","text":{"type":"plain_text","text":"Go"}}]}`
	for _, c := range []struct {
		levels int
		found  bool
	}{{maxNesting, true}, {maxNesting + 1, false}} {
		raw := inner
		for range c.levels {
			raw = `{"type":"container","child_blocks":[` + raw + `]}`
		}
		bs := ParseBlocks(json.RawMessage("[" + raw + "]"))
		if _, ok := find(bs, "act", "go"); ok != c.found {
			t.Errorf("%d containers deep: button found %v, want %v", c.levels, ok, c.found)
		}
	}
}
