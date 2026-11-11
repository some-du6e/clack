package huddleui

import (
	"testing"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/theme"
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
