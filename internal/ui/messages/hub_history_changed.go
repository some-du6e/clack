package messages

import (
	"sort"
	"strings"
)

// historyChanged is core's "history_changed" (core/history_changed.go):
// Slack rewrote ch's history (a bulk delete, retention) and core let go
// of what it cached before latest ("": all of it). Every model of ch
// keeps only what is at or after latest, as
// truncateChannelHistoryPriorToTimestamp keeps a slice's part from latest
// on: core's "evicted" events tell of core's copies, and a model can hold
// more than core's timeline (the pages its list read, a timeline core
// started over since), whose ghosts would stay. The conversation's model
// keeps what is after latest (none left: it loads its newest page
// again); a thread's reads its slice again; a window left with nothing
// loads again around its newest message, as Slack's pane requests the
// slice around its startTs again once its own is gone. Reports whether a
// model changed.
func (hb *hub) historyChanged(ch, latest string) bool {
	// before is the index of m's first message at or after latest (all of
	// them for "")
	before := func(m *model) int {
		if latest == "" {
			return len(m.msgs)
		}
		return sort.Search(len(m.msgs), func(i int) bool { return !tsLess(m.msgs[i].m.TS, latest) })
	}
	changed := false
	hb.mu.Lock()
	chm := hb.chans[ch]
	var threads []*model
	for k, m := range hb.thread {
		if strings.HasPrefix(k, ch+"/") {
			threads = append(threads, m)
		}
	}
	hb.mu.Unlock()
	if chm != nil {
		chm.mu.Lock()
		chm.noteCut(latest)
		chm.cutAt(latest)
		changed = true
		chm.mu.Unlock()
	}
	for _, m := range threads {
		m.mu.Lock()
		if before(m) > 0 {
			m.evictThread()
			changed = true
		}
		m.mu.Unlock()
	}
	for _, m := range hb.windowsOf(ch) {
		m.mu.Lock()
		m.noteCut(latest)
		anchor := ""
		if n := len(m.msgs); n > 0 {
			anchor = m.msgs[n-1].m.TS
		}
		i := before(m)
		reload := i == len(m.msgs) && anchor != ""
		switch {
		case reload:
			// nothing of it is left: drawn as loading until the window
			// around its newest message is in
			m.msgs, m.loaded, m.hasMore = nil, false, false
			m.evictGen++
			m.rev++
			changed = true
		case i > 0:
			m.evict(latest)
			changed = true
		}
		m.mu.Unlock()
		if reload {
			hb.loadWindow(m, anchor)
		}
	}
	return changed
}

// cutAt drops m's messages before latest ("": all of them) but for
// what only this client has (a pending or failed send, an ephemeral
// message: core keeps them too, store/history_changed.go); none from
// Slack left, the model loads its newest page again, and core's carries
// them. m.mu is held.
func (m *model) cutAt(latest string) {
	var keep []entry
	fromSlack := 0
	for _, e := range m.msgs {
		switch {
		case e.m.Pending || e.m.Failed || e.m.Ephemeral:
			keep = append(keep, e)
		case latest != "" && !tsLess(e.m.TS, latest):
			keep = append(keep, e)
			fromSlack++
		}
	}
	switch {
	case len(keep) == len(m.msgs):
	case fromSlack == 0:
		m.evict("")
	default:
		m.msgs = keep
		m.hasMore = true
		m.evictGen++
		m.rev++
	}
}

// cutRec is a history change a load out may land across: its latest and
// cutGen as it came.
type cutRec struct {
	latest string
	gen    uint64
}

// noteCut records a history change for the loads out now (their answers
// may be from before it). m.mu is held.
func (m *model) noteCut(latest string) {
	m.cutGen++
	if m.loading || m.around != "" || m.older || m.newer {
		m.cuts = append(m.cuts, cutRec{latest, m.cutGen})
	}
}

// applyCut is a load landing that started at cut generation gen: what is
// before each history change that came after it goes again, and once no
// load is out the changes are let go. m.mu is held.
func (m *model) applyCut(gen uint64) {
	for _, c := range m.cuts {
		if c.gen > gen {
			m.cutAt(c.latest)
		}
	}
	if !m.loading && m.around == "" && !m.older && !m.newer {
		m.cuts = nil
	}
}
