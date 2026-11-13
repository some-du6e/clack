package ui

// TestDemoReadmeShots takes the README's screenshots (docs/screenshots/):
// `clack demo`'s made-up workspace, light and dark, in a 1440x900 window:
// the main window (#general), a thread, Activity and a huddle (the one
// running in #party-planning-crew). CLACK_DEMO_SHOTS only; the files land
// in $CLACK_DEMO_SHOTS/readme. Synthetic only: the demo runs fakeslack
// on loopback and everything in it is invented.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/demo"
)

func TestDemoReadmeShots(t *testing.T) {
	root := os.Getenv("CLACK_DEMO_SHOTS")
	if root == "" {
		t.Skip("CLACK_DEMO_SHOTS not set")
	}
	dir := filepath.Join(root, "readme")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e, s := demoEnv(t, demo.Options{Quiet: true, Speed: 40, Seed: 3})
			e.setDark(dark)
			e.until("the demo sidebar", 30*time.Second, func() bool { return e.sidebarHas(demo.GeneralID) && e.sidebarHas(demo.CodeID) })
			shot := func(name string) {
				e.settle(1500 * time.Millisecond) // avatars, emoji and pictures in
				e.shotTo(t, dir, "demo-"+name+"-"+mode)
			}
			// Activity first: visiting the channels reads them
			e.a.Navigate("activity")
			e.until("Activity", 20*time.Second, func() bool { return e.a.views[Route("activity")] != nil })
			shot("activity")
			// the main window: #general at its newest messages
			e.openChannel(demo.GeneralID)
			e.until("#general", 20*time.Second, func() bool { return e.loadedMessages(demo.GeneralID) >= 10 })
			e.toLatest()
			shot("main")
			// a thread: #it-help's busiest
			var parent string
			most := 0
			for ts, rs := range s.WS.Replies[demo.ITHelpID] {
				if len(rs) > most || (len(rs) == most && ts > parent) {
					parent, most = ts, len(rs)
				}
			}
			e.openChannel(demo.ITHelpID)
			e.until("#it-help", 20*time.Second, func() bool { return e.loadedMessages(demo.ITHelpID) > 0 })
			e.toLatest()
			e.a.OpenThread(demo.ITHelpID, parent)
			e.until("the thread", 20*time.Second, func() bool {
				th, _ := e.a.data.core.S.Thread(demo.ITHelpID, parent)
				return len(th) > 1
			})
			shot("thread")
			// a huddle: the one running in #party-planning-crew
			e.a.CloseSecondary()
			e.openChannel(demo.LiveHuddleChannel)
			e.until("#party-planning-crew", 20*time.Second, func() bool { return e.loadedMessages(demo.LiveHuddleChannel) > 0 })
			e.toLatest()
			shot("huddle")
			e.a.Navigate("huddles")
			shot("huddles-page")
		})
	}
}
