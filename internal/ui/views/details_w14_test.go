package views

// W14 against the synthetic API (httptest; nothing talks to Slack):
// Settings › Posting permissions (ViewPostingPermissions, its subtitle and
// PostingPermissionsModal's channels.prefs.set), "Add an App" opening the
// apps browser and adding an app, and Custom history in the DM add
// people modal (the first message's date, the preview from the picked
// day, createChild's start, the too_many_messages speed bump).

import (
	"encoding/json"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// w14Host is cdTestHost with overlays (popups.Host).
type w14Host struct {
	*cdTestHost
	mu       sync.Mutex
	overlays []popups.Overlay
}

func (h *w14Host) ShowOverlay(o popups.Overlay) {
	h.mu.Lock()
	h.overlays = append(h.overlays, o)
	h.mu.Unlock()
}
func (h *w14Host) OpenEmojiPicker(image.Point, func(string)) {}

func (h *w14Host) browser() *popups.AppsBrowser {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.overlays) - 1; i >= 0; i-- {
		if b, ok := h.overlays[i].(*popups.AppsBrowser); ok {
			return b
		}
	}
	return nil
}

// w14Extra answers a call before cdTestServer does (method as
// cdTestServer names it: "edge/…" for edge calls).
type w14Extra func(method string, form url.Values, edge map[string]any) (string, bool)

// newW14Env is newCDTestEnv with extra answers and a popups host.
func newW14Env(t *testing.T, srv *cdTestServer, ch string, extra w14Extra) (*cdTestEnv, *w14Host) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := strings.TrimPrefix(r.URL.Path, "/api/")
		call := cdTestCall{method: method}
		if rest, ok := strings.CutPrefix(r.URL.Path, "/edge/"); ok {
			_, p, _ := strings.Cut(rest, "/")
			call.method = "edge/" + p
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &call.edge)
		} else {
			_ = r.ParseForm()
			call.form = r.Form
		}
		if extra != nil {
			if body, ok := extra(call.method, call.form, call.edge); ok {
				srv.mu.Lock()
				srv.calls = append(srv.calls, call)
				srv.mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
				return
			}
		}
		if call.edge != nil { // the body was read: hand cdTestServer a fresh one
			b, _ := json.Marshal(call.edge)
			r.Body = io.NopCloser(strings.NewReader(string(b)))
		}
		srv.handler(w, r)
	}))
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
	s.PutConv(slack.Conversation{ID: "D1", IsIM: true, User: "U1", IsMember: true})
	app := core.New(c, s)
	if u, ok := s.User("USELF"); ok {
		app.S.Self = *u
	}
	app.ServeOffline()
	th := theme.New(false)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	h := &w14Host{cdTestHost: &cdTestHost{app: app, th: th}}
	e := &cdTestEnv{t: t, srv: srv, h: h.cdTestHost}
	e.d = NewChannelDetails(h, ch)
	return e, h
}

// postingPrefsExtra answers channels.prefs.get by pref name.
func postingPrefsExtra(values map[string]string, set func(url.Values) string) w14Extra {
	return func(method string, form url.Values, _ map[string]any) (string, bool) {
		switch method {
		case "channels.prefs.get":
			v, ok := values[form.Get("pref_name")]
			if !ok {
				v = "{}"
			}
			return `{"ok":true,"pref_value":` + v + `}`, true
		case "channels.prefs.set":
			if set != nil {
				return set(form), true
			}
			return `{"ok":true,"prefs":{}}`, true
		case "features.access.policies.list":
			return `{"ok":true,"feature_policies":{"manage_channel_posting__access":{"is_enabled":true}}}`, true
		}
		return "", false
	}
}

func (e *cdTestEnv) settingsReady() {
	e.until("info", e.loaded)
	e.d.ShowTab("Settings")
	e.until("settings reads", func() bool {
		_, p, _ := e.d.x.perms.get()
		return p && e.h.app.FeatureAccess(core.FeatureManageChannelPosting)
	})
}

