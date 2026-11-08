package ui

// The Slack account theme (users.prefs ia_theme) over a fake Slack:
// boot reads it, Appearance writes it back like Slack's client, and
// stylesheet themes still win over it.

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// fakeApp is an App on a fake Slack whose prefs the test sets.
func fakeApp(t *testing.T, prefs map[string]any) (*App, *core.App, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.SetPrefs(prefs)
	ca := core.New(slack.New(fs.Creds()), store.New(""))
	if _, err := ca.RefreshPrefs(context.Background()); err != nil {
		t.Fatal(err)
	}
	images := imgcache.New(func(ctx context.Context, url string) ([]byte, error) { return nil, errors.New("offline") }, "", nil)
	a := New(Options{ConfigDir: t.TempDir(), NoRealtime: true}, images)
	sh := theme.NewShaper()
	a.th.SetShapers(sh, sh)
	a.data = &backend{core: ca, app: a}
	a.th.Restyle = a.restyle
	return a, ca, fs
}

// waitTheme drains posted work until ok or a timeout.
func waitTheme(t *testing.T, a *App, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
		a.drain()
	}
	a.drain()
}

const ochinJSON = `{"primary":{"palette":"ocean"},"highlight1":{"palette":"ocean"},"highlight2":{"palette":"cilantro"},"important":{"palette":"mojito"},"brightness":5,"sidebarInverted":false,"useCustomHex":false}`

func TestAccountThemeFromPrefs(t *testing.T) {
	a, ca, _ := fakeApp(t, map[string]any{"ia_theme": ochinJSON})
	if ca.Prefs().IATheme != ochinJSON {
		t.Fatalf("store ia_theme = %q", ca.Prefs().IATheme)
	}
	a.applyAccountPrefs(ca.Prefs())
	c := a.th.C
	for _, w := range []tkWant{
		{"ChromeA", c.ChromeA, tkHex(0x1264a3)},
		{"ChromeB", c.ChromeB, tkHex(0x093a73)},
		{"SidebarActive", c.SidebarActive, tkHex(0x0b4c8c)},
		{"Badge", c.Badge, tkHex(0x1a8343)},
		{"Accent", c.Accent, tkHex(0x1264a3)},
	} {
		if w.got != w.want {
			t.Errorf("%s = %v, want %v", w.name, w.got, w.want)
		}
	}
	if c.Bg != theme.LightPalette.Bg {
		t.Error("the message pane changed")
	}
	// dark mode follows the theme's dark ramps
	a.th.SetDark(true)
	ocean := tkHex(0x073368) // --dt_color-plt-ocean-10, dark
	if a.th.C.ChromeA != ocean {
		t.Errorf("dark ChromeA = %v, want %v", a.th.C.ChromeA, ocean)
	}
	// a pref_change to the default theme puts Clack's measured palette back
	a.th.SetDark(false)
	a.applyAccountPrefs(core.Prefs{IATheme: theme.DefaultIATheme.JSON()})
	if a.th.C != theme.Flatten(theme.LightPalette) {
		t.Error("the default account theme doesn't give the default palette")
	}
	// gradients off: one flat colour
	a.applyAccountPrefs(core.Prefs{IATheme: ochinJSON, NoWindowGradients: true})
	if a.th.C.ChromeA != a.th.C.ChromeB {
		t.Error("window_background_gradients=false still draws a gradient")
	}
}

// A stylesheet theme (Slick, user.css) still restyles over the account
// theme: its tokens win where it sets them.
func TestAccountThemeUnderStylesheets(t *testing.T) {
	th, _ := theme.ParseIATheme(ochinJSON)
	eng := css.NewEngine()
	eng.Set("slick:theme", nordCSS)
	tt := theme.Bare(true)
	tt.Restyle = func(t *theme.Theme, p *theme.Palette) {
		theme.ApplyIATheme(p, th, t.Dark, true)
		t.Tokens = nil
		restylePalette(eng, t, p)
	}
	tt.Refresh()
	if tt.C.ChromeA != tkHex(0x2e3440) || tt.C.SidebarBg != tkHex(0x3b4252) {
		t.Errorf("the stylesheet lost to the account theme: %v %v", tt.C.ChromeA, tt.C.SidebarBg)
	}
}

// Picking a theme in Appearance writes ia_theme with users.prefs.set the
// way Slack's setIaThemePrefByApi does, and reads back through core.
func TestAccountThemeWriteBack(t *testing.T) {
	a, ca, fs := fakeApp(t, nil)
	a.applyAccountPrefs(ca.Prefs())
	var lagoon theme.ThemePreset
	for _, p := range theme.ThemePresets {
		if p.ID == "lagoon" {
			lagoon = p
		}
	}
	a.setAccountTheme(lagoon.Theme)
	if a.accountTheme() != lagoon.Theme { // applied at once
		t.Fatalf("not applied: %+v", a.accountTheme())
	}
	waitTheme(t, a, "ia_theme on the fake", func() bool {
		v, _ := fs.Pref("ia_theme")
		s, _ := v.(string)
		got, ok := theme.ParseIATheme(s)
		return ok && got == lagoon.Theme
	})
	if got, _ := theme.ParseIATheme(ca.PrefString("ia_theme")); got != lagoon.Theme {
		t.Errorf("core reads back %+v", got)
	}
	if fs.CallCount("users.prefs.set") != 1 {
		t.Errorf("%d users.prefs.set calls", fs.CallCount("users.prefs.set"))
	}
	// dark mode: Slack never saves an inverted sidebar from dark mode
	a.th.SetDark(true)
	a.setAccountTheme(lagoon.Theme)
	waitTheme(t, a, "sidebarInverted=false", func() bool {
		v, _ := fs.Pref("ia_theme")
		s, _ := v.(string)
		return strings.Contains(s, `"sidebarInverted":false`)
	})
	a.th.SetDark(false)

	a.setWindowGradients(false)
	waitTheme(t, a, "window_background_gradients", func() bool {
		v, _ := fs.Pref("window_background_gradients")
		return v == false
	})
	if ca.PrefBool("window_background_gradients", true) || !ca.Prefs().NoWindowGradients {
		t.Error("core still has gradients on")
	}
}

