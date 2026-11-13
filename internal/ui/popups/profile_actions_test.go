package popups

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// pfEnv is hcEnv that also hands back the fake (its hooks).
func pfEnv(t *testing.T) (*tzHost, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(""))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.EnsureUsers(ctx, []string{fakeslack.SelfID, fakeslack.ProfileID, fakeslack.DeactivatedID, fakeslack.AppUserID})
	if u, ok := a.S.User(fakeslack.SelfID); ok {
		a.S.Self = *u
	}
	noNet := func(context.Context, string) ([]byte, error) { return nil, errors.New("no network in tests") }
	th := theme.New(false)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	th.Images = &pvImages{}
	return &tzHost{a: a, th: th, im: imgcache.New(noNet, "", nil)}, fs
}

// pfUntil drains popups' async queue until cond holds.
func pfUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for dl := time.Now().Add(10 * time.Second); !cond(); {
		if time.Now().After(dl) {
			t.Fatalf("timed out waiting for %s", what)
		}
		drain()
		time.Sleep(5 * time.Millisecond)
	}
}

// pfCalls records the fake's calls of some methods.
type pfCalls struct {
	mu    sync.Mutex
	calls map[string][]url.Values
}

func (c *pfCalls) add(m string, f url.Values) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.calls == nil {
		c.calls = map[string][]url.Values{}
	}
	c.calls[m] = append(c.calls[m], f)
}

func (c *pfCalls) get(m string) []url.Values {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]url.Values(nil), c.calls[m]...)
}

// pfFrame lays w out in a sz window once under a tofu scan; with
// PROFILE_SHOTS_DIR set (the own-profile workflow) it also saves shot.
func pfFrame(t *testing.T, th *theme.Theme, sz image.Point, shot string, w func(gtx layout.Context)) []string {
	t.Helper()
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Now: time.Date(2026, 11, 8, 15, 42, 0, 0, time.UTC),
			Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(sz)}
		paint.Fill(ops, colorsOf(th).Bg)
		w(gtx)
	}
	frame()
	stop := theme.StartTofuScan()
	frame()
	st := stop()
	if st.Notdef > 0 {
		t.Errorf("%s: %d .notdef glyphs", shot, st.Notdef)
	}
	if dir := os.Getenv("PROFILE_SHOTS_DIR"); dir != "" && shot != "" {
		gpu, err := pixeltest.NewWindow(sz.X, sz.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		defer gpu.Release()
		if err := gpu.Frame(ops); err != nil {
			t.Fatalf("gpu frame: %v", err)
		}
		img := image.NewRGBA(image.Rectangle{Max: sz})
		if err := gpu.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, shot+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	return st.Texts
}

func hasText(texts []string, s string) bool { return slices.Contains(texts, s) }

// The VIP button (renderVipButton) and togglePriorityUserById: an
// optimistic flip, users.priority.add then .remove on the fake, Slack's
// toasts, the selected icon, and the too_many_vips error toast with the
// list put back.
func TestProfileVIPToggle(t *testing.T) {
	h, fs := pfEnv(t)
	var calls pfCalls
	fail := ""
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if !strings.HasPrefix(method, "users.priority.") {
			return nil, false
		}
		calls.add(method, form)
		if fail != "" && method == "users.priority.add" {
			return map[string]any{"ok": false, "error": fail, "max_vip_limit": 50}, true
		}
		return nil, false
	})
	u, _ := lookupUser(h, fakeslack.ProfileID)
	if !pfCanToggleVIP(h, u) {
		t.Fatal("no VIP button for another member")
	}
	self, _ := lookupUser(h, fakeslack.SelfID)
	gone, _ := lookupUser(h, fakeslack.DeactivatedID)
	if pfCanToggleVIP(h, self) || pfCanToggleVIP(h, gone) {
		t.Error("a VIP button on your own or a deactivated member's profile")
	}
	if s := pfVipSpec(h, u, pfBtnSpec{}); s.label != "VIP" || s.lead != "user-add" || s.selected {
		t.Errorf("not a VIP: %+v", s)
	}
	name := pfMemberName(u)
	pfToggleVIP(h, u)
	if !pfIsVIP(h, u.ID) {
		t.Error("the button didn't flip at once")
	}
	pfUntil(t, "the add toast", func() bool { return slices.Contains(h.toastList(), "Added "+name+" to VIPs") })
	if !slices.Contains(h.a.VIPs(), u.ID) || len(calls.get("users.priority.add")) != 1 {
		t.Fatalf("users.priority.add: %v, VIPs %v", calls.get("users.priority.add"), h.a.VIPs())
	}
	if s := pfVipSpec(h, u, pfBtnSpec{}); s.lead != "user-hide" || !s.selected {
		t.Errorf("a VIP: %+v", s)
	}
	pfToggleVIP(h, u)
	pfUntil(t, "the remove toast", func() bool { return slices.Contains(h.toastList(), "Removed "+name+" from VIPs") })
	if slices.Contains(h.a.VIPs(), u.ID) || calls.get("users.priority.remove")[0].Get("user") != u.ID {
		t.Fatalf("users.priority.remove: %v", calls.get("users.priority.remove"))
	}
	fail = "too_many_vips"
	pfToggleVIP(h, u)
	want := name + " could not be added to VIP. You can only have a maximum of 50 VIPs."
	pfUntil(t, "the error toast", func() bool { return slices.Contains(h.toastList(), want) })
	if pfIsVIP(h, u.ID) {
		t.Error("a failed add left the member a VIP")
	}
}

