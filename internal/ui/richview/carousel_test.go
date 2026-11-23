package richview

import (
	"fmt"
	"image"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/pointer"
)

// Synthetic fixtures only.

// carouselOf is a carousel of n cards; card 1 has a body, so it's the
// tallest.
func carouselOf(n int, extra string) string {
	var cs []string
	for i := range n {
		c := fmt.Sprintf(`{"type":"card","block_id":"c%d","title":{"type":"plain_text","text":"Card %d"}`, i, i)
		if i == 1 {
			c += `,"body":{"type":"mrkdwn","text":"A body that runs over a few lines so this card is taller than the rest of them."}`
		}
		cs = append(cs, c+"}")
	}
	return `[{"type":"carousel","block_id":"cr"` + extra + `,"elements":[` + strings.Join(cs, ",") + `]}]`
}

func oneCarousel(t *testing.T, v *View) CarouselRect {
	t.Helper()
	cs := v.CarouselRects()
	if len(cs) != 1 {
		t.Fatalf("%d carousels, want 1", len(cs))
	}
	return cs[0]
}

// cardFrame is the card block's frame in a carousel card's view.
func cardFrame(t *testing.T, cv *View) image.Rectangle {
	t.Helper()
	for _, b := range cv.d.boxes {
		if b.kind == bBorder && b.radius == 12 {
			return b.r
		}
	}
	t.Fatal("no card frame")
	return image.Rectangle{}
}

// GalleryScroller's cards: 340px each, 16px apart, the scroller the
// view's width and as tall as the tallest card, every card's frame
// stretched to it; the right arrow while more is to come, no left one at
// the start.
func TestCarouselLayout(t *testing.T) {
	hs := newHarness(kitDoc(t, carouselOf(4, "")), &recHandler{})
	hs.w = 600
	hs.frame()
	c := oneCarousel(t, hs.v)
	if c.R.Dx() != 600 || len(c.Cards) != 4 {
		t.Fatalf("scroller %v, %d cards", c.R, len(c.Cards))
	}
	for k, r := range c.Cards {
		if r.Dx() != 340 || r.Min.X != c.R.Min.X+k*356 {
			t.Errorf("card %d at %v, want 340 wide at %d", k, r, c.R.Min.X+k*356)
		}
	}
	tall := 0
	for k := range 4 {
		tall = max(tall, cardFrame(t, hs.v.CarouselCard(0, k)).Dy())
	}
	if c.R.Dy() != tall {
		t.Errorf("scroller %d tall, the tallest card %d", c.R.Dy(), tall)
	}
	for k := range 4 {
		if h := cardFrame(t, hs.v.CarouselCard(0, k)).Dy(); h != tall {
			t.Errorf("card %d's frame %d tall, want the tallest's %d (stretch)", k, h, tall)
		}
	}
	if !c.Left.Empty() || c.Right.Empty() || c.Right.Max.X != c.R.Max.X-12 || c.Right.Dx() != 32 {
		t.Errorf("arrows left %v right %v: want only the right one, 32px, 12px in", c.Left, c.Right)
	}
	// one card fits: no arrows
	hs = newHarness(kitDoc(t, carouselOf(1, "")), &recHandler{})
	hs.w = 600
	hs.frame()
	if c := oneCarousel(t, hs.v); !c.Left.Empty() || !c.Right.Empty() {
		t.Errorf("one card: arrows %v %v", c.Left, c.Right)
	}
}

// An arrow scrolls a page (width - width % 356: 356 at 600px), clamped,
// and to the end when that's under 50px away; the left arrow shows once
// it's scrolled, the right one hides at the end.
func TestCarouselArrows(t *testing.T) {
	hs := newHarness(kitDoc(t, carouselOf(4, "")), &recHandler{})
	hs.w = 600
	hs.frame()
	// 4 cards: 1408px of content, 808px to scroll
	for i, want := range []float32{356, 712, 808} {
		hs.clickRect(oneCarousel(t, hs.v).Right, 0)
		if c := oneCarousel(t, hs.v); c.Scroll != want {
			t.Fatalf("right %d: scroll %v, want %v", i+1, c.Scroll, want)
		}
	}
	c := oneCarousel(t, hs.v)
	if !c.Right.Empty() || c.Left.Empty() {
		t.Errorf("at the end: left %v right %v, want only the left one", c.Left, c.Right)
	}
	if c.Cards[3].Max.X != c.R.Max.X {
		t.Errorf("at the end the last card ends at %d, want the scroller's right %d", c.Cards[3].Max.X, c.R.Max.X)
	}
	for _, want := range []float32{452, 96} {
		hs.clickRect(oneCarousel(t, hs.v).Left, 0)
		if c := oneCarousel(t, hs.v); c.Scroll != want {
			t.Fatalf("left: scroll %v, want %v", c.Scroll, want)
		}
	}
	hs.clickRect(oneCarousel(t, hs.v).Left, 0) // 96 - 356 clamps to 0
	if c := oneCarousel(t, hs.v); c.Scroll != 0 || !c.Left.Empty() {
		t.Errorf("back at the start: scroll %v, left %v", c.Scroll, c.Left)
	}
}

// A sideways wheel scroll over the cards moves them, up to the ends.
func TestCarouselWheel(t *testing.T) {
	hs := newHarness(kitDoc(t, carouselOf(4, "")), &recHandler{})
	hs.w = 600
	hs.frame()
	c := oneCarousel(t, hs.v)
	p := f32.Pt(float32(c.R.Min.X+100), float32(c.R.Min.Y+c.R.Dy()/2))
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p},
		pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: p, Scroll: f32.Pt(120, 0)})
	if c := oneCarousel(t, hs.v); c.Scroll != 120 {
		t.Fatalf("scroll %v after a 120px wheel, want 120", c.Scroll)
	}
	hs.send(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: p, Scroll: f32.Pt(5000, 0)})
	if c := oneCarousel(t, hs.v); c.Scroll != 808 {
		t.Errorf("scroll %v past the end, want 808", c.Scroll)
	}
}

// isLabelVisible draws the label above the scroller.
func TestCarouselLabel(t *testing.T) {
	v := layoutAt(newTestStyle(), kitDoc(t, carouselOf(2, `,"label":"Picks","is_label_visible":true`)), Options{}, 600)
	if !hasWord(v, "Picks") {
		t.Errorf("no visible label")
	}
	if c := oneCarousel(t, v); c.R.Min.Y <= 8 {
		t.Errorf("scroller at %d, want under the label", c.R.Min.Y)
	}
	if v := layoutAt(newTestStyle(), kitDoc(t, carouselOf(2, `,"label":"Picks"`)), Options{}, 600); hasWord(v, "Picks") {
		t.Errorf("the label drew without isLabelVisible")
	}
}
