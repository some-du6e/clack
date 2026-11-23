package taut

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// TestReactionsOfFetchedMessages: a message the store holds only as
// fetched by id (Activity's and Saved's hydration, the Threads view's
// getView: store.PutFetched) has its reactions in the reactions slice, as
// Slack's one messages store has them, and keys and get agree on every
// key: keys("reactions") lists the message through VisitMessages, so get
// must find it too (Store.Find leaves fetched messages out; FindHeld
// doesn't).
func TestReactionsOfFetchedMessages(t *testing.T) {
	h := newHarness(t)
	m := h.tt.mirror
	const ch, ts = "C0FETCHONLY", "1700000400.000100"
	if _, ok := h.app.S.Find(ch, ts); ok {
		t.Fatalf("fixture: %s/%s is already held", ch, ts)
	}
	h.app.S.PutFetched(ch, []slack.Message{{Type: "message", TS: ts, User: "UALICE", Text: "from Activity",
		Reactions: []slack.Reaction{{Name: "tada", Count: 2, Users: []string{"UALICE", "UBOB"}}}}}, h.app.S.LiveSeq())
	if _, ok := h.app.S.Find(ch, ts); ok {
		t.Fatalf("fixture: Find holds the fetched-only %s/%s, so this test proves nothing", ch, ts)
	}
	key := "message-" + ts + "-" + ch
	listed := false
	for _, k := range m.keys("reactions") {
		if k == key {
			listed = true
		}
		if m.get("reactions", k) == nil {
			t.Errorf("keys(\"reactions\") lists %s, but get(\"reactions\", %q) is undefined", k, k)
		}
	}
	if !listed {
		t.Fatalf("keys(\"reactions\") doesn't list the fetched message's %s", key)
	}
	rs, ok := m.get("reactions", key).([]any)
	if !ok || len(rs) != 1 {
		t.Fatalf("get(\"reactions\", %q) = %#v, want the message's one reaction", key, m.get("reactions", key))
	}
	r, _ := rs[0].(map[string]any)
	if r["name"] != "tada" {
		t.Errorf("the reaction is %#v, want tada", r)
	}
}
