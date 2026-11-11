package core

// Channel details reads and writes beyond conversations.info, each the
// call Slack's web client makes (gantry-v2 build 407aff01646b, 2026-11-10):
//
//   - the viewer's channel permissions: fetchPermissions (client-boot
//     qPNw) → permissions.info {user_id, permissions, channel_id}
//   - Member / Guest permissions: fetchChannelManagementPermissions
//     (zTmD HE) → conversations.permissions.accountTypes.list, saved by
//     saveChannelManagementPermissions → …accountTypes.set
//   - channel properties: conversations.canSetProperties (AdminChannelModal's
//     "channel-settings-can-set-properties") and conversations.setProperties
//     (AI use, Guest access, who can manage tabs)
//   - adding people to a DM: createChildChannel (6ndL wb) →
//     conversations.createChild {channel, user_ids, migrate_history_start_ts}
//   - Hide: hideUserById (N0zR) closes the DM and saves hidden_users
//   - live presence for rows that show it (UserPresence)

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Permission names (Slack's permission enum keys, vendors qUia).
const (
	PermRenameChannel           = "RENAME_CHANNEL"
	PermArchiveChannel          = "ARCHIVE_CHANNEL"
	PermManageChannelPermission = "MANAGE_CHANNEL_PERMISSIONS"
	PermManageChannelPosting    = "MANAGE_CHANNEL_POSTING_PERMISSIONS"
	PermEditChannelAIExclusions = "EDIT_CHANNEL_AI_EXCLUSIONS"
	PermManageChannelTabs       = "MANAGE_CHANNEL_TABS"
)

// Account types of conversations.permissions.accountTypes.*.
const (
	AccountFullMember        = "FULL_MEMBER"
	AccountMultiChannelGuest = "MULTI_CHANNEL_GUEST"
)

// ChannelProps are the conversations.info properties the details
// Settings tab reads.
type ChannelProps struct {
	ExcludeFromAI    bool   `json:"exclude_from_slack_ai"`             // vz5j: AI use restricted
	RestrictGuests   bool   `json:"restrict_guests"`                   // PgTJ: guests not allowed
	RestrictSharing  bool   `json:"restrict_message_and_file_sharing"` // e4 I: no sharing or file downloads
	WhoCanManageTabs string `json:"who_can_manage_tabs"`               // "" (everyone) | channel_managers
}

// ParseChannelProps reads a conversation's properties object.
func ParseChannelProps(raw json.RawMessage) ChannelProps {
	var p ChannelProps
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	return p
}

// ChannelPermissions is fetchPermissions for one channel: which of perms
// the viewer may do there (permissions.info's allowed list). Slack treats
// a failed call as nothing allowed; so does the caller.
func (a *App) ChannelPermissions(ctx context.Context, ch string, perms ...string) (map[string]bool, error) {
	if len(perms) == 0 {
		return map[string]bool{}, nil
	}
	pj, _ := json.Marshal(perms)
	p := url.Values{"user_id": {a.S.SelfID()}, "permissions": {string(pj)}}
	if ch != "" && !strings.HasPrefix(ch, "D") { // Slack leaves channel_id off for IMs
		p.Set("channel_id", ch)
	}
	var res struct {
		Allowed []string `json:"allowed"`
	}
	if err := a.C.Call(ctx, "permissions.info", p, &res); err != nil {
		return map[string]bool{}, err
	}
	out := make(map[string]bool, len(perms))
	for _, id := range perms {
		out[id] = slices.Contains(res.Allowed, id)
	}
	return out, nil
}

// AccountTypePermission is one permission of the channel's Member or Guest
// permissions.
type AccountTypePermission struct {
	Permission   string
	Name         string // localized_permission_display_name
	Allowed      bool
	Configurable bool
}

// AccountTypePermissions is conversations.permissions.accountTypes.list,
// by account type (AccountFullMember, AccountMultiChannelGuest).
func (a *App) AccountTypePermissions(ctx context.Context, ch string) (map[string][]AccountTypePermission, error) {
	var res struct {
		AccountTypes []struct {
			AccountType string `json:"account_type"`
			Permissions []struct {
				Permission   string `json:"permission"`
				Configurable bool   `json:"is_configurable"`
				Allowed      bool   `json:"is_allowed"`
				Name         string `json:"localized_permission_display_name"`
			} `json:"permissions"`
		} `json:"account_types"`
	}
	if err := a.C.Call(ctx, "conversations.permissions.accountTypes.list", url.Values{"channel_id": {ch}}, &res); err != nil {
		return nil, err
	}
	out := map[string][]AccountTypePermission{AccountFullMember: {}, AccountMultiChannelGuest: {}}
	for _, at := range res.AccountTypes {
		if at.AccountType == "" {
			continue
		}
		var ps []AccountTypePermission
		for _, p := range at.Permissions {
			if p.Permission != "" {
				ps = append(ps, AccountTypePermission{Permission: p.Permission, Name: p.Name, Allowed: p.Allowed, Configurable: p.Configurable})
			}
		}
		out[at.AccountType] = ps
	}
	return out, nil
}

// SetAccountTypePermissions is conversations.permissions.accountTypes.set
// with the changed permissions ({permission, is_allowed}).
func (a *App) SetAccountTypePermissions(ctx context.Context, ch, accountType string, changed []AccountTypePermission) error {
	type perm struct {
		Permission string `json:"permission"`
		Allowed    bool   `json:"is_allowed"`
	}
	ps := make([]perm, 0, len(changed))
	for _, p := range changed {
		ps = append(ps, perm{p.Permission, p.Allowed})
	}
	pj, _ := json.Marshal(ps)
	return a.C.Call(ctx, "conversations.permissions.accountTypes.set", url.Values{
		"channel_id": {ch}, "account_type": {accountType}, "permissions": {string(pj)},
		"reason": {"set channel account type permissions"},
	}, nil)
}

// PropCan is one conversations.canSetProperties answer.
type PropCan struct {
	CanSet bool
	Reason string // e.g. scg_present, disallowed_action
}

// CanSetProperties asks whether the viewer may set props on ch.
func (a *App) CanSetProperties(ctx context.Context, ch string, props ...string) (map[string]PropCan, error) {
	pj, _ := json.Marshal(props)
	var res struct {
		Props []struct {
			Prop   string `json:"prop"`
			CanSet bool   `json:"can_set"`
			Reason string `json:"reason"`
		} `json:"props"`
	}
	if err := a.C.Call(ctx, "conversations.canSetProperties", url.Values{"channel_id": {ch}, "props": {string(pj)},
		"reason": {"channel-settings-can-set-properties"}}, &res); err != nil {
		return nil, err
	}
	out := map[string]PropCan{}
	for _, p := range res.Props {
		out[p.Prop] = PropCan{CanSet: p.CanSet, Reason: p.Reason}
	}
	return out, nil
}

// SetChannelProperties is conversations.setProperties (props sent as
// JSON, like Slack's jsonStringifyOrFallback). A channel in the sidebar is
// refreshed so the change shows everywhere.
func (a *App) SetChannelProperties(ctx context.Context, ch, reason string, props map[string]any) error {
	pj, err := json.Marshal(props)
	if err != nil {
		return err
	}
	if err := a.C.Call(ctx, "conversations.setProperties", url.Values{"channel_id": {ch}, "props": {string(pj)}, "reason": {reason}}, nil); err != nil {
		return err
	}
	if _, member := a.S.Conv(ch); member {
		a.refreshConv(ctx, ch)
	}
	return nil
}

// AddToConversation is Slack's MpdmAddPeopleModal submit: createChildChannel
// (conversations.createChild) makes the group DM of everyone in ch plus
// users, carrying over history from historyStart ("" for none). The new
// conversation is added to the sidebar.
func (a *App) AddToConversation(ctx context.Context, ch string, users []string, historyStart string) (Conv, error) {
	if len(users) == 0 {
		return Conv{}, errors.New("core: AddToConversation needs users")
	}
	p := url.Values{"channel": {ch}, "user_ids": {strings.Join(users, ",")}, "reason": {"add-to-mpdm"}}
	if historyStart != "" {
		p.Set("migrate_history_start_ts", historyStart)
	}
	var res struct {
		Channel slack.Conversation `json:"channel"`
	}
	if err := a.C.Call(ctx, "conversations.createChild", p, &res); err != nil {
		return Conv{}, err
	}
	c := res.Channel
	if c.ID == "" {
		return Conv{}, errors.New("core: conversations.createChild returned no channel")
	}
	if full, err := a.C.ConversationInfo(ctx, c.ID); err == nil {
		c = *full
	}
	a.EnsureUsers(ctx, append(append([]string(nil), users...), c.Members...))
	a.S.PutConv(c)
	a.openDM(c.ID)
	v := a.conv(&c)
	a.emit(Event{Type: "conv", Channel: c.ID, Conv: &v})
	return v, nil
}

// UserHidden reports whether the viewer hid user (hidden_users).
func (a *App) UserHidden(user string) bool { return slices.Contains(a.HiddenUsers(), user) }

// HideUser is hideUserById: the DM with user is closed if there is one,
// then user joins the hidden_users pref.
func (a *App) HideUser(ctx context.Context, user, dm string) error {
	if dm != "" {
		if err := a.Leave(ctx, dm); err != nil && !isCode(err, "channel_not_found", "not_in_channel") {
			return err
		}
	}
	ids := a.HiddenUsers()
	if !slices.Contains(ids, user) {
		ids = append(ids, user)
	}
	return a.SetPref(ctx, "hidden_users", strings.Join(ids, ","))
}

// UnhideUser takes user off the hidden_users pref.
func (a *App) UnhideUser(ctx context.Context, user string) error {
	ids := slices.DeleteFunc(a.HiddenUsers(), func(id string) bool { return id == user })
	return a.SetPref(ctx, "hidden_users", strings.Join(ids, ","))
}

// UserPresence is a user's current presence (active | away | "") as the
// realtime socket last reported it, without building the whole User:
// rows read it every frame so a presence_change shows at once.
func (a *App) UserPresence(id string) string {
	a.S.RLock()
	p := a.S.Presence[id]
	a.S.RUnlock()
	if p != "" {
		return p
	}
	if u, ok := a.S.User(id); ok && (u.Presence == "active" || u.Presence == "away") {
		return u.Presence
	}
	return ""
}
