package core

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Synthetic sender identities, one per case Slack's web client tells
// apart (sender.go, badge.go). Shapes follow real messages (2026-11-08):
// an agent app's reply carries its bot user plus bot_profile with Slack's
// generic plugin icons; an app posting with the user's token carries the
// user plus the app's bot_id, app_id and bot_profile.
func identityApp(t *testing.T) *App {
	a, _ := testApp(t)
	a.S.PutUsers([]slack.User{
		{ID: "UAGENT", Name: "agentbot", RealName: "Agent", IsBot: true, AgentStatus: "consistently_an_agent",
			Profile: slack.Profile{AvatarHash: "g0agent", BotID: "BAGENT", APIAppID: "AAGENT"}},
		{ID: "UWF", Name: "workflow", IsBot: true, IsWorkflowBot: true, Profile: slack.Profile{BotID: "BWF"}},
		{ID: "UAF", Name: "agentforce", IsBot: true, IsAgentforceBot: true},
		{ID: "UAPPU", Name: "appuser", IsAppUser: true},
		{ID: "USLACKBOT", Name: "slackbot", RealName: "Slackbot"},
	})
	a.S.PutBot("BSTORE", store.BotInfo{Name: "Stored Bot", Icon: "https://img.example/store72.png", AppID: "ASTORE"})
	a.S.PutBot("BWFB", store.BotInfo{Name: "Workflow Builder", Icon: "https://img.example/wfb72.png", AppID: "A0112L5RZ5E", Workflow: true})
	return a
}

var genericIcons = slack.Icons{Image36: "https://a.slack-edge.com/80588/img/plugins/app/bot_36.png",
	Image48: "https://a.slack-edge.com/80588/img/plugins/app/bot_48.png",
	Image72: "https://a.slack-edge.com/80588/img/plugins/app/service_72.png"}

func TestSenderIdentity(t *testing.T) {
	a := identityApp(t)
	for _, c := range []struct {
		name       string
		m          slack.Message
		wantName   string
		wantAvatar string // "" = the member's (the UI reads the user)
		wantBot    bool
	}{
		{"agent app reply (user + bot_profile with generic icons)", slack.Message{User: "UAGENT", BotID: "BAGENT",
			BotProfile: &slack.BotProfile{ID: "BAGENT", Name: "agent", AppID: "AAGENT", UserID: "UAGENT", Icons: genericIcons}}, "", "", false},
		{"app posting as the user (user + bot_id + bot_profile)", slack.Message{User: "U1", BotID: "BX",
			BotProfile: &slack.BotProfile{ID: "BX", Name: "Slack Extra", AppID: "AX", Icons: slack.Icons{Image72: "https://img.example/extra72.png"}}}, "", "", false},
		{"username and icons on a message with a user are ignored", slack.Message{User: "U1", BotID: "BX", Subtype: "bot_message",
			Username: "Override", Icons: &slack.Icons{Image48: "https://img.example/o48.png"}}, "", "", false},
		{"display_as_bot is ignored", slack.Message{User: "U1", DisplayAsBot: true}, "", "", false},
		{"user-less bot: bot_profile name and icons", slack.Message{BotID: "BNEW", Subtype: "bot_message",
			BotProfile: &slack.BotProfile{ID: "BNEW", Name: "Profiled", Icons: slack.Icons{Image36: "https://img.example/p36.png", Image72: "https://img.example/p72.png"}}},
			"Profiled", "https://img.example/p72.png", true},
		{"user-less bot: username + icon_url override the profile", slack.Message{BotID: "BNEW", Subtype: "bot_message", Username: "Custom Name",
			Icons:      &slack.Icons{Image48: "https://img.example/u48.png", Image72: "https://img.example/u72.png"},
			BotProfile: &slack.BotProfile{ID: "BNEW", Name: "Profiled", Icons: genericIcons}}, "Custom Name", "https://img.example/u72.png", true},
		{"user-less bot: icon_emoji", slack.Message{BotID: "BNEW", Username: "Party", Icons: &slack.Icons{Emoji: ":tada:"}}, "Party", "1f389", true},
		{"user-less bot: the store bot's icon", slack.Message{BotID: "BSTORE", Subtype: "bot_message"}, "Stored Bot", "https://img.example/store72.png", true},
		{"Workflow Builder prefers its own icon", slack.Message{BotID: "BWFB", Icons: &slack.Icons{Image72: "https://img.example/msg72.png"}},
			"Workflow Builder", "https://img.example/wfb72.png", true},
		{"user-less bot with no icons anywhere: Slack's ava_0002", slack.Message{BotID: "BNEW",
			BotProfile: &slack.BotProfile{ID: "BNEW", Name: "Plain"}}, "Plain", DefaultBotAvatar, true},
	} {
		c.m.TS, c.m.Text = "1700000000.000100", "synthetic"
		v := a.message("C1", &c.m)
		if v.Name != c.wantName {
			t.Errorf("%s: name %q, want %q", c.name, v.Name, c.wantName)
		}
		if c.wantAvatar == "" && v.Avatar != "" || c.wantAvatar != "" && !strings.Contains(v.Avatar, c.wantAvatar) {
			t.Errorf("%s: avatar %q, want %q", c.name, v.Avatar, c.wantAvatar)
		}
		if v.IsBot != c.wantBot {
			t.Errorf("%s: IsBot %v, want %v", c.name, v.IsBot, c.wantBot)
		}
	}
}

