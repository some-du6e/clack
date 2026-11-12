package core

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// Edit profile's admin locks: edge permissions/info {user_id,
// permissions, as_admin} (qPNw with no channel and no teams); a missing
// grant, or a failed call, allows nothing (T63e Jr); a team on SSO with
// sso_choose_username false sets display names itself (+CT0 m).
func TestProfileGrantsLikeSlack(t *testing.T) {
	a, fs := inviteApp(t, slack.User{}, `{}`)
	ctx := context.Background()
	var mu sync.Mutex
	var bodies []map[string]json.RawMessage
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		if method == "edge/permissions/info" {
			mu.Lock()
			bodies = append(bodies, body)
			mu.Unlock()
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetEdgeHook(nil) })
	if _, ok := a.CachedProfileGrants(); ok {
		t.Fatal("grants cached before any call")
	}
	g := a.LoadProfileGrants(ctx)
	if g != (ProfileGrants{Name: true, DisplayName: true, Photo: true}) {
		t.Errorf("the fake's default grants: %+v", g)
	}
	mu.Lock()
	if len(bodies) != 1 {
		t.Fatalf("%d permissions/info calls", len(bodies))
	}
	b := bodies[0]
	mu.Unlock()
	var perms []string
	_ = json.Unmarshal(b["permissions"], &perms)
	if string(b["as_admin"]) != "true" || string(b["user_id"]) != `"`+a.S.SelfID()+`"` || b["channel_id"] != nil || b["team_ids"] != nil ||
		len(perms) != 3 || perms[0] != PermUpdateName || perms[1] != PermUpdateDisplayName || perms[2] != PermEditProfilePhoto {
		t.Errorf("body %s", b)
	}
	if c, ok := a.CachedProfileGrants(); !ok || c != g {
		t.Errorf("cached %+v %v", c, ok)
	}

	// only the photo granted
	fs.Permissions = map[string][]string{"": {PermEditProfilePhoto}}
	if g := a.LoadProfileGrants(ctx); g != (ProfileGrants{Photo: true}) {
		t.Errorf("photo only: %+v", g)
	}
	// SSO: auth_mode saml with sso_choose_username false locks the display
	// name even with the grant; "normal", or the pref on, doesn't
	fs.Permissions = nil
	for _, c := range []struct {
		prefs string
		want  bool
	}{
		{`{"auth_mode":"saml","sso_choose_username":false}`, false},
		{`{"auth_mode":"saml","sso_choose_username":true}`, true},
		{`{"auth_mode":"saml"}`, true},
		{`{"auth_mode":"normal","sso_choose_username":false}`, true},
	} {
		var m map[string]json.RawMessage
		_ = json.Unmarshal([]byte(c.prefs), &m)
		a.SetTeamPrefs(m)
		if g := a.LoadProfileGrants(ctx); g.DisplayName != c.want || !g.Name {
			t.Errorf("%s: %+v", c.prefs, g)
		}
	}
	// a failed call allows nothing
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		if method == "edge/permissions/info" {
			return map[string]any{"ok": false, "error": "internal_error"}, true
		}
		return nil, false
	})
	if g := a.LoadProfileGrants(ctx); g != (ProfileGrants{}) {
		t.Errorf("failed call: %+v", g)
	}
}
