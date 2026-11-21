package store

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

func msgTS(ms []slack.Message) string {
	var ts []string
	for i := range ms {
		ts = append(ts, ms[i].TS)
	}
	return strings.Join(ts, " ")
}

func page(ts ...string) []slack.Message {
	var out []slack.Message
	for _, x := range ts {
		out = append(out, slack.Message{Type: "message", TS: x, User: "U1", Text: "synthetic " + x})
	}
	return out
}

// Slack's resetReachedEnd at a slow connect (client-boot 6Yed): every
// window's end goes stale at its newest message from Slack (not the
// viewer's unconfirmed send, not an ephemeral message); the socket's
// messages past it wait in Ahead and no window holds them; one at or
// before the end goes into the window as before. A second reset keeps the
// first's end, and moves the generation.
func TestResetReachedEndHoldsTheSocketsMessages(t *testing.T) {
	s := New("")
	const ch = "C1"
	s.SetLatest(ch, page("1700000001.000100", "1700000002.000100"), true)
	s.Upsert(ch, slack.Message{Type: "message", TS: "1700000003.000100", Text: "Only visible to you", IsEphemeral: true})
	s.Upsert(ch, slack.Message{Type: "message", TS: "1700000004.000100", Text: "a send on its way", ClientMsgID: "cid-1"})
	s.SetLatest("C2", page("1700000005.000100"), false)
	if n := s.ResetReachedEnd(map[string]bool{ch + "/1700000004.000100": true}); n != 2 {
		t.Fatalf("reset %d windows, want 2", n)
	}
	end, gen, stale := s.EndOf(ch)
	if !stale || end != "1700000002.000100" {
		t.Fatalf("end %q stale %v, want 1700000002.000100: the newest message from Slack", end, stale)
	}
	if s.UpsertHeard(ch, slack.Message{Type: "message", TS: "1700000002.000100", Text: "an edit of the end"}) {
		t.Error("a message the window holds went ahead")
	}
	if !s.UpsertHeard(ch, slack.Message{Type: "message", TS: "1700000009.000100", Text: "after the gap"}) {
		t.Error("a message past the stale end went into the window")
	}
	if got := msgTS(s.Timeline(ch).Msgs); strings.Contains(got, "1700000009.000100") {
		t.Errorf("the window holds the message past its end: %s", got)
	}
	if !s.IsAhead(ch, "1700000009.000100") || !s.Holds(ch, "1700000009.000100", "") {
		t.Error("the waiting message isn't found where it waits")
	}
	s.Mutate(ch, "1700000009.000100", func(m *slack.Message) { m.Text = "after the gap, edited" })
	if m, ok := s.Find(ch, "1700000009.000100"); !ok || m.Text != "after the gap, edited" {
		t.Errorf("an edit of the waiting message: %q %v", m.Text, ok)
	}
	s.ResetReachedEnd(nil)
	if end2, gen2, _ := s.EndOf(ch); end2 != end || gen2 == gen {
		t.Errorf("a second reset: end %q gen %d, want the first's end %q and a new gen (was %d)", end2, gen2, end, gen)
	}
}

// A page read forward from the end that leaves nothing newer joins the
// window, and what waited past the end joins it: newer than the page (it
// came after the read), or ephemeral. One the page covers and doesn't
// hold was deleted meanwhile. A page with more left leaves the window as
// it was (nothing the lists draw goes past the old end before the join);
// so does one read across another reset (what that gap missed is after
// the page). A page for a window that isn't stale any more is dropped (ok
// false).
func TestCatchUpJoinsTheWindow(t *testing.T) {
	s := New("")
	const ch = "C1"
	s.SetLatest(ch, page("1700000001.000100", "1700000002.000100"), true)
	s.ResetReachedEnd(nil)
	for _, m := range []slack.Message{
		{Type: "message", TS: "1700000004.000100", Text: "posted in the gap, then deleted"},
		{Type: "message", TS: "1700000006.000100", Text: "Only visible to you", IsEphemeral: true},
		{Type: "message", TS: "1700000009.000100", Text: "after the read"},
	} {
		if !s.UpsertHeard(ch, m) {
			t.Fatalf("%s didn't wait past the end", m.TS)
		}
	}
	end, gen, _ := s.EndOf(ch)
	joined, ok := s.CatchUp(ch, end, gen, page("1700000002.000100", "1700000003.000100"), true, s.LiveSeq())
	if joined || !ok {
		t.Fatalf("a page with more left: joined %v ok %v", joined, ok)
	}
	if end2, _, stale := s.EndOf(ch); !stale || end2 != end {
		t.Fatalf("after a page with more left: end %q stale %v, want %q and stale", end2, stale, end)
	}
	if got := msgTS(s.Timeline(ch).Msgs); got != "1700000001.000100 1700000002.000100" {
		t.Errorf("a page with more left changed the window: %s", got)
	}
	s.ResetReachedEnd(nil) // the socket went and came back while the next page was out
	joined, ok = s.CatchUp(ch, end, gen, page("1700000002.000100", "1700000003.000100", "1700000005.000100"), false, s.LiveSeq())
	if joined || !ok {
		t.Fatalf("a page read across another reset: joined %v ok %v, want the end left stale", joined, ok)
	}
	end, gen, _ = s.EndOf(ch)
	joined, ok = s.CatchUp(ch, end, gen, page("1700000002.000100", "1700000003.000100", "1700000005.000100", "1700000007.000100"), false, s.LiveSeq())
	if !joined || !ok {
		t.Fatalf("the last page: joined %v ok %v", joined, ok)
	}
	tl := s.Timeline(ch)
	if got, want := msgTS(tl.Msgs), "1700000001.000100 1700000002.000100 1700000003.000100 1700000005.000100 1700000006.000100 1700000007.000100 1700000009.000100"; got != want {
		t.Errorf("the caught-up window: %s, want %s", got, want)
	}
	if tl.EndStale || len(tl.Ahead) != 0 {
		t.Errorf("caught up but EndStale %v, %d waiting", tl.EndStale, len(tl.Ahead))
	}
	if _, ok := s.CatchUp(ch, "1700000007.000100", gen, page("1700000007.000100"), false, s.LiveSeq()); ok {
		t.Error("a page for a window that isn't stale any more was taken")
	}
}

