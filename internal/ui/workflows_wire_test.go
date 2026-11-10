package ui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Workflows through the shell, against internal/fakeslack's
// #workflows-gallery only (Size.Workflows).

func workflowsEnv(t *testing.T) (*perfEnv, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.Workflows = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if dir := os.Getenv("WORKFLOWS_SHOTS_DIR"); dir != "" && e.gpu == nil {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	return e, fs
}

// wfShot saves the last frame as name.png (WORKFLOWS_SHOTS_DIR only).
func (e *perfEnv) wfShot(name string) {
	dir := os.Getenv("WORKFLOWS_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		e.tb.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		e.tb.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		e.tb.Fatal(err)
	}
}

// TestAddWorkflowChannelMenu: the channel menu has Slack's "Add a
// workflow" (not in DMs), which opens the workflow picker; a picked
// workflow is bookmarked with Slack's bookmarks.add arguments and the
// channel opens.
func TestAddWorkflowChannelMenu(t *testing.T) {
	e, fs := workflowsEnv(t)
	ch := fakeslack.WorkflowsChannelID
	m := popups.NewChannelMenu(e.a, image.Pt(900, 60), ch)
	if l := strings.Join(m.Labels(), " | "); !strings.Contains(l, "--- | Add a workflow | Edit settings") {
		t.Errorf("channel menu: %s", l)
	}
	for id, c := range e.a.model().Convs {
		if c.Kind == "im" {
			if l := strings.Join(popups.NewChannelMenu(e.a, image.Pt(900, 60), id).Labels(), " | "); strings.Contains(l, "Add a workflow") {
				t.Errorf("a DM's menu has Add a workflow: %s", l)
			}
			break
		}
	}
	n := len(e.a.overlays)
	popups.AddWorkflow(e.a, ch) // the row's Do
	e.settle(300 * time.Millisecond)
	if len(e.a.overlays) <= n {
		t.Fatal("Add a workflow opened no picker")
	}
	if _, ok := e.a.overlays[len(e.a.overlays)-1].(*popups.Modal); !ok {
		t.Errorf("top overlay %T, want the picker's modal", e.a.overlays[len(e.a.overlays)-1])
	}
	e.a.addWorkflowBookmark(ch, core.WorkflowTrigger{ID: fakeslack.TriggerDeploy, Name: "Run deploy"}, "")
	e.until("the bookmark", 10*time.Second, func() bool { return len(fs.AddedBookmarks()) == 1 })
	b := fs.AddedBookmarks()[0]
	if b["type"] != "shortcut" || b["shortcut_id"] != fakeslack.TriggerDeploy || b["title"] != "Run deploy" || b["link"] != "" {
		t.Errorf("bookmark %v", b)
	}
	e.until("the channel", 10*time.Second, func() bool { return e.a.router.Current().Channel() == ch })

	// the tab bar's "+": "Workflow" only while the channel has no
	// Workflows tab (the gallery has one)
	e.until("the gallery's tabs", 10*time.Second, func() bool { return e.a.data.tabsDone[ch] })
	if items := e.a.addTabWorkflowItems(ch); len(items) != 0 {
		t.Errorf("a channel with a Workflows tab lists Workflow in +: %v", items)
	}
	if items := e.a.addTabWorkflowItems(fs.ChannelIDs()[0]); len(items) != 1 || items[0].Label != "Workflow" || items[0].Icon != "lightning" {
		t.Errorf("tab bar + Workflow: %+v", items)
	}
}

// TestWorkflowBookmarkTrips: a bookmarked workflow in the folder tab trips
// with Slack's bookmark context instead of opening the browser.
func TestWorkflowBookmarkTrips(t *testing.T) {
	e, fs := workflowsEnv(t)
	ch := fakeslack.WorkflowsChannelID
	bm := core.Bookmark{ID: fakeslack.WorkflowsBookmarkID, Title: "Request time off", Type: "shortcut",
		Link: fakeslack.WorkflowLink(fakeslack.TriggerDeploy), ShortcutID: fakeslack.TriggerDeploy}
	if !e.a.tripBookmark(ch, bm) {
		t.Fatal("a workflow bookmark didn't trip")
	}
	e.until("the trip", 10*time.Second, func() bool { return len(fs.TripCalls()) == 1 })
	c := fs.TripCalls()[0]
	if c.Context["location"] != "bookmark" || c.Context["bookmark_id"] != fakeslack.WorkflowsBookmarkID || c.Context["channel_id"] != ch {
		t.Errorf("trip context %v", c.Context)
	}
	// a bookmark without its link resolves the trigger first
	bm.Link = ""
	if !e.a.tripBookmark(ch, bm) {
		t.Fatal("a linkless shortcut bookmark didn't trip")
	}
	e.until("the second trip", 10*time.Second, func() bool { return len(fs.TripCalls()) == 2 })
	if u := fs.TripCalls()[1].URL; u != fakeslack.WorkflowLink(fakeslack.TriggerDeploy) {
		t.Errorf("trip url %q", u)
	}
	if e.a.tripBookmark(ch, core.Bookmark{ID: "Bk2", Title: "Docs", Type: "link", Link: "https://example.com/docs"}) {
		t.Error("a plain link bookmark tripped")
	}
}

// TestWorkflowsTab: the channel's Workflows tab (Slack's
// useChannelWorkflowsExplorer) lists the featured workflows (one the
// viewer may not see as Slack's placeholder) and the bookmarked ones;
// "Add a workflow" opens it once the tab bar has it.
func TestWorkflowsTab(t *testing.T) {
	e, _ := workflowsEnv(t)
	ch := fakeslack.WorkflowsChannelID
	e.openChannel(ch)
	e.until("the gallery's tabs", 10*time.Second, func() bool { return e.a.data.tabsDone[ch] })
	if e.a.workflowsTabView(ch) != nil {
		t.Fatal("the Workflows tab is open before it was asked for")
	}
	e.a.openWorkflowsTab(ch)
	e.until("the Workflows tab", 10*time.Second, func() bool { return e.a.workflowsTabView(ch) != nil })
	st := e.a.wfTab(ch)
	e.until("its workflows", 10*time.Second, func() bool {
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.loaded && !st.loading
	})
	st.mu.Lock()
	tab := st.tab
	st.mu.Unlock()
	if len(tab.Featured) != 2 || tab.Featured[0].Trigger.Title() != "Run deploy" || !tab.Featured[1].Denied ||
		len(tab.Bookmarked) != 1 || tab.Bookmarked[0].Trigger.Title() != "Request time off" || !tab.CanManage {
		t.Errorf("tab %+v", tab)
	}
	e.settle(300 * time.Millisecond)
	e.wfShot("workflows-tab-light")
	// the Messages tab closes it
	e.a.chTab[ch] = 0
	if e.a.workflowsTabView(ch) != nil {
		t.Error("the Workflows tab stayed open on Messages")
	}
}
