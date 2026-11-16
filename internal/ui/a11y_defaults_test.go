package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// A new member's first boot sets Slack's default accessibility prefs
// (a11y_defaults.go): on a Mac or Windows whose OS asks for reduced motion,
// Allow animations goes off (and the image cache draws stills); on Linux,
// or with full motion, it stays on; either way set_a11y_prefs_new_user goes
// off, so it happens once. An account that isn't new is left alone.
// Synthetic data only.
func TestA11yNewUserDefaultsLikeSlack(t *testing.T) {
	saved := a11yPlatform
	t.Cleanup(func() {
		a11yPlatform = saved
		osMotion.Store(0)
	})
	for _, tc := range []struct {
		name   string
		goos   string
		reduce bool
		isNew  bool
		anim   bool // a11y_animations after boot
	}{
		{"mac, reduced motion", "darwin", true, true, false},
		{"windows, reduced motion", "windows", true, true, false},
		{"mac, full motion", "darwin", false, true, true},
		{"linux, reduced motion", "linux", true, true, true},
		{"mac, reduced motion, not new", "darwin", true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a11yPlatform = tc.goos
			setOSReduceMotion(tc.reduce)
			fs, err := fakeslack.Start(fakeslack.Small)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { fs.Close() })
			if tc.isNew {
				fs.SetPrefs(map[string]any{"set_a11y_prefs_new_user": true})
			}
			home := t.TempDir()
			t.Setenv("CLACK_HOME", home)
			e := startEnv(t, fs, home)
			e.waitReady()
			pref := func(n string) string {
				v, ok := fs.Pref(n)
				if !ok {
					return "absent"
				}
				return fmt.Sprint(v)
			}
			if tc.isNew {
				e.until("set_a11y_prefs_new_user off", 10*time.Second, func() bool { return pref("set_a11y_prefs_new_user") == "false" })
			} else {
				e.settle(time.Second)
				if got := pref("set_a11y_prefs_new_user"); got != "absent" {
					t.Errorf("set_a11y_prefs_new_user is %s on an account that isn't new", got)
				}
			}
			if got, want := pref("a11y_animations"), fmt.Sprint(tc.anim); got != want {
				t.Errorf("a11y_animations %s, want %s", got, want)
			}
			if !tc.anim {
				e.until("the image cache drawing stills", 5*time.Second, func() bool { return !e.a.images.Animating() })
			} else if !e.a.images.Animating() {
				t.Error("animations went off")
			}
		})
	}
}
