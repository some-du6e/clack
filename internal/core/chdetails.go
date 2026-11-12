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
//   - hidden people's messages (gantry-v2 build d3de3a487b1c,
//     2026-11-12): isUserHidden (client-boot 0OuZ a), each message's
//     "Show anyway" (the messages store's isUnhidden, client-boot z+/K
//     setUnhiddenMessageStateInternal; reset by djmN unhideUserApi) and
//     HiddenMessageBody's direct-mention test (checkForMentionInMessage,
//     boot-shared c+16, as aOPx calls it)
//   - live presence for rows that show it (UserPresence)

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"

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

// UserHidden is isUserHidden (client-boot 0OuZ a): user is in the
// viewer's hidden_users, unless the org locked hide_person_opt_out on
// (getLockedOrgPref, 8wTz SI), which turns hiding off everywhere.
// Message rows ask every frame, so the pref's list is parsed once per
// value.
func (a *App) UserHidden(user string) bool {
	if user == "" || jsTruthy(a.EnterpriseLockedPref("hide_person_opt_out")) {
		return false
	}
	raw := a.PrefString("hidden_users")
	hs := a.hidden()
	hs.mu.Lock()
	defer hs.mu.Unlock()
	if hs.set == nil || hs.raw != raw {
		hs.raw, hs.set = raw, map[string]bool{}
		for _, id := range SearchExcludedChannels(raw) {
			hs.set[id] = true
		}
	}
	return hs.set[user]
}

// hiddenState is the hidden-people state message rows read: the parsed
// hidden_users and the messages shown anyway.
type hiddenState struct {
	mu  sync.Mutex
	raw string
	set map[string]bool
	// unhidden: channel/ts → the sender, for each message whose "Show
	// anyway" was clicked (Slack's msg.isUnhidden, kept in memory only)
	unhidden map[string]string
}

var hiddenIndex sync.Map // *App → *hiddenState

func (a *App) hidden() *hiddenState {
	v, _ := hiddenIndex.LoadOrStore(a, &hiddenState{})
	return v.(*hiddenState)
}

// MessageUnhidden is msg.isUnhidden: the viewer clicked "Show anyway" on
// the message at ch/ts.
func (a *App) MessageUnhidden(ch, ts string) bool {
	hs := a.hidden()
	hs.mu.Lock()
	defer hs.mu.Unlock()
	_, ok := hs.unhidden[ch+"/"+ts]
	return ok
}

// SetMessageUnhidden is HiddenMessageBody's two links: "Show anyway"
// (unhideMessage, z+/K FE: isUnhidden true) and "Hide message"
// (hideMessage, Wy with isUnhidden false). user is the sender, so
// UnhideUser can reset their messages.
func (a *App) SetMessageUnhidden(ch, ts, user string, on bool) {
	hs := a.hidden()
	hs.mu.Lock()
	defer hs.mu.Unlock()
	k := ch + "/" + ts
	if !on {
		delete(hs.unhidden, k)
		return
	}
	if hs.unhidden == nil {
		hs.unhidden = map[string]string{}
	}
	hs.unhidden[k] = user
}

// ResetUnhidden is unhideUserApi's first step: every message of user's
// shown anyway goes back to plain (getMessagesByProps {isUnhidden, user},
// each set isUnhidden false). Preferences' People you've hidden calls it
// before writing the pref itself.
func (a *App) ResetUnhidden(user string) {
	hs := a.hidden()
	hs.mu.Lock()
	defer hs.mu.Unlock()
	for k, u := range hs.unhidden {
		if u == user {
			delete(hs.unhidden, k)
		}
	}
}

// MessageFromHiddenSender is isMessageFromHiddenSender (client-boot
// eQJk): the sender is hidden and the message wasn't shown anyway.
func (a *App) MessageFromHiddenSender(m *Message) bool {
	return m != nil && a.UserHidden(m.User) && !a.MessageUnhidden(m.Channel, m.TS)
}

// jsTruthy is JavaScript's truthiness of a JSON value: anything but
// absent, null, false, 0 and "".
func jsTruthy(v json.RawMessage) bool {
	switch strings.TrimSpace(string(v)) {
	case "", "null", "false", "0", `""`:
		return false
	}
	return true
}

// usergroupMentionRe is a user group's mention in mrkdwn (client-boot
// jiA8 i_).
var usergroupMentionRe = regexp.MustCompile(`<!subteam\^(S[A-Za-z0-9]{8,})(\|[^<>]*)?>`)

