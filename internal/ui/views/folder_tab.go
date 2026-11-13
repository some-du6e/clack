package views

// A channel's bookmark folder tab (Slack's "folder" tab type) and its
// peek, from the web client (build 859cadc0e0ab, read from the public CDN
// without a cookie; docs/CHANNELS-AUDIT.md "Bookmark folders"):
//
//   - useTabView's Xy.Folder view is FolderTabView (use-tab-view chunk
//     5Itp): .container__Itvf4.containerPadding__v2oe4 (base-sec, 20
//     above and 24 under) over useFolderRows (M+uF, core.FolderRows) for
//     the tab's data.folderBookmarkId, the folder named by the tab's label
//     or "Bookmarks".
//   - FolderTabViewHeader (.headingChannelFolders__itAUi: 22px Black, at
//     most 968 wide, padding-left 4, 12 under): the name, as
//     EditableFolderTitle when useCanEditTabs (.editButton__j4fSI: 36
//     tall, radius 8, padding 0 8; base-ter with the edit icon on hover; a
//     click edits it in place, Enter or leaving saves a changed name of at
//     most 256 characters: updateTabLabel reason "folder_tab_view_header",
//     Escape drops it); on the right (4 apart) FolderCreateMenu's add
//     button when the viewer may post (plus-filled, white on #007a5a,
//     #148567 hovered) and "More folder actions" (ellipsis-vertical-filled,
//     bordered) when useCanEditTabs: Rename folder (the Rename folder
//     modal, updateTabLabel), a separator, Delete folder (danger:
//     TabDeleteConfirmationModal when the folder has bookmarks, else
//     conversations.removeTab at once).
//   - Rows: a link is FolderListLink (zZVP): a compact Entity (padding 12,
//     12 apart) with its icon (the emoji, else the 36px favicon padded 4
//     with radius 8, else link 20 on a foreground .1 tile, radius 4), the
//     title or the URL over the host, and its More actions (top 2, right
//     2, padding 4) always shown in the tab (forceShowActionsBar); a file
//     is ContextualFile with its thumbnail, title and "Shared by …". The
//     rows share their lines (.folderListFile__PmmtZ: 1px otl-ter, no
//     bottom line but on the last; radius 12 on the first's top and the
//     last's bottom). A click opens the bookmark (openBookmark: the link,
//     or the file).
//   - More actions (FolderListBookmarkMenu): Copy link ("Link copied to
//     clipboard"), and when the viewer may post: Edit link (links: the Add
//     link modal titled "Edit link"), a separator, Move to folder ▸
//     (folder_tab_more.go), a separator, Delete link / Remove file (danger:
//     DeleteBookmarkModal).
//   - Reorder (FolderListDraggableBookmark) when the viewer may post: a
//     row dragged onto another shows the drop line (2px base-inv-hgl-1,
//     radius 4, 2 in, at the target's top when moving up, its bottom when
//     moving down; the dragged row at .5 opacity) and drops into its
//     place (core.ReorderFolderBookmark: onReorderBookmarkInFolder).
//   - Empty: the header, then .emptyContainer__GsJVK (centred, 4 apart,
//     15vh above and below) with the empty folder's picture
//     (folder_tab_more.go) and FolderTabEmptyFolder's EmptyState: "Add
//     files and links to reference later", "Everyone in {channelName} will
//     have access to what you add here." and the add button.
//   - The peek (FolderTabPeek, boot-shared q5Cv): the rows without header
//     as .listItem__hftW3 (primary background, a 1px bottom line but on
//     the last, padding-right 4) whose click opens the bookmark;
//     contentHeight 315 loading (five 63px skeletons), 250 empty
//     (FolderTabEmptyState: the indigo-0 box with its dashed indigo-20
//     border, radius 8, 32 16 12 12 around, padding 16 12; the
//     screen-share-collab-on icon in indigo-70, "Add files and links to
//     reference later" 13px Bold and "Everyone in {channelName} can see
//     them. Drag or add files, lists, and canvases to keep things
//     organized." 13px in content-sec, then the "Add" button), else 63
//     per row.
//
// The add menu, Move to folder, uploads and drops, and the empty state's
// picture are folder_tab_more.go. Not ported: the folder peek's megaphone
// banner and the workflows-in-folder alert's banner art.

