package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/views"
)

// Karim, α.112 (2026-11-20): on the Activity page with a profile pane
// open, a #welcome-to-hack-club hover card sat at the window's top-left
// over the tab rail and the filter chips, with nothing under it. An
// Activity row's message body (a cached richview.View) that was hovered
// and then not drawn for a frame (another Activity tab) never got the
// Leave Gio owed it, and drawn again it reported a channel card anchored
// at its old local pointer moved by everything the pointer did since.
//
// Slack's channel card (BaseMrkdwnChannel's HoverCardTrigger, boot-shared
// pws8, in the Popover client-boot TxLv) is measured once when it opens
// and never follows its trigger; it closes 250ms after the pointer is on
// neither the trigger, as Chromium's hover update after a scroll or a
// layout sees it, nor the card, and at once when the trigger unmounts.
// These run each change with the pointer resting on an Activity row's
// #channel: the card's state after the change must be the state a fresh
// move of the pointer gives (what the hover really is), an open card
// must sit where it opened, and leaving for another tab and coming back
// opens nothing.

const (
	// the #channel first: compact rows (the default density) clamp the
	// body to one line
	hcaText  = "<#" + fakeslack.ViewsChannel + "> has the synthetic notes for the week from every team, gathered so nobody misses a thing, and this message runs on long enough to wrap at any width the inbox takes."
	hcaText2 = "A second synthetic mention, newer than the first."
	hcaUser  = "U0FAKE00001"
)

// hcaMark is the words a body showing text draws (its #channel is drawn
// by name, not as written).
func hcaMark(text string) string {
	if i := strings.Index(text, "> "); i >= 0 {
		text = text[i+2:]
	}
	return text[:24]
}

// hcaEnv is the client on the Activity page with the mention row on top.
type hcaEnv struct {
	*perfEnv
	t     *testing.T
	ctl   *hovercard.Controller
	views map[*richview.View]string // the painted bodies, by their text
	rest  f32.Point                 // the rail: no hover cards there
	// the card open() opened: its trigger and generation (check)
	openKey any
	openGen int
}

func newHCAEnv(t *testing.T) *hcaEnv {
	t.Setenv("CLACK_REDUCE_MOTION", "1") // wheel scrolls land at once
	sz := fakeslack.Small
	sz.Views = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ts, _ := fs.PostQuiet(fakeslack.ViewsChannel, hcaUser, hcaText)
	fs.AddActivityMention("fa-hca", fakeslack.ViewsChannel, ts, hcaUser)
	e := &hcaEnv{perfEnv: startEnv(t, fs, t.TempDir()), t: t, views: map[*richview.View]string{}}
	richview.PaintedHook = func(v *richview.View) {
		if _, ok := e.views[v]; !ok {
			e.views[v] = v.TextRange(0, v.PlainLen())
		}
	}
	t.Cleanup(func() { richview.PaintedHook = nil })
	e.waitReady()
	e.ctl = &e.a.hover.ctl
	e.rest = f32.Pt(30, float32(e.size.Y)/2)
	e.a.Navigate("activity")
	e.until("the mention row", 20*time.Second, func() bool { return e.body(hcaText) != nil })
	e.settle(500 * time.Millisecond)
	return e
}

func (e *hcaEnv) activity() *views.Activity {
	v, _ := e.a.views[Route("activity")].(*views.Activity)
	return v
}

// body is a painted view showing text (nil: none yet).
func (e *hcaEnv) body(text string) *richview.View {
	for v, s := range e.views {
		if strings.Contains(s, hcaMark(text)) {
			return v
		}
	}
	return nil
}

// locate sweeps the pointer over the page until it is on a body showing
// text, and returns the body and its #channel's window box (its first
// line), learned from that fresh pointer event.
func (e *hcaEnv) locate(text string) (*richview.View, image.Rectangle) {
	e.t.Helper()
	for y := 60; y < e.size.Y-10; y += 11 {
		for _, x := range []int{220, 420, 620} {
			e.move(f32.Pt(float32(x), float32(y)))
			for v, s := range e.views {
				if !strings.Contains(s, hcaMark(text)) || !v.Pointer() {
					continue
				}
				org, ok := v.WindowOrigin()
				if !ok {
					continue
				}
				var r image.Rectangle
				for _, l := range v.LinkRects() {
					if l.Kind == richview.HitChannel && l.ID == fakeslack.ViewsChannel && (r.Empty() || l.R.Min.Y == r.Min.Y) {
						r = r.Union(l.R)
					}
				}
				if !r.Empty() {
					return v, r.Add(org)
				}
			}
		}
	}
	e.t.Fatalf("no body showing %q under the pointer anywhere", hcaMark(text))
	return nil, image.Rectangle{}
}

