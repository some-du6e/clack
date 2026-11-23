package messages

import (
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// Live is ch/ts as a message list drawn at or after since holds it: the
// thread pane's copy (threadTS names the thread a reply is in, or is the
// root's own ts; with none, the pane of a thread ts may have started),
// else the channel list's (a channel message, a thread root or a reply
// also sent to the channel), and its revision there (the model's id and
// the entry's rev).
// have is the revision the caller holds: when it's that one, no copy is
// made (m is zero). ok is false when no list drawn since then holds it,
// or its model hasn't loaded yet.
//
// Slack's Activity rows read the messages store the channel list and the
// thread pane read (getMessageByTimestamp), so a message open in both
// shows one revision in every commit. A view reading this draws, in the
// frame a list draws a message's new revision, that same revision (the
// views' Host: views.LiveMessages).
func Live(app *core.App, ch, ts, threadTS string, since time.Time, have uint64) (m core.Message, rev uint64, ok bool) {
	if app == nil || ch == "" || ts == "" || since.IsZero() {
		return core.Message{}, 0, false
	}
	hubsMu.Lock()
	hb := hubs[app]
	hubsMu.Unlock()
	if hb == nil {
		return core.Message{}, 0, false
	}
	ms := make([]*model, 0, 4)
	hb.mu.Lock()
	if threadTS != "" {
		if tm := hb.thread[ch+"/"+threadTS]; tm != nil && !tm.drawn.Before(since) {
			ms = append(ms, tm)
		}
	} else if tm := hb.thread[ch+"/"+ts]; tm != nil && !tm.drawn.Before(since) {
		// a message that may be a root (a copy read before its first
		// reply names no thread): its own thread's pane, as for one that
		// names it
		ms = append(ms, tm)
	}
	// the channel list holds its messages and thread roots, and a reply
	// also sent to the channel (thread_broadcast): a reply looked for there
	// that isn't one finds nothing
	if cm := hb.chans[ch]; cm != nil && !cm.drawn.Before(since) {
		ms = append(ms, cm)
	}
	// a jump's window a list drew since: the copy it draws of a message
	// far from the newest ones
	for w := range hb.wins {
		if w.ch == ch && !w.drawn.Before(since) {
			ms = append(ms, w)
		}
	}
	hb.mu.Unlock()
	for _, md := range ms {
		md.mu.Lock()
		i := -1
		// a model the hub let go and a list took back holds the window it
		// had then until its reload lands (dropped): it missed every event
		// while out, and its copy may be older than the one Activity drew;
		// so does a window reloading (a list took it back)
		if md.loaded && !md.dropped && !(md.win && md.loading) {
			i = md.find(ts)
		}
		if i < 0 {
			md.mu.Unlock()
			continue
		}
		e := &md.msgs[i]
		rev = uint64(md.id)<<32 | uint64(e.rev)
		if rev != have {
			m = e.m
		}
		md.mu.Unlock()
		return m, rev, true
	}
	return core.Message{}, 0, false
}
