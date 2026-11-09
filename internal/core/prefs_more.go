package core

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Preferences-dialog writes beyond SetPref, each the call Slack's web
// client makes (gantry-v2 prefs dialog, 2026-11).

// SetMultiplePrefs writes several account prefs in one users.prefs.set
// (prefs={"name":value,…}), like Slack's setMultipleUserPrefsByApi (Mark
// as read sets mark_msgs_read_immediately and start_scroll_at_oldest
// together). Values keep their JSON type; the local copy is updated and a
// "prefs" event fires.
func (a *App) SetMultiplePrefs(ctx context.Context, kv map[string]any) error {
	b, err := json.Marshal(kv)
	if err != nil {
		return err
	}
	if err := a.C.Call(ctx, "users.prefs.set", url.Values{"prefs": {string(b)}}, nil); err != nil {
		return err
	}
	a.S.RLock()
	raw := make(map[string]json.RawMessage, len(a.S.RawPrefs)+len(kv))
	for k, v := range a.S.RawPrefs {
		raw[k] = v
	}
	a.S.RUnlock()
	for k, v := range kv {
		j, _ := json.Marshal(v)
		raw[k] = j
	}
	a.installPrefs(raw)
	p := a.Prefs()
	a.emit(Event{Type: "prefs", Prefs: &p})
	return nil
}

// GlobalNotifyPref is one of the global notification prefs Slack keeps in
// users.prefs all_notifications_prefs.global (global_desktop,
// global_keywords, threads_everything, no_text_in_notifications,
// push_idle_wait, …).
func (a *App) GlobalNotifyPref(name string) (json.RawMessage, bool) {
	var n struct {
		Global map[string]json.RawMessage `json:"global"`
	}
	if json.Unmarshal([]byte(a.PrefString("all_notifications_prefs")), &n) != nil {
		return nil, false
	}
	v, ok := n.Global[name]
	return v, ok
}

// GlobalNotifyBool is GlobalNotifyPref as a boolean (def when absent).
func (a *App) GlobalNotifyBool(name string, def bool) bool {
	v, ok := a.GlobalNotifyPref(name)
	if !ok {
		return def
	}
	return prefTruthy(v, def)
}

