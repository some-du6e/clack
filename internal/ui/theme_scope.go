package ui

import (
	"fmt"
	"image/color"
	"maps"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// invertedScope is Slack's class for the top nav, rail and sidebar of a
// light client that draws them dark (boot-shared y4F2 useInvertedTheme:
// the theme's brightness is dark; the sidebar also needs Darker
// sidebars), under which its theme stylesheet re-declares the
// --dt_color-theme-* tokens (oWug, mode "light-inverted-sidebar").
const invertedScope = "sk-client-theme--light-inverted-sidebar"

// scopeMod is the Mods bit that puts the top nav's natives in Slack's
// inverted scope (their ClassDefs' first mod): useInvertedTheme "chrome",
// a light client at a dark brightness (theme.IATheme.InvertedChrome).
func (a *App) scopeMod() uint64 {
	if a.scopeInv && !a.th.Dark {
		return 1
	}
	return 0
}

// sidebarScopeMod is the same bit for the sidebar's natives: "sidebar"
// inverts only with Darker sidebars on as well (InvertedSidebar).
func (a *App) sidebarScopeMod() uint64 {
	if a.scopeInvSide && !a.th.Dark {
		return 1
	}
	return 0
}

// scopeTheme is the account theme the scopes follow (focus mode's silent
// sidebar theme while it's on).
func (a *App) scopeTheme() theme.IATheme {
	if ft, ok := a.focusTheme(); ok {
		return ft
	}
	return a.accountTheme()
}

// scopeTokens updates the inverted scope's tokens from the palette restyle
// just built (the account theme in the scope's mode, with the sheets'
// tokens over it), so a plugin element in the top nav or the sidebar
// resolves var(--dt_color-theme-*) as Slack's scope does: StreamerMode's
// toggle (.p-top_nav__button, content-inv-pry) is white over the
// aubergine bar, not the root's aubergine. The chrome's tokens come from
// the chrome fields; the sidebar's from the sidebar fields when the
// sidebar is inverted too, else from the theme in the inverted mode (the
// class declares every token whichever element carries it). UI goroutine.
func (a *App) scopeTokens(t *theme.Theme, p *theme.Palette) {
	ia := a.scopeTheme()
	inv := ia.InvertedChrome(t.Dark)
	a.scopeInv, a.scopeInvSide = inv, ia.InvertedSidebar(t.Dark)
	var vars map[string]string
	if inv {
		side := *p
		if !a.scopeInvSide { // the sidebar fields are the light scope's: the inverted mode's from the measured palette
			side = theme.LightPalette
			ia.SidebarInverted = true
			if f, ok := theme.FieldsAsync(ia, false, a.accountGradients(), acctThemeWait, a.refreshTheme); ok {
				theme.ApplyIAFields(&side, f, false)
			}
		}
		vars = map[string]string{
			"--dt_color-theme-content-inv-pry": cssRGBA(p.ChromeText),
			"--dt_color-theme-content-inv-sec": cssRGBA(p.ChromeTextDim),
			"--dt_color-theme-surf-inv-pry":    cssRGBA(p.ChromeActive),
			"--dt_color-theme-surf-inv-sec":    cssRGBA(side.SidebarBg),
			"--dt_color-theme-surf-inv-ter":    cssRGBA(side.SidebarHover),
			"--dt_color-theme-otl-inv-pry":     cssRGBA(side.SidebarDivider),
			"--dt_color-theme-base-hgl-1":      cssRGBA(side.SidebarActive),
			"--dt_color-theme-content-hgl-1":   cssRGBA(side.SidebarActiveText),
			"--dt_color-theme-base-imp":        cssRGBA(p.RailBadge),
			"--dt_color-theme-content-imp":     cssRGBA(p.RailBadgeText),
		}
	}
	if maps.Equal(vars, a.scopeVars) && (vars == nil) == (a.scopeVars == nil) {
		return
	}
	a.scopeVars = vars
	slackc.SetScopeVars(invertedScope, vars)
}

func cssRGBA(c color.NRGBA) string {
	return fmt.Sprintf("rgba(%d,%d,%d,%.3g)", c.R, c.G, c.B, float64(c.A)/255)
}