import (
	"context"
	"image"
	"image/color"
	"io"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/clipboard"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// FolderHost is optionally implemented by the Host: the tab bar's
// refresh after a rename (TabsChanged) and the move back to Messages
// once the tab is gone (TabRemoved).
type FolderHost interface {
	TabsChanged(ch string)
	TabRemoved(ch, tabID string)
}

// BookmarkOpener is optionally implemented by the Host: openBookmark for
// a link (a workflow trips, a Slack link navigates, the rest open in the
// browser).
type BookmarkOpener interface {
	OpenBookmark(ch string, b core.Bookmark)
}

// FolderTabInfo is the tab a folder view shows.
type FolderTabInfo struct {
	ID, Label string
	FolderID  string // data.folder_bookmark_id ("" the loose bookmarks)
	AppID     string // data.app_id (the Channel Expert's folder: folder_tab_more.go)
}

type ftRow struct {
	row, menu widget.Clickable
	menuSize  image.Point
	menuTip   popups.TipState
	drag      gesture.Drag
	dragging  bool
	dragStart f32.Point
	dragY     float32
}

// FolderTab is a folder tab's view (or, with preview, its peek).
type FolderTab struct {
	base
	fv      *Files
	cf      *ChannelFiles
	ch      string
	tab     FolderTabInfo
	preview bool

	list widget.List
	kb   *vkList
	rows map[string]*ftRow

	all          []core.Bookmark
	items        []core.Bookmark
	pending      []core.PendingFolderFile // FILES_UPLOADING rows (folder_tab_more.go)
	loaded       bool
	hasWorkflows bool

	filesMu sync.Mutex
	files   map[string]core.File
	asked   map[string]bool

	addBtn, moreBtn, titleBtn widget.Clickable
	addSize, moreSize         image.Point
	addTip, moreTip           popups.TipState
	editing                   bool
	titleEd                   widget.Editor
	editFocused               bool

	dragID   string // the row being dragged
	dragFrom int
	dragTo   int
	rowH     int
	peekH    int
}

// NewFolderTab builds the folder tab's view.
func NewFolderTab(h Host, ch string, t FolderTabInfo) *FolderTab {
	v := &FolderTab{base: newBase(h), fv: NewFiles(h), cf: NewChannelFiles(h, ch), ch: ch, tab: t, rows: map[string]*ftRow{},
		files: map[string]core.File{}, asked: map[string]bool{}, dragTo: -1}
	v.list.Axis = layout.Vertical
	v.kb = newVKList(&v.list)
	v.titleEd.SingleLine, v.titleEd.Submit = true, true
	return v
}

// NewFolderTabPeek is the tab's FolderTabPeek.
func NewFolderTabPeek(h Host, ch string, t FolderTabInfo) *FolderTab {
	v := NewFolderTab(h, ch, t)
	v.preview = true
	return v
}

// SetTab updates the tab (a new label from the tab bar).
func (v *FolderTab) SetTab(t FolderTabInfo) {
	if !v.editing {
		v.tab = t
	}
}

// Rows are the folder's bookmarks as last drawn (tests).
func (v *FolderTab) Rows() []core.Bookmark { return v.items }

// Loaded reports whether the channel's bookmarks have loaded.
func (v *FolderTab) Loaded() bool { return v.loaded }

// Name is the folder's name: the tab's label, else "Bookmarks".
func (v *FolderTab) Name() string {
	if v.tab.Label != "" {
		return v.tab.Label
	}
	return nsTabbed.T("Bookmarks")
}

func (v *FolderTab) rowOf(id string) *ftRow {
	r := v.rows[id]
	if r == nil {
		r = new(ftRow)
		v.rows[id] = r
	}
	return r
}

func (v *FolderTab) refresh() {
	c := v.h.Core()
	if c == nil {
		return
	}
	v.all, v.loaded = c.ChannelBookmarks(v.ch)
	v.items, v.hasWorkflows = core.FolderRows(v.all, v.tab.FolderID)
	v.pending = c.FolderUploads(v.ch, v.tab.FolderID)
	if v.dragID != "" && v.dragTo >= 0 {
		return
	}
	for _, b := range v.items {
		if b.Type == "file" && b.EntityID != "" {
			v.askFile(b.EntityID)
		}
	}
}

// askFile loads a file bookmark's file (the files store: files.info).
func (v *FolderTab) askFile(id string) {
	v.filesMu.Lock()
	if v.asked[id] {
		v.filesMu.Unlock()
		return
	}
	v.asked[id] = true
	v.filesMu.Unlock()
	c := v.h.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		f, err := c.FileInfo(ctx, id)
		if err != nil {
			return
		}
		v.filesMu.Lock()
		v.files[id] = f
		v.filesMu.Unlock()
		v.h.Invalidate()
	}()
}

func (v *FolderTab) file(id string) (core.File, bool) {
	v.filesMu.Lock()
	defer v.filesMu.Unlock()
	f, ok := v.files[id]
	return f, ok
}

func (v *FolderTab) canPost() bool {
	c := v.h.Core()
	return c != nil && c.CanEditBookmarks(v.ch)
}

func (v *FolderTab) canEditTabs() bool {
	c := v.h.Core()
	return c != nil && c.CanEditTabs(v.ch)
}

// PeekHeight is FolderTabPeek's contentHeight.
func (v *FolderTab) PeekHeight(gtx layout.Context) int {
	v.refresh()
	switch {
	case !v.loaded:
		return gtx.Dp(315)
	case len(v.items) == 0:
		return gtx.Dp(250)
	}
	return gtx.Dp(63) * len(v.items)
}

// ---- actions ----

// open is openBookmark: a link opens, a file goes to the file opener.
func (v *FolderTab) open(b core.Bookmark) {
	if b.Type == "file" {
		if f, ok := v.file(b.EntityID); ok {
			v.cf.open(fileItem{File: f})
			return
		}
	}
	if bo, ok := v.h.(BookmarkOpener); ok {
		bo.OpenBookmark(v.ch, b)
		return
	}
	if b.Link != "" {
		v.cf.openLink(b.Link)
	}
}

// RowButton is a row's clickable (tests: hover and click it).
func (v *FolderTab) RowButton(id string) *widget.Clickable { return &v.rowOf(id).row }

// MenuButton is a row's More actions button (tests).
func (v *FolderTab) MenuButton(id string) *widget.Clickable { return &v.rowOf(id).menu }

// RowMenuLabels are a row's More actions labels ("─" a separator).
func (v *FolderTab) RowMenuLabels(b core.Bookmark) []string { return menuLabelsOf(v.rowMenuItems(b)) }

func menuLabelsOf(items []popups.MenuItem) []string {
	var out []string
	for _, it := range items {
		if it.Separator {
			out = append(out, "─")
			continue
		}
		out = append(out, it.Label)
	}
	return out
}

func (v *FolderTab) popupHost() (popups.Host, bool) {
	ph, ok := v.h.(popups.Host)
	return ph, ok
}

// openAddLink is FolderCreateMenu's Link: the Add link modal with the
// folder as parent.
func (v *FolderTab) openAddLink() {
	ph, ok := v.popupHost()
	if !ok {
		return
	}
	ch, parent := v.ch, v.tab.FolderID
	m := popups.NewBookmarkLinkModal(ph, popups.BookmarkLinkOpts{Title: nsTabbed.T("Add link"), Save: func(ctx context.Context, link, name string) error {
		c := v.h.Core()
		if c == nil {
			return context.Canceled
		}
		_, err := c.AddFolderLink(ctx, ch, parent, link, name)
		return err
	}})
	ph.ShowOverlay(m)
}

// addMenu is FolderCreateMenu's menu (200 wide, below the button).
func (v *FolderTab) addMenu(btn image.Rectangle, pos string) {
	ph, ok := v.popupHost()
	if !ok {
		return
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, v.AddMenuItems(), popups.MenuOpts{Anchor: btn, Width: 200, Popover: pos}))
}

