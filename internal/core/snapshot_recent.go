package core

import (
	"sync"
	"sync/atomic"
	"time"
)

// recentSnap is the snapshot RecentSnapshot hands out until the sidebar's
// state changes.
type recentSnap struct {
	mu    sync.Mutex
	gen   uint64 // bumped by InvalidateSnapshot
	ok    bool
	built uint64    // gen snap was built at
	at    time.Time // when its build started
	snap  Snapshot

	builds atomic.Int64  // snapshotNow runs, for the tests and freeze reports
	seq    atomic.Uint64 // Snapshot.Seq's source
}

// recentSnapTTL bounds how stale RecentSnapshot gets on a change no event
// announces: a temporary membership running out, a store write a fetch
// made without one.
const recentSnapTTL = time.Second

// RecentSnapshot is Snapshot for readers on the UI goroutine that run
// every frame or keystroke (the Activity page's "+" menu and In filter,
// Unreads' section filter, search's completer). It returns the last one
// it built while no event that can change the sidebar came since
// (InvalidateSnapshot) and it is under a second old, else builds a new
// one. A full snapshot sorts and files every conversation, too much to
// redo per frame on a large workspace. Its Counts can lag a second
// behind (messages and marks don't drop it): read Counts or Count for
// live unread state. The result is shared: callers must not modify it.
// Unlike Snapshot it never waits for Boot: before Boot finished it is what
// core holds (a warm start's boot data, or nothing yet on a cold start),
// and Boot's "booted" event drops it.
func (a *App) RecentSnapshot() Snapshot {
	r := &a.recent
	r.mu.Lock()
	if r.ok && r.built == r.gen && time.Since(r.at) < recentSnapTTL {
		s := r.snap
		r.mu.Unlock()
		return s
	}
	gen := r.gen
	r.mu.Unlock()
	t0 := time.Now()
	s := a.snapshotNow()
	r.mu.Lock()
	if r.gen == gen { // nothing changed while it was built
		r.snap, r.ok, r.built, r.at = s, true, gen, t0
	}
	r.mu.Unlock()
	return s
}

// InvalidateSnapshot drops RecentSnapshot's snapshot: the next one is
// built fresh. Core calls it on every event that can change the sidebar;
// the ui calls it when an input core doesn't own changes (the huddle
// channels, a resnapshot).
func (a *App) InvalidateSnapshot() {
	a.recent.mu.Lock()
	a.recent.gen++
	a.recent.mu.Unlock()
}

// SnapshotBuilds counts the full snapshots built (Snapshot, SnapshotNow
// and RecentSnapshot's own).
func (a *App) SnapshotBuilds() int64 { return a.recent.builds.Load() }

// snapshotQuietEvents are the frequent events that change no part of a
// snapshot RecentSnapshot's readers use (sections, conversations, team,
// people): they keep its snapshot. Every other event drops it.
var snapshotQuietEvents = map[string]bool{
	"typing": true, "presence": true, "message": true, "update": true, "delete": true, "gone": true, "marked": true,
	"counts": true, "activity": true, "activity_badges": true, "assistant_status": true, "canvas_viewers": true,
	"sent": true, "pins_tab": true, "poll": true, "work_objects": true, "suggested_prompts": true,
	"list_record_info": true, "thread_subscription": true, "bookmarks": true, "tabs": true,
}
