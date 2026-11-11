package core

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// liveTestProps is the shape #test (C0BUVSNLEA3) had on 2026-11-08:
// conversations.info's channel.properties, values replaced.
const liveTestProps = `{"record_channel":{"record_id":"AC:1:2:C0CODE001","record_type":"agent_channel","record_label":"Code Channel","record_label_plural":"Code Channels"},` +
	`"code_channel":{},"agent_session":{"status":"active","agent_bot_user_ids":[],"encoded_agent_bot_user_ids":["UAGENT1"],"title":"Fix the flaky test"}}`

func TestParseCodeChannel(t *testing.T) {
	cc := ParseCodeChannel(json.RawMessage(liveTestProps))
	if cc == nil {
		t.Fatal("#test's properties are a code channel")
	}
	if cc.SessionStatus != "active" || cc.Title != "Fix the flaky test" || len(cc.AgentBotIDs) != 1 || cc.AgentBotIDs[0] != "UAGENT1" {
		t.Errorf("agent session: %+v", cc)
	}
	if !cc.Empty || cc.RecordLabel != "Code Channel" || cc.RecordPlural != "Code Channels" {
		t.Errorf("record/code_channel: %+v", cc)
	}
	// aK5v.$n: record_type agent_channel, or an AC: record id
	for props, want := range map[string]bool{
		`{"tabs":[{"type":"files"}]}`:                                                         false, // #bot-tests: a normal channel
		`{"record_channel":{"record_id":"AC:9","record_type":"other"}}`:                       true,
		`{"record_channel":{"record_id":"SF:9","record_type":"agent_channel"}}`:               true,
		`{"record_channel":{"record_id":"SF:9","record_type":"salesforce_record"}}`:           false,
		`{"record_channel":{"record_type":"agent_channel"}}`:                                  false, // no record_id: not a record channel
		`{"agent_session":{"status":"active"},"code_channel":{"repo":"some/repo"}}`:           false,
		`{"record_channel":{"record_id":"AC:1"},"agent_session":{"agent_bot_user_ids":[42]}}`: true,
	} {
		if got := ParseCodeChannel(json.RawMessage(props)) != nil; got != want {
			t.Errorf("%s: code channel %v, want %v", props, got, want)
		}
	}
	cc = ParseCodeChannel(json.RawMessage(`{"record_channel":{"record_id":"AC:1"},"agent_session":{"agent_bot_user_ids":[42]},` +
		`"code_channel":{"repo":"acme/api","branch":"fix-1","pr_status":"open","context_bar_items":[{"key":"pr","label":"PR #12","icon":"branch","url":"https://example.com/pr/12"},{"key":"deploy","item_type":"action","label":"Deploy","icon":"terminal"}],` +
		`"agent_session_views":[{"view_id":"V1","type":"block_kit","name":"Summary"},{"view_id":"V2","type":"pull_request","pr_url":"https://example.com/pr/12"}],` +
		`"agent_views":[{"view_id":"H1","file_id":"F1"}],"diff_file_id":"F2","plan_canvases":[{"canvas_id":"F3"}]}}`))
	if cc.AgentBotIDs[0] != "42" || cc.Repo != "acme/api" || cc.Branch != "fix-1" || cc.PRStatus != "open" || cc.Empty {
		t.Errorf("code_channel: %+v", cc)
	}
	if len(cc.ContextBar) != 2 || cc.ContextBar[0].IsAction() || !cc.ContextBar[1].IsAction() {
		t.Errorf("context bar: %+v", cc.ContextBar)
	}
	var views []string
	for _, v := range cc.Views {
		views = append(views, v.ViewID+":"+v.Type)
	}
	// getCodeChannelViews: no PR views (prViewsEnabled off), the Code diff first
	if got, want := strings.Join(views, ","), "code:diff,V1:block_kit,H1:html,F3:canvas"; got != want {
		t.Errorf("views %s, want %s", got, want)
	}
}