// "View files" is the files from that member: Clack's search, Files tab,
// from:<@id>.
func TestProfileMenuViewFiles(t *testing.T) {
	h, _ := pfEnv(t)
	u, _ := lookupUser(h, fakeslack.ProfileID)
	for _, it := range pfMoreItems(h, u, false, nil) {
		if it.Label == "View files" {
			it.Do()
		}
	}
	want := "search?q=" + url.QueryEscape("from:<@"+u.ID+">") + "&module=files"
	if len(h.navs) != 1 || h.navs[0] != want {
		t.Fatalf("navigated %q, want %q", h.navs, want)
	}
	if srTabOf("files") != srTabFiles {
		t.Error("module=files isn't the Files tab")
	}
	self, _ := lookupUser(h, fakeslack.SelfID)
	var labels []string
	for _, it := range pfMoreItems(h, self, false, nil) {
		labels = append(labels, it.Label)
	}
	if !slices.Contains(labels, "View your files") {
		t.Errorf("own menu %q", labels)
	}
}

// A filterable field opens the people directory filtered by it (Slack's
// profileField.<id> browser filter).
func TestProfileFieldLink(t *testing.T) {
	h, _ := pfEnv(t)
	pfSearchPeopleByField(h, "Xf01ABC", "Fixture Wrangler & Co")
	want := "directories?tab=people&field=Xf01ABC&value=" + url.QueryEscape("Fixture Wrangler & Co")
	if len(h.navs) != 1 || h.navs[0] != want {
		t.Fatalf("navigated %q, want %q", h.navs, want)
	}
}

// pfHost is tzHost with the ui root's pane opener.
type pfHost struct {
	*tzHost
	opened []string
}

func (h *pfHost) OpenUserProfile(id string) { h.opened = append(h.opened, id) }

// Without the ui root's hooks the card's "View full profile" and the user
// menu's Profile reach the host's pane, never a toast.
func TestProfileOpenFullWithoutHooks(t *testing.T) {
	th, _ := pfEnv(t)
	h := &pfHost{tzHost: th}
	if OpenFullProfile != nil {
		t.Skip("OpenFullProfile set by another test")
	}
	pfOpenFull(h, fakeslack.ProfileID)
	if len(h.opened) != 1 || h.opened[0] != fakeslack.ProfileID || len(h.toastList()) > 0 {
		t.Fatalf("opened %v, toasts %v", h.opened, h.toastList())
	}
}

