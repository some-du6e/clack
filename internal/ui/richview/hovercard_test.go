package richview

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	rt "github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/hovercard"
)

// An @mention is Slack's MemberLink, a ProfileHoverTrigger: resting on it
// reports the member at the mention's bounds; plain text reports nothing;
// a click closes the card.
func TestMentionHoverCard(t *testing.T) {
	c := &hovercard.Controller{}
	ptr := image.Point{}
	c.Pointer = func() image.Point { return ptr }
	hovercard.SetDefault(c)
	defer hovercard.SetDefault(nil)
	t0 := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

	hs := newHarness(doc(para(txt("ping "), rt.Span{Kind: rt.User, Text: "@robin", ID: "U0FAKEPROF"}, txt(" please"))), &recHandler{})
	at := func(p f32.Point) {
		ptr = image.Pt(int(p.X+.5), int(p.Y+.5)) // the view sits at the window origin
		hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	}
	f := findFrag(t, &hs.v.d, "@robin")
	at(center(findFrag(t, &hs.v.d, "please")))
	c.Frame(t0, false, false)
	if _, ok := c.Pending(); ok {
		t.Fatal("plain text reported a trigger")
	}
	at(center(f))
	hs.frame()
	c.Frame(t0, false, false)
	req, ok := c.Pending()
	if !ok || req.UserID != "U0FAKEPROF" {
		t.Fatalf("hovering the mention: pending=%v %+v", ok, req)
	}
	if want := fragRect(f); req.Anchor != want {
		t.Errorf("anchor %v, want the mention's bounds %v", req.Anchor, want)
	}
	hs.frame()
	c.Frame(t0.Add(hovercard.ShowDelay), false, false)
	if _, _, open := c.Open(); !open {
		t.Fatal("no card after 350ms on the mention")
	}
	p := center(f)
	hs.send(press(p, time.Second), release(p, time.Second+20*time.Millisecond))
	c.Frame(t0.Add(time.Second), false, false)
	if _, _, open := c.Open(); open {
		t.Fatal("clicking the mention left the hover card open")
	}
}
