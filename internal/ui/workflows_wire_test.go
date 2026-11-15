package ui

import (
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
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

// TestWorkflowsTabSectionLayout: a section heading inside a full-width
// list row keeps its button at the right and room for the title (the
// button was once measured with the row's minimum width, which pushed it
// left and left the title no space).
func TestWorkflowsTabSectionLayout(t *testing.T) {
	a := &App{th: theme.New(false)}
	var ops op.Ops
	w := 900
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, 400)}, Now: time.Now()}
	if bw := buttonWidth(gtx, a.th, popups.BtnOutline, popups.BtnSmall, "Feature workflow"); bw <= 0 || bw > 300 {
		t.Fatalf("button width %d in a %d-wide row", bw, w)
	}
	var act widget.Clickable
	d := a.wfSection("Featured workflows", "Feature up to 15 workflows, so people can use them instead of sending a message.", &act, "Feature workflow", true)(gtx)
	if d.Size.X != w || d.Size.Y < 24+18+2+16 {
		t.Errorf("section %v: the title and subtitle need their lines", d.Size)
	}
}

// openGalleryWorkflowsTab opens #workflows-gallery's Workflows tab and
// waits for its rows.
func openGalleryWorkflowsTab(e *perfEnv) (string, *wfTabState, core.WorkflowsTab) {
	ch := fakeslack.WorkflowsChannelID
	e.openChannel(ch)
	e.until("the gallery's tabs", 10*time.Second, func() bool { return e.a.data.tabsDone[ch] })
	e.a.openWorkflowsTab(ch)
	e.until("the Workflows tab", 10*time.Second, func() bool { return e.a.workflowsTabView(ch) != nil })
	st := e.a.wfTab(ch)
	e.until("its workflows", 10*time.Second, func() bool {
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.loaded && !st.loading
	})
	st.mu.Lock()
	defer st.mu.Unlock()
	return ch, st, st.tab
}

func wfMenuLabels(items []popups.MenuItem) string {
	var out []string
	for _, it := range items {
		if it.Separator {
			out = append(out, "---")
			continue
		}
		out = append(out, it.Label)
	}
	return strings.Join(out, " | ")
}

func wfMenuItem(t *testing.T, items []popups.MenuItem, label string) popups.MenuItem {
	t.Helper()
	for _, it := range items {
		if it.Label == label {
			return it
		}
	}
	t.Fatalf("no %q in %s", label, wfMenuLabels(items))
	return popups.MenuItem{}
}

