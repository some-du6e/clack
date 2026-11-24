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

// Options.NoLinking is SimpleRichTextPreview without withSlackLinks: a
// channel's, a member's and a link's spans are drawn, but no target is
// under the pointer, so no hover card and no click; without it the
// channel mention is BaseMrkdwnChannel's link, its ChannelHoverCard.
func TestNoLinkingMentionsAreInert(t *testing.T) {
	c := &hovercard.Controller{}
	ptr := image.Point{}
	c.Pointer = func() image.Point { return ptr }
	hovercard.SetDefault(c)
	defer hovercard.SetDefault(nil)
	t0 := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	d := doc(para(txt("see "), rt.Span{Kind: rt.Channel, Text: "#plans", ID: "C0NOLINK01"}, txt(" and "),
		rt.Span{Kind: rt.User, Text: "@robin", ID: "U0NOLINK01"}, txt(" at "), rt.Span{Kind: rt.Link, Text: "doc", URL: "https://example.invalid/doc"}))
	for _, inert := range []bool{false, true} {
		hs := newHarness(d, &recHandler{})
		hs.v.SetOptions(Options{NoLinking: inert})
		hs.frame()
		for _, label := range []string{"#plans", "@robin", "doc"} {
			f := findFrag(t, &hs.v.d, label)
			p := center(f)
			ptr = image.Pt(int(p.X+.5), int(p.Y+.5))
			hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
			hs.frame()
			c.Frame(t0, false, false)
			_, pending := c.Pending()
			hit := hs.v.targetAt(f32.Pt(p.X, p.Y)) >= 0
			switch {
			case inert && (pending || hit):
				t.Errorf("NoLinking: %s takes the pointer (card %v, target %v)", label, pending, hit)
			case !inert && label == "#plans" && !pending:
				t.Errorf("a linked %s reports no hover card", label)
			}
			hs.send(pointer.Event{Kind: pointer.Leave, Source: pointer.Mouse, Position: p})
			hs.frame()
			c.Frame(t0, false, false)
		}
	}
}

// Under NoLinking (pointer-events:none) the mentions and links don't set
// the hand either; without it they do.
func TestNoLinkingTakesNoHand(t *testing.T) {
	d := doc(para(txt("see "), rt.Span{Kind: rt.Channel, Text: "#plans", ID: "C0NOLINK01"}, txt(" at "),
		rt.Span{Kind: rt.Link, Text: "doc", URL: "https://example.invalid/doc"}))
	for _, inert := range []bool{false, true} {
		hs := newHarness(d, &recHandler{})
		hs.v.SetOptions(Options{NoLinking: inert})
		hs.frame()
		for _, label := range []string{"#plans", "doc"} {
			f := findFrag(t, &hs.v.d, label)
			if got := hs.v.handFrag(f, false); got == inert {
				t.Errorf("NoLinking %v: %s takes the hand %v", inert, label, got)
			}
		}
	}
}
