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

// A newest page of another window (core started the window over at a
// page that didn't meet it: Page.Epoch) is taken whole: Slack keeps such
// a page a slice of its own and its newest view draws only that slice.
// Glued to the list's older messages it drew as one list with the
// messages between left out. The list can't tell from the messages: its
// own confirmed send past the old end (from "sent"), or an update it took
// in there, is in the newest page too. A page of the same window keeps
// what the list loaded before it.
func TestNewestPageOfAnotherWindowIsTakenWhole(t *testing.T) {
	msgs := func(ts ...string) []core.Message {
		var out []core.Message
		for _, x := range ts {
			out = append(out, core.Message{TS: x})
		}
		return out
	}
	load := func(epoch uint64, ts ...string) *model {
		m := &model{ch: "C1", typing: map[string]typingEntry{}}
		m.takeNewest(core.Page{Messages: msgs(ts...), HasMore: true, Epoch: epoch})
		m.loaded = true
		return m
	}

	m := load(1, "1700000001.000100", "1700000002.000100", "1700000003.000100")
	m.takeNewest(core.Page{Messages: msgs("1700000003.000100", "1700000004.000100"), HasMore: true, Epoch: 1})
	if got, want := modelTS(m), "1700000001.000100 1700000002.000100 1700000003.000100 1700000004.000100"; got != want {
		t.Errorf("a page of the same window: %q, want %q", got, want)
	}

	m = load(1, "1700000001.000100", "1700000002.000100", "1700000003.000100")
	m.takeNewest(core.Page{Messages: msgs("1700000090.000100", "1700000091.000100"), HasMore: true, Epoch: 2})
	if got, want := modelTS(m), "1700000090.000100 1700000091.000100"; got != want {
		t.Errorf("a page of a new window: %q, want %q (the page alone)", got, want)
	}
	if !m.hasMore || m.epoch != 2 {
		t.Errorf("hasMore %v epoch %d: the list pages back from the new window's start", m.hasMore, m.epoch)
	}

	// the viewer's send, confirmed past the old end, is in the new window
	m = load(1, "1700000001.000100", "1700000002.000100")
	m.upsert(core.Message{TS: "1700000095.000100", ClientMsgID: "cid-1"}, 0)
	m.takeNewest(core.Page{Messages: msgs("1700000090.000100", "1700000095.000100"), HasMore: true, Epoch: 2})
	if got, want := modelTS(m), "1700000090.000100 1700000095.000100"; got != want {
		t.Errorf("a new window holding the list's send: %q, want %q", got, want)
	}
}