// away rests the pointer on the rail until any card has closed.
func (e *hcaEnv) away() {
	e.move(e.rest)
	e.settle(hovercard.HideDelay + 100*time.Millisecond)
	if _, _, open := e.ctl.Open(); open {
		e.t.Fatal("a card stayed open with the pointer on the rail")
	}
}

// open rests the pointer on the row's #channel until its card opens and
// returns the pointer, the link and the card.
func (e *hcaEnv) open(what string) (f32.Point, image.Rectangle, image.Rectangle) {
	e.t.Helper()
	e.away()
	v, link := e.locate(hcaText)
	p := f32.Pt(float32(link.Min.X+link.Dx()/2), float32(link.Min.Y+link.Dy()/2))
	e.move(p)
	if !e.untilSoft(5*time.Second, func() bool {
		req, _, open := e.ctl.Open()
		return open && req.ChannelID == fakeslack.ViewsChannel && e.a.hover.chc != nil && !e.a.hover.chc.Rect().Empty()
	}) {
		req, _, open := e.ctl.Open()
		pr, pend := e.ctl.Pending()
		org, _ := v.WindowOrigin()
		var ov []string
		for _, o := range e.a.overlays {
			if o != nil && !o.Dismissed() {
				ov = append(ov, fmt.Sprintf("%T", o))
			}
		}
		e.t.Fatalf("%s: no card over the link %v (pointer %v; body hovered %v, origin %v, links %+v); open %v %+v; pending %v %+v; blocked %v; overlays %v",
			what, link, p, v.Pointer(), org, v.LinkRects(), open, req, pend, pr, e.a.hoverBlocked(), ov)
	}
	card := e.a.hover.chc.Rect()
	req, gen, _ := e.ctl.Open()
	e.openKey, e.openGen = req.Key, gen
	// the trigger's bounds are its words' and its pill's (2px padding)
	if a := req.Anchor; !link.In(a) || a.Dx()-link.Dx() > 8 || a.Dy()-link.Dy() > 8 {
		e.t.Errorf("%s: the card opened anchored at %v, the link is at %v", what, req.Anchor, link)
	}
	if a := req.Anchor; card.Max.Y != a.Min.Y-hovercard.OffsetY && card.Min.Y != a.Max.Y+hovercard.OffsetY {
		e.t.Errorf("%s: card %v isn't 12px above or below the trigger %v", what, card, a)
	}
	return p, link, card
}

// hcaState is the hover card's state: which trigger's card is open or
// pending, and where the open one is.
type hcaState struct {
	open, pend bool
	key        any
	gen        int // the open card's (a card closed and reopened has another)
	card       image.Rectangle
}

func (e *hcaEnv) state() hcaState {
	var s hcaState
	if req, gen, open := e.ctl.Open(); open {
		s.open, s.key, s.gen = true, req.Key, gen
		if e.a.hover.chc != nil {
			s.card = e.a.hover.chc.Rect()
		}
	} else if p, ok := e.ctl.Pending(); ok {
		s.pend, s.key = true, p.Key
	}
	return s
}

// check lets the change settle with the pointer resting at p, then
// compares the card with what a fresh pointer move there gives: the
// hover as Chromium would see it after the layout. An open card must not
// have moved from where it opened (Slack's never follows its trigger).
func (e *hcaEnv) check(what string, p f32.Point, card image.Rectangle) {
	e.t.Helper()
	e.settle(hovercard.ShowDelay + hovercard.HideDelay + 150*time.Millisecond)
	got := e.state()
	e.move(p.Add(f32.Pt(1, 0)))
	e.move(p)
	e.settle(hovercard.ShowDelay + hovercard.HideDelay + 150*time.Millisecond)
	want := e.state()
	if got.open != want.open || got.key != want.key {
		e.t.Errorf("%s: after the change the card was open %v (key %v, card %v); the pointer's real hover gives open %v (key %v, card %v)",
			what, got.open, keyViewHovered(got.key), got.card, want.open, keyViewHovered(want.key), want.card)
	}
	if got.open && got.card != card && got.key != nil && want.key == got.key {
		e.t.Errorf("%s: the open card moved from %v to %v (Slack's stays where it opened)", what, card, got.card)
	}
	// a card still open over the same trigger is the one that opened: not
	// closed by a missed frame and opened again by the resting pointer
	if got.open && got.key == e.openKey && got.gen != e.openGen {
		e.t.Errorf("%s: the card over the same trigger closed and opened again (generation %d, opened as %d)", what, got.gen, e.openGen)
	}
	if got.open && want.open && want.key == got.key && want.gen != got.gen {
		e.t.Errorf("%s: a fresh move closed and reopened the card (generation %d, then %d)", what, got.gen, want.gen)
	}
}

