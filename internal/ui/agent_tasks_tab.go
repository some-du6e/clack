package ui

// The agent DM's Tasks tab (Slack's App3pAgentTasks tab,
// AgentTasksTabView; docs/APP-TASKS.md): blockkit.AppDM puts it in the
// DM's tab bar when core.AgentTasksTab says Slack shows it (client-boot
// L1V2's rule), and the open tab's body is views.AgentTasks, made per DM
// on first use. Run task now ends in the DM's Messages tab, started at
// the prompt it sent and highlighted (runPromptFor3pAgent's navigation:
// the Channel tab with highlightTs).

import (
	"sync"

	"github.com/some-du6e/clack/internal/ui/views"
)

var agentTasksTabs sync.Map // *App -> map[string]*views.AgentTasks (UI goroutine only)

// agentTasksTab is the agent DM ch's Tasks tab, made on first use.
func (a *App) agentTasksTab(ch string) *views.AgentTasks {
	m, _ := agentTasksTabs.Load(a)
	tabs, _ := m.(map[string]*views.AgentTasks)
	if tabs == nil {
		tabs = map[string]*views.AgentTasks{}
		agentTasksTabs.Store(a, tabs)
	}
	v := tabs[ch]
	if v == nil {
		v = views.NewAgentTasks(a, ch)
		tabs[ch] = v
	}
	return v
}

// OpenAppDMMessages is Run task now's navigation: the app DM's Messages
// tab, the conversation started at ts with ts highlighted.
func (a *App) OpenAppDMMessages(ch, ts string) {
	if dm := a.appDM(ch); dm != nil {
		dm.SetTab("messages")
	}
	a.jumpToMessage(ch, ts, "", false)
}
