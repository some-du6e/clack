package messages

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

func modelTS(m *model) string {
	var ts []string
	for _, e := range m.msgs {
		ts = append(ts, e.m.TS)
	}
	return strings.Join(ts, " ")
}

// A newest page that starts after everything the list holds, with more
// history before it, starts the list over: Slack keeps such a page a
// slice of its own (om2Z insert: the two don't intersect) and its newest
// view draws only the newest slice. Glued to the list's older messages it
// drew as one list with the messages between left out (a poll after more
// than a page arrived while the socket was down, a catch-up starting the
// window over). A page that reaches the list keeps what the list loaded
// before it; one holding the whole history (no hasMore) joins too, and
// the viewer's unsent copy is no part of the list's end.
func TestNewestPageThatMissesTheListStartsItOver(t *testing.T) {
	msgs := func(ts ...string) []core.Message {
		var out []core.Message
		for _, x := range ts {
			out = append(out, core.Message{TS: x})
		}
		return out
	}
	load := func(ts ...string) *model {
		m := &model{ch: "C1", typing: map[string]typingEntry{}}
		m.mergeNewest(msgs(ts...), true)
		m.loaded = true
		return m
	}

	m := load("1700000001.000100", "1700000002.000100", "1700000003.000100")
	m.mergeNewest(msgs("1700000090.000100", "1700000091.000100"), true)
	if got, want := modelTS(m), "1700000090.000100 1700000091.000100"; got != want {
		t.Errorf("a newest page past the list's end: the list holds %q, want %q (the page alone)", got, want)
	}
	if !m.hasMore {
		t.Error("the list started over doesn't page back from the page's start")
	}

	m = load("1700000001.000100", "1700000002.000100", "1700000003.000100")
	m.mergeNewest(msgs("1700000003.000100", "1700000004.000100"), true)
	if got, want := modelTS(m), "1700000001.000100 1700000002.000100 1700000003.000100 1700000004.000100"; got != want {
		t.Errorf("a newest page reaching the list: %q, want %q", got, want)
	}

	m = load("1700000001.000100", "1700000002.000100")
	m.mergeNewest(msgs("1700000090.000100"), false)
	if got, want := modelTS(m), "1700000001.000100 1700000002.000100 1700000090.000100"; got != want {
		t.Errorf("a newest page holding the whole history: %q, want %q", got, want)
	}

	// the list's newest is the viewer's pending send (its local ts after
	// the page's): the newest message from Slack is the one before it
	m = load("1700000001.000100", "1700000002.000100")
	m.upsert(core.Message{TS: "1700000099.000100", Pending: true}, 0)
	m.mergeNewest(msgs("1700000090.000100", "1700000091.000100"), true)
	if strings.Contains(modelTS(m), "1700000001.000100") {
		t.Errorf("a page past the list's end behind a pending send: %q, the old messages kept", modelTS(m))
	}
}
