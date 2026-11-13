package views

// The folder tab's pieces past pins-folders (docs/CHANNELS-AUDIT.md
// "Pins tab and bookmark folders"), from the web client (build
// 859cadc0e0ab, read from the public CDN without a cookie):
//
//   - The empty folder's picture: FolderTabView's empty state draws
//     empty_state_folder-7c5c7a1.svg (module adIX) 182 wide, alt "Empty
//     folder", as the first child of .emptyContainer__GsJVK (a centred
//     column 4 apart, 15vh above and below), over FolderTabEmptyFolder.
//     Clack draws it rasterised from that SVG (rsvg-convert, the drop
//     shadows and blur kept) at 182×146 and twice that; Slack serves the
//     one picture in light and dark.
//   - The add menu (FolderCreateMenu, THeG, 200 wide): with
//     unified_file_picker on (the account's group) the file row (sUmX:
//     "File, canvas, or list" while canvases and lists are on; the
//     canvas-browser icon) opening the unified file picker (zk1i, source
//     channel_folder, its picks going through
//     maybeOpenShareFileAsFolderItemConfirmationModal), Link (the Add link
//     modal) and Upload file (laptop: the platform's file chooser, many
//     files) unless the workspace disables uploads (qwL3 "disable_all").
//     The other group's Canvas, List and File rows open AddFileModal
//     (fht5), which isn't ported.
//   - Upload file and a drop onto the tab (FileDragDrop location
//     folder_tab) are uploadFilesToChannelFolder (core.CheckFolderUpload,
//     UploadFilesToFolder): ShareFileAsFolderItemErrorDialog for the cap or
//     a Slack Connect ban, the "Share this file?" prompt unless it is the
//     viewer's own DM, a FILES_UPLOADING row per file (iADM
//     FolderPendingUploadFile: the large spinner in .pendingFile__xCxJg, a
//     36px box with a 1px otl-ter line, surf-pry, radius 8, padding 8, then
//     the file's name in bold) after the folder's rows, and "We couldn’t
//     add … to the folder" naming the files that failed.
//   - The row menu's Move to folder (AddOrMoveToFolderMenuItems, 8HEe SP)
//     between two separators when the viewer may post: for a file, the
//     heading "Move to…" ("No folders available" with nowhere to go), the
//     channel's top-level folders but the bookmark's own (folder icon,
//     sorted by name, then by their tabs' order: t8EV), the channel's
//     Bookmarks folder (the folder tab without a folder id, at its place)
//     when the bookmark is in a folder, and with useCanEditTabs a separator
//     and "New folder" (plus: Create a folder, then the move). A pick is
//     onMoveBookmark (core.MoveFolderBookmark) and the 5s toast "Moved
//     {fileName} to folder <link>{folderName}</link>" whose link opens the
//     folder; a failure is ShareFileAsFolderItemErrorDialog. A link has no
//     file object, so Slack's submenu renders nothing for it: the row is
//     there with its caret and opens nothing.

