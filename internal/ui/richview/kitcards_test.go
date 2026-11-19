package richview

import (
	"image"
	"testing"
)

// TestBoxedBlocksLayout: alert, callout and loading blocks lay out as
// Slack's client-boot-styles draw them (kitcards.go): the alert's 8px
// block margin (kept on the first block), its box with 12/16 padding,
// the 20px level icon 1px down and the text 12px right of it; the
// callout's 12px-radius bordered box 12px around its children, whose
// margins collapse; the loading block's 52px spinner.
func TestBoxedBlocksLayout(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[
		{"type":"alert","text":{"type":"plain_text","text":"Alfa"},"level":"info"},
		{"type":"callout","background_color":"blue","child_blocks":[
			{"type":"section","text":{"type":"mrkdwn","text":"Bravo"}},
			{"type":"section","text":{"type":"mrkdwn","text":"Charlie"}}]},
		{"type":"loading"}
	]`)
	v := layoutAt(st, d, Options{}, 800)
	// the alert: its 8px margin first, then the 1px border and 12px padding
	alfa := findFrag(t, &v.d, "Alfa")
	icons := imgsWithURL(&v.d, iconURL+"info")
	if len(icons) != 1 {
		t.Fatalf("info icons %d", len(icons))
	}
	ic := icons[0].r
	if want := image.Rect(17, 8+1+12+1, 17+20, 8+1+12+1+20); ic != want {
		t.Errorf("alert icon %v, want %v", ic, want)
	}
	if int(alfa.x) != ic.Max.X+12 || alfa.top != 8+1+12 {
		t.Errorf("alert text at (%.0f, %d), want (%d, %d)", alfa.x, alfa.top, ic.Max.X+12, 8+1+12)
	}
	var alertBox, calloutBox *box
	for i := range v.d.boxes {
		b := &v.d.boxes[i]
		switch {
		case b.radius == 8 && b.r.Min.Y == 8 && b.r.Dx() == 800:
			alertBox = b
		case b.radius == 12 && b.kind == bBorder:
			calloutBox = b
		}
	}
	if alertBox == nil || alertBox.r.Dy() != 1+12+22+12+1 {
		t.Fatalf("alert box %+v, want 800 wide at y 8, 48 tall", alertBox)
	}
	if calloutBox == nil {
		t.Fatal("no callout box")
	}
	// the alert's 8px bottom margin, then the callout (no margin of its own)
	if want := alertBox.r.Max.Y + 8; calloutBox.r.Min.Y != want {
		t.Errorf("callout top %d, want %d", calloutBox.r.Min.Y, want)
	}
	bravo, charlie := findFrag(t, &v.d, "Bravo"), findFrag(t, &v.d, "Charlie")
	// 1px border + 12px padding + the first section's own 8px top margin
	if want := calloutBox.r.Min.Y + 1 + 12 + 8; bravo.top != want || int(bravo.x) != 13 {
		t.Errorf("first child at (%.0f, %d), want (13, %d)", bravo.x, bravo.top, want)
	}
	// collapsed margins: max(4, 8) between the two sections
	if want := bravo.bot + 8; charlie.top != want {
		t.Errorf("second child top %d, want %d (margins collapse)", charlie.top, want)
	}
	if want := charlie.bot + 4 + 12 + 1; calloutBox.r.Max.Y != want {
		t.Errorf("callout bottom %d, want %d", calloutBox.r.Max.Y, want)
	}
	if len(v.d.spinners) != 1 || v.d.spinners[0].r != image.Rect(0, calloutBox.r.Max.Y, 52, calloutBox.r.Max.Y+52) {
		t.Errorf("loading spinner %+v, want 52px at (0, %d)", v.d.spinners, calloutBox.r.Max.Y)
	}
}

// TestRichHeaderDividerLayout: rich_text_header and rich_text_divider
// lay out as client-boot-styles draw them: .p-rich_text_header margin
// 1em 0 4px (none above the rich_text block's first child), level_1
// 18px/24, level_2 15px/22; .p-rich_text_divider a 1px rule with 8px
// margins; siblings' margins collapse.
func TestRichHeaderDividerLayout(t *testing.T) {
	st := newTestStyle()
	sec := func(s string) string {
		return `{"type":"rich_text_section","elements":[{"type":"text","text":"` + s + `"}]}`
	}
	d := kitDoc(t, `[{"type":"rich_text","elements":[
		{"type":"rich_text_header","level":1,"elements":[{"type":"text","text":"Alpha"}]},`+sec("Bravo")+`,
		{"type":"rich_text_divider"},`+sec("Charlie")+`,
		{"type":"rich_text_header","level":2,"elements":[{"type":"text","text":"Delta"}]},`+sec("Echo")+`]}]`)
	v := layoutAt(st, d, Options{}, 600)
	a, b, c := findFrag(t, &v.d, "Alpha"), findFrag(t, &v.d, "Bravo"), findFrag(t, &v.d, "Charlie")
	dl, e := findFrag(t, &v.d, "Delta"), findFrag(t, &v.d, "Echo")
	if a.top != 0 || a.bot != 24 || a.size != 18 {
		t.Errorf("level 1 header line %d–%d at %dpx, want 0–24 at 18px", a.top, a.bot, a.size)
	}
	if want := a.bot + 4; b.top != want {
		t.Errorf("text after the header at %d, want %d", b.top, want)
	}
	var rule *box
	for _, bx := range boxesOf(&v.d, bFill) {
		if bx.r.Dy() == 1 && bx.fill == st.Palette.Divider {
			rule = &bx
		}
	}
	if rule == nil || rule.r.Min.Y != b.bot+8 || rule.r.Dx() != 600 {
		t.Fatalf("divider %+v, want a 600px rule at %d", rule, b.bot+8)
	}
	if want := rule.r.Max.Y + 8; c.top != want {
		t.Errorf("text after the divider at %d, want %d", c.top, want)
	}
	if want := c.bot + 15; dl.top != want || dl.size != 15 {
		t.Errorf("level 2 header at %d (%dpx), want %d at 15px", dl.top, dl.size, want)
	}
	if want := dl.bot + 4; e.top != want {
		t.Errorf("text after the second header at %d, want %d", e.top, want)
	}
}
