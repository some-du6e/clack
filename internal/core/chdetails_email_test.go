package core

// W13 (docs/GAPS-2026-11-11.md gap 14): the call apps, calls.request and
// the channel email address methods against internal/fakeslack (nothing
// is dialled or mailed). Slack's cites are in chdetails_email.go.

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

const (
	w13Chan = "C0W13CHAN1"
	w13DM   = "D0W13DM001"
	w13Peer = "U0W13PEER1"
)

func w13App(t *testing.T, prefs string) (*App, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	s := store.New("")
	self := slack.User{ID: fakeslack.SelfID, Name: "me"}
	s.PutUsers([]slack.User{self, {ID: w13Peer, Name: "peer", Profile: slack.Profile{Phone: "+1 555 0100"}}})
	s.Self = self
	s.PutConv(slack.Conversation{ID: w13Chan, Name: "w13", IsChannel: true, IsMember: true, NumMembers: 4})
	s.PutConv(slack.Conversation{ID: w13DM, IsIM: true, User: w13Peer})
	cr := fs.Creds() // off Grid: the plan checks read team.info's plan alone
	cr.EnterpriseID, cr.EnterpriseToken, cr.EnterpriseURL = "", "", ""
	a := New(slack.New(cr), s)
	t.Cleanup(a.Shutdown)
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(prefs), &m); err != nil {
		t.Fatal(err)
	}
	a.SetTeamPrefs(m)
	return a, fs
}

const w13Apps = `{"allow_calls":true,"calls_apps":{"video":[{"id":"A00","name":"Slack"},{"id":"A5GE9BMQC","name":"Zoom","image":"https://x.example/zoom.png"},` +
	`{"id":"AMEET0001","name":"Meet","image":"https://x.example/meet.png"}],"audio":[{"id":"ADIAL0001","name":"Dialpad"}]}}`

// getFlattenTeamPrefCallsApps drops A00; channels get no audio apps;
// getCallAppList adds the audio ones only with a valid phone; the Zoom
// ids draw Slack's zoom icon.
func TestCallAppsLikeSlack(t *testing.T) {
	a, _ := w13App(t, w13Apps)
	if n := a.NumCallApps(); n != 3 {
		t.Fatalf("NumCallApps = %d, want 3 (A00 dropped)", n)
	}
	if !a.CallsAllowed() || !a.ThirdPartyCallEnabled() {
		t.Fatal("allow_calls / third party not read")
	}
	ch := a.CallsAppsFor(w13Chan)
	if len(ch.Video) != 2 || len(ch.Audio) != 0 {
		t.Fatalf("channel apps %+v, want 2 video and no audio", ch)
	}
	dm := a.CallsAppsFor(w13DM)
	if len(dm.Audio) != 1 {
		t.Fatalf("DM apps %+v, want the audio app", dm)
	}
	got := func(l []CallApp) string {
		var s []string
		for _, c := range l {
			s = append(s, c.ID+"/"+c.Type)
		}
		return strings.Join(s, " ")
	}
	if g := got(CallAppList(dm, a.MemberPhone(w13Peer), false)); g != "A5GE9BMQC/video AMEET0001/video ADIAL0001/audio" {
		t.Errorf("DM list %q", g)
	}
	if g := got(CallAppList(dm, "-", false)); g != "A5GE9BMQC/video AMEET0001/video" {
		t.Errorf(`phone "-" list %q`, g)
	}
	if g := got(CallAppList(dm, "+1", true)); g != "ADIAL0001/audio" {
		t.Errorf("hideVideoApps list %q", g)
	}
	if img := CallAppImage(dm.Video[0]); img != zoomIcon {
		t.Errorf("Zoom image %q", img)
	}
	if img := CallAppImage(dm.Video[1]); img != "https://x.example/meet.png" {
		t.Errorf("Meet image %q", img)
	}
	b, _ := w13App(t, `{}`)
	if b.NumCallApps() != 0 || b.CallsAllowed() || b.ThirdPartyCallEnabled() {
		t.Error("no prefs: no apps, calls not allowed")
	}
	if !b.CanCallMember(w13Peer, true) || b.CanCallMember(w13Peer, false) || b.CanCallMember(fakeslack.SelfID, true) {
		t.Error("canCallMember: huddles or calls allowed, not yourself")
	}
}

