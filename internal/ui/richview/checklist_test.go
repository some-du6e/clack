package richview

import (
	"strings"
	"testing"
)

// TestCheckListLayout: in a message a check list draws Slack's
// checkboxes (RichTextList's p-rich_text_list__check_icon: the 16px
// checkbox-filled or square SvgIcon, 8px before the text, the item 4px in
// at indent 0, 28px per indent step deeper); the next lines wrap back
// under the icon. Where the rich_text renderable doesn't allow them it's a
// bullet list.
func TestCheckListLayout(t *testing.T) {
	st := newTestStyle()
	long := strings.Repeat("word ", 40)
	d := kitDoc(t, `[{"type":"rich_text","elements":[
		{"type":"rich_text_list","style":"check","elements":[
			{"type":"rich_text_section","checked":true,"elements":[{"type":"text","text":"Alpha"}]},
			{"type":"rich_text_section","elements":[{"type":"text","text":"Bravo `+long+`"}]}]},
		{"type":"rich_text_list","style":"check","indent":1,"elements":[
			{"type":"rich_text_section","elements":[{"type":"text","text":"Charlie"}]}]}]}]`)
	v := layoutAt(st, d, checkListOpts(), 400)
	filled, squares := imgsWithURL(&v.d, iconURL+"checkbox-filled"), imgsWithURL(&v.d, iconURL+"square")
	if len(filled) != 1 || len(squares) != 2 {
		t.Fatalf("%d filled and %d empty checkboxes, want 1 and 2", len(filled), len(squares))
	}
	a := findFrag(t, &v.d, "Alpha")
	if r := filled[0].r; r.Min.X != 4 || r.Dx() != 16 || r.Min.Y != a.top+(a.bot-a.top-16)/2 {
		t.Errorf("checked icon %v, want 16px at x 4 centred on Alpha's line %d-%d", r, a.top, a.bot)
	}
	if int(a.x) != 4+16+8 {
		t.Errorf("Alpha at x %.0f, want 28", a.x)
	}
	// Bravo's item wraps: its second line starts under the icon
	b := findFrag(t, &v.d, "Bravo")
	second := -1
	for _, f := range v.d.frags {
		if f.top >= b.bot && f.top < b.bot+1 && f.kind == fText && second < 0 {
			second = int(f.x)
		}
	}
	if second != 4 {
		t.Errorf("Bravo's second line starts at %d, want 4 (under the icon)", second)
	}
	if c := findFrag(t, &v.d, "Charlie"); int(c.x) != 56+16+8 || squares[1].r.Min.X != 56 {
		t.Errorf("indent 1: icon at %d, text at %.0f; want 56 and 80", squares[1].r.Min.X, c.x)
	}
	// elsewhere: bullets, no checkboxes
	v = layoutAt(st, d, Options{}, 400)
	if n := len(imgsWithURL(&v.d, iconURL+"square")) + len(imgsWithURL(&v.d, iconURL+"checkbox-filled")); n != 0 {
		t.Errorf("%d checkboxes outside a message, want bullets", n)
	}
	bullets := 0
	for _, bx := range v.d.boxes {
		if bx.kind == bCircle || bx.kind == bRing {
			bullets++
		}
	}
	if bullets != 3 {
		t.Errorf("%d bullets, want 3", bullets)
	}
}