// folderMenu is More folder actions: Rename folder, Delete folder.
func (v *FolderTab) folderMenu(btn image.Rectangle) {
	ph, ok := v.popupHost()
	if !ok || !v.canEditTabs() {
		return
	}
	items := []popups.MenuItem{
		{Key: "rename_folder", Label: nsTabbed.T("Rename folder"), Do: v.openRename},
		popups.Sep(),
		{Key: "remove_folder", Label: nsTabbed.T("Delete folder"), Danger: true, Do: v.deleteFolder},
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: btn, Width: 240, Popover: "bottom"}))
}

func (v *FolderTab) openRename() {
	ph, ok := v.popupHost()
	if !ok {
		return
	}
	ch, id := v.ch, v.tab.ID
	ph.ShowOverlay(popups.NewRenameFolderModal(ph, v.tab.Label, func(ctx context.Context, label string) error {
		return v.rename(ctx, ch, id, label, "updateTabLabel")
	}))
}

// rename is updateTabLabel: the label at once (setChannel with the new
// tabz), conversations.updateTab, the old label back on failure.
func (v *FolderTab) rename(ctx context.Context, ch, id, label, reason string) error {
	c := v.h.Core()
	if c == nil {
		return context.Canceled
	}
	old := v.tab.Label
	v.tab.Label = label
	err := c.RenameFolderTab(ctx, ch, id, label, reason)
	if err != nil {
		v.tab.Label = old
	}
	if fh, ok := v.h.(FolderHost); ok {
		fh.TabsChanged(ch)
	}
	v.h.Invalidate()
	return err
}

// deleteFolder is Delete folder (useRemoveTab with its confirmation).
func (v *FolderTab) deleteFolder() {
	ph, ok := v.popupHost()
	if !ok {
		return
	}
	ch, id := v.ch, v.tab.ID
	remove := func() {
		go func() {
			c := v.h.Core()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := c.RemoveTab(ctx, ch, id); err != nil {
				return
			}
			if fh, ok := v.h.(FolderHost); ok {
				fh.TabRemoved(ch, id)
			}
		}()
	}
	if len(v.items) == 0 {
		remove()
		return
	}
	name := "#" + strings.TrimPrefix(v.channelName(), "#")
	ph.ShowOverlay(popups.NewDeleteFolderModal(ph, len(v.items), v.Name(), name, remove))
}

func (v *FolderTab) channelName() string {
	c := v.h.Core()
	if c == nil {
		return ""
	}
	if cv, ok := c.Conv(v.ch); ok {
		return cv.Name
	}
	return ""
}

// rowMenuItems is FolderListBookmarkMenu.
func (v *FolderTab) rowMenuItems(b core.Bookmark) []popups.MenuItem {
	link := b.Link
	items := []popups.MenuItem{{Key: "copy", Label: nsTabbed.T("Copy link"), DoCtx: func(gtx layout.Context) {
		gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(link))})
		v.h.Toast(nsFile.T("Link copied to clipboard"))
	}}}
	if !v.canPost() {
		return items
	}
	if b.Type != "file" {
		items = append(items, popups.MenuItem{Key: "edit", Label: nsTabbed.T("Edit link"), Do: func() { v.openEdit(b) }})
	}
	del := nsTabbed.T("Delete link")
	if b.Type == "file" {
		del = nsTabbed.T("Remove file")
	}
	return append(items, popups.Sep(), v.MoveMenu(b), popups.Sep(), // Move to folder (folder_tab_more.go)
		popups.MenuItem{Key: "delete", Label: del, Danger: true, Do: func() { v.openDelete(b) }})
}

func (v *FolderTab) openRowMenu(b core.Bookmark, btn image.Rectangle) {
	ph, ok := v.popupHost()
	if !ok {
		return
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, v.rowMenuItems(b), popups.MenuOpts{Anchor: btn, Width: 240,
		Popover: "left-bottom", OffsetX: -4, TryOpposite: true}))
}

func (v *FolderTab) openEdit(b core.Bookmark) {
	ph, ok := v.popupHost()
	if !ok {
		return
	}
	ch := v.ch
	ph.ShowOverlay(popups.NewBookmarkLinkModal(ph, popups.BookmarkLinkOpts{Title: nsTabbed.T("Edit link"), Link: b.Link, Name: b.Title,
		Edit: true, Save: func(ctx context.Context, link, name string) error {
			c := v.h.Core()
			if c == nil {
				return context.Canceled
			}
			_, err := c.EditFolderLink(ctx, ch, b, link, name)
			return err
		}}))
}

func (v *FolderTab) openDelete(b core.Bookmark) {
	ph, ok := v.popupHost()
	if !ok {
		return
	}
	what := b.Title
	if what == "" {
		what = b.Link
	}
	if b.Type == "file" {
		if f, ok := v.file(b.EntityID); ok {
			what = (&fileItem{File: f}).title()
		}
	}
	ch := v.ch
	ph.ShowOverlay(popups.NewDeleteBookmarkModal(ph, b.Type == "file", what, v.Name(), func(ctx context.Context) error {
		c := v.h.Core()
		if c == nil {
			return context.Canceled
		}
		return c.RemoveFolderBookmark(ctx, ch, b)
	}))
}

// drop is the reorder: the dragged bookmark into row to's place.
func (v *FolderTab) drop(id string, to int) {
	c := v.h.Core()
	if c == nil {
		return
	}
	file := false
	for _, b := range v.items {
		if b.ID == id {
			file = b.Type == "file"
		}
	}
	ch, folder := v.ch, v.tab.FolderID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := c.ReorderFolderBookmark(ctx, ch, folder, id, to); err != nil {
			msg := nsTabbed.T("Something went wrong while reordering the link.")
			if file {
				msg = nsTabbed.T("Something went wrong while reordering the file.")
			}
			v.h.Toast(msg)
		}
		v.h.Invalidate()
	}()
}

