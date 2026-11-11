package slackc

import (
	"sync"
	"sync/atomic"
)

// Slack scopes a region's theme tokens with a class. In a light client
// whose chrome is dark (the default Aubergine theme), the chrome, the tab
// rail and the sidebar carry .sk-client-theme--light-inverted-sidebar
// (gantry-v2 boot-shared y4F2 useInvertedTheme), and the theme stylesheet
// (oWug) re-declares every --dt_color-theme-* token for that class from
// the theme's palette in the "light-inverted-sidebar" mode (KlX6). So a
// plugin element in the top nav that takes Slack's
// var(--dt_color-theme-content-inv-pry) (StreamerMode's toggle, through
// .p-top_nav__button) is white there, not the root's aubergine.
//
// SetScopeVars gives a class those custom properties: a native whose
// classes include it declares them, as the stylesheet rule would, and what
// is laid out inside it (a plugin's host tree) inherits them. The ui root
// computes them from the account theme (theme.IATheme.Fields).

var scopeVars struct {
	mu sync.Mutex
	m  map[string]map[string]string // class → custom properties
}

var scopeVarsOn atomic.Bool

// SetScopeVars installs (nil removes) the custom properties class declares.
func SetScopeVars(class string, vars map[string]string) {
	scopeVars.mu.Lock()
	if vars == nil {
		delete(scopeVars.m, class)
	} else {
		if scopeVars.m == nil {
			scopeVars.m = map[string]map[string]string{}
		}
		scopeVars.m[class] = vars
	}
	scopeVarsOn.Store(len(scopeVars.m) > 0)
	scopeVars.mu.Unlock()
	resetNativeStyles()
	invalidate()
}

// scopeVarsOf merges the scope variables of el's classes over parent's
// custom properties (nil when none of its classes has any).
func scopeVarsOf(el *nativeEl, parent map[string]string) map[string]string {
	if !scopeVarsOn.Load() {
		return nil
	}
	scopeVars.mu.Lock()
	defer scopeVars.mu.Unlock()
	var out map[string]string
	for _, c := range el.Classes() {
		vs := scopeVars.m[c]
		if len(vs) == 0 {
			continue
		}
		if out == nil {
			out = make(map[string]string, len(parent)+len(vs))
			for k, v := range parent {
				out[k] = v
			}
		}
		for k, v := range vs {
			out[k] = v
		}
	}
	return out
}
