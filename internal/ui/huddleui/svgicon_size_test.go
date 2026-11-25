package huddleui

import (
	"slices"
	"testing"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Synthetic fixtures only.

// Slack's SvgIcon with no size is 1em of the font it sits in ([data-5jx]
// {width:1em;height:1em;font-size:var(--s,inherit)}): the sidebar's
// HuddleSidebarLinkPeerCountBadge headphones are 1em of the badge's 13px,
// the window title's (HuddleMvaWindowTitle) 1em of its 13px. Clack drew
// both at 16.
func TestSvgIconSizesHuddles(t *testing.T) {
	e := newShotEnv(t, false)
	th := theme.New(false)
	var got map[string][]int
	prev := icons.DrawSizeHook
	icons.DrawSizeHook = func(name string, px int) { got[name] = append(got[name], px) }
	t.Cleanup(func() { icons.DrawSizeHook = prev })

	e.c.mu.Lock()
	e.c.live["C0SVGHUD001"] = liveRoom{ID: "R0SVG", Channel: "C0SVGHUD001", Participants: []string{huddle.FakeRemoteUser, "U0THIRD"}}
	e.c.mu.Unlock()
	var ops op.Ops
	got = map[string][]int{}
	gtx := shotCtx(&ops, 220, 28)
	r := gtx.Constraints.Max.X - gtx.Dp(6)
	e.c.SidebarSuffix(gtx, th, "C0SVGHUD001", r, gtx.Constraints.Max.Y, false)
	if px := got["headphones"]; !slices.Equal(px, []int{gtx.Dp(13)}) {
		t.Errorf("the sidebar's count badge drew its headphones at %v px, want [%d] (13px at %dx)", px, gtx.Dp(13), shotScale)
	}

	const W, H = 589, 351 // Slack's compact window
	e.join(nil, nil)
	for range 2 { // the window's first frame measures
		got = map[string][]int{}
		ops.Reset()
		e.w.layout(shotCtx(&ops, W, H))
	}
	if px := got["headphones"]; !slices.Contains(px, gtx.Dp(13)) {
		t.Errorf("the window drew headphones at %v px, want the title's at %d (13px)", px, gtx.Dp(13))
	}
}