func (v *FolderTab) update(gtx C) {
	if v.addBtn.Clicked(gtx) && !v.kaLocked() { // disableAdd
		// FolderCreateMenu's menu: below the header's button, bottom-left
		// of the empty state's (and the peek's "Add")
		pos := "bottom"
		if v.preview || v.loaded && len(v.items) == 0 && len(v.pending) == 0 {
			pos = "bottom-left"
		}
		v.addMenu(v.fv.buttonRect(&v.addBtn, v.addSize), pos)
	}
	if v.moreBtn.Clicked(gtx) {
		v.folderMenu(v.fv.buttonRect(&v.moreBtn, v.moreSize))
	}
	if v.titleBtn.Clicked(gtx) && v.canEditTabs() && !v.preview {
		v.editing, v.editFocused = true, false
		v.titleEd.SetText(v.tab.Label)
		n := len([]rune(v.tab.Label))
		v.titleEd.SetCaret(n, n)
	}
	if v.editing {
		for {
			e, ok := v.titleEd.Update(gtx)
			if !ok {
				break
			}
			if _, ok := e.(widget.SubmitEvent); ok {
				v.saveTitle()
			}
		}
		for {
			e, ok := gtx.Event(key.Filter{Focus: &v.titleEd, Name: key.NameEscape})
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press {
				v.editing = false
			}
		}
		if v.editFocused && !gtx.Focused(&v.titleEd) { // onBlur saves
			v.saveTitle()
		}
	}
	for _, b := range v.items {
		r := v.rows[b.ID]
		if r == nil {
			continue
		}
		menu := r.menu.Clicked(gtx)
		if r.row.Clicked(gtx) && !menu {
			v.open(b)
		}
		if menu {
			v.openRowMenu(b, v.fv.buttonRect(&r.menu, r.menuSize))
		}
	}
}

// saveTitle is EditableFolderTitle's onBlur / onEnter: a changed,
// non-empty name of at most 256 characters is saved.
func (v *FolderTab) saveTitle() {
	if !v.editing {
		return
	}
	v.editing = false
	label := strings.TrimSpace(v.titleEd.Text())
	if label == "" || label == v.tab.Label || len([]rune(label)) > 256 {
		return
	}
	ch, id := v.ch, v.tab.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = v.rename(ctx, ch, id, label, "folder_tab_view_header")
	}()
}

// Editing reports whether the title is being edited (tests).
func (v *FolderTab) Editing() bool { return v.editing }

// StartTitleEdit is a click on the title (tests).
func (v *FolderTab) StartTitleEdit() {
	if v.canEditTabs() {
		v.editing, v.editFocused = true, false
		v.titleEd.SetText(v.tab.Label)
	}
}

// SetTitleText types into the title editor (tests).
func (v *FolderTab) SetTitleText(s string) { v.titleEd.SetText(s) }

// SubmitTitle is Enter in the title editor (tests).
func (v *FolderTab) SubmitTitle() { v.saveTitle() }

// Drop moves bookmark id to row to (tests: the drag's drop).
func (v *FolderTab) Drop(id string, to int) { v.drop(id, to) }

func (v *FolderTab) kbClick(key string) {
	for _, b := range v.items {
		if b.ID == key {
			v.open(b)
		}
	}
}

// ---- layout ----

// Layout draws the view (or the peek).
func (v *FolderTab) Layout(gtx C) D {
	v.frame()
	v.fv.frame()
	v.cf.frame()
	v.refresh()
	v.update(gtx)
	sz := gtx.Constraints.Max
	if v.preview {
		fill(gtx, image.Rectangle{Max: sz}, v.p.Surface)
		return v.peek(gtx)
	}
	fill(gtx, image.Rectangle{Max: sz}, v.p.Page) // .container__Itvf4: base-sec
	gtx.Constraints = layout.Exact(sz)
	return layout.Inset{Top: 20, Bottom: 24}.Layout(gtx, func(gtx C) D {
		if !v.loaded {
			return col(gtx,
				layout.Rigid(func(gtx C) D { return v.contained(gtx, v.header) }),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 16}.Layout(gtx, func(gtx C) D { return v.contained(gtx, v.skeletonRows) })
				}),
			)
		}
		if len(v.items) == 0 && len(v.pending) == 0 { // useFolderRows' isEmpty: no rows, no uploads
			return col(gtx,
				layout.Rigid(func(gtx C) D { return v.contained(gtx, v.header) }),
				layout.Flexed(1, v.emptyTab),
			)
		}
		return col(gtx,
			layout.Rigid(func(gtx C) D { return v.contained(gtx, v.header) }),
			layout.Flexed(1, v.results),
		)
	})
}

func (v *FolderTab) contained(gtx C, w W) D {
	full := gtx.Constraints.Max.X
	cw := min(gtx.Dp(968), max(0, full-gtx.Dp(32)))
	g := gtx
	g.Constraints.Min.X, g.Constraints.Max.X = cw, cw
	m := op.Record(gtx.Ops)
	d := w(g)
	call := m.Stop()
	func() {
		defer op.Offset(image.Pt((full-cw)/2, 0)).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}()
	return D{Size: image.Pt(full, d.Size.Y)}
}

