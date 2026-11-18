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

// startPastGap is the first message past the gap of a window behind a
// slow connect (core.Event.Epoch with GapView): from it on the list shows
// what is past the gap alone, loading back from its start, as History's
// page does while the catch-up is out and as Slack renders the newest
// slice after resetReachedEnd. Appended under what the list held, it drew
// yesterday's last message straight above today's, the gap unmarked,
// whenever the list's reload after the wake ran before the message came.
// The viewer's local copies newer than the list's newest message from
// Slack stay; the message itself goes in after (apply). The caller holds
// m.mu.
func (m *model) startPastGap(epoch uint64) {
	newest := ""
	for i := len(m.msgs) - 1; i >= 0; i-- {
		if !local(&m.msgs[i].m) {
			newest = m.msgs[i].m.TS
			break
		}
	}
	var keep []core.Message
	for _, e := range m.msgs {
		if local(&e.m) && (newest == "" || tsLess(newest, e.m.TS)) {
			keep = append(keep, e.m)
		}
	}
	m.setAll(keep)
	m.hasMore = true
	m.epoch = epoch
	m.pastGap++ // a load out now read the view from before (ensureLoaded)
}

// landNewest is a channel model's load landing (ensureLoaded): the newest
// page p, read when startPastGap had switched it gap0 times, or the load's
// error. A page read before a switch since is the view from before it (the
// pre-gap messages, put back above the one past the gap whenever the load
// landed after it): it isn't taken, and the next frame reads again. It
// reports whether p was taken. The caller holds m.mu.
func (m *model) landNewest(hb *hub, p core.Page, err error, gap0 uint32) bool {
	m.loading = false
	m.err = err
	if err == nil && m.pastGap != gap0 {
		m.fetched = time.Time{}
		m.rev++
		return false
	}
	live := m.takeLive()
	if err != nil {
		m.failed(hb, err, false, time.Now())
		m.rev++
		return false
	}
	m.takeNewest(p)
	m.keepLive(live)
	if !m.loaded {
		m.lastRead = p.LastRead
	}
	m.loaded = true
	m.fetched = time.Now()
	m.fails, m.retryAt = 0, time.Time{}
	m.rev++
	return true
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
