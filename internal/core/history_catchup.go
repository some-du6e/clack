package core

import "github.com/some-du6e/clack/internal/store"

// behindLatest reports whether t, the newest window core holds for ch,
// ends before ch's latest message as the counts know it (client.counts,
// and every top-level message the socket brought: store.NoteMessage), and
// notes that latest: History reads the newest page again once per latest
// message. A window behind it misses a message the socket never brought
// (it was down; Slack's views refetch the histories a slow reconnect
// reset, fetchAndApplyReconnectCounts → resetChannelsWithFetchedHistory)
// or one a page read before it took out. Once per latest: a latest no
// page shows (a message the history leaves out) costs one read, not one
// per frame.
func (a *App) behindLatest(ch string, t *store.Timeline) bool {
	e, _ := a.S.Count(ch)
	if e.Latest == "" || e.Latest == "0000000000.000000" {
		return false
	}
	if n := len(t.Msgs); n > 0 && !store.TSLess(t.Msgs[n-1].TS, e.Latest) {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.caughtUp[ch] == e.Latest {
		return false
	}
	if a.caughtUp == nil {
		a.caughtUp = map[string]string{}
	}
	a.caughtUp[ch] = e.Latest
	return true
}
