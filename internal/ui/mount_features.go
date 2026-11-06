package ui

import (
	"image"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/messages"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/views"
)

// One *App satisfies every feature package's Host.
var (
	_ messages.Host = (*App)(nil)
	_ views.Host    = (*App)(nil)
	_ popups.Host   = (*App)(nil)
)

func init() {
	mkChannelView = func(a *App, ch string) View { return messages.NewChannelView(a, ch) }
	mkThreadView = func(a *App, ch, ts string) View { return messages.NewThreadView(a, ch, ts) }
	mkPage = func(a *App, r Route) View {
		switch r.Page() {
		case "activity":
			return views.NewActivity(a)
		case "dms":
			return views.NewDMs(a)
		case "drafts":
			return views.NewDrafts(a)
		case "files":
			return views.NewFiles(a)
		case "huddles":
			return views.NewHuddles(a)
		case "later":
			return views.NewLater(a)
		case "threads":
			return views.NewThreads(a)
		}
		return nil
	}
	mkMenu = func(a *App, anchor image.Point, items []MenuItem) Overlay {
		return popups.NewMenu(a, anchor, toPopupItems(items))
	}
}

func toPopupItems(items []MenuItem) []popups.MenuItem {
	out := make([]popups.MenuItem, 0, len(items))
	for i, it := range items {
		if it.Divider {
			out = append(out, popups.MenuItem{Separator: true})
			continue
		}
		if it.Disabled && it.Do == nil && i == 0 {
			out = append(out, popups.MenuItem{Header: it.Label})
			continue
		}
		out = append(out, popups.MenuItem{Label: it.Label, Icon: it.Icon, Hint: it.Hint, Danger: it.Danger, Disabled: it.Disabled, Do: it.Do})
	}
	return out
}
