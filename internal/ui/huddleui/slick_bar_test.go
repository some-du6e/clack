package huddleui

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Karim, α.112: "should be a bar down here if i close the window". Slick's
// LetMeSee is on by default and patches HuddleMiniPanel, Slack's screen
// share mini panel ("Adds a Hide drawings button to the floating huddle bar
// shown during a screenshare"; /+fI HuddleMiniPanelMounter's window). With
// it installed, the docked bar (HuddleBottomBar) must still draw whole once
// the plugin's tree is in: its title and its subtitle, which only the bar
// draws.
func TestSlickViewLetMeSeeKeepsTheBar(t *testing.T) {
	e := newHuddleCallEnv(t)
	e.install("LetMeSee")
	e.until("LetMeSee's patches", 5*time.Second, func() bool { return slackc.Patched("HuddleMiniPanel") })
	e.join()
	time.Sleep(2 * time.Second) // the plugin's trees for this frame's roots, if any
	for i := 0; i < 20; i++ {
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
	var mu sync.Mutex
	seen := map[string]bool{}
	theme.DrawHook = func(s string) { mu.Lock(); seen[s] = true; mu.Unlock() }
	t.Cleanup(func() { theme.DrawHook = nil })
	bar := func() (title, sub bool) {
		mu.Lock()
		defer mu.Unlock()
		for s := range seen {
			title = title || s == "Huddle in #huddle-test"
			// the subtitle, whole or cut to the column ("Remote Example is spe…")
			sub = sub || strings.HasPrefix(s, "Remote Example is") || strings.Contains(s, "people here") || s == "You’re the only one here"
		}
		return
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		e.frame()
		if t1, s1 := bar(); t1 && s1 {
			break
		}
		if time.Now().After(deadline) {
			t1, s1 := bar()
			e.mu.Lock()
			for root, tr := range e.trees {
				s := string(tr)
				if len(s) > 1500 {
					s = s[:1500] + "…"
				}
				t.Logf("tree %d: %s", root, s)
			}
			e.mu.Unlock()
			t.Fatalf("with LetMeSee on the docked bar doesn't draw (its title %v, its subtitle %v)", t1, s1)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
