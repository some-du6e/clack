package fakeslack

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Sender identity fixtures (Size.Identity): one synthetic message per case
// Slack's web client tells apart when it draws a sender (avatar, name,
// badge, grouping, pinned label). Shapes follow real messages seen
// 2026-11-08: an agent app's reply carries its bot user plus a
// bot_profile with Slack's generic plugin icons; an app posting with a
// person's token carries the person plus the app's bot_id, app_id,
// bot_profile and metadata, and no subtype, username or icons.
const (
	IdentityChannelID = "C0IDENT0001"

	IdentityAgentID    = "U0FAKEAGNT" // an agent app's bot user (agent_status consistently_an_agent)
	IdentityAgentBotID = "B0FAKEAGNT"
	IdentityAgentAppID = "A0FAKEAGNT"
	IdentityWorkflowID = "U0FAKEWFLW" // a Workflow Builder bot user (is_workflow_bot)

	IdentityHookBotID  = "B0FAKEHOOK" // a user-less bot with its own icons
	IdentityBareBotID  = "B0FAKEBARE" // a user-less bot with no icons anywhere
	IdentityExtraBotID = "B0FAKEXTRA" // an app that posts with the person's token
	IdentityExtraAppID = "A0FAKEXTRA"
)

// IdentityCase is one fixture message and how Slack draws it.
type IdentityCase struct {
	Name   string
	TS     string
	Header bool   // starts a group (avatar + name line); false: a continuation row
	Sender string // the name shown
	// AvatarUser is the member whose picture is drawn; when "", Avatar
	// is the expected URL (or "default" for Slack's ava_0002, "emoji:<name>").
	AvatarUser, Avatar string
	Badge              string // "" (none), APP, AGENT, WORKFLOW…
	Label              string // the label line above the name ("Pinned by you")
}

func identityTS(i int) string { return "1794100" + strconv.Itoa(100+i*15) + ".000100" }

// identityImg is an avatar the fake serves for name at px.
func (s *Server) identityImg(px int, name string) string {
	return s.URL + "img/" + strconv.Itoa(px) + "x" + strconv.Itoa(px) + "/" + name + ".png"
}

func (s *Server) identityIcons(name string) slack.Icons {
	return slack.Icons{Image36: s.identityImg(36, name), Image48: s.identityImg(48, name), Image72: s.identityImg(72, name)}
}

// genericAppIcons are Slack's generic plugin icons that bot_profile carries
// for an app without its own (the blue notebook at 72px).
var genericAppIcons = slack.Icons{Image36: "https://a.slack-edge.com/80588/img/plugins/app/bot_36.png",
	Image48: "https://a.slack-edge.com/80588/img/plugins/app/bot_48.png",
	Image72: "https://a.slack-edge.com/80588/img/plugins/app/service_72.png"}

// identityUsers are the fixtures' bot users.
func (s *Server) identityUsers() []slack.User {
	prof := func(real, n string) slack.Profile {
		return slack.Profile{RealName: real, Team: TeamID, Fields: []byte(`{}`), Image24: s.identityImg(24, n), Image32: s.identityImg(32, n),
			Image48: s.identityImg(48, n), Image72: s.identityImg(72, n), Image192: s.identityImg(192, n), Image512: s.identityImg(512, n)}
	}
	ag := prof("Kite Agent", "agent")
	ag.BotID, ag.APIAppID = IdentityAgentBotID, IdentityAgentAppID
	wf := prof("Standup Workflow", "workflow")
	return []slack.User{
		{ID: IdentityAgentID, TeamID: TeamID, Name: "kite", RealName: "Kite Agent", IsBot: true, AgentStatus: "consistently_an_agent", Profile: ag},
		{ID: IdentityWorkflowID, TeamID: TeamID, Name: "standup", RealName: "Standup Workflow", IsBot: true, IsWorkflowBot: true, Profile: wf},
	}
}

// identityBots are bots.info's answers for the fixtures' bots.
func (s *Server) identityBots() []slack.BotProfile {
	return []slack.BotProfile{
		{ID: IdentityHookBotID, Name: "Build Hook", AppID: "A0FAKEHOOK", Icons: s.identityIcons("hook")},
		{ID: IdentityBareBotID, Name: "Bare Bot", AppID: "A0FAKEBARE"},
		{ID: IdentityExtraBotID, Name: "Fake Extra", AppID: IdentityExtraAppID, Icons: s.identityIcons("extra")},
		{ID: IdentityAgentBotID, Name: "kite", AppID: IdentityAgentAppID, Icons: genericAppIcons},
	}
}

