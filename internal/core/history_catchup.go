package core

import "github.com/some-du6e/clack/internal/store"

// behindLatest reports whether t, the newest window core holds for ch,
// ends before ch's latest message as the counts know it (client.counts,
// and every top-level message the socket brought: store.NoteMessage) when
// that latest moved after the window was read: the window then misses a
// message the socket never brought (it was down, and the counts read
// after the reconnect name it), as Slack's views refetch the histories a
// slow reconnect reset (fetchAndApplyReconnectCounts →
// resetChannelsWithFetchedHistory). History then reads the newest page
// again, once per latest: a latest no page shows (a message the history
// leaves out) costs one read, not one per frame. A window core didn't
// read itself (one put in the store directly) counts as current for the
// latest known when it is first read.
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
	seen, ok := a.caughtUp[ch]
	if a.caughtUp == nil {
		a.caughtUp = map[string]string{}
	}
	a.caughtUp[ch] = e.Latest
	return ok && seen != e.Latest
}

// noteCaughtUp records that ch's newest window was read from Slack when
// the counts knew latest as its latest message (fetchLatest, before the
// request): a later latest is one the window may miss.
func (a *App) noteCaughtUp(ch, latest string) {
	a.mu.Lock()
	if a.caughtUp == nil {
		a.caughtUp = map[string]string{}
	}
	a.caughtUp[ch] = latest
	a.mu.Unlock()
}
