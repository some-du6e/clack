package ui

// TestDemoSiteShots takes the website's screenshots (site/, see
// site/README.md): `clack demo`'s made-up workspace in a 1440x900 window,
// without the "Demo workspace" banner (the site says once, near the shots,
// that the workspace is made up). Light mode: Activity, the main window
// (#sales-leaderboard), #it-help's busiest thread, the huddle running in
// #party-planning-crew and the emoji picker over #general; then #general in
// a few of Slack's theme presets (Preferences > Appearance), light and dark.
// CLACK_DEMO_SHOTS only; the files land in $CLACK_DEMO_SHOTS/site. Synthetic
// only: the demo runs fakeslack on loopback and everything in it is invented.

import (
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/demo"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/views"
)

func TestDemoSiteShots(t *testing.T) {
	root := os.Getenv("CLACK_DEMO_SHOTS")
	if root == "" {
		t.Skip("CLACK_DEMO_SHOTS not set")
	}
	dir := filepath.Join(root, "site")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	presets := map[string]theme.IATheme{}
	for _, p := range theme.ThemePresets {
		presets[p.ID] = p.Theme
	}
	shot := func(e *perfEnv, name, ch string) {
		e.movePtr(image.Pt(1250, 20))     // the empty top bar: no row hovered
		e.settle(2500 * time.Millisecond) // avatars, emoji and pictures in
		if clearStickyDay(e, ch) {
			e.movePtr(image.Pt(1250, 20))
			e.settle(time.Second)
		}
		e.shotTo(t, dir, name)
	}
	for _, c := range []struct {
		mode string
		ids  []string
	}{
		{"light", []string{"aubergine", "jade", "clementine", "lagoon", "banana", "barbra", "gray", "mood-indigo"}},
		{"dark", []string{"aubergine", "jade", "lagoon", "mood-indigo"}},
	} {
		t.Run(c.mode, func(t *testing.T) {
			e, s := demoEnvBanner(t, demo.Options{Quiet: true, Speed: 40, Seed: 3}, "")
			e.setDark(c.mode == "dark")
			e.until("the demo sidebar", 30*time.Second, func() bool { return e.sidebarHas(demo.GeneralID) && e.sidebarHas(demo.CodeID) })
			if c.mode == "light" {
				// Activity first: visiting the channels reads them
				e.a.Navigate("activity")
				e.until("Activity", 20*time.Second, func() bool { return e.a.views[Route("activity")] != nil })
				e.settle(time.Second)
				if v, ok := e.a.views[Route("activity")].(*views.Activity); ok {
					v.StepItem(1, false) // the newest item, open beside the feed
				}
				shot(e, "demo-activity", demo.GeneralID) // its mention is in #general
				e.openChannel(demo.SalesID)
				e.until("#sales-leaderboard", 20*time.Second, func() bool { return e.loadedMessages(demo.SalesID) >= 10 })
				e.toLatest()
				shot(e, "demo-main", demo.SalesID)
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
				shot(e, "demo-thread", demo.ITHelpID)
				e.a.CloseSecondary()
				e.openChannel(demo.LiveHuddleChannel)
				e.until("#party-planning-crew", 20*time.Second, func() bool { return e.loadedMessages(demo.LiveHuddleChannel) > 0 })
				e.toLatest()
				shot(e, "demo-huddle", demo.LiveHuddleChannel)
				e.openChannel(demo.GeneralID)
				e.until("#general", 20*time.Second, func() bool { return e.loadedMessages(demo.GeneralID) >= 10 })
				e.toLatest()
				e.a.OpenEmojiPicker(image.Pt(700, 700), func(string) {})
				shot(e, "demo-emoji-picker", demo.GeneralID)
				e.a.overlays = nil
			}
			e.openChannel(demo.GeneralID)
			e.until("#general", 20*time.Second, func() bool { return e.loadedMessages(demo.GeneralID) >= 10 })
			e.toLatest()
			for _, id := range c.ids {
				th, ok := presets[id]
				if !ok {
					t.Fatalf("no theme preset %q", id)
				}
				e.a.setAccountTheme(th)
				e.movePtr(image.Pt(1250, 20))
				e.settle(2500 * time.Millisecond) // the restyle, avatars, emoji and pictures in
				if got := e.a.accountTheme(); got != th.ForSave(c.mode == "dark") {
					t.Fatalf("%s: the account theme is %+v", id, got)
				}
				e.shotTo(t, dir, "demo-theme-"+id+"-"+c.mode)
			}
		})
	}
}

// clearStickyDay scrolls ch's list back a little when one of its message
// rows starts 6 to 40px under the list's top, and says whether it did. A
// day divider (40px, its pill 6px in) can be straddling the top edge
// there, and the list then draws the divider's own pill and the sticky
// one up to 34px apart (Slack's label is a single sticky element). Moved
// to 40px, the divider sits whole at the top and pushes the day before's
// sticky pill just out of sight. The demo's messages are placed from the
// clock, so where the dividers land changes with the time of the run. It
// scrolls at (640, 450), inside each of the shots' message lists and left
// of the emoji picker.
func clearStickyDay(e *perfEnv, ch string) bool {
	cv := e.channelView(ch)
	if cv == nil {
		return false
	}
	list := cv.ListRect()
	if list.Empty() {
		return false
	}
	for _, r := range cv.RowRects() {
		if y := r.R.Min.Y - list.Min.Y; y > 6 && y < 40 {
			e.scroll(image.Pt(640, 450), float32(y-40))
			e.settle(time.Second)
			return true
		}
	}
	return false
}