// A forward page that holds the confirmed copy of the viewer's send (its
// client_msg_id) drops the local copy: one message, not two.
func TestCatchUpTakesTheConfirmedSend(t *testing.T) {
	s := New("")
	const ch = "C1"
	s.SetLatest(ch, page("1700000001.000100"), true)
	s.ResetReachedEnd(nil)
	s.Upsert(ch, slack.Message{Type: "message", TS: "1700000099.000100", Text: "mine", ClientMsgID: "cid-7"})
	end, gen, _ := s.EndOf(ch)
	p := page("1700000001.000100", "1700000002.000100")
	p[1].ClientMsgID, p[1].Text = "cid-7", "mine"
	if joined, _ := s.CatchUp(ch, end, gen, p, false, s.LiveSeq()); !joined {
		t.Fatal("not joined")
	}
	if got, want := msgTS(s.Timeline(ch).Msgs), "1700000001.000100 1700000002.000100"; got != want {
		t.Errorf("the window: %s, want %s (the confirmed copy alone)", got, want)
	}
}

// The newest page (a catch-up more than a page behind, a poll) is the
// end: what waited past the stale end joins it, and the end isn't stale.
func TestNewestPageJoinsTheWaitingMessages(t *testing.T) {
	s := New("")
	const ch = "C1"
	s.SetLatest(ch, page("1700000001.000100"), true)
	s.ResetReachedEnd(nil)
	s.UpsertHeard(ch, slack.Message{Type: "message", TS: "1700000080.000100", Text: "in the page too"})
	s.UpsertHeard(ch, slack.Message{Type: "message", TS: "1700000095.000100", Text: "after the read"})
	s.SetLatestRead(ch, page("1700000070.000100", "1700000080.000100", "1700000090.000100"), true, s.LiveSeq())
	tl := s.Timeline(ch)
	if got, want := msgTS(tl.Msgs), "1700000070.000100 1700000080.000100 1700000090.000100 1700000095.000100"; got != want {
		t.Errorf("the newest window: %s, want %s", got, want)
	}
	if tl.EndStale || len(tl.Ahead) != 0 {
		t.Errorf("the newest page left EndStale %v, %d waiting", tl.EndStale, len(tl.Ahead))
	}
}

// An older page goes in front of the window only where the window still
// starts at (or before) the message it was read before: in front of a
// window read again since, it left the messages between out. Nor does an
// older page make a window where none is.
func TestOlderPageJoinsOnlyWhereTheWindowStarts(t *testing.T) {
	s := New("")
	const ch = "C1"
	s.SetLatest(ch, page("1700000050.000100", "1700000051.000100"), true)
	kept := s.PrependOlderAt(ch, "1700000050.000100", page("1700000048.000100", "1700000049.000100"), true, s.LiveSeq())
	if len(kept) != 2 {
		t.Errorf("the read kept %d messages, want 2", len(kept))
	}
	if got, want := msgTS(s.Timeline(ch).Msgs), "1700000048.000100 1700000049.000100 1700000050.000100 1700000051.000100"; got != want {
		t.Errorf("a page before the window's start: %s, want %s", got, want)
	}
	// the window was read again (started over at a newer page)
	s.SetLatestRead(ch, page("1700000090.000100", "1700000091.000100"), true, s.LiveSeq())
	s.PrependOlderAt(ch, "1700000048.000100", page("1700000046.000100", "1700000047.000100"), true, s.LiveSeq())
	if got, want := msgTS(s.Timeline(ch).Msgs), "1700000090.000100 1700000091.000100"; got != want {
		t.Errorf("a page read for the old window: %s, want %s", got, want)
	}
	s.PrependOlderAt("C9", "1700000048.000100", page("1700000047.000100"), true, s.LiveSeq())
	if s.Timeline("C9") != nil {
		t.Error("an older page made a window")
	}
}

