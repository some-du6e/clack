package richview

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/hovercard"
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

// A canvas's channel mention (Options.Canvas) reports the canvas's own
// card (Request.Canvas, HovercardImpl's 250ms open), not the message
// ChannelHoverCard; a channel-tab slug whose tab has a peek (TabSlugPeek)
// reports the tab's peek (Request.Tab), one without (TabSlugCard) the
// channel card.
func TestCanvasChannelAndTabSlugCards(t *testing.T) {
	c := &hovercard.Controller{}
	ptr := image.Point{}
	c.Pointer = func() image.Point { return ptr }
	hovercard.SetDefault(c)
	defer hovercard.SetDefault(nil)
	t0 := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	hover := func(hs *harness, word string) hovercard.Request {
		t.Helper()
		p := center(findFrag(t, &hs.v.d, word))
		ptr = image.Pt(int(p.X+.5), int(p.Y+.5))
		hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
		c.Frame(t0, false, false)
		req, ok := c.Pending()
		if !ok {
			t.Fatalf("hovering %q reported nothing", word)
		}
		return req
	}

	d := doc(para(txt("see "), rt.Span{Kind: rt.Channel, Text: "#general", ID: "C0GENERAL1"}, txt(" today")))
	hs := newHarness(d, &recHandler{})
	hs.v = New(d, Options{Canvas: true})
	hs.frame()
	req := hover(hs, "#general")
	if !req.Canvas || req.ChannelID != "C0GENERAL1" || req.Buttons || req.Delay != hovercard.CanvasShowDelay {
		t.Fatalf("canvas channel mention: %+v", req)
	}
	hs.frame()
	c.Frame(t0.Add(hovercard.CanvasShowDelay), false, false)
	if r, _, open := c.Open(); !open || !r.Canvas {
		t.Fatalf("no canvas card after 250ms: %+v %v", r, open)
	}
	c.Close()

	// the same mention in a message: the ChannelHoverCard
	hs = newHarness(d, &recHandler{})
	req = hover(hs, "#general")
	if req.Canvas || !req.Buttons || req.Delay != 0 {
		t.Fatalf("message channel mention: %+v", req)
	}
	c.Close()

	// channel-tab slugs
	defer func(p, cd func(ch, tab string) bool) { TabSlugPeek, TabSlugCard = p, cd }(TabSlugPeek, TabSlugCard)
	TabSlugPeek = func(ch, tab string) bool { return tab == "code" }
	TabSlugCard = func(ch, tab string) bool { return tab != "code" }
	slug := func(tab, text string) rt.Span {
		return rt.Span{Kind: rt.Channel, Text: text, ID: "C0CODE00001", URL: "https://fake.slack.com/archives/C0CODE00001/tabs/" + tab}
	}
	hs = newHarness(doc(para(slug("code", "Code"), txt(" and "), slug("Ct0GONE", "Gone"))), &recHandler{})
	req = hover(hs, "Code")
	if req.Tab != "code" || req.ChannelID != "C0CODE00001" || req.Buttons {
		t.Fatalf("the Code tab's slug: %+v, want its peek", req)
	}
	c.Close()
	req = hover(hs, "Gone")
	if req.Tab != "" || req.ChannelID != "C0CODE00001" || !req.Buttons {
		t.Fatalf("an unresolved tab's slug: %+v, want the channel card", req)
	}
}