// header is FolderTabViewHeader.
func (v *FolderTab) header(gtx C) D {
	w := gtx.Constraints.Max.X
	h := gtx.Dp(36)
	// the right side: the add button, More folder actions (4 apart)
	right := w
	bs := gtx.Dp(28)
	if v.canEditTabs() && !v.kaLocked() { // disableActions (folder_tab_more.go)
		right -= bs
		dirAt(gtx, image.Pt(right, (h-bs)/2), func(gtx C) D {
			d := v.outlinedIcon(gtx, &v.moreBtn, "ellipsis-vertical-filled", bs)
			v.moreSize = d.Size
			popups.Tooltip(gtx, v.h.Theme(), &v.moreTip, v.moreBtn.Hovered(), d.Size, popups.Above, nsTabbed.T("More folder actions"), "")
			return d
		})
		right -= gtx.Dp(4)
	}
	if v.canPost() && v.knowledge() { // the Channel Expert's "Add Source" (folder_tab_more.go)
		bw := v.kaAddWidth(gtx)
		right -= bw
		dirAt(gtx, image.Pt(right, (h-bs)/2), func(gtx C) D { return v.kaAddButton(gtx) })
	} else if v.canPost() {
		right -= bs
		dirAt(gtx, image.Pt(right, (h-bs)/2), func(gtx C) D {
			d := v.greenPlus(gtx, &v.addBtn, bs)
			v.addSize = d.Size
			popups.Tooltip(gtx, v.h.Theme(), &v.addTip, v.addBtn.Hovered(), d.Size, popups.Above, nsTabbed.T("Add file or link to folder"), "")
			return d
		})
	}
	tx := gtx.Dp(4)
	tw := max(0, right-tx-gtx.Dp(8))
	if v.editing {
		dirAt(gtx, image.Pt(tx, 0), func(gtx C) D {
			if !v.editFocused {
				gtx.Execute(key.FocusCmd{Tag: &v.titleEd})
				v.editFocused = true
			}
			gtx.Constraints = layout.Exact(image.Pt(tw, h))
			return layout.W.Layout(gtx, func(gtx C) D {
				return v.th.Editor(gtx, &v.titleEd, 22, v.p.Text, v.p.SelBg)
			})
		})
	} else if v.canEditTabs() && !v.kaLocked() { // disableEdit
		dirAt(gtx, image.Pt(tx-gtx.Dp(8)+gtx.Dp(4), 0), func(gtx C) D {
			return v.titleBtn.Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				lw := min(v.th.Measure(gtx, 22, wBlack, v.Name()), max(0, tw-gtx.Dp(40)))
				bw := gtx.Dp(8) + lw + gtx.Dp(8)
				hov := v.titleBtn.Hovered()
				if hov {
					bw += gtx.Dp(24)
					rrect(gtx, image.Rect(0, 0, bw, h), gtx.Dp(8), v.lt(rgb(0xeaeaea), rgb(0x212428))) // base-ter
				}
				dirAt(gtx, image.Pt(gtx.Dp(8), 0), func(gtx C) D {
					gtx.Constraints = layout.Constraints{Max: image.Pt(lw+1, h)}
					return v.titleText(gtx, h)
				})
				if hov {
					dirAt(gtx, image.Pt(gtx.Dp(8)+lw+gtx.Dp(8), (h-gtx.Dp(16))/2), func(gtx C) D { return v.icon(gtx, "edit", 16, v.p.Text) })
				}
				return D{Size: image.Pt(bw, h)}
			})
		})
	} else {
		dirAt(gtx, image.Pt(tx, 0), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(tw, h)}
			return v.titleText(gtx, h)
		})
	}
	return D{Size: image.Pt(w, h+gtx.Dp(12))}
}

func (v *FolderTab) titleText(gtx C, h int) D {
	return vmid(gtx, h, func(gtx C) D {
		return v.th.Layout(gtx, theme.TextStyle{Size: 22, LineHeight: 30, Weight: wBlack, Color: v.p.Text, MaxLines: 1}, v.Name())
	})
}

// vmid centres w vertically in a band h tall.
func vmid(gtx C, h int, w W) D {
	m := op.Record(gtx.Ops)
	d := w(gtx)
	call := m.Stop()
	func() {
		defer op.Offset(image.Pt(0, (h-d.Size.Y)/2)).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}()
	return D{Size: image.Pt(d.Size.X, h)}
}

// greenPlus is the add button: plus-filled, white on #007a5a (#148567
// hovered), radius 8.
func (v *FolderTab) greenPlus(gtx C, c *widget.Clickable, s int) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		bg := rgb(0x007a5a)
		if c.Hovered() {
			bg = rgb(0x148567)
		}
		rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), bg)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(s, s))
		layout.Center.Layout(g, func(gtx C) D { return v.icon(gtx, "plus-filled", 16, rgb(0xffffff)) })
		return D{Size: image.Pt(s, s)}
	})
}

// outlinedIcon is a bordered white icon button (sk_white_bg bordered).
func (v *FolderTab) outlinedIcon(gtx C, c *widget.Clickable, icon string, s int) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		r := image.Rect(0, 0, s, s)
		bg := v.p.Surface
		if c.Hovered() {
			bg = theme.Over(v.p.Hover, v.p.Surface)
		}
		rrect(gtx, r, gtx.Dp(8), bg)
		border(gtx, r, gtx.Dp(8), max(1, gtx.Dp(1)), v.p.SegLine)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(s, s))
		layout.Center.Layout(g, func(gtx C) D { return v.icon(gtx, icon, 16, v.p.Sec) })
		return D{Size: image.Pt(s, s)}
	})
}

// skeletonRows are the loading rows: five 64px pillows sharing lines.
func (v *FolderTab) skeletonRows(gtx C) D {
	w := gtx.Constraints.Max.X
	y := 0
	for i := 0; i < 5; i++ {
		h := gtx.Dp(64)
		dirAt(gtx, image.Pt(0, y), func(gtx C) D {
			v.cf.listFrame(gtx, image.Pt(w, h), i == 0, i == 4, false)
			o := image.Pt(gtx.Dp(13), (h-gtx.Dp(36))/2)
			rrect(gtx, image.Rectangle{Min: o, Max: o.Add(image.Pt(gtx.Dp(36), gtx.Dp(36)))}, gtx.Dp(8), v.p.Skel)
			tx := o.X + gtx.Dp(48)
			tw := max(0, w-tx-gtx.Dp(24))
			rrect(gtx, image.Rect(tx, o.Y+gtx.Dp(4), tx+tw*45/100, o.Y+gtx.Dp(16)), gtx.Dp(4), v.p.Skel)
			rrect(gtx, image.Rect(tx, o.Y+gtx.Dp(22), tx+tw*30/100, o.Y+gtx.Dp(32)), gtx.Dp(4), v.p.Skel)
			return D{Size: image.Pt(w, h)}
		})
		y += h - max(1, gtx.Dp(1))
	}
	return D{Size: image.Pt(w, y)}
}

