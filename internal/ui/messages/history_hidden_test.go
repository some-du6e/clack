package messages

import (
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/taut"
)

// TestHiddenHistoryRows: what plugins' patches take out of a list's
// channelHistory (Slick's HideResolved, a help channel's resolved
// tickets) isn't drawn, as Slack's list draws that history, and comes
// back once the patch lets it go. A jump to such a message waits while
// taut's answer is behind the jump's highlight (HideResolved keeps a
// linked ticket shown, through viewUiState's highlightTs) and lands on it
// once the answer keeps it: it didn't fall back to the next message and
// leave the jump done. Synthetic history on the fake; taut's answers are
// the test's (historyHiddenHook).
func TestHiddenHistoryRows(t *testing.T) {
	e := newHistEnv(t)
	var mu sync.Mutex
	hid, ok := (*taut.Hidden)(nil), true
	set := func(h *taut.Hidden, ready bool) {
		mu.Lock()
		hid, ok = h, ready
		mu.Unlock()
	}
	historyHiddenHook = func(key string) (*taut.Hidden, bool) {
		mu.Lock()
		defer mu.Unlock()
		if key != winEndChan {
			return nil, true
		}
		return hid, ok
	}
	t.Cleanup(func() { historyHiddenHook = nil })
	n := len(e.tss)
	gone := e.tss[n-2]
	set(taut.HiddenOf(gone), true)
	e.await(t, "the hidden message's row gone, its neighbours drawn", func() bool {
		return !e.drawn(gone) && e.drawn(e.tss[n-1]) && e.drawn(e.tss[n-3])
	})
	set(nil, true)
	e.await(t, "the row back once nothing is hidden", func() bool { return e.drawn(gone) })

	// a jump to a hidden message while taut's answer is behind its
	// highlight: it waits, then lands on it
	target := e.tss[n-15]
	set(taut.HiddenOf(target), false)
	e.v.Highlight(target)
	e.frames(300 * time.Millisecond)
	if s := e.v.s; s.pendingJump != target || e.drawn(target) {
		t.Fatalf("while taut's answer was behind: the jump pending %q, the message drawn %v; want the jump to %s waiting", s.pendingJump, e.drawn(target), target)
	}
	set(nil, true) // the patch keeps the highlighted message
	e.await(t, "the jump on the highlighted message", func() bool { return e.drawn(target) && e.v.s.pendingJump == "" })
}
