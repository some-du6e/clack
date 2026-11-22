package core

// botHomeTeam against Slack's rIIG getTeamIdFromBotOrUser (useProfileHoverApp's
// homeWorkspaceId, sent as apps.profile.get bot_home_team): the bot's
// team_id when it's a workspace's, then the message's bot_profile.team_id,
// then getTeamIdFromBotAppUser over the member (its workspace; its Grid
// workspaces, the viewer's when it's among them; the viewer's for none,
// Slackbot, the system user, a deactivated org member). An external Slack
// Connect bot posting with no user asked with the viewer's workspace.

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func TestBotHomeTeamLikeSlack(t *testing.T) {
	const cur, ext, other = "T0VIEWER01", "T0PARTNER1", "T0OTHERWS1"
	a := New(slack.New(slack.Creds{Token: "xoxc-test", Team: cur, UserID: "USELF"}), store.New(""))
	a.S.PutUsers([]slack.User{
		{ID: "UWS000001", TeamID: other, IsBot: true},
		{ID: "UGRID0001", TeamID: "E0ORG00001", IsBot: true, EnterpriseUser: &slack.EnterpriseUser{Teams: []string{other, ext}}},
		{ID: "UGRID0002", TeamID: "E0ORG00001", IsBot: true, EnterpriseUser: &slack.EnterpriseUser{Teams: []string{other, cur}}},
		{ID: "UGONE0001", TeamID: "E0ORG00001", IsBot: true, Deleted: true, EnterpriseUser: &slack.EnterpriseUser{Teams: []string{other}}},
		{ID: "USLACKBOT", TeamID: other, IsSlackbot: true},
		{ID: "UNOTEAM01", IsBot: true},
	})
	botTeamFrom(a, "B0EXTINFO1", ext, true)
	botTeamFrom(a, "B0EXTPROF1", ext, false)
	botTeamFrom(a, "B0ORGTEAM1", "E0ORG00001", true) // an org's id: not a workspace's
	for _, c := range []struct {
		name, bot, user, want string
	}{
		{"an external bot by bots.info, no user", "B0EXTINFO1", "", ext},
		{"an external bot by its messages' bot_profile, no user", "B0EXTPROF1", "", ext},
		{"the bot's team over the member's", "B0EXTINFO1", "UWS000001", ext},
		{"a bot whose team is an org's: the member's", "B0ORGTEAM1", "UWS000001", other},
		{"the member's workspace", "", "UWS000001", other},
		{"a Grid member: its first workspace", "", "UGRID0001", other},
		{"a Grid member in the viewer's workspace: the viewer's", "", "UGRID0002", cur},
		{"a deactivated org member: the viewer's", "", "UGONE0001", cur},
		{"Slackbot: the viewer's", "", "USLACKBOT", cur},
		{"a member with no team: the viewer's", "", "UNOTEAM01", cur},
		{"no member, an unknown bot: the viewer's", "B0UNKNOWN1", "", cur},
	} {
		if got := a.botHomeTeam(c.bot, c.user); got != c.want {
			t.Errorf("%s: bot_home_team %q, want %q (rIIG getTeamIdFromBotOrUser)", c.name, got, c.want)
		}
	}
}
