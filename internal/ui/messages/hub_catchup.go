package messages

import "time"

// catchUp is a counts answer for the lists on screen: a channel list whose
// newest message is older than its conversation's latest (core.Count: the
// counts Slack sent, which name a message the socket never brought while
// it was down) loads again, and core's History reads the newest page from
// Slack for a window behind the latest (behindLatest), as Slack's views
// refetch the histories a slow reconnect reset (fetchAndApplyReconnectCounts).
// Without it such a list caught up only at its next 30 s reload. It
// reports whether one does.
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
		m.mu.Lock()
		if n := len(m.msgs); m.loaded && !m.loading && latest != "" && (n == 0 || tsLess(m.msgs[n-1].m.TS, latest)) {
			m.fetched = time.Time{} // ensureLoaded loads it at the next frame
			m.rev++
			behind = true
		}
		m.mu.Unlock()
	}
	return behind
}