// MentionsViewerDirectly is checkForMentionInMessage (boot-shared c+16)
// with the options HiddenMessageBody (aOPx) passes: @here, @channel,
// @everyone and highlight words don't count; the viewer's own mention
// and a mention of a user group they're in do. A message with blocks is
// read from its blocks, else from its text; then its attachments that
// aren't link previews or forwards. pinned_item messages never mention.
func (a *App) MentionsViewerDirectly(m *Message) bool {
	if m == nil || m.Subtype == "pinned_item" {
		return false
	}
	self := a.Self().ID
	text := func(t string) bool {
		if t == "" {
			return false
		}
		if self != "" && mentionsMember(t, self) {
			return true
		}
		for _, g := range usergroupMentionRe.FindAllStringSubmatch(t, -1) {
			if a.S.InUsergroup(g[1]) {
				return true
			}
		}
		return false
	}
	var blocks []json.RawMessage
	if len(m.Blocks) > 0 && json.Unmarshal(m.Blocks, &blocks) == nil && len(blocks) > 0 {
		if a.blocksMention(blocks, self, text, false) {
			return true
		}
	} else if text(m.Text) {
		return true
	}
	var atts []struct {
		FromURL     string            `json:"from_url"`
		IsForwarded bool              `json:"is_forwarded"`
		Title       string            `json:"title"`
		Pretext     string            `json:"pretext"`
		Text        string            `json:"text"`
		Footer      string            `json:"footer"`
		Blocks      []json.RawMessage `json:"blocks"`
		Fields      []struct {
			Value string `json:"value"`
		} `json:"fields"`
	}
	if len(m.Attachments) == 0 || json.Unmarshal(m.Attachments, &atts) != nil {
		return false
	}
	for _, at := range atts {
		if at.FromURL != "" || at.IsForwarded {
			continue
		}
		if text(at.Title) || text(at.Pretext) || text(at.Text) || text(at.Footer) || a.blocksMention(at.Blocks, self, text, true) {
			return true
		}
		for _, f := range at.Fields {
			if text(f.Value) {
				return true
			}
		}
	}
	return false
}

// mentionsMember is getMemberMentionRegExp(id).test(t): "<@id" followed
// by a word boundary.
func mentionsMember(t, id string) bool {
	for i := 0; ; {
		j := strings.Index(t[i:], "<@"+id)
		if j < 0 {
			return false
		}
		end := i + j + 2 + len(id)
		if end == len(t) || !isWordByte(t[end]) {
			return true
		}
		i = end
	}
}

func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// blocksMention is checkForMentionInBlocks: a rich_text block's user and
// usergroup elements (unless noRich: an attachment's blocks), a
// section's mrkdwn text and fields, a header's text and a context's
// elements when they are mrkdwn.
func (a *App) blocksMention(blocks []json.RawMessage, self string, text func(string) bool, noRich bool) bool {
	type textObj struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	mrkdwn := func(t *textObj) bool { return t != nil && t.Type == "mrkdwn" && text(t.Text) }
	for _, raw := range blocks {
		var b struct {
			Type     string            `json:"type"`
			Text     *textObj          `json:"text"`
			Fields   []textObj         `json:"fields"`
			Elements []json.RawMessage `json:"elements"`
		}
		if json.Unmarshal(raw, &b) != nil {
			continue
		}
		switch b.Type {
		case "rich_text":
			if !noRich && a.richMention(b.Elements, self) {
				return true
			}
		case "section":
			if mrkdwn(b.Text) {
				return true
			}
			for i := range b.Fields {
				if mrkdwn(&b.Fields[i]) {
					return true
				}
			}
		case "header":
			if mrkdwn(b.Text) {
				return true
			}
		case "context":
			for _, e := range b.Elements {
				var t textObj
				if json.Unmarshal(e, &t) == nil && mrkdwn(&t) {
					return true
				}
			}
		}
	}
	return false
}

// richMention walks rich text elements (getEntitiesFromBlocks, YHGi nK)
// for a user element naming the viewer or a usergroup element of a group
// they're in.
func (a *App) richMention(els []json.RawMessage, self string) bool {
	for _, raw := range els {
		var e struct {
			Type        string            `json:"type"`
			UserID      string            `json:"user_id"`
			UsergroupID string            `json:"usergroup_id"`
			Elements    []json.RawMessage `json:"elements"`
		}
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		switch e.Type {
		case "user":
			if self != "" && e.UserID == self {
				return true
			}
		case "usergroup":
			if a.S.InUsergroup(e.UsergroupID) {
				return true
			}
		}
		if len(e.Elements) > 0 && a.richMention(e.Elements, self) {
			return true
		}
	}
	return false
}

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

// UnhideUser is unhideUserApi (boot-shared djmN): user's messages shown
// anyway go back to plain (isUnhidden false), then user comes off the
// hidden_users pref.
func (a *App) UnhideUser(ctx context.Context, user string) error {
	a.ResetUnhidden(user)
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
