package core

import (
	"context"
	"encoding/json"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// The Activity experiments (activity_agent.go) and what they change in
// core, against internal/fakeslack (Size.Views). Synthetic content only.

// The view's automation filters reach activity.feed as eP2o sends them,
// and the fixture's server splits the feed on the entries' is_bot.
func TestActivityAutomationFilters(t *testing.T) {
	a, fs := actFake(t)
	ctx := context.Background()
	var mu sync.Mutex
	var forms []url.Values
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "activity.feed" {
			mu.Lock()
			forms = append(forms, f)
			mu.Unlock()
		}
		return nil, false
	})
	last := func() url.Values {
		mu.Lock()
		defer mu.Unlock()
		if len(forms) == 0 {
			t.Fatal("no activity.feed")
		}
		return forms[len(forms)-1]
	}
	all, err := a.Activity(ctx, ActivityOpts{View: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if f := last(); f.Get("exclude_automations") != "false" || f.Get("automations_only") != "false" {
		t.Errorf("plain feed: exclude_automations=%q automations_only=%q", f.Get("exclude_automations"), f.Get("automations_only"))
	}
	bots := 0
	for _, it := range all.Items {
		if it.IsBot {
			bots++
			if it.Key != "fa-slackbot" {
				t.Errorf("%s is_bot", it.Key)
			}
		}
	}
	if bots != 1 {
		t.Fatalf("%d bot entries, want the Slackbot DM", bots)
	}
	people, err := a.Activity(ctx, ActivityOpts{View: "all", ExcludeAutomations: true})
	if err != nil {
		t.Fatal(err)
	}
	if f := last(); f.Get("exclude_automations") != "true" || f.Get("automations_only") != "false" {
		t.Errorf("People only: %v", f)
	}
	if len(people.Items) != len(all.Items)-1 {
		t.Errorf("People only: %d items of %d", len(people.Items), len(all.Items))
	}
	for _, it := range people.Items {
		if it.IsBot {
			t.Errorf("People only listed %s", it.Key)
		}
	}
	autos, err := a.Activity(ctx, ActivityOpts{View: "all", AutomationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if f := last(); f.Get("automations_only") != "true" || f.Get("exclude_automations") != "false" {
		t.Errorf("Apps and agents only: %v", f)
	}
	if len(autos.Items) != 1 || autos.Items[0].Key != "fa-slackbot" {
		t.Errorf("Apps and agents only: %+v", autos.Items)
	}
}

// The Activity getters follow 5Ns/ in every group: each is behind
// activity_inbox, the detail panel's needs nested secondary views
// (off under agentic_ia), agent status isn't behind the inbox.
func TestActivityExperimentGroups(t *testing.T) {
	a, _ := actFake(t)
	type getter struct {
		name string
		exp  string
		get  func() bool
	}
	gs := []getter{
		{"xi", "activity_entry_layout_revisions_2", a.ActivityLayoutRevisions},
		{"Bb", "activity_inbox_remove_persistent_detail_panel", a.ActivityRemovePersistentDetailPanel},
		{"zK", "activity_bot_item_type_reads", a.ActivityBotItemTypeReads},
		{"LS", "activity_automations_only", a.ActivityAutomationsOnly},
		{"UK", "exclude_automations_filter", a.ActivityExcludeAutomationsFilter},
	}
	for _, g := range gs {
		if !g.get() {
			t.Errorf("%s off in the account's groups", g.name)
		}
		for _, grp := range []string{"off", "control", ""} {
			a.SetExperimentGroup(g.exp, grp)
			if g.get() {
				t.Errorf("%s on with %s %q", g.name, g.exp, grp)
			}
		}
		a.ClearExperimentGroup(g.exp)
		a.SetExperimentGroup("activity_inbox", "off")
		if g.get() {
			t.Errorf("%s on without activity_inbox", g.name)
		}
		a.ClearExperimentGroup("activity_inbox")
		if !g.get() {
			t.Errorf("%s off after clearing the forced groups", g.name)
		}
	}
	a.SetExperimentGroup("nested_secondary_view", "")
	if a.ActivityRemovePersistentDetailPanel() {
		t.Error("Bb without nested secondary views")
	}
	a.ClearExperimentGroup("nested_secondary_view")
	a.SetExperimentGroup("agentic_ia", "on")
	if a.ActivityRemovePersistentDetailPanel() {
		t.Error("Bb under agentic_ia (kl02 W7 is off)")
	}
	a.ClearExperimentGroup("agentic_ia")
	a.SetExperimentGroup("activity_inbox", "off")
	if !a.AgentStatusInActivity() {
		t.Error("agent status is M/qt ji, not behind the inbox")
	}
	a.ClearExperimentGroup("activity_inbox")
	a.SetExperimentGroup("agent_status_in_activity", "off")
	if a.AgentStatusInActivity() {
		t.Error("agent status on with the experiment off")
	}
}

// useAgentActivityStatus: Processing and Suspended show, for channel
// posts, threads, DMs and app DMs only, from a conversation's
// agent_session or a thread root's; processing lapses at its expiry; off
// with agent_status_in_activity off.
func TestActivityAgentStatusLikeSlack(t *testing.T) {
	a, _ := actFake(t)
	now := time.Unix(1762800000, 0)
	props := func(status string, expire int64) json.RawMessage {
		s := map[string]any{"status": status}
		if expire > 0 {
			s["date_status_processing_expire"] = expire
		}
		b, _ := json.Marshal(map[string]any{"agent_session": s, "code_channel": map[string]any{}})
		return b
	}
	a.S.PutConv(slack.Conversation{ID: "C0AGENT01", Name: "agent-work", IsChannel: true, IsMember: true, Properties: props("processing", 0)})
	a.S.PutConv(slack.Conversation{ID: "C0AGENT02", Name: "agent-wait", IsChannel: true, IsMember: true, Properties: props("suspended", 0)})
	a.S.PutConv(slack.Conversation{ID: "C0AGENT03", Name: "agent-late", IsChannel: true, IsMember: true, Properties: props("processing", now.Unix()-5)})
	a.S.PutConv(slack.Conversation{ID: "C0AGENT04", Name: "agent-soon", IsChannel: true, IsMember: true, Properties: props("processing", now.Unix()+60)})
	a.S.PutConv(slack.Conversation{ID: "C0AGENT05", Name: "agent-done", IsChannel: true, IsMember: true, Properties: props("closed", 0)})
	a.S.PutConv(slack.Conversation{ID: "C0PLAIN01", Name: "plain", IsChannel: true, IsMember: true})
	root := func(status string, expire int64) *Message {
		m := &Message{TS: "1762790000.000100", Channel: "C0PLAIN01"}
		if status != "" {
			m.AgentSession = &slack.MessageAgentSession{Status: status}
			if expire > 0 {
				m.AgentSession.ProcessingExpire = json.Number(itoa64(expire))
			}
		}
		return m
	}
	cases := []struct {
		name string
		it   ActivityItem
		want string
	}{
		{"channel processing", ActivityItem{Type: "channel", Channel: "C0AGENT01"}, AgentProcessing},
		{"dm suspended", ActivityItem{Type: "dm", Channel: "C0AGENT02"}, AgentSuspended},
		{"app dm processing", ActivityItem{Type: "bot_dm_bundle", Channel: "C0AGENT01"}, AgentProcessing},
		{"processing lapsed", ActivityItem{Type: "channel", Channel: "C0AGENT03"}, ""},
		{"processing until later", ActivityItem{Type: "channel", Channel: "C0AGENT04"}, AgentProcessing},
		{"closed shows nothing", ActivityItem{Type: "channel", Channel: "C0AGENT05"}, ""},
		{"a mention never asks", ActivityItem{Type: "at_user", Channel: "C0AGENT01"}, ""},
		{"a reaction never asks", ActivityItem{Type: "message_reaction", Channel: "C0AGENT02"}, ""},
		{"no session", ActivityItem{Type: "channel", Channel: "C0PLAIN01"}, ""},
		{"thread root processing", ActivityItem{Type: "thread_v2", Channel: "C0PLAIN01", ThreadTS: "1762790000.000100", Root: root("processing", 0)}, AgentProcessing},
		{"thread root suspended", ActivityItem{Type: "thread_v2", Channel: "C0PLAIN01", ThreadTS: "1762790000.000100", Root: root("suspended", 0)}, AgentSuspended},
		{"thread root lapsed", ActivityItem{Type: "thread_v2", Channel: "C0PLAIN01", ThreadTS: "1762790000.000100", Root: root("processing", now.Unix()-1)}, ""},
		{"thread root without a session", ActivityItem{Type: "thread_v2", Channel: "C0PLAIN01", ThreadTS: "1762790000.000100", Root: root("", 0)}, ""},
		{"thread in a session channel", ActivityItem{Type: "thread_v2", Channel: "C0AGENT01", ThreadTS: "1762790000.000100", Root: root("", 0)}, ""},
	}
	for _, c := range cases {
		got, until := a.ActivityAgentStatus(&c.it, now)
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
		if c.name == "processing until later" && !until.Equal(now.Add(60*time.Second)) {
			t.Errorf("%s: until %v", c.name, until)
		}
	}
	if AgentStatusLabel(AgentProcessing) != "Working" || AgentStatusLabel(AgentSuspended) != "Needs attention" {
		t.Error("UaSM labels")
	}
	a.SetExperimentGroup("agent_status_in_activity", "off")
	if got, _ := a.ActivityAgentStatus(&ActivityItem{Type: "channel", Channel: "C0AGENT01"}, now); got != "" {
		t.Errorf("agent_status_in_activity off: %q", got)
	}
	a.SetExperimentGroup("agent_status_in_activity", "")
	if got, _ := a.ActivityAgentStatus(&ActivityItem{Type: "channel", Channel: "C0AGENT01"}, now); got != "" {
		t.Errorf("agent_status_in_activity unassigned: %q", got)
	}
}

func itoa64(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// A dragged tab is updateActivityViewsByApi {nextViewId}: the view's
// sort and density with next_view_id, and the server numbers the views'
// positions again.
func TestActivityMoveView(t *testing.T) {
	a, fs := actFake(t)
	ctx := context.Background()
	c, err := a.CreateActivityView(ctx, ActivityView{Name: "Synthetic moved", Density: "compact",
		Filters: ActivityViewFilters{EntryTypes: []string{"message_reaction"}}})
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.ActivityViewsState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var mentions ActivityView
	for _, v := range st.Views {
		if v.Type == "mentions" {
			mentions = v
		}
	}
	if _, err := a.MoveActivityView(ctx, ActivityView{ID: c.ID, Sort: "newest", Density: "dense"}, mentions.ID); err != nil {
		t.Fatal(err)
	}
	w := actLastWrite(t, fs, "activity.views.update")
	if w.Form.Get("view_id") != c.ID || w.Form.Get("next_view_id") != mentions.ID || w.Form.Get("sort") != "newest" ||
		w.Form.Get("density") != "dense" || w.Form.Get("reason") != "[Activity Views] Update views by API" || w.Form.Has("filters") || w.Form.Has("name") {
		t.Errorf("move form %v", w.Form)
	}
	st, _ = a.ActivityViewsState(ctx)
	var order []string
	pos := map[string]string{}
	for _, v := range st.Views {
		order = append(order, v.Type+":"+v.Name)
		pos[v.ID] = v.Position
	}
	if pos[c.ID] >= pos[mentions.ID] {
		t.Errorf("after the move: %v (positions %v)", order, pos)
	}
	// to the end: next_view_id ""
	if _, err := a.MoveActivityView(ctx, ActivityView{ID: mentions.ID}, ""); err != nil {
		t.Fatal(err)
	}
	if w := actLastWrite(t, fs, "activity.views.update"); !w.Form.Has("next_view_id") || w.Form.Get("next_view_id") != "" || w.Form.Get("sort") != "newest" || w.Form.Get("density") != "compact" {
		t.Errorf("move to the end: %v", w.Form)
	}
	st, _ = a.ActivityViewsState(ctx)
	if last := st.Views[len(st.Views)-1]; last.ID != mentions.ID {
		t.Errorf("last view %+v, want Mentions", last)
	}
}
