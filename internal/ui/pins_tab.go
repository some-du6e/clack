package ui

// The channel's Pins tab and its bookmark folder tabs, drawn in the
// channel's body as Slack's useTabView does for Xy.Bookmarks (the pin
// icon's "Pins": oG, views.ChannelPins) and Xy.Folder (FolderTabView,
// views.FolderTab); docs/CHANNELS-AUDIT.md "Pins tab" and "Bookmark
// folders".

import (
	"strings"
	"sync"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/views"
)

var (
	pinsTabs   sync.Map // *App -> map[string]*views.ChannelPins (UI goroutine only)
	folderTabs sync.Map // *App -> map[string]*views.FolderTab, by channel/tab
)

// isPinsTab: a tab-bar entry is the Pins tab (Slack's "bookmarks" type).
func isPinsTab(t chTab) bool { return t.typ == "bookmarks" }

// isFolderTab: a tab-bar entry is a bookmark folder.
func isFolderTab(t chTab) bool { return t.typ == "folder" }

// pinsTab is ch's Pins view, made on first use.
func (a *App) pinsTab(ch string) *views.ChannelPins {
	m, _ := pinsTabs.Load(a)
	tabs, _ := m.(map[string]*views.ChannelPins)
	if tabs == nil {
		tabs = map[string]*views.ChannelPins{}
		pinsTabs.Store(a, tabs)
	}
	v := tabs[ch]
	if v == nil {
		v = views.NewChannelPins(a, ch)
		tabs[ch] = v
	}
	return v
}

// folderInfo is a folder tab's view input.
func folderInfo(t chTab) views.FolderTabInfo {
	label := ""
	if t.labeled {
		label = t.label
	}
	return views.FolderTabInfo{ID: strings.TrimPrefix(t.key, "tab:"), Label: label, FolderID: t.folder}
}

// folderTab is ch's view of folder tab t, made on first use.
func (a *App) folderTab(ch string, t chTab) *views.FolderTab {
	m, _ := folderTabs.Load(a)
	tabs, _ := m.(map[string]*views.FolderTab)
	if tabs == nil {
		tabs = map[string]*views.FolderTab{}
		folderTabs.Store(a, tabs)
	}
	k := ch + "/" + t.key
	v := tabs[k]
	if v == nil {
		v = views.NewFolderTab(a, ch, folderInfo(t))
		tabs[k] = v
	} else {
		v.SetTab(folderInfo(t))
	}
	return v
}

// selectedTab is ch's selected tab past Messages, when there is one.
func (a *App) selectedTab(ch string) (chTab, bool) {
	tabs := a.channelTabs(ch)
	i := a.chTab[ch]
	if i <= 0 || i >= len(tabs) {
		return chTab{}, false
	}
	return tabs[i], true
}

// pinsTabView is the body of ch's selected tab when it is Pins.
func (a *App) pinsTabView(ch string) View {
	t, ok := a.selectedTab(ch)
	if !ok || !isPinsTab(t) {
		return nil
	}
	return viewBody{a.pinsTab(ch).Layout}
}

// folderTabView is the body of ch's selected tab when it is a folder.
func (a *App) folderTabView(ch string) View {
	t, ok := a.selectedTab(ch)
	if !ok || !isFolderTab(t) {
		return nil
	}
	return viewBody{a.folderTab(ch, t).Layout}
}

type viewBody struct {
	f func(gtx layout.Context) layout.Dimensions
}

func (b viewBody) Layout(gtx layout.Context) layout.Dimensions { return b.f(gtx) }

// OpenPinnedMessage is openPinnedItem's navigation (views.PinOpener): the
// message's permalink opens its thread for a reply, else the channel's
// Messages with the list at the message.
func (a *App) OpenPinnedMessage(ch, ts, threadTS string) {
	if threadTS != "" && threadTS != ts {
		a.OpenThread(ch, threadTS)
		return
	}
	if a.chTab != nil {
		a.chTab[ch] = 0
	}
	if a.router.Current().Channel() != ch {
		a.Navigate("c/" + ch)
	}
	a.FocusMessage(ch, ts)
	a.Invalidate()
}

// openBookmarkURL opens a link bookmark in the browser (tests swap it).
var openBookmarkURL = openURL

// OpenBookmark is openBookmark for a folder's link (views.BookmarkOpener):
// a workflow bookmark trips (workflows_wire.go), a channel tab's link
// opens the tab, anything else the browser.
func (a *App) OpenBookmark(ch string, b core.Bookmark) {
	if a.tripBookmark(ch, b) {
		return
	}
	if c, tab := channelTabOfLink(b.Link); c != "" {
		a.openChannelTab(c, tab)
		return
	}
	if b.Link != "" {
		openBookmarkURL(b.Link)
	}
}

// TabsChanged re-reads ch's tab bar (views.FolderHost: a folder renamed).
func (a *App) TabsChanged(ch string) {
	a.Post(func() {
		if a.data == nil {
			return
		}
		delete(a.data.fetched, ch)
		if a.router.Current().Channel() == ch {
			a.data.opened(ch)
		}
	})
}

// TabRemoved is useRemoveTab's navigation: the channel goes back to
// Messages and its tab bar is read again.
func (a *App) TabRemoved(ch, tabID string) {
	a.Post(func() {
		if a.chTab != nil {
			if t, ok := a.selectedTab(ch); ok && t.key == "tab:"+tabID {
				a.chTab[ch] = 0
			}
		}
	})
	a.TabsChanged(ch)
}
