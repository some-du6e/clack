package views

import (
	"image"
	"slices"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The tabs tabs-3 puts on #files-gallery: Files & links, a canvas, a list
// and two folders (Launch docs, the loose Bookmarks one).
var (
	teCanvasTab = "Ct" + fakeslack.CanvasChannel
	teListTab   = "Ct" + fakeslack.ListTodos
	teFolderTab = "Ct0FILESFLD1"
	teLooseTab  = "Ct0FILESBKS1"
)

func teTabs(fs *fakeslack.Server) {
	fs.SetGalleryTabList(
		fakeslack.GalleryTab(fakeslack.FilesTabID, "files", "", "", ""),
		fakeslack.GalleryTab(teCanvasTab, "canvas", "", fakeslack.CanvasChannel, ""),
		fakeslack.GalleryTab(teListTab, "list", "", fakeslack.ListTodos, ""),
		fakeslack.GalleryTab(teFolderTab, "folder", "Launch docs", "", fakeslack.GalleryFolder),
		fakeslack.GalleryTab(teLooseTab, "folder", "", "", ""),
	)
}

// rowButton finds Manage tabs' row (its "row:" clickable) down x=300,
// then btn along that row.
func rowButton(t *testing.T, e *vaEnv, row, btn *widget.Clickable) image.Point {
	t.Helper()
	for y := 60; y < e.size.Y; y += 4 {
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(300, float32(y))})
		e.frame()
		if row.Hovered() {
			return hoverFind(t, e, btn, y+20, 300, e.size.X-4)
		}
	}
	t.Fatal("no row")
	return image.Point{}
}

// AddBookmarkModal's folder select (showSetFolder, qSP5): the channel's
// default folder tab (SwCK) spliced among the folders and picked first,
// a divider and "New folder" with useCanEditTabs; New folder shows
// "Folder Name" and makes the folder first; with no folders and no default
// tab the banner's "Edit folder name" names the folder to make.
func TestChannelFilesFolderSelectLikeSlack(t *testing.T) {
	for _, dark := range []bool{false, true} {
		v, h, e, fs := cfView(t, dark)
		a := h.Core()
		ch := fakeslack.FilesChannelID
		fs.Permissions = map[string][]string{ch: {core.PermManageChannelTabs}}
		a.ChannelBookmarks(ch)
		e.until(t, "the bookmarks and the tab grant", func() bool {
			_, ok := a.ChannelBookmarks(ch)
			return ok && a.CanEditTabs(ch)
		})
		h.folderTabs = []FolderTabInfo{{ID: teLooseTab}, {ID: teFolderTab, Label: "Launch docs", FolderID: fakeslack.GalleryFolder}}
		l := v.links.links[0]
		v.addLinkToFolder(l)
		e.frame()
		ov := h.open()
		m := ov[len(ov)-1].(*popups.BookmarkLinkModal)
		if got, want := m.FolderOptions(), []string{"Bookmarks", "Launch docs", "─", "New folder"}; !slices.Equal(got, want) {
			t.Fatalf("options %q, want %q", got, want)
		}
		if m.Picked() != "Bookmarks" {
			t.Errorf("picked %q, want the default folder", m.Picked())
		}
		m.OpenFolderSelect()
		e.frame()
		e.frame()
		vaShot(t, e, "tabs3-folder-select", dark)
		if top := h.open(); len(top) == 0 || top[len(top)-1] == popups.Overlay(m) {
			t.Error("the select's list didn't open")
		} else {
			top[len(top)-1].(interface{ Dismiss() }).Dismiss()
		}
		e.frame()
		if dark {
			continue
		}
		// the default folder: parent_id "" (the loose bookmarks)
		m.Save()
		e.until(t, "bookmarks.add", func() bool { return len(fs.GalleryBookmarkAdds()) == 1 && m.Dismissed() })
		if got := fs.GalleryBookmarkAdds()[0]; got.Get("parent_id") != "" || got.Get("type") != "link" {
			t.Errorf("into the default folder: %v", got)
		}
		e.until(t, "the toast", func() bool {
			ts := h.toasts()
			return len(ts) > 0 && ts[len(ts)-1] == "Added "+l.Title+" to folder Bookmarks"
		})
		// New folder: Folder Name, the folder first, then the link in it
		v.addLinkToFolder(v.links.links[1])
		e.frame()
		ov = h.open()
		m = ov[len(ov)-1].(*popups.BookmarkLinkModal)
		m.PickFolder(3)
		e.frame()
		if !m.Naming() || m.CanSave() {
			t.Errorf("New folder: naming %v, can save %v without a name", m.Naming(), m.CanSave())
		}
		m.SetFolderName("Specs")
		e.frame()
		vaShot(t, e, "tabs3-new-folder", dark)
		m.Save()
		e.until(t, "the folder and the link", func() bool { return len(fs.GalleryBookmarkAdds()) == 3 && m.Dismissed() })
		adds := fs.GalleryBookmarkAdds()
		if adds[1].Get("type") != "folder" || adds[1].Get("title") != "Specs" || adds[2].Get("parent_id") == "" {
			t.Errorf("new folder adds %v", adds[1:])
		}
	}
	// no folders, no default tab: the banner, Edit folder name
	v, h, e, fs := cfView(t, false)
	fs.SetGalleryFolders(true)
	a := h.Core()
	ch := fakeslack.FilesChannelID
	a.ChannelBookmarks(ch)
	e.until(t, "no bookmarks", func() bool { _, ok := a.ChannelBookmarks(ch); return ok })
	v.addLinkToFolder(v.links.links[0])
	e.frame()
	ov := h.open()
	m := ov[len(ov)-1].(*popups.BookmarkLinkModal)
	if m.Naming() || !m.CanSave() {
		t.Fatalf("the banner: naming %v, can save %v", m.Naming(), m.CanSave())
	}
	m.EditFolderName()
	e.frame()
	if !m.Naming() || m.CanSave() {
		t.Errorf("Edit folder name: naming %v, can save %v with the name cleared", m.Naming(), m.CanSave())
	}
	m.SetFolderName("Refs")
	e.frame()
	vaShot(t, e, "tabs3-edit-folder-name", false)
	m.Save()
	e.until(t, "the folder and the link", func() bool { return len(fs.GalleryBookmarkAdds()) == 2 })
	if adds := fs.GalleryBookmarkAdds(); adds[0].Get("title") != "Refs" || adds[0].Get("type") != "folder" {
		t.Errorf("adds %v", adds)
	}
}