// A member with MANAGE_CHANNEL_PERMISSIONS (and the posting feature) sees
// Posting permissions first in the permissions section, with Slack's
// three bullets; the modal loads the prefs, saves only after a change,
// and sends channels.prefs.set with formatWhoCanPostApi's value.
func TestDetailsPostingPermissionsSave(t *testing.T) {
	srv := &cdTestServer{people: 3, allowed: `["MANAGE_CHANNEL_PERMISSIONS"]`}
	prefs := map[string]string{
		core.PrefWhoCanPost: `{"type":["regular"]}`, core.PrefCanThread: `{"type":["ra"]}`,
		core.PrefEnableHere: `{"enabled":true}`, core.PrefEnableAtChan: `{}`,
	}
	e, _ := newW14Env(t, srv, "C1", postingPrefsExtra(prefs, nil))
	e.settingsReady()
	inf, _, _ := e.d.info.get()
	vis := e.d.postingVisibility(inf)
	if !vis.Show || vis.Disabled {
		t.Fatalf("posting permissions %+v, want shown and editable", vis)
	}
	e.until("subtitle reads", func() bool { _, ok, _ := e.d.x.postSub.get(); return ok })
	p, _, _ := e.d.x.postSub.get()
	who, thread, here, ok := core.PostingSubtitle(p, e.d.postingViewer(inf))
	got := cdPostingBullets(who, thread, here, false, false, "")
	want := []string{"Everyone except guests can post", "Everyone can reply to messages",
		"Because of your workspace settings, only people who have permission can use @channel and @here mentions"}
	if !ok || !slices.Equal(got, want) {
		t.Errorf("subtitle %q (ok %v), want %q", got, ok, want)
	}

	e.d.x.postRow.Click()
	e.frame()
	if e.d.x.dlg.kind != cdDlgPosting {
		t.Fatalf("the row opened dialog %d", e.d.x.dlg.kind)
	}
	e.until("form", e.d.PostingLoaded)
	f := e.d.x.dlg.post.form
	if f.Who != "regular" || !f.CanThread || !f.AtHere {
		t.Errorf("form %+v", f)
	}
	e.frame() // the radios are drawn
	e.d.x.dlg.ok.Click()
	e.frame()
	if len(srv.seen("channels.prefs.set")) != 0 {
		t.Fatal("Save Changes saved without a change")
	}
	opts := e.d.postingOptions(inf, e.d.x.dlg.post)
	var labels []string
	for _, o := range opts {
		labels = append(labels, o.who+":"+o.label)
	}
	if w := []string{"ra:Everyone", "regular:Everyone, except guests", "admin:Admins only", "multi:Admins, plus specific people"}; !slices.Equal(labels, w) {
		t.Errorf("radios %q, want %q", labels, w)
	}
	e.d.PostingSelect("admin")
	e.frame()
	e.d.x.dlg.post.thread.Click()
	e.frame()
	if !e.d.x.dlg.post.changed || e.d.x.dlg.post.form.CanThread {
		t.Fatalf("after the changes: %+v", e.d.x.dlg.post)
	}
	before := len(srv.seen("channels.prefs.get"))
	e.d.x.dlg.ok.Click()
	e.until("channels.prefs.set", func() bool { return len(srv.seen("channels.prefs.set")) > 0 })
	sf := srv.seen("channels.prefs.set")[0].form
	wantPrefs := `{"who_can_post":"type:admin,user:USELF","can_thread":"type:admin,user:USELF","enable_at_here":"true","enable_at_channel":"true"}`
	if sf.Get("channel_id") != "C1" || sf.Get("prefs") != wantPrefs {
		t.Errorf("channels.prefs.set %v, want prefs %s", sf, wantPrefs)
	}
	e.until("closed and reloaded", func() bool {
		return e.d.x.dlg.kind == cdDlgNone && len(srv.seen("channels.prefs.get")) >= before+4
	})
}