// TestWorkflowsTabRowMenu: a Workflows tab row's ⋮ is Slack's
// MoreActionsMenu for channel_workflows (KIj/), in its order; a row click
// opens the workflow details view (handleWorkflowItemClick); Feature
// workflow, Remove from bookmarks and Remove from featured make Slack's
// calls.
func TestWorkflowsTabRowMenu(t *testing.T) {
	e, fs := workflowsEnv(t)
	ch, st, tab := openGalleryWorkflowsTab(e)
	if len(tab.Featured) < 1 || len(tab.Bookmarked) != 1 {
		t.Fatalf("tab %+v", tab)
	}
	feat := e.a.workflowRowMenu(ch, st, tab.Featured[0], true)
	if got, want := wfMenuLabels(feat), "Start workflow | --- | Copy workflow link | View details | --- | Add to VIP | --- | Copy workflow ID | --- | Remove from featured"; got != want {
		t.Errorf("featured row menu:\n got %s\nwant %s", got, want)
	}
	bm := e.a.workflowRowMenu(ch, st, tab.Bookmarked[0], false)
	if got, want := wfMenuLabels(bm), "Start workflow | Feature workflow | --- | Copy workflow link | View details | --- | Add to VIP | --- | Copy workflow ID | --- | Remove from bookmarks"; got != want {
		t.Errorf("bookmarked row menu:\n got %s\nwant %s", got, want)
	}
	if it := wfMenuItem(t, feat, "Remove from featured"); !it.Danger || it.Key != "delete_workflow" {
		t.Errorf("remove row %+v", it)
	}

	// the row's click: the details view for the trigger, no channel
	e.a.workflowItemClick(ch, tab.Bookmarked[0])
	p, ok := e.a.secondary.view.(*wfDetailsPane)
	if !ok || e.a.secondary.kind != secWorkflow || p.trigger != fakeslack.TriggerTimeOff || p.ch != "" {
		t.Fatalf("row click opened %T kind %v", e.a.secondary.view, e.a.secondary.kind)
	}
	e.a.CloseSecondary()
	wfMenuItem(t, feat, "View details").Do()
	if p, ok := e.a.secondary.view.(*wfDetailsPane); !ok || p.trigger != tab.Featured[0].Trigger.ID {
		t.Errorf("View details opened %T", e.a.secondary.view)
	}

	// Feature workflow: workflows.featured.add with the trigger
	wfMenuItem(t, bm, "Feature workflow").Do()
	e.until("workflows.featured.add", 10*time.Second, func() bool { return len(fs.FeaturedAdds()) == 1 })
	if a := fs.FeaturedAdds()[0]; a.Get("channel_id") != ch || !strings.Contains(a.Get("trigger_ids"), fakeslack.TriggerTimeOff) {
		t.Errorf("featured.add %v", a)
	}

	// Remove from bookmarks: the confirm, then bookmarks.remove
	n := len(e.a.overlays)
	wfMenuItem(t, bm, "Remove from bookmarks").Do()
	e.settle(100 * time.Millisecond)
	if len(e.a.overlays) <= n {
		t.Fatal("Remove from bookmarks opened no confirm")
	}
	m, ok := e.a.overlays[len(e.a.overlays)-1].(*popups.Modal)
	if !ok || m.Title() != "Remove workflow from bookmarks?" {
		t.Fatalf("confirm %T", e.a.overlays[len(e.a.overlays)-1])
	}
	before := fs.CallCount("bookmarks.remove")
	if !m.Press("Remove") {
		t.Fatal("no Remove button")
	}
	e.until("bookmarks.remove", 10*time.Second, func() bool { return fs.CallCount("bookmarks.remove") > before })

	// Remove from featured: its confirm, then workflows.featured.remove
	wfMenuItem(t, feat, "Remove from featured").Do()
	e.settle(100 * time.Millisecond)
	m, ok = e.a.overlays[len(e.a.overlays)-1].(*popups.Modal)
	if !ok || m.Title() != "Remove workflow from featured?" {
		t.Fatalf("confirm %T", e.a.overlays[len(e.a.overlays)-1])
	}
	if !m.Press("Remove from Featured") {
		t.Fatal("no Remove from Featured button")
	}
	e.until("workflows.featured.remove", 10*time.Second, func() bool { return fs.CallCount("workflows.featured.remove") == 1 })

}

// TestWorkflowsTabRowActionsShot: a hovered row's ActionsMenu (▶ and ⋮)
// and its open ⋮ menu, for the shots.
func TestWorkflowsTabRowActionsShot(t *testing.T) {
	e, _ := workflowsEnv(t)
	ch, st, tab := openGalleryWorkflowsTab(e)
	it := tab.Bookmarked[0]
	key := "b:" + it.Trigger.ID
	ui := st.rowUIFor(key)
	ui.menuOpen = true // the row shows its actions (isFocused)
	e.settle(300 * time.Millisecond)
	// the pointer over the row: its origin tracker learns where the ⋮ is
	// (the bookmarked row sits at y≈458 in the 1440x900 window)
	r0 := ui.moreRect
	e.movePtr(image.Pt(900, 458))
	e.settle(200 * time.Millisecond)
	if r := ui.moreRect; r.Empty() || r == r0 || r.Min.Y < 380 || r.Max.Y > 540 || r.Min.X < 1200 {
		t.Fatalf("the ⋮ at %v (before the pointer came: %v)", r, r0)
	}
	e.wfShot("workflows-tab-row-actions-light")
	e.a.openWorkflowRowMenu(ch, st, key, it, false, ui.moreRect)
	e.settle(300 * time.Millisecond)
	e.wfShot("workflows-tab-row-menu-light")
}

