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

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
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
	for _, tab := range slickTabs {
		p.section = tab.id
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
		w, err := headless.NewWindow(r.sz.X, r.sz.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		r.win = w
		defer w.Release()
	}
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, section: "slick"}
	defer p.close()
	draw := func(gtx layout.Context) { p.layout(gtx) }
	// tall draws a Slick tab alone, 600 wide (the dialog's page width).
	tall := func(tab string) func(gtx layout.Context) {
		return func(gtx layout.Context) {
			gtx.Constraints = layout.Exact(image.Pt(600, r.sz.Y))
			if p.slick == nil {
				p.slick = newSlickPage(a)
			}
			p.slick.layout(gtx, tab)
		}
	}
	shotTabs := func(suffix string) {
		for _, tab := range slickTabs {
			p.section = tab.id
			r.save("slick-"+tab.id+"-"+suffix, r.settle(draw, 0, nil))
			r.save("slick-"+tab.id+"-"+suffix+"-full", r.settle(tall(tab.id), 0, nil), r.sz.Y)
		}
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
	p.slick = nil
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
	p.section = "slick_plugins"
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
		p.slick.open = pl.ID
		r.save("slick-plugin-"+safeName(pl.ID), r.settle(tall("slick_plugins"), time.Second, nil), r.sz.Y)
	}
	t.Logf("%d/%d running; setting kinds %v", running, len(ids), kinds)
	p.slick.open = ""
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
			p.section = "slick_themes"
			r.save("slick-themes-nord-selected", r.settle(tall("slick_themes"), 0, nil), 900)
			_ = s.SetTheme("")
		}
	}
}
