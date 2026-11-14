package ui

import (
	"context"
	"image"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// #files-gallery's tabs for tabs-3: Files & links, a canvas, a list and
// two folders (Launch docs, the loose Bookmarks one).
var (
	tbCanvasTab = "Ct" + fakeslack.CanvasChannel
	tbListTab   = "Ct" + fakeslack.ListTodos
	tbFolderTab = "Ct0FILESFLD1"
	tbLooseTab  = "Ct0FILESBKS1"
)

// tbEnv is the client on #files-gallery with those tabs and the tab
// grant, wide enough that every tab shows (narrow: until More holds some
// tabs and two draggable ones stay in the bar).
func tbEnv(t *testing.T, narrow bool) (*perfEnv, *fakeslack.Server, string) {
	t.Helper()
	sz := fakeslack.Small
	sz.Files = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ch := fakeslack.FilesChannelID
	fs.Permissions = map[string][]string{ch: {core.PermManageChannelTabs}}
	fs.SetGalleryTabList(
		fakeslack.GalleryTab(fakeslack.FilesTabID, "files", "", "", ""),
		fakeslack.GalleryTab(tbCanvasTab, "canvas", "", fakeslack.CanvasChannel, ""),
		fakeslack.GalleryTab(tbListTab, "list", "", fakeslack.ListTodos, ""),
		fakeslack.GalleryTab(tbFolderTab, "folder", "Launch docs", "", fakeslack.GalleryFolder),
		fakeslack.GalleryTab(tbLooseTab, "folder", "", "", ""),
	)
	e := startEnv(t, fs, t.TempDir())
	e.size = image.Pt(1400, 800)
	e.waitReady()
	e.openChannel(ch)
	e.until("the tabs and the grant", 15*time.Second, func() bool {
		return e.a.data.tabsDone[ch] && len(e.a.channelTabs(ch)) == 6 && e.a.Core().CanEditTabs(ch)
	})
	e.settle(300 * time.Millisecond)
	if narrow {
		for w := 1400; w >= 560; w -= 20 {
			e.size = image.Pt(w, 800)
			e.settle(60 * time.Millisecond)
			drag := 0
			for _, r := range e.a.tabDrag().rects {
				if r.drag {
					drag++
				}
			}
			if len(e.a.TabsHidden(ch)) >= 2 && drag >= 2 {
				break
			}
		}
		if len(e.a.TabsHidden(ch)) == 0 {
			t.Fatalf("no tab went into More down to %v", e.size)
		}
	}
	if os.Getenv("PREVIEW_SHOTS_DIR") != "" {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
		e.settle(200 * time.Millisecond)
	}
	return e, fs, ch
}

// tbSpan is a tab's window span: a hovered point, then its left and right
// edges along that row.
func tbSpan(t *testing.T, e *perfEnv, ch, key string) (p image.Point, x0, x1 int) {
	t.Helper()
	o := e.a.primaryOrigin
	msgs := e.a.clicks.get("tab." + ch + ".messages")
	mp, ok := e.scanHover(msgs, image.Pt(o.X+40, o.Y), image.Pt(o.X+40, o.Y+200))
	if !ok {
		t.Fatal("no Messages tab in the header")
	}
	btn := e.a.clicks.get("tab." + ch + "." + key)
	if key == "messages" {
		p = mp
	} else if p, ok = e.scanHover(btn, image.Pt(o.X+20, mp.Y), image.Pt(e.size.X-10, mp.Y)); !ok {
		t.Fatalf("no tab %s in the header", key)
	}
	edge := func(d int) int {
		x := p.X
		for {
			e.move(f32.Pt(float32(x+d), float32(p.Y)))
			if !btn.Hovered() {
				return x
			}
			x += d
		}
	}
	x0, x1 = edge(-1), edge(1)
	e.move(f32.Pt(float32(p.X), float32(p.Y)))
	return p, x0, x1
}

// tbDrag presses at from, drags to to in steps (a frame each), runs mid
// with the button still down, and releases there.
func tbDrag(e *perfEnv, from, to image.Point, mid func()) {
	pt := func(p image.Point) f32.Point { return f32.Pt(float32(p.X), float32(p.Y)) }
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt(from)},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt(from)})
	e.frame()
	const n = 12
	for i := 1; i <= n; i++ {
		p := from.Add(to.Sub(from).Mul(i).Div(n))
		e.r.Queue(pointer.Event{Kind: pointer.Drag, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt(p)})
		e.frame()
	}
	if mid != nil {
		mid()
	}
	e.r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt(to)})
	e.frame()
	e.frame()
}

