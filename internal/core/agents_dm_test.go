package core

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// Navigation > "Show Agents in direct messages": with agents_in_dm_sidebar
// an agent's DM is filed under Direct messages (isAgentInDmSidebar);
// hide_agents_in_dm_list (behind agents_in_dm_sidebar_pref) puts it back
// under Apps. A plain app stays an app.
func TestAgentInDMSidebar(t *testing.T) {
	a := identityApp(t)
	for _, e := range []string{"show_as_agent", "agent_badge", "agents_in_dm_sidebar", "agents_in_dm_sidebar_pref"} {
		a.SetExperimentGroup(e, "on")
	}
	a.S.PutConv(slack.Conversation{ID: "DAGENT", IsIM: true, User: "UAGENT"})
	a.S.PutConv(slack.Conversation{ID: "DAPP", IsIM: true, User: "UAPPU"})
	putCard(a, "B:BAGENT", AppCard{AppID: "AAGENT", ShownAsAgent: true})
	if !a.isAppDM("DAGENT") {
		t.Fatal("the agent's DM isn't an app DM")
	}
	if !a.AgentInDMSidebar("DAGENT") {
		t.Fatal("agent DM not under Direct messages")
	}
	if a.AgentInDMSidebar("DAPP") {
		t.Fatal("a plain app's DM moved under Direct messages")
	}
	a.setRawPref("hide_agents_in_dm_list", true)
	if a.AgentInDMSidebar("DAGENT") || !a.HideAgentsInDMs() {
		t.Fatal("hide_agents_in_dm_list didn't take the agent out of Direct messages")
	}
	a.SetExperimentGroup("agents_in_dm_sidebar_pref", "off")
	if !a.AgentInDMSidebar("DAGENT") {
		t.Fatal("the pref applied without agents_in_dm_sidebar_pref")
	}
	a.SetExperimentGroup("agents_in_dm_sidebar", "off")
	if a.AgentInDMSidebar("DAGENT") {
		t.Fatal("agent DM under Direct messages without agents_in_dm_sidebar")
	}
}
