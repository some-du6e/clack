package ui

// The Files & links tab's other ways out (files-links-2,
// docs/FILES-AUDIT.md "Files & links tab, round 2"): the media viewer's
// gallery, the link details pane and the tab bar's right-click menu.

import (
	"context"
	"image"
	"strings"
	"sync"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/views"
)

// secLinkDetails is the secondary pane's kind for LinkDetailsView.
const secLinkDetails secKind = 67

var tabCtxTags sync.Map // *App -> map[string]*int: the tab bar's right-click tags (UI goroutine only)

// OpenMediaGallery opens the media viewer on items at idx (the Files &
// links tab's fileChannelMediaInfo; views.ChannelFiles.openMedia).
func (a *App) OpenMediaGallery(items []popups.GalleryItem, idx int) {
	if lb := popups.NewFileGallery(a, items, idx); lb != nil {
		a.ShowOverlay(lb)
	}
}

// OpenLinkDetails shows LinkDetailsView for url in the secondary pane
// (toLinkDetails: the view's id is link_details, so a second link
// replaces the first).
func (a *App) OpenLinkDetails(url, title, icon string) {
	v := views.NewLinkDetails(a, url, title, icon)
	v.Close = a.CloseSecondary
	a.secondary = secondaryState{kind: secLinkDetails, width: a.secondary.width}
	a.secondary.view = v
	a.Invalidate()
}

// LinkDetailsShown is the link the secondary pane's link details show
// ("" for none).
func (a *App) LinkDetailsShown() string {
	if v, ok := a.secondary.view.(*views.LinkDetails); ok && a.secondary.kind == secLinkDetails && a.secondary.open() {
		return v.URL()
	}
	return ""
}

// tabCtx registers the right-click (or macOS Ctrl+click) on a tab of the
// bar at r: the tab's context menu (useTabSchemaToTabItems'
// getTabContextMenu).
func (a *App) tabCtx(gtx layout.Context, ch string, t chTab, r image.Rectangle) {
	m, _ := tabCtxTags.Load(a)
	tags, _ := m.(map[string]*int)
	if tags == nil {
		tags = map[string]*int{}
		tabCtxTags.Store(a, tags)
	}
	k := ch + "." + t.key
	tag := tags[k]
	if tag == nil {
		tag = new(int)
		tags[k] = tag
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && ctxclick.Press(e) {
			a.openTabMenu(ch, t, a.ptrWin)
		}
	}
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, tag)
}

// openTabMenu is the tab's context menu at the pointer (Slack shows none
// when the template is empty).
func (a *App) openTabMenu(ch string, t chTab, at image.Point) {
	items := a.tabMenuItems(ch, t)
	if len(items) == 0 {
		return
	}
	a.ShowOverlay(popups.NewMenuOpts(a, items, popups.MenuOpts{Anchor: image.Rectangle{Min: at, Max: at.Add(image.Pt(1, 1))},
		Width: 240}))
}

// editTabOf is the bar's tab as views' rename and remove flows take it.
func editTabOf(t chTab) views.EditTab {
	e := views.EditTab{ID: strings.TrimPrefix(t.key, "tab:"), Type: t.typ, Label: t.rawLabel, FileID: t.fileID, FolderID: t.folder}
	if t.fileID != "" {
		e.FileTitle = t.label
	}
	return e
}

// tabMenuItems is getTabContextMenu (useTabSchemaToTabItems, H9Rh) for
// the tabs Clack draws, t its useCanEditTabs:
//
//   - a folder: Add link (with canUserPostInChannel and membership: the
//     Add link modal into the folder);
//   - the "administrative-options" group: Rename tab, or Rename folder,
//     with t for the types the rename table (cO) has;
//   - the "remove" group with t for a tab that isn't static: Hide tab for
//     a built-in one (isKnownChannelTab), Delete folder for a folder, else
//     Remove tab, all danger (useRemoveTab).
//
// Separators open each group; Slack Kit drops a leading one. Add file
// (AddFileModal), Copy link to canvas / list, Add or Move to folder, Canvas
// options, the folder's post updates toggle, Change default list view
// and the split / secondary view and new window items aren't ported.
func (a *App) tabMenuItems(ch string, t chTab) []popups.MenuItem {
	c := a.Core()
	if c == nil || t.key == "messages" {
		return nil
	}
	nsT := i18n.NS("channel_tabs")
	et := editTabOf(t)
	can := c.CanEditTabs(ch)
	folder := t.typ == "folder"
	var items []popups.MenuItem
	if folder && c.CanEditBookmarks(ch) {
		items = append(items, popups.MenuItem{Key: "add_link", Label: nsT.T("Add link"), Do: func() { a.addLinkToFolderTab(ch, et) }})
	}
	if can && core.RenameableTab(t.typ) {
		label := nsT.T("Rename tab")
		if folder {
			label = nsT.T("Rename folder")
		}
		items = append(items, popups.Sep(), popups.MenuItem{Key: "rename", Label: label, Do: func() { views.OpenRenameTab(a, ch, et, folder) }})
	}
	if can && !core.StaticTabType(t.typ) {
		label := nsT.T("Remove tab")
		switch {
		case core.KnownTabID(et.ID):
			label = nsT.T("Hide tab")
		case folder:
			label = nsT.T("Delete folder")
		}
		items = append(items, popups.Sep(), popups.MenuItem{Key: "remove", Label: label, Danger: true, Do: func() { views.RemoveTab(a, ch, et) }})
	}
	for len(items) > 0 && items[0].Separator {
		items = items[1:]
	}
	return items
}

// addLinkToFolderTab is the folder menu's Add link: the Add link modal
// (modalHeaderTitle "Add link", fromChannelFolder) with the folder as
// parent; a save opens the folder (onBookmarkAdded).
func (a *App) addLinkToFolderTab(ch string, t views.EditTab) {
	c := a.Core()
	m := popups.NewBookmarkLinkModal(a, popups.BookmarkLinkOpts{Title: i18n.NS("channel_tabs").T("Add link"),
		Save: func(ctx context.Context, link, name string) error {
			_, err := c.AddFolderLink(ctx, ch, t.FolderID, link, name)
			return err
		}})
	m.OnSaved = func() { a.openChannelTab(ch, t.ID) }
	a.ShowOverlay(m)
}

// SetTabLabel is views.TabLabelHost: updateTabLabel's label at once in
// the bar (a canvas without one shows its file's title again).
func (a *App) SetTabLabel(ch, id string, label string) {
	if a.data == nil {
		return
	}
	tabs := a.data.tabCache[ch]
	for i := range tabs {
		if tabs[i].key != "tab:"+id {
			continue
		}
		title := label
		if title == "" && tabs[i].fileID != "" && tabs[i].rawLabel == "" {
			title = tabs[i].label // the file's title already
		}
		tabs[i].rawLabel, tabs[i].labeled = label, label != ""
		if title != "" {
			tabs[i].label = title
			tabs[i].spans = tabLabelSpans(title, a.Core().Emoji)
		}
	}
	a.Invalidate()
}
