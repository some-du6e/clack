package messages

import (
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
)

// Messages plugins inject (Message Logger's deleted copies) go to the list
// they belong to, and copies of messages the list already has never make
// a second row: the stale pending copy of a send (same client_msg_id,
// local ts) showed every sent message twice with Message Logger on.
func TestInjectedMessagesPlacement(t *testing.T) {
	parent := core.Message{TS: "1794189984.820469", ThreadTS: "1794189984.820469", Text: "root", ReplyCount: 1}
	sent := core.Message{TS: "1794190000.000200", Text: "hi", ClientMsgID: "cid-1"}
	staleLocal := core.Message{TS: "1794190000.000150", Text: "hi", ClientMsgID: "cid-1"} // the pending copy, swapped since
	deletedReply := core.Message{TS: "1794189990.412659", ThreadTS: parent.TS, Text: "test"}
	deletedTop := core.Message{TS: "1794189995.000100", Text: "gone"}
	injected := []core.Message{deletedReply, deletedTop, staleLocal}

	ch := &listState{ch: "C1"}
	ch.snap.msgs = []entry{{m: parent, rev: 1}, {m: sent, rev: 1}}
	for i := range injected {
		if ch.injVisible(&injected[i]) {
			ch.inj.msgs = append(ch.inj.msgs, injected[i])
		}
	}
	ch.mergeInjected()
	var got []string
	for _, e := range ch.snap.msgs {
		got = append(got, e.m.Text)
	}
	if want := []string{"root", "gone", "hi"}; !equalStrings(got, want) {
		t.Errorf("channel rows %q, want %q (the deleted reply stays in its thread; one copy of the sent message)", got, want)
	}

	th := &listState{ch: "C1", ts: parent.TS, thread: true}
	th.snap.msgs = []entry{{m: parent, rev: 1}}
	for i := range injected {
		if th.injVisible(&injected[i]) {
			th.inj.msgs = append(th.inj.msgs, injected[i])
		}
	}
	th.mergeInjected()
	got = got[:0]
	for _, e := range th.snap.msgs {
		got = append(got, e.m.Text)
	}
	if want := []string{"root", "test"}; !equalStrings(got, want) {
		t.Errorf("thread rows %q, want %q", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A plugin's injected messages come back from taut converted afresh (a
// name in one changed, a rename converted it again). A list that compares
// them equal takes the new copies and keeps every rev: kept, the old copies
// were never the new ones again, so every later check (each viewRefresh,
// every list) compared every message deep. One that changed gets a rev of
// its own, so its row draws it; the others keep theirs.
func TestInjectedTakesEqualConversions(t *testing.T) {
	mk := func(ts, text string) core.Message {
		return core.Message{TS: ts, Text: text, Doc: richtext.Doc{Blocks: []richtext.Block{{Spans: []richtext.Span{{Text: text}}}}}}
	}
	var in injState
	if !in.take([]core.Message{mk("1700000001.000100", "one"), mk("1700000002.000200", "two")}) {
		t.Fatal("the first messages changed nothing")
	}
	r1, r2 := in.rev("1700000001.000100"), in.rev("1700000002.000200")
	if r1 == r2 || r1 < injRevBase || r2 < injRevBase {
		t.Fatalf("revs %d, %d; want one each, from injRevBase", r1, r2)
	}
	again := []core.Message{mk("1700000001.000100", "one"), mk("1700000002.000200", "two")}
	if in.take(again) {
		t.Error("the same messages converted again changed the rows")
	}
	if len(in.msgs) != 2 || &in.msgs[0].Doc.Blocks[0] != &again[0].Doc.Blocks[0] || &in.msgs[1].Doc.Blocks[0] != &again[1].Doc.Blocks[0] {
		t.Error("the list kept the old copies: every later check compares them deep")
	}
	if in.rev("1700000001.000100") != r1 || in.rev("1700000002.000200") != r2 {
		t.Errorf("revs moved for messages that didn't change: %d, %d (were %d, %d)", in.rev("1700000001.000100"), in.rev("1700000002.000200"), r1, r2)
	}
	edited := []core.Message{again[0], mk("1700000002.000200", "two, renamed")}
	if !in.take(edited) {
		t.Error("a message converted again with a new name changed nothing")
	}
	if in.rev("1700000001.000100") != r1 || in.rev("1700000002.000200") == r2 {
		t.Errorf("revs %d, %d after the second message changed (were %d, %d); want only its rev new",
			in.rev("1700000001.000100"), in.rev("1700000002.000200"), r1, r2)
	}
}
