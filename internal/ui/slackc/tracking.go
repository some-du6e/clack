package slackc

import (
	"sync"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's Theme (boot-shared oWug) defines --custom-font-tracking-* on
// .use-theme-font for the fonts it tracks, and Slack's CSS reads them as
// letter-spacing (var(--custom-font-tracking-body, inherit)). slackc runs
// that CSS itself (its words carry the computed letter-spacing), so
// rootVars adds theme.TrackingVars to the design tokens while such a font
// is the UI font. The views restyle when the font changes (View.Layout).
// slackc's words are shaped Untracked, so the shaper's own tracking
// (theme/tracking.go) doesn't add to the CSS's. Not done: Slack's body
// rule gives text with no letter-spacing of its own body's 0.06pt by
// inheritance; slackc's root style starts from the initial value.

var tracked struct {
	sync.Mutex
	maps [2]map[string]string // light, dark: SlackVars plus the tracking
}

// rootVars are :root's custom properties for this frame.
func rootVars(dark bool) map[string]string {
	base := css.SlackVars(dark)
	tv := theme.TrackingVars()
	if tv == nil {
		return base
	}
	i := 0
	if dark {
		i = 1
	}
	tracked.Lock()
	defer tracked.Unlock()
	if m := tracked.maps[i]; m != nil {
		return m
	}
	m := make(map[string]string, len(base)+len(tv))
	for k, v := range base {
		m[k] = v
	}
	for k, v := range tv {
		m[k] = v
	}
	tracked.maps[i] = m
	return m
}
