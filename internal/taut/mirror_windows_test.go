package taut

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// TestMirrorHoldsJumpWindows: a jump's window (core's store windows) is in
// the mirror as Slack's messages store and channelHistory hold every
// slice: messages[ch] has its messages, channelHistory[ch] lists it as a
// slice of its own before the timeline's, its arrival changes messages'
// fingerprint (the sweep sends it to JS), and Held counts its messages
// (InjectedMessages doesn't take them for a plugin's). The mirror held the
// timeline and the threads only: plugins never saw a window's messages,
// and its rows drew unpatched.
func TestMirrorHoldsJumpWindows(t *testing.T) {
	h := newHarness(t)
	m := h.tt.mirror
	fp := m.messagesFP(chGen)
	old := []slack.Message{
		{Type: "message", TS: "1600000100.000100", User: "UALICE", Text: "an old message"},
		{Type: "message", TS: "1600000200.000100", User: "UBOB", Text: "another old one"},
	}
	if _, merged := h.app.S.AddWindow(chGen, old, old[0].TS, old[1].TS, true, true, h.app.S.LiveSeq()); merged {
		t.Fatal("the old stretch joined the timeline; the test needs a window")
	}
	if m.messagesFP(chGen) == fp {
		t.Error("the window left messages' fingerprint as it was: the sweep never sends it to JS")
	}
	b := m.bucket(chGen)
	for _, x := range old {
		if b[x.TS] == nil {
			t.Errorf("messages[%s] lacks the window's %s", chGen, x.TS)
		}
	}
	hist, _ := m.history(chGen).(map[string]any)
	slices, _ := hist["slices"].([]any)
	var first []string
	if len(slices) > 0 {
		if s0, ok := slices[0].(map[string]any); ok {
			first, _ = s0["timestamps"].([]string)
		}
	}
	if len(slices) != 2 || len(first) != 2 || first[0] != old[0].TS || first[1] != old[1].TS {
		t.Errorf("channelHistory[%s]: %d slices, the first %v; want the window's (%s, %s) and then the timeline's", chGen, len(slices), first, old[0].TS, old[1].TS)
	}
	if held := h.app.S.Held(chGen, []store.MsgKey{{TS: old[0].TS}, {TS: old[1].TS}}); !held[0] || !held[1] {
		t.Errorf("Held %v for the window's messages: InjectedMessages takes them for a plugin's own", held)
	}
}
