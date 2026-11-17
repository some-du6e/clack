package messages

import (
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// zeroTS is the latest Slack's counts send for a conversation with no
// messages.
const zeroTS = "0000000000.000000"

// catchUp is a counts answer for the lists on screen: a channel list whose
// newest message is older than its conversation's latest (core.Count: the
// counts Slack sent, which name a message the socket never brought while
// it was down) loads again, and core's History reads the newest page from
// Slack for a window behind the latest (windowBehind,
// core/history_catchup.go). Without it such a list caught up only at its
// next 30 s reload. A list loads again once per latest: an empty
// conversation, or a latest no page shows (a deleted message the counts
// still name), would otherwise load at every counts answer, about every
// 1.5 s on a busy Grid. It reports whether one does.
func (hb *hub) catchUp() bool {
	if hb.app == nil {
		return false
	}
	hb.mu.Lock()
	var ms []*model
	if !hb.frame.IsZero() {
		for _, m := range hb.chans {
			if m.drawn.Equal(hb.frame) {
				ms = append(ms, m)
			}
		}
	}
	hb.mu.Unlock()
	behind := false
	for _, m := range ms {
		latest := hb.app.Count(m.ch).Latest
		if latest == "" || latest == zeroTS {
			continue
		}
		m.mu.Lock()
		if n := len(m.msgs); m.loaded && m.caughtUp != latest && (n == 0 || tsLess(m.msgs[n-1].m.TS, latest)) {
			m.caughtUp = latest
			if m.loading {
				m.again = true // a load read before these counts is out: once more after it (afterLoad)
			} else {
				m.fetched = time.Time{} // ensureLoaded loads it at the next frame
				m.rev++
				behind = true
			}
		}
		m.mu.Unlock()
	}
	return behind
}

// historyChanged is core's history_stale and history_purged: the counts
// said ch's history changed in a way the socket never told
// (core/history_catchup.go). Its models load again, the lists on screen
// at the next frame and the others when next shown, and core's History
// and Replies read Slack for them. After a purge a list on screen takes
// the newest page whole at that load (its older messages go too, as
// Slack's purge drops them) while it keeps drawing what it has until
// then; a model not on screen starts over. It reports whether a list on
// screen loads.
func (hb *hub) historyChanged(ev core.Event) bool {
	purge := ev.Type == "history_purged"
	hb.mu.Lock()
	var ms []*model
	var shown []bool
	on := func(m *model) bool { return !hb.frame.IsZero() && m.drawn.Equal(hb.frame) }
	if m := hb.chans[ev.Channel]; m != nil {
		ms, shown = append(ms, m), append(shown, on(m))
	}
	if purge {
		for _, m := range hb.thread {
			if m.ch == ev.Channel {
				ms, shown = append(ms, m), append(shown, on(m))
			}
		}
	}
	hb.mu.Unlock()
	redraw := false
	for i, m := range ms {
		m.mu.Lock()
		switch {
		case !shown[i] && purge:
			m.evict("")
		case !shown[i]:
			m.fetched = time.Time{}
		default:
			m.purged = m.purged || purge && m.threadTS == ""
			if m.loading {
				m.again = true // its answer was read before the change (afterLoad)
			} else {
				m.fetched = time.Time{}
				m.rev++
				redraw = true
			}
		}
		m.mu.Unlock()
	}
	return redraw
}

// takeNewest puts a History answer into a channel model: merged over the
// newest messages it has, or the whole list after a purge (historyChanged)
// once a load read after it lands. The caller holds m.mu.
func (m *model) takeNewest(p core.Page) {
	if m.purged && !m.again {
		m.purged = false
		m.setAll(p.Messages)
		m.hasMore = p.HasMore
		return
	}
	m.mergeNewest(p.Messages, p.HasMore)
}
