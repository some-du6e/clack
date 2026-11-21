package core

// The agent DM's Tasks tab (agent_tasks.go): L1V2's rule against the fake
// Slack's Atlas fixture gate by gate, the list's request shape, order and
// rows, each action's request, the live events and the schedule lines
// (docs/APP-TASKS.md).

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func agentTasksApp(t *testing.T, setup func(st *fakeslack.AgentTasksState)) (*App, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.AgentTasks = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	if setup != nil {
		fs.AgentTasks().Set(setup)
	}
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a, fs
}

// agentTabNow reads the DM's app home and profile, the features and the
// team prefs the rule reads, then the rule.
func agentTabNow(t *testing.T, a *App) (bool, AgentTasksTabInputs) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	home, _, err := a.Interactive().AppHome(ctx, fakeslack.AgentTasksDMID)
	if err != nil {
		t.Fatalf("app home: %v", err)
	}
	if _, err := a.AppCardOf(ctx, fakeslack.AgentTasksUserID, ""); err != nil {
		t.Fatalf("app profile: %v", err)
	}
	a.loadFeatures(ctx)
	if err := a.LoadTeamPrefs(ctx); err != nil {
		t.Fatalf("team prefs: %v", err)
	}
	if err := a.LoadEnterprisePrefs(ctx); err != nil {
		t.Fatalf("enterprise prefs: %v", err)
	}
	shown, pending := a.AgentTasksTab(fakeslack.AgentTasksDMID, home)
	if pending {
		t.Fatal("the app profile still reads as pending after AppCardOf")
	}
	in, _ := a.AgentTasksTabInputsOf(fakeslack.AgentTasksDMID, home)
	return shown, in
}

// TestAgentTasksGridLockedPref: on Enterprise Grid, Slack reads
// ai_apps_settings from the org's locked prefs only (o1P2: TI4y
// getLockedOrgPref under THpq isEnterprise), so a workspace's own entry
// turning the app's tasks off doesn't hide the tab; the org's does (the
// rule table's "admin turned it off").
func TestAgentTasksGridLockedPref(t *testing.T) {
	a, _ := agentTasksApp(t, func(st *fakeslack.AgentTasksState) { st.TeamAdminOff = true })
	a.SetExperimentGroup(ExpAgentTasksTab, "on")
	if shown, in := agentTabNow(t, a); !shown {
		t.Errorf("a workspace's ai_apps_settings entry hid the tab on Grid (%+v): want the org's locked pref to rule", in)
	}
}

// TestAgentTasksTabRule: every one of L1V2's inputs is needed.
func TestAgentTasksTabRule(t *testing.T) {
	all := AgentTasksTabInputs{AgentApp: true, ShownAsAgent: true, Experiment: true, AllowedByPlan: true, EnabledByDeveloper: true}
	if !AgentTasksTabRule(all) {
		t.Fatal("all inputs on: no Tasks tab")
	}
	for name, off := range map[string]func(*AgentTasksTabInputs){
		"not an agent app":       func(in *AgentTasksTabInputs) { in.AgentApp = false },
		"not shown as an agent":  func(in *AgentTasksTabInputs) { in.ShownAsAgent = false },
		"experiment off":         func(in *AgentTasksTabInputs) { in.Experiment = false },
		"turned off by an admin": func(in *AgentTasksTabInputs) { in.Disabled = true },
		"not on the plan":        func(in *AgentTasksTabInputs) { in.AllowedByPlan = false },
		"developer's switch off": func(in *AgentTasksTabInputs) { in.EnabledByDeveloper = false },
	} {
		in := all
		off(&in)
		if AgentTasksTabRule(in) {
			t.Errorf("%s: the Tasks tab shows", name)
		}
	}
}