// The newest page (a catch-up more than a page behind, a poll) keeps the
// viewer's sends the window held and it doesn't: one confirmed after the
// page was read, a pending copy, a failed one (mine). A local copy whose
// confirmed copy the page holds (its client_msg_id) is that copy. A
// message that isn't one of the viewer's sends goes, as it did.
func TestNewestPageKeepsTheViewersSends(t *testing.T) {
	s := New("")
	const ch = "C1"
	s.SetLatest(ch, page("1700000001.000100", "1700000002.000100"), true)
	for _, m := range []slack.Message{
		{Type: "message", TS: "1700000050.000100", Text: "sent and confirmed after the page was read", ClientMsgID: "cid-a"},
		{Type: "message", TS: "1700000051.000100", Text: "still pending", ClientMsgID: "cid-b"},
		{Type: "message", TS: "1700000052.000100", Text: "failed", ClientMsgID: "cid-c"},
		{Type: "message", TS: "1700000053.000100", Text: "pending, confirmed in the page", ClientMsgID: "cid-d"},
		{Type: "message", TS: "1700000003.000100", Text: "someone else's, deleted since"},
	} {
		s.Upsert(ch, m)
	}
	mine := map[string]bool{"1700000050.000100": true, "1700000051.000100": true, "1700000052.000100": true, "1700000053.000100": true}
	p := page("1700000002.000100", "1700000040.000100", "1700000041.000100")
	p[2].ClientMsgID = "cid-d"
	s.SetLatestReadAt(ch, p, true, s.LiveSeq(), s.EndGen(), mine)
	if got, want := msgTS(s.Timeline(ch).Msgs), "1700000002.000100 1700000040.000100 1700000041.000100 1700000050.000100 1700000051.000100 1700000052.000100"; got != want {
		t.Errorf("the newest window: %s, want %s", got, want)
	}
}

// A slow connect while the newest page was out: what that gap missed is
// after the page, so the window's end stays stale at the page's newest
// message, and a message heard past it still waits.
func TestNewestPageReadAcrossAResetStaysStale(t *testing.T) {
	s := New("")
	const ch = "C1"
	s.SetLatest(ch, page("1700000001.000100"), true)
	gen := s.EndGen()
	s.ResetReachedEnd(nil) // the hello lands while the page is out
	s.UpsertHeard(ch, slack.Message{Type: "message", TS: "1700000095.000100", Text: "after the reconnect"})
	s.SetLatestReadAt(ch, page("1700000070.000100", "1700000080.000100"), true, s.LiveSeq(), gen, nil)
	end, _, stale := s.EndOf(ch)
	if !stale || end != "1700000080.000100" {
		t.Fatalf("end %q stale %v, want 1700000080.000100 and stale", end, stale)
	}
	if !s.IsAhead(ch, "1700000095.000100") {
		t.Error("the message after the reconnect doesn't wait past the new end")
	}
	if got := msgTS(s.Timeline(ch).Msgs); got != "1700000070.000100 1700000080.000100" {
		t.Errorf("the window: %s", got)
	}
}

// A newest page that meets the window keeps its epoch; one with more
// history before it whose oldest message is newer than the window's
// newest from Slack (the viewer's sends aside, its stale End) starts a
// new window, which the lists take whole.
func TestNewestPageThatMissesStartsANewEpoch(t *testing.T) {
	s := New("")
	const ch = "C1"
	s.SetLatest(ch, page("1700000001.000100", "1700000002.000100"), true)
	e0 := s.Timeline(ch).Epoch
	s.SetLatestReadAt(ch, page("1700000002.000100", "1700000003.000100"), true, s.LiveSeq(), s.EndGen(), nil)
	if e := s.Timeline(ch).Epoch; e != e0 {
		t.Errorf("a page reaching the window moved the epoch %d → %d", e0, e)
	}
	s.Upsert(ch, slack.Message{Type: "message", TS: "1700000095.000100", Text: "mine, confirmed", ClientMsgID: "cid-x"})
	s.SetLatestReadAt(ch, page("1700000090.000100", "1700000091.000100"), true, s.LiveSeq(), s.EndGen(), map[string]bool{"1700000095.000100": true})
	e1 := s.Timeline(ch).Epoch
	if e1 == e0 {
		t.Error("a page past the window's end (the viewer's send aside) kept the epoch")
	}
	if !s.Holds(ch, "1700000095.000100", "") {
		t.Error("the viewer's send went with the old window")
	}
	s.ResetReachedEnd(nil)
	s.SetLatestReadAt(ch, page("1700000091.000100", "1700000099.000100"), true, s.LiveSeq(), s.EndGen(), nil)
	if e := s.Timeline(ch).Epoch; e != e1 {
		t.Errorf("a page reaching the stale end moved the epoch %d → %d", e1, e)
	}
}