// "View as" on your own profile: Slack's ProfilePreviewMenu items, the
// preview bar, the profile others see (Message and Huddle, no Edit, add
// links or View as), and Exit Preview back to yours.
func TestProfileViewAs(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h, _ := pfEnv(t)
		h.th.SetDark(dark)
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		v := NewProfileView(h, fakeslack.SelfID)
		st := pvFrames(t, v, nil, "")
		if !has(st.Texts, "View as") || !has(st.Texts, "Set a status") {
			t.Fatalf("own profile: %q", st.Texts)
		}
		items := v.previewItems()
		org := pfOrgName(h)
		wantTeam := "A coworker"
		if org != "" {
			wantTeam = "A coworker at " + org
		}
		if len(items) != 2 || items[0].Label != wantTeam || items[1].Label != "A contact from other organizations" {
			t.Fatalf("preview menu %q / %q", items[0].Label, items[1].Label)
		}
		items[0].Do()
		if v.preview != pfPreviewInternal {
			t.Fatal("no Internal preview")
		}
		x := pfFrame(t, h.th, image.Pt(pvW, pvH), "own-profile-view-as-"+mode, func(gtx layout.Context) { v.Layout(gtx) })
		for _, want := range []string{"Viewing As Coworker", "Exit Preview", "Message", "Huddle"} {
			if !hasText(x, want) {
				t.Errorf("%s preview: %q not drawn", mode, want)
			}
		}
		for _, bad := range []string{"Edit", "Set a status", "View as", "Add pronouns", "Add Phone"} {
			if hasText(x, bad) {
				t.Errorf("%s preview: %q drawn", mode, bad)
			}
		}
		v.setPreview(pfPreviewExternal)
		x = pfFrame(t, h.th, image.Pt(pvW, pvH), "", func(gtx layout.Context) { v.Layout(gtx) })
		if !hasText(x, "Viewing as External") {
			t.Errorf("%s: no external preview bar", mode)
		}
		v.setPreview(pfPreviewNone)
		x = pfFrame(t, h.th, image.Pt(pvW, pvH), "", func(gtx layout.Context) { v.Layout(gtx) })
		if hasText(x, "Exit Preview") || !hasText(x, "Set a status") {
			t.Errorf("%s: Exit Preview didn't bring your profile back", mode)
		}
	}
}

// The app card's "Add this app to a channel…": the picker, the membership
// check (edge channels/membership), "already a member", then
// apps.permissions.internal.add unconfirmed → AppPermissionDialog with the
// scopes → Allow (confirmed) or Don't Allow (denyAdd), all on the fake.
func TestAppAddToChannel(t *testing.T) {
	h, fs := pfEnv(t)
	var calls pfCalls
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		switch method {
		case "apps.permissions.internal.add":
			calls.add(method, form)
			if form.Get("did_confirm") == "false" {
				return map[string]any{"ok": true, "scope_info": []map[string]any{
					{"scopes": []string{"chat:write"}, "short_description": "Send messages as Deploy Bot"},
					{"scopes": []string{"channels:history"}, "short_description": "View messages and other content in public channels",
						"long_description": "Including messages in threads.", "is_dangerous": true},
				}}, true
			}
			return map[string]any{"ok": true}, true
		case "apps.permissions.internal.denyAdd":
			calls.add(method, form)
			return map[string]any{"ok": true}, true
		}
		return nil, false
	})
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		if !strings.HasSuffix(method, "channels/membership") {
			return nil, false
		}
		var ch string
		_ = json.Unmarshal(body["channel"], &ch)
		calls.add("channels/membership", url.Values{"channel": {ch}})
		if ch == "C0MEMBER" {
			return map[string]any{"ok": true, "members": []string{fakeslack.AppUserID}}, true
		}
		return map[string]any{"ok": true, "non_members": []string{fakeslack.AppUserID}}, true
	})
	d := pfOpenInviteToChannel(h, fakeslack.AppUserID)
	if d == nil {
		t.Fatal("no picker dialog")
	}
	app, _ := lookupUser(h, fakeslack.AppUserID)
	title := "Add " + pfMemberName(app) + " to a channel"
	x := pfFrame(t, h.th, image.Pt(1000, 700), "app-add-to-channel", func(gtx layout.Context) { d.modal.Layout(gtx) })
	if !hasText(x, title) || !hasText(x, "Select a channel") || !hasText(x, "Add") {
		t.Errorf("picker: %q", x)
	}
	if d.state != pfInviteDisabled {
		t.Error("Add enabled before a channel is picked")
	}
	d.pick("C0MEMBER")
	pfUntil(t, "the membership answer", func() bool { return d.state != pfInviteLoading })
	if !d.isMember || d.state != pfInviteDisabled {
		t.Fatalf("already a member: member %v state %d", d.isMember, d.state)
	}
	x = pfFrame(t, h.th, image.Pt(1000, 700), "", func(gtx layout.Context) { d.modal.Layout(gtx) })
	if !hasText(x, pfMemberName(app)+" is already a member of this channel.") {
		t.Errorf("no inline error: %q", x)
	}
	d.pick("C0OTHER")
	pfUntil(t, "the membership answer", func() bool { return d.state == pfInviteEnabled })
	if !d.add() {
		t.Fatal("Add didn't close the picker")
	}
	pfUntil(t, "the permission dialog", func() bool { return d.perm != nil })
	if got := calls.get("apps.permissions.internal.add"); len(got) != 1 || got[0].Get("app_user") != fakeslack.AppUserID ||
		got[0].Get("channel") != "C0OTHER" || got[0].Get("did_confirm") != "false" {
		t.Fatalf("unconfirmed add: %v", got)
	}
	p := d.perm
	x = pfFrame(t, h.th, image.Pt(1000, 700), "app-permission-dialog", func(gtx layout.Context) { p.modal.Layout(gtx) })
	for _, want := range []string{"Send messages as Deploy Bot", "View messages and other content in public channels", "Allow", "Don’t Allow"} {
		if !hasText(x, want) {
			t.Errorf("permission dialog: %q not drawn", want)
		}
	}
	if !slices.ContainsFunc(x, func(s string) bool { return strings.Contains(s, "would") }) {
		t.Errorf("no title: %q", x)
	}
	p.allowNow()
	pfUntil(t, "the confirmed add", func() bool { return len(calls.get("apps.permissions.internal.add")) == 2 })
	if c := calls.get("apps.permissions.internal.add")[1]; c.Get("did_confirm") != "true" || c.Get("channel") != "C0OTHER" {
		t.Errorf("confirmed add: %v", c)
	}
	if !p.modal.Dismissed() {
		t.Error("Allow left the dialog open")
	}
	q := newAppPermissionDialog(h, fakeslack.AppUserID, "C0OTHER", nil)
	q.denyNow()
	pfUntil(t, "denyAdd", func() bool { return len(calls.get("apps.permissions.internal.denyAdd")) == 1 })
	if c := calls.get("apps.permissions.internal.denyAdd")[0]; c.Get("app_user") != fakeslack.AppUserID || c.Get("channel") != "C0OTHER" {
		t.Errorf("denyAdd: %v", c)
	}
	if ts := h.toastList(); len(ts) > 0 {
		t.Errorf("toasts: %q", ts)
	}
}