// GlobalNotifyString is GlobalNotifyPref as text (numbers as digits).
func (a *App) GlobalNotifyString(name string) string {
	v, _ := a.GlobalNotifyPref(name)
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	var f float64
	if json.Unmarshal(v, &f) == nil {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return ""
}

// SetGlobalNotifyPref writes a global notification pref the way Slack's
// setNotificationsGlobalPref does: users.prefs.setNotifications with
// name, value (as text), global=true, sync=false. Prefs are re-read so
// all_notifications_prefs shows the change, and a "prefs" event fires.
func (a *App) SetGlobalNotifyPref(ctx context.Context, name string, value any) error {
	v := notifyValue(value)
	err := a.C.Call(ctx, "users.prefs.setNotifications", url.Values{"name": {name}, "value": {v},
		"global": {"true"}, "sync": {"false"}}, nil)
	if err != nil {
		return err
	}
	if _, err := a.RefreshPrefs(ctx); err == nil {
		p := a.Prefs()
		a.emit(Event{Type: "prefs", Prefs: &p})
	}
	return nil
}

// VIPs is the VIP list, users.prefs vip_users (comma-separated user ids).
func (a *App) VIPs() []string {
	var out []string
	for _, id := range strings.Split(a.PrefString("vip_users"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// RemoveVIP takes user off the VIP list (users.priority.remove, like
// Slack's "Add/remove user from priority status"); vip_users is updated
// locally (Slack's vip_users_updated follows) and a "prefs" event fires.
func (a *App) RemoveVIP(ctx context.Context, user string) error {
	if err := a.C.Call(ctx, "users.priority.remove", url.Values{"user": {user}}, nil); err != nil {
		return err
	}
	var keep []string
	for _, id := range a.VIPs() {
		if id != user {
			keep = append(keep, id)
		}
	}
	a.setRawPref("vip_users", strings.Join(keep, ","))
	return nil
}

// AddVIP puts user on the VIP list (users.priority.add).
func (a *App) AddVIP(ctx context.Context, user string) error {
	if err := a.C.Call(ctx, "users.priority.add", url.Values{"user": {user}, "reason": {"add-user-to-priority"}}, nil); err != nil {
		return err
	}
	ids := a.VIPs()
	for _, id := range ids {
		if id == user {
			return nil
		}
	}
	a.setRawPref("vip_users", strings.Join(append(ids, user), ","))
	return nil
}

// setRawPref updates one pref locally (after a write that isn't
// users.prefs.set) and emits "prefs".
func (a *App) setRawPref(name string, value any) {
	j, _ := json.Marshal(value)
	a.S.RLock()
	raw := make(map[string]json.RawMessage, len(a.S.RawPrefs)+1)
	for k, v := range a.S.RawPrefs {
		raw[k] = v
	}
	a.S.RUnlock()
	raw[name] = j
	a.installPrefs(raw)
	p := a.Prefs()
	a.emit(Event{Type: "prefs", Prefs: &p})
}

// SetGlobalNotifyPrefs writes several global notification prefs in one
// users.prefs.setNotifications (prefs=[{name,value}], global=true), like
// Slack's setNotificationsMultiGlobalPrefsByApi ("What to notify you
// about" sets global_desktop and global_mobile together).
func (a *App) SetGlobalNotifyPrefs(ctx context.Context, kv map[string]any) error {
	type nv struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	var list []nv
	for k, v := range kv {
		list = append(list, nv{k, notifyValue(v)})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	b, _ := json.Marshal(list)
	if err := a.C.Call(ctx, "users.prefs.setNotifications", url.Values{"prefs": {string(b)}, "global": {"true"}}, nil); err != nil {
		return err
	}
	if _, err := a.RefreshPrefs(ctx); err == nil {
		p := a.Prefs()
		a.emit(Event{Type: "prefs", Prefs: &p})
	}
	return nil
}

// notifyValue is a notification pref's value as setNotifications takes it.
func notifyValue(value any) string {
	switch x := value.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	}
	b, _ := json.Marshal(value)
	return string(b)
}

// TeamURL is the workspace's web address (https://<domain>.slack.com/),
// "" before the first users.prefs/team load.
func (a *App) TeamURL() string {
	a.S.RLock()
	d := a.S.TeamDomain
	a.S.RUnlock()
	if d == "" {
		return ""
	}
	return "https://" + d + ".slack.com/"
}

// WorkflowAppAuth is one app in Preferences > Connected accounts > "Apps
// connected through workflows" (apps.auth.external.userAuths.get apps).
type WorkflowAppAuth struct {
	AppID, Name, Icon string
}

// WorkflowAppAuths is Slack's fetchUserAuth (reason fetch_user_auth):
// the third-party accounts the viewer set up through workflows, by app.
func (a *App) WorkflowAppAuths(ctx context.Context) ([]WorkflowAppAuth, error) {
	var r struct {
		Apps map[string]struct {
			AppName    string `json:"app_name"`
			AppIconURL string `json:"app_icon_url"`
		} `json:"apps"`
	}
	if err := a.C.Call(ctx, "apps.auth.external.userAuths.get", url.Values{"reason": {"fetch_user_auth"}}, &r); err != nil {
		return nil, err
	}
	out := make([]WorkflowAppAuth, 0, len(r.Apps))
	for id, ap := range r.Apps {
		out = append(out, WorkflowAppAuth{AppID: id, Name: ap.AppName, Icon: ap.AppIconURL})
	}
	slices.SortFunc(out, func(x, y WorkflowAppAuth) int {
		return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
	})
	return out, nil
}

// EmailVerified is users.emailVerificationStatus.get is_verified (Privacy
// & visibility lists blocked invitations only for a verified email).
func (a *App) EmailVerified(ctx context.Context) (bool, error) {
	var r struct {
		IsVerified bool `json:"is_verified"`
	}
	err := a.C.Call(ctx, "users.emailVerificationStatus.get", url.Values{"reason": {"privacy-visibility/use-email-verification-status"}}, &r)
	return r.IsVerified, err
}

// BlockedInviteEmails is users.listIgnoredUsers emails: the people whose
// Slack Connect invitations the viewer blocked.
func (a *App) BlockedInviteEmails(ctx context.Context) ([]string, error) {
	var r struct {
		Emails []string `json:"emails"`
	}
	err := a.C.Call(ctx, "users.listIgnoredUsers", url.Values{"reason": {"privacy-visibility/use-blocked-emails-list"}}, &r)
	return r.Emails, err
}

// UnblockInvites is users.removeUserBlock: invitations from email arrive
// again.
func (a *App) UnblockInvites(ctx context.Context, email string) error {
	return a.C.Call(ctx, "users.removeUserBlock", url.Values{"email": {email}, "reason": {"privacy-visibility/remove-user-block"}}, nil)
}

// HiddenUsers is users.prefs hidden_users (People you've hidden).
func (a *App) HiddenUsers() []string { return SearchExcludedChannels(a.PrefString("hidden_users")) }