// Manage tabs (ManageChannelTabsListItem): a canvas or folder row's Rename
// tab (edit) opens RenameTabModal ("Rename tab", Tab name) and saves with
// conversations.updateTab; a canvas, list or folder row's visibility slot
// is trash "Delete tab" (useRemoveTab: a list shared nowhere else asks
// "Also remove access to this list?", Keep keeps the share).
func TestManageTabsRenameDeleteLikeSlack(t *testing.T) {
	for _, dark := range []bool{false, true} {
		_, h, _, fs := cfView(t, dark)
		ch := fakeslack.FilesChannelID
		fs.Permissions = map[string][]string{ch: {core.PermManageChannelTabs}}
		teTabs(fs)
		d := NewChannelDetails(h, ch)
		e := &vaEnv{size: image.Pt(700, 900)}
		e.draw = func(gtx layout.Context) {
			d.Layout(gtx)
			for _, o := range h.open() {
				o.Layout(gtx)
			}
		}
		e.frame()
		d.ShowTab("Tabs")
		m := d.manageTabs()
		e.until(t, "the tabs and the grant", func() bool {
			o, ok, _ := m.order.get()
			_, tok, _ := d.tabList.get()
			return ok && tok && len(o) == 5 && h.Core().CanEditTabs(ch)
		})
		ren := dirClick(m.clk, "rename:"+teCanvasTab)
		p := rowButton(t, e, dirClick(m.clk, "row:"+teCanvasTab), ren)
		vaShot(t, e, "tabs3-manage-tabs", dark)
		e.click(p)
		e.until(t, "Rename tab", func() bool {
			ov := h.open()
			if len(ov) == 0 {
				return false
			}
			r, ok := ov[len(ov)-1].(*popups.RenameTabModal)
			return ok && r.Title() == "Rename tab"
		})
		ov := h.open()
		r := ov[len(ov)-1].(*popups.RenameTabModal)
		if r.Label() == "" {
			t.Error("the field doesn't start with the canvas's title")
		}
		r.SetLabel("")
		if !r.CanSave() {
			t.Error("a canvas's empty name (its file's name again) can't save")
		}
		r.SetLabel("Roadmap")
		e.frame()
		vaShot(t, e, "tabs3-rename-tab", dark)
		r.Save()
		e.until(t, "conversations.updateTab", func() bool { return fs.GalleryTabLabels()[teCanvasTab] == "Roadmap" })
		if dark {
			continue
		}
		// Delete tab on the list: shared only here, so it asks; Keep
		fs.SetGalleryHasShares(false)
		del := dirClick(m.clk, "vis:"+teListTab)
		p = rowButton(t, e, dirClick(m.clk, "row:"+teListTab), del)
		e.click(p)
		e.until(t, "the remove-access question", func() bool {
			ov := h.open()
			if len(ov) == 0 {
				return false
			}
			mm, ok := ov[len(ov)-1].(*popups.Modal)
			return ok && mm.Title() == "Also remove access to this list?"
		})
		vaShot(t, e, "tabs3-remove-access", false)
		ov = h.open()
		ov[len(ov)-1].(*popups.Modal).Press("Keep")
		e.until(t, "removeTab with keep_file_share", func() bool {
			for _, w := range fs.TabWrites() {
				if w.Get("method") == "conversations.removeTab" && w.Get("id") == teListTab && w.Get("keep_file_share") == "true" {
					return true
				}
			}
			return false
		})
		if slices.Contains(fs.GalleryTabs(), teListTab) {
			t.Errorf("the list tab is still there: %v", fs.GalleryTabs())
		}
	}
}
