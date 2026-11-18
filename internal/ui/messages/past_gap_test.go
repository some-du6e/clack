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

// The first message past the gap of a window behind a slow connect
// (core.Event.Epoch with GapView) switches the list to the slice past the
// gap: the pre-gap messages go, the viewer's local copy past them stays.
// And a newest page a load read before that switch is the view from
// before it: it isn't taken, and the list reads again. Taken, it put the
// pre-gap messages back above the post-gap one whenever the load landed
// after the message (TestNoHoleWhileCatchingUp, red in ci on 8623f0b0 and
// be67dbd2).
func TestPastGapSwitchOutlivesAStaleLoad(t *testing.T) {
	m := &model{ch: "C1", typing: map[string]typingEntry{}}
	pre := core.Page{Messages: []core.Message{{TS: "1700000001.000100"}, {TS: "1700000002.000100"}}, HasMore: true, Epoch: 5}
	if !m.landNewest(nil, pre, nil, m.pastGap) {
		t.Fatal("the first page wasn't taken")
	}
	m.upsert(core.Message{TS: "1700000003.000100", Pending: true}) // a send after the wake
	gap0 := m.pastGap                                              // a reload starts (the connection event's)
	// the socket's first message past the gap (hub.apply)
	m.startPastGap(5 | core.GapView)
	m.upsert(core.Message{TS: "1700000009.000100"})
	m.noteLive("1700000009.000100", false)
	if got, want := modelTS(m), "1700000003.000100 1700000009.000100"; got != want {
		t.Fatalf("after the switch the list holds %q, want %q", got, want)
	}
	if !m.hasMore || m.epoch != 5|core.GapView {
		t.Errorf("hasMore %v, epoch %x: the slice past the gap loads back from its start", m.hasMore, m.epoch)
	}
	// the reload lands with the view from before the switch
	if m.landNewest(nil, pre, nil, gap0) {
		t.Error("a page read before the switch was taken")
	}
	if got, want := modelTS(m), "1700000003.000100 1700000009.000100"; got != want {
		t.Errorf("after the stale page the list holds %q, want %q: the pre-gap messages came back", got, want)
	}
	if !m.fetched.IsZero() {
		t.Error("the list doesn't read again after dropping the stale page")
	}
	// read again: core's page past the gap, the same view
	past := core.Page{Messages: []core.Message{{TS: "1700000003.000100", Pending: true}, {TS: "1700000009.000100"}}, HasMore: true, Epoch: 5 | core.GapView}
	if !m.landNewest(nil, past, nil, m.pastGap) {
		t.Fatal("the page past the gap wasn't taken")
	}
	if got, want := modelTS(m), "1700000003.000100 1700000009.000100"; got != want {
		t.Errorf("the page past the gap left %q, want %q", got, want)
	}
	// the catch-up lands: the window whole again
	whole := core.Page{Messages: []core.Message{{TS: "1700000001.000100"}, {TS: "1700000002.000100"}, {TS: "1700000005.000100"}, {TS: "1700000009.000100"}}, HasMore: true, Epoch: 5}
	if !m.landNewest(nil, whole, nil, m.pastGap) {
		t.Fatal("the caught-up window wasn't taken")
	}
	if got := modelTS(m); !strings.Contains(got, "1700000005.000100") || !strings.HasPrefix(got, "1700000001.000100") {
		t.Errorf("after the catch-up the list holds %q, want the whole window", got)
	}
}
