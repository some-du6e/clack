package ui

// Preferences > Slick and the Slick wiring over a synthetic workspace.
// TestSlickNoPluginsStartup and TestDeepLinkRoute always run;
// TestSlickPrefsRender needs Slick's plugins built by the ui-taut workflow
// (SLICK_PLUGINS_DIR, with Slick's themes/ copied in) and writes PNGs to
// TAUT_SHOTS_DIR when set.

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/pluginlib"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

func TestSlickNoPluginsStartup(t *testing.T) {
	a, _ := ttApp(t, t.TempDir())
	t0 := time.Now()
	a.data.startTaut(a)
	d := time.Since(t0)
	t.Cleanup(func() { a.data.stopTaut() })
	s := a.Slick()
	if s == nil {
		t.Fatal("slick didn't start")
	}
	t.Logf("startTaut + slick.New with no plugins: %v", d)
	if a.Taut().Started() || s.Started() {
		t.Fatal("a JS runtime started with no plugins")
	}
	// every Slick tab draws from the Go model, without JS
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	var ids []string // the plugin tabs, both Plugins modes, and the old ids
	for _, tab := range pluginNav {
		ids = append(ids, tab.id)
	}
	for _, id := range append(ids, libraryTabID, "taut", "slick", "slick_plugins", "slick_install") {
		p.selectTab(id)
		gtx := ttContext(new(op.Ops), image.Pt(800, 560))
		a.slackcFrame(gtx)
		p.layout(gtx)
	}
	p.close()
	if a.Taut().Started() || s.Started() {
		t.Fatal("drawing Preferences > Slick started JS")
	}
	// route and theme reach Slick
	a.slickRoute(Route("c/" + ttGen))
	a.setDark(true)
	a.dispatchWait()
	if !a.th.Dark {
		t.Error("setDark didn't apply")
	}
}

// dispatchWait lets dispatched natives (goroutines) post, then drains.
func (a *App) dispatchWait() {
	time.Sleep(100 * time.Millisecond)
	a.drain()
}

func TestDeepLinkRoute(t *testing.T) {
	for _, c := range []struct {
		uri, route, user string
		ok               bool
	}{
		{"slack://channel?team=T1&id=C123", "c/C123", "", true},
		{"slack://channel?team=T1&id=C123&thread_ts=1700000000.000100", "c/C123/t/1700000000.000100", "", true},
		{"slack://channel?team=T1&id=C123&message=1700000000.000100", "c/C123", "", true},
		{"slack://user?team=T1&id=U42", "", "U42", true},
		{"slack://open?team=T1", "home", "", true},
		{"slack://channel?team=T1", "", "", false},
		{"slack://app?team=T1&id=A1", "", "", false},
		{"https://example.com", "", "", false},
	} {
		r, u, ok := deepLinkRoute(c.uri)
		if r != c.route || u != c.user || ok != c.ok {
			t.Errorf("%s: %q %q %v, want %q %q %v", c.uri, r, u, ok, c.route, c.user, c.ok)
		}
	}
}

