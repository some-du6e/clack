package views

// The Files sidebar's sections (custom_file_sections), Recently viewed,
// Bulk manage, the create menus and paging, against internal/fakeslack
// (synthetic files only; nothing talks to Slack).

import (
	"image"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ToastUndo records the toast and keeps its Undo (tests).
func (h *filesHost) ToastUndo(text string, undo func()) {
	h.Toast(text + " [Undo]")
	h.action = undo
}

// sectionsView is filesView with custom_file_sections on, a custom section
// holding the shared canvas, and files.collections.list answered.
func sectionsView(t *testing.T) (*Files, *filesHost, *vaEnv, *fakeslack.Server, string) {
	t.Helper()
	v, h, e, fs := filesView(t)
	id := fs.AddFileCollection("Launch", "", fakeslack.CanvasShared)
	h.Core().SetExperimentGroup("custom_file_sections", "on")
	e.until(t, "files.collections.list", func() bool { _, ok := h.Core().FileCollections(); return ok })
	e.until(t, "the section's canvas", func() bool { return hasRow(v, fakeslack.CanvasShared+"-"+id) })
	return v, h, e, fs, id
}

func hasRow(v *Files, key string) bool {
	for _, r := range v.sidebarRows() {
		if r.key == key {
			return true
		}
	}
	return false
}

func rowKeys(v *Files) string {
	var out []string
	for _, r := range v.sidebarRows() {
		out = append(out, r.key)
	}
	return strings.Join(out, " ")
}

// With custom_file_sections the sidebar lists files.collections.list: the
// divider, the starred collection (empty: its row), then the custom
// section with its canvas; the header's ⋮ is useFileCollectionMenuTemplate.
// Deleted sits after Lists: file_trash_updates is on for the account
// (core's defaults), which always shows it (TestFilesViewDeletedNav).
func TestFilesViewSectionsSidebar(t *testing.T) {
	v, h, e, fs, id := sectionsView(t)
	want := "all canvases lists horizontal-divider deleted file-collections-divider coll:" + fakeslack.StarredCollectionID +
		" empty-collection-" + fakeslack.StarredCollectionID + " coll:" + id + " " + fakeslack.CanvasShared + "-" + id
	if got := rowKeys(v); !strings.HasSuffix(got, want) {
		t.Errorf("rows %q\nwant …%q", got, want)
	}
	if got := menuLabels(v.collectionMenuItems(fakeslack.StarredCollectionID)); got != "|New canvas|New list|Sidebar section|-|Sort by" {
		t.Errorf("starred menu %q", got)
	}
	items := v.collectionMenuItems(id)
	if got := menuLabels(items); got != "|New canvas|New list|Sidebar section|-|Rename section|Sort by|-|Delete section" {
		t.Errorf("section menu %q", got)
	}
	if items[0].Header != "Create…" {
		t.Errorf("menu header %q", items[0].Header)
	}
	var sortBy popups.MenuItem
	for _, it := range items {
		if it.Label == "Sort by" {
			sortBy = it
		}
	}
	if got := menuLabels(sortBy.Sub); got != "Last viewed|Last updated|A-Z|Date added" || !sortBy.Sub[3].Checked {
		t.Errorf("Sort by %q (Date added checked %v)", got, len(sortBy.Sub) == 4 && sortBy.Sub[3].Checked)
	}
	// Sort by A-Z: files.collections.update {collection, sort}
	sortBy.Sub[2].Do()
	e.until(t, "the sort", func() bool { c, _ := h.Core().FileCollection(id); return c.Sort == core.CollectionSortAlpha })
	calls := fs.FilesViewCalls()
	if last := calls[len(calls)-1]; last.Method != "files.collections.update" || last.Args.Get("sort") != "alpha" || last.Args.Get("collection") != id {
		t.Errorf("last call %+v", last)
	}
	// a header click collapses it (the client store only)
	e.frame()
	s := v.rowState("coll:" + id)
	s.row.Click()
	e.frame()
	if !h.Core().FileCollectionCollapsed(id) || hasRow(v, fakeslack.CanvasShared+"-"+id) {
		t.Error("the section didn't collapse")
	}
	s.row.Click()
	e.frame()
	if h.Core().FileCollectionCollapsed(id) {
		t.Error("a second click didn't open it")
	}
	// the result rows' star is FileSectionDropdownButton
	it := v.item(fakeslack.CanvasShared)
	if it == nil || !v.sectionButtonShown(it) {
		t.Fatal("no section button on the shared canvas")
	}
	if got := menuLabels(v.moveToItems(it.File, true, "file_pillow")); got != "|Starred|Launch|-|Create section|Remove from Launch" {
		t.Errorf("Move to… %q", got)
	}
	// the ⋯ menu drops Add to starred under custom_file_sections
	if got := menuLabels(v.fileMenuItems(*it)); strings.Contains(got, "starred") {
		t.Errorf("file menu %q", got)
	}
}

// Moving a file: files.favorites.add {file_id, collection_id}, the row
// under its new section, "Added {file} to {section}." with Undo.
func TestFilesViewSectionsMove(t *testing.T) {
	v, h, e, fs, id := sectionsView(t)
	starred, _ := h.Core().FileCollection(fakeslack.StarredCollectionID)
	it := *v.item(fakeslack.CanvasShared)
	v.moveToCollection(it.File, starred, false)
	e.until(t, "the move", func() bool {
		s := fs.StarredFiles()
		return len(s) == 1 && s[0] == fakeslack.CanvasShared
	})
	e.until(t, "the starred row", func() bool { return hasRow(v, fakeslack.CanvasShared+"-"+fakeslack.StarredCollectionID) })
	if hasRow(v, fakeslack.CanvasShared+"-"+id) || hasRow(v, "empty-collection-"+fakeslack.StarredCollectionID) {
		t.Errorf("rows after the move: %s", rowKeys(v))
	}
	calls := fs.FilesViewCalls()
	last := calls[len(calls)-1]
	if last.Method != "files.favorites.add" || last.Args.Get("file_id") != fakeslack.CanvasShared || last.Args.Get("collection_id") != fakeslack.StarredCollectionID {
		t.Errorf("last call %+v", last)
	}
	ts := h.toasts()
	if len(ts) == 0 || ts[len(ts)-1] != "Added Weekly sync notes to Starred. [Undo]" {
		t.Fatalf("toasts %q", ts)
	}
	h.action() // Undo: back to Launch
	e.until(t, "the undo", func() bool { return len(fs.FileCollections()["Launch"]) == 1 && len(fs.StarredFiles()) == 0 })
	// the row's × takes it out of its section
	e.until(t, "the section row", func() bool { return hasRow(v, fakeslack.CanvasShared+"-"+id) })
	for _, r := range v.sidebarRows() {
		if r.key == fakeslack.CanvasShared+"-"+id {
			v.dismissRow(r)
		}
	}
	e.until(t, "files.favorites.remove", func() bool { return len(fs.FileCollections()["Launch"]) == 0 })
	calls = fs.FilesViewCalls()
	if last := calls[len(calls)-1]; last.Method != "files.favorites.remove" || last.Args.Get("collection_id") != id {
		t.Errorf("last call %+v", last)
	}
}

// Recently viewed: a canvas FileOpened joins it (not a starred one, or
// with file sections one in a section); the × and the context menu's
// "Remove from recently viewed" take it out.
func TestFilesViewRecentlyViewed(t *testing.T) {
	v, h, e, _ := filesView(t)
	a := h.Core()
	a.FileOpened(fakeslack.CanvasShared)
	e.until(t, "the recent row", func() bool { return hasRow(v, fakeslack.CanvasShared) })
	keys := rowKeys(v)
	if !strings.Contains(keys, "spacer-1 "+core.RecentlyViewedCollection+" "+fakeslack.CanvasShared+" spacer-2 starred-canvas-header") {
		t.Errorf("rows %q", keys)
	}
	var row sbRow
	for _, r := range v.sidebarRows() {
		if r.key == fakeslack.CanvasShared {
			row = r
		}
	}
	if got := menuLabels(v.fileRowMenu(row)); got != "Copy link to canvas|-|Open in split view|-|Remove from recently viewed" {
		t.Errorf("recent canvas menu %q", got)
	}
	v.dismissRow(row)
	e.frame()
	if hasRow(v, fakeslack.CanvasShared) || len(a.RecentlyViewedFileIDs()) != 0 {
		t.Errorf("× kept it: %s", rowKeys(v))
	}
	// starred files stay out (no file sections)
	it := *v.item(fakeslack.CanvasDM)
	v.toggleStar(it.File)
	a.FileOpened(fakeslack.CanvasDM)
	if ids := a.RecentlyViewedFileIDs(); len(ids) != 0 {
		t.Errorf("a starred canvas joined Recently viewed: %v", ids)
	}
	// with file sections: the collection header and its divider
	a.FileOpened(fakeslack.CanvasShared)
	a.SetExperimentGroup("custom_file_sections", "on")
	e.until(t, "files.collections.list", func() bool { _, ok := a.FileCollections(); return ok })
	e.frame()
	keys = rowKeys(v)
	if !strings.Contains(keys, "file-collections-divider "+core.RecentlyViewedCollection+" "+fakeslack.CanvasShared+" recently-viewed-collections-divider coll:") {
		t.Errorf("sections rows %q", keys)
	}
}

// Bulk manage files: the gear's one item; picks, Move to… (one toast with
// Undo), Remove from {section}, which ends edit mode.
func TestFilesViewBulkManage(t *testing.T) {
	v, h, e, fs, id := sectionsView(t)
	if got := menuLabels(v.gearMenuItems()); got != "Bulk manage files" {
		t.Fatalf("gear menu %q", got)
	}
	if got := menuLabels(v.plusMenuItems()); got != "Canvas|List|Sidebar section" {
		t.Errorf("+ menu %q", got)
	}
	v.gearMenuItems()[0].Do()
	if !v.organize {
		t.Fatal("not in edit mode")
	}
	e.frame()
	s := v.rowState(fakeslack.CanvasShared + "-" + id)
	s.row.Click()
	e.frame()
	if !v.picked[fakeslack.CanvasShared] {
		t.Fatal("the click didn't pick the canvas")
	}
	if got := v.removeLabel(); got != "Remove from Launch" {
		t.Errorf("remove label %q", got)
	}
	starred, _ := h.Core().FileCollection(fakeslack.StarredCollectionID)
	v.bulkMove(starred)
	e.until(t, "the bulk move", func() bool { return len(fs.StarredFiles()) == 1 })
	if ts := h.toasts(); len(ts) == 0 || ts[len(ts)-1] != "Moved 1 file. [Undo]" {
		t.Errorf("toasts %q", ts)
	}
	if len(v.picked) != 0 || !v.organize {
		t.Errorf("after the move: picked %v, organize %v", v.picked, v.organize)
	}
	v.picked[fakeslack.CanvasShared] = true
	v.bulkRemove()
	e.until(t, "the bulk remove", func() bool { return len(fs.StarredFiles()) == 0 })
	if ts := h.toasts(); ts[len(ts)-1] != "Removed 1 file from their sections. [Undo]" {
		t.Errorf("toasts %q", ts)
	}
	if v.organize {
		t.Error("still in edit mode after Remove")
	}
}

// The create menus and the gear: "+" without file sections has Canvas
// and List; "New" follows the section; no gear without file sections.
func TestFilesViewCreateMenus(t *testing.T) {
	v, _, _, _ := filesView(t)
	if got := menuLabels(v.plusMenuItems()); got != "Canvas|List" {
		t.Errorf("+ menu %q", got)
	}
	if got := v.plusMenuItems()[0].Subtitle + " / " + v.plusMenuItems()[1].Subtitle; got != "Create and share content / Track and manage projects" {
		t.Errorf("subtitles %q", got)
	}
	if n := len(v.gearMenuItems()); n != 0 {
		t.Errorf("gear items without file sections: %d", n)
	}
	for nav, want := range map[int]string{0: "New canvas|New list", 1: "New canvas", 2: "New list"} {
		v.nav = nav
		if got := menuLabels(v.newMenuItems()); got != want {
			t.Errorf("nav %d New menu %q, want %q", nav, got, want)
		}
	}
	if filesNav[0].icon != "canvas-browser" || filesNav[1].icon != "canvas-content" || filesNav[2].icon != "lists" {
		t.Errorf("nav icons %q %q %q", filesNav[0].icon, filesNav[1].icon, filesNav[2].icon)
	}
}

// Paging: getCurrentPageRange, the page asked for, and the sorts.
func TestFilesViewPages(t *testing.T) {
	for _, c := range []struct {
		page, total int
		want        string
	}{{1, 1, "1"}, {1, 3, "1 2 3"}, {1, 9, "1 2 3 4 5"}, {4, 9, "2 3 4 5 6"}, {9, 9, "5 6 7 8 9"}, {8, 9, "5 6 7 8 9"}} {
		var got []string
		for _, p := range pageRange(c.page, c.total) {
			got = append(got, strconv.Itoa(p))
		}
		if s := strings.Join(got, " "); s != c.want {
			t.Errorf("pageRange(%d, %d) = %q, want %q", c.page, c.total, s, c.want)
		}
	}
	v, _, e, _ := filesView(t)
	v.pages = 3
	v.selectPage(2)
	if v.wantPage != 2 || !v.reload {
		t.Errorf("selectPage: want %d reload %v", v.wantPage, v.reload)
	}
	e.frame()
	if v.wantPage != 0 {
		t.Error("the page wasn't asked for")
	}
	if got := v.sortLabel(); got != "Recently viewed" {
		t.Errorf("sort label %q", got)
	}
	v.query = "plan"
	if got := v.sortOptions(); len(got) != 2 || got[0].label != "Most relevant" || got[1].label != "Last updated" {
		t.Errorf("search sorts %+v", got)
	}
	// the Types picks become the search's fileType filter
	for i := range v.typeOn {
		v.typeOn[i] = i == 1 || i == 8
	}
	if got := strings.Join(v.typeValues(), " "); got != "quip documents images" {
		t.Errorf("type values %q", got)
	}
}

// TestFilesViewSectionsShots saves the sidebar's sections with Recently
// viewed, the section header's ⋮, Move to…, the section modals, the
// create menus, Bulk manage and the pagination row, light and dark
// (VIEWS_SHOTS_DIR, the files-view workflow).
func TestFilesViewSectionsShots(t *testing.T) {
	if os.Getenv("VIEWS_SHOTS_DIR") == "" {
		t.Skip("VIEWS_SHOTS_DIR not set")
	}
	for _, dark := range []bool{false, true} {
		v, h, e, fs, id := sectionsView(t)
		h.th = theme.New(dark)
		a := h.Core()
		a.FileOpened(fakeslack.CanvasChannel)
		a.FileOpened(fakeslack.ListKitchen)
		starred, _ := a.FileCollection(fakeslack.StarredCollectionID)
		v.moveToCollection(v.item(fakeslack.CanvasDM).File, starred, true)
		e.until(t, "the starred canvas", func() bool { return len(fs.StarredFiles()) == 1 })
		e.until(t, "the recent rows", func() bool { return hasRow(v, fakeslack.CanvasChannel) && hasRow(v, fakeslack.ListKitchen) })
		e.frame()
		vaShot(t, e, "files-sections", dark)
		v.openCollectionMenu(id, image.Rect(300, 330, 320, 350))
		e.frame()
		vaShot(t, e, "files-section-menu", dark)
		dismissAll(h)
		if it := v.item(fakeslack.CanvasShared); it != nil {
			v.openMoveMenu(*it, image.Rect(1086, 316, 1114, 344))
			e.frame()
			vaShot(t, e, "files-move-menu", dark)
			dismissAll(h)
		}
		v.sectionModal("")
		e.frame()
		vaShot(t, e, "files-section-create", dark)
		dismissAll(h)
		v.deleteSection(id)
		e.frame()
		vaShot(t, e, "files-section-delete", dark)
		dismissAll(h)
		v.openMenuAt(v.plusMenuItems(), image.Rect(292, 12, 320, 40), "bottom-left", 0)
		e.frame()
		vaShot(t, e, "files-plus-menu", dark)
		dismissAll(h)
		v.openMenuAt(v.newMenuItems(), image.Rect(1100, 14, 1176, 42), "bottom-right", 250)
		e.frame()
		vaShot(t, e, "files-new-menu", dark)
		dismissAll(h)
		v.startOrganize()
		v.picked[fakeslack.CanvasShared] = true
		e.frame()
		vaShot(t, e, "files-bulk-manage", dark)
		v.stopOrganize()
		v.pages, v.page = 7, 3
		e.frame()
		vaShot(t, e, "files-pagination", dark)
	}
}

// Gio hands a click on a button laid out inside a row's Clickable to the
// row's handler too (the router delivers to the hit handler and every
// enclosing one), so the Files rows ignore their own click when one of
// their buttons took it.
func TestFilesViewNestedClicks(t *testing.T) {
	var outer, inner widget.Clickable
	var r input.Router
	var ops op.Ops
	gtxOf := func() layout.Context {
		return layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(200, 40)), Now: time.Now()}
	}
	draw := func() {
		ops.Reset()
		gtx := gtxOf()
		outer.Layout(gtx, func(gtx C) D {
			func() {
				defer op.Offset(image.Pt(160, 6)).Push(gtx.Ops).Pop()
				g := gtx
				g.Constraints = layout.Exact(image.Pt(28, 28))
				inner.Layout(g, func(gtx C) D { return D{Size: image.Pt(28, 28)} })
			}()
			return D{Size: image.Pt(200, 40)}
		})
		r.Frame(&ops)
	}
	draw()
	at := f32.Pt(170, 20)
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: at},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at})
	gtx := gtxOf()
	in, out := inner.Clicked(gtx), outer.Clicked(gtx)
	if !in || !out {
		t.Fatalf("inner clicked %v, outer clicked %v: the row double-click guard rests on both", in, out)
	}

	// the result rows: a star click doesn't open the file
	v, h, e, _ := filesView(t)
	e.frame()
	c := v.clicksOf(fakeslack.CanvasShared)
	before := len(h.opened)
	c.star.Click()
	c.row.Click()
	e.frame()
	if len(h.opened) != before {
		t.Errorf("a star click opened %q", h.last())
	}
	c.row.Click()
	e.frame()
	if h.last() != "canvas:"+fakeslack.CanvasShared {
		t.Errorf("a row click opened %q", h.last())
	}
	// the sidebar: a × click removes the row and doesn't open the file
	h.Core().FileOpened(fakeslack.CanvasChannel)
	e.until(t, "the recent row", func() bool { return hasRow(v, fakeslack.CanvasChannel) })
	e.frame()
	s := v.rowState(fakeslack.CanvasChannel)
	before = len(h.opened)
	s.close.Click()
	s.row.Click()
	e.frame()
	if len(h.opened) != before || len(h.Core().RecentlyViewedFileIDs()) != 0 {
		t.Errorf("× click: opened %v, recents %v", h.opened[before:], h.Core().RecentlyViewedFileIDs())
	}
}