func TestSenderIconsPick(t *testing.T) {
	ic := &SenderIcons{Image36: "36", Image48: "48", Image72: "72"}
	for px, want := range map[int]string{24: "36", 36: "36", 40: "48", 72: "72", 192: "72"} {
		if got := ic.Pick(px); got != want {
			t.Errorf("Pick(%d) = %q, want %q", px, got, want)
		}
	}
	if (&SenderIcons{Emoji: ":x:"}).Pick(72) != "" || (*SenderIcons)(nil).Pick(72) != "" {
		t.Error("no images should pick nothing")
	}
}

func TestSenderBadge(t *testing.T) {
	a := identityApp(t)
	a.SetExperimentGroup("show_as_agent", "on")
	a.SetExperimentGroup("agent_badge", "on")
	cases := []struct{ user, bot, want string }{
		{"U1", "", ""},                 // a person
		{"U1", "BX", ""},               // an app posting as the person
		{"USLACKBOT", "", ""},          // Slackbot is served is_bot false
		{"UAGENT", "BAGENT", BadgeApp}, // agent_status alone isn't read (support_lots_of_agents off)
		{"UWF", "", BadgeWorkflow},     // is_workflow_bot
		{"UAF", "", BadgeAgentforce},   // is_agentforce_bot
		{"UAPPU", "", BadgeApp},        // is_app_user
		{"", "BSTORE", BadgeApp},       // user-less bot
		{"", "BWFB", BadgeWorkflow},    // the bot's is_workflow_bot
		{"", "BUNKNOWN", BadgeApp},     // unknown bot: still APP
		{"UNOTLOADED", "", ""},         // not loaded yet: Slack's unknown placeholder
	}
	for _, c := range cases {
		if got := a.SenderBadge(c.user, c.bot); got != c.want {
			t.Errorf("SenderBadge(%q, %q) = %q, want %q", c.user, c.bot, got, c.want)
		}
	}
	// the app's full profile says is_shown_as_agent: AGENT
	putCard(a, "B:BAGENT", AppCard{AppID: "AAGENT", ShownAsAgent: true})
	if got := a.SenderBadge("UAGENT", "BAGENT"); got != BadgeAgent {
		t.Errorf("shown-as-agent app: %q, want AGENT", got)
	}
	a.SetExperimentGroup("agent_badge", "off")
	if got := a.SenderBadge("UAGENT", "BAGENT"); got != BadgeApp {
		t.Errorf("agent_badge off: %q, want APP", got)
	}
	// support_lots_of_agents: agent_status decides
	a.SetExperimentGroup("agent_badge", "on")
	a.SetExperimentGroup("support_lots_of_agents", "on")
	putCard(a, "B:BAGENT", AppCard{AppID: "AAGENT"})
	if got := a.SenderBadge("UAGENT", "BAGENT"); got != BadgeAgent {
		t.Errorf("support_lots_of_agents + consistently_an_agent: %q, want AGENT", got)
	}
}