func TestCodeChannelStatus(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	ts := func(ago time.Duration) string { return strconv.FormatInt(now.Add(-ago).Unix(), 10) + ".000100" }
	cc := func(status string, expire int64) *CodeChannel {
		return &CodeChannel{SessionStatus: status, ProcessingExpires: expire}
	}
	for _, c := range []struct {
		cc     *CodeChannel
		latest string
		on     bool
		want   string
	}{
		{cc("active", 0), ts(time.Hour), true, AgentActive},
		{cc("", 0), ts(time.Hour), true, AgentActive},
		{cc("active", 0), ts(4 * 24 * time.Hour), true, AgentStale}, // quiet for over 3 days
		{cc("active", 0), ts(4 * 24 * time.Hour), false, AgentActive},
		{cc("closed", 0), ts(9 * 24 * time.Hour), true, AgentClosed},
		{cc("suspended", 0), ts(9 * 24 * time.Hour), true, AgentSuspended},
		{cc("processing", 0), ts(9 * 24 * time.Hour), true, AgentProcessing},
		{cc("processing", now.Unix()+60), ts(time.Minute), true, AgentProcessing},
		{cc("processing", now.Unix()-60), ts(time.Minute), true, AgentActive}, // expired, no live signal
		{cc("bogus", 0), ts(time.Minute), true, AgentActive},
	} {
		if got := codeChannelStatus(c.cc, c.latest, c.on, now); got != c.want {
			t.Errorf("%+v latest %s on=%v: %s, want %s", c.cc, c.latest, c.on, got, c.want)
		}
	}
	for _, c := range []struct{ status, pr, icon, tip string }{
		{AgentClosed, "", "check-circle", "Done"},
		{AgentStale, "", "minus", "Inactive"},
		{AgentSuspended, "", "needs-attention", "Needs attention"},
		{AgentProcessing, "open", "", "Working"},
		{AgentActive, "open", "pr-open", "PR open"},
		{AgentActive, "draft", "pr-open", "PR open"},
		{AgentActive, "blocked", "needs-attention", "PR blocked"},
		{AgentActive, "closed", "pr-closed", "PR closed"},
		{AgentActive, "merged", "branch", "PR merged"},
		{AgentActive, "", "", "Idle"},
	} {
		if icon, tip := AgentStatusDisplay(c.status, c.pr); icon != c.icon || tip != c.tip {
			t.Errorf("%s/%s: %q %q, want %q %q", c.status, c.pr, icon, tip, c.icon, c.tip)
		}
	}
}

// Code channels sit in Slack's client-made Code channels section, first
// in the list; starred ones stay in Starred; a custom section still wins.
func TestCodeChannelsSection(t *testing.T) {
	a, _ := testApp(t)
	s := a.S
	code := func(id, name string) slack.Conversation {
		return slack.Conversation{ID: id, Name: name, IsChannel: true, IsMember: true, Properties: json.RawMessage(liveTestProps)}
	}
	s.PutConv(code("CCODE1", "fix-flaky-test"))
	s.PutConv(code("CCODE2", "starred-session"))
	s.PutConv(code("CCODE3", "in-custom"))
	sec := func(id, typ, name, next string, chans ...string) slack.Section {
		x := slack.Section{ID: id, Type: typ, Name: name, Next: next}
		x.ChannelIDsPage.ChannelIDs = chans
		return x
	}
	s.Sections = []slack.Section{sec("L1", "stars", "", "L2", "CCODE2"), sec("L2", "standard", "Work", "L3", "CCODE3"), sec("L3", "channels", "", "")}
	close(a.ready)
	got := map[string]string{}
	var order []string
	for _, v := range a.Snapshot().Sections {
		order = append(order, v.Type)
		for _, id := range v.Channels {
			got[id] = v.ID
		}
	}
	if order[0] != "coding_channels" {
		t.Errorf("sections %v: Code channels should be spliced in first", order)
	}
	if got["CCODE1"] != LocalCodingChannelsID || got["CCODE2"] != "L1" || got["CCODE3"] != "L2" || got["C1"] != "L3" {
		t.Errorf("assignment %v", got)
	}
	a.SetExperimentGroup("session_channels", "off")
	for _, v := range a.Snapshot().Sections {
		if v.Type == "coding_channels" {
			t.Error("Code channels shown with session_channels off")
		}
	}
}

// channel_updated merges its updates into the stored channel: the agent
// status changes live, and the UI is told.
func TestCodeChannelUpdated(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "CCODE1", Name: "fix", IsChannel: true, IsMember: true, Topic: &slack.Purpose{Value: "keep me"}, Properties: json.RawMessage(liveTestProps)})
	evs, stop := a.Subscribe()
	defer stop()
	a.handle(rtm("channel_updated", map[string]any{"channel": "CCODE1", "updates": map[string]any{
		"properties": map[string]any{"agent_session": map[string]any{"status": "processing"},
			"code_channel": map[string]any{"repo": "acme/api", "branch": "main"}}}}))
	ev := next(t, evs, "conv")
	if ev.Conv == nil || ev.Conv.Code == nil || ev.Conv.CodeStatus != AgentProcessing {
		t.Fatalf("conv event %+v", ev.Conv)
	}
	c, _ := a.Conv("CCODE1")
	if c.Code.Title != "Fix the flaky test" || c.Code.Repo != "acme/api" || c.Code.Empty || c.Topic != "keep me" {
		t.Errorf("merge lost fields: %+v topic %q", c.Code, c.Topic)
	}
	if a.CodeStatus("CCODE1") != AgentProcessing {
		t.Errorf("CodeStatus %q", a.CodeStatus("CCODE1"))
	}
}