// emptyTab is FolderTabView's empty state under the header.
func (v *FolderTab) emptyTab(gtx C) D {
	sz := gtx.Constraints.Max
	y := gtx.Dp(100)
	if wh, ok := v.h.(interface{ WindowHeight() int }); ok {
		y = int(float32(wh.WindowHeight()) * 0.15)
	}
	title := nsTabbed.T("Add files and links to reference later")
	desc := nsTabbed.T("Everyone in {channelName} will have access to what you add here.", "channelName", "#"+v.channelName())
	if v.knowledge() { // aTvS's empty state
		title = nsTabbed.T("Add trusted sources for the agent")
		desc = nsTabbed.T("The Channel Expert will use the sources you add here to generate replies.")
		v.kaFooter(gtx, sz)
	}
	tw := min(sz.X-gtx.Dp(32), gtx.Dp(480))
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(tw, sz.Y)}
	m1 := op.Record(gtx.Ops)
	t1 := v.th.Layout(g, theme.TextStyle{Size: 18, LineHeight: 24, Weight: wBlack, Color: v.p.Text, Align: text.Middle}, title)
	c1 := m1.Stop()
	m2 := op.Record(gtx.Ops)
	t2 := v.th.Layout(g, theme.TextStyle{Size: 15, LineHeight: 22, Weight: wReg, Color: v.p.Text, Align: text.Middle}, desc)
	c2 := m2.Stop()
	y += v.emptyFolderPicture(gtx, sz.X/2, y) + gtx.Dp(4) // the picture, 4 above the empty state (folder_tab_more.go)
	func() {
		defer op.Offset(image.Pt((sz.X-t1.Size.X)/2, y)).Push(gtx.Ops).Pop()
		c1.Add(gtx.Ops)
	}()
	y += t1.Size.Y + gtx.Dp(4)
	func() {
		defer op.Offset(image.Pt((sz.X-t2.Size.X)/2, y)).Push(gtx.Ops).Pop()
		c2.Add(gtx.Ops)
	}()
	y += t2.Size.Y + gtx.Dp(16)
	if v.canPost() && v.knowledge() {
		dirAt(gtx, image.Pt((sz.X-v.kaAddWidth(gtx))/2, y), func(gtx C) D { return v.kaAddButton(gtx) })
	} else if v.canPost() {
		bs := gtx.Dp(28)
		dirAt(gtx, image.Pt((sz.X-bs)/2, y), func(gtx C) D {
			d := v.greenPlus(gtx, &v.addBtn, bs)
			v.addSize = d.Size
			return d
		})
	}
	return D{Size: sz}
}

func (v *FolderTab) kbItems() []vkItem {
	items := make([]vkItem, 0, len(v.items))
	for i, b := range v.items {
		items = append(items, vkItem{key: b.ID, elem: i, focusable: true})
	}
	return items
}

func (v *FolderTab) results(gtx C) D {
	v.kb.setItems(v.kbItems())
	v.kb.update(gtx, gtx.Constraints.Max.Y, vkOpts{Click: v.kbClick})
	v.kb.begin()
	n := len(v.items) + len(v.pending)
	d := v.scrollList(gtx, &v.list, n, func(gtx C, i int) D {
		if i >= len(v.items) { // the uploads after the rows (FILES_UPLOADING)
			p := v.pending[i-len(v.items)]
			return v.contained(gtx, func(gtx C) D {
				d := v.pendingRow(gtx, p, i, n)
				if i == n-1 {
					d.Size.Y += gtx.Dp(32)
				}
				return d
			})
		}
		b := v.items[i]
		return v.kb.elem(i, v.contained(gtx, func(gtx C) D {
			d := v.row(gtx, b, i, n)
			if i == n-1 {
				d.Size.Y += gtx.Dp(32)
			}
			return d
		}))
	})
	v.kb.area(gtx, image.Rectangle{Max: d.Size})
	v.kb.after()
	return d
}

// row is one bookmark's row with its drag (the tab) or the peek's row.
func (v *FolderTab) row(gtx C, b core.Bookmark, i, n int) D {
	r := v.rowOf(b.ID)
	first, last := i == 0, i == n-1
	w := gtx.Constraints.Max.X
	bw := max(1, gtx.Dp(1))
	h := 2*gtx.Dp(12) + gtx.Dp(36) + bw
	if last || v.preview {
		h += bw
	}
	if v.preview {
		h = gtx.Dp(63)
	}
	v.rowH = h - bw
	sz := image.Pt(w, h)
	if !v.preview && v.canPost() {
		v.dragEvents(gtx, r, b, i, n)
	}
	m := op.Record(gtx.Ops)
	r.row.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		hover := r.row.Hovered() || r.menu.Hovered()
		if v.preview {
			fill(gtx, image.Rectangle{Max: sz}, v.p.Surface)
			if !last {
				fill(gtx, image.Rect(0, h-bw, w, h), theme.Over(v.p.CardLine, v.p.Surface))
			}
		} else {
			v.cf.listFrame(gtx, sz, first, last, hover)
		}
		ob := bw
		if v.preview {
			ob = 0
		}
		pr := 0
		if v.preview {
			pr = gtx.Dp(4) // .listItem__hftW3: padding-right 4
		}
		v.rowContents(gtx, b, image.Pt(ob, ob), w-2*ob-pr, h-ob, r, hover)
		if v.kb.ringOn(b.ID) {
			v.vkRing(gtx, image.Rectangle{Max: sz}, gtx.Dp(12))
		}
		return D{Size: sz}
	})
	call := m.Stop()
	if r.dragging {
		defer paint.PushOpacity(gtx.Ops, .5).Pop() // .dragging__xo_yE
	}
	call.Add(gtx.Ops)
	if !v.preview && v.canPost() {
		st := clip.Rect{Max: sz}.Push(gtx.Ops)
		pass := pointer.PassOp{}.Push(gtx.Ops)
		r.drag.Add(gtx.Ops)
		pass.Pop()
		st.Pop()
	}
	// the drop line on the row the drag is over
	if v.dragID != "" && v.dragTo == i && v.dragFrom != i {
		lh := gtx.Dp(2)
		y := 0 // moving up: the target's top
		if v.dragFrom < i {
			y = h - lh // moving down: its bottom
		}
		rrect(gtx, image.Rect(gtx.Dp(2), y, w-gtx.Dp(2), y+lh), gtx.Dp(4), v.lt(rgb(0x1264a3), rgb(0x0a77a7)))
	}
	if last || v.preview {
		return D{Size: sz}
	}
	return D{Size: image.Pt(w, h-bw)}
}

