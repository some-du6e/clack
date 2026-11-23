package richview

import (
	"testing"
	"time"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// actionTable is a data_table whose rows end in an action_cell button.
const actionTable = `[{"type":"data_table","block_id":"dtact","caption":"Tasks","rows":[
	[{"type":"raw_text","text":"Task"},{"type":"raw_text","text":"Action"}],
	[{"type":"raw_text","text":"a"},{"type":"action_cell","element":{"type":"button","action_id":"done_a","text":{"type":"plain_text","text":"Mark done"},"value":"a"}}],
	[{"type":"raw_text","text":"b"},{"type":"action_cell","element":{"type":"button","action_id":"done_b","text":{"type":"plain_text","text":"Mark done"},"value":"b"}}]]}]`

// The fullscreen view's action cells are its message's live elements: the
// inline table's TableControl carries the view's element host, the
// fullscreen copy keeps it, and the fullscreen layout puts each button in
// an element slot of that host (DataTableFullscreenVirtualized's rows pass
// the table's onAction), as the inline rows do.
func TestDataTableFullscreenActions(t *testing.T) {
	h := &dtHandler{}
	d := kitDoc(t, actionTable)
	hs := newHarness(d, h)
	hs.v.SetOptions(Options{Elements: fixedElements{}})
	hs.w = 700
	hs.frame()
	tbl := oneTable(t, hs.v)
	if n := len(hs.v.ElementSlots()); n != 2 {
		t.Fatalf("inline table: %d element slots, want 2 (the action cells)", n)
	}
	hs.clickRect(tbl.Buttons["view"], time.Second)
	if len(h.ctls) != 1 || h.acts[0] != "view" {
		t.Fatalf("View: actions %q", h.acts)
	}
	ctl := h.ctls[0]
	if _, ok := ctl.Elements.(fixedElements); !ok {
		t.Fatalf("the View click's TableControl carries element host %T, want the view's", ctl.Elements)
	}
	// the fullscreen copy (OpenDataTableFull's): every row, the same host
	full := *ctl.Table
	full.Full = true
	fc := NewTableControl(&full, ctl)
	if _, ok := fc.Elements.(fixedElements); !ok {
		t.Fatalf("NewTableControl dropped the element host: %T", fc.Elements)
	}
	fv := New(rt.Doc{Blocks: []rt.Block{{Kind: rt.DataTableBlock, BlockID: full.BlockID, DataTable: &full}}}, Options{NoHoverCards: true, Elements: fc.Elements})
	fv.SetTableControl(&full, fc)
	fv.relayout(newTestStyle(), 900, 1, 1)
	slots := fv.ElementSlots()
	if len(slots) != 2 {
		t.Fatalf("fullscreen table: %d element slots, want 2", len(slots))
	}
	// the buttons keep the table's block id and their own action ids: the
	// host's block_actions
	var ids []string
	for i := range fv.d.slots {
		for _, b := range fv.d.slots[i].bs {
			ids = append(ids, b.BlockID+"/"+b.ActionID)
		}
	}
	if len(ids) != 2 || ids[0] != "dtact/done_a" || ids[1] != "dtact/done_b" {
		t.Errorf("fullscreen buttons %q, want dtact/done_a and dtact/done_b", ids)
	}
}
