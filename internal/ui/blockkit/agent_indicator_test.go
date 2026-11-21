package blockkit

// A running plan's spinner asks for its next frame (33ms) only while the
// window is active, as ui's indicatorFrame does for every working agent's
// indicator: an inactive window's spinners hold still.

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestAgentSpinnerInactiveWindow(t *testing.T) {
	h := &agentHost{th: theme.New(false)}
	s := newAgentShot(h, agentMsg(t, "plan-in-progress"))
	now := time.Now().Add(time.Hour)
	setIndicatorActive(t, false)
	s.frame(560, 300, now)
	s.r.WakeupTime() // read what the first frame asked for
	s.frame(560, 300, now.Add(100*time.Millisecond))
	if at, ok := s.r.WakeupTime(); ok {
		t.Errorf("an inactive window's spinner asked for a frame (+%v)", at.Sub(now))
	}
	setIndicatorActive(t, true)
	s.frame(560, 300, now.Add(200*time.Millisecond))
	if _, ok := s.r.WakeupTime(); !ok {
		t.Errorf("an active window's spinner asked for no frame")
	}
}