// TestWorkflowMentions: a workflow_mention reads the triggers store (the
// tab's previews, else workflows.triggers.info) and draws Slack's slug
// labels; a click on one opens the workflow details view.
func TestWorkflowMentions(t *testing.T) {
	e, _ := workflowsEnv(t)
	c := e.a.Core()
	el := func(id string) json.RawMessage {
		return json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"workflow_mention","function_trigger_id":"` +
			id + `","workflow_id":"Wf0X","text":"Typed name","url":"` + fakeslack.WorkflowLink(id) + `"}]}]}]`)
	}
	slug := func(id string) (richtext.Span, bool) {
		for _, b := range richtext.FromMessage(c.S, &slack.Message{Blocks: el(id)}).Blocks {
			for _, s := range b.Spans {
				if s.Kind == richtext.Canvas {
					return s, true
				}
			}
		}
		return richtext.Span{}, false
	}
	for _, cs := range []struct{ id, text, icon string }{
		{fakeslack.TriggerDeploy, "Deploy", "play"},
		{"Ft0FAKEGONE1", "Deleted workflow", "trash"},
	} {
		var sp richtext.Span
		e.until("the "+cs.id+" mention", 10*time.Second, func() bool {
			var ok bool
			sp, ok = slug(cs.id)
			return ok
		})
		if sp.Text != cs.text || sp.Icon != cs.icon || sp.ID != cs.id {
			t.Errorf("%s: %q %q %q", cs.id, sp.Text, sp.Icon, sp.ID)
		}
	}
	m := &core.Message{Channel: fakeslack.WorkflowsChannelID, TS: "1.0", Blocks: el(fakeslack.TriggerDeploy)}
	if e.a.OpenWorkflowMention(m, "https://example.com/elsewhere") {
		t.Error("a link that isn't a mention opened the details view")
	}
	if !e.a.OpenWorkflowMention(m, fakeslack.WorkflowLink(fakeslack.TriggerDeploy)) {
		t.Fatal("the mention's click wasn't taken")
	}
	if p, ok := e.a.secondary.view.(*wfDetailsPane); !ok || p.trigger != fakeslack.TriggerDeploy || p.ch != fakeslack.WorkflowsChannelID {
		t.Errorf("mention click opened %T", e.a.secondary.view)
	}
}

// TestWorkflowTripToasts: Slack's trip toasts (Izws) in its Toast: 2 s
// success, the rocket slow toast, the error toasts; none while the
// workflow execution window is on.
func TestWorkflowTripToasts(t *testing.T) {
	e, _ := workflowsEnv(t)
	c := e.a.Core()
	c.SetExperimentGroup("workflow_execution_window", "")
	e.a.tripToast(fakeslack.TriggerDeploy, core.TripResult{}, nil)
	if ts := e.a.toast; !ts.rich || ts.text != "You successfully started a workflow" || time.Until(ts.until) > 2*time.Second {
		t.Errorf("success toast %+v", ts)
	}
	e.a.tripToast("Ft0NOTKNOWN1", core.TripResult{Slow: true}, nil)
	if ts := e.a.toast; ts.icon != "rocket" || !strings.Contains(ts.text, "This workflow might take a little longer to run.") {
		t.Errorf("slow toast %+v", ts)
	}
	c.SetExperimentGroup("enable_wfb_ai_answer_step", "on")
	e.a.tripToast("Ft0NOTKNOWN1", core.TripResult{Slow: true, AISteps: true}, nil)
	if ts := e.a.toast; ts.icon != "bolt" || ts.text != "You started a workflow.\nThis workflow has an AI step and may take longer to run." {
		t.Errorf("AI step toast %q %+v", ts.text, ts)
	}
	e.settle(100 * time.Millisecond)
	e.wfShot("workflow-toast-ai-step-light")
	e.a.tripToast("", core.TripResult{}, &core.TripError{Code: "trigger_not_found"})
	if ts := e.a.toast; ts.text != "This workflow no longer exists." || !ts.rich {
		t.Errorf("error toast %+v", ts)
	}
	e.a.tripToast("", core.TripResult{}, &core.TripError{Code: "free_team_not_allowed"})
	if ts := e.a.toast; ts.text != "Workflow currently unavailable.\nContact a workspace owner for help." {
		t.Errorf("plan error toast %q", ts.text)
	}
	e.a.tripToast("", core.TripResult{}, &core.TripError{Code: "access_denied", SubType: "away_user_home_only_workflow"})
	if ts := e.a.toast; ts.linkHref != "https://slack.com/help/articles/14844871922195" {
		t.Errorf("restrictions toast %+v", ts)
	}
	c.SetExperimentGroup("workflow_execution_window", "on")
	e.a.toast = toastState{}
	e.a.tripToast(fakeslack.TriggerDeploy, core.TripResult{}, nil)
	if e.a.toast.text != "" {
		t.Errorf("a toast with the execution window on: %+v", e.a.toast)
	}
}

