package richview

import (
	"image"
	"strconv"
	"testing"
	"time"
)

// TestCallCardLayout: a call block lays out as Slack's PlatformCallSubtype
// (boot-shared 9CtN, client-boot-styles .p-call_subtype*): "Call" and
// its toggle, then 4px down the card (≤440 wide, 1px border, 8px
// corners): the 16px-padded header with the 36px app icon and the title
// 12px right of it, the "Meeting ID" body, the footer with the 36px
// faces and the primary Join button (min-width 80) at its right edge.
// Join opens the call's join_url; the toggle hides the card.
func TestCallCardLayout(t *testing.T) {
	st := newTestStyle()
	start := strconv.FormatInt(time.Now().Unix()-600, 10)
	d := kitDoc(t, `[{"type":"call","call_id":"R1","call":{"v1":{"id":"R1","name":"Standup","app_id":"A1",
		"app_icon_urls":{"image_72":"https://call.test/72.png"},"date_start":`+start+`,"display_id":"123-456",
		"join_url":"https://call.test/j","active_participants":[{"slack_id":"U1"},{"slack_id":"U2"}],"all_participants":[]}}}]`)
	v := layoutAt(st, d, Options{}, 800)
	lbl := findFrag(t, &v.d, "Call")
	var card *box
	for i := range v.d.boxes {
		b := &v.d.boxes[i]
		if b.kind == bBorder && b.radius == 8 && b.r.Dx() == 440 && b.fill == st.Palette.Panel {
			card = b
			break
		}
	}
	if card == nil {
		t.Fatalf("no 440px call card; boxes %+v", v.d.boxes)
	}
	if card.r.Min.Y != lbl.bot+4 || card.r.Min.X != 0 {
		t.Errorf("card at %v, want 4px under the Call line (bottom %d) at x 0", card.r.Min, lbl.bot)
	}
	icons := imgsWithURL(&v.d, "https://call.test/72.png")
	if len(icons) != 1 {
		t.Fatalf("app icons %d", len(icons))
	}
	ic := icons[0].r
	if want := image.Rect(17, card.r.Min.Y+17, 17+36, card.r.Min.Y+17+36); ic != want {
		t.Errorf("icon %v, want %v (1px border + 16px padding, 36px)", ic, want)
	}
	title := findFrag(t, &v.d, "Standup")
	if int(title.x) != ic.Max.X+12 || title.top != ic.Min.Y {
		t.Errorf("title at (%.0f, %d), want (%d, %d)", title.x, title.top, ic.Max.X+12, ic.Min.Y)
	}
	for _, w := range []string{"Started", "Meeting", "ID:", "Join"} {
		if !hasFrag(&v.d, w) {
			t.Errorf("no %q in the card; have %q", w, fragStrings(&v.d))
		}
	}
	join := findFrag(t, &v.d, "Join")
	var btn *box
	for i := range v.d.boxes {
		b := &v.d.boxes[i]
		if b.kind == bFill && b.fill == st.Palette.Primary && b.r.Dy() == 36 {
			btn = b
		}
	}
	if btn == nil {
		t.Fatal("no 36px primary Join button")
	}
	if btn.r.Max.X != card.r.Max.X-17 || btn.r.Dx() < 80 || join.x < float32(btn.r.Min.X) || join.x+join.w > float32(btn.r.Max.X) {
		t.Errorf("Join button %v (label at %.0f+%.0f), want ≥80 wide, 16px inside the card's right edge %d", btn.r, join.x, join.w, card.r.Max.X)
	}
	faces := 0
	for _, b := range boxesOf(&v.d, bFill) {
		if b.r.Dx() == 36 && b.r.Dy() == 36 && b.fill == st.Palette.Placeholder {
			faces++
		}
	}
	if faces != 2 {
		t.Errorf("facepile faces %d, want 2", faces)
	}
	// Join opens the join URL
	h := &recHandler{}
	tj := int16(-1)
	for i, tg := range v.d.targets {
		if tg.kind == tCard && tg.url == "https://call.test/j" {
			tj = int16(i)
		}
	}
	if tj < 0 {
		t.Fatal("no Join target")
	}
	v.activate(tj, btn.r.Min, h)
	if len(h.urls) != 1 || h.urls[0] != "https://call.test/j" {
		t.Errorf("Join opened %q", h.urls)
	}
	// the toggle hides the card
	tc := int16(-1)
	for i, tg := range v.d.targets {
		if tg.kind == tCollapse {
			tc = int16(i)
		}
	}
	if tc < 0 {
		t.Fatal("no Call toggle")
	}
	v.activate(tc, image.Point{}, h)
	v.relayout(st, 800, 1, 1)
	if hasFrag(&v.d, "Standup") || !hasFrag(&v.d, "Call") {
		t.Errorf("collapsed call still draws its card: %q", fragStrings(&v.d))
	}
}
