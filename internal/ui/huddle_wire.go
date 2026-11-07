package ui

import "github.com/some-du6e/yoloslackgoclient/internal/ui/huddleui"

// huddles is the native huddle controller (internal/ui/huddleui): the
// sidebar huddle bar, the header's huddle button and the huddle window.
func (a *App) huddles() *huddleui.Controller {
	if a.hud == nil {
		huddleui.AppName = AppName
		a.hud = huddleui.New(a)
	}
	return a.hud
}

// StartHuddle starts or joins the huddle in a channel or DM (views'
// Huddles page calls it through its Host).
func (a *App) StartHuddle(ch string) { a.huddles().Start(ch) }