// A plain member (no grants) doesn't get the row; a refused save shows
// Slack's restricted_action text in the modal.
func TestDetailsPostingPermissionsHiddenAndRefused(t *testing.T) {
	srv := &cdTestServer{people: 3}
	e, _ := newW14Env(t, srv, "C1", postingPrefsExtra(nil, nil))
	e.settingsReady()
	inf, _, _ := e.d.info.get()
	if vis := e.d.postingVisibility(inf); vis.Show {
		t.Errorf("a plain member sees posting permissions: %+v", vis)
	}

	srv2 := &cdTestServer{people: 3, allowed: `["MANAGE_CHANNEL_PERMISSIONS"]`}
	e2, _ := newW14Env(t, srv2, "C1", postingPrefsExtra(nil, func(url.Values) string {
		return `{"ok":false,"error":"restricted_action"}`
	}))
	e2.settingsReady()
	inf2, _, _ := e2.d.info.get()
	e2.d.openPosting(inf2)
	e2.until("form", e2.d.PostingLoaded)
	if f := e2.d.x.dlg.post.form; f.Who != "ra" || !f.CanThread {
		t.Errorf("an empty who_can_post reads %+v, want everyone with replies", f)
	}
	e2.frame()
	e2.d.x.dlg.post.here.Click()
	e2.frame()
	e2.d.x.dlg.ok.Click()
	e2.until("the error", func() bool { return e2.d.x.dlg.post != nil && e2.d.x.dlg.post.err != "" })
	if !strings.HasPrefix(e2.d.x.dlg.post.err, "Unable to save posting permissions.") {
		t.Errorf("error %q", e2.d.x.dlg.post.err)
	}
}

