package ui

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"

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
		case "directories":
			return views.NewDirectories(a)
		case "search":
			return popups.NewSearchView(a, r.Query())
		}
		return nil
	}
	openDetails = func(a *App, ch string) bool {
		a.ShowOverlay(detailsOverlay{views.NewChannelDetails(a, ch)})
		return true
	}
	mkProfile = func(a *App, user string) View { return popups.NewProfileView(a, user) }
	mkSwitcher = func(a *App) Overlay { return popups.NewQuickSwitcher(a) }
	mkSearch = func(a *App, q string) Overlay { return popups.NewSearchPopover(a, q, a.searchPill) }
	mkEmojiPicker = func(a *App, anchor image.Point, onPick func(string)) Overlay {
		return popups.NewEmojiPickerAt(a, anchor, onPick)
	}
	mkProfileCard = func(a *App, anchor image.Point, user string) Overlay { return popups.NewProfileCard(a, anchor, user) }
	mkMessageMenu = func(a *App, anchor image.Point, m core.Message) Overlay { return popups.NewMessageMenu(a, anchor, m) }
	mkLightbox = func(a *App, img richtext.Image, m core.Message) Overlay { return popups.NewLightbox(a, img, m) }
	mkTeamMenu = func(a *App, anchor image.Point) Overlay { return popups.NewTeamMenu(a, anchor) }
	mkUserMenu = func(a *App, anchor image.Point) Overlay { return popups.NewUserMenu(a, anchor) }
	mkChannelMenu = func(a *App, anchor image.Point, ch string) Overlay { return popups.NewChannelMenu(a, anchor, ch) }
	mkStatusModal = func(a *App) Overlay { return popups.NewSetStatusModal(a) }
	mkMenu = func(a *App, anchor image.Point, items []MenuItem) Overlay {
		return popups.NewMenu(a, anchor, toPopupItems(items))
	}
}

// detailsOverlay shows views.ChannelDetails (a centered modal dialog, as
// in Slack 2026) on the overlay stack.
type detailsOverlay struct{ d *views.ChannelDetails }

func (o detailsOverlay) Layout(gtx layout.Context) layout.Dimensions { return o.d.Layout(gtx) }
func (o detailsOverlay) Dismissed() bool                             { return o.d.Closed() }

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
