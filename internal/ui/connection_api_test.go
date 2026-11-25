package ui

import (
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
)

// The connection states' APIs the ui tests use, through helpers (the red
// tree's shim stubs them: the old code has none of them).

func setClientOnlineFor(f func(*core.App) bool) (restore func()) {
	prev := clientOnlineFor
	clientOnlineFor = f
	return func() { clientOnlineFor = prev }
}

func offlineToast(a *App, ch string) { a.offlineChannelToast(ch) }

// toastSeen is the toast as it stands.
type toastSeen struct {
	text  string
	top   bool
	icon  string
	parts []i18n.Part
}

func toastNow(a *App) toastSeen {
	return toastSeen{text: a.toast.text, top: a.toast.top, icon: a.toast.icon, parts: a.toast.parts}
}

func clearToast(a *App) { a.toast = toastState{} }

// routeTo is the root's afterRoute for a move from prev to r (fakeApp's
// backend has no header reads map: its conversation-open path needs one).
func routeTo(a *App, prev, r Route) {
	if a.data != nil && a.data.fetched == nil {
		a.data.fetched = map[string]time.Time{}
	}
	a.afterRoute(prev, r)
}