// "Add an App" opens the apps browser (no toast); the browser lists
// apps.index.list's categories, offers Add for an installed app whose bot
// isn't in the channel (edge channels/membership), and Add invites the
// bot (conversations.invite) and closes.
func TestDetailsAddAppOpensBrowser(t *testing.T) {
	srv := &cdTestServer{people: 3}
	index := `{"ok":true,"total_apps_installed":2,"categories":[{"id":"Ai01","display_name":"Installed apps","apps":[` +
		`{"app_id":"A1","name":"Polly","short_description":"Polls","bot_user_id":"UPOLLY","bot_id":"B1","is_installed":true},` +
		`{"app_id":"A2","name":"Giphy","short_description":"GIFs","bot_user_id":"UGIPHY","bot_id":"B2","is_installed":true}]},` +
		`{"id":"Ai02","display_name":"Recommended","apps":[{"app_id":"A3","name":"Jira","short_description":"Tickets","is_installed":false}]}]}`
	e, h := newW14Env(t, srv, "C1", func(method string, form url.Values, edge map[string]any) (string, bool) {
		switch method {
		case "apps.index.list":
			return index, true
		case "apps.index.search":
			return `{"ok":true,"categories":[]}`, true
		case "edge/channels/membership":
			return `{"ok":true,"members":["UGIPHY"],"non_members":["UPOLLY"]}`, true
		}
		return "", false
	})
	e.until("info", e.loaded)
	e.d.addApp.Click()
	e.frame()
	b := h.browser()
	if b == nil {
		t.Fatal("Add an App opened no apps browser")
	}
	for _, s := range e.h.tsts {
		if strings.Contains(s, "supported") {
			t.Errorf("toast %q", s)
		}
	}
	if want := "Add apps to general-ish"; b.Title() != want {
		t.Errorf("title %q, want %q", b.Title(), want)
	}
	var r input.Router
	var ops op.Ops
	frame := func() {
		popups.Pump()
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1440, 900)), Now: time.Now()}
		b.Layout(gtx)
		r.Frame(&ops)
	}
	deadline := time.Now().Add(10 * time.Second)
	for b.Loading() || len(b.Rows()) == 0 {
		frame()
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for apps.index.list")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var names []string
	labels := map[string]string{}
	for _, row := range b.Rows() {
		switch row.Kind {
		case "category":
			names = append(names, "#"+row.Category.DisplayName)
		case "app":
			names = append(names, row.App.Name)
			labels[row.App.Name] = b.ActionLabel(row.App)
		}
	}
	if w := []string{"#Installed apps", "Polly", "Giphy", "#Recommended", "Jira"}; !slices.Equal(names, w) {
		t.Errorf("rows %q, want %q", names, w)
	}
	if labels["Polly"] != "Add" || labels["Giphy"] != "View" || labels["Jira"] != "Install" {
		t.Errorf("buttons %v, want Polly Add, Giphy View, Jira Install", labels)
	}
	b.Select("A1")
	deadline = time.Now().Add(10 * time.Second)
	for len(srv.seen("conversations.invite")) == 0 {
		frame()
		if time.Now().After(deadline) {
			t.Fatal("Add sent no conversations.invite")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if f := srv.seen("conversations.invite")[0].form; f.Get("channel") != "C1" || f.Get("users") != "UPOLLY" {
		t.Errorf("conversations.invite %v", f)
	}
	if !b.Dismissed() {
		t.Error("the browser stayed open after Add")
	}
}

// historyExtra answers conversations.history: the first message at
// oldest, and for the preview three messages from the day after it.
func historyExtra(oldest time.Time, tooMany *bool) w14Extra {
	return func(method string, form url.Values, _ map[string]any) (string, bool) {
		switch method {
		case "conversations.history":
			if form.Get("oldest") == core.HistoryFromBeginning && form.Get("limit") == "1" {
				return `{"ok":true,"messages":[{"type":"message","user":"U1","text":"first","ts":"` + strconv.FormatInt(oldest.Unix(), 10) + `.000100"}]}`, true
			}
			var ms []string
			for i := 0; i < 3; i++ {
				ts := strconv.FormatInt(oldest.AddDate(0, 0, 1).Add(time.Duration(i)*time.Hour).Unix(), 10) + ".000100"
				ms = append(ms, `{"type":"message","user":"U1","text":"m`+strconv.Itoa(i)+`","ts":"`+ts+`"}`)
			}
			return `{"ok":true,"messages":[` + strings.Join(ms, ",") + `]}`, true
		case "conversations.createChild":
			if tooMany != nil && *tooMany {
				return `{"ok":false,"error":"too_many_messages"}`, true
			}
		}
		return "", false
	}
}

func (e *cdTestEnv) toHistoryStep() *cdAddPeople {
	e.t.Helper()
	e.until("info", e.loaded)
	e.d.addPeople.Click()
	e.frame()
	a := e.d.x.dlg.add
	if a == nil {
		e.t.Fatal("no add people modal")
	}
	a.picked = append(a.picked, core.User{ID: "U2", Name: "bob", RealName: "Bob Example"})
	e.d.x.dlg.ok.Click() // Next
	e.frame()
	if a.step != 1 {
		e.t.Fatalf("step %d", a.step)
	}
	e.until("the first message", func() bool { _, ok, _ := a.hx.oldest.get(); return ok })
	e.frame()
	return a
}

// Custom: Done waits for a date; the preview loads from the picked day
// (oldest = its local midnight, limit 200, inclusive); Done sends that
// start; too_many_messages opens the speed bump, whose OK closes it.
func TestDetailsDMAddPeopleCustomHistory(t *testing.T) {
	now := time.Now()
	oldest := dayStart(now.AddDate(0, 0, -10)).Add(9 * time.Hour)
	tooMany := false
	srv := &cdTestServer{}
	e, _ := newW14Env(t, srv, "D1", historyExtra(oldest, &tooMany))
	a := e.toHistoryStep()
	if got := historyOptions(a); !slices.Equal(got, []int{cdHistNone, cdHistToday, cdHistAll, cdHistCustom}) {
		t.Errorf("options %v", got)
	}
	if p := srv.seen("conversations.history"); len(p) == 0 || p[0].form.Get("oldest") != core.HistoryFromBeginning || p[0].form.Get("limit") != "1" {
		t.Errorf("first fetch %v", p)
	}
	a.histClk[cdHistCustom].Click()
	e.frame()
	if a.hist != cdHistCustom {
		t.Fatalf("hist %d after Custom", a.hist)
	}
	e.d.x.dlg.ok.Click() // Done, disabled without a date
	e.frame()
	if len(srv.seen("conversations.createChild")) != 0 {
		t.Fatal("Done created the group DM without a date")
	}
	day := dayStart(oldest.AddDate(0, 0, 1))
	a.hx.day = day // the calendar's onDateChange
	e.frame()
	start := strconv.FormatInt(day.Unix(), 10)
	e.until("the preview", func() bool {
		_, ok, _ := a.hx.prev.get()
		return ok && !a.hx.prev.busy()
	})
	var pf url.Values
	for _, c := range srv.seen("conversations.history") {
		if c.form.Get("oldest") == start {
			pf = c.form
		}
	}
	if pf == nil || pf.Get("limit") != "200" || pf.Get("inclusive") != "true" {
		t.Errorf("preview fetch %v (want oldest %s, limit 200, inclusive)", pf, start)
	}
	msgs, _, _ := a.hx.prev.get()
	rows := core.HistoryPreviewRows(msgs, start, time.Local)
	if len(rows) != 4 || rows[0].Kind != "day_divider" || rows[1].Msg.Text != "m0" {
		t.Errorf("preview rows %+v", rows)
	}

	tooMany = true
	e.d.x.dlg.ok.Click()
	e.until("the speed bump", func() bool { return a.hx.tooMany })
	a.hx.bumpOK.Click()
	e.frame()
	if a.hx.tooMany || e.d.x.dlg.kind != cdDlgAddPeople {
		t.Fatalf("OK left the bump %v / dialog %d", a.hx.tooMany, e.d.x.dlg.kind)
	}
	tooMany = false
	n := len(srv.seen("conversations.createChild"))
	e.d.x.dlg.ok.Click()
	e.until("createChild", func() bool { return len(srv.seen("conversations.createChild")) > n })
	if f := srv.seen("conversations.createChild")[n].form; f.Get("migrate_history_start_ts") != start || f.Get("user_ids") != "U2" {
		t.Errorf("createChild %v, want start %s", f, start)
	}
}

// A first message from today: no From today or Custom, and From the
// beginning selected with "(Today)".
func TestDetailsDMAddPeopleHistoryFromToday(t *testing.T) {
	now := time.Now()
	srv := &cdTestServer{}
	e, _ := newW14Env(t, srv, "D1", historyExtra(dayStart(now).Add(time.Minute), nil))
	a := e.toHistoryStep()
	if got := historyOptions(a); !slices.Equal(got, []int{cdHistNone, cdHistAll}) || a.hist != cdHistAll {
		t.Errorf("options %v, hist %d; want none and all, all selected", got, a.hist)
	}
	if l := histBeginningLabel(a.histOldestTime(), now); l != "From the beginning (Today)" {
		t.Errorf("label %q", l)
	}
}

func TestHistoryBeginningLabel(t *testing.T) {
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.Local)
	for _, c := range []struct {
		old  time.Time
		want string
	}{
		{time.Time{}, "From the beginning"},
		{time.Date(2026, 11, 11, 8, 0, 0, 0, time.Local), "From the beginning (Yesterday)"},
		{time.Date(2026, 11, 2, 8, 0, 0, 0, time.Local), "From the beginning (November 2nd)"},
		{time.Date(2025, 3, 23, 8, 0, 0, 0, time.Local), "From the beginning (March 23rd, 2025)"},
	} {
		if got := histBeginningLabel(c.old, now); got != c.want {
			t.Errorf("%v: %q, want %q", c.old, got, c.want)
		}
	}
}

func TestPostingBullets(t *testing.T) {
	got := cdPostingBullets("admin_multi", false, true, true, false, "")
	want := []string{"Only admins and specific people can post", "No one can reply to messages",
		"Because of your workspace settings, only people who have permission can use @everyone mentions"}
	if !slices.Equal(got, want) {
		t.Errorf("#general: %q, want %q", got, want)
	}
	if got := cdPostingBullets("regular", true, false, false, true, "Synthetic Club"); got[0] != "Only Synthetic Club members can post" ||
		got[2] != "No one can use @channel or @here mentions" {
		t.Errorf("external: %q", got)
	}
	if cdPostingBullets("", true, true, false, false, "") != nil {
		t.Error("no who_can_post drew bullets")
	}
}
