package ui

import (
	"image"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/canvasview"
	"github.com/some-du6e/clack/internal/ui/listview"
)

// The deleted-file-tab fixture: #files-gallery's bar holds tabs whose files
// files.info refuses, each answered per file id (fakeslack/file_gone.go),
// and a live canvas tab for the peek.
const (
	dtDelCanvas   = "Ctb0DTDEL01" // an unlabeled canvas tab, file_deleted
	dtDelLabeled  = "Ctb0DTDEL02" // labeled "Roadmap", file_deleted
	dtDelList     = "Ctb0DTDEL03" // an unlabeled list tab, file_deleted
	dtNotFound    = "Ctb0DTDEL04" // an unlabeled canvas tab, file_not_found
	dtHidden      = "Ctb0DTDEL05" // an unlabeled canvas tab, not_visible
	dtHiddenLabel = "Ctb0DTDEL06" // labeled "Plans", not_visible
	dtAddCanvas   = "Ctb0DTDEL07" // channel_canvas, no file
	dtLive        = "Ctb0DTLIVE1" // the channel's live canvas
	dtLiveList    = "Ctb0DTLIVE2" // a live list

	dtFDelCanvas  = "F0DTDEL0001"
	dtFDelLabeled = "F0DTDEL0002"
	dtFDelList    = "F0DTDEL0003"
	dtFNotFound   = "F0DTDEL0004"
	dtFHidden     = "F0DTDEL0005"
	dtFHiddenLbl  = "F0DTDEL0006"
)

// dtEnv is the client on #files-gallery with the fixture's tabs, wide
// enough that every tab shows.
func dtEnv(t *testing.T) (*perfEnv, *fakeslack.Server, string) {
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
		fakeslack.GalleryTab(dtDelCanvas, "canvas", "", dtFDelCanvas, ""),
		fakeslack.GalleryTab(dtDelLabeled, "canvas", "Roadmap", dtFDelLabeled, ""),
		fakeslack.GalleryTab(dtDelList, "list", "", dtFDelList, ""),
		fakeslack.GalleryTab(dtNotFound, "canvas", "", dtFNotFound, ""),
		fakeslack.GalleryTab(dtHidden, "canvas", "", dtFHidden, ""),
		fakeslack.GalleryTab(dtHiddenLabel, "canvas", "Plans", dtFHiddenLbl, ""),
		fakeslack.GalleryTab(dtAddCanvas, "channel_canvas", "", "", ""),
		fakeslack.GalleryTab(dtLive, "canvas", "", fakeslack.CanvasChannel, ""),
		fakeslack.GalleryTab(dtLiveList, "list", "", fakeslack.ListTodos, ""),
	)
	fs.SetFileGone(dtFDelCanvas, "file_deleted", nil)
	fs.SetFileGone(dtFDelLabeled, "file_deleted", nil)
	fs.SetFileGone(dtFDelList, "file_deleted", nil)
	fs.SetFileGone(dtFNotFound, "file_not_found", nil)
	secret := map[string]any{"filetype": "quip", "title": "Secret plan"}
	fs.SetFileGone(dtFHidden, "not_visible", secret)
	fs.SetFileGone(dtFHiddenLbl, "not_visible", secret)
	fs.SetCanUndelete(dtFDelList, true, "Groceries", time.Now().Unix())
	e := startEnv(t, fs, t.TempDir())
	e.size = image.Pt(2400, 800)
	e.waitReady()
	e.openChannel(ch)
	e.until("the tabs", 15*time.Second, func() bool {
		return e.a.data.tabsDone[ch] && len(e.a.channelTabs(ch)) == 11
	})
	e.settle(300 * time.Millisecond)
	return e, fs, ch
}

// dtMid is the middle of ch's tab key (its label's centre line), and the
// tab's x span: tbSpan's point is the tab's top edge, which may sit above
// the 32.25px label.
func dtMid(t *testing.T, e *perfEnv, ch, key string) (p image.Point, x0, x1 int) {
	t.Helper()
	p, x0, x1 = tbSpan(t, e, ch, key)
	btn := e.a.clicks.get("tab." + ch + "." + key)
	y := p.Y
	for btn.Hovered() && y < p.Y+200 {
		y++
		e.move(f32.Pt(float32(p.X), float32(y)))
	}
	p.Y = (p.Y + y - 1) / 2
	e.move(f32.Pt(float32(p.X), float32(p.Y)))
	return p, x0, x1
}

