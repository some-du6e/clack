package ui

// The older About tab (agent_about_page off) asks for the app's profile
// once while it loads. LayoutBody calls loadAbout on every frame until the
// answer, and loadAbout had no in-flight check, so a slow apps.profile.get
// was sent again on every frame, about 30 a second with the loading
// spinner (re-review MINOR, ui f0a2a911). Synthetic only.

import (
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/blockkit"
)

func TestAppDMOldAboutAsksOnce(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Gallery)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	e.a.data.core.SetExperimentGroup(core.ExpAgentAboutPage, "off")
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	var about atomic.Int32
	fs.SetHook(func(m string, f url.Values) (any, bool) {
		if m == "apps.profile.get" {
			if f.Get("bot") == "" { // the About tab's (AppProfileOf with the app alone); the tab bar's card names the bot
				about.Add(1)
			}
			<-gate
		}
		return nil, false
	})
	t.Cleanup(func() { release(); fs.SetHook(nil) })
	e.a.Navigate("c/" + fakeslack.AppDMID)
	var dm *blockkit.AppDM
	e.until("the app DM's tabs", 20*time.Second, func() bool { dm = blockkit.AppDMFor(e.a, fakeslack.AppDMID); return dm != nil })
	dm.SetTab("about")
	e.until("the About tab's profile asked for", 10*time.Second, func() bool { return about.Load() > 0 })
	e.settle(1500 * time.Millisecond) // frames while the answer is held
	if n := about.Load(); n != 1 {
		t.Errorf("the older About tab sent %d apps.profile.get while one was out, want 1", n)
	}
	release()
	e.settle(500 * time.Millisecond)
}
