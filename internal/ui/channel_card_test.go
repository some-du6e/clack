package ui

import (
	"fmt"
	"image"
	"os"
	"slices"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// chcLink is the window box of the visible message link to channel ch.
func (e *perfEnv) chcLink(conv, ch string) image.Rectangle {
	c := e.a.convos[conv+"/"]
	if c == nil {
		return image.Rectangle{}
	}
	l, ok := c.list.(channelList)
	if !ok {
		return image.Rectangle{}
	}
	for _, r := range l.LinkRects() {
		if r.Kind == richview.HitChannel && r.ID == ch {
			return r.R
		}
	}
	return image.Rectangle{}
}

// chcOpen rests the pointer on the #channel link to ch until its card
// opens (350ms) and returns the trigger's box.
func (e *perfEnv) chcOpen(t *testing.T, conv, ch string) image.Rectangle {
	t.Helper()
	var r image.Rectangle
	dl := time.Now().Add(5 * time.Second)
	for r = e.chcLink(conv, ch); r.Empty() && time.Now().Before(dl); r = e.chcLink(conv, ch) {
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
	if r.Empty() {
		var seen []string
		if c := e.a.convos[conv+"/"]; c != nil {
			if l, ok := c.list.(channelList); ok {
				for _, lr := range l.LinkRects() {
					seen = append(seen, fmt.Sprintf("%s %d %s %v", lr.TS, lr.Kind, lr.ID, lr.R))
				}
			}
		} else {
			seen = append(seen, "no conversation view")
		}
		t.Fatalf("no link to %s in %s; links: %q; route %q", ch, conv, seen, e.a.router.Current().Channel())
	}
	mid := f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+r.Dy()/2))
	e.move(mid)
	ctl := &e.a.hover.ctl
	if p, ok := ctl.Pending(); !ok || p.ChannelID != ch || !p.Buttons || p.Texty {
		t.Fatalf("over the link to %s: pending %+v %v, want its channel card with buttons", ch, p, ok)
	}
	if _, _, open := ctl.Open(); open {
		t.Fatal("the card opened without HoverCardTrigger's 350ms")
	}
	time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
	e.frame()
	if req, _, open := ctl.Open(); !open || req.ChannelID != ch {
		t.Fatalf("after 400ms: open %v %+v", open, req)
	}
	return r
}

