package ui

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The Slack account's theme. Slack's 2026 client keeps the theme colours
// in users.prefs (ia_theme, window_background_gradients) and writes them
// back with users.prefs.set when you pick a theme (its
// setIaThemePrefByApi / setUserPrefByApi thunks), so Clack does the same:
// your own Slack theme shows at boot, and a theme picked in Clack shows
// in Slack. Light/dark and "Sync with OS" are not account prefs: Slack
// stores them per device in the client's localStorage ("userTheme",
// "systemThemeSyncEnabled"), so Clack keeps them in ui.json.
//
// The account theme is laid under the stylesheets' tokens (restyle), so
// Slick themes, user CSS and plugins still restyle on top of it.

type acctTheme struct {
	set       bool // a theme came from the account
	t         theme.IATheme
	gradients bool
}

// restyle is theme.Theme.Restyle: the account theme, then the sheets. It
// runs on the UI goroutine whenever the palette is rebuilt; the theme's
// colours come from theme.FieldsAsync's cache, computed off the UI
// goroutine (a custom-hex theme is ~250 HCT solves). A theme that isn't
// ready within acctThemeWait applies on a later frame instead.
func (a *App) restyle(t *theme.Theme, p *theme.Palette) {
	if a.acct.set {
		if f, ok := theme.FieldsAsync(a.acct.t, t.Dark, a.acct.gradients, acctThemeWait, a.refreshTheme); ok {
			theme.ApplyIAFields(p, f, t.Dark)
		}
	}
	restyleFromSheets(t, p)
}

// acctThemeWait bounds how long a palette rebuild waits for the account
// theme's colours (normally ~1 ms).
const acctThemeWait = 30 * time.Millisecond

// refreshTheme rebuilds the palette on the UI goroutine (any goroutine).
func (a *App) refreshTheme() {
	a.Post(func() {
		a.th.Refresh()
		a.Invalidate()
	})
}

// accountTheme is the theme Appearance shows (Slack's default when the
// account has none).
func (a *App) accountTheme() theme.IATheme {
	if a.acct.set {
		return a.acct.t
	}
	return theme.DefaultIATheme
}

// accountGradients is window_background_gradients (default on).
func (a *App) accountGradients() bool { return !a.acct.set || a.acct.gradients }

// applyAccountPrefs installs the account theme from core's prefs. UI
// goroutine.
func (a *App) applyAccountPrefs(p core.Prefs) {
	a.sw.themeFinal = true // the account's own prefs are known (startup.go)
	a.applySpellPref()     // webapp_spellcheck (spellcheck_wire.go)
	a.setAcct(acctFromPrefs(p.IATheme, p.NoWindowGradients))
	// Kept per device in ui.json, like the theme Slack's desktop keeps in
	// localStorage for its loading screen: the next start's first frame
	// (and the reveal cover) has the account's colours before
	// users.prefs answers.
	if a.prefs.AcctTheme != p.IATheme || a.prefs.AcctNoGradients != p.NoWindowGradients {
		a.prefs.AcctTheme, a.prefs.AcctNoGradients = p.IATheme, p.NoWindowGradients
		a.savePrefsSoon()
	}
}

func acctFromPrefs(ia string, noGradients bool) acctTheme {
	t, ok := theme.ParseIATheme(ia)
	n := acctTheme{set: ok, t: t, gradients: !noGradients}
	if !ok && noGradients {
		n.set, n.t = true, theme.DefaultIATheme
	}
	return n
}

func (a *App) setAcct(n acctTheme) {
	if n == a.acct {
		return
	}
	a.startupTheme(true)
	a.acct = n
	if a.th.Restyle == nil {
		a.th.Restyle = a.restyle
	}
	a.th.Refresh()
	a.Invalidate()
}