// TestAgentTasksTabGates: the rule's inputs as Atlas's app home, profile,
// features and team prefs give them; messaging off turns the tab off
// (the settings page's coupling, which the app home reports).
func TestAgentTasksTabGates(t *testing.T) {
	a, _ := agentTasksApp(t, nil)
	if shown, in := agentTabNow(t, a); shown {
		t.Fatalf("wfb_3p_agent_tasks_tab unassigned: the tab shows (%+v)", in)
	}
	a.SetExperimentGroup(ExpAgentTasksTab, "on")
	if shown, in := agentTabNow(t, a); !shown {
		t.Fatalf("experiment on: no tab (%+v)", in)
	}
	a.SetExperimentGroup(ExpAgentTasksTab, "control")
	if shown, _ := agentTabNow(t, a); shown {
		t.Error("experiment control: the tab shows")
	}

	for name, setup := range map[string]func(st *fakeslack.AgentTasksState){
		"developer's switch off": func(st *fakeslack.AgentTasksState) { st.TasksTab = false },
		"messaging off":          func(st *fakeslack.AgentTasksState) { st.MessagesTab = false },
		"not shown as an agent":  func(st *fakeslack.AgentTasksState) { st.ShownAsAgent = false },
		"plan without the write": func(st *fakeslack.AgentTasksState) { st.Plan = false },
		"admin turned it off":    func(st *fakeslack.AgentTasksState) { st.AdminOff = true },
	} {
		t.Run(strings.ReplaceAll(name, " ", "_"), func(t *testing.T) {
			a, _ := agentTasksApp(t, setup)
			a.SetExperimentGroup(ExpAgentTasksTab, "on")
			if shown, in := agentTabNow(t, a); shown {
				t.Errorf("%s: the tab shows (%+v)", name, in)
			}
		})
	}
}

