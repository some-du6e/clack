package richview

import (
	"image"
	"strings"
	"testing"
)

// TestAlertWidth: a message's alert is its block wrapper's flex item
// (.p-block_kit_renderer__block_wrapper{display:flex}; .c-alert sets no
// width), so its box shrinks to the text (1px border, 16px padding, the
// 20px icon and 12px, the text, 16px, 1px) and wraps only at the column.
// A callout's child (renderBlocks: no wrapper) fills the callout.
func TestAlertWidth(t *testing.T) {
	st := newTestStyle()
	long := strings.Repeat("word ", 60)
	d := kitDoc(t, `[
		{"type":"alert","text":{"type":"plain_text","text":"Alfa"},"level":"info"},
		{"type":"alert","text":{"type":"plain_text","text":"`+long+`Bravo"},"level":"warning"},
		{"type":"callout","child_blocks":[{"type":"alert","text":{"type":"plain_text","text":"Charlie"},"level":"error"}]}
	]`)
	v := layoutAt(st, d, Options{}, 800)
	var alerts []box
	var callout *box
	for i := range v.d.boxes {
		switch b := &v.d.boxes[i]; {
		case b.radius == 8 && b.kind == bFill:
			alerts = append(alerts, *b)
		case b.radius == 12 && b.kind == bBorder:
			callout = b
		}
	}
	if len(alerts) != 3 || callout == nil {
		t.Fatalf("%d alert boxes and callout %v, want 3 and one", len(alerts), callout)
	}
	alfa := findFrag(t, &v.d, "Alfa")
	if want := int(alfa.x+alfa.w+0.999) + 16 + 1; alerts[0].r.Min.X != 0 || alerts[0].r.Max.X != want {
		t.Errorf("short alert %v, want 0 to %d (its text and padding)", alerts[0].r, want)
	}
	if alerts[1].r.Dx() != 800 {
		t.Errorf("long alert %v, want the column's 800px", alerts[1].r)
	}
	if want := callout.r.Inset(1 + 12); alerts[2].r.Min.X != want.Min.X || alerts[2].r.Max.X != want.Max.X {
		t.Errorf("callout's alert %v, want the callout's inner %d to %d", alerts[2].r, want.Min.X, want.Max.X)
	}
}

// TestBoxedBlocksLayout: alert, callout and loading blocks lay out as
// Slack's client-boot-styles draw them (kitcards.go): the alert's 8px
// block margin (kept on the first block), its box with 12/16 padding,
// the 20px level icon 1px down and the text 12px right of it; the
// callout's 12px-radius bordered box 12px around its children, whose
// margins collapse; the loading block's 52px spinner, centred.
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
		case b.radius == 8 && b.r.Min.Y == 8:
			alertBox = b
		case b.radius == 12 && b.kind == bBorder:
			calloutBox = b
		}
	}
	if alertBox == nil || alertBox.r.Dy() != 1+12+22+12+1 {
		t.Fatalf("alert box %+v, want one at y 8, 48 tall", alertBox)
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
	// the loading block's spinner: 52px, centred (margin: 0 auto), its
	// ring filling the box (noMargin)
	if len(v.d.spinners) != 1 || v.d.spinners[0].r != image.Rect(374, calloutBox.r.Max.Y, 426, calloutBox.r.Max.Y+52) ||
		!spinFills(v.d.spinners[0]) {
		t.Errorf("loading spinner %+v, want 52px at (374, %d) with its ring filling it", v.d.spinners, calloutBox.r.Max.Y)
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

// TestContainerLayout: a container block lays out as Slack's
// ContainerBlock (yWQF, .containerBlock*): 16px down (kept on the first
// block), a 480px standard box with radius 12, the 12px-padded header
// with the 36px icon and the title 12px right of it, centred on the row;
// the bare children 12px in, the first without its top margin; a
// collapsible container's header hides and shows them.
func TestContainerLayout(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[
		{"type":"container","title":{"type":"plain_text","text":"Alpha"},"subtitle":{"type":"plain_text","text":"Bravo"},
		 "icon":{"type":"image","image_url":"https://img.test/c.png","alt_text":"c"},
		 "child_blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Charlie"}}]},
		{"type":"container","title":{"type":"plain_text","text":"Delta"},"is_collapsible":true,"width":"narrow",
		 "child_blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Echo"}}]}
	]`)
	v := layoutAt(st, d, Options{}, 800)
	var boxes []box
	for _, b := range v.d.boxes {
		if b.kind == bBorder && b.radius == 12 {
			boxes = append(boxes, b)
		}
	}
	if len(boxes) != 2 {
		t.Fatalf("container boxes %d, want 2", len(boxes))
	}
	c1, c2 := boxes[0], boxes[1]
	if c1.r.Min != image.Pt(0, 16) || c1.r.Dx() != 480 || c2.r.Dx() != 320 {
		t.Errorf("containers %v and %v, want 480 wide at (0, 16) and 320 wide", c1.r, c2.r)
	}
	icons := imgsWithURL(&v.d, "https://img.test/c.png")
	if len(icons) != 1 {
		t.Fatalf("icons %d", len(icons))
	}
	ic := icons[0].r
	// title 23 + subtitle 16 = 39 > 36: the icon is centred on the 39px row
	if want := image.Rect(13, 16+13+1, 13+36, 16+13+1+36); ic != want {
		t.Errorf("icon %v, want %v", ic, want)
	}
	a, b, c := findFrag(t, &v.d, "Alpha"), findFrag(t, &v.d, "Bravo"), findFrag(t, &v.d, "Charlie")
	if int(a.x) != ic.Max.X+12 || a.top != 16+13 || b.top != a.top+23 || b.size != 13 {
		t.Errorf("title at (%.0f, %d), subtitle top %d (%dpx); want (%d, %d), %d, 13px", a.x, a.top, b.top, b.size, ic.Max.X+12, 16+13, 16+13+23)
	}
	// the header ends 12px under the row; the first child has no top margin
	if want := 16 + 1 + 12 + 39 + 12; c.top != want || int(c.x) != 13 {
		t.Errorf("child at (%.0f, %d), want (13, %d)", c.x, c.top, want)
	}
	if want := c.bot + 4 + 12 + 1; c1.r.Max.Y != want {
		t.Errorf("container bottom %d, want %d (the section's 4px, 12px padding, border)", c1.r.Max.Y, want)
	}
	if !hasFrag(&v.d, "Echo") {
		t.Fatal("the collapsible container starts open")
	}
	tc := int16(-1)
	for i, tg := range v.d.targets {
		if tg.kind == tCollapse {
			tc = int16(i)
		}
	}
	if tc < 0 {
		t.Fatal("no header toggle")
	}
	h := &recHandler{}
	v.activate(tc, image.Point{}, h)
	v.relayout(st, 800, 1, 1)
	if hasFrag(&v.d, "Echo") || !hasFrag(&v.d, "Delta") {
		t.Errorf("collapsed container: %q", fragStrings(&v.d))
	}
}
