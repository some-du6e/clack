package blockkit

import "github.com/some-du6e/clack/internal/core"

// ctaLoaded reports whether the app actions list has come
// (CachedShortcuts, which asks for it the first time).
func ctaLoaded(a *core.App) bool {
	_, ok := a.Interactive().CachedShortcuts()
	return ok
}