// setAccountTheme picks a theme the way Slack's Appearance does: it applies
// at once and is written to the account (users.prefs.set ia_theme) through
// setIaThemePrefByApi, so plugins see it. A failed write puts the old
// theme back.
func (a *App) setAccountTheme(t theme.IATheme) {
	t = t.ForSave(a.th.Dark)
	old := a.acct
	n := old
	n.set, n.t = true, t
	if !old.set {
		n.gradients = true
	}
	a.setAcct(n)
	args := map[string]any{}
	_ = json.Unmarshal([]byte(t.JSON()), &args)
	args["mode"] = map[bool]string{true: "dark", false: "light"}[a.th.Dark]
	a.dispatch("setIaThemePrefByApi", args, func(ctx context.Context, args map[string]any) (any, error) {
		delete(args, "mode")
		b, _ := json.Marshal(args)
		v, ok := theme.ParseIATheme(string(b))
		if !ok {
			v = t
		}
		err := a.writePref(ctx, "ia_theme", v)
		if err != nil {
			a.Post(func() {
				a.setAcct(old)
				a.Toast("Couldn't save the theme: " + err.Error())
			})
		}
		return nil, err
	})
}

// setWindowGradients is Appearance's "Show gradients"
// (window_background_gradients through setUserPrefByApi).
func (a *App) setWindowGradients(on bool) {
	old := a.acct
	n := old
	if !n.set {
		n.set, n.t = true, theme.DefaultIATheme
	}
	n.gradients = on
	a.setAcct(n)
	a.dispatch("setUserPrefByApi", map[string]any{"pref": "window_background_gradients", "value": on},
		func(ctx context.Context, args map[string]any) (any, error) {
			v, _ := args["value"].(bool)
			err := a.writePref(ctx, "window_background_gradients", v)
			if err != nil {
				a.Post(func() {
					a.setAcct(old)
					a.Toast("Couldn't save the setting: " + err.Error())
				})
			}
			return nil, err
		})
}

// writePref is core.SetPref with a timeout (no-op without a backend, e.g.
// before sign-in).
func (a *App) writePref(ctx context.Context, name string, v any) error {
	c := a.Core()
	if c == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	err := c.SetPref(ctx, name, v)
	if err != nil {
		log.Printf("ui: users.prefs.set %s: %v", name, err)
	}
	return err
}

// ---- light / dark / sync with OS (local, like Slack's localStorage) ----

// themeSyncOn is "Sync with OS setting": Slack's default is on when the
// user never picked light or dark.
func (a *App) themeSyncOn() bool {
	if a.prefs.ThemeSync != nil {
		return *a.prefs.ThemeSync
	}
	return a.prefs.Dark == nil && !a.opts.Dark
}

// settleThemeSync pins "Sync with OS setting" in the prefs at start.
// savePrefs writes "dark" on every save, and it did before theme_sync
// existed (alpha.7 and older, where light was the default), so "dark" in
// ui.json is not a choice by itself: without theme_sync, the first save
// (a route change) turned sync off for every later start, and a Mac in
// dark mode left Clack light, where Slick's dark-only themes (AMOLED,
// Catppuccin, Nord…) don't apply. An old dark:false is that default, so
// sync is on; dark:true was picked (Ctrl+Shift+Y, the menu), so it stays.
func (a *App) settleThemeSync() {
	if a.prefs.ThemeSync != nil || a.opts.Dark {
		return
	}
	on := a.prefs.Dark == nil || !*a.prefs.Dark
	a.prefs.ThemeSync = &on
}

// setThemeSync turns OS sync on or off; on applies the OS mode now.
func (a *App) setThemeSync(on bool) {
	a.prefs.ThemeSync = &on
	a.savePrefs()
	if on {
		a.followOSTheme()
	}
	a.Invalidate()
}

// pickMode is choosing Light or Dark in Appearance: it ends OS sync.
func (a *App) pickMode(dark bool) {
	off := false
	a.prefs.ThemeSync = &off
	if a.th.Dark != dark {
		a.setDark(dark)
	} else {
		a.savePrefs()
	}
}

// followOSTheme applies the OS colour scheme when syncing. It reads the
// watcher's atomic (osscheme.go): no OS call on the UI goroutine.
func (a *App) followOSTheme() {
	if !a.themeSyncOn() {
		return
	}
	if dark, ok := osSchemeDark(); ok && dark != a.th.Dark {
		a.setDark(dark)
	}
}

// watchOSTheme wakes the UI when the OS scheme changes (the platform
// watcher runs off the UI goroutine); followOSTheme applies it if sync is
// on.
func (a *App) watchOSTheme() {
	onOSScheme(func() { a.Post(a.followOSTheme) })
}