// dtTab is ch's tab with tabz id id, and its index in the bar.
func dtTab(t *testing.T, e *perfEnv, ch, id string) (chTab, int) {
	t.Helper()
	for i, tb := range e.a.channelTabs(ch) {
		if tb.key == "tab:"+id {
			return tb, i
		}
	}
	t.Fatalf("no tab %s in %s's bar", id, ch)
	return chTab{}, -1
}

// A gone file's tab draws by Slack's three paths (useTabLabel,
// ChannelTabLabelBaseComponent, SmallFileEntity): an unlabeled canvas
// the document label with its file's Name and SmallFileEntity's icon, a
// labeled tab its label and type icon (a gated file's Name and
// eye-closed), an unlabeled list under list_channel_tab_show_view_name_fe
// "Untitled"; channel_canvas is the AddCanvas placeholder.
func TestDeletedTabLabels(t *testing.T) {
	e, _, ch := dtEnv(t)
	check := func(tabs []chTab, id, icon, label string, doc bool) {
		t.Helper()
		for _, tb := range tabs {
			if tb.key != "tab:"+id {
				continue
			}
			if tb.icon != icon || tb.label != label || tb.doc != doc {
				t.Errorf("tab %s: icon %q label %q doc %v, want %q %q %v", id, tb.icon, tb.label, tb.doc, icon, label, doc)
			}
			return
		}
		t.Errorf("no tab %s", id)
	}
	// the account's groups: list_channel_tab_show_view_name_fe on
	e.a.Core().SetExperimentGroup("list_channel_tab_show_view_name_fe", "on")
	tabs, _ := e.a.data.tabsOf(e.a.data.tabSrc[ch].tabs, false)
	check(tabs, dtDelCanvas, "trash", "Deleted file", true)
	check(tabs, dtDelLabeled, "canvas", "Roadmap", false)
	check(tabs, dtDelList, "list-tab", "Untitled", false)
	check(tabs, dtNotFound, "question", "Hidden file", true)
	check(tabs, dtHidden, "question", "Private canvas", true)
	check(tabs, dtHiddenLabel, "eye-closed", "Private canvas", false)
	check(tabs, dtAddCanvas, "add-channel-canvas", "Add canvas", false)
	// view names off: the list takes the document label
	e.a.Core().SetExperimentGroup("list_channel_tab_show_view_name_fe", "off")
	off, _ := e.a.data.tabsOf(e.a.data.tabSrc[ch].tabs, false)
	check(off, dtDelList, "trash", "Deleted file", true)
	// the files.info answers, as core keeps them
	for _, c := range []struct {
		id, status, name string
		gated            bool
	}{
		{dtDelCanvas, "deleted", "Deleted file", false},
		{dtNotFound, "hidden", "Hidden file", false},
		{dtHidden, "hidden", "Private canvas", true},
	} {
		for _, tb := range e.a.data.tabSrc[ch].tabs {
			if tb.ID != c.id {
				continue
			}
			if st, name, gated, ok := dtTabGone(tb); !ok || st != c.status || name != c.name || gated != c.gated {
				t.Errorf("tab %s: gone %v %q %q gated %v, want %q %q %v", c.id, ok, st, name, gated, c.status, c.name, c.gated)
			}
		}
	}
}