import (
	"bytes"
	"context"
	_ "embed"
	"image"
	"image/png"
	"slices"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// FolderTabsHost is optionally implemented by the Host: the channel's
// folder tabs in the tab bar's order, opening one, and the unified file
// picker.
type FolderTabsHost interface {
	FolderTabs(ch string) []FolderTabInfo
	OpenFolderTab(ch, tabID string)
}

// FilePickerHost is optionally implemented by the Host: the unified file
// picker for a folder (source channel_folder); picked runs with each file
// the viewer inserts.
type FilePickerHost interface {
	OpenUnifiedFilePicker(ch, folderID string)
}

// LinkToaster is optionally implemented by the Host: a Toast whose <link>
// run opens href (the ui root's ToastLink).
type LinkToaster interface {
	ToastLink(parts []i18n.Part, href string, d time.Duration)
}

func (v *FolderTab) post(f func()) {
	if p, ok := v.h.(interface{ Post(func()) }); ok {
		p.Post(f)
		return
	}
	f()
}

func (v *FolderTab) show(o popups.Overlay) {
	if ph, ok := v.popupHost(); ok {
		v.post(func() { ph.ShowOverlay(o) })
	}
}

// fileRowLabel is sUmX Z: the add menu's file row.
func (v *FolderTab) fileRowLabel() string {
	c := v.h.Core()
	if c != nil && c.Experiment("surfaces_skills_unipicker") == "on" {
		return i18n.NS("menu_file").T("Files")
	}
	return i18n.NS("menu_file").T("File, canvas, or list")
}

// uploadsAllowed is qwL3 p != "disable_all".
func (v *FolderTab) uploadsAllowed() bool {
	c := v.h.Core()
	if c == nil {
		return true
	}
	if raw, ok := c.TeamPref("allow_box_cfs"); ok && string(raw) == "true" {
		return true
	}
	return c.TeamPrefString("disable_file_uploads") != "disable_all"
}

// AddMenuItems are FolderCreateMenu's rows (tests read them).
func (v *FolderTab) AddMenuItems() []popups.MenuItem {
	c := v.h.Core()
	var items []popups.MenuItem
	if c != nil && c.Experiment("unified_file_picker") == "on" {
		items = append(items, popups.MenuItem{Key: "file", Label: v.fileRowLabel(), Icon: "canvas-browser", Do: v.openPicker})
	}
	items = append(items, popups.MenuItem{Key: "link", Label: nsTabbed.T("Link"), Icon: "link", Do: v.openAddLink})
	if v.uploadsAllowed() {
		items = append(items, popups.MenuItem{Key: "upload", Label: nsTabbed.T("Upload file"), Icon: "laptop", Do: v.pickUpload})
	}
	return items
}

func (v *FolderTab) openPicker() {
	if fp, ok := v.h.(FilePickerHost); ok {
		fp.OpenUnifiedFilePicker(v.ch, v.tab.FolderID)
	}
}

// pickUpload is Upload file: the hidden <input type=file multiple>.
func (v *FolderTab) pickUpload() {
	if popups.PickFiles == nil {
		return
	}
	go func() {
		fs, err := popups.PickFiles("", true)
		if err != nil || len(fs) == 0 {
			return
		}
		v.UploadFiles(fs)
	}()
}

// UploadFiles is uploadFilesToChannelFolder for files (Upload file or a
// drop onto the tab). It may run on any goroutine.
func (v *FolderTab) UploadFiles(files []core.UploadFile) {
	c := v.h.Core()
	if c == nil || len(files) == 0 {
		return
	}
	ph, ok := v.popupHost()
	if !ok {
		return
	}
	ch, folder := v.ch, v.tab.FolderID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := c.CheckFolderUpload(ctx, ch, len(files))
		cancel()
		if err != nil {
			v.show(popups.NewFolderErrorModal(ph, err.Error()))
			return
		}
		submit := func() {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				if failed := c.UploadFilesToFolder(ctx, ch, folder, files); len(failed) > 0 {
					v.show(popups.NewFolderUploadFailedModal(ph, failed))
				}
				v.h.Invalidate()
			}()
		}
		if c.FolderUploadSkipsPrompt(ch) {
			submit()
			return
		}
		kind, name := popups.FolderShareChannel, "#"+v.channelName()
		if cv, ok := c.Conv(ch); ok {
			switch cv.Kind {
			case "im":
				kind, name = popups.FolderShareDM, cv.Name
			case "mpim":
				kind = popups.FolderShareMPIM
			}
		}
		v.show(popups.NewFolderShareModal(ph, len(files), kind, name, submit))
	}()
}

// ---- Move to folder ----

// folderTabs is the channel's folder tabs (the host's), in order.
func (v *FolderTab) folderTabs() []FolderTabInfo {
	if fh, ok := v.h.(FolderTabsHost); ok {
		return fh.FolderTabs(v.ch)
	}
	return nil
}

// moveTarget is a folder Move to folder offers.
type moveTarget struct {
	folderID, name, tabID string
}

