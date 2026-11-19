package richview

import (
	"image"
	"testing"
)

// TestCardLayout: a card lays out as Slack's Card (7Xkb on aJif,
// client-boot-styles .card*): 8px down, at most 340px wide (at least 180),
// the 1px otl-ter border with radius 12; 12px padding, 12px between the
// parts: the hero image at the content's width and 4:3 with radius 4,
// the header with the 36px icon and the title 12px right of it, the
// actions at the bottom against the right edge.
func TestCardLayout(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[{"type":"card","block_id":"c1","hero_image":{"type":"image","image_url":"https://img.test/h.png","alt_text":"h"},
		"icon":{"type":"image","image_url":"https://img.test/i.png","alt_text":"i"},"title":{"type":"plain_text","text":"Alpha"},
		"body":{"type":"mrkdwn","text":"Bravo"},
		"actions":[{"type":"button","action_id":"a","text":{"type":"plain_text","text":"Charlie"}}]}]`)
	v := layoutAt(st, d, Options{}, 800)
	var frame *box
	for i := range v.d.boxes {
		if b := &v.d.boxes[i]; b.kind == bBorder && b.radius == 12 {
			frame = b
			break
		}
	}
	if frame == nil || frame.r.Min != image.Pt(0, 8) || frame.r.Dx() != 340 {
		t.Fatalf("card frame %+v, want 340 wide at (0, 8)", frame)
	}
	heroes, icons := imgsWithURL(&v.d, "https://img.test/h.png"), imgsWithURL(&v.d, "https://img.test/i.png")
	if len(heroes) != 1 || len(icons) != 1 {
		t.Fatalf("%d hero images, %d icons", len(heroes), len(icons))
	}
	hero, ic := heroes[0].r, icons[0].r
	inner := 340 - 2*13
	if want := image.Rect(13, 8+13, 13+inner, 8+13+int(float32(inner)/1.33333+0.5)); hero != want || heroes[0].radii[0] != 4 {
		t.Errorf("hero %v (radius %d), want %v with radius 4", hero, heroes[0].radii[0], want)
	}
	if ic.Min != image.Pt(13, hero.Max.Y+12) || ic.Dx() != 36 || !icons[0].fit {
		t.Errorf("icon %v (fit %v), want 36px contained at (13, %d)", ic, icons[0].fit, hero.Max.Y+12)
	}
	if a := findFrag(t, &v.d, "Alpha"); int(a.x) != ic.Max.X+12 {
		t.Errorf("title at x %.0f, want %d", a.x, ic.Max.X+12)
	}
	b := findFrag(t, &v.d, "Bravo")
	if b.top != ic.Max.Y+12 || int(b.x) != 13 {
		t.Errorf("body at (%.0f, %d), want (13, %d)", b.x, b.top, ic.Max.Y+12)
	}
	c := findFrag(t, &v.d, "Charlie")
	var btn *box
	for i := range v.d.boxes {
		if bx := &v.d.boxes[i]; bx.r.Min.Y > b.bot && bx.r.Max.X <= 340 && bx.r.Min.X <= int(c.x) && bx.r.Max.X >= int(c.x+c.w) && bx.r.Dy() >= 28 {
			btn = bx
		}
	}
	if btn == nil || btn.r.Max.X != 340-13 || btn.r.Min.Y != b.bot+12 {
		t.Errorf("button %+v, want its right edge at %d, 12px under the body (%d)", btn, 340-13, b.bot+12)
	}
	if frame.r.Max.Y != btn.r.Max.Y+13 {
		t.Errorf("card bottom %d, want the button's %d + 12 + 1", frame.r.Max.Y, btn.r.Max.Y)
	}
	// a narrow column: the card keeps its 180px minimum
	v = layoutAt(st, d, Options{}, 150)
	for i := range v.d.boxes {
		if b := &v.d.boxes[i]; b.kind == bBorder && b.radius == 12 {
			if b.r.Dx() != 180 {
				t.Errorf("narrow card %v, want 180 wide", b.r)
			}
			break
		}
	}
}
