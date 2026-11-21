package richview

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/unit"

	rt "github.com/some-du6e/clack/internal/richtext"
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

// fixedElements is an Elements host whose button runs are 60×36 per
// button, 8px apart, and draw nothing.
type fixedElements struct{}

func (fixedElements) ElementsSize(bs []rt.Button, maxW int, _ bool, _ unit.Metric) (image.Point, bool) {
	return image.Pt(min(60*len(bs)+8*(len(bs)-1), maxW), 36), false
}

func (fixedElements) LayoutElements(gtx layout.Context, _ []rt.Button, _ bool) layout.Dimensions {
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

func (fixedElements) ElementsVersion() uint32 { return 0 }

// TestCardActionSlots: in a message a card's buttons are the Elements
// host's (they always have an action_id), placed as Card's
// .actionsContainer places them: three or more put the danger ones in
// .actionsLeft at the content's left edge (partitionActions, fWi9), the
// rest in .actionsRight against its right edge, primary last; one row,
// 12px under the body.
func TestCardActionSlots(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[{"type":"card","block_id":"c1","title":{"type":"plain_text","text":"Alpha"},"body":{"type":"mrkdwn","text":"Bravo"},
		"actions":[{"type":"button","action_id":"no","text":{"type":"plain_text","text":"Decline"},"style":"danger"},
		{"type":"button","action_id":"yes","text":{"type":"plain_text","text":"Accept"},"style":"primary"},
		{"type":"button","action_id":"maybe","text":{"type":"plain_text","text":"Maybe"}}]}]`)
	v := layoutAt(st, d, Options{Elements: fixedElements{}}, 800)
	b := findFrag(t, &v.d, "Bravo")
	if len(v.d.slots) != 2 {
		t.Fatalf("%d slots, want the left group and the right one: %+v", len(v.d.slots), v.d.slots)
	}
	var left, right slot
	for _, s := range v.d.slots {
		if s.bs[0].ActionID == "no" {
			left = s
		} else {
			right = s
		}
	}
	ids := func(bs []rt.Button) (out []string) {
		for _, bt := range bs {
			out = append(out, bt.ActionID)
		}
		return
	}
	if got := ids(left.bs); len(got) != 1 || got[0] != "no" {
		t.Errorf("left group %q, want [no]", got)
	}
	if got := ids(right.bs); len(got) != 2 || got[0] != "maybe" || got[1] != "yes" {
		t.Errorf("right group %q, want [maybe yes] (primary last)", got)
	}
	if left.r != image.Rect(13, b.bot+12, 13+60, b.bot+12+36) {
		t.Errorf("left group at %v, want 60×36 at (13, %d)", left.r, b.bot+12)
	}
	if right.r != image.Rect(340-13-128, b.bot+12, 340-13, b.bot+12+36) {
		t.Errorf("right group at %v, want 128×36 ending at x %d, y %d", right.r, 340-13, b.bot+12)
	}
}

// TestCardIconBox: a card's icon image sits in Card's .iconImageContainer:
// a 36px ThumbnailContainer with the surface.secondary background, radius
// 8, and the image is object-fit contain at max-width/max-height 100%, so
// a small icon keeps its natural size (CSS px), centred, instead of
// growing to 36px.
func TestCardIconBox(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[{"type":"card","block_id":"c1","icon":{"type":"image","image_url":"https://img.test/i.png","alt_text":"i"},
		"title":{"type":"plain_text","text":"Alpha"}}]`)
	v := layoutAt(st, d, Options{}, 800)
	icons := imgsWithURL(&v.d, "https://img.test/i.png")
	if len(icons) != 1 || !icons[0].fit || !imgBare(icons[0]) || imgNoUp(icons[0]) != 1 {
		t.Fatalf("icon %+v, want one contained image that never grows (noUp 1 at scale 1), nothing under it", icons)
	}
	ir := icons[0].r
	found := false
	for _, b := range v.d.boxes {
		if b.kind == bFill && b.r == ir && b.radius == 8 && b.fill == (&lay{st: st}).tok("surf-sec") {
			found = true
		}
	}
	if !found {
		t.Errorf("no surface.secondary box (radius 8) at the icon's %v", ir)
	}
	for _, c := range []struct {
		r     image.Rectangle
		sz    image.Point
		f     float32
		want  image.Rectangle
		small bool
	}{
		{image.Rect(0, 0, 36, 36), image.Pt(16, 16), 1, image.Rect(10, 10, 26, 26), true},
		{image.Rect(0, 0, 72, 72), image.Pt(16, 16), 2, image.Rect(20, 20, 52, 52), true}, // 16 CSS px at 2x
		{image.Rect(0, 0, 36, 36), image.Pt(64, 16), 1, image.Rect(0, 0, 36, 36), false},  // wider than the box: contained
		{image.Rect(0, 0, 36, 36), image.Pt(36, 36), 1, image.Rect(0, 0, 36, 36), false},
	} {
		if got, small := noUpRectOf(c.r, c.sz, c.f); got != c.want || small != c.small {
			t.Errorf("noUpRect(%v, %v, %v) = %v %v, want %v %v", c.r, c.sz, c.f, got, small, c.want, c.small)
		}
	}
}

// TestCardLabelLines: a card's title and subtitle are StackedLabels in
// Panda's text styles, body 15px/1.5 (a 22.5px line) and caption
// 13px/1.25 (16.25px).
func TestCardLabelLines(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[{"type":"card","block_id":"c1","title":{"type":"plain_text","text":"Alpha"},"subtitle":{"type":"plain_text","text":"Bravo"}}]`)
	v := layoutAt(st, d, Options{}, 800)
	a, b := findFrag(t, &v.d, "Alpha"), findFrag(t, &v.d, "Bravo")
	if a.bot-a.top != 23 || b.bot-b.top != 16 || b.top != a.bot {
		t.Errorf("title line %d-%d, subtitle %d-%d; want 23px and 16px lines, one under the other", a.top, a.bot, b.top, b.bot)
	}
}
