package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// msgs are n synthetic messages of ch, one a minute up to base.
func evictMsgs(ch string, n int, base int64) []slack.Message {
	out := make([]slack.Message, n)
	for i := range out {
		out[i] = slack.Message{TS: fmt.Sprintf("%d.000100", base-int64(n-i)*60), User: "U1", Text: "synthetic text"}
	}
	return out
}

func evictInput(now time.Time, lastRead map[string]string) EvictInput {
	return EvictInput{Now: now, Accessed: map[string]time.Time{}, Keep: map[string]bool{}, Pending: map[string]bool{},
		Conv:     func(ch string) (bool, bool) { return ch != "Cunknown", ch != "Cpreview" },
		WholeOK:  func(ch string) bool { return ch != "Cmention" },
		LastRead: func(ch string) string { return lastRead[ch] },
	}
}

// Slack's message cache budget (evict.go): nothing goes while fewer than
// cacheSizeMaximum messages may be evicted.
func TestEvictUnderBudget(t *testing.T) {
	s := New("")
	base := int64(1762500000)
	s.SetLatest("C1", evictMsgs("C1", EvictMaximum-1, base), true)
	in := evictInput(time.Now(), map[string]string{"C1": fmt.Sprintf("%d.000100", base)})
	if out := s.EvictMessages(in); out != nil {
		t.Fatalf("evicted %v under the budget", out)
	}
	if n := len(s.Timeline("C1").Msgs); n != EvictMaximum-1 {
		t.Fatalf("C1 has %d messages, want %d", n, EvictMaximum-1)
	}
}

// Over the budget the oldest read messages go first, down to
// cacheSizeTarget; unread, pending and on-screen messages stay, a channel
// the viewer isn't in loses everything first-in-order, and a quiet
// channel not read for an hour starts over whole.
func TestEvictToTarget(t *testing.T) {
	s := New("")
	now := time.Unix(1762600000, 0)
	base := now.Unix() - 3600*24
	// C1: 2,500 messages, the newest 100 unread, read 30 min ago
	s.SetLatest("C1", evictMsgs("C1", 2500, base), true)
	// C2: 1,500 read messages, read 2 h ago, with a mention waiting
	// (not reset whole: Cmention is the one WholeOK refuses)
	s.SetLatest("Cmention", evictMsgs("Cmention", 1500, base-100000), true)
	// Cquiet: 400 read messages, not read for 3 h: reset whole
	s.SetLatest("Cquiet", evictMsgs("Cquiet", 400, base-200000), true)
	// a preview (not a member): everything may go
	s.SetLatest("Cpreview", evictMsgs("Cpreview", 300, base-300000), true)
	// on screen: untouched whatever its size
	s.SetLatest("Copen", evictMsgs("Copen", 1200, base-400000), true)
	// unknown conversation: Slack skips it
	s.SetLatest("Cunknown", evictMsgs("Cunknown", 50, base-500000), true)
	// a pending send in C1's read part never goes
	c1 := s.Timeline("C1").Msgs
	s.Mutate("C1", c1[5].TS, func(m *slack.Message) { m.ClientMsgID = "pend-1" })

	lastRead := map[string]string{"C1": c1[2399].TS, "Cmention": fmt.Sprintf("%d.000100", base), "Cquiet": fmt.Sprintf("%d.000100", base),
		"Copen": fmt.Sprintf("%d.000100", base)}
	in := evictInput(now, lastRead)
	in.Accessed["C1"] = now.Add(-30 * time.Minute)
	in.Accessed["Cmention"] = now.Add(-2 * time.Hour)
	in.Accessed["Cquiet"] = now.Add(-3 * time.Hour)
	in.Keep["Copen"] = true
	in.Pending["pend-1"] = true
	out := s.EvictMessages(in)
	got := map[string]Evicted{}
	for _, e := range out {
		got[e.Channel] = e
	}
	if !got["Cquiet"].Whole || s.Timeline("Cquiet") != nil {
		t.Errorf("Cquiet wasn't reset whole: %+v", got["Cquiet"])
	}
	if !got["Cpreview"].Whole || s.Timeline("Cpreview") != nil {
		t.Errorf("the preview kept messages: %+v", got["Cpreview"])
	}
	if _, ok := got["Copen"]; ok || len(s.Timeline("Copen").Msgs) != 1200 {
		t.Errorf("the open conversation lost messages")
	}
	if _, ok := got["Cunknown"]; ok || len(s.Timeline("Cunknown").Msgs) != 50 {
		t.Errorf("the unknown conversation lost messages")
	}
	// what's left that may be evicted is cacheSizeTarget messages at most
	kept := 0
	c1 = s.Timeline("C1").Msgs
	for _, m := range c1 {
		if !TSLess(lastRead["C1"], m.TS) && m.ClientMsgID != "pend-1" {
			kept++
		}
	}
	if cm := s.Timeline("Cmention"); cm != nil {
		kept += len(cm.Msgs)
	}
	if kept > EvictTarget {
		t.Errorf("%d evictable messages kept, target %d", kept, EvictTarget)
	}
	unread, pending := 0, false
	for _, m := range c1 {
		if TSLess(lastRead["C1"], m.TS) {
			unread++
		}
		pending = pending || m.ClientMsgID == "pend-1"
	}
	if unread != 100 {
		t.Errorf("C1 kept %d unread messages, want all 100", unread)
	}
	if !pending {
		t.Error("the pending send went")
	}
	// C1 was read 30 min ago: its messages sort by that access, after
	// Cmention's (ts order, older): Cmention goes before C1 does
	if cm := s.Timeline("Cmention"); cm != nil && len(cm.Msgs) > 0 && len(c1) < 2500-1 {
		t.Errorf("C1 lost messages while Cmention, older and unread for 2 h, kept %d", len(cm.Msgs))
	}
	if !s.Timeline("C1").HasMore {
		t.Error("a trimmed timeline must say older messages exist")
	}
}
