package ui

import (
	"context"
	"log"
	"runtime"
	"time"
)

// a11yNewUserDefaults is Slack's default accessibility prefs for a new
// member (module N09W's updateA11yPreferences on bootComplete, which calls
// wzOE's B0, "trying to set default accessibility preferences"): while
// set_a11y_prefs_new_user is on, on a Mac or Windows, when the OS asks for
// reduced motion (matchMedia "(prefers-reduced-motion: reduce)") and
// Accessibility > "Allow animations" (a11y_animations) is on, it turns that
// off ("Setting a11y_animations pref to false based on media-query"); then
// it turns set_a11y_prefs_new_user off, on every platform, so it happens
// once per account. It's the only way the OS setting reaches Slack's
// animated emoji and images: Electron leaves imageAnimationPolicy at
// "animate", and prefers-reduced-motion otherwise reaches only CSS and a
// few hooks (useShouldAnimate: skeleton shimmers, floating views). Once the
// pref is off, the image cache draws stills (applyAccountPrefs:
// SetAnimate).
//
// matchMedia always has an answer; Clack's OS watcher (osmotion.go) may not
// have read the setting yet when boot completes (macOS reads it on the main
// queue), or ever (an SPI call that fails on Windows). So on a Mac or
// Windows it waits for the first read (a11yMotionWait at most, off the UI
// goroutine), and without one writes nothing: the flag stays for the next
// boot, where clearing it would have lost the default for good. A missing
// a11y_animations is off, as B0's n&&a reads it: nothing to turn off.
func (b *backend) a11yNewUserDefaults(a *App) {
	if !b.core.PrefBool("set_a11y_prefs_new_user", false) {
		return
	}
	parent := b.ctx
	if parent == nil {
		parent = context.Background()
	}
	mac := a11yPlatform == "darwin" || a11yPlatform == "windows"
	go func() {
		reduce := false
		if mac {
			var known bool
			for dl := time.Now().Add(a11yMotionWait); ; time.Sleep(50 * time.Millisecond) {
				if reduce, known = osReduceMotion(); known {
					break
				}
				if time.Now().After(dl) || parent.Err() != nil {
					log.Print("ui: the OS's reduced motion setting isn't known: set_a11y_prefs_new_user stays for the next start")
					return
				}
			}
		}
		ctx, cancel := context.WithTimeout(parent, 15*time.Second)
		defer cancel()
		if mac && reduce && b.core.PrefBool("a11y_animations", false) {
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

// a11yMotionWait is how long a11yNewUserDefaults waits for the OS motion
// setting's first read (tests: less).
var a11yMotionWait = 10 * time.Second
