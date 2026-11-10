package ui

import (
	"image"
	"net/url"
	"strings"

	"gioui.org/layout"
	"gioui.org/x/explorer"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"

	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/views"
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
func (c composerBox) SetOnFocusList(f func())                     { c.OnFocusList = f }
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
		a.explorer = e // plugins' <input type=file> (taut PickFiles)
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
		case "unreads":
			return views.NewUnreads(a)
		case "directories":
			d := views.NewDirectories(a)
			// "Browse channels" (Add channels, the ⊕ menu) and "Browse all
			// people" open their tab: directories?tab=channels
			if i, ok := directoriesTab[routeArg(r, "tab")]; ok {
				d.ShowTab(i)
			}
			// a profile field's link (popups.pfSearchPeopleByField):
			// directories?tab=people&field=<id>&value=<value>
			if f, _ := url.QueryUnescape(routeArg(r, "field")); f != "" {
				v, _ := url.QueryUnescape(routeArg(r, "value"))
				d.FilterByProfileField(f, v)
			}
			return d
		case "search":
			return popups.NewSearchView(a, r.Query())
		case "saved":
			return views.NewSaved(a)
		}
		return nil
	}
	openDetails = func(a *App, ch string) bool {
		a.ShowOverlay(detailsOverlay{d: views.NewChannelDetails(a, ch), a: a})
		return true
	}
	openDetailsTab = func(a *App, ch, tab string) bool {
		d := views.NewChannelDetails(a, ch)
		d.ShowTab(tab)
		a.ShowOverlay(detailsOverlay{d: d, a: a})
		return true
	}
	mkProfile = func(a *App, user string) View { return popups.NewProfileView(a, user) }
	onPlace = popups.RecordRecentPlace
	mkSearchInChannel = func(a *App, ch string) View { return popups.NewSearchInChannel(a, ch) }
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
	mkPrefsModal = func(a *App, title string, body layout.Widget, done func()) Overlay {
		return popups.NewModal(a, popups.ModalOpts{Title: title, Body: body, Width: 800, HeadH: 76, Flush: true, BasePry: true, OnClose: done,
			Class: "p-prefs_dialog__modal p-prefs_dialog__modal--ia_layout"}) // Slack's classes on the prefs c-sk-modal
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
		popups.OpenChannelSettings = func(ch string) { a.OpenDetailsTab(ch, "Settings") }
		popups.MoveChannel = func(ch string, anchor image.Point) {
			a.openMoveToMenu(ch, image.Rectangle{Min: anchor, Max: anchor}, true)
		}
		popups.MoveChannelItems = func(ch string) ([]popups.MenuItem, bool) {
			st := a.channelSections(ch)
			custom := false
			for _, s := range st.sections {
				custom = custom || s.Type == "standard"
			}
			return toPopupItems(a.moveToItems(ch, st)), custom
		}
		popups.CreateSectionWith = func(ch string) { a.openCreateSectionWith(ch) }
		popups.OpenPreferences = func() { a.openPrefs() }
		popups.OpenPreferencesSection = func(sec string) { a.openPrefsSection(sec) }
		popups.SignOut = func() { a.signOut() }
		popups.CheckForUpdates = a.menuCheckForUpdates()
		popups.UpdateItemLabel = a.updateMenuItem
	}
	mkStatusModal = func(a *App) Overlay { return popups.NewSetStatusModal(a) }
	mkCreateChannel = func(a *App, section string) Overlay {
		return popups.NewCreateChannelModal(a, popups.CreateChannelOpts{Section: section})
	}
	mkMenuAt = func(a *App, anchor image.Rectangle, side int, width float32, items []MenuItem) Overlay {
		return popups.NewMenuOpts(a, toPopupItems(items), popups.MenuOpts{Anchor: anchor, Side: popups.Side(side), Width: width})
	}
	mkMenu = func(a *App, anchor image.Point, items []MenuItem) Overlay {
		return popups.NewMenu(a, anchor, toPopupItems(items))
	}
}

// detailsOverlay shows views.ChannelDetails (a centered modal dialog, as
// in Slack 2026) on the overlay stack.
type detailsOverlay struct {
	d *views.ChannelDetails
	a *App
}

// Dismiss is the root's Esc: it closes the innermost layer only (a menu,
// the stacked Edit topic dialog), like Slack's topmost modal. The root
// drops the overlay right after, so one still open shows itself again.
func (o detailsOverlay) Dismiss() {
	if o.d.Escape() && o.a != nil {
		o.a.ShowOverlay(o)
	}
}

func (o detailsOverlay) Layout(gtx layout.Context) layout.Dimensions { return o.d.Layout(gtx) }
func (o detailsOverlay) Dismissed() bool                             { return o.d.Closed() }

func toPopupItems(items []MenuItem) []popups.MenuItem {
	out := make([]popups.MenuItem, 0, len(items))
	for i, it := range items {
		if it.Divider {
			out = append(out, popups.MenuItem{Separator: true})
			continue
		}
		if it.Header || it.Disabled && it.Do == nil && i == 0 && it.Sub == nil {
			out = append(out, popups.MenuItem{Key: it.Key, Header: it.Label})
			continue
		}
		var sub []popups.MenuItem
		if it.Sub != nil {
			sub = toPopupItems(it.Sub)
		}
		out = append(out, popups.MenuItem{Key: it.Key, Label: it.Label, Icon: it.Icon, Hint: it.Hint, Subtitle: it.Subtitle,
			Right: it.Right, Danger: it.Danger, Disabled: it.Disabled, Checked: it.Checked, Radio: it.Radio, Sub: sub, Do: it.Do,
			LabelText: it.LabelText})
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

// directoriesTab maps directories?tab= to the Directories tab index.
var directoriesTab = map[string]int{"people": 0, "channels": 1, "groups": 2, "usergroups": 2, "external": 3, "invitations": 4}

// routeArg is a route's name= query value ("" when absent).
func routeArg(r Route, name string) string {
	s := string(r)
	i := strings.IndexByte(s, '?')
	if i < 0 {
		return ""
	}
	for _, kv := range strings.Split(s[i+1:], "&") {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			return v
		}
	}
	return ""
}
