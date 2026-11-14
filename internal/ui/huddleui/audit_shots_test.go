package huddleui

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// The states docs/HUDDLES-AUDIT.md compares with Slack, against the fake
// Chime (nothing real is joined): the huddle window's title, toolbar,
// closed captions, ⋯ menu, informational banner and hidden self-view; the
// docked bar with the window closed and open; the sidebar row's huddle
// badge. The huddles-audit-shots workflow uploads them.
func TestHuddleShotsAudit(t *testing.T) {
	e := newShotEnv(t, false)
	s := e.join(nil, nil)
	// ⋯ › Show captions › Closed captions: startEventLog (the fake's
	// rooms.startTranscription), "Captions have been turned on.", then
	// the transcript
	e.c.ToggleClosedCaptions(s)
	st := waitState(t, s, "connected with a caption", func(st huddle.State) bool {
		return st.Phase == huddle.Connected && len(st.Captions) > 1 && st.Captions[0].Start && len(st.Participants) > 1
	})
	if title, sub := e.c.windowTitle(st); title != "Huddle in huddle-test" || sub != "2 people" {
		t.Fatalf("title %q / %q", title, sub)
	}

	withPictures(e) // the raise-hand half of the sticker pair is Slack's emoji image
	loadPictures(t, e, 620, 400, func(gtx C) { e.w.layout(gtx) })
	img := render(t, "audit-window", 620, 400, func(gtx C) { e.w.layout(gtx) })
	// the compact window: Leave ends the centred toolbar row, after ⋯
	right := image.Rect(e.w.moreBtn.Max.X, (400-winFooterH)*shotScale, e.w.moreBtn.Max.X+100*shotScale, (400-5)*shotScale)
	if n := pixeltest.Count(img, right, hudLeave, 12); n < 3000 {
		t.Fatalf("no Leave button after ⋯ (%d px)", n)
	}
	// the closed captions: a black box at the content's bottom left
	cc := image.Rect(8*shotScale, 200*shotScale, 310*shotScale, (400-56)*shotScale)
	if n := pixeltest.Count(img, cc, color.NRGBA{A: 0xff}, 8); n < 1500 {
		t.Fatalf("no caption box at the bottom left (%d px)", n)
	}

	// the ⋯ menu above its button
	e.w.toggleMore(s, s.State())
	if e.w.more == nil {
		t.Fatal("the ⋯ menu didn't open")
	}
	var labels []string
	for _, it := range e.c.moreMenu(s, s.State(), e.w) {
		if it.Label != "" {
			labels = append(labels, it.Label)
		}
	}
	want := []string{"Copy huddle link", "Go to #huddle-test", "Add topic", "Show captions", "Hide self-view", "Huddle preferences"}
	if len(labels) != len(want) {
		t.Fatalf("⋯ menu %q, want %q", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Fatalf("⋯ menu %q, want %q", labels, want)
		}
	}
	render(t, "audit-window-more", 620, 400, func(gtx C) { e.w.layout(gtx) })
	e.w.more.Dismiss()
	e.w.more = nil

	// "Someone muted you." and Hide self-view's banner
	e.c.mu.Lock()
	e.c.win = e.w
	e.c.mu.Unlock()
	e.c.showBanner("Someone muted you.", "mic-off")
	render(t, "audit-window-banner", 620, 400, func(gtx C) { e.w.layout(gtx) })
	e.c.hideSelf.Store(true)
	e.c.showBannerLink("Self-view is hidden.", "eye-closed", "Undo", func() { e.c.hideSelf.Store(false) })
	render(t, "audit-window-hide-self", 620, 400, func(gtx C) { e.w.layout(gtx) })
	e.c.hideSelf.Store(false)

	// the dock: collapsed while the window is open, the full bar without it
	var ops op.Ops
	if h := e.c.DockHeight(shotCtx(&ops, 1000, 200)) / shotScale; h != dockCollapsed+dockPad {
		t.Fatalf("collapsed dock %d dp", h)
	}
	render(t, "audit-dock-collapsed", 1000, dockCollapsed+dockPad, func(gtx C) {
		theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
		e.c.LayoutDock(gtx, e.h.th)
	})
	e.c.mu.Lock()
	e.c.win = nil
	e.c.mu.Unlock()
	dock := render(t, "audit-dock", 1000, dockBar+dockPad, func(gtx C) {
		theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
		e.c.LayoutDock(gtx, e.h.th)
	})
	if n := pixeltest.Count(dock, image.Rect(800*shotScale, 0, 1000*shotScale, dockBar*shotScale), hudLeave, 12); n < 3000 {
		t.Fatalf("no Leave in the dock (%d px)", n)
	}

	// the sidebar row's badge, for a huddle you're in and one you aren't
	for _, dark := range []bool{false, true} {
		th := theme.New(dark)
		k := sbBadge[mode(th)]
		suffix := "light"
		if dark {
			suffix = "dark"
		}
		e.c.mu.Lock()
		e.c.live["C0OTHER"] = liveRoom{ID: "R2", Channel: "C0OTHER", Participants: []string{huddle.FakeRemoteUser, "U0THIRD"}}
		e.c.mu.Unlock()
		row := func(ch string) func(gtx C) {
			return func(gtx C) {
				theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, th.C.ChromeA)
				theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, th.C.SidebarBg)
				e.c.SidebarSuffix(gtx, th, ch, gtx.Constraints.Max.X-gtx.Dp(6), gtx.Constraints.Max.Y, false)
			}
		}
		img := render(t, "audit-sidebar-other-"+suffix, 220, 28, row("C0OTHER"))
		if n := pixeltest.Count(img, img.Bounds(), k.bg, 6); n < 400 {
			t.Fatalf("%s: no badge (%d px)", suffix, n)
		}
		render(t, "audit-sidebar-in-"+suffix, 220, 28, row(hcChannel))
	}
}
