package popups

import (
	"encoding/json"
	"image"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// toolsEnv is ccEnv (Grid, a plain member) with the menus' links caught.
func toolsEnv(t *testing.T) (*tzHost, *[]string) {
	t.Helper()
	h, _ := ccEnv(t)
	var opened []string
	old := menuOpenURL
	menuOpenURL = func(_ Host, u string) { opened = append(opened, u) }
	t.Cleanup(func() { menuOpenURL = old })
	return h, &opened
}

func toolsKeys(items []MenuItem) []string {
	var ks []string
	for i := range items {
		ks = append(ks, menuKey(&items[i], i))
	}
	return ks
}

func toolsFind(items []MenuItem, key string) (MenuItem, bool) {
	for i := range items {
		if menuKey(&items[i], i) == key {
			return items[i], true
		}
	}
	return MenuItem{}, false
}

// toolsSetSelf changes the viewer's role in the store.
func toolsSetSelf(h *tzHost, admin, guest, frontline bool) {
	s := h.a.S
	s.Lock()
	if u := s.Users[s.Self.ID]; u != nil {
		u.IsAdmin, u.IsRestricted = admin, guest
		u.RestrictedUserType = ""
		if frontline {
			u.RestrictedUserType = "frontline_worker"
		}
	}
	s.Self.IsAdmin, s.Self.IsRestricted = admin, guest
	s.Unlock()
}

func toolsPrefs(t *testing.T, h *tzHost, js string) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatal(err)
	}
	h.a.SetTeamPrefs(m)
}

// The workspace menu as Slack's HomeHeaderMenu (q56z, build
// 1359418bd580) has it on Grid for a plain member: no row toasts, Tools
// holds Workspace analytics (a link to the gantry analytics page), Join or
// leave workspaces opens the team's site, and the rows Slack's desktop
// build doesn't draw (Open the Slack App) or Clack can't (Sign in on
// mobile's QR modal, Workflow Builder's Managed by you view) are gone.
func TestTeamMenuTools(t *testing.T) {
	h, opened := toolsEnv(t)
	m := NewTeamMenu(h, image.Pt(80, 60))
	keys := strings.Join(toolsKeys(m.items), " ")
	for _, gone := range []string{"Sign in on mobile", "Open the Slack App", "Workflows", "launch-workflow-builder", "open-desktop-app", "qr-code-signin-mobile"} {
		if strings.Contains(keys, gone) {
			t.Errorf("%q still in the menu: %s", gone, keys)
		}
		for _, it := range m.items {
			if it.Label == gone {
				t.Errorf("row %q still drawn", gone)
			}
		}
	}
	blurb, ok := toolsFind(m.items, "team-blurb-menu-item")
	if !ok || blurb.Widget == nil || blurb.Do != nil || blurb.DoCtx != nil {
		t.Errorf("team blurb %+v (found %v): it must draw and do nothing", blurb, ok)
	}
	tools, ok := toolsFind(m.items, "tools-and-settings-submenu")
	if !ok || tools.Label != "Tools" || len(tools.Sub) != 1 || tools.Sub[0].Key != "workspace-analytics" || tools.Sub[0].Label != "Workspace analytics" || !tools.Sub[0].External {
		t.Fatalf("Tools %+v (found %v)", tools, ok)
	}
	join, ok := toolsFind(m.items, "join-leave-workspaces")
	if !ok || join.Label != "Join or leave workspaces" || !join.External {
		t.Fatalf("Join or leave workspaces %+v (found %v)", join, ok)
	}
	if _, ok := toolsFind(m.items, "sign-out"); !ok {
		t.Errorf("no sign-out row: %s", keys)
	}
	// every row's action, sign out aside
	for i := range m.items {
		it := m.items[i]
		if it.Do != nil && menuKey(&m.items[i], i) != "sign-out" {
			it.Do()
		}
	}
	tools.Sub[0].Do()
	want := []string{h.a.TeamURL(), "https://app.slack.com/manage/" + fakeslack.TeamID + "/analytics"}
	if h.a.TeamURL() == "" || strings.Join(*opened, " ") != strings.Join(want, " ") {
		t.Errorf("opened %q, want %q", *opened, want)
	}
	if len(h.toasts) != 0 {
		t.Errorf("toasts %q", h.toasts)
	}
}

// Workspace analytics follows A8Mg P: never for guests, for admins only
// while stats_only_admins is on; with no tools the Tools row goes too.
// Frontline members get no Join or leave workspaces (KBR4 q).
func TestTeamMenuToolsGates(t *testing.T) {
	h, _ := toolsEnv(t)
	has := func(key string) bool {
		_, ok := toolsFind(NewTeamMenu(h, image.Pt(80, 60)).items, key)
		return ok
	}
	toolsPrefs(t, h, `{"stats_only_admins": true}`)
	if has("tools-and-settings-submenu") {
		t.Error("a member sees Tools under stats_only_admins")
	}
	toolsSetSelf(h, true, false, false)
	if !has("tools-and-settings-submenu") {
		t.Error("an admin doesn't see Workspace analytics under stats_only_admins")
	}
	toolsPrefs(t, h, `{"stats_only_admins": false}`)
	toolsSetSelf(h, false, true, false)
	if has("tools-and-settings-submenu") {
		t.Error("a guest sees Workspace analytics")
	}
	toolsSetSelf(h, false, false, true)
	if has("join-leave-workspaces") {
		t.Error("a frontline member sees Join or leave workspaces")
	}
	toolsSetSelf(h, false, false, false)
	if !has("tools-and-settings-submenu") || !has("join-leave-workspaces") {
		t.Error("a plain member lost Tools or Join or leave workspaces")
	}
}

// Account settings is UserMenu's account-settings row:
// openAuthenticatedUrlInBrowser(team, "/account/settings"), no toast.
func TestUserMenuAccountSettings(t *testing.T) {
	h, opened := toolsEnv(t)
	m := NewUserMenu(h, image.Pt(40, 760))
	it, ok := toolsFind(m.items, "account-settings")
	if !ok || it.Label != "Account settings" || !it.External || it.Do == nil {
		t.Fatalf("Account settings %+v (found %v)", it, ok)
	}
	it.Do()
	if want := h.a.TeamURL() + "account/settings"; len(*opened) != 1 || (*opened)[0] != want {
		t.Errorf("opened %q, want %q", *opened, want)
	}
	if len(h.toasts) != 0 {
		t.Errorf("toasts %q", h.toasts)
	}
}
