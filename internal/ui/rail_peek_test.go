package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// PeekTrigger's isMouseInsideSafeArea: the button and the card, their
// union until a previous position exists, then the triangle from the
// previous position to the card's outermost corners.
func TestRailPeekSafeArea(t *testing.T) {
	trig := image.Rect(9, 300, 61, 368)   // the More button
	card := image.Rect(67, 256, 367, 460) // right-bottom, offset 6 / -44
	for _, tc := range []struct {
		name string
		e    image.Point
		prev image.Point
		has  bool
		want bool
	}{
		{"on the button", image.Pt(30, 330), image.Point{}, false, true},
		{"on the card", image.Pt(200, 400), image.Point{}, false, true},
		{"between them, no previous move (union)", image.Pt(64, 330), image.Point{}, false, true},
		{"far below, no previous move", image.Pt(30, 600), image.Point{}, false, false},
		{"heading for the card", image.Pt(66, 340), image.Pt(60, 340), true, true},
		{"heading down the rail", image.Pt(40, 420), image.Pt(40, 360), true, false},
		{"into the sidebar below the card", image.Pt(200, 500), image.Pt(62, 340), true, false},
	} {
		if got := railPeekSafe(tc.e, trig, card, tc.prev, tc.has); got != tc.want {
			t.Errorf("%s: safe = %v, want %v", tc.name, got, tc.want)
		}
	}
	// getSafeTriangleVertices from a point left of the card: its top-left
	// and bottom-left corners (smallest and largest angle)
	a, b := safeTriangle(image.Pt(60, 340), card)
	if a != image.Pt(67, 256) || b != image.Pt(67, 460) {
		t.Errorf("triangle corners %v %v, want the card's left corners", a, b)
	}
}

func (e *perfEnv) movePtr(p image.Point) {
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
	e.frame()
}

func railPeekMenu(e *perfEnv) *popups.Menu {
	if !e.a.railPeekOpen() {
		return nil
	}
	return e.a.peek.menu
}

// The More button opens its menu like Slack's: 300 ms after the pointer
// comes onto it, without taking the window (no scrim); moving away closes
// it; a click keeps it until a click outside (Karim, 2026-11-10: "the More
// popover stays open after the pointer leaves").
func TestRailMorePeek(t *testing.T) {
	e, _ := previewEnv(t)
	e.settle(100 * time.Millisecond)
	trig := e.a.peek.trig
	if trig.Empty() {
		t.Fatal("no More button in the rail")
	}
	mid := trig.Min.Add(trig.Size().Div(2))
	away := image.Pt(trig.Max.X+400, trig.Max.Y+300)

	e.movePtr(away)
	e.movePtr(mid)
	t0 := time.Now()
	for time.Since(t0) < 150*time.Millisecond {
		e.frame()
		time.Sleep(5 * time.Millisecond)
	}
	if railPeekMenu(e) != nil {
		t.Fatal("the peek opened before the 300 ms trigger delay")
	}
	e.until("the hover peek", 3*time.Second, func() bool { return railPeekMenu(e) != nil })
	if d := time.Since(t0); d < railPeekDelay {
		t.Fatalf("opened after %v, before the %v delay", d, railPeekDelay)
	}
	m := railPeekMenu(e)
	if !m.Peeking() {
		t.Fatal("a hover peek opened as a pinned menu")
	}
	e.settle(30 * time.Millisecond)
	card := m.Rect()
	if card.Empty() {
		t.Fatal("the peek has no card")
	}
	// right-bottom, offsetX 6, offsetY -44 (allowanceY 4 keeps it in the window)
	if card.Min.X != trig.Max.X+6 {
		t.Errorf("card at x %d, want the button's right edge + 6 = %d", card.Min.X, trig.Max.X+6)
	}
	if want := max(trig.Min.Y-44, 4); card.Min.Y != want && card.Max.Y != e.size.Y-4 {
		t.Errorf("card top %d, want %d (button top − 44)", card.Min.Y, want)
	}
	e.shot("rail-more-peek")
	// through the card it stays; out of the safe area it closes at once
	e.movePtr(image.Pt(card.Min.X+20, card.Min.Y+card.Dy()/2))
	if railPeekMenu(e) == nil {
		t.Fatal("moving onto the card closed the peek")
	}
	e.movePtr(away)
	e.frame()
	if railPeekMenu(e) != nil {
		t.Fatal("the hover peek stayed open after the pointer left the button and the card")
	}

	// a click opens it at once and keeps it
	e.movePtr(mid)
	e.click(mid)
	e.frame()
	e.frame()
	m = railPeekMenu(e)
	if m == nil || m.Peeking() {
		t.Fatalf("clicking More: menu %v, peeking %v", m != nil, m != nil && m.Peeking())
	}
	e.movePtr(away)
	e.settle(50 * time.Millisecond)
	if railPeekMenu(e) == nil {
		t.Fatal("the clicked menu closed when the pointer left")
	}
	e.click(away) // outside: the scrim closes it
	e.frame()
	e.frame()
	if railPeekMenu(e) != nil {
		t.Fatal("a click outside left the clicked menu open")
	}
}