func TestCodeChannelCalls(t *testing.T) {
	a, f := testApp(t)
	f.reply["client.codeChannels.action"] = `{"ok":true}`
	f.reply["agents.sessions.stop"] = `{"ok":true}`
	f.reply["client.codeChannels.getBlockKitView"] = `{"ok":true,"blocks":[{"type":"section","text":{"type":"mrkdwn","text":"hi"}}],"service_id":"B1","app_id":"A1"}`
	ctx := context.Background()
	if err := a.CodeChannelAction(ctx, "CCODE1", "deploy"); err != nil {
		t.Fatal(err)
	}
	if v := f.last("client.codeChannels.action"); v.Get("channel_id") != "CCODE1" || v.Get("action_key") != "deploy" || !strings.HasPrefix(v.Get("client_token"), "web-") {
		t.Errorf("action args %v", v)
	}
	if err := a.StopAgent(ctx, "CCODE1"); err != nil {
		t.Fatal(err)
	}
	if v := f.last("agents.sessions.stop"); v.Get("channel") != "CCODE1" || v.Get("reason") != "stopped-by-user" {
		t.Errorf("stop args %v", v)
	}
	b, err := a.CodeChannelBlockKitView(ctx, "CCODE1", "V1")
	if err != nil || !strings.Contains(string(b), `"section"`) {
		t.Errorf("block kit view %s %v", b, err)
	}
	if v := f.last("client.codeChannels.getBlockKitView"); v.Get("channel_id") != "CCODE1" || v.Get("view_id") != "V1" {
		t.Errorf("view args %v", v)
	}
}

// code_channels_naming and session_channel_views: the section heading,
// the row tooltip and the details view's label are Slack's in every
// group (p4Ef, ConnectedChannel, WwBZ).
func TestCodeChannelNaming(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "UAGENT1", Name: "claude", IsBot: true, Profile: slack.Profile{DisplayName: "Claude", RealName: "Claude Code"}}})
	a.S.PutConv(slack.Conversation{ID: "CCODE1", Name: "fix", IsChannel: true, IsMember: true, Properties: json.RawMessage(liveTestProps)})
	a.S.PutConv(slack.Conversation{ID: "GCODE2", Name: "secret", IsChannel: true, IsPrivate: true, IsMember: true,
		Properties: json.RawMessage(`{"record_channel":{"record_id":"AC:1:2:GCODE2","record_type":"agent_channel"},"code_channel":{"repo":"acme/api"},"agent_session":{"status":"active","encoded_agent_bot_user_ids":["UNKNOWN"]}}`)})
	sec := slack.Section{ID: "S9", Type: "coding_channels", Name: "My agents"}
	a.S.Sections = []slack.Section{sec, {ID: "L3", Type: "channels"}}
	close(a.ready)
	heading := func() string {
		for _, v := range a.Snapshot().Sections {
			if v.Type == "coding_channels" {
				return v.Name
			}
		}
		return "(none)"
	}
	tip := func(ch string) string {
		kind, agent, ok := a.CodeChannelRowTooltip(ch)
		if !ok {
			return "(none)"
		}
		return strings.TrimSpace(kind + " " + agent)
	}
	a.setExperiments(map[string]string{"session_channels": "on", "code_channels_naming": "on", "session_channel_views": "on"})
	if h := heading(); h != "Code channels" {
		t.Errorf("heading %q (Slack's static name wins over the persisted one)", h)
	}
	if got := tip("CCODE1"); got != "Public code channel @Claude" {
		t.Errorf("tooltip %q", got)
	}
	if got := tip("GCODE2"); got != "Private code channel" { // the agent isn't loaded: no @name
		t.Errorf("private tooltip %q", got)
	}
	if got := tip("C1"); got != "(none)" {
		t.Errorf("plain channel tooltip %q", got)
	}
	if got := a.CodeChannelDetailsLabel(); got != "Code channel details" {
		t.Errorf("details label %q", got)
	}

	a.SetExperimentGroup("code_channels_naming", "off")
	if h := heading(); h != "Session channels" {
		t.Errorf("naming off: heading %q", h)
	}
	if got := tip("CCODE1"); got != "Public session channel @Claude" {
		t.Errorf("naming off: tooltip %q", got)
	}
	if got := tip("GCODE2"); got != "Private session channel" {
		t.Errorf("naming off: private tooltip %q", got)
	}
	if got := a.CodeChannelDetailsLabel(); got != "Session channel details" {
		t.Errorf("naming off: details label %q", got)
	}

	a.ClearExperimentGroup("code_channels_naming")
	a.SetExperimentGroup("session_channel_views", "") // unassigned
	if got := a.CodeChannelDetailsLabel(); got != "Session channel details" {
		t.Errorf("session_channel_views unassigned: details label %q", got)
	}

	a.SetExperimentGroup("session_channels", "off")
	if h := heading(); h != "My agents" {
		t.Errorf("session_channels off: heading %q, want the section's own name", h)
	}
	if got := tip("CCODE1"); got != "(none)" {
		t.Errorf("session_channels off: tooltip %q", got)
	}
}
