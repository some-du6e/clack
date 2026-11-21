package ui

// The agent DM's Tasks tab end to end on the fake Slack's Atlas fixture
// (synthetic only; docs/APP-TASKS.md): the tab shows exactly when
// L1V2's rule holds, the list draws each task state, every action sends
// Slack's request and changes the row, a live event changes the list on
// the next frame, and the empty and error states. AGENT_TASKS_SHOTS_DIR
// saves light and dark screenshots (the bk-agent-shots workflow).

import (
	"encoding/json"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/views"
)

const agentTasksShotsEnv = "AGENT_TASKS_SHOTS_DIR"

func agentTasksEnv(t *testing.T, setup func(st *fakeslack.AgentTasksState)) (*perfEnv, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.AgentTasks = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	if setup != nil {
		fs.AgentTasks().Set(setup)
	}
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv(agentTasksShotsEnv) != "" && e.gpu == nil {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	e.a.data.core.SetExperimentGroup(core.ExpAgentTasksTab, "on")
	return e, fs
}

// agentDM opens the DM with Atlas and waits for its tab bar.
func (e *perfEnv) agentDM(t *testing.T, want string) *blockkit.AppDM {
	t.Helper()
	e.a.Navigate("c/" + fakeslack.AgentTasksDMID)
	var dm *blockkit.AppDM
	e.until("the agent DM's tabs "+want, 20*time.Second, func() bool {
		dm = blockkit.AppDMFor(e.a, fakeslack.AgentTasksDMID)
		return dm != nil && strings.Join(dm.Tabs(), ",") == want
	})
	return dm
}

// openTasks opens the Tasks tab and waits for state.
func (e *perfEnv) openTasks(t *testing.T, dm *blockkit.AppDM, state string) *views.AgentTasks {
	t.Helper()
	dm.SetTab("tasks")
	v := e.a.agentTasksTab(fakeslack.AgentTasksDMID)
	e.until("the Tasks tab "+state, 20*time.Second, func() bool { return dm.Tab() == "tasks" && v.State() == state })
	e.settle(300 * time.Millisecond)
	return v
}

// drawnFrame runs one frame and returns the labels it drew.
func (e *perfEnv) drawnFrame() []string {
	var out []string
	prev := theme.DrawHook
	theme.DrawHook = func(s string) { out = append(out, s) }
	e.frame()
	theme.DrawHook = prev
	return out
}

func (e *perfEnv) agentShots(t *testing.T, name string) {
	dir := os.Getenv(agentTasksShotsEnv)
	if dir == "" {
		return
	}
	for _, dark := range []bool{false, true} {
		e.a.setDark(dark)
		e.settle(700 * time.Millisecond)
		e.shotTo(t, dir, name+map[bool]string{false: "-light", true: "-dark"}[dark])
	}
	e.a.setDark(false)
	e.settle(200 * time.Millisecond)
}

func injectRaw(e *perfEnv, ev map[string]any) {
	b, _ := json.Marshal(ev)
	typ, _ := ev["type"].(string)
	e.a.data.core.Inject(slack.Event{Type: typ, Raw: b})
}

// TestAgentTasksTabShows: Atlas's DM shows Messages, About and Tasks
// (L1V2's App3pAgentTasks after About, Atlas having no App Home) while
// wfb_3p_agent_tasks_tab is on, and not without it; an open Tasks tab
// that goes away falls back to Messages.
func TestAgentTasksTabShows(t *testing.T) {
	e, _ := agentTasksEnv(t, nil)
	dm := e.agentDM(t, "messages,about,tasks")
	if drawn := e.drawnFrame(); !slices.Contains(drawn, "Tasks") {
		t.Errorf("the tab bar drew no Tasks: %q", drawn)
	}
	e.openTasks(t, dm, "list")
	e.agentShots(t, "agent-tasks-tab-bar")
	e.a.data.core.SetExperimentGroup(core.ExpAgentTasksTab, "control")
	e.until("the Tasks tab gone", 10*time.Second, func() bool { return strings.Join(dm.Tabs(), ",") == "messages,about" })
	if dm.Tab() != "messages" {
		t.Errorf("the open Tasks tab went away: on %q, want messages", dm.Tab())
	}
	e.a.data.core.SetExperimentGroup(core.ExpAgentTasksTab, "on")
	e.until("the Tasks tab back", 10*time.Second, func() bool { return strings.Join(dm.Tabs(), ",") == "messages,about,tasks" })
}

// TestAgentTasksTabOff: the developer's switch off (or messaging off,
// which the settings page couples it to) leaves Tasks out.
func TestAgentTasksTabOff(t *testing.T) {
	for name, setup := range map[string]func(st *fakeslack.AgentTasksState){
		"tasks_tab_off": func(st *fakeslack.AgentTasksState) { st.TasksTab = false },
		"messages_off":  func(st *fakeslack.AgentTasksState) { st.MessagesTab = false },
		"not_an_agent":  func(st *fakeslack.AgentTasksState) { st.ShownAsAgent = false },
	} {
		t.Run(name, func(t *testing.T) {
			e, _ := agentTasksEnv(t, setup)
			want := "messages,about"
			if name == "messages_off" {
				want = "about" // the Messages tab off too
			}
			dm := e.agentDM(t, want)
			e.settle(1500 * time.Millisecond) // the app profile and features are in by now
			if got := strings.Join(dm.Tabs(), ","); got != want {
				t.Errorf("tabs %q, want %q", got, want)
			}
		})
	}
}

// TestAgentTasksTabRows: each task as AgentTaskItem draws it: published
// first then by title, PAUSED on the paused one, the schedule lines, and
// the hover actions each state has.
func TestAgentTasksTabRows(t *testing.T) {
	e, _ := agentTasksEnv(t, nil)
	v := e.openTasks(t, e.agentDM(t, "messages,about,tasks"), "list")
	want := []string{fakeslack.TaskTriage, fakeslack.TaskBriefing, fakeslack.TaskReview, fakeslack.TaskStandup,
		fakeslack.TaskDigest, fakeslack.TaskChecklist}
	if got := v.TaskIDs(); !slices.Equal(got, want) {
		t.Fatalf("rows %v\nwant %v", got, want)
	}
	drawn := strings.Join(e.drawnFrame(), "\n")
	for _, s := range []string{"Tasks", "Search your tasks", "Morning briefing", "Weekly support digest", "Release checklist",
		"Quarterly review prep", "Standup notes", "Inbox triage", "PAUSED",
		"Mondays, Tuesdays, Wednesdays, Thursdays, and Fridays at 9:00 AM PST", "Mondays at 9:00 AM PST",
		"Daily at 5:30 PM PST", "First Monday of every month at 10:00 AM PST", "Daily at 8:00 AM PST"} {
		if !strings.Contains(drawn, s) {
			t.Errorf("not drawn: %q", s)
		}
	}
	if n := strings.Count(drawn, "PAUSED"); n != 1 {
		t.Errorf("PAUSED drawn %d times, want once", n)
	}
	by := map[string]core.AgentTask{}
	for _, tk := range v.Tasks() {
		by[tk.WorkflowID] = tk
	}
	for id, acts := range map[string][]string{
		fakeslack.TaskBriefing:  {"Run task now", "Pause task", "More actions"},
		fakeslack.TaskChecklist: {"Restart task", "More actions"},               // paused: no Run task now
		fakeslack.TaskTriage:    {"Pause task", "More actions"},                 // its prompt reads {{inputs.channel_id}}
		fakeslack.TaskStandup:   {"Run task now", "Pause task", "More actions"}, // once
	} {
		if got := views.RowActions(by[id]); !slices.Equal(got, acts) {
			t.Errorf("%s: actions %q, want %q", id, got, acts)
		}
	}
	if items := v.MoreItems(by[fakeslack.TaskBriefing]); len(items) != 1 || items[0].Label != "Delete task" || !items[0].Danger || items[0].Icon != "trash" {
		t.Errorf("More actions: %+v", items)
	}
	e.agentShots(t, "agent-tasks-list")
	v.HoverForTest(fakeslack.TaskBriefing)
	e.agentShots(t, "agent-tasks-row-hover")
	v.HoverForTest(fakeslack.TaskChecklist)
	e.agentShots(t, "agent-tasks-row-paused-hover")
	v.HoverForTest("")

	// the search filters the loaded tasks by title
	v.SetQuery("brief")
	e.settle(200 * time.Millisecond)
	if got := v.TaskIDs(); !slices.Equal(got, []string{fakeslack.TaskBriefing}) {
		t.Errorf("search brief: %v", got)
	}
	v.SetQuery("nothing like it")
	e.until("No tasks found", 5*time.Second, func() bool { return v.State() == "no-match" })
	if drawn := strings.Join(e.drawnFrame(), "\n"); !strings.Contains(drawn, "No tasks found") || !strings.Contains(drawn, "Try a different search term.") {
		t.Errorf("no-match drew %q", drawn)
	}
	e.agentShots(t, "agent-tasks-no-match")
	v.SetQuery("")
}

// TestAgentTasksTabActions: Pause task and Restart task (optimistic, the
// row re-sorted), a failed pause put back with Slack's toast, Run task now
// (the prompt in the DM, Messages open on it) and Delete task (the
// confirm, then the trigger's and the workflow's delete).
func TestAgentTasksTabActions(t *testing.T) {
	e, fs := agentTasksEnv(t, nil)
	dm := e.agentDM(t, "messages,about,tasks")
	v := e.openTasks(t, dm, "list")
	task := func(id string) core.AgentTask {
		for _, tk := range v.Tasks() {
			if tk.WorkflowID == id {
				return tk
			}
		}
		t.Fatalf("no task %s", id)
		return core.AgentTask{}
	}
	calls := func(method string) []url.Values {
		var out []url.Values
		for _, c := range fs.AgentTasks().AgentTaskCalls() {
			if c.Method == method {
				out = append(out, c.Form)
			}
		}
		return out
	}

	// Pause task: the row is PAUSED at once and moves among the paused
	v.SetPublished(task(fakeslack.TaskDigest), false)
	if !task(fakeslack.TaskDigest).Paused() {
		t.Error("Pause task didn't show PAUSED before the answer")
	}
	e.until("functions.workflows.unpublish", 10*time.Second, func() bool { return len(calls("functions.workflows.unpublish")) == 1 })
	if f := calls("functions.workflows.unpublish")[0]; f.Get("workflow_id") != fakeslack.TaskDigest || f.Get("reason") != "slackbot-automation-item-pause" {
		t.Errorf("unpublish %v", f)
	}
	e.settle(300 * time.Millisecond)
	ids := v.TaskIDs()
	if ids[len(ids)-1] != fakeslack.TaskDigest && ids[len(ids)-2] != fakeslack.TaskDigest {
		t.Errorf("the paused digest isn't among the paused rows: %v", ids)
	}
	e.agentShots(t, "agent-tasks-paused")

	// Restart task
	v.SetPublished(task(fakeslack.TaskChecklist), true)
	e.until("functions.workflows.publish", 10*time.Second, func() bool { return len(calls("functions.workflows.publish")) == 1 })
	if f := calls("functions.workflows.publish")[0]; f.Get("workflow_id") != fakeslack.TaskChecklist || f.Get("reason") != "slackbot-automation-item-restart" {
		t.Errorf("publish %v", f)
	}
	e.until("the checklist published", 5*time.Second, func() bool { return !task(fakeslack.TaskChecklist).Paused() })

	// a failed pause: back to published, "Something went wrong. Please try again."
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method == "functions.workflows.unpublish" {
			return map[string]any{"ok": false, "error": "internal_error"}, true
		}
		return nil, false
	})
	v.SetPublished(task(fakeslack.TaskBriefing), false)
	e.until("the failed pause put back", 10*time.Second, func() bool { return !task(fakeslack.TaskBriefing).Paused() && e.a.toast.text != "" })
	fs.SetHook(nil)
	if e.a.toast.text != views.AgentTasksFailed {
		t.Errorf("toast %q", e.a.toast.text)
	}

	// Run task now: the prompt into the DM, the Messages tab on it
	brief := task(fakeslack.TaskBriefing)
	v.RunNow(brief)
	e.until("the prompt sent", 10*time.Second, func() bool { return len(calls("chat.postMessage")) == 1 })
	if f := calls("chat.postMessage")[0]; f.Get("channel") != fakeslack.AgentTasksDMID || !strings.Contains(f.Get("blocks"), brief.Prompt) {
		t.Errorf("chat.postMessage %v", f)
	}
	e.until("the Messages tab", 10*time.Second, func() bool { return dm.Tab() == "messages" })
	if r := e.a.router.Current(); r.Channel() != fakeslack.AgentTasksDMID || r.Message() == "" {
		t.Errorf("route %q, want the DM at the sent prompt", r)
	}

	// Delete task: the confirm, Confirm, then both deletes, the row gone
	dm.SetTab("tasks")
	e.settle(200 * time.Millisecond)
	standup := task(fakeslack.TaskStandup)
	v.ConfirmDelete(standup)
	e.frame()
	m, ok := topModal(e.a)
	if !ok || m.Title() != "Permanently delete this task?" {
		t.Fatalf("no delete confirm (%v)", ok)
	}
	e.agentShots(t, "agent-tasks-delete-confirm")
	if !m.Press("Confirm") {
		t.Fatal("no Confirm button")
	}
	e.until("the task gone", 10*time.Second, func() bool { return !slices.Contains(v.TaskIDs(), fakeslack.TaskStandup) })
	td, wd := calls("workflows.triggers.delete"), calls("functions.workflows.delete")
	if len(td) != 1 || td[0].Get("trigger_id") != fakeslack.TaskTriggerID(fakeslack.TaskStandup) || td[0].Get("reason") != "slackbot-automation-item-delete" {
		t.Errorf("workflows.triggers.delete %v", td)
	}
	if len(wd) != 1 || wd[0].Get("workflow_id") != fakeslack.TaskStandup {
		t.Errorf("functions.workflows.delete %v", wd)
	}
}

