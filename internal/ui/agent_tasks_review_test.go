package ui

// Review fixes on the Tasks tab, through the real tab against
// internal/fakeslack (Atlas). Synthetic only.

import (
	"encoding/json"
	"image"
	"net/url"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/popups"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/blockkit"
)

// TestAgentTasksTabDeleteStalePage: a list read that went out before a
// delete committed answers after it with the task still in it; the task
// stays gone (Slack removes the item on the delete's success, and a list
// answer older than that doesn't bring it back).
func TestAgentTasksTabDeleteStalePage(t *testing.T) {
	e, fs := agentTasksEnv(t, nil)
	dm := e.agentDM(t, "messages,about,tasks")
	v := e.openTasks(t, dm, "list")
	st := fs.AgentTasks()
	var standup core.AgentTask
	for _, tk := range v.Tasks() {
		if tk.WorkflowID == fakeslack.TaskStandup {
			standup = tk
		}
	}
	if standup.WorkflowID == "" {
		t.Fatal("no Standup notes task")
	}
	// hold the next list read; when it goes on, answer as of before the
	// delete (the task still listed)
	gate := make(chan struct{})
	var held, answered atomic.Bool
	fs.SetHook(func(m string, _ url.Values) (any, bool) {
		if m == "functions.workflows.list" && held.CompareAndSwap(false, true) {
			<-gate
			st.Set(func(s *fakeslack.AgentTasksState) {
				for _, tk := range s.Tasks {
					if tk.ID == fakeslack.TaskStandup {
						tk.Deleted = false // read before the delete committed
					}
				}
			})
			answered.Store(true)
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	v.Refresh()
	e.until("the list read held", 10*time.Second, func() bool { return held.Load() })
	v.ConfirmDelete(standup)
	e.frame()
	m, ok := topModal(e.a)
	if !ok || !m.Press("Confirm") {
		t.Fatal("no delete confirm")
	}
	e.until("the delete done", 10*time.Second, func() bool { return v.Removed(fakeslack.TaskStandup) })
	close(gate)
	e.until("the held list answered", 10*time.Second, func() bool { return answered.Load() })
	e.settle(800 * time.Millisecond)
	if slices.Contains(v.TaskIDs(), fakeslack.TaskStandup) {
		t.Errorf("the deleted task came back with a list answer older than its delete")
	}
}

// TestAgentTasksTabPauseHold: a Pause that succeeded holds over a list
// that hasn't caught up yet (vRD3 holdPublishedState: until a copy agrees
// or 30 s pass), instead of flipping the row back to active.
func TestAgentTasksTabPauseHold(t *testing.T) {
	e, fs := agentTasksEnv(t, nil)
	dm := e.agentDM(t, "messages,about,tasks")
	v := e.openTasks(t, dm, "list")
	st := fs.AgentTasks()
	published := func() (bool, bool) {
		for _, tk := range v.Tasks() {
			if tk.WorkflowID == fakeslack.TaskBriefing {
				return tk.Published, true
			}
		}
		return false, false
	}
	var brief core.AgentTask
	for _, tk := range v.Tasks() {
		if tk.WorkflowID == fakeslack.TaskBriefing {
			brief = tk
		}
	}
	v.SetPublished(brief, false)
	e.until("the pause sent", 10*time.Second, func() bool {
		for _, c := range st.AgentTaskCalls() {
			if c.Method == "functions.workflows.unpublish" {
				return true
			}
		}
		return false
	})
	e.settle(500 * time.Millisecond) // its success applied
	// the next list answers as if the pause hadn't landed yet
	var stale atomic.Bool
	fs.SetHook(func(m string, _ url.Values) (any, bool) {
		if m == "functions.workflows.list" && stale.CompareAndSwap(false, true) {
			st.Set(func(s *fakeslack.AgentTasksState) {
				for _, tk := range s.Tasks {
					if tk.ID == fakeslack.TaskBriefing {
						tk.Published = true
					}
				}
			})
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	v.Refresh()
	e.until("the list read again", 10*time.Second, func() bool { return stale.Load() })
	e.settle(800 * time.Millisecond)
	if p, ok := published(); !ok || p {
		t.Errorf("a list that hasn't caught up undid the pause: published %v (listed %v)", p, ok)
	}
}

// TestAgentTasksTabRowButtons: the row's hover actions and its More menu,
// clicked through Gio's router as a person would. Pause task sends the
// unpublish; More actions, Delete task asks first: nothing is deleted
// before Confirm, Cancel deletes nothing, and Confirm sends both deletes.
func TestAgentTasksTabRowButtons(t *testing.T) {
	e, fs := agentTasksEnv(t, nil)
	dm := e.agentDM(t, "messages,about,tasks")
	v := e.openTasks(t, dm, "list")
	calls := func(m string) int {
		n := 0
		for _, c := range fs.AgentTasks().AgentTaskCalls() {
			if c.Method == m {
				n++
			}
		}
		return n
	}
	// a row's button: hover down the tab's right side to the row, then
	// along it to the button
	find := func(id, action string) image.Point {
		t.Helper()
		for y := 40; y < e.size.Y; y += 6 {
			e.movePtr(image.Pt(e.size.X-60, y))
			if !v.RowHoveredForTest(id) {
				continue
			}
			for _, dy := range []int{0, 6, 12, 18, 24, -6, -12} {
				for x := e.size.X - 16; x > e.size.X/2; x -= 3 {
					e.movePtr(image.Pt(x, y+dy))
					if v.ActionHoveredForTest(id, action) {
						return image.Pt(x, y+dy)
					}
				}
			}
		}
		t.Fatalf("no %s button on task %s", action, id)
		return image.Point{}
	}
	e.click(find(fakeslack.TaskBriefing, "pause"))
	e.until("Pause task sent", 10*time.Second, func() bool { return calls("functions.workflows.unpublish") == 1 })

	deleteMenu := func() *popups.Modal {
		t.Helper()
		e.click(find(fakeslack.TaskStandup, "more"))
		e.frame()
		e.frame()
		var menu *popups.Menu
		if n := len(e.a.overlays); n > 0 {
			menu, _ = e.a.overlays[n-1].(*popups.Menu)
		}
		if menu == nil {
			t.Fatal("More actions opened no menu")
		}
		i := slices.Index(menu.Labels(), "Delete task")
		rows := menu.RowRects()
		if i < 0 || i >= len(rows) {
			t.Fatalf("no Delete task in More actions: %q", menu.Labels())
		}
		r := rows[i]
		e.click(image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2))
		e.frame()
		e.frame()
		m, ok := topModal(e.a)
		if !ok || m.Title() != "Permanently delete this task?" {
			t.Fatalf("Delete task opened no confirm (%v)", ok)
		}
		return m
	}
	m := deleteMenu()
	if calls("workflows.triggers.delete")+calls("functions.workflows.delete") != 0 {
		t.Fatal("a delete went out before Confirm")
	}
	if !m.Press("Cancel") {
		t.Fatal("no Cancel button")
	}
	e.settle(800 * time.Millisecond)
	if calls("workflows.triggers.delete")+calls("functions.workflows.delete") != 0 {
		t.Error("a delete went out after Cancel")
	}
	if !slices.Contains(v.TaskIDs(), fakeslack.TaskStandup) {
		t.Error("Cancel took the task out of the list")
	}
	m = deleteMenu()
	if !m.Press("Confirm") {
		t.Fatal("no Confirm button")
	}
	e.until("both deletes", 10*time.Second, func() bool {
		return calls("workflows.triggers.delete") == 1 && calls("functions.workflows.delete") == 1
	})
}

// TestAgentTasksTabCardFailureQuiet: with the agent DM open and its app
// card's fetch failing, the tab bar asks for the card once and then
// waits (Slack keeps the app non-existent until a reconnect or an app
// event), instead of fetching and redrawing on every frame. The client's
// failure backoff is 1 ms here, so a fetch on every frame would reach the
// fake.
func TestAgentTasksTabCardFailureQuiet(t *testing.T) {
	prevBase := slack.BackoffBase
	slack.BackoffBase = time.Millisecond
	t.Cleanup(func() { slack.BackoffBase = prevBase })
	e, fs := agentTasksEnv(t, nil)
	var fetches atomic.Int32
	fs.SetHook(func(m string, _ url.Values) (any, bool) {
		if m == "apps.profile.get" {
			fetches.Add(1)
			return map[string]any{"ok": false, "error": "internal_error"}, true
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	e.a.Navigate("c/" + fakeslack.AgentTasksDMID)
	e.until("the agent DM's tabs", 20*time.Second, func() bool { return blockkit.AppDMFor(e.a, fakeslack.AgentTasksDMID) != nil })
	e.settle(1500 * time.Millisecond)
	var asked atomic.Int32
	prev := invalidateHook
	invalidateHook = func(*App) { asked.Add(1) }
	t.Cleanup(func() { invalidateHook = prev })
	before := fetches.Load()
	e.settle(2 * time.Second)
	if n := asked.Load(); n > 3 {
		t.Errorf("the open DM asked for %d frames in 2 s while its app card failed (and %d fetches): want it quiet", n, fetches.Load()-before)
	}
	if n := fetches.Load() - before; n > 1 {
		t.Errorf("the app card was fetched %d more times in 2 s: want the failure remembered", n)
	}
}

// TestAgentTasksTabAfterAppEvent: a card fetch that failed as the DM
// opened leaves the app non-existent and the Tasks tab hidden until
// something marks the app stale; apps_installed for Atlas does
// (sharedUpsertAppFromImsg), and Tasks comes back at once, without a
// reconnect or a timer.
func TestAgentTasksTabAfterAppEvent(t *testing.T) {
	e, fs := agentTasksEnv(t, nil)
	var fetches atomic.Int32
	refuse := atomic.Bool{}
	refuse.Store(true)
	fs.SetHook(func(m string, _ url.Values) (any, bool) {
		if m == "apps.profile.get" {
			fetches.Add(1)
			if refuse.Load() {
				return map[string]any{"ok": false, "error": "internal_error"}, true
			}
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	e.agentDM(t, "messages,about")
	e.until("the card's refused fetch", 10*time.Second, func() bool { return fetches.Load() >= 1 })
	e.settle(500 * time.Millisecond)
	dm := blockkit.AppDMFor(e.a, fakeslack.AgentTasksDMID)
	if dm == nil {
		t.Fatal("no agent DM")
	}
	if slices.Contains(dm.Tabs(), "tasks") {
		t.Fatalf("with the card refused the tabs are %v, want no Tasks", dm.Tabs())
	}
	before := fetches.Load()
	refuse.Store(false)
	b, _ := json.Marshal(map[string]any{"type": "apps_installed", "app": map[string]any{"id": fakeslack.AgentTasksAppID}})
	e.a.data.core.Inject(slack.Event{Type: "apps_installed", Raw: b})
	e.until("Tasks after apps_installed", 5*time.Second, func() bool {
		dm := blockkit.AppDMFor(e.a, fakeslack.AgentTasksDMID)
		return dm != nil && slices.Contains(dm.Tabs(), "tasks")
	})
	if n := fetches.Load() - before; n != 1 {
		t.Errorf("apps_installed brought %d card fetches, want 1", n)
	}
}
