package core

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// The message change log (msg_changes.go) holds the "update" and "delete"
// events in the order emit sent them, and says so when a reader fell
// further behind than it keeps.
func TestMessageChangesInOrder(t *testing.T) {
	a := New(slack.New(slack.Creds{Token: "xoxc-test"}), store.New(""))
	start := a.MessageChangeSeq()
	a.emit(Event{Type: "update", Channel: "C1", TS: "1.000001", Msg: &Message{TS: "1.000001", Text: "a"}})
	a.emit(Event{Type: "message", Channel: "C1", TS: "2.000001"}) // a new message isn't a change
	a.emit(Event{Type: "delete", Channel: "C1", TS: "1.000001"})
	evs, now, ok := a.MessageChanges(start)
	if !ok || now != start+2 || len(evs) != 2 || evs[0].Type != "update" || evs[0].Msg.Text != "a" || evs[1].Type != "delete" {
		t.Fatalf("MessageChanges(%d) = %d events %+v, now %d, ok %v", start, len(evs), evs, now, ok)
	}
	if evs, _, ok := a.MessageChanges(now); !ok || len(evs) != 0 {
		t.Errorf("nothing changed since %d: %d events, ok %v", now, len(evs), ok)
	}
	for i := 0; i <= maxMsgChanges; i++ {
		a.emit(Event{Type: "update", Channel: "C1", TS: "3.000001", Msg: &Message{TS: "3.000001"}})
	}
	if _, _, ok := a.MessageChanges(now); ok {
		t.Error("a reader more than the log behind was told it had everything")
	}
}

// A reaction to a message the store holds only as fetched by id (an
// Activity entry's, from a conversation nobody opened) changes that copy
// and goes out as an "update", as Slack's reactions reducer reaches every
// message of its store (store/fetched.go).
func TestReactionReachesFetchedMessage(t *testing.T) {
	a := New(slack.New(slack.Creds{Token: "xoxc-test"}), store.New(""))
	const ch, ts = "C0LIVE0001", "1762790200.000100"
	a.S.PutFetched(ch, []slack.Message{{Type: "message", TS: ts, User: "U2", Text: "a synthetic mention"}}, a.S.LiveSeq())
	start := a.MessageChangeSeq()
	raw, _ := json.Marshal(map[string]any{"type": "reaction_added", "user": "U1", "reaction": "tada",
		"item": map[string]any{"type": "message", "channel": ch, "ts": ts}})
	a.Inject(slack.Event{Type: "reaction_added", Raw: raw})
	evs, _, _ := a.MessageChanges(start)
	if len(evs) != 1 || evs[0].Type != "update" || evs[0].Name != UpdateReactions || len(evs[0].Msg.Reactions) != 1 {
		t.Fatalf("the reaction emitted %+v; want one reactions update", evs)
	}
	if m, ok := a.S.FindHeld(ch, ts); !ok || len(m.Reactions) != 1 || m.Reactions[0].Name != "tada" {
		t.Errorf("the fetched copy: %v %+v", ok, m.Reactions)
	}
}
