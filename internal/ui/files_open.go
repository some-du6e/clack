package ui

// The Files sidebar's Recently viewed (core files_sections.go) in the
// shell: a canvas or list opened in the main view or the side panel joins
// it, as Slack's FileOpenSubscriber does for the canvas and list views
// (boot-shared I0On maybeAddToRecentIfNotStarred; a channel tab's file
// stays out, getCurrentChannelTab). Slack keeps the list in local storage
// per team (maybeSaveOpenFilesToLocalStorage, d9Vx: {displayOrder,
// cacheOrder}); Clack keeps it in ui.json by team. It holds file ids only.

import "github.com/some-du6e/clack/internal/core"

// openFilesPrefs is one team's saved list.
type openFilesPrefs struct {
	Display []string `json:"display_order,omitempty"`
	Cache   []string `json:"cache_order,omitempty"`
}

// openFilesTeam is the team the list is kept under (the signed-in
// team's id).
func openFilesTeam(c *core.App) string {
	if c.C == nil {
		return ""
	}
	return c.C.Creds.Team
}

// attachOpenFiles restores the team's list into core and saves every
// change (on the UI goroutine, through savePrefsSoon).
func (a *App) attachOpenFiles(c *core.App) {
	team := openFilesTeam(c)
	if team == "" {
		return
	}
	if p, ok := a.prefs.OpenFiles[team]; ok {
		c.RehydrateOpenFiles(p.Display, p.Cache)
	}
	c.SetOpenFilesSaver(func(display, cache []string) {
		a.Post(func() {
			if a.prefs.OpenFiles == nil {
				a.prefs.OpenFiles = map[string]openFilesPrefs{}
			}
			if len(display) == 0 {
				delete(a.prefs.OpenFiles, team)
			} else {
				a.prefs.OpenFiles[team] = openFilesPrefs{Display: display, Cache: cache}
			}
			a.savePrefsSoon()
		})
	})
}

// fileOpenedRoute is FileOpenSubscriber for the main view: a docs/ or
// lists/ route's file.
func (a *App) fileOpenedRoute(r Route) {
	c := a.Core()
	if c == nil {
		return
	}
	if id := canvasRouteID(r); id != "" {
		c.FileOpened(id)
	} else if id := listRouteID(r); id != "" {
		c.FileOpened(id)
	}
}

// fileOpenedSide is FileOpenSubscriber for a canvas or list in the side
// panel.
func (a *App) fileOpenedSide(id string) {
	if c := a.Core(); c != nil {
		c.FileOpened(id)
	}
}
