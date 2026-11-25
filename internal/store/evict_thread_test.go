package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Karim's #help thread (2026-11-24): the message cache evicted its root
// and replies, and it kept the ephemerals (they're never evicted). Slack's
// eviction leaves a thread's history whole (removeCachedReplyTimestamps is
// the channel history's), so the pane's slice still lists every reply,
// and what the store no longer holds is ThreadSliceMissing, fetched by id.
// Clack took the evicted replies out of the slice: it listed the
// ephemerals alone, and the pane drew them with no root and no replies.
func TestEvictKeepsThreadSlice(t *testing.T) {
	s := New("")
	now := time.Unix(1762600000, 0)
	base := now.Unix() - 3600*24
	s.SetLatest("C1", evictMsgs("C1", 3500, base), true) // over the budget
	// not reset whole (evictInput's WholeOK refuses it): its read
	// messages go one by one
	const ch = "Cmention"
	root := fmt.Sprintf("%d.000100", base-500000)
	ts := func(i int) string { return fmt.Sprintf("%d.000100", base-500000+int64(i)) }
	s.SetLatest(ch, []slack.Message{{TS: root, ThreadTS: root, ReplyCount: 4, User: "U1", Text: "synthetic root"}}, false)
	msgs := []slack.Message{{TS: root, ThreadTS: root, ReplyCount: 4, User: "U1", Text: "synthetic root"}}
	tss := []string{root}
	for i := 1; i <= 4; i++ {
		msgs = append(msgs, slack.Message{TS: ts(i), ThreadTS: root, User: "UBOT", Text: "synthetic reply"})
		tss = append(tss, ts(i))
	}
	s.AddThreadPageSince(ch, root, msgs, tss, ThreadPageBounds{Oldest: root, HasMore: false}, s.LiveSeq())
	s.AddThreadPageSince(ch, root, msgs[:1], []string{root}, ThreadPageBounds{Latest: root, HasMore: false}, s.LiveSeq())
	for i := 5; i <= 6; i++ { // the bot's two "Only visible to you" messages, over the socket
		s.AddReply(ch, slack.Message{TS: ts(i), ThreadTS: root, User: "UBOT", Text: "synthetic ephemeral", IsEphemeral: true})
	}
	sl, _ := s.ThreadSliceAt(ch, root, root)
	if len(sl.TSs) != 7 {
		t.Fatalf("control: before the eviction the slice lists %v", sl.TSs)
	}
	// the channel's own window isn't read past its root, so it keeps it
	// (a timeline that loses every message starts over with its threads:
	// not Karim's case, whose #help window held more)
	in := evictInput(now, map[string]string{"C1": fmt.Sprintf("%d.000100", base), ch: "0000000000.000000"})
	if out := s.EvictMessages(in); out == nil {
		t.Fatal("control: nothing was evicted")
	}
	if th, _ := s.Thread(ch, root); len(th) != 2 {
		t.Fatalf("control: the thread's cache holds %d messages after the eviction, want its two ephemerals", len(th))
	}
	sl, _ = s.ThreadSliceAt(ch, root, root)
	if len(sl.TSs) != 7 {
		t.Errorf("after the eviction the slice lists %v, want all 7 timestamps (Slack's thread history keeps them)", sl.TSs)
	}
	if got := s.ThreadSliceMissing(ch, root, sl); len(got) != 4 {
		t.Errorf("missing %v, want the 4 evicted replies (the root is the channel window's)", got)
	}
	// the root comes from the channel's window, as Slack's one store gives
	// it: the pane draws it and its "N replies" divider (thread-ephemerals-2)
	if got := s.ThreadSliceMsgs(ch, root, sl); len(got) != 3 || got[0].TS != root || !got[1].IsEphemeral || !got[2].IsEphemeral {
		t.Errorf("the slice's held messages are %d (%v), want the root from the channel's window, then the 2 ephemerals", len(got), got)
	}
}
