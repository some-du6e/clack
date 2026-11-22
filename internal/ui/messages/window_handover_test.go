package messages

import (
	"errors"
	"testing"
	"time"
)

// TestHandOverWaitsForReadAfterJoin: a window that met the newest
// messages goes over to the conversation's model only once a read of it
// begun after the join has landed (Slack's union is in the very state the
// pane reads): not on a read already out when the window joined (it began
// before the join, so it lacks the window), and not on a failed read (it
// holds nothing). The list kept the window meanwhile. Clack took the
// first read to land, or the model's error, and could swap a loaded
// window for an error or the newest page. The reads here are played by
// the test (no request goes out: the hub's loads are off).
func TestHandOverWaitsForReadAfterJoin(t *testing.T) {
	hb := &hub{}
	hb.authFailed.Store(true) // ensureLoaded starts nothing: the test plays the reads
	chm := &model{ch: "C1", loaded: true, loading: true, readGen: 3, readOK: 2, fetched: time.Now()}
	win := &model{ch: "C1", win: true, merged: true, loaded: true}
	s := &listState{hb: hb, ch: "C1", chm: chm, m: win, win: win}
	step := func(what string, f func()) {
		chm.mu.Lock()
		f()
		chm.mu.Unlock()
		if s.handOver() || s.win == nil {
			t.Fatalf("%s: the window went over to the conversation's model", what)
		}
	}
	step("a read out when the window joined", func() {})
	step("that read landed", func() { chm.loading, chm.readOK = false, 3 })
	step("the read after the join is out", func() { chm.readGen, chm.loading = 4, true })
	step("it failed", func() { chm.loading, chm.err = false, errors.New("synthetic failure") })
	chm.mu.Lock()
	chm.readGen, chm.readOK, chm.err = 5, 5, nil // a later read landed
	chm.mu.Unlock()
	if !s.handOver() || s.win != nil || s.m != chm {
		t.Fatalf("a read begun after the join landed: the list still shows the window")
	}
}
