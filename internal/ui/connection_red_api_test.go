package ui

import (
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
)

// Red-tree shims: the connection states' APIs don't exist on the old code.

func setClientOnlineFor(f func(*core.App) bool) (restore func()) { return func() {} }
func offlineToast(a *App, ch string)                             {}

type toastSeen struct {
	text  string
	top   bool
	icon  string
	parts []i18n.Part
}

func toastNow(a *App) toastSeen { return toastSeen{} }
func clearToast(a *App)         {}
func routeTo(a *App, prev, r Route) {
	if a.data != nil && a.data.fetched == nil {
		a.data.fetched = map[string]time.Time{}
	}
	a.afterRoute(prev, r)
}
