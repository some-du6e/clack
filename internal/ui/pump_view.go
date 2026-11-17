package ui

import (
	"sync"

	"github.com/some-du6e/clack/internal/core"
)

// The shell's event pump (backend.start) draws a frame for every core
// event it posts, and every frame redraws the whole window. A busy
// workspace's socket brings many events a second that change nothing on
// screen: a message in a channel that is already unread, a profile change
// that leaves a DM row's name, picture and status emoji as they are,
// presence that didn't move, badge counts that stayed, a lookup of a
// channel the viewer isn't in. Slack's store takes each of them and its
// components re-render only when what they select changed (a sidebar
// row's channelIsUnread and badgeCount props; userChange only invalidates
// another member's cache entry; handleBadgeCountsUpdatedRTMEvent sets the
// counts), so Chromium paints nothing. pumpView is what the pump compares
// an event with to tell (wants): the drawn fields of the model, which the
// UI goroutine copies here as it changes them.
type pumpView struct {
	mu     sync.Mutex
	ready  bool                    // a model was applied
	counts map[string]drawnCount   // the model's conversations
	people map[string]*drawnPerson // the viewer and the model's DM people
	ext    map[string]bool         // conversations resolved outside the model (backend.ext), and their DM partners
	// faces are the members the model's channels draw (their headers'
	// facepiles: convInfo.Members): a lookup ("names") of one redraws
	faces map[string]bool
	inbox bool // the rail's Activity badge reads activity_v2 (Rail.ActivityInbox)
	badge int  // the rail's Activity badge (model.ActivityCount)
}

// drawnCount is what the shell draws of a conversation's unread state
// (setCount): Latest isn't drawn (markRead reads core's).
type drawnCount struct {
	unread   bool
	mentions int
	lastRead string
}

// drawnPerson is what the shell draws of a person: a DM row's name,
// picture, status emoji and presence; a group DM's name lists its
// members' names; the viewer's own row.
type drawnPerson struct {
	self, dm, mpim bool
	pres           bool   // an IM partner: the row draws their presence (an app's doesn't)
	name           string // core.User.Name as the model has it
	avatar, emoji  string // a DM partner's (convInfo.Avatar, StatusEmoji)
	active         bool   // an IM partner's or the viewer's presence
}

// syncView copies the whole model (apply; UI goroutine).
func (b *backend) syncView() {
	m := &b.m
	counts := make(map[string]drawnCount, len(m.Convs))
	people := map[string]*drawnPerson{}
	faces := map[string]bool{}
	person := func(id string) *drawnPerson {
		p := people[id]
		if p == nil {
			p = &drawnPerson{}
			people[id] = p
		}
		return p
	}
	if b.selfID != "" {
		p := person(b.selfID)
		p.self, p.active = true, m.SelfActive
	}
	for id, c := range m.Convs {
		counts[id] = drawnCount{c.Unread, c.Mentions, c.LastRead}
		switch c.Kind {
		case "channel", "private":
			for _, u := range c.Members {
				faces[u] = true
			}
		case "im", "app":
			if c.User != "" {
				p := person(c.User)
				p.dm, p.avatar, p.emoji = true, c.Avatar, c.StatusEmoji
				if c.Kind == "im" {
					p.pres, p.active = true, c.Active
				}
				if u, ok := b.core.RawUser(c.User); ok {
					p.name = u.Name
				}
			}
		case "mpim":
			for _, id := range c.Members {
				if id == "" {
					continue
				}
				p := person(id)
				if !p.mpim {
					p.mpim = true
					if u, ok := b.core.RawUser(id); ok {
						p.name = u.Name
					}
				}
			}
		}
	}
	// the headers resolved outside the model (closed DMs, channels opened
	// from search): their partner and facepile draw too
	ext := make(map[string]bool, len(b.ext))
	for id, ci := range b.ext {
		ext[id] = true
		if ci == nil {
			continue
		}
		if ci.User != "" {
			ext[ci.User] = true
		}
		for _, u := range ci.Members {
			faces[u] = true
		}
	}
	v := &b.view
	v.mu.Lock()
	v.ready, v.counts, v.people, v.faces, v.ext = true, counts, people, faces, ext
	v.inbox, v.badge = m.Rail.ActivityInbox, m.ActivityCount
	v.mu.Unlock()
}

// viewCount copies one conversation's unread state (UI goroutine).
func (b *backend) viewCount(c *convInfo) {
	v := &b.view
	v.mu.Lock()
	if v.counts != nil {
		v.counts[c.ID] = drawnCount{c.Unread, c.Mentions, c.LastRead}
	}
	v.mu.Unlock()
}

// viewFaces notes members a header's facepile draws (UI goroutine).
func (b *backend) viewFaces(ids []string) {
	v := &b.view
	v.mu.Lock()
	if v.faces == nil {
		v.faces = map[string]bool{}
	}
	for _, id := range ids {
		v.faces[id] = true
	}
	v.mu.Unlock()
}

// viewBadge copies the rail's Activity badge (recount; UI goroutine).
func (b *backend) viewBadge() {
	v := &b.view
	v.mu.Lock()
	v.inbox, v.badge = b.m.Rail.ActivityInbox, b.m.ActivityCount
	v.mu.Unlock()
}