// Scrolling under a resting pointer: a little (the #channel stays under
// it: the card stays where it opened) and far (the row leaves the list:
// its card goes at once, with the row).
func TestHoverCardAnchorActivityScroll(t *testing.T) {
	e := newHCAEnv(t)
	p, link, card := e.open("scroll a little")
	e.scroll(image.Pt(int(p.X), int(p.Y)), float32(link.Dy()/3))
	e.frame()
	e.check("scroll a little", p, card)

	p, _, card = e.open("scroll away")
	e.scroll(image.Pt(int(p.X), int(p.Y)), 2000)
	e.frame()
	e.frame()
	if v, _ := e.locateQuiet(hcaText); v == nil {
		if _, _, open := e.ctl.Open(); open {
			t.Error("scroll away: the row left the list and its card stayed open (Slack's goes with the row)")
		}
	}
	e.check("scroll away", p, card)
	e.scroll(image.Pt(int(e.rest.X)+300, int(e.rest.Y)), -4000)
	e.frame()
}

// locateQuiet reports whether a body showing text was drawn in the last
// frame, without moving the pointer.
func (e *hcaEnv) locateQuiet(text string) (*richview.View, bool) {
	drawn := map[*richview.View]bool{}
	prev := richview.PaintedHook
	richview.PaintedHook = func(v *richview.View) { drawn[v] = true; prev(v) }
	e.frame()
	richview.PaintedHook = prev
	for v := range drawn {
		if strings.Contains(e.views[v], hcaMark(text)) {
			return v, true
		}
	}
	return nil, false
}

// Opening the profile pane narrows the inbox: the body wraps again under
// the resting pointer.
func TestHoverCardAnchorActivityProfilePane(t *testing.T) {
	e := newHCAEnv(t)
	p, _, card := e.open("profile pane")
	e.a.OpenProfile(image.Point{}, "U0FAKE00002")
	e.frame()
	e.check("profile pane", p, card)
	if !e.a.secondary.open() {
		t.Fatal("no profile pane")
	}
	// and with the pane open, a card from the narrowed row
	p, _, card = e.open("with the pane")
	e.a.CloseSecondary()
	e.frame()
	e.check("closing the pane", p, card)
}

// Karim's: another Activity tab and back. The row unmounts with its tab
// (its card goes at once), and back on All with the pointer on the tab
// strip no card opens.
func TestHoverCardAnchorActivityTabs(t *testing.T) {
	e := newHCAEnv(t)
	e.a.OpenProfile(image.Point{}, "U0FAKE00002") // Karim's capture: page activity, pane profile
	e.settle(300 * time.Millisecond)
	e.open("tabs")
	// another tab (a shortcut: the pointer still on the mention), the row
	// not drawn; the pointer goes off to the side; back to All
	e.activity().ShowTab(1)
	e.frame()
	if _, _, open := e.ctl.Open(); open {
		t.Error("another tab: the row's card stayed open (Slack's goes with the row)")
	}
	e.move(e.rest)
	e.settle(hovercard.HideDelay + 50*time.Millisecond)
	e.activity().ShowTab(0)
	e.settle(hovercard.ShowDelay + hovercard.HideDelay + 200*time.Millisecond)
	if req, _, open := e.ctl.Open(); open {
		t.Fatalf("back on All with the pointer at %v: a card opened at %v (anchor %v, nothing under it)", e.rest, e.a.hover.chc.Rect(), req.Anchor)
	}
	if pr, ok := e.ctl.Pending(); ok {
		t.Fatalf("back on All with the pointer at %v: a card is pending, anchored at %v", e.rest, pr.Anchor)
	}
}

// New Activity items move the row under the resting pointer, and the
// window resizes under it.
func TestHoverCardAnchorActivityLayout(t *testing.T) {
	e := newHCAEnv(t)
	p, _, card := e.open("new items")
	ts, _ := e.fs.PostQuiet(fakeslack.ViewsChannel, hcaUser, hcaText2)
	e.fs.AddActivityMention("fa-hca2", fakeslack.ViewsChannel, ts, hcaUser)
	raw, _ := json.Marshal(map[string]any{"type": "activity", "subtype": "activity_invalidated"})
	e.a.data.core.Inject(slack.Event{Type: "activity", Subtype: "activity_invalidated", Raw: raw})
	e.until("the new row", 15*time.Second, func() bool { return e.body(hcaText2) != nil })
	e.check("new items", p, card)

	for _, w := range []int{1100, 1440} {
		p, _, card = e.open("resize")
		e.size = image.Pt(w, e.size.Y)
		e.frame()
		e.check("resize", p, card)
	}
}
