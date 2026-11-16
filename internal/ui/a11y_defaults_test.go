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
// off, so it happens once. An account that isn't new is left alone. On a
// Mac or Windows the OS setting is waited for: read after boot, it still
// counts; never read, nothing is written and the flag stays for the next
// boot (it was cleared, and the default lost for good). A missing
// a11y_animations isn't turned off (B0's n&&a). Synthetic data only.
func TestA11yNewUserDefaultsLikeSlack(t *testing.T) {
	saved, savedWait := a11yPlatform, a11yMotionWait
	t.Cleanup(func() {
		a11yPlatform, a11yMotionWait = saved, savedWait
		osMotion.Store(0)
	})
	const (
		known   = iota // the OS setting read before boot
		late           // read 300 ms after boot
		unknown        // never read
	)
	for _, tc := range []struct {
		name   string
		goos   string
		motion int
		reduce bool
		isNew  bool
		anim   any    // a11y_animations in the account before boot (nil: missing)
		want   string // a11y_animations after
		flag   string // set_a11y_prefs_new_user after
	}{
		{"mac, reduced motion", "darwin", known, true, true, true, "false", "false"},
		{"windows, reduced motion", "windows", known, true, true, true, "false", "false"},
		{"mac, full motion", "darwin", known, false, true, true, "true", "false"},
		{"linux, reduced motion", "linux", known, true, true, true, "true", "false"},
		{"mac, reduced motion, not new", "darwin", known, true, false, true, "true", "absent"},
		{"mac, reduced motion read after boot", "darwin", late, true, true, true, "false", "false"},
		{"mac, the OS setting never read", "darwin", unknown, true, true, true, "true", "true"},
		{"linux, the OS setting never read", "linux", unknown, false, true, true, "true", "false"},
		{"mac, reduced motion, no a11y_animations", "darwin", known, true, true, nil, "<nil>", "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a11yPlatform, a11yMotionWait = tc.goos, time.Second
			osMotion.Store(0)
			if tc.motion == known {
				setOSReduceMotion(tc.reduce)
			}
			fs, err := fakeslack.Start(fakeslack.Small)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { fs.Close() })
			fs.SetPrefs(map[string]any{"a11y_animations": tc.anim})
			if tc.isNew {
				fs.SetPrefs(map[string]any{"set_a11y_prefs_new_user": true})
			}
			home := t.TempDir()
			t.Setenv("CLACK_HOME", home)
			e := startEnv(t, fs, home)
			e.waitReady()
			if tc.motion == late {
				time.AfterFunc(300*time.Millisecond, func() { setOSReduceMotion(tc.reduce) })
			}
			pref := func(n string) string {
				v, ok := fs.Pref(n)
				if !ok {
					return "absent"
				}
				return fmt.Sprint(v)
			}
			if tc.flag == "false" {
				e.until("set_a11y_prefs_new_user off", 10*time.Second, func() bool { return pref("set_a11y_prefs_new_user") == "false" })
			} else {
				e.settle(a11yMotionWait + time.Second)
			}
			if got := pref("set_a11y_prefs_new_user"); got != tc.flag {
				t.Errorf("set_a11y_prefs_new_user %s, want %s", got, tc.flag)
			}
			if got := pref("a11y_animations"); got != tc.want {
				t.Errorf("a11y_animations %s, want %s", got, tc.want)
			}
			switch tc.want {
			case "false":
				e.until("the image cache drawing stills", 5*time.Second, func() bool { return !e.a.images.Animating() })
			case "true":
				if !e.a.images.Animating() {
					t.Error("animations went off")
				}
			}
		})
	}
}
