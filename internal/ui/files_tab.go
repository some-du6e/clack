package ui

// The channel's Files & links tab (Slack's built-in files tab, fY.Files):
// selecting it shows the files_tab_view in the channel's body, as Slack's
// useTabView does for Xy.Files with resources_channel_tab off (the
// account's group; ResourcesTabView, the "Resources" tab, isn't ported).
// The view is views.ChannelFiles; the tab's icon is canvas-browser
// (useTabIconProps: canvas-browser-filled while selected).

import (
	"sync"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/views"
)

var filesTabs sync.Map // *App -> map[string]*views.ChannelFiles (UI goroutine only)

// filesTab is ch's Files & links view, made on first use.
func (a *App) filesTab(ch string) *views.ChannelFiles {
	m, _ := filesTabs.Load(a)
	tabs, _ := m.(map[string]*views.ChannelFiles)
	if tabs == nil {
		tabs = map[string]*views.ChannelFiles{}
		filesTabs.Store(a, tabs)
	}
	v := tabs[ch]
	if v == nil {
		v = views.NewChannelFiles(a, ch)
		tabs[ch] = v
	}
	return v
}

// isFilesTab: a tab-bar entry is the Files & links tab.
func isFilesTab(t chTab) bool { return t.typ == "files" }

// filesTabView is the body of ch's selected tab when it is Files & links.
func (a *App) filesTabView(ch string) View {
	tabs := a.channelTabs(ch)
	i := a.chTab[ch]
	if i <= 0 || i >= len(tabs) || !isFilesTab(tabs[i]) {
		return nil
	}
	return filesTabBody{v: a.filesTab(ch)}
}

type filesTabBody struct{ v *views.ChannelFiles }

func (b filesTabBody) Layout(gtx layout.Context) layout.Dimensions { return b.v.Layout(gtx) }
