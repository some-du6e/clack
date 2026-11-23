package blockkit

import (
	"encoding/json"
	"testing"
)

// A data_table's action_cell buttons are the message's elements (the
// table's block id, the cell's action id): its live buttons draw them
// and send block_actions as any button's (DataTableBlockRow's
// renderCellContent). Synthetic fixture.
func TestDataTableActionCells(t *testing.T) {
	bs := ParseBlocks(json.RawMessage(`[{"type":"data_table","block_id":"dt1","rows":[
		[{"type":"raw_text","text":"Task"},{"type":"raw_text","text":"Action"}],
		[{"type":"raw_text","text":"a"},{"type":"action_cell","element":{"type":"button","action_id":"mark_done","text":{"type":"plain_text","text":"Mark done"},"value":"task_123"},"fallback":{"type":"raw_text","text":"Open"}}],
		[{"type":"raw_text","text":"b"},{"type":"action_cell","element":{"type":"button","action_id":"mark_done_b","text":{"type":"plain_text","text":"Mark done"},"value":"task_124","style":"primary"}}]]},
		{"type":"data_table","block_id":"dt2","rows":{"not":"rows"}}]`))
	if len(bs) != 2 || len(bs[0].Elements) != 2 || len(bs[1].Elements) != 0 {
		t.Fatalf("parse: %d blocks, %d / %d elements", len(bs), len(bs[0].Elements), len(bs[min(1, len(bs)-1)].Elements))
	}
	r, ok := find(bs, "dt1", "mark_done_b")
	if !ok || r.el.Type != "button" || r.el.Value != "task_124" || r.el.Style != "primary" || r.block.BlockID != "dt1" {
		t.Fatalf("find: %+v %v", r, ok)
	}
	if _, ok := find(bs, "dt1", "mark_done"); !ok {
		t.Errorf("the first action cell isn't an element")
	}
}

// Rows sharing one action_id are told apart by their buttons' values: a
// click on row 2 finds row 2's element (its value goes out), and each
// row gets its own widget key.
func TestDataTableSharedActionID(t *testing.T) {
	bs := ParseBlocks(json.RawMessage(`[{"type":"data_table","block_id":"dt1","rows":[
		[{"type":"raw_text","text":"Task"},{"type":"raw_text","text":"Action"}],
		[{"type":"raw_text","text":"a"},{"type":"action_cell","element":{"type":"button","action_id":"done","text":{"type":"plain_text","text":"Done"},"value":"row_1"}}],
		[{"type":"raw_text","text":"b"},{"type":"action_cell","element":{"type":"button","action_id":"done","text":{"type":"plain_text","text":"Done"},"value":"row_2"}}]]}]`))
	r, ok := findValue(bs, "dt1", "done", "row_2")
	if !ok || r.el.Value != "row_2" {
		t.Fatalf("row 2: %+v %v", r.el, ok)
	}
	if r, ok := findValue(bs, "dt1", "done", "row_1"); !ok || r.el.Value != "row_1" {
		t.Errorf("row 1: %+v %v", r.el, ok)
	}
	b := bs[0]
	if k1, k2 := surfKey(&b, &b.Elements[0]), surfKey(&b, &b.Elements[1]); k1 == k2 {
		t.Errorf("the rows share the widget key %q", k1)
	}
	// any other block keeps its key without the value
	sec := Block{Type: "actions", BlockID: "a1"}
	if k := surfKey(&sec, &Element{Type: "button", ActionID: "go", Value: "v"}); k != "a1\x00go\x00button" {
		t.Errorf("an actions block's button key %q", k)
	}
}