// TestWorkflowRunToasts: with workflow_execution_window on, a run's
// realtime events show Slack's WorkflowStatusToast and then its
// WorkflowOutcomeToast; a dismissed run stays quiet.
func TestWorkflowRunToasts(t *testing.T) {
	e, _ := workflowsEnv(t)
	c := e.a.Core()
	c.SetExperimentGroup("workflow_execution_window", "on")
	step := func(exec string, n int, cb, title string, branching bool) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"type": "workflow_execution_step_started", "workflow_id": "Wf0FAKE01", "trigger_execution_id": exec,
			"workflow_execution_id": "Wx" + exec, "step_number": n, "step_title": title, "total_steps": 3, "trigger_id": fakeslack.TriggerDeploy,
			"has_conditional_branching": branching, "callback_id": cb})
		return b
	}
	outcome := func(exec, status string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"type": "workflow_execution_outcome_changed", "workflow_id": "Wf0FAKE01", "trigger_execution_id": exec, "workflow_instance_status": status})
		return b
	}
	st := e.a.wfRuns()
	// an outcome for a run whose steps never came shows nothing
	e.a.workflowRunEvent("workflow_execution_outcome_changed", outcome("Te0", "SUCCESS"))
	if st.cur != nil {
		t.Fatalf("outcome without a step: %+v", st.cur)
	}
	e.a.workflowRunEvent("workflow_execution_step_started", step("Te1", 0, "send_message", "Post the note", false))
	if cur := st.cur; cur == nil || cur.count != "1/3" || cur.text != "Sending a message to a channel" || cur.until.IsZero() {
		t.Fatalf("status toast %+v", st.cur)
	}
	e.settle(150 * time.Millisecond)
	e.wfShot("workflow-run-status-light")
	e.a.workflowRunEvent("workflow_execution_step_started", step("Te1", 1, "custom_fn", "Ask the bot", true))
	if cur := st.cur; cur.count != "" || cur.text != "In progress: Ask the bot" {
		t.Errorf("branching custom step %+v", cur)
	}
	e.a.workflowRunEvent("workflow_execution_step_started", step("Te1", 2, "ai_answer", "Answer", false))
	if cur := st.cur; cur.text != "Generating AI response" || !cur.until.IsZero() {
		t.Errorf("AI step %+v (it doesn't fade)", cur)
	}
	e.a.workflowRunEvent("workflow_execution_outcome_changed", outcome("Te1", "ERROR"))
	if cur := st.cur; cur.outcome != "ERROR" || cur.text != "Workflow failed" {
		t.Errorf("outcome toast %+v", cur)
	}
	e.settle(150 * time.Millisecond)
	e.wfShot("workflow-run-outcome-light")
	// dismissed: the run's later events show nothing
	st.dismissed["Te2"] = true
	st.cur = nil
	e.a.workflowRunEvent("workflow_execution_step_started", step("Te2", 0, "send_message", "Post", false))
	if st.cur != nil {
		t.Errorf("a dismissed run showed %+v", st.cur)
	}
	// the window off: nothing
	c.SetExperimentGroup("workflow_execution_window", "")
	e.a.workflowRunEvent("workflow_execution_step_started", step("Te3", 0, "send_message", "Post", false))
	if st.cur != nil {
		t.Errorf("the window off showed %+v", st.cur)
	}
}