// The User Group pane on the fake's two group kinds, light and dark
// (client-channel-user-group-view's BaseUserGroupView):
//   - a workspace group lists usergroups.users.list's members, sorted, with
//     no search box and no search.enterprise call (on the real grid that
//     query finds nobody: "Nobody’s in this group" for a group of 29);
//   - an org-level group lists search.enterprise's, under "Find members";
//   - a failed fetch on a workspace group reads "Nobody’s in this group",
//     as Slack's (no error state); an empty workspace group says so too, with the "workspace
//     administrator" link to the people browser's admins, and its Channels
//     tab says "No default channels".
func TestUserGroupPane(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		h, fs := pfEnv(t)
		h.th.SetDark(dark)
		gs, err := h.a.C.Usergroups(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		h.a.S.SetUsergroups(gs)
		var calls pfCalls
		var failList atomic.Bool
		fs.SetHook(func(method string, form url.Values) (any, bool) {
			calls.add(method, form)
			if method == "usergroups.users.list" && failList.Load() {
				return map[string]any{"ok": false, "error": "fatal_error"}, true
			}
			return nil, false
		})
		sz := image.Pt(pvW, 900)
		frame := func(v *UserGroupView, shot string) []string {
			return pfFrame(t, h.th, sz, shot, func(gtx layout.Context) { v.Layout(gtx) })
		}

		// a workspace group
		v := NewUserGroupView(h, fakeslack.WorkspaceGroupID)
		frame(v, "")
		pfUntil(t, mode+" workspace members", func() bool { return v.loaded && v.started && !v.loading && len(v.members) == 6 })
		x := frame(v, "usergroup-pane-"+mode)
		for _, want := range []string{"User Group", "Snack crew", "Synthetic workspace group", "@snackcrew", "Members", "6", "Channels"} {
			if !hasText(x, want) {
				t.Errorf("%s workspace group: %q not drawn", mode, want)
			}
		}
		for _, bad := range []string{"Find members", "Nobody’s in this group"} {
			if hasText(x, bad) {
				t.Errorf("%s workspace group: %q drawn", mode, bad)
			}
		}
		for i := 1; i < len(v.members); i++ {
			p, _ := h.a.S.User(v.members[i-1].ID)
			q, _ := h.a.S.User(v.members[i].ID)
			if strings.ToLower(store.PrefName(p, false)) > strings.ToLower(store.PrefName(q, false)) {
				t.Errorf("%s: members not sorted: %q before %q", mode, store.PrefName(p, false), store.PrefName(q, false))
			}
		}
		if l := calls.get("usergroups.users.list"); len(l) != 1 || l[0].Get("usergroup") != fakeslack.WorkspaceGroupID ||
			l[0].Get("team_id") != fakeslack.TeamID || l[0].Has("limit") {
			t.Errorf("%s usergroups.users.list: %v", mode, l)
		}
		if se := calls.get("search.enterprise"); len(se) != 0 {
			t.Errorf("%s workspace group asked search.enterprise: %v", mode, se)
		}
		if us := calls.get("usergroups.search"); len(us) != 1 || us[0].Get("keyword") != "snackcrew" || us[0].Get("count") != "1" {
			t.Errorf("%s usergroups.search: %v", mode, us)
		}
		v.tab = 1
		if x = frame(v, ""); !hasText(x, "No default channels") || hasText(x, "No channels are connected to this group") {
			t.Errorf("%s workspace channels tab: %q", mode, x)
		}

		// an org-level group
		o := NewUserGroupView(h, fakeslack.OrgGroupID)
		frame(o, "")
		pfUntil(t, mode+" org members", func() bool { return o.loaded && o.started && !o.loading && len(o.members) == 4 })
		x = frame(o, "usergroup-org-pane-"+mode)
		for _, want := range []string{"Org crew", "@orgcrew", "Find members"} {
			if !hasText(x, want) {
				t.Errorf("%s org group: %q not drawn", mode, want)
			}
		}
		se := calls.get("search.enterprise")
		if len(se) == 0 || !strings.Contains(se[0].Get("query"), `"belongs_to_usergroup_id"`) || !strings.Contains(se[0].Get("query"), fakeslack.OrgGroupID) ||
			se[0].Get("count") != "100" || se[0].Get("cursor_mark") != "*" || se[0].Get("sort_dir") != "asc" {
			t.Errorf("%s org search.enterprise: %v", mode, se)
		}
		for _, m := range o.members {
			if m.ID == fakeslack.DeactivatedID {
				t.Errorf("%s org group lists the deactivated member", mode)
			}
		}

		// an empty workspace group, then its admin link
		e := NewUserGroupView(h, "S0FAKE00000")
		frame(e, "")
		pfUntil(t, mode+" empty group", func() bool { return e.loaded && e.started && !e.loading })
		x = frame(e, "usergroup-empty-pane-"+mode)
		for _, want := range []string{"Nobody’s in this group", "Contact", "workspace administrator", "members."} {
			if !hasText(x, want) {
				t.Errorf("%s empty group: %q not drawn", mode, want)
			}
		}
		e.adminLink.Click()
		frame(e, "")
		h.mu.Lock()
		navs := slices.Clone(h.navs)
		h.mu.Unlock()
		if !slices.Contains(navs, "directories?tab=people&account=admins") {
			t.Errorf("%s admin link: navs %v", mode, navs)
		}

		// a failed fetch with nothing cached: Slack's classic pane has no
		// error state, so it reads "Nobody’s in this group"
		failList.Store(true)
		f := NewUserGroupView(h, "S0FAKE00008")
		frame(f, "")
		pfUntil(t, mode+" failed fetch", func() bool { return f.loaded && f.started && !f.loading })
		if x = frame(f, ""); !hasText(x, "Nobody’s in this group") || !f.failed {
			t.Errorf("%s failed fetch: failed=%v, %q", mode, f.failed, x)
		}
		// and with members cached from before, it keeps them
		w := NewUserGroupView(h, fakeslack.WorkspaceGroupID)
		frame(w, "")
		pfUntil(t, mode+" cached members", func() bool { return w.loaded && w.started && !w.loading })
		if len(w.members) != 6 || w.failed {
			t.Errorf("%s failed refetch: %d members, failed=%v", mode, len(w.members), w.failed)
		}
		failList.Store(false)
	}
}