// moveTargets are the submenu's folders for b, in order (t8EV, SwCK and
// AddOrMoveToFolderMenuItems' validFolders and default folder).
func (v *FolderTab) moveTargets(b core.Bookmark) []moveTarget {
	tabs := v.folderTabs()
	tabOf := func(folderID string) int {
		return slices.IndexFunc(tabs, func(t FolderTabInfo) bool { return t.FolderID == folderID })
	}
	var folders []core.Bookmark
	for _, f := range v.all {
		if f.Type == "local_folder" && f.ParentID == "" && core.ShouldShowBookmark(v.all, f) {
			folders = append(folders, f)
		}
	}
	title := func(f core.Bookmark) string { return firstNonEmpty(f.Title, f.Link) }
	slices.SortStableFunc(folders, func(a, c core.Bookmark) int {
		return strings.Compare(strings.ToLower(title(a)), strings.ToLower(title(c)))
	})
	slices.SortStableFunc(folders, func(a, c core.Bookmark) int { return tabOf(a.ID) - tabOf(c.ID) })
	def := slices.IndexFunc(tabs, func(t FolderTabInfo) bool { return t.FolderID == "" }) // SwCK folderIndex
	at := def
	var out []moveTarget
	for i, f := range folders {
		if f.ID == b.ParentID {
			if i < def {
				at--
			}
			continue
		}
		tid := ""
		if j := tabOf(f.ID); j >= 0 {
			tid = tabs[j].ID
		}
		out = append(out, moveTarget{folderID: f.ID, name: title(f), tabID: tid})
	}
	if def >= 0 && b.ParentID != "" {
		name := tabs[def].Label
		if name == "" {
			name = nsTabbed.T("Bookmarks")
		}
		at = min(max(at, 0), len(out))
		out = slices.Insert(out, at, moveTarget{name: name, tabID: tabs[def].ID})
	}
	return out
}

// MoveMenu is the row menu's Move to folder row for b.
func (v *FolderTab) MoveMenu(b core.Bookmark) popups.MenuItem {
	it := popups.MenuItem{Key: "move", Label: nsTabbed.T("Move to folder")}
	if b.Type != "file" {
		it.SubCaret = true
		return it
	}
	targets := v.moveTargets(b)
	head := nsTabbed.T("No folders available")
	if len(targets) > 0 {
		head = nsTabbed.T("Move to…")
	}
	sub := []popups.MenuItem{{Header: head}}
	for _, t := range targets {
		sub = append(sub, popups.MenuItem{Key: "move_to_folder", Label: t.name, Icon: "folder", Do: func() { v.move(b, t) }})
	}
	if v.canEditTabs() {
		sub = append(sub, popups.Sep(), popups.MenuItem{Key: "move_file_to_new_channel_folder", Label: nsTabbed.T("New folder"), Icon: "plus",
			Do: func() { v.moveToNewFolder(b) }})
	}
	it.Sub = sub
	return it
}

// fileName is the bookmark's file title (c9YI), else its title.
func (v *FolderTab) fileName(b core.Bookmark) string {
	if f, ok := v.file(b.EntityID); ok {
		return (&fileItem{File: f}).title()
	}
	return firstNonEmpty(b.Title, b.Link)
}

// move is onMoveBookmark, its toast and its error dialog.
func (v *FolderTab) move(b core.Bookmark, t moveTarget) {
	c := v.h.Core()
	if c == nil {
		return
	}
	ch := v.ch
	name := v.fileName(b)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := c.MoveFolderBookmark(ctx, ch, b, t.folderID); err != nil {
			if ph, ok := v.popupHost(); ok {
				v.show(popups.NewFolderErrorModal(ph, ""))
			}
			return
		}
		parts := nsTabbed.RT("Moved {fileName} to folder <link>{folderName}</link>", "fileName", name, "folderName", t.name)
		v.post(func() {
			if lt, ok := v.h.(LinkToaster); ok && t.tabID != "" {
				lt.ToastLink(parts, "slack://channel?id="+ch+"&tab="+t.tabID, 5*time.Second)
				return
			}
			v.h.Toast(i18n.Plain(parts))
		})
	}()
}

