package ui

// The bookmark folders' host side (views.FolderTabsHost, FilePickerHost):
// the channel's folder tabs for Move to folder, opening one, and the
// unified file picker (views/file_picker.go).

import (
	"strings"

	"github.com/some-du6e/clack/internal/ui/views"
)

// FolderTabs are ch's folder tabs in the tab bar's order.
func (a *App) FolderTabs(ch string) []views.FolderTabInfo {
	var out []views.FolderTabInfo
	for _, t := range a.channelTabs(ch) {
		if isFolderTab(t) {
			out = append(out, folderInfo(t))
		}
	}
	return out
}

// OpenFolderTab opens ch's tab tabID.
func (a *App) OpenFolderTab(ch, tabID string) {
	a.openChannelTab(ch, strings.TrimPrefix(tabID, "tab:"))
}

// OpenUnifiedFilePicker is FolderCreateMenu's file row: the unified file
// picker for folder in ch.
func (a *App) OpenUnifiedFilePicker(ch, folder string) {
	a.ShowOverlay(views.NewFolderFilePicker(a, ch, folder))
}

// WindowHeight is the window's height in pixels (the views' vh margins).
func (a *App) WindowHeight() int { return a.winSize.Y }
