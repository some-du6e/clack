package theme

import (
	"sync"

	"gioui.org/text"
)

// The packages' caches keyed by *text.Shaper (styles built on a shaper's
// faces) register a forget func here, and ForgetShaper drops a shaper
// from all of them and from the theme's own maps (shaperThemes,
// shaperFaces). An entry keeps the shaper reachable, and through it its
// parsed faces and its Theme with the Theme's image cache: internal/ui's
// tests make an App with its own shaper per test, and kept every one of
// them (ci-speed-3).
var (
	forgetMu      sync.Mutex
	shaperForgets []func(*text.Shaper)
)

// OnForgetShaper registers f to run for each shaper ForgetShaper drops;
// a package with a cache keyed by shaper calls it from init.
func OnForgetShaper(f func(*text.Shaper)) {
	forgetMu.Lock()
	shaperForgets = append(shaperForgets, f)
	forgetMu.Unlock()
}

// ForgetShaper drops sh from every cache keyed by a shaper. Only for a
// shaper nothing draws with again.
func ForgetShaper(sh *text.Shaper) {
	if sh == nil {
		return
	}
	shaperThemes.Delete(sh)
	shaperFaces.Delete(sh)
	forgetMu.Lock()
	fs := append([]func(*text.Shaper){}, shaperForgets...)
	forgetMu.Unlock()
	for _, f := range fs {
		f(sh)
	}
}

// ForgetShapers drops every shaper t was given (SetShapers, SetShaper,
// the ones a font change replaced included) from the caches keyed by a
// shaper: for a Theme that's done, a finished App's.
func (t *Theme) ForgetShapers() {
	var mine []*text.Shaper
	shaperThemes.Range(func(k, v any) bool {
		if v.(*Theme) == t {
			mine = append(mine, k.(*text.Shaper))
		}
		return true
	})
	mine = append(mine, t.shapers[0], t.shapers[1], t.Shaper)
	seen := map[*text.Shaper]bool{}
	for _, sh := range mine {
		if sh != nil && !seen[sh] {
			seen[sh] = true
			ForgetShaper(sh)
		}
	}
}
