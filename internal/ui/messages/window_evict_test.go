package messages

// The list cache (stateIn) keeps 2*maxModels lists. Past that it let one
// go in map order and dropped that list's window from the hub. The ui
// root keeps its views (the conversation LRU), so the list it let go could
// be the one on screen, still drawing its window: then no reaction, edit,
// reply count or delete reached the window's rows, and core could let its
// copy of the window go. Slack's channelHistory slices stay while a pane
// shows one (client-boot 1qrw truncates per channel). Synthetic history
// on the fake.

import (
	"fmt"
	"testing"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// TestListCacheKeepsShownWindow: the cache lets the least recently drawn
// list go, never the one on screen, and only that list's window leaves
// the hub. 1200 new lists: map order picked the shown one sooner or later.
func TestListCacheKeepsShownWindow(t *testing.T) {
	app, _, _ := shotsApp(t)
	h := coreHost{app: app, th: theme.New(false)}
	saved, prevC, prevT := listCache, lastChannel, lastThread
	listCache = map[string]*listState{}
	t.Cleanup(func() { listCache, lastChannel, lastThread = saved, prevC, prevT })
	shown := stateIn(h, "C0SHOWN01", "", "")
	w := shown.hb.newWindow(shown.ch) // a jump's window; its load isn't needed here
	shown.win, shown.m = w, w
	lastChannel, lastThread = shown, nil
	for i := 0; i < 1200; i++ {
		stateIn(h, fmt.Sprintf("C0OTHER%04d", i), "", "")
	}
	if listCache["C0SHOWN01"] != shown {
		t.Error("the list cache let the list on screen go")
	}
	shown.hb.mu.Lock()
	held := shown.hb.wins[w]
	shown.hb.mu.Unlock()
	if !held {
		t.Error("the window of the list on screen left the hub: no live change reaches its rows")
	}
	if n := len(listCache); n > 2*maxModels+1 {
		t.Errorf("the list cache holds %d lists", n)
	}
}

// TestDrawnWindowTakenBack: a list whose window left the hub while a view
// still drew it (the cache's eviction) takes the window back when drawn:
// a reaction someone adds over the socket reaches the window's message.
func TestDrawnWindowTakenBack(t *testing.T) {
	e := newWinEnv(t)
	s := e.v.s
	s.hb.dropWindow(s.win) // what the cache's eviction did to the list
	e.rt.frame()           // its view draws it again
	target := e.tss[40]
	injectEvent(e.h.app, winEndChan, "reaction_added", "", map[string]any{"user": "UBEN00001", "reaction": "eyes",
		"item": map[string]any{"type": "message", "channel": winEndChan, "ts": target}})
	e.await(t, "someone's reaction on the window's message", func() bool {
		r := s.rows[target]
		return r != nil && len(r.m.Reactions) == 1
	})
	if !e.v.InWindow() {
		t.Errorf("the list left the window: %s", e.state())
	}
}