func TestSlickPrefsRender(t *testing.T) {
	pdir := os.Getenv("SLICK_PLUGINS_DIR")
	if pdir == "" {
		t.Skip("SLICK_PLUGINS_DIR not set: the ui-taut workflow builds Slick's plugins and runs this")
	}
	home := t.TempDir()
	// Slick's bundled themes come from <slick dir>/themes (Clack doesn't ship them)
	if themes, _ := filepath.Glob(filepath.Join(pdir, "themes", "*.json")); len(themes) > 0 {
		dst := filepath.Join(home, "slick", "themes")
		if err := os.MkdirAll(dst, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, f := range themes {
			b, _ := os.ReadFile(f)
			_ = os.WriteFile(filepath.Join(dst, filepath.Base(f)), b, 0o600)
		}
	}
	a, _ := ttApp(t, home)
	a.data.startTaut(a)
	t.Cleanup(func() { a.data.stopTaut() })
	s := a.Slick()
	if s == nil {
		t.Fatal("slick didn't start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	shots := os.Getenv("TAUT_SHOTS_DIR")
	r := &ttRenderer{t: t, a: a, sz: image.Pt(800, 1600), dir: shots}
	if shots != "" {
		w, err := pixeltest.NewWindow(r.sz.X, r.sz.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		r.win = w
		defer w.Release()
	}
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, section: pluginsTabID}
	defer p.close()
	draw := func(gtx layout.Context) { p.layout(gtx) }
	// tall draws a plugin tab's page alone, 600 wide (the dialog's page width).
	tall := func(tab string) func(gtx layout.Context) {
		return func(gtx layout.Context) {
			gtx.Constraints = layout.Exact(image.Pt(600, r.sz.Y))
			p.section1(gtx, tab, image.Point{})
		}
	}
	// the plugin tabs: Plugins > Installed and > Browse, Themes, Plugin settings
	shotTabs := func(suffix string) {
		for _, tab := range []struct{ id, name, mode string }{
			{pluginsTabID, "plugins-installed", "installed"}, {pluginsTabID, "plugins-browse", "browse"},
			{themesTabID, "themes", ""}, {pluginSettingsTabID, "plugin-settings", ""},
		} {
			p.section = tab.id
			if tab.mode != "" {
				p.pluginsPage().setMode(tab.mode)
			}
			r.save("slick-"+tab.name+"-"+suffix, r.settle(draw, 0, nil))
			r.save("slick-"+tab.name+"-"+suffix+"-full", r.settle(tall(tab.id), 0, nil), r.sz.Y)
		}
		p.pluginsPage().setMode("installed")
	}
	shotTabs("empty-light")

	files, _ := filepath.Glob(filepath.Join(pdir, "*.js"))
	var ids []string
	for _, f := range files {
		b := filepath.Base(f)
		if strings.HasSuffix(b, ".main.js") || strings.HasSuffix(b, ".user.js") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(b, ".js"))
	}
	sort.Strings(ids)
	t0 := time.Now()
	for _, id := range ids {
		for _, f := range []string{id + ".js", id + ".main.js"} {
			code, err := os.ReadFile(filepath.Join(pdir, f))
			if err != nil {
				continue // no main half
			}
			if _, err := s.Import(ctx, code); err != nil {
				t.Errorf("import %s: %v", f, err)
			}
		}
	}
	t.Logf("imported %d Slick plugins in %v", len(ids), time.Since(t0))

	// Installed, all off: a fresh start loads cached metadata, no JS.
	for _, pl := range s.Settings().Plugins {
		if pl.Enabled {
			_ = s.SetEnabled(ctx, pl.ID, false)
		}
	}
	a.data.stopTaut()
	t0 = time.Now()
	a.data.startTaut(a)
	t.Logf("startTaut + slick.New with %d Slick plugins installed, none on: %v", len(ids), time.Since(t0))
	if s = a.Slick(); s == nil {
		t.Fatal("slick didn't restart")
	}
	if a.Taut().Started() || s.Started() {
		t.Error("JS started with every Slick plugin off")
	}
	p.close()
	p.slick, p.plugins, p.settings, p.taut = nil, nil, nil, nil
	shotTabs("off-light")

	t0 = time.Now()
	for _, pl := range s.Settings().Plugins {
		if err := s.SetEnabled(ctx, pl.ID, true); err != nil {
			t.Logf("enable %s: %v", pl.ID, err)
		}
	}
	t.Logf("enabled %d Slick plugins in %v", len(ids), time.Since(t0))
	r.settle(draw, 30*time.Second, func() bool {
		for _, pl := range s.Settings().Plugins {
			if pl.Enabled && !pl.Running && pl.StartError == "" {
				return false
			}
		}
		return true
	})
	kinds := map[string]int{}
	running := 0
	p.section = pluginsTabID
	pp := p.pluginsPage()
	for _, pl := range s.Settings().Plugins {
		if pl.Running {
			running++
		}
		if pl.StartError != "" {
			t.Logf("%s: %s", pl.ID, pl.StartError)
		}
		for _, o := range pl.Options {
			kinds[o.Type]++
		}
		pp.openPlugin(pluginlib.Slick, pl.ID)
		r.save("slick-plugin-"+safeName(pl.ID), r.settle(tall(pluginsTabID), time.Second, nil), r.sz.Y)
	}
	t.Logf("%d/%d running; setting kinds %v", running, len(ids), kinds)
	// a schedule rule with its days popover open, and a names entry
	for _, pl := range s.Settings().Plugins {
		for _, o := range pl.Options {
			switch o.Type {
			case "schedule":
				status := ""
				if len(o.Statuses) > 0 {
					status = o.Statuses[0].Value
				}
				rules := []any{
					map[string]any{"days": []any{1, 2, 3, 4, 5}, "start": "09:00", "end": "17:30", "status": status},
					map[string]any{"days": []any{0, 6}, "start": "22:00", "end": "08:00", "status": status},
				}
				if err := s.SetOption(ctx, pl.ID, o.Key, rules); err != nil {
					t.Errorf("schedule %s: %v", pl.ID, err)
				}
				pp.openPlugin(pluginlib.Slick, pl.ID)
				r.settle(tall(pluginsTabID), time.Second, nil)
				pp.s.daysOpen = pl.ID + "/" + o.Key + ".0"
				r.save("slick-schedule-"+safeName(pl.ID), r.settle(tall(pluginsTabID), time.Second, nil), r.sz.Y)
				pp.s.daysOpen = ""
			case "names":
				if err := s.SetOption(ctx, pl.ID, o.Key, map[string]any{"UALICE01": "Alice Example", "UBOB0002": "Bobby"}); err != nil {
					t.Errorf("names %s: %v", pl.ID, err)
				}
				pp.openPlugin(pluginlib.Slick, pl.ID)
				r.save("slick-names-"+safeName(pl.ID)+"-"+o.Key, r.settle(tall(pluginsTabID), time.Second, nil), r.sz.Y)
			}
		}
	}
	pp.s.open = ""
	for _, dark := range []bool{false, true} {
		a.th.SetDark(dark)
		s.SetDark(dark)
		mode := "light"
		if dark {
			mode = "dark"
		}
		shotTabs("on-" + mode)
	}
	a.th.SetDark(false)
	// pick a bundled theme and back
	for _, th := range s.Settings().Themes {
		if th.ID == "nord" {
			if err := s.SetTheme("nord"); err != nil {
				t.Error(err)
			}
			p.section = themesTabID
			r.save("slick-themes-nord-selected", r.settle(tall(themesTabID), 0, nil), 900)
			_ = s.SetTheme("")
		}
	}
}
