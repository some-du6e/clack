package views

// Channel details against a synthetic API (httptest; nothing talks to
// Slack): the Settings sections Slack's AdminChannelModal shows and the
// writes behind them, maybeLeaveChannel's last-member archive, the DM add
// people modal (conversations.createChild), VIP / Hide, and the Members
// tab's presence set.

import (
	"encoding/json"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// cdTestCall is one API call the synthetic server saw.
type cdTestCall struct {
	method string
	form   url.Values
	edge   map[string]any
}

type cdTestServer struct {
	mu      sync.Mutex
	calls   []cdTestCall
	people  int    // users/counts people
	allowed string // permissions.info allowed (JSON list)
	props   string // conversations.info properties (JSON)
	org     string // enterprise.prefs.get org_prefs (JSON)
	canSet  string // conversations.canSetProperties props (JSON list; "" the default)
	// recorded, when set, runs after newW14Env's extra answered a call
	// and it was recorded
	recorded func(cdTestCall)
}

func (s *cdTestServer) seen(method string) []cdTestCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []cdTestCall
	for _, c := range s.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

func (s *cdTestServer) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	call := cdTestCall{method: path}
	if rest, ok := strings.CutPrefix(r.URL.Path, "/edge/"); ok {
		_, p, _ := strings.Cut(rest, "/")
		call.method = "edge/" + p
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &call.edge)
	} else {
		_ = r.ParseForm()
		call.form = r.Form
	}
	s.mu.Lock()
	s.calls = append(s.calls, call)
	people, allowed, props, org, canSet := s.people, s.allowed, s.props, s.org, s.canSet
	s.mu.Unlock()
	if org == "" {
		org = "{}"
	}
	if allowed == "" {
		allowed = "[]"
	}
	if props == "" {
		props = "{}"
	}
	write := func(v string) { _, _ = w.Write([]byte(v)) }
	switch call.method {
	case "conversations.info":
		switch call.form.Get("channel") {
		case "C1":
			write(`{"ok":true,"channel":{"id":"C1","name":"general-ish","is_channel":true,"is_member":true,"num_members":3,"properties":` + props + `}}`)
		case "C9":
			write(`{"ok":true,"channel":{"id":"C9","name":"secret","is_channel":true,"is_private":true,"is_member":true,"num_members":1}}`)
		case "C8": // archived (details_managing_test.go)
			write(`{"ok":true,"channel":{"id":"C8","name":"old-project","is_channel":true,"is_member":true,"is_archived":true,"num_members":2}}`)
		case "C7": // shared with another org, hosted here (details_managing_test.go)
			write(`{"ok":true,"channel":{"id":"C7","name":"partners","is_channel":true,"is_member":true,"is_ext_shared":true,"num_members":2}}`)
		case "D1":
			write(`{"ok":true,"channel":{"id":"D1","is_im":true,"user":"U1"}}`)
		case "G9":
			write(`{"ok":true,"channel":{"id":"G9","name":"mpdm-self--alice--bob-1","is_mpim":true,"is_member":true,"members":["USELF","U1","U2"]}}`)
		default:
			write(`{"ok":false,"error":"channel_not_found"}`)
		}
	case "edge/users/counts":
		b, _ := json.Marshal(map[string]any{"ok": true, "counts": map[string]int{"people": people, "everyone": people}})
		_, _ = w.Write(b)
	case "edge/users/list":
		write(`{"ok":true,"results":[{"id":"USELF","name":"self","real_name":"Test Self","profile":{"email":"self@example.test"}},` +
			`{"id":"U2","name":"bob","real_name":"Bob Example","profile":{"email":"bob@example.test"}}],"next_marker":""}`)
	case "permissions.info":
		write(`{"ok":true,"allowed":` + allowed + `}`)
	case "conversations.permissions.accountTypes.list":
		write(`{"ok":true,"account_types":[{"account_type":"FULL_MEMBER","permissions":[` +
			`{"permission":"ARCHIVE_CHANNEL","is_configurable":true,"is_allowed":true,"localized_permission_display_name":"Archive the channel"},` +
			`{"permission":"INVITE_TO_CHANNEL","is_configurable":false,"is_allowed":false,"localized_permission_display_name":"Add people"}]},` +
			`{"account_type":"MULTI_CHANNEL_GUEST","permissions":[]}]}`)
	case "conversations.canSetProperties":
		if canSet != "" {
			write(`{"ok":true,"props":` + canSet + `}`)
			break
		}
		write(`{"ok":true,"props":[{"prop":"restrict_guests","can_set":false,"reason":"scg_present"},{"prop":"restrict_message_and_file_sharing","can_set":false,"reason":"disallowed_action"}]}`)
	case "enterprise.prefs.get":
		write(`{"ok":true,"org_prefs":` + org + `,"locked_prefs":{},"unlocked_prefs":{}}`)
	case "conversations.createChild":
		write(`{"ok":true,"channel":{"id":"G9","is_mpim":true,"members":["USELF","U1","U2"]}}`)
	default:
		write(`{"ok":true}`)
	}
}

