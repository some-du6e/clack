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

// historyChanged is core's history_stale, history_purged and
// history_caught_up (core/history_catchup.go): the counts said ch's
// history changed in a way the socket never told, or a background
// catch-up landed. Its models load again, the lists on screen at the next
// frame and the others when next shown. A purge starts a model not on
// screen over; one on screen keeps drawing what it has until its load
// lands, and takes the page whole then (its window's Epoch changed:
// takeNewest). A caught-up thread names its thread (ThreadTS); a stale
// mark or a purge covers the channel's threads too. It reports whether a
// list on screen loads.
func (hb *hub) historyChanged(ev core.Event) bool {
	purge := ev.Type == "history_purged"
	threads := ev.Type != "history_caught_up"
	hb.mu.Lock()
	var ms []*model
	var shown []bool
	on := func(m *model) bool { return !hb.frame.IsZero() && m.drawn.Equal(hb.frame) }
	if m := hb.chans[ev.Channel]; m != nil && ev.ThreadTS == "" {
		ms, shown = append(ms, m), append(shown, on(m))
	}
	for _, m := range hb.thread {
		if m.ch == ev.Channel && (threads || m.threadTS == ev.ThreadTS && ev.ThreadTS != "") {
			ms, shown = append(ms, m), append(shown, on(m))
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
		case m.loading:
			m.again = true // its answer was read before the change (afterLoad)
		default:
			m.fetched = time.Time{}
			m.rev++
			redraw = true
		}
		m.mu.Unlock()
	}
	return redraw
}

// takeNewest puts a History answer into a channel model: merged over the
// newest messages it has while core's window is the same one (its Epoch),
// or the whole list once it started over (a newest page that didn't reach
// what the window had: a gap of more than a page, a purge), as Slack
// renders the newest slice and loads back from its start. The caller holds
// m.mu.
func (m *model) takeNewest(p core.Page) {
	if m.loaded && p.Epoch != m.epoch {
		m.setAll(p.Messages)
		m.hasMore = p.HasMore
	} else {
		m.mergeNewest(p.Messages, p.HasMore)
	}
	m.epoch = p.Epoch
}

// local reports whether msg is a copy no history page holds: the viewer's
// send Slack hasn't confirmed (pending or failed), an ephemeral message.
// Where a page starts, and where the next older page reads from, go by
// the others.
func local(msg *core.Message) bool { return msg.Pending || msg.Failed || msg.Ephemeral }

// oldestFromSlack is where Slack's history the model holds starts: its
// oldest message that isn't a local copy, the next older page's before
// (the model's first message when every one is local). The caller holds
// m.mu.
func (m *model) oldestFromSlack() string {
	for i := range m.msgs {
		if !local(&m.msgs[i].m) {
			return m.msgs[i].m.TS
		}
	}
	if len(m.msgs) > 0 {
		return m.msgs[0].m.TS
	}
	return ""
}
