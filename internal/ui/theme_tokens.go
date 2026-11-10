package ui

import (
	"image/color"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
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
	// the primary view and its message pane (clsPrimaryView, clsMessagePane)
	pathPrimaryView = []string{"p-ia4_client_container p-ia4_client p-ia", "p-workspace__primary_view p-view_contents p-view_contents--primary"}
	pathMessagePane = []string{"p-ia4_client_container p-ia4_client p-ia", "p-workspace__primary_view p-view_contents p-view_contents--primary", "p-message_pane p-message_pane--classic-nav"}
)

// paneExpr is the expression Slack's primary surfaces are painted from
// (theme.TokPaneBg's first alternative; nothing else reads it).
const paneExpr = "rgb(var(--sk_primary_background))"

// paneTokens are the sheet tokens with the primary view's background a
// sheet paints with raw CSS (Material's `.p-view_contents--primary,
// .p-message_pane{background:var(--slick-material-background)}`): every
// pane palette (theme, message list, richview, views) reads the pane from
// paneExpr, so it answers with that background and the rest from the
// tokens.
type paneTokens struct {
	theme.Tokens // nil when no sheet declares a root token
	bg           color.NRGBA
}

func (t paneTokens) Color(expr string) (color.NRGBA, bool, bool) {
	if expr == paneExpr {
		return t.bg, true, true
	}
	if t.Tokens == nil {
		return color.NRGBA{}, false, false
	}
	return t.Tokens.Color(expr)
}

func (t paneTokens) Overridden() bool { return true }

func (t paneTokens) Key() uint64 {
	var k uint64
	if t.Tokens != nil {
		k = t.Tokens.Key()
	}
	k ^= uint64(t.bg.R)<<40 | uint64(t.bg.G)<<32 | uint64(t.bg.B)<<24 | uint64(t.bg.A)<<16 | 0x9e37
	return k*0x100000001b3 | 1
}

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
	// the pane: the message pane's own background over the primary view's
	bg, ok := tk.Background(pathMessagePane...)
	if !ok {
		bg, ok = tk.Background(pathPrimaryView...)
	}
	if ok && bg.A == 0xff {
		p.Bg = bg
		var base theme.Tokens
		if tk.Overridden() {
			base = tk
		}
		t.Tokens = paneTokens{Tokens: base, bg: bg}
	}
}