// dragEvents runs a row's drag: past the slop it is the dragged row, its
// target the row under the pointer; the release drops it there.
func (v *FolderTab) dragEvents(gtx C, r *ftRow, b core.Bookmark, i, n int) {
	for {
		ev, ok := r.drag.Update(gtx.Metric, gtx.Source, gesture.Vertical)
		if !ok {
			break
		}
		switch ev.Kind {
		case pointer.Press:
			r.dragStart, r.dragY = ev.Position, ev.Position.Y
		case pointer.Drag:
			r.dragY = ev.Position.Y
			if !r.dragging && abs32(r.dragY-r.dragStart.Y) > float32(gtx.Dp(4)) {
				r.dragging = true
				v.dragID, v.dragFrom = b.ID, i
			}
			if r.dragging && v.rowH > 0 {
				// the row under the pointer: rows are one height apart
				dy := float64(r.dragY-r.dragStart.Y) / float64(v.rowH)
				v.dragTo = min(max(i+int(math.Round(dy)), 0), n-1)
			}
		case pointer.Release, pointer.Cancel:
			if r.dragging {
				r.dragging = false // past the slop the drag grabbed the pointer: the row's click was cancelled
				if ev.Kind == pointer.Release && v.dragTo >= 0 && v.dragTo != v.dragFrom {
					v.drop(v.dragID, v.dragTo)
				}
			}
			v.dragID, v.dragTo = "", -1
		}
	}
}

// rowContents draws a link's or a file's entity at o in a w×h box.
func (v *FolderTab) rowContents(gtx C, b core.Bookmark, o image.Point, w, h int, r *ftRow, hover bool) {
	is := gtx.Dp(36)
	ix := o.X + gtx.Dp(12)
	iy := o.Y + (h-o.Y-is)/2
	var primary, secondary string
	if b.Type == "file" {
		f, ok := v.file(b.EntityID)
		fi := fileItem{File: f}
		if ok {
			dirAt(gtx, image.Pt(ix, iy), func(gtx C) D { return v.fv.fileTile(gtx, &fi, 36) })
			primary, secondary = fi.title(), v.cf.fileDetail(&fi)
		} else {
			rrect(gtx, image.Rect(ix, iy, ix+is, iy+is), gtx.Dp(8), v.p.Skel)
			primary = b.Title
		}
	} else {
		dirAt(gtx, image.Pt(ix, iy), func(gtx C) D { return v.linkIcon(gtx, b) })
		primary = b.Title
		if primary == "" {
			primary = b.Link
		}
		secondary = b.Link
		if u, err := url.Parse(b.Link); err == nil && u.Hostname() != "" {
			secondary = u.Hostname()
		}
	}
	tx := ix + is + gtx.Dp(12)
	actions := (!v.preview || hover) && (b.Type != "file" || hover)
	tw := max(0, o.X+w-tx-gtx.Dp(12))
	if actions {
		tw = max(0, tw-gtx.Dp(36))
	}
	textH := gtx.Dp(22)
	if secondary != "" {
		textH += gtx.Dp(18)
	}
	dirAt(gtx, image.Pt(tx, o.Y+(h-o.Y-textH)/2), func(gtx C) D {
		gtx.Constraints.Max.X = tw
		return col(gtx,
			layout.Rigid(func(gtx C) D {
				return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: wBold, Color: v.p.Text, MaxLines: 1}, primary)
			}),
			layout.Rigid(func(gtx C) D {
				if secondary == "" {
					return D{}
				}
				return v.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Weight: wReg, Color: v.p.Muted, MaxLines: 1}, secondary)
			}),
		)
	})
	if actions {
		// .actionsButton__WyiPa: top 2, right 2, padding 4
		bs := gtx.Dp(28)
		p := image.Pt(o.X+w-gtx.Dp(6)-bs, o.Y+gtx.Dp(6))
		dirAt(gtx, p, func(gtx C) D {
			d := v.iconButton(gtx, &r.menu, "ellipsis-vertical-filled", 28, 16, v.p.Sec)
			r.menuSize = d.Size
			popups.Tooltip(gtx, v.h.Theme(), &r.menuTip, r.menu.Hovered(), d.Size, popups.Above, nsTabbed.T("More actions"), "")
			return d
		})
	}
}

// linkIcon is FolderListLink's icon: the emoji, the favicon (36, padding
// 4, radius 8), else link 20 on a foreground .1 tile (radius 4).
func (v *FolderTab) linkIcon(gtx C, b core.Bookmark) D {
	s := gtx.Dp(36)
	switch {
	case b.Emoji != "" && b.EmojiURL != "":
		e := gtx.Dp(28)
		dirAt(gtx, image.Pt((s-e)/2, (s-e)/2), func(gtx C) D { return v.emojiImg(gtx, b.EmojiURL, 28) })
	case b.Icon != "" && v.h.Images() != nil:
		if img, ok := v.h.Images().Get(b.Icon, s*2); ok {
			p := gtx.Dp(4)
			dirAt(gtx, image.Pt(p, p), func(gtx C) D { return themeImage(gtx, img, image.Pt(s-2*p, s-2*p), gtx.Dp(4)) })
		} else {
			rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), v.p.Skel)
		}
	default:
		rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(4), theme.Over(v.p.CardLine, v.p.Surface))
		dirAt(gtx, image.Pt((s-gtx.Dp(20))/2, (s-gtx.Dp(20))/2), func(gtx C) D { return v.icon(gtx, "link", 20, v.p.Text) })
	}
	return D{Size: image.Pt(s, s)}
}

// peek is FolderTabPeek's body.
func (v *FolderTab) peek(gtx C) D {
	sz := gtx.Constraints.Max
	switch {
	case !v.loaded:
		y := 0
		for i := 0; i < 5; i++ {
			h := gtx.Dp(63)
			fill(gtx, image.Rect(0, y, sz.X, y+h), v.p.Surface)
			if i < 4 {
				fill(gtx, image.Rect(0, y+h-max(1, gtx.Dp(1)), sz.X, y+h), theme.Over(v.p.CardLine, v.p.Surface))
			}
			o := image.Pt(gtx.Dp(12), y+(h-gtx.Dp(36))/2)
			rrect(gtx, image.Rectangle{Min: o, Max: o.Add(image.Pt(gtx.Dp(36), gtx.Dp(36)))}, gtx.Dp(8), v.p.Skel)
			tx := o.X + gtx.Dp(48)
			tw := max(0, sz.X-tx-gtx.Dp(24))
			rrect(gtx, image.Rect(tx, o.Y+gtx.Dp(4), tx+tw*50/100, o.Y+gtx.Dp(16)), gtx.Dp(4), v.p.Skel)
			rrect(gtx, image.Rect(tx, o.Y+gtx.Dp(22), tx+tw*30/100, o.Y+gtx.Dp(32)), gtx.Dp(4), v.p.Skel)
			y += h
		}
		return D{Size: sz}
	case len(v.items) == 0:
		return v.peekEmpty(gtx)
	}
	y := 0
	n := len(v.items)
	for i, b := range v.items {
		d := dirAt(gtx, image.Pt(0, y), func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(sz.X, gtx.Dp(63)))
			return v.row(gtx, b, i, n)
		})
		y += d.Size.Y
	}
	v.peekH = y
	return D{Size: image.Pt(sz.X, min(y, sz.Y))}
}

