package ui

// A reaction's tooltip sits over its pill in a thread opened in the
// Activity tab's pane (Karim, 11-24: "i finally took a picuter of this
// bug": the tip ~620 dp above the pill, near the top of the pane). The
// pane's list took its window origin from an origin tracker that pointer
// events never reached (the list's own handlers above it take them), so
// it kept its fallback, the pointer itself: the window bounds the tip is
// kept inside were the window less the pointer, and the tip was pushed up
// into them. The trackers now read where the router put their area.
// Synthetic content only.

import (
	"fmt"
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestReactionTipOverItsPillInTheActivityPanesThread(t *testing.T) {
	e := newActShell(t)
	crPersistent(e)
	ch, ts := fakeslack.ViewsChannel, fakeslack.ViewsThreadUnread
	e.fs.PostAtQuiet(ch, ts, "U0FAKE00001", "a synthetic question for the team")
	// enough replies that the thread scrolls; the reacted one near its end
	var reacted string
	for i := 0; i < 16; i++ {
		rts := fmt.Sprintf("%d.000100", 1762600100+i*60)
		e.fs.ReplyAtQuiet(ch, ts, rts, []string{"U0FAKE00002", fakeslack.SelfID}[i%2], fmt.Sprintf("synthetic reply number %d", i))
		if i == 13 {
			reacted = rts
		}
	}
	if !e.fs.ReactReplyQuiet(ch, ts, reacted, "thumbsup", "U0FAKE00001", "U0FAKE00002") {
		t.Fatal("no reply to react to")
	}
	cmOpenActivity(e)
	for i := 0; i < 40 && e.activity().OpenedItem() != "fa-thread"; i++ {
		e.activity().StepItem(1, false)
		e.frames(5)
	}
	if k := e.activity().OpenedItem(); k != "fa-thread" {
		t.Fatalf("stepping through the inbox never opened the thread item (at %q)", k)
	}
	if e.a.secondary.open() {
		t.Fatalf("the item opened as a secondary view (%v), not in the Activity tab's own pane", e.a.secondary.kind)
	}

	// the pill as the pane's list knows it, and where it really is: the
	// list's rects are its believed origin plus local; the convo's area is
	// where the router put the pane
	var c *convo
	var tl threadList
	var pill image.Rectangle
	e.until("the pane's thread drawn with the reacted reply's pill", func() bool {
		if c = e.a.convos[ch+"/"+ts]; c == nil {
			return false
		}
		var ok bool
		if tl, ok = c.list.(threadList); !ok {
			return false
		}
		for _, r := range tl.ReactionRects() {
			if r.TS == reacted {
				pill = r.R
				return true
			}
		}
		tl.ScrollToBottom()
		return false
	})
	paneAt := func() image.Point {
		full, _, ok := e.rt.Source().AreaBounds(&c.org.tag)
		if !ok {
			t.Fatal("the pane's origin tracker has no area")
		}
		return full.Min
	}
	truePill := pill.Sub(tl.Origin).Add(paneAt())
	if !truePill.In(image.Rectangle{Max: e.sz}) {
		t.Fatalf("the pill %v is off the window %v", truePill, e.sz)
	}

	// the tooltip's box in window coordinates: an area registered where
	// it's drawn, read back from the router
	var tipTag int
	drawn := 0
	prev := theme.TooltipDrawn
	theme.TooltipDrawn = func(gtx layout.Context, r image.Rectangle) {
		if prev != nil {
			prev(gtx, r)
		}
		drawn++
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &tipTag)
	}
	t.Cleanup(func() { theme.TooltipDrawn = prev })

	// the pointer comes in from the Activity list, onto the pill
	move := func(p image.Point) {
		e.rt.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
	}
	move(image.Pt(e.sz.X/6, e.sz.Y/2))
	e.frames(3)
	cp := image.Pt((truePill.Min.X+truePill.Max.X)/2, (truePill.Min.Y+truePill.Max.Y)/2)
	move(cp)
	e.frames(2)
	deadline := time.Now().Add(400 * time.Millisecond) // past the pill's 300 ms
	for time.Now().Before(deadline) {
		e.frames(1)
	}
	drawn = 0
	e.until("the reaction tooltip", func() bool { return drawn > 0 })
	e.frames(1) // the router has the area of the frame that drew it
	tip, _, ok := e.rt.Source().AreaBounds(&tipTag)
	if !ok || tip.Empty() {
		t.Fatal("the tooltip's box isn't known")
	}
	// Slack's ReactionTip in a top BaseTooltip: the box 8px over the pill
	// (its margin), the arrow on the pill's centre
	if tip.Max.Y > truePill.Min.Y || tip.Max.Y < truePill.Min.Y-16 {
		t.Errorf("the tooltip %v isn't just above its pill %v (its bottom %d px over the pill's top; Slack's is 8): the pane's list origin %v, its area at %v",
			tip, truePill, truePill.Min.Y-tip.Max.Y, tl.Origin, paneAt())
	}
	if tip.Min.X > cp.X || tip.Max.X < cp.X {
		t.Errorf("the tooltip %v doesn't span its pill's centre x %d", tip, cp.X)
	}

	// it closes when the pointer leaves the pill: onto the reply's plain
	// text above it, where nothing has a tooltip
	move(image.Pt(truePill.Min.X+10, truePill.Min.Y-30))
	e.frames(3)
	drawn = 0
	e.frames(3)
	if drawn > 0 {
		t.Errorf("a tooltip still drew %d times after the pointer left the pill", drawn)
	}
}
