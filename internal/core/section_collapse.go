package core

// A sidebar section's collapsed state lives on the account, in the
// channel_sections user pref, as Slack's web client keeps it (build
// fa938a086569, gantry-v2-async-client-boot-render hJIH
// setChannelSectionCollapsed):
//
//   - the pref's object gets {<section id>: {c: "1"}} collapsed or "0"
//     expanded, other sections and keys ("sidebar", "sort") kept; the
//     local pref is updated at once (n3H2 rX), so the sidebar redraws;
//   - users.prefs.set name=channel_sections value=<the object as JSON>
//     (setUserPrefFetcher, nfg+ CI, with Yl7S Vc's JSON serialisation)
//     goes out 3 s after the last toggle (S6U4, a debounce cancelled by
//     each toggle), with reason
//     set_channel_section_collapse_in_home_sidebar;
//   - on teardown the pending write is sent at once
//     (flushPendingChannelSectionCollapsedSync, by sendBeacon).
//
// Another client's toggle comes back as pref_change, which reloads the
// prefs (events.go). A write still waiting keeps the local object, so an
// older server copy can't flip the section back meanwhile.

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

const prefChannelSections = "channel_sections"

// SectionCollapseDelay is hJIH's debounce before users.prefs.set.
var SectionCollapseDelay = 3 * time.Second

type sectionCollapseState struct {
	mu      sync.Mutex // the read-modify-write of the pref, the timer
	timer   *time.Timer
	pending atomic.Bool // a write waits (or is in flight)
}

// channelSectionsPref is the channel_sections pref's object (users.prefs.get
// sends it as JSON text in a string).
func (a *App) channelSectionsPref() map[string]map[string]any {
	cur, _ := a.Pref(prefChannelSections)
	var text string
	if json.Unmarshal(cur, &text) != nil {
		text = string(cur)
	}
	l := map[string]map[string]any{}
	_ = json.Unmarshal([]byte(text), &l)
	return l
}

// SetSectionCollapsed is setChannelSectionCollapsed for each id: the
// local pref changes now, the account 3 s after the last call.
func (a *App) SetSectionCollapsed(ids []string, collapsed bool) {
	if len(ids) == 0 {
		return
	}
	s := &a.secCol
	s.mu.Lock()
	l := a.channelSectionsPref()
	c := "0"
	if collapsed {
		c = "1"
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if l[id] == nil {
			l[id] = map[string]any{}
		}
		l[id]["c"] = c
	}
	b, err := json.Marshal(l)
	if err != nil {
		s.mu.Unlock()
		return
	}
	local, _ := json.Marshal(string(b))
	a.mergePrefs(map[string]json.RawMessage{prefChannelSections: local})
	s.pending.Store(true)
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(SectionCollapseDelay, a.FlushSectionCollapsed)
	s.mu.Unlock()
	p := a.Prefs()
	a.emit(Event{Type: "prefs", Prefs: &p})
}

// FlushSectionCollapsed sends a write still waiting in the debounce now
// (the window closing, tests).
func (a *App) FlushSectionCollapsed() {
	s := &a.secCol
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if !s.pending.Load() {
		s.mu.Unlock()
		return
	}
	b, err := json.Marshal(a.channelSectionsPref())
	s.mu.Unlock()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p := url.Values{
		"name":   {prefChannelSections},
		"value":  {string(b)},
		"reason": {"set_channel_section_collapse_in_home_sidebar"},
	}
	err = a.C.Call(ctx, "users.prefs.set", p, nil)
	s.mu.Lock()
	if s.timer == nil { // no newer toggle meanwhile
		s.pending.Store(false)
	}
	s.mu.Unlock()
	if err != nil {
		log.Printf("channel_sections: users.prefs.set: %v", err)
	}
}

// sectionCollapsePending keeps the local channel_sections over a reload
// (pref_change) while a write waits: the server's copy is older.
// installPrefs calls it (reloads only, not mergePrefs' local writes).
func (a *App) sectionCollapsePending(old, nw map[string]json.RawMessage) {
	if v, ok := old[prefChannelSections]; a.secCol.pending.Load() && ok && nw != nil {
		nw[prefChannelSections] = v
	}
}
