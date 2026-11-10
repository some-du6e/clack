package core

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Slack's computeLocality: org members (team_id the workspace, or the org
// on Grid) aren't external; a Slack Connect member is.
func TestLocality(t *testing.T) {
	a := New(slack.New(slack.Creds{Team: "T1", EnterpriseID: "E1"}), store.New(""))
	cases := []struct {
		name string
		u    slack.User
		team string
		ext  bool
	}{
		{"workspace member", slack.User{ID: "U1", TeamID: "T1"}, "T1", false},
		{"grid member of another workspace", slack.User{ID: "U2", TeamID: "T2", EnterpriseUser: &slack.EnterpriseUser{ID: "U2", EnterpriseID: "E1", Teams: []string{"T2"}}}, "E1", false},
		{"grid member listing the workspace", slack.User{ID: "U3", TeamID: "T9", EnterpriseUser: &slack.EnterpriseUser{Teams: []string{"T1"}}}, "T9", false},
		{"team_id the org", slack.User{ID: "U4", TeamID: "E1"}, "E1", false},
		{"no team", slack.User{ID: "U5"}, "T1", false},
		{"slack connect", slack.User{ID: "U6", TeamID: "TX"}, "TX", true},
		{"other org", slack.User{ID: "U7", TeamID: "TX", EnterpriseUser: &slack.EnterpriseUser{EnterpriseID: "EX", Teams: []string{"TX"}}}, "EX", true},
	}
	for _, c := range cases {
		team, ext := a.locality(&c.u)
		if team != c.team || ext != c.ext {
			t.Errorf("%s: team %q external %v, want %q %v", c.name, team, ext, c.team, c.ext)
		}
	}
}

// The fake's app and external fixtures through core: the bot user's ids,
// the external member's team, the app card mapped like Slack's ToML, the
// partner team's name.
func TestAppCardAndExternal(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(""))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.EnsureUsers(ctx, []string{fakeslack.AppUserID, fakeslack.ExternalID, fakeslack.ProfileID, fakeslack.ExternalAppUserID})
	u, ok := a.User(fakeslack.AppUserID)
	if !ok || !u.IsBot || u.BotID != fakeslack.AppBotID || u.AppID != fakeslack.AppAppID || u.External {
		t.Fatalf("app user %+v", u)
	}
	x, _ := a.User(fakeslack.ExternalID)
	if !x.External || x.TeamID != fakeslack.ExternalTeamID {
		t.Fatalf("external user: team %q external %v", x.TeamID, x.External)
	}
	if p, _ := a.User(fakeslack.ProfileID); p.External {
		t.Fatal("a workspace member is external")
	}
	c, err := a.AppCardOf(ctx, fakeslack.AppUserID, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Deploy Bot" || c.Tagline == "" || c.Color != "1264a3" || c.Icon == "" || !c.Active ||
		c.InstallerID != fakeslack.AdminID || c.AppUserID != fakeslack.AppUserID || c.External || len(c.Commands) != 1 || c.Commands[0].Name != "/deploy" {
		t.Fatalf("app card %+v", c)
	}
	if got, ok := a.CachedAppCard("", fakeslack.AppBotID); !ok || got.Name != c.Name {
		t.Fatal("card not cached by bot id")
	}
	xc, err := a.AppCardOf(ctx, fakeslack.ExternalAppUserID, "")
	if err != nil || !xc.External || xc.TopTeam() != fakeslack.ExternalTeamID {
		t.Fatalf("external app card %+v %v", xc, err)
	}
	if _, err := a.AppCardOf(ctx, fakeslack.ProfileID, ""); err != ErrNotAnApp {
		t.Fatalf("a member's app card: %v", err)
	}
	a.EnsureTeam(ctx, fakeslack.ExternalTeamID)
	if name, icon, ok := a.Team(fakeslack.ExternalTeamID, 20); !ok || name != "Partner Co" || icon == "" {
		t.Fatalf("team %q %q %v", name, icon, ok)
	}
}