// The whole client on the fake Slack: a message's #channel opens Slack's
// ChannelHoverCard after 350ms, centred 12px above the link; the pointer
// may rest on the card; leaving closes it after 250ms; Escape closes it;
// Join Channel joins (conversations.join) and the card then offers View
// Channel, which goes to the channel and closes the card. Shots of the
// Slack Connect, not-member and archived cards, light and dark.
func TestChannelHoverCardInChannel(t *testing.T) {
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	conv := fs.ChannelIDs()[1]
	fs.PostQuiet(conv, fakeslack.ProfileID, "Notes in <#"+fakeslack.ThreadSharedID+"> and <#"+fakeslack.ThreadElsewhereID+
		">, the old ones in <#"+fakeslack.ThreadArchivedID+">")
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("HOVER_SHOTS_DIR") != "" {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	e.openChannel(conv)
	e.settle(800 * time.Millisecond)
	ctl := &e.a.hover.ctl
	away := f32.Pt(float32(e.size.X-40), float32(e.size.Y-40))
	leave := func() {
		e.move(away)
		time.Sleep(hovercard.HideDelay + 50*time.Millisecond)
		e.frame()
	}
	// waitCard waits for the open card to satisfy ok (a theme switch
	// rebuilds the message's link, whose card then reopens on its own
	// 350ms with the pointer still on it)
	waitCard := func(what string, ok func(c *popups.ChannelHoverCard) bool) {
		e.until(what, 5*time.Second, func() bool {
			c := e.a.hover.chc
			_, _, open := ctl.Open()
			return open && c != nil && ok(c)
		})
	}
	shotBoth := func(name string, ok func(c *popups.ChannelHoverCard) bool) {
		for _, dark := range []bool{false, true} {
			e.setDark(dark)
			time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
			e.frame()
			waitCard(name, ok)
			e.settle(150 * time.Millisecond)
			hcShot(t, e, name+"-"+map[bool]string{false: "light", true: "dark"}[dark])
		}
		e.setDark(false)
		time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
		e.frame()
		waitCard(name, ok)
	}

	// Slack Connect: every row, View Channel
	r := e.chcOpen(t, conv, fakeslack.ThreadSharedID)
	e.until("the shared card's rows", 5*time.Second, func() bool {
		c := e.a.hover.chc
		if c == nil || c.State() != popups.ChannelCardFull {
			return false
		}
		it := c.Items()
		return len(it) == 3 && it[0] != "" && it[1] != "" && it[2] == "Partner Co is in this channel"
	})
	card := e.a.hover.chc.Rect()
	if card.Dx() != 340 {
		t.Errorf("card width %d", card.Dx())
	}
	above := card.Max.Y == r.Min.Y-hovercard.OffsetY
	below := card.Min.Y == r.Max.Y+hovercard.OffsetY
	if !above && !below {
		t.Errorf("card %v isn't 12px above or below the link %v", card, r)
	}
	if c := card.Min.X + card.Dx()/2; c != r.Min.X+r.Dx()/2 && card.Min.X != hovercard.AllowanceX && card.Max.X != e.size.X-hovercard.AllowanceX {
		t.Errorf("card %v isn't centred on the link %v (position top)", card, r)
	}
	if b := e.a.hover.chc.Button(); b != "View Channel" {
		t.Errorf("button %q", b)
	}
	e.settle(300 * time.Millisecond) // the partner's icon
	shotBoth("channel-card-shared", func(c *popups.ChannelHoverCard) bool { return len(c.Items()) == 3 && c.Items()[1] != "" })
	card = e.a.hover.chc.Rect()
	// onto the card and resting there: it stays
	e.move(f32.Pt(float32(card.Min.X+30), float32(card.Min.Y+20)))
	time.Sleep(hovercard.HideDelay + 100*time.Millisecond)
	e.frame()
	if _, _, open := ctl.Open(); !open {
		t.Fatal("the card closed with the pointer on it")
	}
	// off both: 250ms later it's gone
	e.move(away)
	if _, _, open := ctl.Open(); !open {
		t.Fatal("closed without the 250ms grace")
	}
	time.Sleep(hovercard.HideDelay + 50*time.Millisecond)
	e.frame()
	if _, _, open := ctl.Open(); open {
		t.Fatal("still open after the pointer left")
	}
	// Escape closes it
	e.chcOpen(t, conv, fakeslack.ThreadSharedID)
	e.key(key.NameEscape, 0)
	e.frame()
	if _, _, open := ctl.Open(); open {
		t.Fatal("Escape left the card open")
	}
	leave()

	// archived: no rows, View Channel
	e.chcOpen(t, conv, fakeslack.ThreadArchivedID)
	e.until("the archived card", 5*time.Second, func() bool {
		c := e.a.hover.chc
		return c != nil && c.State() == popups.ChannelCardFull && len(c.Items()) == 0
	})
	shotBoth("channel-card-archived", func(c *popups.ChannelHoverCard) bool { return len(c.Items()) == 0 })
	leave()

	// a channel the viewer isn't in: Join Channel joins it
	e.chcOpen(t, conv, fakeslack.ThreadElsewhereID)
	e.until("the not-member card", 5*time.Second, func() bool {
		c := e.a.hover.chc
		return c != nil && c.Button() == "Join Channel" && len(c.Items()) == 2 && c.Items()[0] != "" && c.Items()[1] != ""
	})
	shotBoth("channel-card-join", func(c *popups.ChannelHoverCard) bool {
		return c.Button() == "Join Channel" && len(c.Items()) == 2 && c.Items()[1] != ""
	})
	card = e.a.hover.chc.Rect()
	btn := image.Pt(card.Min.X+card.Dx()/2, card.Max.Y-16-14) // the 28px button, 16 from the bottom
	e.move(f32.Pt(float32(btn.X), float32(btn.Y)))
	e.clickAt(btn, 10*time.Second)
	e.frame()
	e.until("conversations.join", 5*time.Second, func() bool {
		return slices.ContainsFunc(fs.JoinCalls(), func(j fakeslack.JoinCall) bool { return j.Channel == fakeslack.ThreadElsewhereID })
	})
	e.until("View Channel after the join", 5*time.Second, func() bool {
		c := e.a.hover.chc
		return c != nil && c.Button() == "View Channel"
	})
	if _, _, open := ctl.Open(); !open {
		t.Fatal("joining closed the card (Slack's joinChannel leaves it open)")
	}
	if got := e.a.router.Current().Channel(); got != conv {
		t.Errorf("the join navigated to %q", got)
	}
	// View Channel goes there and closes the card
	e.clickAt(btn, 12*time.Second)
	e.frame()
	e.until("the joined channel", 5*time.Second, func() bool { return e.a.router.Current().Channel() == fakeslack.ThreadElsewhereID })
	if _, _, open := ctl.Open(); open {
		t.Error("View Channel left the card open")
	}
}
