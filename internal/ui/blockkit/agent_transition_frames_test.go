package blockkit

// The height transition (.block's height .25s) asks the host for frames
// while it runs, through me.changed() and Host.Invalidate, never through a
// mid-layout op.InvalidateCmd, and stops asking once it ends: a settled
// plan asks for none, opening it asks while it grows, and the frames after
// the .25s ask for none again. Drawn on the wall clock, which the
// measuring pass also uses (Message.scratch).

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestAgentTransitionAsksFramesThenStops(t *testing.T) {
	h := &agentHost{th: theme.New(false)}
	m := agentMsg(t, "plan-long-details")
	s := newAgentShot(h, m)
	const w, hgt = 560, 900
	frames := func(n int, gap time.Duration) {
		for i := 0; i < n; i++ {
			s.frame(w, hgt, time.Now())
			time.Sleep(gap)
		}
	}
	frames(3, 20*time.Millisecond)
	n0 := h.inval.Load()
	frames(3, 20*time.Millisecond)
	if n := h.inval.Load() - n0; n != 0 {
		t.Fatalf("a settled, closed plan asked the host for %d frames", n)
	}
	s.rows[0].me.agentState().open["plan\x00p5"] = true
	n1 := h.inval.Load()
	frames(4, 16*time.Millisecond)
	if h.inval.Load() == n1 {
		t.Error("opening the plan asked for no frames: its .25s height transition didn't run")
	}
	time.Sleep(300 * time.Millisecond) // past the .25s
	frames(2, 16*time.Millisecond)     // the frame that ends it
	n2 := h.inval.Load()
	frames(4, 20*time.Millisecond)
	if n := h.inval.Load() - n2; n != 0 {
		t.Errorf("after the transition ended the plan asked for %d more frames", n)
	}
}