type cdTestHost struct {
	app  *core.App
	th   *theme.Theme
	mu   sync.Mutex
	nav  []string
	tsts []string
}

func (h *cdTestHost) Core() *core.App                           { return h.app }
func (h *cdTestHost) Theme() *theme.Theme                       { return h.th }
func (h *cdTestHost) Images() *imgcache.Cache                   { return nil }
func (h *cdTestHost) Invalidate()                               {}
func (h *cdTestHost) OpenThread(string, string)                 {}
func (h *cdTestHost) CloseSecondary()                           {}
func (h *cdTestHost) OpenProfile(image.Point, string)           {}
func (h *cdTestHost) OpenMessageMenu(image.Point, core.Message) {}
func (h *cdTestHost) OpenLightbox(richtext.Image, core.Message) {}
func (h *cdTestHost) Navigate(r string) {
	h.mu.Lock()
	h.nav = append(h.nav, r)
	h.mu.Unlock()
}
func (h *cdTestHost) Toast(s string) {
	h.mu.Lock()
	h.tsts = append(h.tsts, s)
	h.mu.Unlock()
}

// cdTestEnv is one dialog laid out at 1440×900 frame after frame.
type cdTestEnv struct {
	t   *testing.T
	srv *cdTestServer
	h   *cdTestHost
	d   *ChannelDetails
	r   input.Router
	ops op.Ops
}

func newCDTestEnv(t *testing.T, srv *cdTestServer, ch string) *cdTestEnv {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(srv.handler))
	t.Cleanup(ts.Close)
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test", UserID: "USELF", Team: "T1", EnterpriseID: "E1",
		TeamURL: ts.URL + "/", EnterpriseURL: ts.URL + "/"})
	c.EdgeURL = ts.URL + "/edge/"
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName = "T1", "Synthetic Club"
	s.Unlock()
	s.PutUsers([]slack.User{
		{ID: "USELF", Name: "self", RealName: "Test Self"},
		{ID: "U1", Name: "alice", RealName: "Alice Example"},
		{ID: "U2", Name: "bob", RealName: "Bob Example"},
	})
	s.PutConv(slack.Conversation{ID: "C1", Name: "general-ish", IsChannel: true, IsMember: true, NumMembers: 3})
	s.PutConv(slack.Conversation{ID: "C9", Name: "secret", IsChannel: true, IsPrivate: true, IsMember: true, NumMembers: 1})
	s.PutConv(slack.Conversation{ID: "D1", IsIM: true, User: "U1", IsMember: true})
	app := core.New(c, s)
	if u, ok := s.User("USELF"); ok {
		app.S.Self = *u
	}
	app.ServeOffline()
	th := theme.New(false)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	h := &cdTestHost{app: app, th: th}
	e := &cdTestEnv{t: t, srv: srv, h: h}
	e.d = NewChannelDetails(h, ch)
	return e
}

func (e *cdTestEnv) frame() {
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Source: e.r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1440, 900)), Now: time.Now()}
	e.d.Layout(gtx)
	e.r.Frame(&e.ops)
}

// until lays out frames until cond holds (10 s at most).
func (e *cdTestEnv) until(what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		e.frame()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *cdTestEnv) loaded() bool { _, ok, _ := e.d.info.get(); return ok }

// scrollTo brings a Settings section's field group into the list's view
// (each section is a 16px gap and its card).
func (e *cdTestEnv) scrollTo(sec string) {
	inf, _, _ := e.d.info.get()
	i := slices.Index(e.d.settingsSections(inf), sec)
	if i < 0 {
		e.t.Fatalf("no %s section", sec)
	}
	e.d.list(cdSettings).ScrollTo(2 * i)
	e.frame()
}

