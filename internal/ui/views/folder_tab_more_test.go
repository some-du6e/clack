package views

// folders-2's folder pieces (folder_tab_more.go, file_picker*.go) and the
// Pins toolbar's Add to list against internal/fakeslack's #pins-gallery
// (synthetic only): the add menu, Move to folder and Create a folder,
// uploads with their pending rows, the unified file picker, plus light
// and dark screenshots (VIEWS_SHOTS_DIR, the views-audit workflow).

import (
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// FolderTabs are #pins-gallery's folder tabs in its tab bar's order.
func (h *pfHost) FolderTabs(ch string) []FolderTabInfo {
	return []FolderTabInfo{
		{ID: fakeslack.PinsFolderTabID, Label: "Design docs", FolderID: fakeslack.PinsFolderID},
		{ID: fakeslack.PinsBookmarksTabID},
		{ID: fakeslack.PinsEmptyTabID, Label: "Later", FolderID: fakeslack.PinsEmptyFolderID},
	}
}

func (h *pfHost) OpenFolderTab(ch, tabID string) {}

func (h *pfHost) AddMessageToList(m core.Message) { h.listed = append(h.listed, m.TS) }

func pfModal(h *pfHost, title string) *popups.Modal {
	for _, o := range h.open() {
		if m, ok := o.(*popups.Modal); ok && m.Title() == title {
			return m
		}
	}
	return nil
}

func subLabels(items []popups.MenuItem) string {
	var out []string
	for _, it := range items {
		switch {
		case it.Separator:
			out = append(out, "─")
		case it.Header != "":
			out = append(out, "["+it.Header+"]")
		default:
			out = append(out, it.Label)
		}
	}
	return strings.Join(out, "|")
}

// FolderCreateMenu under unified_file_picker; the row menus with Move to
// folder; its submenu for a file (the other folders, the Bookmarks
// folder, New folder) and none for a link; a move's reorder and toast.
func TestFolderTabMoveLikeSlack(t *testing.T) {
	app, fs := pfCore(t)
	app.SetExperimentGroup("unified_file_picker", "on")
	h := pfHostFor(t, app, fs, false)
	v := NewFolderTab(h, fakeslack.PinsChannelID, FolderTabInfo{ID: fakeslack.PinsFolderTabID, Label: "Design docs", FolderID: fakeslack.PinsFolderID})
	e := &vaEnv{size: image.Pt(1100, 900)}
	pfDraw(e, h, v.Layout)
	e.until(t, "the rows", func() bool { return v.Loaded() && len(v.Rows()) == 4 })
	if got := subLabels(v.AddMenuItems()); got != "File, canvas, or list|Link|Upload file" {
		t.Errorf("add menu %q", got)
	}
	var file, link core.Bookmark
	for _, b := range v.Rows() {
		switch b.ID {
		case "Bk0PINSF1":
			file = b
		case "Bk0PINSL1":
			link = b
		}
	}
	if got := strings.Join(v.RowMenuLabels(file), "|"); got != "Copy link|─|Move to folder|─|Remove file" {
		t.Errorf("file menu %q", got)
	}
	if got := strings.Join(v.RowMenuLabels(link), "|"); got != "Copy link|Edit link|─|Move to folder|─|Delete link" {
		t.Errorf("link menu %q", got)
	}
	if m := v.MoveMenu(link); !m.SubCaret || len(m.Sub) != 0 {
		t.Errorf("a link's Move to folder: caret %v, %d rows (Slack's submenu renders nothing)", m.SubCaret, len(m.Sub))
	}
	mv := v.MoveMenu(file)
	if got := subLabels(mv.Sub); got != "[Move to…]|Bookmarks|Later|─|New folder" {
		t.Errorf("file's Move to folder %q", got)
	}
	for _, it := range mv.Sub {
		if it.Label == "Later" {
			it.Do()
		}
	}
	e.until(t, "the move", func() bool { return len(fs.PinsFoldersForms("bookmarks.reorder")) == 1 })
	rf := fs.PinsFoldersForms("bookmarks.reorder")[0]
	if rf.Get("parent_id") != fakeslack.PinsEmptyFolderID || rf.Get("center") != file.ID || rf.Get("bookmark_type") != "file" {
		t.Errorf("bookmarks.reorder %v", rf)
	}
	e.until(t, "the row gone", func() bool { return len(v.Rows()) == 3 })
	e.until(t, "the toast", func() bool {
		for _, s := range h.toasts() {
			if s == "Moved Launch plan to folder Later" {
				return true
			}
		}
		return false
	})
}

// New folder: Create a folder (bookmarks.add type folder), then the move
// into it.
func TestFolderCreateAndMoveLikeSlack(t *testing.T) {
	app, fs := pfCore(t)
	h := pfHostFor(t, app, fs, false)
	v := NewFolderTab(h, fakeslack.PinsChannelID, FolderTabInfo{ID: fakeslack.PinsFolderTabID, Label: "Design docs", FolderID: fakeslack.PinsFolderID})
	e := &vaEnv{size: image.Pt(1100, 900)}
	pfDraw(e, h, v.Layout)
	e.until(t, "the rows", func() bool { return v.Loaded() && len(v.Rows()) == 4 })
	var file core.Bookmark
	for _, b := range v.Rows() {
		if b.ID == "Bk0PINSF1" {
			file = b
		}
	}
	v.moveToNewFolder(file)
	e.frame()
	var m *popups.CreateFolderModal
	for _, o := range h.open() {
		if c, ok := o.(*popups.CreateFolderModal); ok {
			m = c
		}
	}
	if m == nil {
		t.Fatal("no Create a folder modal")
	}
	if m.CanCreate() {
		t.Error("Create is enabled with no name")
	}
	m.SetName(strings.Repeat("x", 258))
	if m.CanCreate() {
		t.Error("Create is enabled past 257 characters")
	}
	m.SetName("Specs")
	m.Create()
	e.until(t, "the folder", func() bool {
		for _, f := range fs.PinsFoldersForms("bookmarks.add") {
			if f.Get("type") == "folder" && f.Get("title") == "Specs" {
				return true
			}
		}
		return false
	})
	e.until(t, "the move into it", func() bool {
		rf := fs.PinsFoldersForms("bookmarks.reorder")
		return len(rf) == 1 && strings.HasPrefix(rf[0].Get("parent_id"), "Bk0PINSNEW") && rf[0].Get("center") == file.ID
	})
}

// Upload file: "Share these files?", a pending row per file while it
// uploads, then the file bookmarks in the folder.
func TestFolderUploadLikeSlack(t *testing.T) {
	app, fs := pfCore(t)
	h := pfHostFor(t, app, fs, false)
	v := NewFolderTab(h, fakeslack.PinsChannelID, FolderTabInfo{ID: fakeslack.PinsEmptyTabID, Label: "Later", FolderID: fakeslack.PinsEmptyFolderID})
	e := &vaEnv{size: image.Pt(1100, 900)}
	pfDraw(e, h, v.Layout)
	e.until(t, "the empty folder", func() bool { return v.Loaded() })
	v.UploadFiles([]core.UploadFile{{Name: "notes.txt", Title: "notes.txt", Data: []byte("synthetic")}, {Name: "plan.md", Title: "plan.md", Data: []byte("# synthetic")}})
	var m *popups.Modal
	e.until(t, "the share prompt", func() bool { m = pfModal(h, "Share these files?"); return m != nil })
	fs.SetFault("upload", fakeslack.Fault{Delay: 1500 * time.Millisecond, Times: 2})
	m.Press("Share")
	e.until(t, "the pending rows", func() bool { return len(v.Pending()) == 2 })
	e.frame()
	vaShot(t, e, "folder-uploading", false)
	e.until(t, "the uploaded rows", func() bool { return len(v.Pending()) == 0 && len(v.Rows()) == 2 })
	for _, f := range fs.PinsFoldersForms("bookmarks.add") {
		if f.Get("type") != "file" || f.Get("parent_id") != fakeslack.PinsEmptyFolderID {
			t.Errorf("bookmarks.add %v", f)
		}
	}
}

// The unified file picker: the tabs' counts, the type tabs, a pick, the
// share prompt and the file bookmark.
func TestFolderFilePickerLikeSlack(t *testing.T) {
	app, fs := pfCore(t)
	h := pfHostFor(t, app, fs, false)
	p := NewFolderFilePicker(h, fakeslack.PinsChannelID, fakeslack.PinsEmptyFolderID)
	h.ShowOverlay(p)
	e := &vaEnv{size: image.Pt(1280, 900)}
	pfDraw(e, h, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
	e.until(t, "the counts", func() bool {
		return p.Count("pdfs") == 1 && p.Count("spreadsheets") == 1 && p.Count("canvas") == 0 && len(p.Files()) == 2
	})
	if p.Tab() != "files" {
		t.Errorf("first tab %q", p.Tab())
	}
	p.SelectTab("pdfs")
	e.until(t, "the PDFs", func() bool { return len(p.Files()) == 1 && p.Files()[0].ID == fakeslack.PinsDocFileID })
	p.Pick(fakeslack.PinsDocFileID)
	p.Insert()
	var m *popups.Modal
	e.until(t, "the share prompt", func() bool { m = pfModal(h, "Share this file?"); return m != nil })
	if !p.Dismissed() && pfModal(h, "Share this file?") == nil {
		t.Error("no prompt")
	}
	m.Press("Share")
	e.until(t, "the bookmark", func() bool {
		for _, f := range fs.PinsFoldersForms("bookmarks.add") {
			if f.Get("type") == "file" && f.Get("entity_id") == fakeslack.PinsDocFileID && f.Get("parent_id") == fakeslack.PinsEmptyFolderID {
				return true
			}
		}
		return false
	})
	e.until(t, "the picker closed", func() bool { return p.Dismissed() })
}

// The Pins toolbar's Add to list… opens the Add to list dialog.
func TestPinsAddToListButton(t *testing.T) {
	app, fs := pfCore(t)
	h := pfHostFor(t, app, fs, false)
	v := NewChannelPins(h, fakeslack.PinsChannelID)
	e := &vaEnv{size: image.Pt(1100, 900)}
	pfDraw(e, h, v.Layout)
	e.until(t, "the pins", func() bool { return v.Loaded() && len(v.Items()) == 4 })
	it := v.Items()[0]
	v.toolbar(it, cpTbList, image.Rectangle{})
	if len(h.listed) != 1 || h.listed[0] != it.TS {
		t.Errorf("Add to list opened %v", h.listed)
	}
}

// TestFoldersMoreShots saves the add menu, a file's Move to folder, Create
// a folder, the upload prompt and failure, the file picker and its share
// prompt, light and dark (VIEWS_SHOTS_DIR).
func TestFoldersMoreShots(t *testing.T) {
	if os.Getenv("VIEWS_SHOTS_DIR") == "" {
		t.Skip("VIEWS_SHOTS_DIR not set")
	}
	for _, dark := range []bool{false, true} {
		app, fs := pfCore(t)
		app.SetExperimentGroup("unified_file_picker", "on")
		h := pfHostFor(t, app, fs, dark)
		settle := func(e *vaEnv) {
			for range 30 {
				e.frame()
				time.Sleep(15 * time.Millisecond)
			}
		}
		fv := NewFolderTab(h, fakeslack.PinsChannelID, FolderTabInfo{ID: fakeslack.PinsFolderTabID, Label: "Design docs", FolderID: fakeslack.PinsFolderID})
		fe := &vaEnv{size: image.Pt(1100, 760)}
		pfDraw(fe, h, fv.Layout)
		fe.until(t, "the folder", func() bool { return fv.Loaded() && len(fv.Rows()) == 4 })
		settle(fe)
		fv.addMenu(image.Rect(990, 20, 1018, 48), "bottom")
		fe.frame()
		vaShot(t, fe, "folder-add-menu-picker", dark)
		dismissAll(h.filesHost)
		var file core.Bookmark
		for _, b := range fv.Rows() {
			if b.ID == "Bk0PINSF1" {
				file = b
			}
		}
		fv.openRowMenu(file, image.Rect(1010, 278, 1038, 306))
		fe.frame()
		vaShot(t, fe, "folder-file-menu", dark)
		dismissAll(h.filesHost)
		mv := fv.MoveMenu(file)
		if ph, ok := fv.popupHost(); ok {
			ph.ShowOverlay(popups.NewMenuOpts(ph, mv.Sub, popups.MenuOpts{Anchor: image.Rect(760, 300, 1000, 336), Width: 240, Popover: "right-bottom"}))
		}
		fe.frame()
		vaShot(t, fe, "folder-move-submenu", dark)
		dismissAll(h.filesHost)
		fv.moveToNewFolder(file)
		fe.frame()
		vaShot(t, fe, "folder-create-folder", dark)
		dismissAll(h.filesHost)
		if ph, ok := fv.popupHost(); ok {
			ph.ShowOverlay(popups.NewFolderShareModal(ph, 2, popups.FolderShareChannel, "#pins-gallery", func() {}))
			fe.frame()
			vaShot(t, fe, "folder-share-files", dark)
			dismissAll(h.filesHost)
			ph.ShowOverlay(popups.NewFolderUploadFailedModal(ph, []string{"notes.txt", "plan.md"}))
			fe.frame()
			vaShot(t, fe, "folder-upload-failed", dark)
			dismissAll(h.filesHost)
			ph.ShowOverlay(popups.NewFolderErrorModal(ph, "too_many_bookmarks"))
			fe.frame()
			vaShot(t, fe, "folder-too-many", dark)
			dismissAll(h.filesHost)
		}
		// the picker and its share prompt
		p := NewFolderFilePicker(h, fakeslack.PinsChannelID, fakeslack.PinsEmptyFolderID)
		h.ShowOverlay(p)
		pe := &vaEnv{size: image.Pt(1280, 900)}
		pfDraw(pe, h, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
		pe.until(t, "the picker", func() bool { return len(p.Files()) == 2 && p.Count("pdfs") == 1 })
		settle(pe)
		vaShot(t, pe, "folder-file-picker", dark)
		p.Pick(fakeslack.PinsDocFileID)
		settle(pe)
		vaShot(t, pe, "folder-file-picker-picked", dark)
		p.Insert()
		pe.until(t, "the share prompt", func() bool { return pfModal(h, "Share this file?") != nil })
		settle(pe)
		vaShot(t, pe, "folder-picker-share", dark)
		dismissAll(h.filesHost)
	}
}