// A click on a gone file's tab opens it in place (navigateToChannelTab),
// as a click from the More menu and a tab permalink do: the body is the
// canvas view in QuipDeletedState or the list view in BaseListErrorState,
// and nothing goes to the browser.
func TestDeletedTabOpensInPlace(t *testing.T) {
	e, fs, ch := dtEnv(t)
	var mu sync.Mutex
	var opened []string
	prev := openURL
	openURL = func(u string) { mu.Lock(); opened = append(opened, u); mu.Unlock() }
	t.Cleanup(func() { openURL = prev })
	canvasGone := func(id string) bool {
		v, ok := e.a.canvasTabView(ch).(*canvasview.View)
		if !ok || v.ID() != id {
			return false
		}
		return dtCanvasGone(v)
	}
	listGone := func() bool {
		v, ok := e.a.listTabView(ch).(*listview.View)
		return ok && dtListGone(v)
	}
	toMessages := func() {
		e.a.chTab[ch] = 0
		e.settle(50 * time.Millisecond)
	}
	// a click
	p, _, _ := tbSpan(t, e, ch, "tab:"+dtDelCanvas)
	e.click(p)
	e.until("the deleted canvas in its tab", 10*time.Second, func() bool { return canvasGone(dtFDelCanvas) })
	if _, i := dtTab(t, e, ch, dtDelCanvas); e.a.chTab[ch] != i {
		t.Errorf("tab %d selected, want the deleted canvas's %d", e.a.chTab[ch], i)
	}
	v := e.a.canvasTabView(ch).(*canvasview.View)
	e.settle(100 * time.Millisecond)
	title, _ := dtCanvasTitle(v)
	if !slices.ContainsFunc(dtDeletedTitles, func(s string) bool { return s == title }) {
		t.Errorf("deleted canvas title %q, want one of QuipDeletedState's six", title)
	}
	toMessages()
	p, _, _ = tbSpan(t, e, ch, "tab:"+dtDelList)
	e.click(p)
	e.until("the deleted list in its tab", 10*time.Second, listGone)
	e.settle(300 * time.Millisecond)
	lv := e.a.listTabView(ch).(*listview.View)
	e.until("files.canUndelete's answer", 5*time.Second, func() bool { _, _, r := dtListText(lv); return r })
	if ti, sub, _ := dtListText(lv); ti != "This list was deleted." || sub != "Was that a mistake? Restore this list." {
		t.Errorf("deleted list %q / %q", ti, sub)
	}
	if calls := fs.CanUndeleteCalls(); len(calls) == 0 || calls[0].Get("file_id") != dtFDelList {
		t.Errorf("files.canUndelete calls %v, want one for %s", calls, dtFDelList)
	}
	// a tab permalink
	toMessages()
	e.a.OpenChannelTabLink("https://fakeclub.slack.com/archives/" + ch + "/tabs/" + dtNotFound)
	e.until("the not-found canvas from its link", 10*time.Second, func() bool { return canvasGone(dtFNotFound) })
	if title, _ := dtCanvasTitle(e.a.canvasTabView(ch).(*canvasview.View)); title != "This canvas was not found." {
		t.Errorf("not-found canvas title %q", title)
	}
	// the not-visible canvas: its Request Access button
	toMessages()
	e.a.OpenChannelTabLink("https://fakeclub.slack.com/archives/" + ch + "/tabs/" + dtHidden)
	e.until("the not-visible canvas", 10*time.Second, func() bool { return canvasGone(dtFHidden) })
	e.settle(100 * time.Millisecond)
	if title, btn := dtCanvasTitle(e.a.canvasTabView(ch).(*canvasview.View)); title != "You don’t have access to this canvas." || btn != "Request Access" {
		t.Errorf("not-visible canvas %q, button %q", title, btn)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(opened) > 0 {
		t.Errorf("a gone file's tab went to the browser: %v", opened)
	}
}

// dtDeletedTitles are QuipDeletedState's six (nAOp Ft).
var dtDeletedTitles = []string{"This canvas has gone fishing", "This canvas has flown the coop", "This canvas was swept away",
	"This canvas is on permanent vacation", "This canvas was retired", "This canvas’s work is all done"}

// ChannelTabLabelWithPeek: hovering an unselected tab's label opens its
// peek after 300 ms, bottom-left of the label moved by -8, -6, 360px wide;
// the selected tab and Messages have none; a press closes it and selects
// the tab; the pointer may go onto the card, and leaving away from it
// closes it; a deleted canvas's card is QuipDeletedState; a canvas peek is
// a preview (no files.open). peeksEnabledOnMouseEnter is the rail's gate
// (peeksOnHover), tested there.
func TestChannelTabPeek(t *testing.T) {
	e, _, ch := dtEnv(t)
	calls := e.recordCalls("files.open")
	key := dtTabPeekKey(ch, dtLive)
	p, x0, x1 := dtMid(t, e, ch, "tab:"+dtLive)
	away := image.Pt(p.X, p.Y-60) // the channel header: off the label and the card
	e.move(f32.Pt(float32(away.X), float32(away.Y)))
	e.settle(100 * time.Millisecond)
	if k := e.a.anyTabPeekOpen(); k != "" {
		t.Fatalf("peek %q still open with the pointer in the header", k)
	}
	// hover: nothing before 300 ms, the card from then on
	t0 := time.Now()
	e.move(f32.Pt(float32(p.X), float32(p.Y)))
	var openedAt time.Duration
	e.until("the live canvas tab's peek", 3*time.Second, func() bool {
		if e.a.tabPeekOpen(key) {
			openedAt = time.Since(t0)
			return true
		}
		return false
	})
	if openedAt < 300*time.Millisecond {
		t.Errorf("the peek opened after %v, before PeekTrigger's 300 ms", openedAt)
	}
	e.settle(250 * time.Millisecond) // the card's .16s slide is over
	pop := e.a.tpeeks.m[key].pop
	r := pop.Rect()
	if r.Dx() != 360 {
		t.Errorf("card %v, want 360px wide", r)
	}
	trig := e.a.tpeeks.m[key].trig
	if r.Min.X != trig.Min.X-8 || r.Min.Y != trig.Max.Y-6 {
		t.Errorf("card at %v, want the label %v's bottom left moved by -8, -6", r.Min, trig)
	}
	if trig.Min.X != x0 && trig.Min.X != x0-1 && trig.Min.X != x0+1 || absPx(trig.Max.X-x1) > 1 {
		t.Errorf("trigger %v, want the tab's label across %d..%d", trig, x0, x1)
	}
	if trig.Dy() != 32 && trig.Dy() != 33 {
		t.Errorf("trigger %v, want the 32.25px label box", trig)
	}
	// the hover look while the card shows
	var looks []string
	dtLookHook(func(k string) { looks = append(looks, k) })
	t.Cleanup(func() { dtLookHook(nil) })
	e.move(f32.Pt(float32(r.Min.X+20), float32(r.Min.Y+20)))
	e.settle(50 * time.Millisecond)
	if !e.a.tabPeekOpen(key) {
		t.Fatal("the card closed when the pointer went onto it")
	}
	looks = nil
	e.frame()
	if !slices.Contains(looks, "tab:"+dtLive) {
		t.Errorf("with the pointer on the card the tab drew no hover look (looks %v)", looks)
	}
	dtLookHook(nil)
	if n := len(calls.of("files.open", "", "file")); n > 0 {
		t.Errorf("a canvas peek called files.open %d times", n)
	}
	// a press on the label closes it and selects the tab
	e.click(p)
	e.settle(100 * time.Millisecond)
	if e.a.tabPeekOpen(key) {
		t.Error("a press left the peek open")
	}
	if _, i := dtTab(t, e, ch, dtLive); e.a.chTab[ch] != i {
		t.Errorf("tab %d selected after the press, want %d", e.a.chTab[ch], i)
	}
	// the selected tab: no peek
	e.move(f32.Pt(float32(away.X), float32(away.Y)))
	e.settle(50 * time.Millisecond)
	e.move(f32.Pt(float32(p.X), float32(p.Y)))
	e.settle(500 * time.Millisecond)
	if e.a.tabPeekOpen(key) {
		t.Error("the selected tab opened a peek")
	}
	// Messages: none either (an empty fragment)
	mp, _, _ := dtMid(t, e, ch, "messages")
	e.click(mp)
	e.settle(100 * time.Millisecond)
	e.move(f32.Pt(float32(mp.X), float32(mp.Y)))
	e.settle(500 * time.Millisecond)
	if k := e.a.anyTabPeekOpen(); k != "" {
		t.Errorf("hovering Messages opened %q", k)
	}
	// a deleted canvas's peek: QuipDeletedState
	dp, _, _ := dtMid(t, e, ch, "tab:"+dtDelCanvas)
	e.move(f32.Pt(float32(away.X), float32(away.Y)))
	e.settle(50 * time.Millisecond)
	e.move(f32.Pt(float32(dp.X), float32(dp.Y)))
	dkey := dtTabPeekKey(ch, dtDelCanvas)
	e.until("the deleted canvas's peek", 3*time.Second, func() bool { return e.a.tabPeekOpen(dkey) })
	e.until("the peek's deleted state", 5*time.Second, func() bool {
		v := dtTabPeekCanvas(e, dkey)
		return v != nil && dtCanvasGone(v)
	})
	// leaving past the safe triangle closes it
	e.move(f32.Pt(float32(dp.X), float32(dp.Y-60)))
	e.settle(100 * time.Millisecond)
	if e.a.tabPeekOpen(dkey) {
		t.Error("leaving the label upward left the peek open")
	}
}

// file_deleted turns a live tab into a deleted file's and the open canvas
// tab into QuipDeletedState at once (tombstoneFiles); file_undeleted brings
// both back.
func TestDeletedTabLive(t *testing.T) {
	e, fs, ch := dtEnv(t)
	go e.a.Core().SetRealtime(true)
	e.until("the socket", 15*time.Second, func() bool { return e.a.data.m.Connected && fs.Sockets() > 0 })
	p, _, _ := tbSpan(t, e, ch, "tab:"+dtLive)
	e.click(p)
	cv := func() *canvasview.View { v, _ := e.a.canvasTabView(ch).(*canvasview.View); return v }
	e.until("the live canvas", 10*time.Second, func() bool { v := cv(); return v != nil && v.Doc() != nil })
	fs.PushFileDeleted(fakeslack.CanvasChannel)
	e.until("the tab's deleted look", 10*time.Second, func() bool {
		tb, _ := dtTab(t, e, ch, dtLive)
		return tb.icon == "trash" && tb.label == "Deleted file"
	})
	e.until("the canvas's deleted state", 10*time.Second, func() bool { return dtCanvasGone(cv()) })
	fs.PushFileUndeleted(fakeslack.CanvasChannel)
	e.until("the canvas again", 10*time.Second, func() bool { v := cv(); return v.Doc() != nil })
	e.until("the tab again", 15*time.Second, func() bool {
		tb, _ := dtTab(t, e, ch, dtLive)
		return tb.icon != "trash" && !strings.HasPrefix(tb.label, "Deleted")
	})
}

// The relatives: a file bookmark to a canvas opens the canvas view
// (handleUrlOpen), not the browser, in its deleted state when the canvas
// is gone; deleting the file of the open tab (DeleteFileDialog) opens
// Messages, removes the tab (conversations.removeTab) and toasts "This
// list was deleted." at the top right.
func TestDeletedTabRelatives(t *testing.T) {
	e, _, ch := dtEnv(t)
	var mu sync.Mutex
	var opened []string
	prev := openBookmarkURL
	openBookmarkURL = func(u string) { mu.Lock(); opened = append(opened, u); mu.Unlock() }
	t.Cleanup(func() { openBookmarkURL = prev })
	e.a.OpenBookmark(ch, core.Bookmark{ID: "Bk0DTDEL01", Type: "file", EntityID: dtFDelCanvas, Title: "Plan",
		Link: "https://fakeclub.slack.com/docs/" + fakeslack.TeamID + "/" + dtFDelCanvas})
	e.until("the bookmarked canvas's view", 10*time.Second, func() bool {
		v, ok := e.a.secondary.view.(*canvasview.View)
		return ok && v.ID() == dtFDelCanvas && dtCanvasGone(v)
	})
	mu.Lock()
	if len(opened) > 0 {
		t.Errorf("the canvas bookmark went to the browser: %v", opened)
	}
	mu.Unlock()
	e.a.CloseSecondary()
	// the open tab's own list deleted
	calls := e.recordCalls("conversations.removeTab")
	p, _, _ := tbSpan(t, e, ch, "tab:"+dtLiveList)
	e.click(p)
	_, li := dtTab(t, e, ch, dtLiveList)
	e.until("the list tab", 10*time.Second, func() bool { return e.a.chTab[ch] == li })
	if !dtLeaveFileTab(e, fakeslack.ListTodos) {
		t.Fatal("leaveFileTab didn't take the list's own tab")
	}
	if e.a.chTab[ch] != 0 {
		t.Errorf("tab %d selected after the delete, want Messages", e.a.chTab[ch])
	}
	e.until("conversations.removeTab", 5*time.Second, func() bool {
		return slices.Contains(calls.of("conversations.removeTab", "", "id", "channel_id"), "id="+dtLiveList+" channel_id="+ch)
	})
	if dtLeaveFileTab(e, fakeslack.ListTodos) {
		t.Error("leaveFileTab took a tab with Messages selected")
	}
}