// viewPerson copies what the model now draws of person id: DM rows,
// group DM names, the viewer's own (UI goroutine).
func (b *backend) viewPerson(id string, u *core.User) {
	v := &b.view
	v.mu.Lock()
	defer v.mu.Unlock()
	p := v.people[id]
	if p == nil {
		return
	}
	if u != nil && u.Name != "" {
		p.name = u.Name
	}
	if id == b.selfID {
		p.active = b.m.SelfActive
	}
	for _, c := range b.m.Convs {
		if (c.Kind == "im" || c.Kind == "app") && c.User == id {
			p.avatar, p.emoji = c.Avatar, c.StatusEmoji
			if c.Kind == "im" {
				p.active = c.Active
			}
		}
	}
}

// viewExt notes a conversation resolved outside the model, or forgotten
// (UI goroutine).
func (b *backend) viewExt(ch string, on bool) {
	v := &b.view
	v.mu.Lock()
	if on {
		if v.ext == nil {
			v.ext = map[string]bool{}
		}
		v.ext[ch] = true
	} else {
		delete(v.ext, ch)
	}
	v.mu.Unlock()
}

// countMoves reports whether cnt draws differently from what the model
// shows for ch (any goroutine). A conversation the model doesn't hold
// draws nothing (setCount).
func (v *pumpView) countMoves(ch string, cnt core.Count) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.ready {
		return true
	}
	d, ok := v.counts[ch]
	return ok && d != drawnCount{cnt.Unread, cnt.Mentions, cnt.LastRead}
}

// countsMove is countMoves for every conversation (a "counts" refresh).
func (v *pumpView) countsMove(cs map[string]core.Count) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.ready {
		return true
	}
	for id, cnt := range cs {
		if d, ok := v.counts[id]; ok && d != (drawnCount{cnt.Unread, cnt.Mentions, cnt.LastRead}) {
			return true
		}
	}
	return false
}

// badgeMoves reports whether core's Activity badge differs from the
// rail's (activityBadgeMoved, from any goroutine).
func (v *pumpView) badgeMoves(c *core.App) bool {
	v.mu.Lock()
	inbox, badge, ready := v.inbox, v.badge, v.ready
	v.mu.Unlock()
	if !ready {
		return true
	}
	return inbox && c != nil && c.ActivityBadgeCount() != badge
}

// presenceMoves reports whether a presence change moves a dot the model
// draws: a DM partner's or the viewer's.
func (v *pumpView) presenceMoves(ev core.Event) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.ready {
		return true
	}
	active := ev.Presence == "active"
	for _, id := range append([]string{ev.UserID}, ev.Users...) {
		if p := v.people[id]; id != "" && p != nil && (p.pres || p.self) && p.active != active {
			return true
		}
	}
	return false
}

// userMoves reports whether a profile change shows in the model: the
// viewer's own (their name, picture and status read core on the rail
// too: always), a DM row's name, picture or status emoji, a group DM's
// member's name.
func (v *pumpView) userMoves(ev core.Event) bool {
	id := ev.UserID
	if ev.User != nil {
		id = ev.User.ID
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.ready {
		return true
	}
	p := v.people[id]
	switch {
	case p == nil:
		return false
	case p.self || ev.User == nil:
		return true
	}
	u := ev.User
	if u.Name != "" && u.Name != p.name {
		return true
	}
	return p.dm && (u.Avatar != p.avatar || u.StatusEmojiURL != p.emoji)
}

// namesShown reports whether a lookup's ids ("names": members, bots,
// conversations core resolved in the background) name something the shell
// draws: a person, a conversation, a facepile's member, a header resolved
// outside the model (any goroutine). The member fetcher answers for every
// sender the store didn't hold, in any channel (a busy workspace's
// messages from people nobody drew), and the message lists' hub redraws
// the rows that show them on its own.
func (v *pumpView) namesShown(ids []string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.ready {
		return true
	}
	for _, id := range ids {
		if _, ok := v.counts[id]; ok || v.people[id] != nil || v.faces[id] || v.ext[id] {
			return true
		}
	}
	return false
}

// holdsConv reports whether the shell draws conversation ch: a row of the
// model, or a header resolved outside it.
func (v *pumpView) holdsConv(ch string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.ready {
		return true
	}
	_, ok := v.counts[ch]
	return ok || v.ext[ch]
}

// pumpIgnores are the events handle has nothing to do for: the views,
// the message lists' hub and the composer that draw them follow them on
// their own (Activity, Threads, the canvas, the agent's status line, the
// lists' deletes and sends), and the raw passthroughs change nothing the
// shell or anyone reads (a pref_change's "prefs", a thread's and the
// badges' "counts" and "activity_badges", the sections' "sidebar_changed"
// follow on their own).
var pumpIgnores = map[string]bool{
	"delete": true, "activity": true, "assistant_status": true, "canvas_viewers": true, "sent": true,
	"evicted": true, "poll": true, "thread_subscription": true, "thread_marked": true,
	"update_thread_state": true, "badge_counts_updated": true, "pref_change": true, "dnd_updated_user": true,
	"channel_sections_upserted": true, "channel_sections_channels_upserted": true,
	"channel_sections_channels_removed": true, "channel_sections_deleted": true,
}
