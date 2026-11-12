package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// inviteApp is an App against fakeslack with self as the viewer and the
// given team prefs.
func inviteApp(t *testing.T, self slack.User, prefs string) (*App, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	s := store.New("")
	self.ID = fakeslack.SelfID
	self.Profile.Email = "me@fakeclub.example"
	s.PutUsers([]slack.User{self, {ID: "U0OTHER001", Name: "other", Profile: slack.Profile{Email: "Other@FakeClub.example"}}})
	s.Self = self
	a := New(slack.New(fs.Creds()), s)
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(prefs), &m); err != nil {
		t.Fatal(err)
	}
	a.SetTeamPrefs(m)
	return a, fs
}

// The c1OI rules: who sees the modal, who only requests, who may pick
// Guest.
func TestInviteRules(t *testing.T) {
	member, admin := slack.User{}, slack.User{IsAdmin: true}
	guest := slack.User{IsRestricted: true}
	frontline := slack.User{RestrictedUserType: "frontline_worker"}
	for _, c := range []struct {
		name                  string
		self                  slack.User
		prefs                 string
		see, request, members bool
		guestRequest          bool
	}{
		{"open team", member, `{}`, true, false, true, true},
		{"admins only, requests on", member, `{"invites_only_admins":true,"invite_requests_enabled":true}`, true, true, false, true},
		{"admins only, no requests", member, `{"invites_only_admins":true}`, false, true, false, true},
		{"pref as a JS-truthy string", member, `{"invites_only_admins":"false","invite_requests_enabled":1}`, true, true, false, true},
		{"falsy prefs", member, `{"invites_only_admins":0,"invite_requests_enabled":""}`, true, false, true, true},
		{"admin", admin, `{"invites_only_admins":true}`, true, false, true, false},
		{"guest", guest, `{"invite_requests_enabled":true}`, false, false, false, true},
		{"frontline", frontline, `{}`, false, false, false, true},
	} {
		a, _ := inviteApp(t, c.self, c.prefs)
		if got := a.CanSeeInviteModal(); got != c.see {
			t.Errorf("%s: CanSeeInviteModal %v, want %v", c.name, got, c.see)
		}
		if got := a.IsRequestingInvites(InviteMember); got != c.request {
			t.Errorf("%s: IsRequestingInvites(member) %v, want %v", c.name, got, c.request)
		}
		if got := a.CanInviteMembers(); got != c.members {
			t.Errorf("%s: CanInviteMembers %v, want %v", c.name, got, c.members)
		}
		if got := a.IsRequestingInvites(InviteMultiChannelGuest); got != c.guestRequest {
			t.Errorf("%s: IsRequestingInvites(guest) %v, want %v", c.name, got, c.guestRequest)
		}
	}
	if a, _ := inviteApp(t, member, `{}`); !a.IsEnterprise() {
		t.Error("fakeslack's team is on Grid")
	}
}

func TestMemberIDByEmail(t *testing.T) {
	a, _ := inviteApp(t, slack.User{}, `{}`)
	if id := a.MemberIDByEmail(" other@fakeclub.EXAMPLE "); id != "U0OTHER001" {
		t.Errorf("by email: %q", id)
	}
	if id := a.MemberIDByEmail("nobody@fakeclub.example"); id != "" {
		t.Errorf("unknown: %q", id)
	}
}

