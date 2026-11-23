package ui

// Directories › External and Invitations (Slack's Slack Connect hub) and
// the external organization page a row opens, against internal/fakeslack
// (build a09b625d90aa: ExternalOrganizationsView SjEJ, SlackConnectPage
// gmwg, ExternalOrganizationView zJKG). The fake is a grid; its
// ThreadHeader fixture holds Partner Co (fakeslack.ExternalTeamID), its
// person Sam Partner and its bot, and #th-partner-co shared with it.
// Synthetic only: writes go to the fake.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/views"
)

// hubEnv starts the app on a fake with the Partner Co fixture; prep runs
// on the fake before boot.
func hubEnv(t *testing.T, prep func(fs *fakeslack.Server)) (*perfEnv, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	if prep != nil {
		prep(fs)
	}
	e := startEnv(t, fs, t.TempDir())
	if dir := os.Getenv("PREVIEW_SHOTS_DIR"); dir != "" && e.gpu == nil {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	return e, fs
}

// hubTab opens Directories on tab i (3 External, 4 Invitations).
func hubTab(t *testing.T, e *perfEnv, i int, loaded func(*views.Directories) bool) *views.Directories {
	t.Helper()
	e.a.Navigate("directories")
	e.settle(300 * time.Millisecond)
	v, ok := e.a.views[Route("directories")].(*views.Directories)
	if !ok {
		t.Fatalf("no Directories: %T", e.a.views[Route("directories")])
	}
	v.ShowTab(i)
	e.until("the tab's reads", 20*time.Second, func() bool { return loaded(v) })
	return v
}

func partnerCo() fakeslack.ExternalTeamFixture {
	return fakeslack.ExternalTeamFixture{ID: fakeslack.ExternalTeamID, Name: "Partner Co", Domain: "partnerco", Verified: true, PublicMemberOf: 1}
}

func lastForm(t *testing.T, fs *fakeslack.Server, method string) url.Values {
	t.Helper()
	fms := fs.SlackConnectForms(method)
	if len(fms) == 0 {
		t.Fatalf("no %s", method)
	}
	return fms[len(fms)-1]
}

func TestSlackConnectExternalLikeSlack(t *testing.T) {
	avail := []fakeslack.ExternalTeamFixture{{ID: "T0SHARES1", Name: "Shares Already", PrivateMemberOf: 2}}
	for i := 0; i < 24; i++ {
		avail = append(avail, fakeslack.ExternalTeamFixture{ID: fmt.Sprintf("T0AVAIL%02d", i), Name: fmt.Sprintf("Avail %02d", i)})
	}
	e, fs := hubEnv(t, func(fs *fakeslack.Server) {
		fs.SetSlackConnectHub(fakeslack.SlackConnectHub{Connected: []fakeslack.ExternalTeamFixture{partnerCo()}, Available: avail, DMEligible: true})
	})
	v := hubTab(t, e, 3, (*views.Directories).SCExternalLoaded)
	// the lists as useExternalTeamsList asks them
	var conn, av url.Values
	for _, f := range fs.SlackConnectForms("edge/externalTeams/list") {
		switch f.Get("filter") {
		case "connected":
			conn = f
		case "available":
			av = f
		}
	}
	if conn.Get("count") != "60" || av.Get("count") != "20" || conn.Has("marker") || av.Has("marker") {
		t.Fatalf("externalTeams/list: connected %v, available %v; want count 60 and 20, no marker", conn, av)
	}
	e.settle(500 * time.Millisecond)
	e.shot("sc-external-light")
	rows := v.SCExternalRows()
	// the banner, Your connections, the other teams (minus the one the
	// viewer already shares a channel with), Load More, then the tutorial
	want := []string{"nux-banner", "connected-teams", "connected-" + fakeslack.ExternalTeamID, "available-teams"}
	if len(rows) < len(want) || !slices.Equal(rows[:len(want)], want) {
		t.Fatalf("rows %v, want them to start %v", rows, want)
	}
	if slices.Contains(rows, "available-T0SHARES1") {
		t.Error("an available team the viewer shares a channel with is listed")
	}
	if !slices.Contains(rows, "load-more-available-orgs") || rows[len(rows)-2] != "tutorial" || rows[len(rows)-1] != "social-proof" {
		t.Fatalf("rows %v: want Load More, then the tutorial and the social proof last", rows)
	}
	// Load More: the next page by next_marker, then no Load More
	v.SCExternalLoadMore()
	e.until("the second page", 10*time.Second, func() bool {
		return v.SCExternalLoaded() && slices.Contains(v.SCExternalRows(), "available-T0AVAIL23")
	})
	if f := lastForm(t, fs, "edge/externalTeams/list"); f.Get("marker") != "etm:20" || f.Get("filter") != "available" {
		t.Errorf("Load More asked %v; want filter available, marker etm:20", f)
	}
	if slices.Contains(v.SCExternalRows(), "load-more-available-orgs") {
		t.Error("Load More after the last page")
	}
	// the banner's cards: the DM one on (eligible), the channel one off
	// without the org's invite prefs, so Contact an admin… in channels
	if ch, chOff, dmOff, contact := v.SCCTA(); !ch || !chOff || dmOff || contact != "Contact an admin to use Slack Connect in channels" {
		t.Errorf("cards: channel %v off %v, DM off %v, contact %q", ch, chOff, dmOff, contact)
	}

	// search: externalTeams/search, "Organizations", then "No results found"
	v.SCExternalSearch("partner")
	e.until("the search", 10*time.Second, func() bool {
		r := v.SCExternalRows()
		return len(r) > 0 && r[0] != "list-loading"
	})
	if r := v.SCExternalRows(); !slices.Equal(r, []string{"org-search-results-label", "search-" + fakeslack.ExternalTeamID}) {
		t.Fatalf("search rows %v", r)
	}
	if f := lastForm(t, fs, "edge/externalTeams/search"); f.Get("query") != "partner" {
		t.Errorf("externalTeams/search %v", f)
	}
	v.SCExternalSearch("zzzz")
	e.until("no results", 10*time.Second, func() bool {
		return slices.Equal(v.SCExternalRows(), []string{"search-results-empty"})
	})

	// a row opens the organization page beside the hub
	v.SCOpenTeam(fakeslack.ExternalTeamID)
	e.frame()
	o, ok := e.a.secondary.view.(*views.ExternalOrg)
	if !ok || !e.a.secondary.open() || o.TeamID() != fakeslack.ExternalTeamID {
		t.Fatalf("secondary %T (open %v)", e.a.secondary.view, e.a.secondary.open())
	}
}

// orgFixture is Partner Co's page: #th-partner-co (the viewer is in it)
// and #partner-lobby (not), with their counts.
func orgFixture(fs *fakeslack.Server) {
	lobby := slack.Conversation{ID: "C0PLOBBY1", Name: "partner-lobby", NumMembers: 5, IsExtShared: true,
		ConnectedTeamIDs: []string{fakeslack.EnterpriseID, fakeslack.ExternalTeamID, "T0THIRD01"}}
	fs.AddOtherChannel(lobby)
	fs.SetSlackConnectHub(fakeslack.SlackConnectHub{Connected: []fakeslack.ExternalTeamFixture{partnerCo()},
		Orgs: map[string]fakeslack.HubOrg{
			fakeslack.ExternalTeamID: {ChannelIDs: []string{fakeslack.ThreadSharedID, "C0PLOBBY1"}, Counts: map[string]fakeslack.HubCount{
				fakeslack.ThreadSharedID: {People: 12, ByTeam: map[string]int{fakeslack.TeamID: 9, fakeslack.ExternalTeamID: 3}},
				"C0PLOBBY1":              {People: 5, ByTeam: map[string]int{fakeslack.TeamID: 2, fakeslack.ExternalTeamID: 2, "T0THIRD01": 1}},
			}},
			"T0BROKEN1": {Fail: true},
		}})
}

func openOrg(t *testing.T, e *perfEnv, team string) *views.ExternalOrg {
	t.Helper()
	e.a.OpenExternalOrg(team)
	e.frame()
	o, ok := e.a.secondary.view.(*views.ExternalOrg)
	if !ok {
		t.Fatalf("secondary %T", e.a.secondary.view)
	}
	e.until("the org page's reads", 15*time.Second, o.OrgLoaded)
	return o
}

func TestSlackConnectOrgPageLikeSlack(t *testing.T) {
	e, fs := hubEnv(t, orgFixture)
	o := openOrg(t, e, fakeslack.ExternalTeamID)
	if f := lastForm(t, fs, "users.slackConnect.orgInfo"); f.Get("team_id") != fakeslack.ExternalTeamID || f.Get("reason") != "use-org-info" {
		t.Errorf("orgInfo %v", f)
	}
	if o.Title() != "About Partner Co" {
		t.Errorf("title %q", o.Title())
	}
	want := []string{"organization-page-header", "filter-input",
		"conversation-list-member-title", "conversation-list-member-" + fakeslack.ThreadSharedID,
		"conversation-list-nonmember-title", "conversation-list-nonmember-C0PLOBBY1"}
	if r := o.OrgRows(); !slices.Equal(r, want) {
		t.Fatalf("rows %v, want %v", r, want)
	}
	// the captions from each channel's users/counts by_team
	e.until("the captions", 10*time.Second, func() bool {
		return o.OrgCaption(fakeslack.ThreadSharedID, false) == "3 members are from Partner Co" &&
			o.OrgCaption("C0PLOBBY1", true) == "3 members are from other organizations"
	})
	if len(fs.SlackConnectForms("edge/users/search")) != 0 || len(fs.SlackConnectForms("edge/channels/search")) != 0 {
		t.Error("an empty query searched (the searcher's allowEmptyQuery is off)")
	}
	e.shot("sc-org-page-light")

	// a query: the team-scoped searches, the person (not the bot), the channels
	o.OrgSearch("partner")
	e.until("the search", 10*time.Second, o.OrgLoaded)
	r := o.OrgRows()
	for _, k := range []string{"members-list-title", "member-grid", "conversation-list-member-" + fakeslack.ThreadSharedID, "conversation-list-nonmember-C0PLOBBY1"} {
		if !slices.Contains(r, k) {
			t.Errorf("search rows %v lack %s", r, k)
		}
	}
	us := lastForm(t, fs, "edge/users/search")
	if us.Get("external_team") != fakeslack.ExternalTeamID || us.Get("filter") != "NOT deactivated AND NOT org" || us.Get("count") != "25" {
		t.Errorf("users/search %v", us)
	}
	cs := lastForm(t, fs, "edge/channels/search")
	if cs.Get("external_team") != fakeslack.ExternalTeamID || cs.Get("filter") != "xws AND shared" {
		t.Errorf("channels/search %v", cs)
	}
	_, members, err := e.a.data.core.SearchExternalOrg(t.Context(), fakeslack.ExternalTeamID, "partner", core.ExternalOrgLimit)
	if err != nil || len(members) != 1 || members[0].ID != fakeslack.ExternalID {
		t.Errorf("members %v (%v), want Sam Partner alone", members, err)
	}
	o.OrgSearch("zzzz")
	e.until("no results", 10*time.Second, func() bool { return o.OrgLoaded() && slices.Contains(o.OrgRows(), "search-no-results") })

	// a failed orgInfo: nothing to list, "No results"
	b := openOrg(t, e, "T0BROKEN1")
	if r := b.OrgRows(); !slices.Equal(r, []string{"organization-page-header", "filter-input", "search-no-results"}) {
		t.Errorf("failed page rows %v", r)
	}
}

func hubInvites() []fakeslack.HubInvite {
	now := time.Now().Unix()
	return []fakeslack.HubInvite{
		{ID: "I0SENTMAIL", Sig: "s1", Kind: "channel", Direction: "outgoing", Status: "sent", RecipientEmail: "pat@partner.example",
			ChannelID: fakeslack.ThreadSharedID, ChannelName: "th-partner-co", DateCreated: now - 3600, DateInvalid: now + 86400*14,
			InviterID: fakeslack.SelfID, InviterName: "me", TeamID: fakeslack.TeamID, TeamName: "Clack Test"},
		{ID: "I0SENTLINK", Sig: "s2", Kind: "im", Direction: "outgoing", Status: "sent", Link: "https://join.slack.com/share/x",
			DateCreated: now - 7200, DateInvalid: now + 86400*14, InviterID: fakeslack.SelfID, InviterName: "me", TeamID: fakeslack.TeamID, TeamName: "Clack Test"},
		{ID: "I0RECEIVED", Sig: "s3", Kind: "channel", Direction: "incoming", Status: "sent", ChannelName: "partner-launch",
			DateCreated: now - 600, DateInvalid: now + 86400*14, InviterID: fakeslack.ExternalID, InviterName: "Sam Partner",
			TeamID: fakeslack.ExternalTeamID, TeamName: "Partner Co", TeamVerified: true},
	}
}

func TestSlackConnectInvitationsLikeSlack(t *testing.T) {
	e, fs := hubEnv(t, func(fs *fakeslack.Server) {
		fs.SetSlackConnectHub(fakeslack.SlackConnectHub{Invites: hubInvites(), DMEligible: true})
	})
	v := hubTab(t, e, 4, (*views.Directories).SCInvitesLoaded)
	if s := v.SCInvitesState(); s != "list" {
		t.Fatalf("state %q", s)
	}
	e.settle(300 * time.Millisecond)
	e.shot("sc-invitations-light")
	f := lastForm(t, fs, "connectInvites.list")
	if f.Get("invite_types") != `["channel","im"]` || f.Get("include_invite_requests") != "true" || f.Has("direction") {
		t.Errorf("connectInvites.list %v", f)
	}
	rows := v.SCInviteRows()
	idx := func(prefix string) int {
		return slices.IndexFunc(rows, func(r string) bool { return strings.HasPrefix(r, prefix) })
	}
	sent, mail, link, recv, in := idx("Sent"), idx("I0SENTMAIL:"), idx("I0SENTLINK:"), idx("Received"), idx("I0RECEIVED:")
	if sent != 1 || mail != 2 || link != 3 || recv != 4 || in != 5 {
		t.Fatalf("rows %v: want controls, Sent, the email invite, the link (newest first), Received, the received invite", rows)
	}
	if !strings.HasSuffix(rows[mail], ":revoke") || !strings.HasSuffix(rows[link], ":copy-link,revoke") {
		t.Errorf("sent actions %q %q: an email invite revokes, a link copies and disables", rows[mail], rows[link])
	}
	if !strings.Contains(rows[in], "accept") {
		t.Errorf("received actions %q", rows[in])
	}
	// the filter
	v.SCInviteFilter(2)
	if r := v.SCInviteRows(); slices.ContainsFunc(r, func(s string) bool { return strings.HasPrefix(s, "I0SENT") }) {
		t.Errorf("Received shows %v", r)
	}
	v.SCInviteFilter(1)
	if r := v.SCInviteRows(); slices.ContainsFunc(r, func(s string) bool { return strings.HasPrefix(s, "I0RECEIVED") }) {
		t.Errorf("Sent shows %v", r)
	}
	v.SCInviteFilter(0)

	// Revoke: the modal, conversations.revokeSharedInvite, the outgoing list read again
	v.SCInviteAct("I0SENTMAIL", core.InviteActRevoke)
	e.frame()
	m := e.topModal("Revoke this invitation?")
	if m == nil {
		t.Fatal("no revoke modal")
	}
	if !m.Press("Revoke") {
		t.Fatal("no Revoke button")
	}
	e.until("the revoke", 10*time.Second, func() bool {
		inv, _ := fs.HubInviteNow("I0SENTMAIL")
		return inv.Status == "revoked" && len(fs.SlackConnectForms("connectInvites.list")) >= 2
	})
	if f := lastForm(t, fs, "conversations.revokeSharedInvite"); f.Get("invite_id") != "I0SENTMAIL" {
		t.Errorf("revoke %v", f)
	}
	if f := lastForm(t, fs, "connectInvites.list"); f.Get("direction") != "outgoing" {
		t.Errorf("after the revoke %v; want the outgoing invites", f)
	}
	e.until("the revoked row", 10*time.Second, func() bool {
		r := v.SCInviteRows()
		i := slices.IndexFunc(r, func(s string) bool { return strings.HasPrefix(s, "I0SENTMAIL:") })
		return i >= 0 && !strings.HasSuffix(r[i], ":revoke")
	})
}

func TestSlackConnectInvitationsStates(t *testing.T) {
	// nothing sent or received: the empty page
	e, fs := hubEnv(t, func(fs *fakeslack.Server) {
		fs.SetSlackConnectHub(fakeslack.SlackConnectHub{})
		fs.SetHook(func(method string, form url.Values) (any, bool) {
			if method == "connectInvites.list" && form.Has("include_invite_requests") {
				return json.RawMessage(`{"ok":false,"error":"fatal_error"}`), true
			}
			return nil, false
		})
	})
	v := hubTab(t, e, 4, (*views.Directories).SCInvitesLoaded)
	if s := v.SCInvitesState(); s != "error" {
		t.Fatalf("a failed list: state %q, want error", s)
	}
	fs.SetHook(nil)
	v.SCInvitesRetry()
	e.until("Try again", 10*time.Second, func() bool { return v.SCInvitesLoaded() && v.SCInvitesState() != "error" })
	if s := v.SCInvitesState(); s != "empty" {
		t.Errorf("no invites: state %q, want empty", s)
	}
}

// Who sees what: an org admin with no connections gets the admin CTAs; a
// member the org's who_can prefs leave out gets the channel card
// disabled and "Contact an admin…".
func TestSlackConnectHubRoles(t *testing.T) {
	lock := map[string]any{"can_create_slack_connect_channel_invite": true, "can_create_external_limited_invite": true,
		"who_can_create_slack_connect_channel_invite": map[string]any{"type": []string{"TOPLEVEL_ADMINS_AND_OWNERS"}},
		"who_can_create_external_limited_invite":      map[string]any{"type": []string{"TOPLEVEL_ADMINS_AND_OWNERS"}}}
	t.Run("member", func(t *testing.T) {
		e, _ := hubEnv(t, func(fs *fakeslack.Server) {
			fs.SetEnterpriseLockedPrefs(lock)
			fs.SetSlackConnectHub(fakeslack.SlackConnectHub{Connected: []fakeslack.ExternalTeamFixture{partnerCo()}})
		})
		e.until("the org prefs", 10*time.Second, func() bool {
			return len(e.a.data.core.EnterpriseLockedPref("who_can_create_slack_connect_channel_invite")) > 0
		})
		v := hubTab(t, e, 3, (*views.Directories).SCExternalLoaded)
		ch, chOff, dmOff, contact := v.SCCTA()
		if !ch || !chOff || !dmOff || contact != "Contact an admin to use Slack Connect" {
			t.Errorf("cards: channel %v off %v, DM off %v, contact %q", ch, chOff, dmOff, contact)
		}
		if slices.Contains(v.SCExternalRows(), "admin") {
			t.Error("admin CTAs for a member")
		}
	})
	t.Run("org admin", func(t *testing.T) {
		e, _ := hubEnv(t, func(fs *fakeslack.Server) {
			fs.SetEnterpriseLockedPrefs(lock)
			fs.SetSlackConnectHub(fakeslack.SlackConnectHub{DMEligible: true})
			fs.SetSelfEnterpriseUser(slack.EnterpriseUser{EnterpriseID: fakeslack.EnterpriseID, EnterpriseName: "Fake Org", IsAdmin: true})
		})
		e.until("the org admin", 10*time.Second, e.a.data.core.SelfOrgAdmin)
		v := hubTab(t, e, 3, (*views.Directories).SCExternalLoaded)
		r := v.SCExternalRows()
		for _, k := range []string{"admin", "customize-settings", "manage-requests", "manage-connections"} {
			if !slices.Contains(r, k) {
				t.Errorf("rows %v lack %s", r, k)
			}
		}
		if _, chOff, _, contact := v.SCCTA(); contact != "" || chOff {
			t.Errorf("an org admin: channel off %v, contact %q", chOff, contact)
		}
	})
}