// IdentityBots returns bots.info's bots for the fixtures (tests seed a store).
func (s *Server) IdentityBots() []slack.BotProfile { return s.identityBots() }

// IdentityUsers returns the fixtures' users with the people they quote.
func (s *Server) IdentityUsers() []slack.User {
	out := s.identityUsers()
	for _, u := range s.variants() {
		if u.ID == ProfileID || u.ID == AdminID || u.ID == SlackbotID {
			out = append(out, u)
		}
	}
	return out
}

// IdentityMessages returns the fixture messages (oldest first) and the
// cases they demonstrate.
func (s *Server) IdentityMessages() ([]slack.Message, []IdentityCase) {
	var msgs []slack.Message
	var cases []IdentityCase
	add := func(m slack.Message, c IdentityCase) {
		m.Type, m.TS = "message", identityTS(len(msgs))
		if m.Text == "" {
			m.Text = c.Name
		}
		c.TS = m.TS
		msgs, cases = append(msgs, m), append(cases, c)
	}
	extraProfile := func() *slack.BotProfile {
		return &slack.BotProfile{ID: IdentityExtraBotID, Name: "Fake Extra", AppID: IdentityExtraAppID, TeamID: TeamID, Icons: s.identityIcons("extra")}
	}
	agentProfile := func() *slack.BotProfile {
		return &slack.BotProfile{ID: IdentityAgentBotID, Name: "kite", AppID: IdentityAgentAppID, UserID: IdentityAgentID, TeamID: TeamID, Icons: genericAppIcons}
	}
	hookProfile := &slack.BotProfile{ID: IdentityHookBotID, Name: "Build Hook", AppID: "A0FAKEHOOK", TeamID: TeamID, Icons: s.identityIcons("hook")}
	meta := []byte(`{"event_type":"anchor","event_payload":{}}`)

	add(slack.Message{User: ProfileID, BotID: IdentityExtraBotID, BotProfile: extraProfile(), Metadata: meta, Text: "anchored, pinned by the viewer",
		PinnedTo: []string{IdentityChannelID}, PinnedInfo: &slack.PinnedInfo{Channel: IdentityChannelID, PinnedBy: SelfID, PinnedTS: 1794100150}},
		IdentityCase{Name: "app as the user, pinned by you", Header: true, Sender: "robin", AvatarUser: ProfileID, Label: "Pinned by you"})
	add(slack.Message{User: ProfileID, Text: "a person writes"},
		IdentityCase{Name: "person after their pinned message: grouped", Sender: "robin", AvatarUser: ProfileID})
	add(slack.Message{User: ProfileID, BotID: IdentityExtraBotID, BotProfile: extraProfile(), Metadata: meta, Text: "posted by an app with the person's token"},
		IdentityCase{Name: "app as the user: the person, grouped", Sender: "robin", AvatarUser: ProfileID})
	add(slack.Message{User: IdentityAgentID, BotID: IdentityAgentBotID, BotProfile: agentProfile(), Text: "Thinking completed"},
		IdentityCase{Name: "agent app reply: its bot user's picture, APP", Header: true, Sender: "Kite Agent", AvatarUser: IdentityAgentID, Badge: "APP"})
	add(slack.Message{User: IdentityAgentID, BotID: IdentityAgentBotID, BotProfile: agentProfile(), Text: "a second reply"},
		IdentityCase{Name: "agent app, grouped", Sender: "Kite Agent", AvatarUser: IdentityAgentID, Badge: "APP"})
	add(slack.Message{BotID: IdentityHookBotID, Subtype: "bot_message", BotProfile: hookProfile, Text: "build 412 passed"},
		IdentityCase{Name: "user-less bot: bot_profile", Header: true, Sender: "Build Hook", Avatar: s.identityImg(72, "hook"), Badge: "APP"})
	add(slack.Message{BotID: IdentityHookBotID, Subtype: "bot_message", BotProfile: hookProfile, Username: "Release Notes", Icons: &slack.Icons{Emoji: ":rocket:"}, Text: "v2.1 is out"},
		IdentityCase{Name: "username + icon_emoji override", Header: true, Sender: "Release Notes", Avatar: "emoji:rocket", Badge: "APP"})
	add(slack.Message{BotID: IdentityHookBotID, Subtype: "bot_message", BotProfile: hookProfile, Username: "Release Notes", Icons: &slack.Icons{Emoji: ":rocket:"}, Text: "v2.1.1 too"},
		IdentityCase{Name: "same username and icons: grouped", Sender: "Release Notes", Avatar: "emoji:rocket", Badge: "APP"})
	add(slack.Message{BotID: IdentityHookBotID, Subtype: "bot_message", BotProfile: hookProfile, Username: "Release Notes",
		Icons: &slack.Icons{Image48: s.identityImg(48, "notes"), Image72: s.identityImg(72, "notes")}, Text: "with an icon_url"},
		IdentityCase{Name: "icon_url override: new group", Header: true, Sender: "Release Notes", Avatar: s.identityImg(72, "notes"), Badge: "APP"})
	add(slack.Message{BotID: IdentityBareBotID, BotProfile: &slack.BotProfile{ID: IdentityBareBotID, Name: "Bare Bot", AppID: "A0FAKEBARE", TeamID: TeamID}, Text: "no icons anywhere"},
		IdentityCase{Name: "bot without icons: Slack's default avatar", Header: true, Sender: "Bare Bot", Avatar: "default", Badge: "APP"})
	add(slack.Message{User: IdentityWorkflowID, Text: "standup time"},
		IdentityCase{Name: "workflow bot: WORKFLOW", Header: true, Sender: "Standup Workflow", AvatarUser: IdentityWorkflowID, Badge: "WORKFLOW"})
	add(slack.Message{User: SlackbotID, Text: "a Slackbot note"},
		IdentityCase{Name: "Slackbot: no badge", Header: true, Sender: "Slackbot", AvatarUser: SlackbotID})
	add(slack.Message{User: AdminID, Text: "pinned by someone else",
		PinnedTo: []string{IdentityChannelID}, PinnedInfo: &slack.PinnedInfo{Channel: IdentityChannelID, PinnedBy: ProfileID, PinnedTS: 1794100300}},
		IdentityCase{Name: "pinned by another member", Header: true, Sender: "avery.admin", AvatarUser: AdminID, Label: "Pinned by robin"})
	add(slack.Message{User: AdminID, DisplayAsBot: true, Text: "display_as_bot is ignored"},
		IdentityCase{Name: "display_as_bot: still the person", Sender: "avery.admin", AvatarUser: AdminID})
	add(slack.Message{User: ProfileID, BotID: IdentityExtraBotID, Subtype: "bot_message", Username: "Ignored Name",
		Icons: &slack.Icons{Image72: s.identityImg(72, "ignored")}, Text: "a user's message ignores username and icons"},
		IdentityCase{Name: "user + username/icons: the person", Header: true, Sender: "robin", AvatarUser: ProfileID})
	return msgs, cases
}

