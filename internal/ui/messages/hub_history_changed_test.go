package messages

// The lists' side of a history rewritten on Slack's side
// (hub_history_changed.go): a load out across the change doesn't bring
// what went back, and an open inline edit survives the purge. Core's
// side is played here as core plays it (store TruncateHistoryBefore and
// DropHistory, then the events). Synthetic history on the fake.

import (
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// hcLoading reports whether the list's model has a load out.
func (e *winEnv) hcLoading() bool {
	m := e.v.s.m
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loading
}

func hcHas(tss []string, ts string) bool {
	for _, x := range tss {
		if x == ts {
			return true
		}
	}
	return false
}

// A newest-page load that read core's cached history before the change
// and lands after it: the messages before the change don't come back
// (mergeNewest kept everything older than the page's first for good).
func TestHistoryChangeAcrossALoad(t *testing.T) {
	const ch = "C0HCRACE01"
	e, fs := guardEnv(t, ch, nil, nil)
	loaded := e.v.LoadedTS()
	n := len(loaded)
	if n < 22 {
		t.Fatalf("control: the list holds %d messages", n)
	}
	gone, latest := append([]string(nil), loaded[n-20:n-10]...), loaded[n-10] // in the newest page of 28
	release := HoldLoad(ch, "")
	defer release()
	e.v.s.hb.refresh(e.v.s.m) // a reload out: its answer is core's history as it is now
	e.await(t, "the reload out", func() bool { return e.hcLoading() })
	time.Sleep(100 * time.Millisecond) // its answer read (HoldLoad holds it once it's in)
	fs.DeleteQuiet(ch, gone...)
	app := e.h.app
	for _, g := range app.S.TruncateHistoryBefore(ch, latest, nil) {
		ts := g.Oldest
		if g.Whole {
			ts = ""
		}
		e.v.s.hb.apply(core.Event{Type: "evicted", Channel: g.Channel, ThreadTS: g.Thread, TS: ts})
	}
	e.v.s.hb.apply(core.Event{Type: "history_changed", Channel: ch, TS: latest})
	release()
	e.await(t, "the reload landed", func() bool { return !e.hcLoading() })
	for k := 0; k < 5; k++ {
		e.rt.frame()
	}
	var ghosts []string
	for _, ts := range e.v.LoadedTS() {
		if hcHas(gone, ts) {
			ghosts = append(ghosts, ts)
		}
	}
	if len(ghosts) > 0 {
		t.Errorf("the load out across the change brought back %d messages from before it (%v)", len(ghosts), ghosts)
	}
	if !hcHas(e.v.LoadedTS(), loaded[n-1]) {
		t.Errorf("the newest message went with the history before the change")
	}
}

// An inline edit open with text typed when core purges the conversation
// (counts' newer history_invalid): its row comes back with the newest
// page, and the editor with it, the text kept (keepEdit).
func TestHistoryPurgeKeepsTheInlineEdit(t *testing.T) {
	const ch = "C0HCEDIT01"
	e, _ := guardEnv(t, ch, func(i int) bool { return i == 235 }, nil)
	mine := e.tss[235]
	e.v.Edit(mine)
	e.await(t, "the editor", func() bool {
		ed := e.v.EditComposer()
		return ed != nil && strings.Contains(ed.DocText(), "note 235")
	})
	ed := e.v.EditComposer()
	ed.InsertText(" and more")
	want := ed.DocText()
	app := e.h.app
	for _, g := range app.S.DropHistory(ch, nil) {
		e.v.s.hb.apply(core.Event{Type: "evicted", Channel: g.Channel, ThreadTS: g.Thread})
	}
	e.v.s.hb.apply(core.Event{Type: "history_changed", Channel: ch})
	e.await(t, "the newest page again, with the edited message", func() bool { return e.drawn(e.newest()) && hcHas(e.v.LoadedTS(), mine) })
	e.frames(200 * time.Millisecond)
	got := e.v.EditComposer()
	if got == nil {
		t.Fatalf("after the purge the inline edit is gone: %s", e.state())
	}
	if got.DocText() != want {
		t.Errorf("after the purge the editor holds %q, want %q", got.DocText(), want)
	}
}
