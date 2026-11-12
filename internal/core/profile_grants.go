package core

import (
	"context"
	"sync"
	"time"
)

// Edit profile's admin locks, read the way Slack's form pane does
// (gantry-v2-async-client-boot-shared eNqz EditProfileFormPane's
// mapStateToProps, build fa938a086569):
//
//   - isRealNameUneditable: !isRealNameEditable (xynP): the UPDATE_NAME
//     grant.
//   - isDisplayNameUneditable: !(+CT0 m): the UPDATE_DISPLAY_NAME grant,
//     and not a team on SSO (mAu1 isSsoEnabled: auth_mode set and not
//     "normal") whose sso_choose_username pref is false
//     (isSsoUsernameChoiceDisabled).
//   - isProfilePhotoUneditable: !(ckjT f): the EDIT_PROFILE_PHOTO grant.
//
// Each grant is getPermissionForAnyWorkspace (QaSy yI) for the team: what
// fetchPermissions (qPNw) stored from edge permissions/info {user_id,
// permissions, as_admin: true} (no channel, no team_ids: the workspace on
// Grid too, since getTeamId comes first in K5IA JY), isAllowed (T63e Jr)
// false when the grant is missing. A failed call stores nothing allowed
// (qPNw's catch answers empty lists, AcSl upsertNonExistent). Slack fetches
// the client permissions at boot (AcSl maybeFetchClientPermissions) and
// keeps them an hour; Clack asks when Edit profile opens.

// The grants (QxkB's permission enum keys).
const (
	PermUpdateName        = "UPDATE_NAME"
	PermUpdateDisplayName = "UPDATE_DISPLAY_NAME"
	PermEditProfilePhoto  = "EDIT_PROFILE_PHOTO"
)

// ProfileGrants is what Edit profile may change.
type ProfileGrants struct {
	Name        bool // isRealNameEditable
	DisplayName bool // +CT0 m (the grant and the SSO rule)
	Photo       bool // ckjT f
}

type profileGrantCache struct {
	mu      sync.Mutex
	allowed map[string]bool
	at      time.Time
}

var profileGrantIndex sync.Map // *App → *profileGrantCache

func (a *App) profileGrantCache() *profileGrantCache {
	if v, ok := profileGrantIndex.Load(a); ok {
		return v.(*profileGrantCache)
	}
	v, _ := profileGrantIndex.LoadOrStore(a, &profileGrantCache{})
	return v.(*profileGrantCache)
}

// CachedProfileGrants returns the grants from the last answer when it is
// under an hour old (ok false: ask LoadProfileGrants).
func (a *App) CachedProfileGrants() (ProfileGrants, bool) {
	c := a.profileGrantCache()
	c.mu.Lock()
	allowed, at := c.allowed, c.at
	c.mu.Unlock()
	if allowed == nil || time.Since(at) > chPermMaxAge {
		return ProfileGrants{}, false
	}
	return a.profileGrantsFrom(allowed), true
}

// LoadProfileGrants asks permissions/info for the three grants. It has no
// error: like Slack, a failed call allows nothing.
func (a *App) LoadProfileGrants(ctx context.Context) ProfileGrants {
	body := map[string]any{
		"user_id":     a.S.SelfID(),
		"permissions": []string{PermUpdateName, PermUpdateDisplayName, PermEditProfilePhoto},
		"as_admin":    true, // qPNw: neither teamIds nor a channel
	}
	var r struct {
		Allowed []string `json:"allowed"`
	}
	allowed := map[string]bool{}
	if err := a.C.Edge(ctx, "permissions/info", body, &r); err == nil {
		for _, p := range r.Allowed {
			allowed[p] = true
		}
	}
	c := a.profileGrantCache()
	c.mu.Lock()
	c.allowed, c.at = allowed, time.Now()
	c.mu.Unlock()
	return a.profileGrantsFrom(allowed)
}

func (a *App) profileGrantsFrom(allowed map[string]bool) ProfileGrants {
	return ProfileGrants{
		Name:        allowed[PermUpdateName],
		DisplayName: allowed[PermUpdateDisplayName] && !a.ssoUsernameLocked(),
		Photo:       allowed[PermEditProfilePhoto],
	}
}

// ssoUsernameLocked is isSsoEnabled(team) && isSsoUsernameChoiceDisabled
// (team): auth_mode set and not "normal", and sso_choose_username
// strictly false.
func (a *App) ssoUsernameLocked() bool {
	mode := a.TeamPrefString("auth_mode")
	if mode == "" || mode == "normal" {
		return false
	}
	raw, ok := a.TeamPref("sso_choose_username")
	return ok && string(raw) == "false"
}
