package ui

import (
	"image"

	"gioui.org/layout"
	"gioui.org/x/explorer"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/composer"
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

// Adapters giving the feature widgets' exported fields method access.
type channelList struct{ *messages.ChannelView }

func (c channelList) SetOrigin(p image.Point) { c.Origin = p }

type threadList struct{ *messages.ThreadView }

func (t threadList) SetOrigin(p image.Point) { t.Origin = p }

type composerBox struct{ *composer.Composer }

func (c composerBox) SetOrigin(p image.Point)                     { c.Origin = p }
func (c composerBox) SetOnEditLast(f func())                      { c.OnEditLast = f }
func (c composerBox) Layout(gtx layout.Context) layout.Dimensions { return c.Composer.Layout(gtx) }

func init() {
	mkChannelView = func(a *App, ch string) View { return channelList{messages.NewChannelView(a, ch)} }
	mkThreadView = func(a *App, ch, ts string) View { return threadList{messages.NewThreadView(a, ch, ts)} }
	mkComposer = func(a *App, ch, ts string) View { return composerBox{composer.New(a, ch, ts)} }
	mkEditComposer = func(a *App, m core.Message, onDone func()) composerView {
		return composerBox{composer.NewEdit(a, m, onDone)}
	}
	// gioui.org/x/explorer: file picker for composer attachments; it must
	// see every window event.
	onWindow = func(a *App) {
		e := explorer.NewExplorer(a.win)
		composer.SetExplorer(e)
		onEvent = e.ListenEvents
	}
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
		case "saved":
			return views.NewSaved(a)
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
		anchor = a.anchor(anchor)
		p := popups.NewEmojiPickerAt(a, anchor, onPick)
		// From a composer: Slack opens the picker above the composer box.
		for _, c := range a.convos {
			if !c.compRect.Empty() && anchor.In(c.compRect) {
				p.SetAnchorRect(c.compRect)
				break
			}
		}
		return p
	}
	mkProfileCard = func(a *App, anchor image.Point, user string) Overlay {
		return popups.NewProfileCard(a, a.anchor(anchor), user)
	}
	mkMessageMenu = func(a *App, anchor image.Point, m core.Message) Overlay {
		return popups.NewMessageMenuOpts(a, a.anchor(anchor), m, popups.MessageMenuOpts{
			OnEdit:  func() { a.editMessage(m) },
			OnReply: func() { a.OpenThread(m.Channel, threadRoot(m)) },
		})
	}
	mkLightbox = func(a *App, img richtext.Image, m core.Message) Overlay { return popups.NewLightbox(a, img, m) }
	mkAvailability = func(a *App) pagePositioned { return popups.NewAvailabilityPage(a) }
	mkModal = func(a *App, title string, body layout.Widget, done func()) Overlay {
		return popups.NewModal(a, popups.ModalOpts{Title: title, Body: body, Width: 800, OnClose: done,
			Buttons: []*popups.ModalButton{{Label: "Done", Kind: popups.BtnPrimary, Do: func() bool { return true }}}})
	}
	mkTeamMenu = func(a *App, anchor image.Point) Overlay { return popups.NewTeamMenu(a, a.anchor(anchor)) }
	mkUserMenu = func(a *App, anchor image.Point) Overlay { return popups.NewUserMenu(a, a.anchor(anchor)) }
	mkChannelMenu = func(a *App, anchor image.Point, ch string) Overlay {
		return popups.NewChannelMenu(a, a.anchor(anchor), ch)
	}
	installHooks = func(a *App) {
		popups.OpenFullProfile = func(uid string) { a.OpenUserProfile(uid) }
		popups.OpenOwnProfile = func() { a.OpenUserProfile(a.model().SelfID) }
		popups.OpenChannelDetails = func(ch string) { a.OpenDetails(ch) }
		popups.OpenPreferences = func() { a.openPrefs() }
		popups.OpenPreferencesSection = func(sec string) { a.openPrefsSection(sec) }
		popups.SignOut = func() { a.signOut() }
	}
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

// ---- views.Panes: real conversations inside the DMs and Activity tabs ----

var _ views.Panes = (*App)(nil)

// ChannelPane draws a conversation (header, tabs, history, composer).
func (a *App) ChannelPane(ch string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions { return a.layoutChannel(gtx, ch) }
}

// ThreadPane draws a thread (header, root + replies, composer).
func (a *App) ThreadPane(ch, ts string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return a.layoutConvo(gtx, a.convoFor(ch, ts), a.ptrWin)
	}
}