// identityConv is the fixtures' channel.
func identityConv() slack.Conversation {
	return slack.Conversation{ID: IdentityChannelID, Name: "identity-fixtures", IsChannel: true, IsMember: true,
		Created: 1700000000, Creator: SelfID, NumMembers: 6, ContextTeamID: TeamID,
		Purpose: &slack.Purpose{Value: "Synthetic bot and app senders"}}
}

// addIdentity adds the channel, its history and bots (Size.Identity).
func (s *Server) addIdentity() {
	s.convs = append(s.convs, identityConv())
	msgs, _ := s.IdentityMessages()
	newest := make([]slack.Message, len(msgs))
	for i, m := range msgs {
		newest[len(msgs)-1-i] = m
	}
	s.hist[IdentityChannelID] = newest
}

// handleIdentity answers bots.info for the fixtures' bots.
func (s *Server) handleIdentity(method string, f url.Values) (any, bool) {
	if method != "bots.info" {
		return nil, false
	}
	want := map[string]bool{}
	for _, id := range strings.Split(f.Get("bots")+","+f.Get("bot"), ",") {
		if id != "" {
			want[id] = true
		}
	}
	var out []slack.BotProfile
	for _, b := range s.identityBots() {
		if want[b.ID] {
			out = append(out, b)
		}
	}
	return map[string]any{"ok": true, "bots": out}, true
}

// identityAppProfile is apps.profile.get for the agent app: a full
// profile with is_shown_as_agent, as the real kyto's (Slack shows AGENT
// once it holds this, APP before).
func (s *Server) identityAppProfile(bot, app string) (map[string]any, bool) {
	if bot != IdentityAgentBotID && app != IdentityAgentAppID {
		return nil, false
	}
	icons := map[string]any{}
	for _, px := range []int{32, 48, 64, 72, 192} {
		icons["image_"+strconv.Itoa(px)] = "https://a.slack-edge.com/80588/img/apps/default_new_app_icon.png"
	}
	return map[string]any{"id": IdentityAgentAppID, "name": "kite", "desc": "A synthetic agent", "app_user_id": IdentityAgentID,
		"team_id": TeamID, "is_installed": true, "is_agent_app": true, "is_shown_as_agent": true, "icons": icons,
		"bot_user": map[string]any{"id": IdentityAgentID, "username": "kite"}}, true
}
