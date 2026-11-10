package ui

// Link preview hover cards, context menus, the rail remove button and
// "Added by <app>" against internal/fakeslack's #embeds-gallery (synthetic
// only; nothing talks to Slack; docs/EMBEDS.md has Slack's rules). With
// EMBEDS_SHOTS_DIR set (the embeds-shots workflow) it saves each state,
// light and dark.

import (
	"image"
	"os"
	"slices"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
)

func (e *perfEnv) embedsTS(name string) string {
	for _, ec := range e.fs.EmbedsCases() {
		if ec.Name == name {
			return ec.TS
		}
	}
	e.tb.Fatalf("no embeds case %q", name)
	return ""
}

func (e *perfEnv) embedsList() channelList {
	e.tb.Helper()
	c := e.a.convos[fakeslack.EmbedsChannelID+"/"]
	if c == nil {
		e.tb.Fatal("#embeds-gallery isn't open")
	}
	l, ok := c.list.(channelList)
	if !ok {
		e.tb.Fatalf("list is %T", c.list)
	}
	return l
}

// embedsCard is the on-screen v3 card of message name.
func (e *perfEnv) embedsCard(name string) messages.CardRect {
	e.tb.Helper()
	ts := e.embedsTS(name)
	for _, c := range e.embedsList().CardRects() {
		if c.TS == ts {
			return c
		}
	}
	e.tb.Fatalf("%s has no v3 card on screen (%+v)", name, e.embedsList().CardRects())
	return messages.CardRect{}
}

// embedsRail is the on-screen rail remove button of message name.
func (e *perfEnv) embedsRail(name string) image.Rectangle {
	e.tb.Helper()
	ts := e.embedsTS(name)
	for _, r := range e.embedsList().RemoveRects() {
		if r.TS == ts && r.Rail {
			return r.R
		}
	}
	e.tb.Fatalf("%s has no rail remove button on screen (%+v)", name, e.embedsList().RemoveRects())
	return image.Rectangle{}
}

func (e *perfEnv) rightClickAt(p image.Point, at time.Duration) {
	pt := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt, Time: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonSecondary, Position: pt, Time: at + time.Millisecond},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Time: at + 2*time.Millisecond},
	)
	for i := 0; i < 3; i++ {
		e.frame()
	}
}

func (e *perfEnv) topMenu() *popups.Menu {
	if n := len(e.a.overlays); n > 0 {
		if m, ok := e.a.overlays[n-1].(*popups.Menu); ok {
			return m
		}
	}
	return nil
}

func attMenuLabels(m *popups.Menu) []string {
	var out []string
	for _, it := range m.Items() {
		if it.Separator {
			out = append(out, "-")
			continue
		}
		out = append(out, it.Label)
	}
	return out
}

func TestEmbedsPreviewHover(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) { embedsPreviewHover(t, dark, mode) })
		if os.Getenv("EMBEDS_SHOTS_DIR") == "" {
			break // the dark run is only for the screenshots
		}
	}
}

