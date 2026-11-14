package ui

import (
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// The whole client on the fake Slack: with truncated_url_hover_card on
// (the account's group), resting on a message's link opens Slack's
// UrlHoverCard after 350ms, 12px above (or below) the link and centred on
// it (HoverCardTrigger position "top"), 280–400px wide, with the URL
// lowercased; Copy link's tooltip turns "Copied!"; a message permalink
// opens none (its hover is the message preview). Off, no link opens a
// card, as before. Light and dark shots (url-card-*.png).
func TestURLHoverCard(t *testing.T) {
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	conv := fakeslack.ThreadPublicID
	const link = "https://Example.com/Docs/Release-Notes?Version=4.53&Section=Huddles"
	const perma = "https://hackclub.slack.com/archives/C0FAKE00001/p1700000000000100"
	fs.PostQuiet(conv, fakeslack.ProfileID, "The notes: <"+link+"|release notes>, and the thread: <"+perma+"|that one>")
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("HOVER_SHOTS_DIR") != "" {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	if g := e.a.Core().Experiment("truncated_url_hover_card"); g != "on" {
		t.Fatalf("truncated_url_hover_card is %q in fakeslack, want the account's on", g)
	}
	e.openChannel(conv)
	e.settle(800 * time.Millisecond)
	ctl := &e.a.hover.ctl
	away := f32.Pt(float32(e.a.primaryOrigin.X)-60, float32(e.size.Y)/2)
	leave := func() {
		e.move(away)
		time.Sleep(hovercard.HideDelay + 50*time.Millisecond)
		e.frame()
	}
	rectOf := func(u string) image.Rectangle {
		var r image.Rectangle
		e.until("the link "+u, 5*time.Second, func() bool {
			r = e.chcRectOf(conv, func(l richview.LinkRect) bool { return l.Kind == richview.HitLink && l.URL == u })
			return !r.Empty()
		})
		return r
	}
	hover := func(r image.Rectangle) {
		e.move(f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+r.Dy()/2)))
		time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
		e.frame()
		e.frame()
	}

	r := rectOf(link)
	e.move(f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+r.Dy()/2)))
	if p, ok := ctl.Pending(); !ok || p.URL != link {
		t.Fatalf("over the link: pending %+v %v, want its UrlHoverCard", p, ok)
	}
	time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
	e.frame()
	e.until("the URL card", 5*time.Second, func() bool {
		req, _, open := ctl.Open()
		return open && req.URL == link && e.a.hover.urc != nil
	})
	e.frame()
	card := e.a.hover.urc.Rect()
	if card.Dx() < 280 || card.Dx() > 400 {
		t.Errorf("card width %d, want fit-content within 280–400", card.Dx())
	}
	above := card.Max.Y == r.Min.Y-hovercard.OffsetY
	below := card.Min.Y == r.Max.Y+hovercard.OffsetY
	if !above && !below {
		t.Errorf("card %v isn't 12px above or below the link %v", card, r)
	}
	if c := card.Min.X + card.Dx()/2; c != r.Min.X+r.Dx()/2 && card.Min.X != hovercard.AllowanceX && card.Max.X != e.size.X-hovercard.AllowanceX {
		t.Errorf("card %v isn't centred on the link %v (position top)", card, r)
	}
	if got := popups.URLCardText(link); got != strings.ToLower(link) {
		t.Errorf("shown URL %q, want it lowercased", got)
	}
	if long := popups.URLCardText(strings.Repeat("a", 2100)); len([]rune(long)) != 2001 || !strings.HasSuffix(long, "…") {
		t.Errorf("a 2100-character URL shows %d characters", len([]rune(long)))
	}
	for _, dark := range []bool{false, true} {
		e.setDark(dark)
		r = rectOf(link)
		hover(r)
		e.until("the URL card after the theme switch", 5*time.Second, func() bool {
			_, _, open := ctl.Open()
			return open && e.a.hover.urc != nil
		})
		e.settle(150 * time.Millisecond)
		hcShot(t, e, "url-card-"+map[bool]string{false: "light", true: "dark"}[dark])
	}
	e.setDark(false)
	r = rectOf(link)
	hover(r)
	e.until("the URL card", 5*time.Second, func() bool { _, _, open := ctl.Open(); return open && e.a.hover.urc != nil })
	// Copy link: the tooltip says Copied!
	_, cp := e.a.hover.urc.Buttons()
	mid := image.Pt(cp.Min.X+cp.Dx()/2, cp.Min.Y+cp.Dy()/2)
	e.move(f32.Pt(float32(mid.X), float32(mid.Y)))
	e.clickAt(mid, 10*time.Second)
	e.frame()
	e.frame()
	if _, _, open := ctl.Open(); !open || e.a.hover.urc == nil {
		t.Fatal("clicking Copy link closed the card")
	}
	if tip := e.a.hover.urc.CopyTip(time.Now()); tip != "Copied!" {
		t.Errorf("Copy link's tooltip after the click %q, want Copied!", tip)
	}
	e.settle(200 * time.Millisecond)
	hcShot(t, e, "url-card-copied-light")
	leave()
	if _, _, open := ctl.Open(); open {
		t.Fatal("the card stayed open after the pointer left")
	}

	// a message permalink: no URL card (Slack's hover is the message
	// preview there)
	if pr := e.chcRectOf(conv, func(l richview.LinkRect) bool { return l.Kind == richview.HitLink && l.URL == perma }); !pr.Empty() {
		hover(pr)
		if req, _, open := ctl.Open(); open && req.URL != "" {
			t.Errorf("a message permalink opened a URL card: %+v", req)
		}
		leave()
	} else {
		t.Logf("the permalink draws no plain link (links: %+v)", e.chcRectOf(conv, func(l richview.LinkRect) bool { return l.Kind == richview.HitLink }))
	}
	if urlHoverCardLink(perma) || !urlHoverCardLink(link) || urlHoverCardLink("javascript:void(0)") || urlHoverCardLink("slack-action://BAPP/x") {
		t.Error("urlHoverCardLink: a permalink, javascript: or slack-action:// link gets the card, or an external one doesn't")
	}

	// off: no card on the link
	e.a.Core().SetExperimentGroup("truncated_url_hover_card", "off")
	e.settle(300 * time.Millisecond)
	hover(rectOf(link))
	if p, ok := ctl.Pending(); ok && p.URL != "" {
		t.Errorf("off: pending URL card %+v", p)
	}
	if req, _, open := ctl.Open(); open && req.URL != "" {
		t.Errorf("off: the link opened a URL card %+v", req)
	}
	leave()
	e.a.Core().ClearExperimentGroup("truncated_url_hover_card")
}
