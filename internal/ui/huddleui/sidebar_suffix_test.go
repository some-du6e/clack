package huddleui

import (
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Slack's sidebar huddle facepile (BaseChannelSuffixHuddleIcon) shows the
// first two people whether or not you're in the huddle; it hides only
// while your huddle there is starting or ending (HuddleAvatarStackContent:
// activeHuddleChannelId === channelId && isHuddleTransitionState). Clack
// used to drop the faces for any huddle you were in (Karim, 2026-11-10:
// "# fallen" with only the headphones and "4").
func TestSidebarSuffixFaces(t *testing.T) {
	e := newShotEnv(t, false)
	th := theme.New(false)
	var ops op.Ops
	width := func(ch string) int {
		ops.Reset()
		gtx := shotCtx(&ops, 220, 28)
		r := gtx.Constraints.Max.X - gtx.Dp(6)
		return r - e.c.SidebarSuffix(gtx, th, ch, r, gtx.Constraints.Max.Y, false)
	}
	gtx := shotCtx(&ops, 220, 28)
	fs := gtx.Dp(hdrFace)
	faces := sbFaceSteps(2, fs)[1] + fs + gtx.Dp(8) // two faces and margin-left 8px

	e.c.mu.Lock()
	e.c.live["C0OTHER"] = liveRoom{ID: "R2", Channel: "C0OTHER", Participants: []string{huddle.FakeRemoteUser, "U0THIRD"}}
	e.c.mu.Unlock()
	other := width("C0OTHER")
	// joining it (STARTING / PENDING): the badge and the fake mask only
	e.c.mu.Lock()
	e.c.joining = "C0OTHER"
	e.c.mu.Unlock()
	if got := other - width("C0OTHER"); got != faces {
		t.Errorf("joining: the faces took %d px, want them gone (%d px)", got, faces)
	}
	e.c.mu.Lock()
	e.c.joining = ""
	e.c.mu.Unlock()

	// in the huddle, connected: the faces stay
	s := e.join(nil, nil)
	waitState(t, s, "connected with the remote", func(st huddle.State) bool {
		return st.Phase == huddle.Connected && len(st.Participants) == 2
	})
	if got := width(hcChannel); got != other {
		t.Errorf("in your huddle the suffix is %d px wide, a huddle you're not in with as many people %d (the faces are missing)", got, other)
	}
}

// --huddles-offset: -2.235% of the stack's own width.
func TestSidebarFaceSteps(t *testing.T) {
	if got := sbFaceSteps(1, 20); len(got) != 1 || got[0] != 0 {
		t.Fatalf("one face: %v", got)
	}
	// two 20px faces: W = 40 / 1.02235 = 39.13, the second at 20 − 0.87
	if got := sbFaceSteps(2, 20); got[1] != 19 {
		t.Errorf("two 20px faces: second at %d, want 19", got[1])
	}
	if got := sbFaceSteps(2, 40); got[1] != 38 { // 2x: 40 − 1.75
		t.Errorf("two 40px faces: second at %d, want 38", got[1])
	}
}

// ChannelHoverCardHuddleBadge (CardBadge) is the sidebar suffix's badge:
// the same stack, mask and count badge, as wide; without tooltips; and its
// fake mask is p-search_autocomplete__huddle_fake_mask_svg's 19px, so the
// mask's bottom row shows what's under it where the sidebar's 20px one
// doesn't. The painted badge ends 3px before the run's end (margin-right
// 4px, left 1px).
func TestCardBadgeLikeSidebar(t *testing.T) {
	for _, dark := range []bool{false, true} {
		e := tipEnv(t, dark, huddle.FakeRemoteUser, "U0THIRD")
		th := theme.New(dark)
		bg := color.NRGBA{R: 255, A: 255}
		draw := func(card bool) (*image.RGBA, int, layout.Context) {
			var ops op.Ops
			gtx := shotCtx(&ops, 220, 28)
			theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, bg)
			r := gtx.Constraints.Max.X - gtx.Dp(6)
			var left int
			if card {
				left = e.c.CardBadge(gtx, th, "C0OTHER", r, gtx.Constraints.Max.Y)
			} else {
				left = e.c.SidebarSuffix(gtx, th, "C0OTHER", r, gtx.Constraints.Max.Y, false)
			}
			return pixeltest.Render(t, 220*shotScale, 28*shotScale, &ops), r - left, gtx
		}
		side, sw, gtx := draw(false)
		card, cw, _ := draw(true)
		if cw != sw {
			t.Errorf("card badge %d px wide, the sidebar's %d", cw, sw)
		}
		if hs := e.c.hdrState("C0OTHER"); hs.sbBadge.tip.Showing(time.Now()) || hs.sbFaces.tip.Showing(time.Now()) {
			t.Error("the card's badge shows a tooltip")
		}
		r := gtx.Constraints.Max.X - gtx.Dp(6)
		nw := th.Measure(gtx, 13, theme.Bold, "2")
		w := max(gtx.Dp(36), gtx.Dp(6)+gtx.Dp(16)+gtx.Dp(2)+nw+gtx.Dp(6))
		x1 := r - gtx.Dp(3)
		y0 := (gtx.Constraints.Max.Y - gtx.Dp(20)) / 2
		near := func(a color.RGBA, b color.NRGBA) bool {
			d := func(x, y uint8) int {
				if x > y {
					return int(x - y)
				}
				return int(y - x)
			}
			return d(a.R, b.R) < 12 && d(a.G, b.G) < 12 && d(a.B, b.B) < 12
		}
		if c := card.RGBAAt(x1-1, y0+gtx.Dp(10)); near(c, bg) {
			t.Errorf("no badge just inside x=%d (it should end 3px before the run's end): %v", x1, c)
		}
		if c := card.RGBAAt(x1+1, y0+gtx.Dp(10)); !near(c, bg) {
			t.Errorf("badge paint at x=%d, past its end: %v", x1+1, c)
		}
		mx := x1 - w - 1 // the mask's right column
		low := y0 + gtx.Dp(19) + 1
		if c := card.RGBAAt(mx, low); !near(c, bg) {
			t.Errorf("card: the 19px mask filled its 20th row at (%d,%d): %v", mx, low, c)
		}
		if c := side.RGBAAt(mx, low); near(c, bg) {
			t.Errorf("sidebar: the 20px mask left its 20th row empty at (%d,%d)", mx, low)
		}
		if c := card.RGBAAt(mx, y0+gtx.Dp(10)); near(c, bg) {
			t.Errorf("card: no mask at (%d,%d)", mx, y0+gtx.Dp(10))
		}
		render(t, "card-badge-"+map[bool]string{false: "light", true: "dark"}[dark], 220, 28, func(gtx C) {
			theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, th.C.Bg)
			e.c.CardBadge(gtx, th, "C0OTHER", gtx.Constraints.Max.X-gtx.Dp(6), gtx.Constraints.Max.Y)
		})
	}
}
