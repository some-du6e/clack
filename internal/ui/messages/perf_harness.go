//go:build perfharness

package messages

import "gioui.org/layout"

// The memory workflow's driver (internal/ui/perfdrive.go) scrolls the main
// pane's message lists through these, as a reader with a mouse wheel
// does: to the oldest loaded message (infinite scroll pages the older
// one in), then down through every row. perfharness builds only; UI
// goroutine.

// PerfList reports ch's main-pane list: the messages its view holds,
// whether older ones exist on the server, and whether a page is loading.
func PerfList(ch string) (msgs int, hasMore, loading, ok bool) {
	s, found := listCache[ch]
	if !found {
		return 0, false, false, false
	}
	return len(s.snap.msgs), s.snap.hasMore, s.snap.older || s.snap.loading, true
}

// PerfScrollTop puts ch's list at its oldest loaded message.
func PerfScrollTop(ch string) bool {
	s, found := listCache[ch]
	if !found {
		return false
	}
	s.list.Position = layout.Position{BeforeEnd: true}
	return true
}

// PerfScrollDown moves ch's list down by n rows and reports whether it is
// at the newest message.
func PerfScrollDown(ch string, n int) (atEnd bool) {
	s, found := listCache[ch]
	if !found {
		return true
	}
	f := s.list.Position.First + n
	if f >= len(s.items)-1 {
		s.list.Position = layout.Position{}
		return true
	}
	s.list.Position = layout.Position{BeforeEnd: true, First: f}
	return false
}

// PerfCounts sums what the message lists hold: cached list states, their
// rows (and how many still have laid-out text: a released row has none),
// and the shared models' conversations and messages.
func PerfCounts() (lists, rows, laidOut, models, msgs int) {
	for _, s := range listCache {
		lists++
		rows += len(s.rows)
		for _, r := range s.rows {
			if r.rv != nil && r.rv.PlainLen() > 0 {
				laidOut++
			}
		}
	}
	hubsMu.Lock()
	hbs := make([]*hub, 0, len(hubs))
	for _, hb := range hubs {
		hbs = append(hbs, hb)
	}
	hubsMu.Unlock()
	for _, hb := range hbs {
		hb.mu.Lock()
		for _, ms := range []map[string]*model{hb.chans, hb.thread} {
			for _, m := range ms {
				models++
				m.mu.Lock()
				msgs += len(m.msgs)
				m.mu.Unlock()
			}
		}
		hb.mu.Unlock()
	}
	return
}
