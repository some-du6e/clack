package ui

import (
	"fmt"
	"image/color"
	"maps"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// invertedScope is Slack's class for the chrome and sidebar of a light
// client whose sidebar is dark (y4F2 useInvertedTheme: the theme's
// brightness is dark), under which its theme stylesheet re-declares the
// --dt_color-theme-* tokens (oWug, mode "light-inverted-sidebar").
const invertedScope = "sk-client-theme--light-inverted-sidebar"

// scopeMod is the Mods bit that puts the top nav's and the sidebar's
// natives in Slack's inverted scope (their ClassDefs' first mod).
func (a *App) scopeMod() uint64 {
	if a.scopeInv && !a.th.Dark {
		return 1
	}
	return 0
}

// scopeTokens updates the inverted scope's tokens from the palette restyle
// just built (the account theme in the scope's mode, with the sheets'
// tokens over it), so a plugin element in the top nav or the sidebar
// resolves var(--dt_color-theme-*) as Slack's scope does: StreamerMode's
// toggle (.p-top_nav__button, content-inv-pry) is white over the
// aubergine bar, not the root's aubergine. UI goroutine.
func (a *App) scopeTokens(t *theme.Theme, p *theme.Palette) {
	ia := a.accountTheme()
	if ft, ok := a.focusTheme(); ok {
		ia = ft
	}
	inv := !t.Dark && ia.SidebarInverted
	a.scopeInv = inv
	var vars map[string]string
	if inv {
		vars = map[string]string{
			"--dt_color-theme-content-inv-pry": cssRGBA(p.ChromeText),
			"--dt_color-theme-content-inv-sec": cssRGBA(p.ChromeTextDim),
			"--dt_color-theme-surf-inv-pry":    cssRGBA(p.ChromeActive),
			"--dt_color-theme-surf-inv-sec":    cssRGBA(p.SidebarBg),
			"--dt_color-theme-surf-inv-ter":    cssRGBA(p.SidebarHover),
			"--dt_color-theme-otl-inv-pry":     cssRGBA(p.SidebarDivider),
			"--dt_color-theme-base-hgl-1":      cssRGBA(p.SidebarActive),
			"--dt_color-theme-content-hgl-1":   cssRGBA(p.SidebarActiveText),
			"--dt_color-theme-base-imp":        cssRGBA(p.Badge),
			"--dt_color-theme-content-imp":     cssRGBA(p.BadgeText),
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