// Settings for a member of a public channel, with the experiments the test
// account had: Slack's section order, AI use and Guest access read from
// properties and canSetProperties, Member permissions from accountTypes.
func TestDetailsSettingsSections(t *testing.T) {
	srv := &cdTestServer{people: 3, allowed: `["RENAME_CHANNEL","EDIT_CHANNEL_AI_EXCLUSIONS"]`, props: `{"exclude_from_slack_ai":true}`}
	e := newCDTestEnv(t, srv, "C1")
	for _, x := range []string{"ai_channel_exclusions", "enable_new_channel_settings", "move_channel_tabs_settings"} {
		e.h.app.SetExperimentGroup(x, "on")
	}
	e.until("info", e.loaded)
	e.d.ShowTab("Settings")
	if e.d.Tab() != "Settings" {
		t.Fatalf("no Settings tab for a channel member: %q", e.d.Tab())
	}
	e.until("settings reads", func() bool {
		_, p, _ := e.d.x.perms.get()
		_, a, _ := e.d.x.acct.get()
		_, g, _ := e.d.x.canProps.get()
		return p && a && g
	})
	inf, _, _ := e.d.info.get()
	want := []string{cdSecRename, cdSecPermissions, cdSecTabPermissions, cdSecChannelPerms, cdSecMemberSuggest, cdSecCopyMembers}
	if got := e.d.settingsSections(inf); !slices.Equal(got, want) {
		t.Errorf("sections %q, want %q", got, want)
	}
	if !inf.Props.ExcludeFromAI {
		t.Error("properties.exclude_from_slack_ai not read")
	}
	if st, ok := e.d.guestAccessState(); !ok || st.CanSet || st.Reason != "scg_present" {
		t.Errorf("guest access state %+v %v, want read-only (scg_present)", st, ok)
	}
	if c := srv.seen("permissions.info"); len(c) == 0 || c[0].form.Get("channel_id") != "C1" || !strings.Contains(c[0].form.Get("permissions"), "EDIT_CHANNEL_AI_EXCLUSIONS") {
		t.Errorf("permissions.info form %v", c)
	}

	// AI use: "Allow" opens AiExclusionsModal; its button sets the property
	e.d.x.aiRow.Click()
	e.frame()
	if e.d.x.dlg.kind != cdDlgAIUse {
		t.Fatalf("AI use opened dialog %d", e.d.x.dlg.kind)
	}
	e.d.x.dlg.ok.Click()
	e.until("conversations.setProperties", func() bool { return len(srv.seen("conversations.setProperties")) > 0 })
	f := srv.seen("conversations.setProperties")[0].form
	if f.Get("channel_id") != "C1" || f.Get("props") != `{"exclude_from_slack_ai":false}` {
		t.Errorf("setProperties form %v", f)
	}

	// Channel member suggestions: unchecking puts the channel in the pref
	e.scrollTo(cdSecMemberSuggest)
	e.d.x.suggest.Click()
	e.until("users.prefs.set", func() bool { return len(srv.seen("users.prefs.set")) > 0 })
	pf := srv.seen("users.prefs.set")[0].form
	if pf.Get("name") != cdMemberSuggestPref || pf.Get("value") != `{"C1":true}` {
		t.Errorf("member suggestions pref form %v", pf)
	}
}

// Member permissions: the allowed ones listed; Edit (for whoever may manage
// channel permissions) saves only what changed.
func TestDetailsMemberPermissionsSave(t *testing.T) {
	srv := &cdTestServer{people: 3, allowed: `["MANAGE_CHANNEL_PERMISSIONS"]`}
	e := newCDTestEnv(t, srv, "C1")
	e.until("info", e.loaded)
	e.d.ShowTab("Settings")
	e.until("account types", func() bool { _, ok, _ := e.d.x.acct.get(); _, p, _ := e.d.x.perms.get(); return ok && p })
	e.scrollTo(cdSecChannelPerms)
	e.d.x.memPermRow.Click()
	e.frame()
	if e.d.x.dlg.kind != cdDlgPerms || e.d.x.dlg.acct != core.AccountFullMember {
		t.Fatalf("Member permissions opened %d %q", e.d.x.dlg.kind, e.d.x.dlg.acct)
	}
	dirClick(e.d.x.dlg.checks, "ARCHIVE_CHANNEL").Click()
	e.frame()
	e.d.x.dlg.ok.Click()
	e.until("accountTypes.set", func() bool { return len(srv.seen("conversations.permissions.accountTypes.set")) > 0 })
	f := srv.seen("conversations.permissions.accountTypes.set")[0].form
	if f.Get("account_type") != core.AccountFullMember || f.Get("permissions") != `[{"permission":"ARCHIVE_CHANNEL","is_allowed":false}]` {
		t.Errorf("accountTypes.set form %v", f)
	}
}

// 1:1 DMs have no Settings sections, so no Settings tab (isSettingsVisible).
func TestDetailsDMHasNoSettings(t *testing.T) {
	e := newCDTestEnv(t, &cdTestServer{}, "D1")
	if slices.Contains(e.d.tabKinds, cdSettings) {
		t.Errorf("a DM shows Settings: %v", e.d.tabKinds)
	}
}

