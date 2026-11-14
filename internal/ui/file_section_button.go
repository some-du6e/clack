package ui

// The canvas and list headers' FileSectionDropdownButton (boot-shared
// ofyN; custom_file_sections): canvasview and listview draw it, the
// Files page's FileSectionMoveToMenu (views.SectionMenuItems) is its menu.

import (
	"image"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/views"
)

// fileSectionButton is the button's face for fileID; ok false without
// custom_file_sections (FileSectionDropdownButton renders null).
func (a *App) fileSectionButton(fileID string) (look int, emoji string, ok bool) {
	c := a.Core()
	if c == nil || !c.FileSectionsOn() {
		return 0, "", false
	}
	l, u := views.SectionButtonFor(c, fileID)
	return int(l), u, true
}

// openFileSection opens the button's menu under btn.
func (a *App) openFileSection(f core.File, btn image.Rectangle, source string) {
	views.OpenSectionMenu(a, f, btn, source)
}

// fileSectionItems is the same menu as shell rows (a submenu's).
func (a *App) fileSectionItems(f core.File, source string) []MenuItem {
	return fromPopupItems(views.SectionMenuItems(a, f, source))
}

// fromPopupItems is toPopupItems the other way.
func fromPopupItems(items []popups.MenuItem) []MenuItem {
	out := make([]MenuItem, 0, len(items))
	for _, it := range items {
		switch {
		case it.Separator:
			out = append(out, MenuItem{Divider: true})
			continue
		case it.Header != "":
			out = append(out, MenuItem{Key: it.Key, Label: it.Header, Header: true})
			continue
		}
		var sub []MenuItem
		if it.Sub != nil {
			sub = fromPopupItems(it.Sub)
		}
		out = append(out, MenuItem{Key: it.Key, Label: it.Label, Icon: it.Icon, Hint: it.Hint, Subtitle: it.Subtitle,
			Right: it.Right, Danger: it.Danger, Disabled: it.Disabled, Checked: it.Checked, Radio: it.Radio, Sub: sub, Do: it.Do,
			LabelText: it.LabelText, Indeterminate: it.Indeterminate, IconURL: it.IconURL})
	}
	return out
}
