package core

import (
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

// recentSnap is the snapshot RecentSnapshot hands out: the newest full
// snapshot any build made, with whether nothing its readers use changed
// since.
type recentSnap struct {
	mu    sync.Mutex
	gen   uint64 // bumped by InvalidateSnapshot
	ok    bool
	built uint64 // gen snap's build started at; snap is fresh while it is gen
	snap  Snapshot
	// what snap's readers read that a frequent event names (snapshotQuiet):
	// the people it holds (its Users, and the members its group DMs'
	// names and order come from), its conversations and its sections'
	// custom emoji
	people, convs, emoji map[string]bool
	// building counts the full builds under way (any goroutine): while one
	// runs, an event it may have read past drops what it makes
	building int
	// the background rebuild RecentSnapshot asks for: one at a time, at
	// most one start every recentRebuildGap, and wake (SetSnapshotWake)
	// when it lands
	running bool
	last    time.Time
	wake    func()

	builds atomic.Int64  // snapshotNow runs, for the tests and freeze reports
	seq    atomic.Uint64 // Snapshot.Seq's source
}

// recentRebuildGap spaces RecentSnapshot's background rebuilds: under a
// burst of changes its readers see one at most this much behind (each
// build of a large workspace's sidebar is ~10 ms and ~5 MB of garbage).
var recentRebuildGap = 500 * time.Millisecond

// RecentSnapshot is Snapshot for readers on the UI goroutine that run
// every frame or keystroke (the Activity page's "+" menu and In filter,
// Unreads' section filter, search's completer, the composer's
// autocomplete). It never builds one on its caller: it returns the newest
// snapshot any build made (Snapshot, SnapshotNow, a warm start's, its
// own), and when an event that can change what its readers use came since
// (InvalidateSnapshot) it starts one build in the background, which wakes
// the window when it lands (SetSnapshotWake), as a selector's new value
// re-renders Slack's components. A full snapshot sorts and files every
// conversation, ~10 ms on a large workspace: on the UI goroutine every
// change dropped a frame. Only the very first read, before any snapshot
// exists (a cold start, while the store is still empty), builds on its
// caller. Its Counts can lag any time behind (messages and marks keep it):
// read Counts or Count for live unread state, and OwnTeam for the team.
// The result is shared: callers must not modify it. Unlike Snapshot it
// never waits for Boot: before Boot finished it is what core holds (a
// warm start's boot data, or nothing yet on a cold start), and Boot's
// "booted" event marks it stale.
func (a *App) RecentSnapshot() Snapshot {
	r := &a.recent
	r.mu.Lock()
	if !r.ok {
		r.mu.Unlock()
		return a.snapshotNow() // the first one; snapshotNow offers it to the cache
	}
	s := r.snap
	if r.built != r.gen && !r.running {
		r.running = true
		wait := recentRebuildGap - time.Since(r.last)
		go a.rebuildRecent(wait)
	}
	r.mu.Unlock()
	return s
}

// rebuildRecent is RecentSnapshot's background build: after wait (the
// gap since the last one), one snapshot, offered to the cache, and the
// window woken to draw it.
func (a *App) rebuildRecent(wait time.Duration) {
	if wait > 0 {
		select {
		case <-time.After(wait):
		case <-a.bg.Done():
		}
	}
	r := &a.recent
	r.mu.Lock()
	r.last = time.Now()
	fresh := r.ok && r.built == r.gen // another build landed meanwhile (a resnapshot's)
	wake := r.wake
	r.mu.Unlock()
	if !fresh && a.bg.Err() == nil {
		a.snapshotNow()
	}
	r.mu.Lock()
	r.running = false
	r.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// endBuild ends a full build (beginBuild's): its snapshot goes to
// RecentSnapshot's cache unless that holds a newer one. Started at gen
// (InvalidateSnapshot's), it is fresh while nothing that can change it
// came since. The build ends and its snapshot lands under one lock, so
// an event in between is judged with what the new one holds.
func (a *App) endBuild(s Snapshot, gen uint64) {
	people := make(map[string]bool, len(s.Users)+1)
	convs := make(map[string]bool, len(s.Convs))
	if s.Self.ID != "" {
		people[s.Self.ID] = true
	}
	for id := range s.Users {
		people[id] = true
	}
	for i := range s.Convs {
		c := &s.Convs[i]
		convs[c.ID] = true
		if c.Kind == "mpim" { // its name and its place in the sort are its members'
			for _, m := range c.Members {
				people[m] = true
			}
		}
	}
	emoji := map[string]bool{}
	for _, sec := range s.Sections {
		if sec.Emoji != "" {
			emoji[sec.Emoji] = true
		}
	}
	r := &a.recent
	r.mu.Lock()
	defer r.mu.Unlock()
	r.building--
	if r.ok && s.Seq < r.snap.Seq {
		return // a later build landed first
	}
	r.snap, r.ok, r.built = s, true, gen
	r.people, r.convs, r.emoji = people, convs, emoji
}

// InvalidateSnapshot marks RecentSnapshot's snapshot stale: the next read
// starts a new build. Core calls it on every event that can change what
// its readers use; the ui calls it when an input core doesn't own
// changes (the huddle channels, a resnapshot).
func (a *App) InvalidateSnapshot() {
	a.recent.mu.Lock()
	a.recent.gen++
	a.recent.mu.Unlock()
}

// SetSnapshotWake sets what RecentSnapshot calls when a build it started
// lands (the window's Invalidate: the readers that saw the old one draw
// the new one). nil: nothing.
func (a *App) SetSnapshotWake(f func()) {
	a.recent.mu.Lock()
	a.recent.wake = f
	a.recent.mu.Unlock()
}

// SnapshotBuilds counts the full snapshots built (Snapshot, SnapshotNow
// and RecentSnapshot's own).
func (a *App) SnapshotBuilds() int64 { return a.recent.builds.Load() }

// snapshotBuildHook, when set, runs at the start of every full snapshot
// build, on the goroutine that builds it (SetSnapshotBuildHook).
var snapshotBuildHook atomic.Pointer[func()]

// SetSnapshotBuildHook makes f run at the start of every full snapshot
// build, on the building goroutine (tests: no frame on the UI goroutine
// builds one). nil removes it.
func SetSnapshotBuildHook(f func()) {
	if f == nil {
		snapshotBuildHook.Store(nil)
		return
	}
	snapshotBuildHook.Store(&f)
}

// beginBuild notes a full build starting and returns the generation it
// starts at.
func (a *App) beginBuild() uint64 {
	if h := snapshotBuildHook.Load(); h != nil {
		(*h)()
	}
	r := &a.recent
	r.mu.Lock()
	defer r.mu.Unlock()
	r.building++
	return r.gen
}

// OwnTeam is the viewer's team as the store holds it now (its name, icon
// and domain): what a snapshot's Team would say, without building one.
func (a *App) OwnTeam() Team {
	a.S.RLock()
	defer a.S.RUnlock()
	return Team{ID: a.S.TeamID, Name: a.S.TeamName, Icon: a.S.TeamIcon, Domain: a.S.TeamDomain}
}

// snapshotQuietEvents are the frequent events that change no part of a
// snapshot RecentSnapshot's readers use (sections, conversations, team,
// people, prefs): they keep its snapshot. The raw passthroughs are a
// thread's read state, DND, badge counts, a pref change (its "prefs"
// event follows) and the section writes (their "sidebar_changed"
// follows); "huddle": the huddle channels the sidebar rules read are the
// ui's, which drops the snapshot itself when they change (sbHuddleSync).
// "user", "names", "emoji_changed" and "conv" keep it when they name
// nothing it holds (snapshotQuiet). Every other event drops it.
var snapshotQuietEvents = map[string]bool{
	"typing": true, "presence": true, "message": true, "update": true, "delete": true, "marked": true,
	"counts": true, "activity": true, "activity_badges": true, "assistant_status": true, "canvas_viewers": true,
	"sent": true, "pins_tab": true, "poll": true, "work_objects": true, "suggested_prompts": true,
	"list_record_info": true, "thread_subscription": true, "bookmarks": true, "tabs": true,
	"huddle": true, "thread_marked": true, "update_thread_state": true, "dnd_updated_user": true,
	"badge_counts_updated": true, "pref_change": true, "channel_sections_upserted": true,
	"channel_sections_channels_upserted": true, "channel_sections_channels_removed": true,
	"channel_sections_deleted": true, "desktop_notification": true, "draft": true, "saved": true,
	"client_online": true, "permissions": true, "megaphone": true, "member_count": true, "list": true,
	"canvas": true, "channel_email": true, "file_trash": true,
}

// snapshotQuiet reports whether ev leaves RecentSnapshot's snapshot as it
// is (emit; any goroutine). A busy workspace's socket brings profile
// changes, lookups, new custom emoji and other channels' changes many a
// second; only those about what the snapshot holds can change it, the
// way Slack's selectors return the same value then. While a build is
// under way any of them may have landed after it read the store, so none
// keeps it.
func (a *App) snapshotQuiet(ev Event) bool {
	if snapshotQuietEvents[ev.Type] {
		return true
	}
	switch ev.Type {
	case "user", "names", "emoji_changed", "conv":
	default:
		return false
	}
	r := &a.recent
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.ok || r.building > 0 {
		return false
	}
	switch ev.Type {
	case "user":
		id := ev.UserID
		if id == "" && ev.User != nil {
			id = ev.User.ID
		}
		return id != "" && !r.people[id]
	case "names":
		if len(ev.Users) == 0 {
			return false // the user groups' names: re-read everything
		}
		for _, id := range ev.Users {
			if r.people[id] || r.convs[id] {
				return false
			}
		}
		return true
	case "emoji_changed":
		return ev.Name != "" && !r.emoji[ev.Name]
	case "conv":
		// a channel the viewer isn't in, looked up (channel_rename,
		// channel_created: every member of the team gets them), stays out
		// of the snapshot unless it is the open preview's row, which the
		// snapshot then holds (selectedExt; opening one drops it with
		// "sidebar_changed"). One that joins the member set isn't a
		// Preview. The store isn't read here: emit runs under its callers'
		// locks.
		return ev.Channel != "" && ev.Conv != nil && ev.Conv.Preview && !r.convs[ev.Channel]
	}
	return false
}

// CardOnly reports whether a profile change from prev to u changes only
// what a profile card shows: the status text, title, pronouns, email,
// time zone, the first and last names the autocomplete matches, the
// roles, presence (presence_change moves the dots). A message row's
// sender, a mention, an Activity row and a sidebar row draw none of them,
// so a busy workspace's status changes redraw no list (Slack's userChange
// only drops the member's cache entry; a component re-renders when the
// fields it selects change). False when prev is nil (the store didn't
// hold the member).
func CardOnly(prev, u *User) bool {
	if prev == nil || u == nil {
		return false
	}
	a, b := *prev, *u
	for _, x := range []*User{&a, &b} {
		x.StatusText, x.StatusCanonical, x.Title, x.Pronouns, x.Email = "", "", "", "", ""
		x.TZ, x.TZLabel, x.TZOffset = "", "", 0
		x.FirstName, x.LastName = "", ""
		x.IsAdmin, x.IsOwner, x.EnterpriseAdmin = false, false, false
		x.Presence = ""
	}
	return reflect.DeepEqual(a, b)
}