// startPlatformCall: calls.request {channel, app, type, phone_number for
// audio}; a failure posts Slackbot's message in the conversation.
func TestStartCallRequest(t *testing.T) {
	a, fs := w13App(t, w13Apps)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := a.StartCall(ctx, w13Chan, "AMEET0001", CallVideo, "+1 555 0100")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.URL, "fake-call/AMEET0001") || r.DesktopProtocolURL == "" {
		t.Fatalf("answer %+v", r)
	}
	if _, err := a.StartCall(ctx, w13DM, "ADIAL0001", CallAudio, "+1 555 0100"); err != nil {
		t.Fatal(err)
	}
	calls := fs.CallsEmailCalls()
	if len(calls) != 2 {
		t.Fatalf("calls %+v", calls)
	}
	v := calls[0].Form
	if v.Get("channel") != w13Chan || v.Get("app") != "AMEET0001" || v.Get("type") != "video" || v.Has("phone_number") {
		t.Errorf("video call form %v (no phone_number for video)", v)
	}
	if calls[1].Form.Get("phone_number") != "+1 555 0100" || calls[1].Form.Get("type") != "audio" {
		t.Errorf("audio call form %v", calls[1].Form)
	}
	if _, err := a.StartCall(ctx, w13Chan, CallAppLegacy, CallVideo, ""); err == nil || len(fs.CallsEmailCalls()) != 2 {
		t.Error("A00 must not call calls.request")
	}
	if got := (StartCallResult{URL: "https://z.example/j/1?pwd=a b"}).OpenURL("AMEET0001"); got != "https://z.example/j/1?pwd=a%20b" {
		t.Errorf("encodeURI: %q", got)
	}
	if got := (StartCallResult{URL: "https://z.example/a b"}).OpenURL("AD6B97QES"); got != "https://z.example/a b" {
		t.Errorf("raw app URL: %q", got)
	}

	evs, stop := a.Subscribe()
	defer stop()
	fs.SetCallsEmailError("calls.request", "app_not_installed")
	if _, err := a.StartCall(ctx, w13Chan, "AMEET0001", CallVideo, ""); err == nil {
		t.Fatal("the error didn't come back")
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-evs:
			if ev.Type == "message" && ev.Channel == w13Chan && ev.Msg != nil {
				if !strings.Contains(ev.Msg.Text, "Darn, that didn’t work.") || ev.UserID != "USLACKBOT" {
					t.Fatalf("Slackbot's message %q from %s", ev.Msg.Text, ev.UserID)
				}
				return
			}
		case <-deadline:
			t.Fatal("no Slackbot message after the failed call")
		}
	}
}