func putCard(a *App, key string, card AppCard) {
	c := a.appCards()
	c.mu.Lock()
	c.m[key] = &appCardEntry{card: card}
	c.mu.Unlock()
}

func TestAdjacent(t *testing.T) {
	ic := func(u string) *SenderIcons { return &SenderIcons{Image72: u} }
	for _, c := range []struct {
		name       string
		prev, msg  Message
		wantAdjoin bool
	}{
		{"same user", Message{User: "U1"}, Message{User: "U1"}, true},
		{"different users", Message{User: "U1"}, Message{User: "U2"}, false},
		{"same user, one via an app (bot_id)", Message{User: "U1"}, Message{User: "U1", BotID: "BX"}, true},
		{"same bot user, different username/icons", Message{User: "UB", Username: "a"}, Message{User: "UB", Username: "b", Icons: ic("x")}, true},
		{"user-less bot, same bot", Message{BotID: "B1"}, Message{BotID: "B1"}, true},
		{"user-less bot, different bots", Message{BotID: "B1"}, Message{BotID: "B2"}, false},
		{"user-less bot, username changed", Message{BotID: "B1", Username: "a"}, Message{BotID: "B1", Username: "b"}, false},
		{"user-less bot, username dropped", Message{BotID: "B1", Username: "a"}, Message{BotID: "B1"}, true},
		{"user-less bot, icons changed", Message{BotID: "B1", Icons: ic("a")}, Message{BotID: "B1", Icons: ic("b")}, false},
		{"user-less bot after its bot user", Message{User: "UB", BotID: "B1"}, Message{BotID: "B1"}, true},
		{"username only", Message{Username: "rss"}, Message{Username: "rss"}, true},
		{"bot user then user-less same bot with username", Message{User: "UB", BotID: "B1"}, Message{BotID: "B1", Username: "x"}, false},
	} {
		if got := Adjacent(&c.prev, &c.msg); got != c.wantAdjoin {
			t.Errorf("%s: Adjacent = %v, want %v", c.name, got, c.wantAdjoin)
		}
	}
}

// pin_added records who pinned it (the label's "Pinned by …");
// pin_removed clears it.
func TestPinEventPinnedBy(t *testing.T) {
	a, _ := testApp(t)
	a.S.SetLatest("C1", []slack.Message{{Type: "message", TS: "1700000000.000100", User: "U1", Text: "synthetic"}}, false)
	a.Inject(rtm("pin_added", map[string]any{"user": "U2", "channel_id": "C1", "event_ts": "1700000100.000200",
		"item": map[string]any{"type": "message", "channel": "C1", "created_by": "U2", "created": 1700000100,
			"message": map[string]any{"ts": "1700000000.000100", "user": "U1", "text": "synthetic"}}}))
	got := func() Message {
		m, ok := a.S.Find("C1", "1700000000.000100")
		if !ok {
			t.Fatal("message gone")
		}
		return a.message("C1", &m)
	}
	if m := got(); !m.Pinned || m.PinnedBy != "U2" {
		t.Errorf("after pin_added: pinned %v by %q, want U2", m.Pinned, m.PinnedBy)
	}
	a.Inject(rtm("pin_removed", map[string]any{"user": "U2", "channel_id": "C1",
		"item": map[string]any{"type": "message", "channel": "C1", "message": map[string]any{"ts": "1700000000.000100"}}}))
	if m := got(); m.Pinned || m.PinnedBy != "" {
		t.Errorf("after pin_removed: pinned %v by %q", m.Pinned, m.PinnedBy)
	}
}