// TestAgentTasksList: functions.workflows.list and workflows.triggers.list
// as fetchWorkflowsForUser sends them, the rows merged and sorted as
// useAgentTasksExplorer has them (the empty draft left out).
func TestAgentTasksList(t *testing.T) {
	a, fs := agentTasksApp(t, nil)
	ctx := context.Background()
	page, err := a.AgentTasks(ctx, fakeslack.AgentTasksAppID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var list, trig *fakeslack.AgentTaskCall
	for _, c := range fs.AgentTasks().AgentTaskCalls() {
		c := c
		switch c.Method {
		case "functions.workflows.list":
			list = &c
		case "workflows.triggers.list":
			trig = &c
		}
	}
	if list == nil || trig == nil {
		t.Fatalf("calls: list %v, triggers %v", list, trig)
	}
	want := map[string]string{
		"limit":                 "50",
		"filter_options":        `{"name":"","creation_source_type":"AGENT","creation_source_id":"` + fakeslack.AgentTasksAppID + `"}`,
		"sort_options":          `{"column":"date_updated","direction":"desc"}`,
		"workflow_builder_only": "true",
		"reason":                "automations_gallery",
	}
	for k, v := range want {
		if got := list.Form.Get(k); got != v {
			t.Errorf("functions.workflows.list %s = %q, want %q", k, got, v)
		}
	}
	if list.Form.Has("cursor") {
		t.Errorf("the first page sent cursor %q", list.Form.Get("cursor"))
	}
	if ids := strings.Split(trig.Form.Get("app_ids"), ","); len(ids) != 7 || trig.Form.Get("reason") != "automations_gallery" {
		t.Errorf("workflows.triggers.list app_ids %q reason %q", trig.Form.Get("app_ids"), trig.Form.Get("reason"))
	}
	SortAgentTasks(page.Tasks)
	var got []string
	for _, tk := range page.Tasks {
		got = append(got, tk.Title)
	}
	order := []string{"Inbox triage", "Morning briefing", "Quarterly review prep", "Standup notes", "Weekly support digest", "Release checklist"}
	if strings.Join(got, "|") != strings.Join(order, "|") {
		t.Errorf("rows %q\nwant %q", got, order)
	}
	by := map[string]AgentTask{}
	for _, tk := range page.Tasks {
		by[tk.WorkflowID] = tk
	}
	ch := by[fakeslack.TaskChecklist]
	if !ch.Paused() || ch.TriggerID != fakeslack.TaskTriggerID(fakeslack.TaskChecklist) || ch.TriggerType != "scheduled" {
		t.Errorf("Release checklist: paused %v trigger %q type %q", ch.Paused(), ch.TriggerID, ch.TriggerType)
	}
	if ch.CanRunNow() {
		t.Error("a paused task offers Run task now")
	}
	if br := by[fakeslack.TaskBriefing]; !br.CanRunNow() || !br.ThirdParty || br.AgentAppID != fakeslack.AgentTasksAppID ||
		!br.CanDelete(fakeslack.SelfID) || br.Prompt == "" {
		t.Errorf("Morning briefing: run %v 3p %v agent %q delete %v prompt %q", br.CanRunNow(), br.ThirdParty, br.AgentAppID, br.CanDelete(fakeslack.SelfID), br.Prompt)
	}
	if tr := by[fakeslack.TaskTriage]; tr.CanRunNow() {
		t.Error("a prompt that reads {{inputs.channel_id}} offers Run task now")
	}
	if _, ok := by[fakeslack.TaskDraft]; ok {
		t.Error("the is_empty draft is listed")
	}

	// the search: name in filter_options
	if _, err := a.AgentTasks(ctx, fakeslack.AgentTasksAppID, "brief", ""); err != nil {
		t.Fatal(err)
	}
	calls := fs.AgentTasks().AgentTaskCalls()
	var last fakeslack.AgentTaskCall
	for _, c := range calls {
		if c.Method == "functions.workflows.list" {
			last = c
		}
	}
	var fo map[string]string
	_ = json.Unmarshal([]byte(last.Form.Get("filter_options")), &fo)
	if fo["name"] != "brief" {
		t.Errorf("the search's filter_options %q", last.Form.Get("filter_options"))
	}
}

// TestAgentTasksPaging: a next_cursor is the next page's cursor.
func TestAgentTasksPaging(t *testing.T) {
	a, fs := agentTasksApp(t, func(st *fakeslack.AgentTasksState) {
		for i := 0; i < 55; i++ {
			id := "Wf0PAGE" + string(rune('A'+i/26)) + string(rune('A'+i%26))
			st.Tasks = append(st.Tasks, &fakeslack.FakeTask{ID: id, Title: "Page task " + id, Prompt: "Do it.", Published: true,
				DateUpdated: int64(1762000000 - i), StartTime: "2026-11-23T17:00:00Z", Frequency: map[string]any{"type": "daily", "repeats_every": 1}})
		}
	})
	ctx := context.Background()
	p1, err := a.AgentTasks(ctx, fakeslack.AgentTasksAppID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if p1.Cursor == "" || len(p1.Tasks) != 49 { // 50 workflows, the draft left out
		t.Fatalf("page 1: %d tasks, cursor %q", len(p1.Tasks), p1.Cursor)
	}
	p2, err := a.AgentTasks(ctx, fakeslack.AgentTasksAppID, "", p1.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Cursor != "" || len(p2.Tasks) != 12 { // 62 workflows in all
		t.Errorf("page 2: %d tasks, cursor %q", len(p2.Tasks), p2.Cursor)
	}
	var cursor string
	for _, c := range fs.AgentTasks().AgentTaskCalls() {
		if c.Method == "functions.workflows.list" {
			cursor = c.Form.Get("cursor")
		}
	}
	if cursor != p1.Cursor {
		t.Errorf("page 2 sent cursor %q, want %q", cursor, p1.Cursor)
	}
}

// TestAgentTasksActions: Pause task, Restart task, Delete task and Run
// task now send Slack's requests.
func TestAgentTasksActions(t *testing.T) {
	a, fs := agentTasksApp(t, nil)
	ctx := context.Background()
	st := fs.AgentTasks()
	if err := a.SetAgentTaskPublished(ctx, fakeslack.TaskDigest, false); err != nil {
		t.Fatal(err)
	}
	if err := a.SetAgentTaskPublished(ctx, fakeslack.TaskChecklist, true); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteAgentTask(ctx, fakeslack.TaskTriggerID(fakeslack.TaskStandup), fakeslack.TaskStandup); err != nil {
		t.Fatal(err)
	}
	page, err := a.AgentTasks(ctx, fakeslack.AgentTasksAppID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var brief AgentTask
	for _, tk := range page.Tasks {
		if tk.WorkflowID == fakeslack.TaskBriefing {
			brief = tk
		}
	}
	ch, ts, err := a.RunAgentTaskNow(ctx, fakeslack.AgentTasksUserID, brief)
	if err != nil {
		t.Fatal(err)
	}
	if ch != fakeslack.AgentTasksDMID || ts == "" {
		t.Errorf("run now: %q %q", ch, ts)
	}
	type call struct{ m, k, v string }
	var got []call
	for _, c := range st.AgentTaskCalls() {
		switch c.Method {
		case "functions.workflows.publish", "functions.workflows.unpublish", "functions.workflows.delete":
			got = append(got, call{c.Method, c.Form.Get("workflow_id"), c.Form.Get("reason")})
		case "workflows.triggers.delete":
			got = append(got, call{c.Method, c.Form.Get("trigger_id"), c.Form.Get("reason")})
		case "chat.postMessage":
			got = append(got, call{c.Method, c.Form.Get("channel"), c.Form.Get("text")})
		}
	}
	want := []call{
		{"functions.workflows.unpublish", fakeslack.TaskDigest, "slackbot-automation-item-pause"},
		{"functions.workflows.publish", fakeslack.TaskChecklist, "slackbot-automation-item-restart"},
		{"workflows.triggers.delete", fakeslack.TaskTriggerID(fakeslack.TaskStandup), "slackbot-automation-item-delete"},
		{"functions.workflows.delete", fakeslack.TaskStandup, "slackbot-automation-item-delete"},
		{"chat.postMessage", fakeslack.AgentTasksDMID, ""}, // blocks alone: TestAgentTasksRunNowBlocks
	}
	if len(got) != len(want) {
		t.Fatalf("calls %v\nwant %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d: %v, want %v", i, got[i], want[i])
		}
	}
	if fs.CallCount("conversations.open") != 0 {
		t.Error("Run task now opened the DM again (startImByMemberId reuses the open IM)")
	}
	if tk := st.Task(fakeslack.TaskDigest); tk.Published {
		t.Error("the fake still has the digest published")
	}
}

// TestAgentTasksLiveEvents: workflow and hermes_trigger_updated reach the
// tab as agent_task events.
func TestAgentTasksLiveEvents(t *testing.T) {
	a, _ := agentTasksApp(t, nil)
	if _, err := a.AgentTasks(context.Background(), fakeslack.AgentTasksAppID, "", ""); err != nil {
		t.Fatal(err)
	}
	evs, stop := a.Subscribe()
	defer stop()
	inject := func(ev map[string]any) {
		b, _ := json.Marshal(ev)
		typ, _ := ev["type"].(string)
		a.Inject(slack.Event{Type: typ, Raw: b})
	}
	wait := func(want Event) {
		t.Helper()
		dl := time.After(10 * time.Second)
		for {
			select {
			case ev := <-evs:
				if ev.Type == "agent_task" && ev.Name == want.Name && ev.TS == want.TS && ev.ThreadTS == want.ThreadTS {
					return
				}
			case <-dl:
				t.Fatalf("no agent_task event %+v", want)
			}
		}
	}
	inject(fakeslack.WorkflowEvent(fakeslack.TaskDigest, "deleted"))
	wait(Event{Name: "deleted", TS: fakeslack.TaskDigest})
	inject(fakeslack.WorkflowEvent(fakeslack.TaskDigest, "unpublished"))
	wait(Event{Name: "unpublished", TS: fakeslack.TaskDigest})
	trig := fakeslack.TaskTriggerID(fakeslack.TaskBriefing)
	inject(fakeslack.TriggerUpdatedEvent(trig, false, false))
	wait(Event{Name: "trigger_updated", TS: fakeslack.TaskBriefing, ThreadTS: trig})
	inject(fakeslack.TriggerUpdatedEvent(trig, true, false))
	wait(Event{Name: "trigger_deleted", TS: fakeslack.TaskBriefing, ThreadTS: trig})
}

// TestAgentTasksScheduleZones: the zone in a schedule line is Intl's
// en-US short name (ZgZs u.format("z"), dayjs's timezone offsetName): the
// letters CLDR's en-US data has for US zones and UTC/GMT, GMT±H[:MM] for
// everything else.
func TestAgentTasksScheduleZones(t *testing.T) {
	now := time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)
	sched := func(zone, start string, f *TaskFrequency) AgentTask {
		return AgentTask{TriggerType: "scheduled", Description: "fallback",
			Schedule: &TaskSchedule{StartTime: start, Timezone: zone, Frequency: f}}
	}
	daily := &TaskFrequency{Type: "daily", RepeatsEvery: 1}
	for _, c := range []struct {
		name string
		t    AgentTask
		want string
	}{
		{"Paris in winter", sched("Europe/Paris", "2026-11-23T08:00:00Z", daily), "Daily at 9:00 AM GMT+1"},
		{"London in summer", sched("Europe/London", "2026-07-15T13:00:00Z", nil), "Once on July 15th at 2:00 PM GMT+1"},
		{"London in winter", sched("Europe/London", "2026-12-01T14:00:00Z", nil), "Once on December 1st at 2:00 PM GMT"},
		{"Kolkata", sched("Asia/Kolkata", "2026-11-23T04:00:00Z", daily), "Daily at 9:30 AM GMT+5:30"},
		{"Manila", sched("Asia/Manila", "2026-11-23T01:00:00Z", daily), "Daily at 9:00 AM GMT+8"},
		{"Shanghai", sched("Asia/Shanghai", "2026-11-23T01:00:00Z", daily), "Daily at 9:00 AM GMT+8"},
		{"St. John's", sched("America/St_Johns", "2026-11-23T12:30:00Z", daily), "Daily at 9:00 AM GMT-3:30"},
		{"Chicago", sched("America/Chicago", "2026-11-23T15:00:00Z", daily), "Daily at 9:00 AM CST"},
		{"Honolulu", sched("Pacific/Honolulu", "2026-11-23T19:00:00Z", daily), "Daily at 9:00 AM HST"},
		{"UTC", sched("UTC", "2026-11-23T09:00:00Z", daily), "Daily at 9:00 AM UTC"},
	} {
		if got := TaskScheduleText(c.t, TaskTextOpts{Now: now}); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// TestAgentTasksScheduleText: ZgZs's lines for the fixture's schedules and
// the other branches (America/Los_Angeles, en-US).
func TestAgentTasksScheduleText(t *testing.T) {
	now := time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)
	sched := func(start string, f *TaskFrequency) AgentTask {
		return AgentTask{TriggerType: "scheduled", Description: "fallback",
			Schedule: &TaskSchedule{StartTime: start, Timezone: "America/Los_Angeles", Frequency: f}}
	}
	one := 1
	cases := []struct {
		name string
		t    AgentTask
		o    TaskTextOpts
		want string
	}{
		{"weekdays", sched("2026-11-23T17:00:00Z", &TaskFrequency{Type: "weekly", RepeatsEvery: 1,
			OnDays: []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday"}}), TaskTextOpts{},
			"Mondays, Tuesdays, Wednesdays, Thursdays, and Fridays at 9:00 AM PST"},
		{"weekly", sched("2026-11-23T17:00:00Z", &TaskFrequency{Type: "weekly", RepeatsEvery: 1, OnDays: []string{"Monday"}}), TaskTextOpts{},
			"Mondays at 9:00 AM PST"},
		{"two days", sched("2026-11-23T17:00:00Z", &TaskFrequency{Type: "weekly", RepeatsEvery: 1, OnDays: []string{"Monday", "Friday"}}), TaskTextOpts{},
			"Mondays and Fridays at 9:00 AM PST"},
		{"every 2 weeks", sched("2026-11-23T17:00:00Z", &TaskFrequency{Type: "weekly", RepeatsEvery: 2, OnDays: []string{"Monday"}}), TaskTextOpts{},
			"Mondays every 2 weeks at 9:00 AM PST"},
		{"daily", sched("2026-11-23T01:30:00Z", &TaskFrequency{Type: "daily", RepeatsEvery: 1}), TaskTextOpts{},
			"Daily at 5:30 PM PST"},
		{"daily 24h", sched("2026-11-23T01:30:00Z", &TaskFrequency{Type: "daily", RepeatsEvery: 1}), TaskTextOpts{Time24: true},
			"Daily at 17:30 PST"},
		{"daily window", sched("2026-11-23T01:30:00Z", &TaskFrequency{Type: "daily", RepeatsEvery: 1}), TaskTextOpts{Window: true},
			"Daily between 5:30 PM and 5:45 PM PST"},
		{"every 3 days", sched("2026-11-23T17:00:00Z", &TaskFrequency{Type: "daily", RepeatsEvery: 3}), TaskTextOpts{},
			"Every 3 days at 9:00 AM PST"},
		{"first Monday", sched("2026-12-07T18:00:00Z", &TaskFrequency{Type: "monthly", RepeatsEvery: 1, OnDays: []string{"Monday"}, OnWeekNum: &one}), TaskTextOpts{},
			"First Monday of every month at 10:00 AM PST"},
		{"last day", sched("2026-11-30T17:00:00Z", &TaskFrequency{Type: "monthly", RepeatsEvery: 1, OnLastDayOfMonth: true}), TaskTextOpts{},
			"Last day of every month at 9:00 AM PST"},
		{"monthly", sched("2026-11-23T17:00:00Z", &TaskFrequency{Type: "monthly", RepeatsEvery: 1}), TaskTextOpts{},
			"Monthly at 9:00 AM PST"},
		{"yearly", sched("2026-11-23T17:00:00Z", &TaskFrequency{Type: "yearly", RepeatsEvery: 1}), TaskTextOpts{},
			"Yearly on November 23rd at 9:00 AM PST"},
		{"once this year", sched("2026-12-01T22:00:00Z", nil), TaskTextOpts{},
			"Once on December 1st at 2:00 PM PST"},
		{"once next year", sched("2027-07-01T21:00:00Z", nil), TaskTextOpts{},
			"Once on July 1st, 2027 at 2:00 PM PDT"},
		{"morning bucket", AgentTask{TriggerType: "scheduled", Schedule: &TaskSchedule{StartTime: "2026-11-19T17:00:00Z",
			Timezone: "America/Los_Angeles", TimeBucket: "morning", Frequency: &TaskFrequency{Type: "daily", RepeatsEvery: 1}}}, TaskTextOpts{},
			"Starts tomorrow in the morning and repeats daily"},
		{"not scheduled", AgentTask{TriggerType: "event", Description: "When a message is posted"}, TaskTextOpts{},
			"When a message is posted"},
	}
	for _, c := range cases {
		c.o.Now = now
		if got := TaskScheduleText(c.t, c.o); got != c.want {
			t.Errorf("%s: %q\n want %q", c.name, got, c.want)
		}
	}
}

// TestAgentTasksFeatureAccessLive: feature_access_policies_updated
// replaces the policies at once (boot-render featureAccessUpdate), so the
// Tasks tab's agent_tasks__write follows a plan change live.
func TestAgentTasksFeatureAccessLive(t *testing.T) {
	a, _ := agentTasksApp(t, nil)
	a.SetExperimentGroup(ExpAgentTasksTab, "on")
	if shown, in := agentTabNow(t, a); !shown {
		t.Fatalf("no tab before the change (%+v)", in)
	}
	if !a.FeatureAccess("user_groups__manage") {
		t.Fatal("user_groups__manage off before the event; the fake's list has it on")
	}
	f := a.features()
	f.mu.Lock()
	readAt := f.at
	f.mu.Unlock()
	evs, stop := a.Subscribe()
	defer stop()
	b, _ := json.Marshal(map[string]any{"type": "feature_access_policies_updated",
		"feature_access_policies": map[string]any{"agent_tasks__write": map[string]any{"is_enabled": false}}})
	a.Inject(slack.Event{Type: "feature_access_policies_updated", Raw: b})
	dl := time.After(10 * time.Second)
	for got := false; !got; {
		select {
		case ev := <-evs:
			got = ev.Type == "features"
		case <-dl:
			t.Fatal("no features event after feature_access_policies_updated")
		}
	}
	if a.FeatureAccess("agent_tasks__write") {
		t.Error("agent_tasks__write still on after the event turned it off")
	}
	// updateFeatureAccessStore merges: the policies the event leaves out
	// keep their values, and the hourly read keeps its time
	if !a.FeatureAccess("user_groups__manage") {
		t.Error("user_groups__manage went off with an event that only named agent_tasks__write, want it kept (Slack merges)")
	}
	f.mu.Lock()
	at := f.at
	f.mu.Unlock()
	if !at.Equal(readAt) {
		t.Errorf("the event moved the policies' read time from %v to %v, want the hourly read left on its time", readAt, at)
	}
}

// TestAgentTasksMonthlyNthDayShape: TGBB matches the monthly option with
// a deep isEqual against {type, onDays, onWeekNum}, so a trigger that
// also sends repeats_every 1 is "custom" and its time-bucket line says so
// (znID), not "repeats monthly on the first Monday".
func TestAgentTasksMonthlyNthDayShape(t *testing.T) {
	now := time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)
	line := func(freq string) string {
		raw := `{"id":"Ft1","type":"scheduled","workflow":{"id":"Wf1","app_id":"A1","app":{"id":"A1","is_workflow_app":true}},` +
			`"schedule":{"start_time":"2026-12-07T18:00:00Z","timezone":"America/Los_Angeles","time_bucket":"morning","frequency":` + freq + `}}`
		var tr rawTaskTrigger
		if err := json.Unmarshal([]byte(raw), &tr); err != nil {
			t.Fatal(err)
		}
		return TaskScheduleText(agentTaskOf(rawTaskWorkflow{ID: "Wf1"}, &tr), TaskTextOpts{TimeBuckets: true, Now: now})
	}
	exact := line(`{"type":"monthly","on_days":["Monday"],"on_week_num":1}`)
	extra := line(`{"type":"monthly","repeats_every":1,"on_days":["Monday"],"on_week_num":1}`)
	const nth = "repeats monthly on the first Monday"
	if !strings.Contains(exact, nth) {
		t.Errorf("exactly {type, on_days, on_week_num}: %q, want %q", exact, nth)
	}
	if strings.Contains(extra, nth) {
		t.Errorf("with repeats_every 1: %q, want Slack's custom repeat (its isEqual fails on the extra key)", extra)
	}
}