// getChannelEmailAddress, create, update and delete, and the free-plan
// rejection.
func TestChannelEmailAddresses(t *testing.T) {
	a, fs := w13App(t, `{}`)
	a.setTeamPlan("std", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	e := a.GetChannelEmail(ctx, w13Chan, false)
	if e.Err || e.Address != "" {
		t.Fatalf("info without an address: %+v", e)
	}
	if _, ok := a.ChannelEmailCached(w13Chan); ok {
		t.Error("an empty answer was kept")
	}
	if err := a.DeleteChannelEmail(ctx, w13Chan, "x@y"); err == nil {
		t.Error("delete without a kept address must fail")
	}
	e = a.GetChannelEmail(ctx, w13Chan, true)
	if e.Err || e.Address == "" || !e.CreatedNew {
		t.Fatalf("create: %+v", e)
	}
	kept, ok := a.ChannelEmailCached(w13Chan)
	if !ok || kept.Address != e.Address {
		t.Fatalf("not kept: %+v", kept)
	}
	if !IsDefaultEmailIcon(kept.Icons.Image()) {
		t.Errorf("new address icon %q", kept.Icons.Image())
	}
	again := a.GetChannelEmail(ctx, w13Chan, true)
	if again.CreatedNew || again.Address != e.Address {
		t.Errorf("second get made another: %+v", again)
	}
	u, err := a.UpdateChannelEmail(ctx, w13Chan, EmailUpdate{Name: "Alerts", Address: e.Address, Emoji: ":bell:"})
	if err != nil || u.Name != "Alerts" || u.Icons.Emoji != ":bell:" {
		t.Fatalf("update: %+v %v", u, err)
	}
	var upd []string
	for _, c := range fs.CallsEmailCalls() {
		upd = append(upd, c.Method)
		if c.Method == "conversations.emailaddresses.update" {
			if c.Form.Get("conversation_id") != w13Chan || c.Form.Get("emoji") != ":bell:" || c.Form.Has("avatar_id") || c.Form.Has("use_default_icon") {
				t.Errorf("update form %v", c.Form)
			}
		}
	}
	if want := "conversations.emailaddresses.info conversations.emailaddresses.info conversations.emailaddresses.create conversations.emailaddresses.info conversations.emailaddresses.update"; strings.Join(upd, " ") != want {
		t.Errorf("calls\n got %s\nwant %s", strings.Join(upd, " "), want)
	}
	if err := a.DeleteChannelEmail(ctx, w13Chan, e.Address); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.ChannelEmailCached(w13Chan); ok {
		t.Error("delete kept the address")
	}

	free, _ := w13App(t, `{}`)
	free.setTeamPlan("", "")
	if r := free.GetChannelEmail(ctx, w13Chan, true); !r.Err {
		t.Errorf("free team: %+v, want isError", r)
	}
	ic, err := a.UploadEmailIcon(ctx, "icon.png", []byte("\x89PNG fake"))
	if err != nil || ic.ID == "" || ic.URL == "" {
		t.Fatalf("avatars.upload: %+v %v", ic, err)
	}
	if _, err := a.CropEmailIcon(ctx, ic.ID, "1,2,30"); err != nil {
		t.Fatal(err)
	}
	last := fs.CallsEmailCalls()
	if f := last[len(last)-1].Form; f.Get("avatar") != ic.ID || f.Get("cropbox") != "1,2,30" {
		t.Errorf("avatars.crop form %v", f)
	}
}

// showChannelEmailAddress, the upgrade prompt and the org check.
func TestChannelEmailVisibility(t *testing.T) {
	a, fs := w13App(t, `{}`)
	fs.Permissions = map[string][]string{w13Chan: {PermCreateChannelEmailAddresses}, "": {}}
	a.setTeamPlan("std", "")
	deadline := time.Now().Add(5 * time.Second)
	for !a.ShowChannelEmail(w13Chan) {
		if time.Now().After(deadline) {
			t.Fatal("paid team with the grant: no emails field")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if a.ChannelEmailUpgradePrompt(w13Chan) {
		t.Error("a paid team got the upgrade prompt")
	}
	if !a.ChannelEmailEnabledByOrg() {
		t.Error("off Grid the org check passes")
	}
	a.SetTeamPrefs(map[string]json.RawMessage{"disable_email_ingestion": json.RawMessage("true")})
	if a.ShowChannelEmail(w13Chan) {
		t.Error("disable_email_ingestion still shows it")
	}
	a.SetTeamPrefs(map[string]json.RawMessage{})
	a.setTeamPlan("", "")
	if a.ShowChannelEmail(w13Chan) || !a.ChannelEmailUpgradePrompt(w13Chan) {
		t.Error("free team: the upgrade prompt instead")
	}
	a.setTeamPlan("enterprise", "E0FAKE00001")
	if a.ChannelEmailEnabledByOrg() {
		t.Error("Grid without channel_email_addresses_enabled")
	}
	a.setEnterprisePrefs(map[string]json.RawMessage{"org_prefs": json.RawMessage(`{"channel_email_addresses_enabled":true}`)})
	if !a.ChannelEmailEnabledByOrg() {
		t.Error("Grid with the org pref")
	}
	if got := EmailCropBox(10, 20, 100, 230, 460, 500, 1000); got != "22,43,217" {
		t.Errorf("cropbox %q", got)
	}
	if got := EmailCropBox(10, 20, 36, 200, 100, 100, 50); got != "10,20,36" {
		t.Errorf("small image cropbox %q (scale at least 1)", got)
	}
}