// A direct invite: users.admin.inviteBulk with Slack's arguments, one
// result per address.
func TestSendInvitesDirect(t *testing.T) {
	a, fs := inviteApp(t, slack.User{}, `{}`)
	res, err := a.SendInvites(context.Background(), InviteOpts{
		Emails: []string{"new@x.example", "already@x.example", "fail@x.example"}, Channels: []string{"C1", "C2"},
		Message: "come along", Source: "team_menu"})
	if err != nil {
		t.Fatal(err)
	}
	calls := fs.InviteCalls()
	if len(calls) != 1 || calls[0].Method != "users.admin.inviteBulk" {
		t.Fatalf("calls %+v", calls)
	}
	f := calls[0].Form
	want := map[string]string{
		"invites":          `[{"email":"new@x.example","mode":"manual","type":"regular"},{"email":"already@x.example","mode":"manual","type":"regular"},{"email":"fail@x.example","mode":"manual","type":"regular"}]`,
		"team_id":          fakeslack.TeamID,
		"restricted":       "false",
		"ultra_restricted": "false",
		"campaign":         "team_menu",
		"channels":         "C1,C2",
		"extra_message":    "come along",
	}
	for k, v := range want {
		if got := f.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if f.Has("requester_message") || f.Has("requests") {
		t.Errorf("request args on an invite: %v", f)
	}
	if len(res) != 3 || !res[0].OK || res[1].OK || res[1].Error != "already_in_team" || res[2].Error != "invite_failed" || res[0].Requested {
		t.Errorf("results %+v", res)
	}
	// a guest: restricted
	if _, err := a.SendInvites(context.Background(), InviteOpts{Emails: []string{"g@x.example"}, Type: InviteMultiChannelGuest, Channels: []string{"C1"}}); err != nil {
		t.Fatal(err)
	}
	if calls := fs.InviteCalls(); calls[1].Method != "users.inviteRequests.create" {
		// a member who can't invite guests asks an admin
		t.Errorf("guest invite from a member went to %s", calls[1].Method)
	} else if f := calls[1].Form; f.Get("restricted") != "true" || f.Get("ultra_restricted") != "false" ||
		f.Get("requests") != `[{"email":"g@x.example","mode":"manual","type":"restricted"}]` {
		t.Errorf("guest request form %v", f)
	}
	fs.SetInviteError("invite_limit_reached")
	_, err = a.SendInvites(context.Background(), InviteOpts{Emails: []string{"n@x.example"}})
	var ie *InviteError
	if !errors.As(err, &ie) || ie.Code != "invite_limit_reached" || len(ie.Results) != 0 {
		t.Errorf("whole-call failure: %v", err)
	}
}

// A request for an admin: users.inviteRequests.create, requester_message,
// every address Requested; a failure lists each request.
func TestSendInvitesRequest(t *testing.T) {
	a, fs := inviteApp(t, slack.User{}, `{"invites_only_admins":true,"invite_requests_enabled":true}`)
	res, err := a.SendInvites(context.Background(), InviteOpts{Emails: []string{"a@x.example", "b@x.example"}, Message: "they run the club", Source: "huddles_page"})
	if err != nil {
		t.Fatal(err)
	}
	calls := fs.InviteCalls()
	if len(calls) != 1 || calls[0].Method != "users.inviteRequests.create" {
		t.Fatalf("calls %+v", calls)
	}
	f := calls[0].Form
	if f.Get("requester_message") != "they run the club" || f.Has("extra_message") || f.Get("campaign") != "huddles_page" ||
		f.Get("requests") != `[{"email":"a@x.example","mode":"manual","type":"regular"},{"email":"b@x.example","mode":"manual","type":"regular"}]` {
		t.Errorf("form %v", f)
	}
	if len(res) != 2 || !res[0].OK || !res[0].Requested || res[1].Email != "b@x.example" {
		t.Errorf("results %+v", res)
	}
	fs.SetInviteError("request_failed")
	_, err = a.SendInvites(context.Background(), InviteOpts{Emails: []string{"c@x.example"}})
	var ie *InviteError
	if !errors.As(err, &ie) || ie.Code != "request_failed" || len(ie.Results) != 1 || ie.Results[0].Email != "c@x.example" || ie.Results[0].OK {
		t.Errorf("failure %v %+v", err, ie)
	}
}

func TestInviteLink(t *testing.T) {
	a, fs := inviteApp(t, slack.User{}, `{}`)
	ctx := context.Background()
	if _, ok, err := a.CurrentInviteLink(ctx); err != nil || ok {
		t.Fatalf("no link yet: %v %v", ok, err)
	}
	l, err := a.CreateInviteLink(ctx)
	if err != nil || l.URL == "" || l.Code == "" {
		t.Fatalf("create: %+v %v", l, err)
	}
	cur, ok, err := a.CurrentInviteLink(ctx)
	if err != nil || !ok || cur.Code != l.Code || cur.ShareURL() != l.URL {
		t.Errorf("current %+v %v %v", cur, ok, err)
	}
	if d := cur.DaysLeft(time.Now()); d < 29 || d > 30 {
		t.Errorf("days left %d", d)
	}
	if d := (SharedInviteLink{}).DaysLeft(time.Now()); d != 366 {
		t.Errorf("no expiry: %d", d)
	}
	calls := fs.InviteCalls()
	if len(calls) != 3 || calls[0].Form.Get("mode") != "last_active_for_user" ||
		calls[1].Method != "users.admin.createSharedInvite" || calls[1].Form.Get("expiration") != "30" || calls[1].Form.Get("max_signups") != "400" {
		t.Errorf("calls %+v", calls)
	}
	fs.SetInviteLinkError("not_allowed")
	if _, _, err := a.CurrentInviteLink(ctx); err == nil {
		t.Error("list error not reported")
	}
}
