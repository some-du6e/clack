package ui

// Preferences > Plugins (Taut's main screen over both systems, the plugin
// library with a plugin both systems have and Install's system choice, and
// Taut's Advanced, Appearance and About screens), drawn over the whole
// client signed in to
// internal/fakeslack, with synthetic Taut and Slick plugins and a local
// synthetic plugin library (plugins-shots workflow). PLUGINS_SHOTS_DIR
// gets the PNGs, light and dark. Synthetic data only: nothing reaches
// Slack or the published library.

import (
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/pluginbench"
	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// synthTaut is a Taut plugin in build:user-plugin's shape.
func synthTaut(id, name, desc, cat string, broken bool) []byte {
	start := "start() {}"
	if broken {
		start = "start() { throw new Error('synthetic failure for the screenshot') }"
	}
	return []byte(`(() => {
const TautPlugin = globalThis.TautPlugin
class ` + id + ` extends TautPlugin {
  static id = ` + jsStr(id) + `
  static pluginName = ` + jsStr(name) + `
  static description = ` + jsStr(desc) + `
  static category = ` + jsStr(cat) + `
  static authors = ['jeremy']
  static defaultConfig = { enabled: false }
  ` + start + `
  stop() {}
}
return ` + id + `
})()`)
}

// synthSlick is a Slick renderer in build:plugin's shape.
func synthSlick(id, name, desc, cat string) []byte {
	hdr, _ := json.Marshal(map[string]string{"id": id, "name": name, "description": desc})
	return []byte(`// slick-plugin ` + string(hdr) + `
(globalThis.__slickPlugins ??= []).push((require) => {
var import_slick = require("$slick");
class ` + id + ` extends import_slick.SlickPlugin {
  static id = ` + jsStr(id) + `;
  static pluginName = ` + jsStr(name) + `;
  static description = ` + jsStr(desc) + `;
  static category = ` + jsStr(cat) + `;
  static settings = {};
  static requiredAPIs = [];
  start() {}
}
return ` + id + `;
});
`)
}

type shotPlugin struct {
	system, id, name, desc, cat string
	on, broken                  bool
}

// shotInstalled are the plugins the shots have installed.
var shotInstalled = []shotPlugin{
	{pluginlib.Taut, "SilentTyping", "Silent Typing", "Don't tell others when you're typing", "messageBox", true, false},
	{pluginlib.Slick, "restrictedChannelWarning", "Restricted Channel Warning", "Warns before you post in a restricted channel", "composer", false, false},
	{pluginlib.Taut, "UserPronouns", "User Pronouns", "Shows pronouns next to names", "people", true, false},
	{pluginlib.Slick, "quickJoin", "Quick Join", "Join channels from the sidebar in one click", "channels", true, false},
	{pluginlib.Slick, "huddleSounds", "Huddle Sounds", "Custom sounds for huddle events", "sounds", false, false},
	{pluginlib.Taut, "BrokenExample", "Broken Example", "A synthetic plugin that fails to start", "app", true, true},
	{pluginlib.Taut, "Oneko", "Oneko", "A cat that follows your cursor", "fun", false, false},
}

// publishShotLibrary writes a synthetic index: Clear URLs and Silent
// Typing exist in both systems (Silent Typing is installed in Taut).
func publishShotLibrary(l *libServer) {
	src := map[string]pluginlib.Source{
		pluginlib.Taut:  {Name: "Taut", Repo: "https://github.com/jeremy46231/taut", Commit: "6fe5123e3b4cffaa", License: "GPL-3.0"},
		pluginlib.Slick: {Name: "Slick", Repo: "https://github.com/3kh0/slick", Commit: "f52d55dbd458feb0", License: "GPL-3.0"},
	}
	silent := synthTaut("SilentTyping", "Silent Typing", "Don't tell others when you're typing", "messageBox", false)
	entry := func(sys, id, name, desc, cat string, code []byte) pluginlib.Entry {
		return pluginlib.Entry{System: sys, ID: id, Name: name, Description: desc, Category: cat,
			Authors: []pluginlib.Author{{Name: "Synthetic Author"}}, File: pluginlib.File{File: sys + "-" + id + ".js", SHA256: pluginlib.Hash(code)}}
	}
	ix := pluginlib.Index{Schema: 1, Sources: src, Plugins: []pluginlib.Entry{
		entry(pluginlib.Taut, "ClearURLs", "Clear URLs", "Removes tracking parameters from links you send", "messageBox", []byte("a")),
		entry(pluginlib.Slick, "clearUrls", "ClearURLs", "Strips tracking junk from links", "privacy", []byte("b")),
		entry(pluginlib.Taut, "SilentTyping", "Silent Typing", "Don't tell others when you're typing", "messageBox", silent),
		entry(pluginlib.Slick, "silentTyping", "Silent Typing", "Hide your typing indicator", "privacy", []byte("c")),
		entry(pluginlib.Slick, "messageLinks", "Message Links", "Previews links to Slack messages", "messages", []byte("d")),
		entry(pluginlib.Taut, "Censorship", "Censorship", "Blurs words you choose", "fun", []byte("e")),
		{System: pluginlib.Slick, ID: "clubExample", Name: "Club Example", Description: "A Hack Club-only example", Category: "hackclub",
			HackClubOnly: true, File: pluginlib.File{File: "slick-clubExample.js", SHA256: pluginlib.Hash([]byte("f"))}},
	}}
	b, _ := json.Marshal(ix)
	l.mu.Lock()
	l.files["index.json"] = b
	l.mu.Unlock()
}

func TestPluginsPrefsShots(t *testing.T) {
	ls := newLibServer(t)
	publishShotLibrary(ls)
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	t.Setenv("CLACK_HOME", home) // Taut's and Slick's dirs
	e := startEnv(t, fs, home)
	dir := os.Getenv("PLUGINS_SHOTS_DIR")
	if dir != "" && e.gpu == nil {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	e.until("Taut and Slick", 60*time.Second, func() bool { return e.a.Taut() != nil && e.a.Slick() != nil })
	tt, sl := e.a.Taut(), e.a.Slick()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for _, sp := range shotInstalled {
		var err error
		if sp.system == pluginlib.Taut {
			if _, err = tt.Import(ctx, synthTaut(sp.id, sp.name, sp.desc, sp.cat, sp.broken), ""); err == nil && sp.on {
				_ = tt.SetEnabled(ctx, sp.id, true) // BrokenExample fails to start: its row shows why
			}
		} else if _, err = sl.Import(ctx, synthSlick(sp.id, sp.name, sp.desc, sp.cat)); err == nil && sp.on {
			err = sl.SetEnabled(ctx, sp.id, true)
		}
		if err != nil {
			t.Fatalf("install %s %s: %v", sp.system, sp.id, err)
		}
	}

	shot := func(name string) {
		e.settle(150 * time.Millisecond)
		if dir == "" || e.gpu == nil {
			return
		}
		img := image.NewRGBA(image.Rectangle{Max: e.size})
		if err := e.gpu.Screenshot(img); err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	var pm *prefsModal
	prefsTap = func(p *prefsModal) { pm = p }
	t.Cleanup(func() { prefsTap = nil })

	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		e.a.setDark(dark)
		e.settle(200 * time.Millisecond)
		for e.a.dismissTop() {
		}
		e.a.showPrefs(pluginsTabID)
		e.until("Preferences", 5*time.Second, func() bool { return pm != nil && pm.plugins != nil })
		pp := pm.plugins

		// Installed: both systems in one list, tagged
		ps := pp.installed()
		seen := map[string]bool{}
		for _, u := range ps {
			seen[u.System] = true
		}
		if len(ps) != len(shotInstalled) || !seen[pluginlib.Taut] || !seen[pluginlib.Slick] {
			t.Fatalf("installed list: %d rows, systems %v", len(ps), seen)
		}
		shot("plugins-installed-" + mode)

		// a search nothing installed matches suggests the library's plugins
		// that do (Clear URLs and Message Links mention links), each with
		// its Install action
		pp.s.query.SetText("links")
		e.until("library suggestions", 30*time.Second, func() bool {
			return pp.lib.ix != nil && len(pp.lib.suggest([]string{"links"})) == 2
		})
		shot("plugins-search-suggestions-" + mode)
		pp.s.query.SetText("")

		// Browse: one row per plugin name
		pp.setMode("browse")
		e.until("the library", 30*time.Second, func() bool { return pp.lib.ix != nil && !pp.lib.loading })
		var clear, silent libGroup
		for _, g := range libraryGroups(libraryEntries(pp.lib.ix)) {
			switch g.first().ID {
			case "ClearURLs":
				clear = g
			case "SilentTyping":
				silent = g
			}
		}
		if len(clear.Entries) != 2 || len(silent.Entries) != 2 {
			t.Fatalf("dual rows: Clear URLs %d entries, Silent Typing %d", len(clear.Entries), len(silent.Entries))
		}
		pp.lib.bench = &pluginbench.Data{} // no bench data: Install asks
		if act := pp.lib.action(clear); act.label != "Install…" {
			t.Errorf("Clear URLs (both systems, neither installed): %q", act.label)
		}
		if act := pp.lib.action(silent); act.label != "Settings" {
			t.Errorf("Silent Typing (installed in Taut): %q", act.label)
		}
		shot("plugins-browse-" + mode)

		// Install… on the dual row asks which system: find its button
		// with the pointer (as a user would), then click it
		b := pp.lib.s.k.btn("lib.act." + libKey(clear.first()))
		gpu := e.gpu
		e.gpu = nil // layout only while sweeping
		var at image.Point
		found := false
		for _, x := range []int{1024, 1000, 1050, 980} {
			for y := 120; y < e.size.Y-20 && !found; y += 5 {
				at = image.Pt(x, y)
				e.r.Queue(pointerMove(at))
				e.frame()
				e.frame()
				found = b.Hovered()
			}
			if found {
				break
			}
		}
		e.gpu = gpu
		if !found {
			t.Fatal("didn't find Clear URLs' Install… button")
		}
		before := len(e.a.overlays)
		e.click(at)
		e.until("the system choice", 5*time.Second, func() bool { return len(e.a.overlays) > before })
		shot("plugins-browse-install-choice-" + mode)
		e.a.dismissTop()
		e.settle(50 * time.Millisecond)

		// with plugin bench data for the row: Install installs the pick
		// (Slick here) and the chevron beside it offers both, the pick
		// first, "(recommended)", with the bench's reason
		pp.lib.bench = &pluginbench.Data{Schema: 1, Plugins: []pluginbench.Plugin{{
			Name: "Clear URLs", Winner: pluginlib.Slick, Reason: "works fully, 3.9x faster to load, 1.5x less JS memory",
			Variants: []pluginbench.Variant{
				{System: pluginlib.Taut, ID: clear.Entries[0].ID, SHA256: clear.Entries[0].SHA256},
				{System: pluginlib.Slick, ID: clear.Entries[1].ID, SHA256: clear.Entries[1].SHA256},
			},
		}}}
		if act := pp.lib.action(clear); act.label != "Install" || act.menu == nil {
			t.Errorf("Clear URLs with bench data: %q (menu %v)", act.label, act.menu != nil)
		}
		shot("plugins-browse-bench-" + mode)
		more := pp.lib.s.k.btn("lib.more." + libKey(clear.first()))
		e.gpu = nil
		found = false
		for x := at.X; x < e.size.X-4 && !found; x += 4 {
			for _, y := range []int{at.Y, at.Y - 4, at.Y + 4} {
				e.r.Queue(pointerMove(image.Pt(x, y)))
				e.frame()
				e.frame()
				if more.Hovered() {
					at, found = image.Pt(x, y), true
					break
				}
			}
		}
		e.gpu = gpu
		if !found {
			t.Fatal("didn't find Clear URLs' Install chevron")
		}
		before = len(e.a.overlays)
		e.click(at)
		e.until("the bench's Install for menu", 5*time.Second, func() bool { return len(e.a.overlays) > before })
		shot("plugins-browse-install-bench-" + mode)
		e.a.dismissTop()
		pp.lib.bench = &pluginbench.Data{}
		e.settle(50 * time.Millisecond)
		pp.setMode("installed")

		// a plugin's own page from Installed, one per system
		if !dark {
			pp.openPlugin(pluginlib.Taut, "UserPronouns")
			shot("plugins-installed-taut-plugin-" + mode)
			pp.taut.go_("main", "")
			pp.openPlugin(pluginlib.Slick, "quickJoin")
			shot("plugins-installed-slick-plugin-" + mode)
			pp.s.open = ""
		}

		// Taut's other screens: Advanced (installing, the config folders,
		// Slick's switch and URL updates, config.json), Appearance, About;
		// the old Plugin settings tab opens Advanced
		pm.selectTab(pluginSettingsTabID)
		e.settle(50 * time.Millisecond)
		if pm.section != pluginsTabID || pp.taut.screen != "advanced" {
			t.Errorf("Plugin settings opened %q / %q, want Plugins > Advanced", pm.section, pp.taut.screen)
		}
		if !strings.Contains(tt.Dir(), home) {
			t.Errorf("Taut dir %q isn't under the test home", tt.Dir())
		}
		shot("plugins-advanced-" + mode)
		pp.openScreen("appearance")
		shot("plugins-appearance-" + mode)
		pp.openScreen("about")
		shot("plugins-about-" + mode)
		pp.taut.go_("main", "")
		for e.a.dismissTop() {
		}
		pm = nil
	}
}

func pointerMove(p image.Point) pointer.Event {
	return pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))}
}

// jsStr is s as a JS string literal.
func jsStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
