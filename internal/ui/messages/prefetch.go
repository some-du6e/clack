package messages

import "time"

// PrefetchHistory starts loading ch's newest page before its view mounts:
// the press on its sidebar row (ui sidebar_prefetch.go, Slack's
// prefetchPointerdownHistory). The view that mounts on the click takes the
// load in flight (ensureLoaded sees it loading). A conversation whose
// history core holds already, or that a model has loaded or is loading,
// is left alone (Slack's has_history and loading skips), and so is one
// whose last load failed moments ago (its backoff): it reports whether a
// load started.
func PrefetchHistory(h Host, ch string) bool {
	app := h.Core()
	if app == nil || ch == "" || app.S.Timeline(ch) != nil {
		return false
	}
	hb := hubFor(h)
	if hb.authFailed.Load() {
		return false
	}
	m := hb.channel(ch)
	m.mu.Lock()
	skip := m.loaded || m.loading || time.Now().Before(m.retryAt)
	m.mu.Unlock()
	if skip {
		return false
	}
	hb.ensureLoaded(m)
	return true
}