// peekEmpty is FolderTabEmptyFolder isPeek.
func (v *FolderTab) peekEmpty(gtx C) D {
	sz := gtx.Constraints.Max
	box := image.Rect(gtx.Dp(12), gtx.Dp(32), sz.X-gtx.Dp(16), 0)
	title := nsTabbed.T("Add files and links to reference later")
	desc := nsTabbed.T("Everyone in {channelName} can see them. Drag or add files, lists, and canvases to keep things organized.",
		"channelName", "#"+v.channelName())
	tw := max(0, box.Dx()-gtx.Dp(24))
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(tw, sz.Y)}
	sec := v.lt(rgb(0x454447), rgb(0xb9babd))
	m1 := op.Record(gtx.Ops)
	t1 := v.th.Layout(g, theme.TextStyle{Size: 13, LineHeight: 18, Weight: wBold, Color: sec, Align: text.Middle}, title)
	c1 := m1.Stop()
	m2 := op.Record(gtx.Ops)
	t2 := v.th.Layout(g, theme.TextStyle{Size: 13, LineHeight: 18, Weight: wReg, Color: sec, Align: text.Middle}, desc)
	c2 := m2.Stop()
	box.Max.Y = box.Min.Y + gtx.Dp(16) + gtx.Dp(20) + gtx.Dp(8) + t1.Size.Y + t2.Size.Y + gtx.Dp(16)
	bg := v.lt(rgb(0xf0f3fc), rgb(0x040b3d))
	line := v.lt(rgb(0xa0b3f3), rgb(0x19297f))
	rrect(gtx, box, gtx.Dp(8), bg)
	dashed(gtx, box, gtx.Dp(8), max(1, gtx.Dp(1)), line)
	cx := box.Min.X + box.Dx()/2
	y := box.Min.Y + gtx.Dp(16)
	dirAt(gtx, image.Pt(cx-gtx.Dp(10), y), func(gtx C) D {
		return v.icon(gtx, "screen-share-collab-on", 20, v.lt(rgb(0x2840a9), rgb(0x899bdc)))
	})
	y += gtx.Dp(28)
	func() {
		defer op.Offset(image.Pt(cx-t1.Size.X/2, y)).Push(gtx.Ops).Pop()
		c1.Add(gtx.Ops)
	}()
	y += t1.Size.Y
	func() {
		defer op.Offset(image.Pt(cx-t2.Size.X/2, y)).Push(gtx.Ops).Pop()
		c2.Add(gtx.Ops)
	}()
	y = box.Max.Y + gtx.Dp(12)
	if v.canPost() {
		// the small "Add" button (plus, base-inv-hgl-2)
		label := nsTabbed.T("Add")
		lw := v.th.Measure(gtx, 13, wBold, label)
		bw := gtx.Dp(8) + gtx.Dp(16) + gtx.Dp(4) + lw + gtx.Dp(12)
		bh := gtx.Dp(28)
		dirAt(gtx, image.Pt((sz.X-bw)/2, y), func(gtx C) D {
			return v.addBtn.Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				bg := v.lt(rgb(0x1264a3), rgb(0x1d9bd1))
				rrect(gtx, image.Rect(0, 0, bw, bh), gtx.Dp(8), bg)
				dirAt(gtx, image.Pt(gtx.Dp(8), (bh-gtx.Dp(16))/2), func(gtx C) D { return v.icon(gtx, "plus", 16, rgb(0xffffff)) })
				dirAt(gtx, image.Pt(gtx.Dp(28), 0), func(gtx C) D {
					return vmid(gtx, bh, func(gtx C) D {
						return v.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Weight: wBold, Color: rgb(0xffffff), MaxLines: 1}, label)
					})
				})
				return D{Size: image.Pt(bw, bh)}
			})
		})
	}
	return D{Size: sz}
}

// dashed strokes r's rounded outline in dashes (border: 1px dashed).
func dashed(gtx C, r image.Rectangle, radius, w int, c color.NRGBA) {
	dash, gap := gtx.Dp(4), gtx.Dp(3)
	for x := r.Min.X + radius; x < r.Max.X-radius; x += dash + gap {
		e := min(x+dash, r.Max.X-radius)
		fill(gtx, image.Rect(x, r.Min.Y, e, r.Min.Y+w), c)
		fill(gtx, image.Rect(x, r.Max.Y-w, e, r.Max.Y), c)
	}
	for y := r.Min.Y + radius; y < r.Max.Y-radius; y += dash + gap {
		e := min(y+dash, r.Max.Y-radius)
		fill(gtx, image.Rect(r.Min.X, y, r.Min.X+w, e), c)
		fill(gtx, image.Rect(r.Max.X-w, y, r.Max.X, e), c)
	}
	// the corners: the solid rounded outline clipped to them
	for _, q := range []image.Rectangle{
		{Min: r.Min, Max: r.Min.Add(image.Pt(radius, radius))},
		{Min: image.Pt(r.Max.X-radius, r.Min.Y), Max: image.Pt(r.Max.X, r.Min.Y+radius)},
		{Min: image.Pt(r.Min.X, r.Max.Y-radius), Max: image.Pt(r.Min.X+radius, r.Max.Y)},
		{Min: r.Max.Sub(image.Pt(radius, radius)), Max: r.Max},
	} {
		st := clip.Rect(q).Push(gtx.Ops)
		border(gtx, r, radius, w, c)
		st.Pop()
	}
}
