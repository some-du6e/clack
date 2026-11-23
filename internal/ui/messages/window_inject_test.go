package messages

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

// TestInjectedMessagesStayInTheirSlice: a plugin's injected messages
// (injectMessages; Message Logger puts its kept deletions back that way)
// go into a jump's window only where the window's slice covers them, as
// withTimestamps adds an injected ts only to a channelHistory slice whose
// start and end cover it (05_slack.js). A deletion kept from the newest
// range came under the window's last row and counted as the window's end:
// a jump from the window to a message between the two counted as covered
// and landed on the kept message. Synthetic history on the fake; the
// injected message is the list's own (no plugin runtime).
func TestInjectedMessagesStayInTheirSlice(t *testing.T) {
	e := newWinEnv(t)
	s := e.v.s
	kept := core.Message{TS: strings.TrimSuffix(e.tss[220], "100") + "500", Channel: winEndChan, User: "UBEN00001",
		Text: "a deleted message a logger kept"}
	s.inj.msgs = []core.Message{kept}
	s.snap.rev = 0 // the next sync merges what it covers
	e.rt.frame()
	w := s.win
	w.mu.Lock()
	first, last := w.msgs[0].m.TS, w.msgs[len(w.msgs)-1].m.TS
	w.mu.Unlock()
	for i := range s.snap.msgs {
		if s.snap.msgs[i].m.TS == kept.TS {
			t.Errorf("the window, messages %d to %d, merged an injected message from the newest range (after message 220)",
				e.index(first), e.index(last))
		}
	}
	target := e.tss[120] // past the window's end, before the kept message
	e.v.Highlight(target)
	e.await(t, "the jump past the window's end", func() bool { return e.drawn(target) && s.pendingJump == "" })
}
