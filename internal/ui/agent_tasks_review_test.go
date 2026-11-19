package ui

// Review fixes on the Tasks tab, through the real tab against
// internal/fakeslack (Atlas). Synthetic only.

import (
	"net/url"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
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
