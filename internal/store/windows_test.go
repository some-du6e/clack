package store

import (
	"fmt"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// msgsAt is messages with ts "1700000<i>.000100" for i in [lo, hi), each
// i a second apart (synthetic).
func msgsAt(lo, hi int) []slack.Message {
	var out []slack.Message
	for i := lo; i < hi; i++ {
		out = append(out, slack.Message{TS: tsAt(i), User: "U1", Text: "synthetic"})
	}
	return out
}

func tsAt(i int) string { return fmt.Sprintf("17000%05d.000100", i) }

// TestWindowsAreSlices: a stretch of history apart from the newest
// messages is a window of its own (Slack's channelHistory slices, invH
// insert/union): the timeline doesn't take it, Find and Mutate reach its
// messages, pages that meet it join it, and it joins the timeline only
// once the two meet (a newer page reaching it, the timeline paging back to
// it, or a page that reaches the newest message).
func TestWindowsAreSlices(t *testing.T) {
	s := New("")
	s.SetLatest("C1", msgsAt(900, 950), true) // the newest 50
	// a jump far back: the window around 100
	if _, merged := s.AddWindow("C1", msgsAt(72, 128), tsAt(72), tsAt(127), true, true); merged {
		t.Fatalf("a window far from the newest joined the timeline")
	}
	if tl := s.Timeline("C1"); len(tl.Msgs) != 50 || tl.Msgs[0].TS != tsAt(900) {
		t.Errorf("the timeline took the window: %d messages from %s", len(tl.Msgs), tl.Msgs[0].TS)
	}
	if _, ok := s.Find("C1", tsAt(100)); !ok {
		t.Errorf("Find doesn't reach a window's message")
	}
	if _, ok := s.Mutate("C1", tsAt(100), func(m *slack.Message) { m.Text = "edited" }); !ok {
		t.Errorf("Mutate doesn't reach a window's message")
	}
	if w, ok := s.WindowAt("C1", tsAt(100)); !ok || len(w.Msgs) != 56 || !w.HasOlder || w.Msgs[28].Text != "edited" {
		t.Errorf("WindowAt: %d messages, older %v, ok %v", len(w.Msgs), w.HasOlder, ok)
	}
	// the page before it (requestOlder: latest its start, inclusive) joins it
	w, merged := s.AddWindow("C1", msgsAt(44, 73), "", tsAt(72), true, true)
	if merged || w.Start != tsAt(44) || w.End != tsAt(127) || len(w.Msgs) != 84 {
		t.Errorf("the older page: window %s…%s, %d messages, merged %v; want one window 44…127", w.Start, w.End, len(w.Msgs), merged)
	}
	if tl := s.Timeline("C1"); len(tl.Msgs) != 50 {
		t.Errorf("an older page of the window reached the timeline: %d messages", len(tl.Msgs))
	}
	// the timeline paging back to the window: they're one
	s.PrependOlder("C1", msgsAt(120, 900), true)
	if _, ok := s.WindowAt("C1", tsAt(100)); ok {
		t.Errorf("the window stayed apart from the timeline that grew back to it")
	}
	if tl := s.Timeline("C1"); tl.Msgs[0].TS != tsAt(44) || !tl.HasMore || len(tl.Msgs) != 906 {
		t.Errorf("the timeline after it met the window: %d messages from %s (more %v), want 906 from 44", len(tl.Msgs), tl.Msgs[0].TS, tl.HasMore)
	}
	// a window whose newer page reaches the newest messages joins them
	s2 := New("")
	s2.SetLatest("C2", msgsAt(500, 550), true)
	s2.AddWindow("C2", msgsAt(400, 456), "", "", true, true)
	if _, merged := s2.AddWindow("C2", msgsAt(455, 510), tsAt(455), "", true, true); !merged {
		t.Errorf("a newer page meeting the timeline didn't join it")
	}
	if tl := s2.Timeline("C2"); tl.Msgs[0].TS != tsAt(400) || len(tl.Msgs) != 150 {
		t.Errorf("the timeline after the window met it: %d messages from %s, want 150 from 400", len(tl.Msgs), tl.Msgs[0].TS)
	}
	// a page that reaches the newest message is the timeline's, with no
	// timeline yet too
	s3 := New("")
	if _, merged := s3.AddWindow("C3", msgsAt(10, 40), "", "", true, false); !merged || s3.Timeline("C3") == nil {
		t.Errorf("a window reaching the newest message isn't the timeline")
	}
	s.Remove("C1", tsAt(60))
	if _, ok := s.Find("C1", tsAt(60)); ok {
		t.Errorf("Remove left the message")
	}
}
