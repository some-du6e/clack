package blockkit

// A plan task's own icon (TaskItem's TaskIcon, IDPB): a Block Kit icon
// name Slack knows (Qw/u deriveIconName) draws instead of the task's
// status glyph, unless the task is running or failed; an unknown name or
// a stray shape keeps the glyph, and never costs the block. Both looks.

import (
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestAgentTaskIcons(t *testing.T) {
	want := "rocket bug emoji-objects" // t1, t2, t5: the running, failed, unknown and string ones draw their glyphs
	for _, look := range []struct {
		name string
		h    *agentHost
	}{{"slim", &agentHost{th: theme.New(false)}}, {"cards", cardHost(false, "off")}} {
		got := captureTaskIcons(t)
		m := agentMsg(t, "plan-icons")
		s := newAgentShot(look.h, m)
		st := s.rows[0].me.agentState()
		st.open["plan\x00p7"] = true
		now := time.Now().Add(time.Hour)
		s.frame(560, 900, now)
		*got = (*got)[:0] // the next frame only draws (the first one measured too)
		s.frame(560, 900, now.Add(100*time.Millisecond))
		if g := strings.Join(*got, " "); g != want {
			t.Errorf("%s: task icons drawn %q, want %q", look.name, g, want)
		}
		bt := agentButton(&s.rows[0].m, 0)
		b := s.rows[0].me.agentBlock(&bt)
		if b == nil || len(b.Tasks) != 7 {
			t.Errorf("%s: the plan with a string icon didn't parse whole: %+v", look.name, b)
		}
	}
}
