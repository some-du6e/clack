package ui

// TestDemoSiteShots takes the website's theme screenshots (site/, see
// site/README.md): `clack demo`'s made-up workspace in a 1440x900 window,
// #general in a few of Slack's theme presets (Preferences > Appearance),
// light and dark. CLACK_DEMO_SHOTS only; the files land in
// $CLACK_DEMO_SHOTS/site as demo-theme-<preset>-<mode>.png. Synthetic only:
// the demo runs fakeslack on loopback and everything in it is invented.

import (
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/demo"
	"github.com/some-du6e/clack/internal/ui/theme"
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
	for _, c := range []struct {
		mode string
		ids  []string
	}{
		{"light", []string{"aubergine", "jade", "clementine", "lagoon", "banana", "barbra", "gray", "mood-indigo"}},
		{"dark", []string{"aubergine", "jade", "lagoon", "mood-indigo"}},
	} {
		t.Run(c.mode, func(t *testing.T) {
			e, _ := demoEnv(t, demo.Options{Quiet: true, Speed: 40, Seed: 3})
			e.setDark(c.mode == "dark")
			e.until("the demo sidebar", 30*time.Second, func() bool { return e.sidebarHas(demo.GeneralID) && e.sidebarHas(demo.CodeID) })
			e.openChannel(demo.GeneralID)
			e.until("#general", 20*time.Second, func() bool { return e.loadedMessages(demo.GeneralID) >= 10 })
			e.toLatest()
			for _, id := range c.ids {
				th, ok := presets[id]
				if !ok {
					t.Fatalf("no theme preset %q", id)
				}
				e.a.setAccountTheme(th)
				e.movePtr(image.Pt(1000, 55))     // on the banner: no row hovered
				e.settle(2500 * time.Millisecond) // the restyle, avatars, emoji and pictures in
				if got := e.a.accountTheme(); got != th.ForSave(c.mode == "dark") {
					t.Fatalf("%s: the account theme is %+v", id, got)
				}
				e.shotTo(t, dir, "demo-theme-"+id+"-"+c.mode)
			}
		})
	}
}