// maybeLeaveChannel: the last member of a private channel gets "Leave and
// archive"; its button archives the channel.
func TestDetailsLastMemberLeaveArchives(t *testing.T) {
	srv := &cdTestServer{people: 1}
	e := newCDTestEnv(t, srv, "C9")
	e.until("info", e.loaded)
	e.d.leaveRow.Click()
	e.until("archive dialog", func() bool { return e.d.x.dlg.kind == cdDlgLeaveArchive })
	if n := len(srv.seen("edge/users/counts")); n == 0 {
		t.Error("no fresh member count before deciding")
	}
	e.d.x.dlg.ok.Click()
	e.until("conversations.archive", func() bool { return len(srv.seen("conversations.archive")) > 0 })
	if len(srv.seen("conversations.leave")) != 0 {
		t.Error("the last member's leave also called conversations.leave")
	}
	e.until("closed", func() bool { return e.d.closed })
}

// Another member left in a private channel: LeaveChannel's confirmation.
func TestDetailsPrivateLeaveConfirms(t *testing.T) {
	srv := &cdTestServer{people: 4}
	e := newCDTestEnv(t, srv, "C9")
	e.until("info", e.loaded)
	e.d.leaveRow.Click()
	e.until("leave dialog", func() bool { return e.d.x.dlg.kind == cdDlgLeave })
	e.d.x.dlg.ok.Click()
	e.until("conversations.leave", func() bool { return len(srv.seen("conversations.leave")) > 0 })
}

// "Add people to this conversation" in a DM: MpdmAddPeopleModal picks
// people, then history (From today by default); Done is
// conversations.createChild and opens the new group DM.
func TestDetailsDMAddPeopleCreatesChild(t *testing.T) {
	srv := &cdTestServer{}
	e := newCDTestEnv(t, srv, "D1")
	e.until("info", e.loaded)
	e.d.addPeople.Click()
	e.frame()
	if e.d.x.dlg.kind != cdDlgAddPeople || e.d.x.dlg.add == nil {
		t.Fatalf("Add people opened %d", e.d.x.dlg.kind)
	}
	a := e.d.x.dlg.add
	if !a.members["U1"] || !a.members["USELF"] || a.addRoom() != cdMaxGroupDM-2 {
		t.Errorf("members %v, room %d", a.members, a.addRoom())
	}
	a.picked = append(a.picked, core.User{ID: "U2", Name: "bob", RealName: "Bob Example"})
	e.d.x.dlg.ok.Click() // Next
	e.frame()
	if a.step != 1 || a.hist != cdHistToday {
		t.Fatalf("step %d hist %d, want the history step on From today", a.step, a.hist)
	}
	now := time.Now()
	y, m, dd := now.Date()
	day := time.Date(y, m, dd, 0, 0, 0, 0, now.Location()).Unix()
	e.d.x.dlg.ok.Click() // Done
	e.until("conversations.createChild", func() bool { return len(srv.seen("conversations.createChild")) > 0 })
	f := srv.seen("conversations.createChild")[0].form
	if f.Get("channel") != "D1" || f.Get("user_ids") != "U2" || f.Get("migrate_history_start_ts") != cdUnixDay(day) {
		t.Errorf("createChild form %v (day %d)", f, day)
	}
	e.until("navigated", func() bool {
		e.h.mu.Lock()
		defer e.h.mu.Unlock()
		return slices.Contains(e.h.nav, "c/G9")
	})
	for _, s := range e.h.tsts {
		if strings.Contains(s, "supported") {
			t.Errorf("toast %q", s)
		}
	}
}

// The 1:1 header's VIP is users.priority.add; Hide asks first, then closes
// the DM and saves hidden_users.
func TestDetailsDMVIPAndHide(t *testing.T) {
	srv := &cdTestServer{}
	e := newCDTestEnv(t, srv, "D1")
	e.until("info", e.loaded)
	e.d.vip.Click()
	e.until("users.priority.add", func() bool { return len(srv.seen("users.priority.add")) > 0 })
	if u := srv.seen("users.priority.add")[0].form.Get("user"); u != "U1" {
		t.Errorf("VIP for %q", u)
	}
	e.d.hide.Click()
	e.frame()
	if e.d.x.dlg.kind != cdDlgHide || e.d.x.dlg.userID != "U1" {
		t.Fatalf("Hide opened %d for %q", e.d.x.dlg.kind, e.d.x.dlg.userID)
	}
	e.d.x.dlg.ok.Click()
	e.until("hidden_users", func() bool {
		for _, c := range srv.seen("users.prefs.set") {
			if c.form.Get("name") == "hidden_users" && c.form.Get("value") == "U1" {
				return true
			}
		}
		return false
	})
	if len(srv.seen("conversations.close")) == 0 {
		t.Error("Hide didn't close the DM (hideUserById)")
	}
}

// The Members tab keeps the loaded members for the presence watcher.
func TestDetailsMembersPresenceSet(t *testing.T) {
	e := newCDTestEnv(t, &cdTestServer{people: 2}, "C1")
	e.until("info", e.loaded)
	e.d.ShowTab("Members")
	e.until("members", func() bool { ids := e.d.presIDs.Load(); return ids != nil && (*ids)["U2"] })
}
