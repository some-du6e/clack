package messages

// The lists' side of a history rewritten on Slack's side
// (hub_history_changed.go): a load out across the change doesn't bring
// what went back, and an open inline edit survives the purge. Core's
// side is played here as core plays it (store TruncateHistoryBefore and
// DropHistory, then the events). Synthetic history on the fake.

import (
	"strings"
	"sync"
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

// Two loads out across one history change, landing one after the other:
// a reload of the newest page and the older page the list reads ahead of
// the reader (olderAhead), both read before the change. Each answer may
// be from before it, so each landing cuts again. The model kept one cut:
// the older page landed first and took it, and the reload's newest page,
// read before the change, brought back the messages the change deleted
// (TestHistoryChangeAcrossALoad on history-limit's full-test 45551097510,
// once pages of 28 had the list read the older page with the reload out).
// Each load is held on its own gate as its answer is in (loadLanded); no
// frame runs while they are out, so the list starts no load of its own.
func TestHistoryChangeAcrossTwoLoads(t *testing.T) {
	const ch = "C0HCRACE02"
	e, fs := guardEnv(t, ch, nil, nil)
	m := e.v.s.m
	state := func() (loading, older, hasMore bool, n int) {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.loading, m.older, m.hasMore, len(m.msgs)
	}
	e.await(t, "no load out", func() bool {
		loading, older, hasMore, n := state()
		return !loading && !older && hasMore && n > 0
	})
	loaded := e.v.LoadedTS()
	n := len(loaded)
	if n < 22 {
		t.Fatalf("control: the list holds %d messages", n)
	}
	gone, latest := append([]string(nil), loaded[n-20:n-10]...), loaded[n-10] // in the newest page of 28

	arrived := make(chan struct{}, 2)
	gates := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
	var once [2]sync.Once
	open := func(i int) { once[i].Do(func() { close(gates[i]) }) }
	defer open(0)
	defer open(1)
	var mu sync.Mutex
	k := 0
	hook := func(lm *model) {
		if lm != m {
			return
		}
		mu.Lock()
		i := k
		k++
		mu.Unlock()
		if i < len(gates) {
			arrived <- struct{}{}
			<-gates[i]
		}
	}
	loadLanded.Store(&hook)
	t.Cleanup(func() { loadLanded.Store(nil) })
	in := func(what string) {
		t.Helper()
		select {
		case <-arrived:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: its answer isn't in after 10 s", what)
		}
	}
	until := func(what string, cond func() bool) {
		t.Helper()
		for dl := time.Now().Add(10 * time.Second); !cond(); time.Sleep(2 * time.Millisecond) {
			if time.Now().After(dl) {
				t.Fatalf("%s: not after 10 s", what)
			}
		}
	}

	hb := e.v.s.hb
	hb.refresh(m) // gate 0: core's newest page as it is now
	in("the reload")
	if loading, older, _, _ := state(); !loading || older {
		t.Fatalf("control: held first is not the reload alone (loading %v, older %v)", loading, older)
	}
	if !hb.loadOlder(m) { // gate 1: the page before the list's oldest
		t.Fatal("control: the older page didn't go out")
	}
	in("the older page")

	fs.DeleteQuiet(ch, gone...)
	app := e.h.app
	for _, g := range app.S.TruncateHistoryBefore(ch, latest, nil) {
		ts := g.Oldest
		if g.Whole {
			ts = ""
		}
		hb.apply(core.Event{Type: "evicted", Channel: g.Channel, ThreadTS: g.Thread, TS: ts})
	}
	hb.apply(core.Event{Type: "history_changed", Channel: ch, TS: latest})

	open(1)
	until("the older page landed", func() bool { _, older, _, _ := state(); return !older })
	open(0)
	until("the reload landed", func() bool { loading, _, _, _ := state(); return !loading })
	ghosts := func(tss []string) []string {
		var out []string
		for _, ts := range tss {
			if hcHas(gone, ts) {
				out = append(out, ts)
			}
		}
		return out
	}
	m.mu.Lock()
	var held []string
	for _, x := range m.msgs {
		held = append(held, x.m.TS)
	}
	m.mu.Unlock()
	if g := ghosts(held); len(g) > 0 {
		t.Errorf("the reload landing after the older page brought back %d messages from before the change (%v)", len(g), g)
	}
	for k := 0; k < 5; k++ {
		e.rt.frame()
	}
	if g := ghosts(e.v.LoadedTS()); len(g) > 0 {
		t.Errorf("the list shows %d messages from before the change (%v)", len(g), g)
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
