package ui

import (
	"context"
	"log"
	"runtime"
	"time"
)

// a11yNewUserDefaults is Slack's default accessibility prefs for a new
// member (client-boot module wzOE's B0, "trying to set default
// accessibility preferences"): while set_a11y_prefs_new_user is on, on a
// Mac or Windows, when the OS asks for reduced motion (matchMedia
// "(prefers-reduced-motion: reduce)") and Accessibility > "Allow
// animations" (a11y_animations) is on, it turns that off ("Setting
// a11y_animations pref to false based on media-query"); then it turns
// set_a11y_prefs_new_user off, on every platform, so it happens once per
// account. It's the only way the OS setting reaches Slack's animated emoji
// and images: Electron leaves imageAnimationPolicy at "animate", and
// prefers-reduced-motion otherwise reaches only CSS and a few hooks
// (useShouldAnimate: skeleton shimmers, floating views). Once the pref is
// off, the image cache draws stills (applyAccountPrefs: SetAnimate).
func (b *backend) a11yNewUserDefaults(a *App) {
	if !b.core.PrefBool("set_a11y_prefs_new_user", false) {
		return
	}
	reduce, known := osReduceMotion()
	off := (a11yPlatform == "darwin" || a11yPlatform == "windows") && known && reduce && b.core.PrefBool("a11y_animations", true)
	parent := b.ctx
	if parent == nil {
		parent = context.Background()
	}
	go func() {
		ctx, cancel := context.WithTimeout(parent, 15*time.Second)
		defer cancel()
		if off {
			log.Print("ui: Allow animations is off for a new member: the OS asks for reduced motion (Slack's set_a11y_prefs_new_user)")
			if err := b.core.SetPref(ctx, "a11y_animations", false); err != nil {
				log.Printf("ui: a11y_animations: %v", err)
			}
		}
		if err := b.core.SetPref(ctx, "set_a11y_prefs_new_user", false); err != nil {
			log.Printf("ui: set_a11y_prefs_new_user: %v", err)
		}
	}()
}

// a11yPlatform is the platform a11yNewUserDefaults follows (tests: another
// one's rule).
var a11yPlatform = runtime.GOOS