func tbKeys(e *perfEnv, ch string) []string {
	var out []string
	for _, tb := range e.a.channelTabs(ch) {
		out = append(out, strings.TrimPrefix(tb.key, "tab:"))
	}
	return out
}

func tbLabels(items []popups.MenuItem) []string {
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

func tbTab(t *testing.T, e *perfEnv, ch, id string) chTab {
	t.Helper()
	for _, tb := range e.a.channelTabs(ch) {
		if tb.key == "tab:"+id {
			return tb
		}
	}
	t.Fatalf("no tab %s in %v", id, tbKeys(e, ch))
	return chTab{}
}

func tbItem(t *testing.T, items []popups.MenuItem, label string) popups.MenuItem {
	t.Helper()
	for _, it := range items {
		if it.Label == label {
			return it
		}
	}
	t.Fatalf("no %q in %q", label, tbLabels(items))
	return popups.MenuItem{}
}

func (e *perfEnv) lastWrite(fs *fakeslack.Server, method, id string) (got map[string]string, ok bool) {
	for _, w := range slices.Backward(fs.TabWrites()) {
		if w.Get("method") == method && w.Get("id") == id {
			got = map[string]string{}
			for k := range w {
				got[k] = w.Get(k)
			}
			return got, true
		}
	}
	return nil, false
}

// getTabContextMenu with useCanEditTabs: Hide tab for a built-in tab,
// Rename tab and Remove tab for a canvas or list, Add link, Rename folder
// and Delete folder for a folder. Rename tab is RenameTabModal
// (conversations.updateTab, the bar's label at once); Delete folder on a
// folder holding a link asks first; Remove tab on a canvas shared
// elsewhere goes at once without keep_file_share. Shots: tabs3-menu,
// tabs3-rename, tabs3-delete-folder (light and dark).
func TestTabBarMenuLikeSlack(t *testing.T) {
	e, fs, ch := tbEnv(t, false)
	for id, want := range map[string][]string{
		"files":     {"Hide tab"},
		tbCanvasTab: {"Rename tab", "─", "Remove tab"},
		tbListTab:   {"Rename tab", "─", "Remove tab"},
		tbFolderTab: {"Add link", "─", "Rename folder", "─", "Delete folder"},
		tbLooseTab:  {"Add link", "─", "Rename folder", "─", "Delete folder"},
	} {
		if got := tbLabels(e.a.tabMenuItems(ch, tbTab(t, e, ch, id))); !slices.Equal(got, want) {
			t.Errorf("%s's menu %q, want %q", id, got, want)
		}
	}
	if items := e.a.tabMenuItems(ch, e.a.channelTabs(ch)[0]); len(items) != 0 {
		t.Errorf("Messages has a menu %q", tbLabels(items))
	}
	// the right-click on the canvas tab
	cp, _, _ := tbSpan(t, e, ch, "tab:"+tbCanvasTab)
	e.rightClickAt(cp, time.Duration(time.Now().UnixNano()))
	e.until("the canvas tab's menu", 5*time.Second, func() bool {
		m := e.topMenu()
		return m != nil && slices.Contains(m.Labels(), "Rename tab")
	})
	pfShot(e, "tabs3-menu")
	m := e.topMenu()
	ren := tbItem(t, m.Items(), "Rename tab")
	m.Dismiss()
	ren.Do()
	e.frame()
	r, ok := e.a.overlays[len(e.a.overlays)-1].(*popups.RenameTabModal)
	if !ok || r.Title() != "Rename tab" {
		t.Fatalf("overlay %T", e.a.overlays[len(e.a.overlays)-1])
	}
	if r.Label() != tbTab(t, e, ch, tbCanvasTab).label {
		t.Errorf("the field starts %q, want the canvas's title", r.Label())
	}
	r.SetLabel("Roadmap")
	pfShot(e, "tabs3-rename")
	r.Save()
	e.frame()
	if got := tbTab(t, e, ch, tbCanvasTab).label; got != "Roadmap" {
		t.Errorf("the bar says %q right after Save", got)
	}
	e.until("conversations.updateTab", 10*time.Second, func() bool { return fs.GalleryTabLabels()[tbCanvasTab] == "Roadmap" })
	if w, _ := e.lastWrite(fs, "conversations.updateTab", tbCanvasTab); w["label"] != "Roadmap" || w["channel_id"] != ch {
		t.Errorf("updateTab %v", w)
	}

	// Rename folder's wording
	tbItem(t, e.a.tabMenuItems(ch, tbTab(t, e, ch, tbFolderTab)), "Rename folder").Do()
	e.frame()
	if r, ok := e.a.overlays[len(e.a.overlays)-1].(*popups.RenameTabModal); !ok || r.Title() != "Rename folder" || r.Label() != "Launch docs" {
		t.Errorf("Rename folder: %T", e.a.overlays[len(e.a.overlays)-1])
	} else {
		r.Dismiss()
	}

	// Delete folder on a folder with a link: TabDeleteConfirmationModal
	a := e.a.Core()
	if _, err := a.AddFolderLink(context.Background(), ch, fakeslack.GalleryFolder, "https://example.com/spec", "Spec"); err != nil {
		t.Fatal(err)
	}
	e.until("the link in the folder", 5*time.Second, func() bool { return a.FolderItemCount(ch, fakeslack.GalleryFolder) == 1 })
	tbItem(t, e.a.tabMenuItems(ch, tbTab(t, e, ch, tbFolderTab)), "Delete folder").Do()
	title := "Are you sure you want to delete this folder with 1 item?"
	e.until("the delete question", 5*time.Second, func() bool { return e.topModal(title) != nil })
	pfShot(e, "tabs3-delete-folder")
	e.topModal(title).Press("Delete")
	e.until("the folder tab gone", 10*time.Second, func() bool {
		return !slices.Contains(fs.GalleryTabs(), tbFolderTab) && !slices.Contains(tbKeys(e, ch), tbFolderTab)
	})

	// Remove tab on a canvas shared elsewhere: at once, the share kept out
	fs.SetGalleryHasShares(true)
	tbItem(t, e.a.tabMenuItems(ch, tbTab(t, e, ch, tbCanvasTab)), "Remove tab").Do()
	e.until("the canvas tab gone", 10*time.Second, func() bool { _, ok := e.lastWrite(fs, "conversations.removeTab", tbCanvasTab); return ok })
	if w, _ := e.lastWrite(fs, "conversations.removeTab", tbCanvasTab); w["keep_file_share"] != "" {
		t.Errorf("removeTab %v", w)
	}
	asked := false
	for _, x := range fs.TabWrites() {
		asked = asked || x.Get("method") == "files.hasShares" && x.Get("file_id") == fakeslack.CanvasChannel
	}
	if !asked {
		t.Error("no files.hasShares before removing the canvas tab")
	}
}

// Dragging tabs on the bar (DraggableDroppableTabs): the list onto Files
// & links' left goes first (conversations.moveTab without after, the bar at
// once); the canvas onto a folder's centre is the folder's file bookmark
// and removeTab; Files & links onto the last tab's right goes last (after
// it). Shots: tabs3-drag (the indicator and the preview), tabs3-drag-folder.
func TestTabBarDragLikeSlack(t *testing.T) {
	e, fs, ch := tbEnv(t, false)
	lp, _, _ := tbSpan(t, e, ch, "tab:"+tbListTab)
	fp, fx0, _ := tbSpan(t, e, ch, "tab:files")
	tbDrag(e, lp, image.Pt(fx0+3, fp.Y), func() {
		if !e.a.TabDragging(ch) {
			t.Fatal("no drag after the slop")
		}
		tgt, ok := e.a.dragTarget(e.a.tabDrag())
		if !ok || tgt.id != "files" || e.a.tabDrag().posOf(tgt) != "left" {
			t.Errorf("target %+v %v", tgt, ok)
		}
		pfShot(e, "tabs3-drag")
	})
	if got := tbKeys(e, ch); len(got) < 3 || got[1] != tbListTab || got[2] != "files" {
		t.Errorf("the bar after the drop %v", got)
	}
	e.until("conversations.moveTab", 10*time.Second, func() bool { _, ok := e.lastWrite(fs, "conversations.moveTab", tbListTab); return ok })
	if w, _ := e.lastWrite(fs, "conversations.moveTab", tbListTab); w["after"] != "" {
		t.Errorf("moveTab %v (first after Messages takes no after)", w)
	}
	if g := fs.GalleryTabs(); len(g) == 0 || g[0] != tbListTab {
		t.Errorf("server order %v", g)
	}
	// a drop isn't a click: Messages stays open
	if e.a.chTab[ch] != 0 {
		t.Errorf("the drop opened tab %d", e.a.chTab[ch])
	}

	// the canvas into Launch docs (its centre)
	cp, _, _ := tbSpan(t, e, ch, "tab:"+tbCanvasTab)
	dp, dx0, dx1 := tbSpan(t, e, ch, "tab:"+tbFolderTab)
	tbDrag(e, cp, image.Pt((dx0+dx1)/2, dp.Y), func() {
		tgt, ok := e.a.dragTarget(e.a.tabDrag())
		if !ok || tgt.id != tbFolderTab || e.a.tabDrag().posOf(tgt) != "center" {
			t.Errorf("folder target %+v %v", tgt, ok)
		}
		pfShot(e, "tabs3-drag-folder")
	})
	e.until("the canvas in the folder", 10*time.Second, func() bool {
		for _, b := range fs.GalleryBookmarkAdds() {
			if b.Get("type") == "file" && b.Get("entity_id") == fakeslack.CanvasChannel && b.Get("parent_id") == fakeslack.GalleryFolder {
				_, removed := e.lastWrite(fs, "conversations.removeTab", tbCanvasTab)
				return removed
			}
		}
		return false
	})
	e.until("the canvas tab gone", 10*time.Second, func() bool { return !slices.Contains(tbKeys(e, ch), tbCanvasTab) })

	// Files & links onto the last tab's right side
	fp, _, _ = tbSpan(t, e, ch, "tab:files")
	keys := tbKeys(e, ch)
	lastID := keys[len(keys)-1]
	bp, _, bx1 := tbSpan(t, e, ch, "tab:"+lastID)
	tbDrag(e, fp, image.Pt(bx1-3, bp.Y), nil)
	e.until("Files & links last", 10*time.Second, func() bool {
		w, ok := e.lastWrite(fs, "conversations.moveTab", "files")
		return ok && w["after"] == lastID
	})
	if got := tbKeys(e, ch); got[len(got)-1] != "files" {
		t.Errorf("the bar %v", got)
	}
}

// The More tab while dragging (DroppableOverflowMenuTab): a bar tab held
// over More opens its menu after 500 ms and dropped there goes to the end;
// a menu row dragged onto a bar tab's left lands before it; a row's
// right-click (channel_tab_overflow_right_click) has Rename and Remove.
// Shots: tabs3-more-drag, tabs3-more-row-drag.
func TestTabBarMoreDragLikeSlack(t *testing.T) {
	e, fs, ch := tbEnv(t, true)
	var visible []tabRect
	for _, r := range e.a.tabDrag().rects {
		if r.drag {
			visible = append(visible, r)
		}
	}
	src := visible[0]
	sp, _, _ := tbSpan(t, e, ch, src.key)
	pm, ok := e.scanHover(e.a.clicks.get("tab."+ch+".more"), image.Pt(e.a.primaryOrigin.X+20, sp.Y), image.Pt(e.size.X-10, sp.Y))
	if !ok {
		t.Fatal("no More tab")
	}
	all := tbKeys(e, ch)
	lastID := all[len(all)-1]
	tbDrag(e, sp, pm, func() {
		e.until("More's menu opened by the drag", 3*time.Second, func() bool {
			m := e.topMenu()
			return m != nil && slices.Contains(m.Labels(), "Edit tabs")
		})
		pfShot(e, "tabs3-more-drag")
	})
	e.until("the tab moved to the end", 10*time.Second, func() bool {
		w, ok := e.lastWrite(fs, "conversations.moveTab", src.id)
		return ok && w["after"] == lastID
	})
	if got := tbKeys(e, ch); got[len(got)-1] != src.id {
		t.Errorf("the bar %v", got)
	}
	e.settle(200 * time.Millisecond)

	// a row of the menu onto the first draggable bar tab's left
	var tgt tabRect
	for _, r := range e.a.tabDrag().rects {
		if r.drag {
			tgt = r
			break
		}
	}
	tp, tx0, _ := tbSpan(t, e, ch, tgt.key)
	e.click(pm)
	e.until("the More menu", 5*time.Second, func() bool { return e.topMenu() != nil })
	e.settle(100 * time.Millisecond)
	m := e.topMenu()
	hidden := e.a.TabsHidden(ch)
	row := e.a.channelTabs(ch)[hidden[0]]
	rr := m.RowRect(row.label)
	if rr.Empty() {
		t.Fatalf("no row %q in %q", row.label, m.Labels())
	}
	rowID := strings.TrimPrefix(row.key, "tab:")
	tbDrag(e, image.Pt(rr.Min.X+rr.Dx()/2, rr.Min.Y+rr.Dy()/2), image.Pt(tx0+3, tp.Y), func() {
		if d := e.a.tabDrag(); !d.dragging || !d.src.inMenu {
			t.Errorf("no drag from the menu row (%+v)", d.src)
		}
		pfShot(e, "tabs3-more-row-drag")
	})
	e.until("the row's tab moved", 10*time.Second, func() bool { _, ok := e.lastWrite(fs, "conversations.moveTab", rowID); return ok })
	keys := tbKeys(e, ch)
	if i, j := slices.Index(keys, rowID), slices.Index(keys, tgt.id); i < 0 || j < 0 || i > j {
		t.Errorf("the row's tab %s isn't before %s: %v", rowID, tgt.id, keys)
	}

	// a More row's right-click: the tab's own menu with Rename / Remove
	e.settle(200 * time.Millisecond)
	e.click(pm)
	e.until("the More menu", 5*time.Second, func() bool { return e.topMenu() != nil })
	e.settle(100 * time.Millisecond)
	m = e.topMenu()
	hidden = e.a.TabsHidden(ch)
	var pick chTab
	for _, i := range hidden {
		if tb := e.a.channelTabs(ch)[i]; core.RenameableTab(tb.typ) {
			pick = tb
		}
	}
	if pick.key == "" {
		t.Skipf("no renameable tab in More at %v", e.size)
	}
	rr = m.RowRect(pick.label)
	rp := image.Pt(rr.Min.X+rr.Dx()/2, rr.Min.Y+rr.Dy()/2)
	e.move(f32.Pt(float32(rp.X), float32(rp.Y)))
	e.rightClickAt(rp, time.Duration(time.Now().UnixNano()))
	e.until("the row's tab menu", 5*time.Second, func() bool {
		top := e.topMenu()
		return top != nil && top != m && (slices.Contains(top.Labels(), "Rename tab") || slices.Contains(top.Labels(), "Rename folder"))
	})
}
