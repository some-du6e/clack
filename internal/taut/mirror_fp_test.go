package taut

import (
	"fmt"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// A channel's messages fingerprint is the same every time while nothing
// changed, however many threads are cached: the sweep re-sends the slice
// to JS when it differs, and a fingerprint that walked the threads in map
// order re-rendered every messages-reading plugin's rows every 750 ms.
func TestMessagesFingerprintStable(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 12; i++ {
		root := fmt.Sprintf("17000001%02d.000100", i)
		h.app.S.SetThread(chGen, root, []slack.Message{
			{Type: "message", TS: root, User: "UALICE", Text: "root", ThreadTS: root, ReplyCount: 1},
			{Type: "message", TS: fmt.Sprintf("17000002%02d.000100", i), User: "UBOB", Text: "reply", ThreadTS: root},
		})
	}
	m := h.tt.mirror
	fp := m.messagesFP(chGen)
	for i := 0; i < 50; i++ {
		if got := m.messagesFP(chGen); got != fp {
			t.Fatalf("fingerprint changed with nothing changed (%x then %x)", fp, got)
		}
	}
}
