package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"

	"github.com/some-du6e/clack/internal/slack"
)

// Calls behind the profile surfaces (popups/profile_*.go, app_card.go),
// each the one Slack's web client makes (build a84662d2d135, 2026-11).

// ChannelMembership is getChannelMembershipInfoForUsers' flannel call
// (client-boot 42rc fetchChannelMembershipForChannel): edge
// channels/membership {channel, users, as_admin} answers members and
// non_members. The add-to-channel dialog asks it for the picked channel.
func (a *App) ChannelMembership(ctx context.Context, ch string, users []string) (map[string]bool, error) {
	var r struct {
		Members    []string `json:"members"`
		NonMembers []string `json:"non_members"`
	}
	if err := a.C.Edge(ctx, "channels/membership", map[string]any{"channel": ch, "users": users, "as_admin": false}, &r); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(users))
	for _, u := range r.Members {
		out[u] = true
	}
	for _, u := range r.NonMembers {
		out[u] = false
	}
	return out, nil
}

// AppScope is one entry of apps.permissions.internal.add's scope_info
// (OauthScopeInfo draws it: the short description, a dangerous one with
// the warning icon, the long one behind "Show more").
type AppScope struct {
	Scopes    []string `json:"scopes"`
	Short     string   `json:"short_description"`
	Long      string   `json:"long_description"`
	Dangerous bool     `json:"is_dangerous"`
}

// AddAppToChannel is fetchAppAddResource (boot-shared hRNA):
// apps.permissions.internal.add {app_user, channel, did_confirm,
// send_ephemeral_error_message}. Unconfirmed, Slack's addAppToChannel
// (XcD2) asks it for the scopes the app wants in ch and shows them in
// AppPermissionDialog; Allow sends it again confirmed, which adds the app.
func (a *App) AddAppToChannel(ctx context.Context, appUser, ch string, confirm bool) ([]AppScope, error) {
	var r struct {
		ScopeInfo []AppScope `json:"scope_info"`
	}
	p := url.Values{"app_user": {appUser}, "channel": {ch}, "did_confirm": {strconv.FormatBool(confirm)},
		"send_ephemeral_error_message": {"false"}}
	if err := a.C.Call(ctx, "apps.permissions.internal.add", p, &r); err != nil {
		return nil, err
	}
	return r.ScopeInfo, nil
}

// DenyAddAppToChannel is fetchAppDenyAddResource (hRNA): "Don't Allow" in
// AppPermissionDialog sends apps.permissions.internal.denyAdd {app_user,
// channel}.
func (a *App) DenyAddAppToChannel(ctx context.Context, appUser, ch string) error {
	return a.C.Call(ctx, "apps.permissions.internal.denyAdd", url.Values{"app_user": {appUser}, "channel": {ch}}, nil)
}

// VIPError is togglePriorityUserById's error toast text for a failed
// users.priority.add (boot-shared 77my PriorityErrorToast): "" when the
// error isn't an API answer (Slack toasts only errors with data).
func VIPError(err error, name string) string {
	var se *slack.Error
	if !errors.As(err, &se) {
		return ""
	}
	switch se.Code {
	case "user_not_found":
		return "User is not eligible to be added as VIP."
	case "too_many_vips":
		var b struct {
			Max json.Number `json:"max_vip_limit"`
		}
		_ = json.Unmarshal(se.Body, &b)
		return name + " could not be added to VIP. You can only have a maximum of " + b.Max.String() + " VIPs."
	}
	return "Hmm, something went wrong. Try again?"
}

// UsergroupProfile is what Slack's user group pane (UserGroupDetails)
// shows of a group.
type UsergroupProfile struct {
	Usergroup
	Disabled bool     // date_delete set: "<name> (Deactivated)", no tabs
	Channels []string // prefs.channels: the Channels tab
}

// UsergroupProfile finds group id: the boot list first, then
// UserGroupDetails' refresh, usergroups.search {keyword: the handle,
// count: 1} (client-channel-user-group-view), which also brings the
// group's channels and state.
func (a *App) UsergroupProfile(ctx context.Context, id string) (UsergroupProfile, error) {
	var out UsergroupProfile
	a.S.RLock()
	for _, g := range a.S.Groups {
		if g.ID == id {
			out.Usergroup = Usergroup{ID: g.ID, Handle: g.Handle, Name: g.Name, Description: g.Description, UserCount: g.UserCount}
		}
	}
	a.S.RUnlock()
	if out.Handle == "" {
		return out, errors.New("unknown user group " + id)
	}
	var res struct {
		Usergroups []struct {
			ID          string          `json:"id"`
			Handle      string          `json:"handle"`
			Name        string          `json:"name"`
			Description string          `json:"description"`
			UserCount   json.RawMessage `json:"user_count"`
			DateDelete  int64           `json:"date_delete"`
			Prefs       struct {
				Channels []string `json:"channels"`
			} `json:"prefs"`
		} `json:"usergroups"`
	}
	if err := a.C.Call(ctx, "usergroups.search", url.Values{"keyword": {out.Handle}, "count": {"1"}}, &res); err != nil {
		return out, err
	}
	for _, g := range res.Usergroups {
		if g.ID != id {
			continue
		}
		out.Usergroup = Usergroup{ID: g.ID, Handle: g.Handle, Name: g.Name, Description: g.Description, UserCount: int(looseInt(g.UserCount))}
		out.Disabled = g.DateDelete != 0
		out.Channels = g.Prefs.Channels
	}
	return out, nil
}

// UsergroupMembers is searchUserGroupMembers (client-channel-user-group-
// view oXTE, "Fetch and upsert user group members via search.enterprise"):
// people in group (is user, belongs_to_usergroup_id, plus fuzzy_with_email
// for the "Find members" term), 100 a page from cursor ("*" first),
// ascending, no deleted users or bots. It returns the members, the next
// cursor ("" when done) and num_found.
func (a *App) UsergroupMembers(ctx context.Context, group, term, cursor string) ([]User, string, int, error) {
	clauses := []map[string]string{{"type": "is", "value": "user"}, {"type": "belongs_to_usergroup_id", "value": group}}
	if term != "" {
		clauses = append(clauses, map[string]string{"type": "fuzzy_with_email", "value": term})
	}
	q, _ := json.Marshal(map[string]any{"type": "and", "clauses": clauses})
	if cursor == "" {
		cursor = "*"
	}
	p := url.Values{"query": {string(q)}, "count": {"100"}, "cursor_mark": {cursor}, "include_deleted": {"0"},
		"include_bots": {"0"}, "include_dangling": {"0"}, "sort_dir": {"asc"}}
	var res struct {
		Items      []slack.User `json:"items"`
		NextCursor string       `json:"next_cursor_mark"`
		NumFound   int          `json:"num_found"`
	}
	if err := a.C.Call(ctx, "search.enterprise", p, &res); err != nil {
		return nil, "", 0, err
	}
	a.S.PutUsers(res.Items)
	out := make([]User, 0, len(res.Items))
	for i := range res.Items {
		if res.Items[i].ID == "" {
			continue
		}
		out = append(out, a.userOf(&res.Items[i]))
	}
	next := res.NextCursor
	if next == cursor {
		next = "" // Slack: a cursor equal to the one sent means done
	}
	return out, next, res.NumFound, nil
}
