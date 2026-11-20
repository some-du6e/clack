package core

import (
	"encoding/json"
	"testing"
)

// Off Enterprise Grid the team's ai_apps_settings pref decides whether an
// app's Tasks tab is turned off (o1P2 sQ): an entry for the app with
// is_agent_tasks_enabled false turns it off; true, no flag, another app's
// entry or no pref leave it on; the pref sent as a JSON string reads the
// same. The two blocked apps are off whatever the pref says.
func TestAgentTasksDisabledTeamPref(t *testing.T) {
	a, _ := testApp(t)
	if a.C.Creds.EnterpriseID != "" {
		t.Fatal("testApp is on Enterprise Grid; this is the workspace path")
	}
	set := func(v string) {
		a.SetTeamPrefs(map[string]json.RawMessage{"ai_apps_settings": json.RawMessage(v)})
	}
	str, _ := json.Marshal(`{"apps":[{"app_id":"A1","is_agent_tasks_enabled":false}]}`)
	for _, c := range []struct {
		name, pref, app string
		off             bool
	}{
		{"the app turned off", `{"apps":[{"app_id":"A1","is_agent_tasks_enabled":false}]}`, "A1", true},
		{"another app turned off", `{"apps":[{"app_id":"A1","is_agent_tasks_enabled":false}]}`, "A2", false},
		{"the app turned on", `{"apps":[{"app_id":"A1","is_agent_tasks_enabled":true}]}`, "A1", false},
		{"the app's entry without the flag", `{"apps":[{"app_id":"A1"}]}`, "A1", false},
		{"the pref as a JSON string", string(str), "A1", true},
		{"no pref", `null`, "A1", false},
	} {
		set(c.pref)
		if got := a.agentTasksDisabled(c.app); got != c.off {
			t.Errorf("%s: disabled %v, want %v", c.name, got, c.off)
		}
	}
	set(`{"apps":[]}`)
	for id := range agentTasksBlockedApps {
		if !a.agentTasksDisabled(id) {
			t.Errorf("blocked app %s reads enabled", id)
		}
	}
	if a.agentTasksDisabled("") {
		t.Error("no app id reads disabled")
	}
}
