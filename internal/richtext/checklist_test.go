package richtext

import "testing"

// Check lists (N8wa BS: checklist true or style "check"): each item's
// state is its own checked, else the list's (RichTextList's e.checked ??
// c); a check list never draws ordered markers.
func TestCheckLists(t *testing.T) {
	sec := func(text, extra string) string {
		return `{"type":"rich_text_section"` + extra + `,"elements":[{"type":"text","text":"` + text + `"}]}`
	}
	d := blocksMsg(t, `[{"type":"rich_text","elements":[
		{"type":"rich_text_list","style":"check","checked":true,"elements":[`+sec("done", "")+`,`+sec("todo", `,"checked":false`)+`]},
		{"type":"rich_text_list","style":"ordered","checklist":true,"indent":1,"elements":[`+sec("nested", "")+`]},
		{"type":"rich_text_list","style":"bullet","elements":[`+sec("plain", "")+`]}]}]`)
	if len(d.Blocks) != 3 {
		t.Fatalf("blocks %s", dump(d.Blocks))
	}
	type state struct {
		Check   bool
		Checked []bool
		Ordered bool
	}
	var got []state
	for _, b := range d.Blocks {
		c, cs := checkOf(b)
		got = append(got, state{c, cs, b.Ordered})
	}
	eq(t, "check lists", got, []state{{true, []bool{true, false}, false}, {true, []bool{false}, false}, {false, nil, false}})
}
