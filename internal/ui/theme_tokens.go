package ui

import (
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Native surfaces follow the stylesheets' Slack design tokens (Slick
// themes, user.css, plugin styles) the way Slack's own stylesheet paints
// from them: see theme.ApplyTokens for the token behind each palette
// field. Two surfaces bundled themes repaint with raw CSS instead of
// tokens, .p-theme_background (the window gradient) and
// .p-view_contents--sidebar, are read from the cascade under their Slack
// class names; the sidebar is also a real native element of that class so
// rules and token overrides scoped to it reach the sidebar's rows.

var clsSidebarView = slackc.Class("p-view_contents p-view_contents--sidebar")

// Slack class paths (outermost first) under .p-client_container.
var (
	pathThemeBg     = []string{"p-ia4_client_container p-ia4_client p-ia", "p-theme_background"}
	pathSidebarView = []string{"p-ia4_client_container p-ia4_client p-ia", "p-view_contents p-view_contents--sidebar"}
)

// syncThemeTokens re-derives the palette when a stylesheet changed: one
// atomic load per frame otherwise.
func (a *App) syncThemeTokens() {
	if a.th.Restyle == nil {
		a.th.Restyle = a.restyle
	}
	if v := slackc.Styles.Version(); v != a.styleVer {
		a.styleVer = v
		a.th.Refresh()
	}
}

// restyleFromSheets is theme.Theme.Restyle: it lays the active sheets'
// root tokens and chrome backgrounds over the mode's palette. With no
// sheet declaring any of them p is left exactly as it was.
func restyleFromSheets(t *theme.Theme, p *theme.Palette) {
	t.Tokens = nil
	if slackc.Styles.Empty() {
		return
	}
	restylePalette(slackc.Styles, t, p)
}

func restylePalette(eng *css.Engine, t *theme.Theme, p *theme.Palette) {
	tk := eng.RootTokens(css.Env{Dark: t.Dark})
	if tk.Overridden() {
		t.Tokens = tk
		theme.ApplyTokens(p, tk, t.Dark)
	}
	if sb := tk.Scope(pathSidebarView...); sb != tk {
		theme.ApplySidebarTokens(p, sb)
	}
	if c, ok := tk.Background(pathThemeBg...); ok {
		p.ChromeA, p.ChromeB, p.ChromeC = c, c, c
	}
	if c, ok := tk.Background(pathSidebarView...); ok {
		p.SidebarBg = c
	}
}