func embedsPreviewHover(t *testing.T, dark bool, mode string) {
	e := embedsEnv(t, image.Pt(1440, 1500), dark)
	defer e.shutdown()
	ctl := &e.a.hover.ctl
	var clock time.Duration

	// bots.info names the apps: "Added by GitHub", "Added by Example Tracker"
	e.until("the fixture bots' names", 20*time.Second, func() bool {
		gh, kit := docAtts(e.embedsMsg("github")), docAtts(e.embedsMsg("block kit"))
		return len(gh) == 1 && gh[0].BotName == "GitHub" && len(kit) == 1 && kit[0].BotName == "Example Tracker"
	})
	e.settle(300 * time.Millisecond)

	// the hover card: 350ms on the row card, then 340 wide, centered over
	// the hover wrapper, 12px above it (or below)
	card := e.embedsCard("large image")
	if !card.Popover {
		t.Fatalf("the large image card has no hover card")
	}
	p := image.Pt(card.R.Min.X+80, card.R.Min.Y+30)
	e.move(f32.Pt(float32(p.X), float32(p.Y)))
	req, ok := ctl.Pending()
	if !ok || req.Preview == nil {
		t.Fatalf("resting on the card: pending=%v %+v", ok, req)
	}
	if _, _, open := ctl.Open(); open {
		t.Fatal("the hover card opened without the delay")
	}
	time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
	e.frame()
	e.settle(400 * time.Millisecond) // the fade, the image
	pc := e.a.hover.prev
	if pc == nil {
		t.Fatal("no preview card drawn after 400ms")
	}
	r := pc.Rect()
	wrap := req.Anchor
	if r.Dx() != 340 {
		t.Errorf("hover card width %d, want 340", r.Dx())
	}
	if want := wrap.Min.X + (wrap.Dx()-340)/2; r.Min.X != want {
		t.Errorf("hover card x %d, want %d (centered over %v)", r.Min.X, want, wrap)
	}
	if r.Max.Y != wrap.Min.Y-hovercard.OffsetY && r.Min.Y != wrap.Max.Y+hovercard.OffsetY {
		t.Errorf("hover card %v isn't 12px above or below the wrapper %v", r, wrap)
	}
	e.embedsShot("embeds-hovercard-" + mode)
	// onto the card: it stays; away: it closes after 250ms
	e.move(f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+20)))
	time.Sleep(hovercard.HideDelay + 100*time.Millisecond)
	e.frame()
	if e.a.hover.prev == nil {
		t.Error("the hover card closed with the pointer on it")
	}
	e.move(f32.Pt(float32(e.size.X-30), 140))
	e.frame() // the card area sees the Leave after this frame's timers ran
	time.Sleep(hovercard.HideDelay + 100*time.Millisecond)
	e.frame()
	if e.a.hover.prev != nil {
		t.Error("the hover card stayed open after the pointer left")
	}

	// right-click on the viewer's own row card: Remove preview, Copy link
	own := e.embedsCard("small thumb")
	clock += time.Second
	e.rightClickAt(image.Pt(own.R.Min.X+100, own.R.Min.Y+40), clock)
	m := e.topMenu()
	if m == nil {
		t.Fatal("right-clicking the viewer's preview opened no menu")
	}
	if got := attMenuLabels(m); !slices.Equal(got, []string{"Remove preview", "-", "Copy link"}) {
		t.Errorf("own preview menu %q", got)
	}
	if e.a.hover.prev != nil {
		t.Error("the hover card is open under the context menu")
	}
	e.embedsShot("embeds-context-menu-" + mode)
	e.a.dismissTop()
	e.frame()
	// someone else's: Copy link only
	clock += time.Second
	e.rightClickAt(image.Pt(card.R.Min.X+100, card.R.Min.Y+40), clock)
	if m := e.topMenu(); m == nil || !slices.Equal(attMenuLabels(m), []string{"Copy link"}) {
		t.Errorf("another person's preview menu: %v", m)
	} else {
		e.embedsShot("embeds-context-menu-other-" + mode)
	}
	e.a.dismissTop()
	e.frame()

	// the v1 card's rail: shown while the card is hovered; a click asks
	// "Remove preview?"
	rail := e.embedsRail("github")
	if rail.Dx() != 20 || rail.Dy() != 20 {
		t.Errorf("rail button %v, want 20x20", rail)
	}
	e.move(f32.Pt(float32(rail.Max.X+120), float32(rail.Min.Y+10)))
	e.embedsShot("embeds-rail-" + mode)
	e.move(f32.Pt(float32(rail.Min.X+10), float32(rail.Min.Y+10)))
	e.embedsShot("embeds-rail-hover-" + mode)
	clock += time.Second
	e.clickAt(image.Pt(rail.Min.X+10, rail.Min.Y+10), clock)
	for i := 0; i < 3; i++ {
		e.frame()
	}
	if len(e.a.overlays) == 0 {
		t.Error("clicking the rail button opened nothing")
	}
	e.a.dismissTop()
	e.frame()
	// the Block Kit card's rail shows only under the pointer
	kit := e.embedsRail("block kit")
	e.move(f32.Pt(float32(kit.Min.X+10), float32(kit.Min.Y+10)))
	e.embedsShot("embeds-kit-rail-hover-" + mode)
	if len(e.fs.DeletedAttachments()) != 0 {
		t.Errorf("a preview was removed: %+v", e.fs.DeletedAttachments())
	}
}
