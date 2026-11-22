package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// A right-click on the Files & links tab (getTabContextMenu): with tab
// editing, "Hide tab" (danger); it sends conversations.removeTab, the bar
// drops the tab and the open tab goes back to Messages. View link details
// opens the link details pane in the secondary column.
func TestFilesTabHideAndLinkDetails(t *testing.T) {
	sz := fakeslack.Small
	sz.Files = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.Permissions = map[string][]string{fakeslack.FilesChannelID: {core.PermManageChannelTabs}}
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	ch := fakeslack.FilesChannelID
	e.openChannel(ch)
	e.until("the gallery's tabs", 10*time.Second, func() bool { return e.a.data.tabsDone[ch] })
	var files chTab
	idx := -1
	for i, tb := range e.a.channelTabs(ch) {
		if isFilesTab(tb) {
			files, idx = tb, i
		}
	}
	if idx < 0 {
		t.Fatal("no Files & links tab")
	}
	msgs := e.a.clicks.get("tab." + ch + ".messages")
	o := e.a.primaryOrigin
	mp, ok := e.scanHover(msgs, image.Pt(o.X+40, o.Y), image.Pt(o.X+40, o.Y+200))
	if !ok {
		t.Fatal("no Messages tab in the header")
	}
	btn := e.a.clicks.get("tab." + ch + "." + files.key)
	fp, ok := e.scanHover(btn, mp, image.Pt(e.size.X-60, mp.Y))
	if !ok {
		t.Fatal("no Files & links tab in the header")
	}
	e.click(fp) // open it, so hiding it goes back to Messages
	e.frame()
	e.until("the Files & links view", 5*time.Second, func() bool { return e.a.filesTabView(ch) != nil })
	n := len(e.a.overlays)
	pt := f32.Pt(float32(fp.X), float32(fp.Y))
	e.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonSecondary, Position: pt},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt})
	e.frame()
	e.until("the tab's menu", 10*time.Second, func() bool { return len(e.a.overlays) > n })
	m, ok := e.a.overlays[len(e.a.overlays)-1].(*popups.Menu)
	if !ok {
		t.Fatalf("overlay %T", e.a.overlays[len(e.a.overlays)-1])
	}
	items := m.Items()
	if len(items) != 1 || items[0].Label != "Hide tab" || !items[0].Danger {
		t.Fatalf("menu %+v", items)
	}
	m.Dismiss()
	// a header read already out when the tab goes (a tab click's, a
	// retry's), slow to post: its members call held. It read the bar with
	// the tab; the hide's own read, after, reads it without and answers
	// first (files-filters-3's push ci 44980164338: the old read landed
	// last and kept the hidden tab)
	fs.SetFault("edge/users/list", fakeslack.Fault{Delay: 1500 * time.Millisecond, Times: 1})
	info, members := fs.CallCount("conversations.info"), fs.CallCount("edge/users/list")
	delete(e.a.data.fetched, ch)
	e.a.data.opened(ch)
	// its tabs and info read the bar as it is now (the fake answers what
	// it holds when the request comes), its members call is held
	e.until("the header read out", 5*time.Second, func() bool {
		return fs.CallCount("conversations.info") >= info+2 && fs.CallCount("edge/users/list") > members
	})
	items[0].Do()
	e.until("conversations.removeTab", 10*time.Second, func() bool {
		g := fs.GalleryTabs()
		return len(g) == 1 && g[0] == "-files"
	})
	if w := fs.TabWrites(); len(w) != 1 || w[0].Get("id") != fakeslack.FilesTabID || w[0].Get("channel_id") != ch {
		t.Errorf("writes %v", w)
	}
	if e.a.chTab[ch] != 0 {
		t.Errorf("the hidden tab stayed open (tab %d)", e.a.chTab[ch])
	}
	// every header read out has answered, the held one too: the bar is
	// what the last read of it says, not a moment between two
	e.until("every header read answered", 10*time.Second, func() bool {
		return e.a.data.headerAnswered == e.a.data.headerStarted
	})
	e.until("the bar without the tab", 10*time.Second, func() bool {
		for _, tb := range e.a.channelTabs(ch) {
			if isFilesTab(tb) {
				return false
			}
		}
		return true
	})
	// View link details: the pane, then ✕
	e.a.OpenLinkDetails(fakeslack.LinkSharedTwice, "Roadmap: Q1 2026", "")
	e.frame()
	if got := e.a.LinkDetailsShown(); got != fakeslack.LinkSharedTwice {
		t.Fatalf("pane shows %q", got)
	}
	e.until("the link's conversations", 10*time.Second, func() bool {
		return len(fs.SearchForms("search.modules.messages")) > 0
	})
	e.a.CloseSecondary()
	e.frame()
	if e.a.LinkDetailsShown() != "" {
		t.Error("the pane stayed after closing")
	}
}

// A cold start's header read (no header cache) whose answer comes back
// after a newer read of the same header failed: the older answer is the
// only good one, so it lands. Dropping every answer older than the newest
// read left the bar empty here (the failed read set it to nothing, and
// nothing retries a tab read for 2 minutes).
func TestHeaderOlderReadLandsAfterNewerFails(t *testing.T) {
	sz := fakeslack.Small
	sz.Files = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	e.until("boot's header reads answered", 10*time.Second, func() bool {
		return e.a.data.headerAnswered == e.a.data.headerStarted
	})
	ch := fakeslack.FilesChannelID
	if _, ok := e.a.data.core.CachedHeader(ch); ok {
		t.Fatal("a header cache on a fresh home")
	}
	// the open's read: its tabs and info answered, its members call held
	fs.SetFault("edge/users/list", fakeslack.Fault{Delay: 3 * time.Second, Times: 1})
	info, members := fs.CallCount("conversations.info"), fs.CallCount("edge/users/list")
	e.openChannel(ch)
	e.until("the open's header read out", 5*time.Second, func() bool {
		return fs.CallCount("conversations.info") >= info+2 && fs.CallCount("edge/users/list") > members
	})
	if e.a.data.tabsDone[ch] {
		t.Fatal("the open's read answered before its members call came back")
	}
	// a second read of the header, every conversations.info failing (a
	// 500 is final: internal_error is tried again after a second): it
	// answers first, with nothing
	fs.SetFault("conversations.info", fakeslack.Fault{Status: 500})
	started := e.a.data.headerStarted
	delete(e.a.data.fetched, ch)
	e.a.data.opened(ch)
	if e.a.data.headerStarted != started+1 {
		t.Fatal("no second header read")
	}
	e.until("the failed read answered", 10*time.Second, func() bool { return e.a.data.tabsDone[ch] })
	fs.SetFault("conversations.info", fakeslack.Fault{})
	e.until("every header read answered", 10*time.Second, func() bool {
		return e.a.data.headerAnswered == e.a.data.headerStarted
	})
	e.frame()
	for _, tb := range e.a.channelTabs(ch) {
		if isFilesTab(tb) {
			return
		}
	}
	t.Errorf("the bar has no Files & links tab after the older good read answered: %v", e.a.channelTabs(ch))
}