// A failed write puts the old theme back.
func TestAccountThemeWriteFails(t *testing.T) {
	a, ca, fs := fakeApp(t, nil)
	a.applyAccountPrefs(ca.Prefs())
	fs.Close() // the write can't reach Slack
	before := a.accountTheme()
	a.setAccountTheme(theme.ThemePresets[3].Theme)
	waitTheme(t, a, "revert", func() bool { return a.accountTheme() == before })
}

// plt_ramps.go is generated from slackvars.go: keep them in step.
func TestPltRampsMatchSlackVars(t *testing.T) {
	shades := []string{"0", "5", "10", "20", "30", "40", "50", "60", "70", "80", "90", "100"}
	for _, dark := range []bool{false, true} {
		vars := css.SlackVars(dark)
		for _, name := range theme.PaletteNames {
			for i, s := range shades {
				want := strings.ReplaceAll(vars["--dt_color-plt-"+name+"-"+s], " ", "")
				c := theme.PaletteShade(name, i, dark)
				if got := fmt.Sprintf("%d,%d,%d", c.R, c.G, c.B); got != want {
					t.Errorf("dark=%v %s-%s = %s, slackvars %s", dark, name, s, got, want)
				}
			}
		}
	}
}

// Appearance draws for a default, a named and a custom-hex theme, in both
// modes, and its rows are clickable.
func TestAppearancePageDraws(t *testing.T) {
	a, ca, _ := fakeApp(t, map[string]any{"ia_theme": `{"primary":{"palette":"aubergine","custom":"#2D0033"},"highlight1":{"palette":"grass","custom":"#F9EDFF"},"highlight2":{"palette":"grass","custom":"#20A271"},"important":{"palette":"aubergine","custom":"#EABDFB"},"brightness":6,"sidebarInverted":true,"useCustomHex":true}`})
	a.applyAccountPrefs(ca.Prefs())
	for _, dark := range []bool{false, true} {
		a.th.SetDark(dark)
		p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, section: "appearance"}
		gtx := ttContext(new(op.Ops), image.Pt(800, 624))
		a.slackcFrame(gtx)
		p.layout(gtx)
		for _, k := range []string{"mode.light", "mode.dark", "mode.os", "preset.lagoon", "role.primary.ocean", "gradients", "bright.4"} {
			if _, ok := p.clicks[k]; !ok {
				t.Errorf("dark=%v: no %s row", dark, k)
			}
		}
	}
}

// "Sync with OS setting" survives saves and restarts. savePrefs writes
// "dark" every time (and did before theme_sync existed), so without
// pinning theme_sync the first save turned sync off for every later
// start, and a Mac in dark mode left Clack light: Slick's dark-only
// themes (AMOLED, Catppuccin, Nord…) then never applied.
func TestThemeSyncSurvivesSaves(t *testing.T) {
	images := imgcache.New(func(ctx context.Context, url string) ([]byte, error) { return nil, errors.New("offline") }, "", nil)
	open := func(dir string) *App { return New(Options{ConfigDir: dir, NoRealtime: true}, images) }
	for _, c := range []struct {
		name, uiJSON string
		sync         bool
	}{
		{"fresh", "", true},
		{"alpha.7 default (dark:false written by every save)", `{"dark":false,"route":"c/C1"}`, true},
		{"alpha.7 dark picked", `{"dark":true}`, false},
		{"sync off picked", `{"dark":false,"theme_sync":false}`, false},
		{"sync on picked, OS dark", `{"dark":true,"theme_sync":true}`, true},
	} {
		dir := t.TempDir()
		if c.uiJSON != "" {
			if err := os.WriteFile(filepath.Join(dir, "ui.json"), []byte(c.uiJSON), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		a := open(dir)
		if a.themeSyncOn() != c.sync {
			t.Errorf("%s: sync %v at start", c.name, a.themeSyncOn())
		}
		// the OS went dark (sync on), then any save, then a restart
		if c.sync {
			a.th.SetDark(true)
		}
		a.savePrefs()
		if b := open(dir); b.themeSyncOn() != c.sync || b.th.Dark != a.th.Dark {
			t.Errorf("%s: after a save and a restart sync %v dark %v", c.name, b.themeSyncOn(), b.th.Dark)
		}
	}
	// Ctrl+Shift+Y is a choice, like Appearance's Light/Dark: sync ends.
	a := open(t.TempDir())
	a.ToggleDark()
	if a.themeSyncOn() {
		t.Error("toggling dark mode kept syncing with the OS")
	}
}
