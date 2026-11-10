package messages

import (
	"testing"

	"github.com/some-du6e/clack/internal/core"
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