// moveToNewFolder is New folder: Create a folder, then the move into it.
func (v *FolderTab) moveToNewFolder(b core.Bookmark) {
	ph, ok := v.popupHost()
	c := v.h.Core()
	if !ok || c == nil {
		return
	}
	ch := v.ch
	ph.ShowOverlay(popups.NewCreateFolderModal(ph, func(ctx context.Context, name string) error {
		f, err := c.AddBookmarkFolder(ctx, ch, name)
		if err != nil {
			return err
		}
		if fh, ok := v.h.(FolderHost); ok {
			fh.TabsChanged(ch)
		}
		tid := ""
		for _, t := range v.folderTabs() {
			if t.FolderID == f.ID {
				tid = t.ID
			}
		}
		v.move(b, moveTarget{folderID: f.ID, name: firstNonEmpty(f.Title, name), tabID: tid})
		return nil
	}))
}

// ---- pending uploads ----

// pendingRow is FolderPendingUploadFile: the large spinner in its box and
// the file's name in bold, a row of the folder's list.
func (v *FolderTab) pendingRow(gtx C, p core.PendingFolderFile, i, n int) D {
	first, last := i == 0, i == n-1
	w := gtx.Constraints.Max.X
	bw := max(1, gtx.Dp(1))
	h := 2*gtx.Dp(12) + gtx.Dp(36) + bw
	if last {
		h += bw
	}
	sz := image.Pt(w, h)
	v.cf.listFrame(gtx, sz, first, last, false)
	is := gtx.Dp(36)
	ix, iy := bw+gtx.Dp(12), bw+gtx.Dp(12)
	box := image.Rect(ix, iy, ix+is, iy+is)
	rrect(gtx, box, gtx.Dp(8), theme.Over(v.p.CardLine, v.p.Surface)) // otl-ter
	rrect(gtx, box.Inset(bw), gtx.Dp(8)-bw, v.p.Surface)
	sp := gtx.Dp(20)
	dirAt(gtx, image.Pt(ix+(is-sp)/2, iy+(is-sp)/2), func(gtx C) D { return actSpinner(gtx, sp, v.p.Sec) })
	tx := ix + is + gtx.Dp(12)
	dirAt(gtx, image.Pt(tx, 0), func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(max(0, w-tx-gtx.Dp(12)), h)}
		return vmid(gtx, h, func(gtx C) D {
			return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: wBold, Color: v.p.Text, MaxLines: 1}, p.Name)
		})
	})
	if last {
		return D{Size: sz}
	}
	return D{Size: image.Pt(w, h-bw)}
}

// Pending is the folder's uploading files as last drawn (tests).
func (v *FolderTab) Pending() []core.PendingFolderFile { return v.pending }

//go:embed assets/empty_state_folder-7c5c7a1@1x.png
var emptyFolderPNG1x []byte

//go:embed assets/empty_state_folder-7c5c7a1@2x.png
var emptyFolderPNG2x []byte

var (
	emptyFolderOnce sync.Once
	emptyFolderImg  [2]paint.ImageOp
	emptyFolderOK   bool
)

// emptyFolderImage is the empty folder's picture for the screen's
// density.
func emptyFolderImage(pxPerDp float32) (paint.ImageOp, bool) {
	emptyFolderOnce.Do(func() {
		a, err1 := png.Decode(bytes.NewReader(emptyFolderPNG1x))
		b, err2 := png.Decode(bytes.NewReader(emptyFolderPNG2x))
		if err1 != nil || err2 != nil {
			return
		}
		emptyFolderImg = [2]paint.ImageOp{paint.NewImageOp(a), paint.NewImageOp(b)}
		emptyFolderOK = true
	})
	if pxPerDp > 1 {
		return emptyFolderImg[1], emptyFolderOK
	}
	return emptyFolderImg[0], emptyFolderOK
}

// emptyFolderPicture draws the picture (182×146) with its top centre at
// (cx, y) and returns its height.
func (v *FolderTab) emptyFolderPicture(gtx C, cx, y int) int {
	sz := image.Pt(gtx.Dp(182), gtx.Dp(146))
	if img, ok := emptyFolderImage(gtx.Metric.PxPerDp); ok {
		dirAt(gtx, image.Pt(cx-sz.X/2, y), func(gtx C) D { return themeImage(gtx, img, sz, 0) })
	}
	return sz.Y
}