// TestAgentTasksTabLive: a workflow event naming a deleted task takes the
// row out on the very next frame; an unpublished one reads the list again
// and the row shows PAUSED.
func TestAgentTasksTabLive(t *testing.T) {
	e, fs := agentTasksEnv(t, nil)
	v := e.openTasks(t, e.agentDM(t, "messages,about,tasks"), "list")

	injectRaw(e, fakeslack.WorkflowEvent(fakeslack.TaskDigest, "deleted"))
	dl := time.Now().Add(10 * time.Second)
	for !v.Removed(fakeslack.TaskDigest) { // the event goroutine took it (no frames run)
		if time.Now().After(dl) {
			t.Fatal("the workflow event never arrived")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if drawn := strings.Join(e.drawnFrame(), "\n"); strings.Contains(drawn, "Weekly support digest") {
		t.Error("the deleted task still drawn on the next frame")
	}
	if slices.Contains(v.TaskIDs(), fakeslack.TaskDigest) {
		t.Error("the deleted task is still listed")
	}

	// paused elsewhere: the server's state, then the event
	fs.AgentTasks().Set(func(st *fakeslack.AgentTasksState) {
		for _, tk := range st.Tasks {
			if tk.ID == fakeslack.TaskBriefing {
				tk.Published = false
			}
		}
	})
	injectRaw(e, fakeslack.WorkflowEvent(fakeslack.TaskBriefing, "unpublished"))
	e.until("Morning briefing PAUSED", 10*time.Second, func() bool {
		for _, tk := range v.Tasks() {
			if tk.WorkflowID == fakeslack.TaskBriefing {
				return tk.Paused()
			}
		}
		return false
	})
	if n := strings.Count(strings.Join(e.drawnFrame(), "\n"), "PAUSED"); n != 2 {
		t.Errorf("PAUSED drawn %d times after the event, want 2", n)
	}
}

// TestAgentTasksTabEmpty: no tasks is ThirdPartyAgentTasksEmptyState's
// card, with no search.
func TestAgentTasksTabEmpty(t *testing.T) {
	e, _ := agentTasksEnv(t, func(st *fakeslack.AgentTasksState) {
		st.Tasks = []*fakeslack.FakeTask{{ID: fakeslack.TaskDraft, Title: "Untitled task", Empty: true}}
	})
	e.openTasks(t, e.agentDM(t, "messages,about,tasks"), "empty")
	drawn := strings.Join(e.drawnFrame(), "\n")
	for _, s := range []string{"Tasks", views.AgentTasksEmptyTitle, views.AgentTasksEmptyBody} {
		if !strings.Contains(drawn, s) {
			t.Errorf("empty state: %q not drawn", s)
		}
	}
	if strings.Contains(drawn, "Search your tasks") {
		t.Error("the empty state has the search")
	}
	e.agentShots(t, "agent-tasks-empty")
}

// TestAgentTasksTabError: a failed list is the error row with Retry, and
// Retry reads it again.
func TestAgentTasksTabError(t *testing.T) {
	e, fs := agentTasksEnv(t, func(st *fakeslack.AgentTasksState) { st.ListError = true })
	v := e.openTasks(t, e.agentDM(t, "messages,about,tasks"), "error")
	drawn := strings.Join(e.drawnFrame(), "\n")
	for _, s := range []string{views.AgentTasksErrorTitle, views.AgentTasksErrorBody, "Retry"} {
		if !strings.Contains(drawn, s) {
			t.Errorf("error state: %q not drawn", s)
		}
	}
	e.agentShots(t, "agent-tasks-error")
	fs.AgentTasks().Set(func(st *fakeslack.AgentTasksState) { st.ListError = false })
	v.Retry()
	e.until("the list after Retry", 10*time.Second, func() bool { return v.State() == "list" })
}
