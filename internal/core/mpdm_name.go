package core

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// MpdmCustomName is a group DM's custom name, when Slack shows one
// (client-boot tpcx `JM`): the conversation has
// properties.has_custom_mpdm_name, mpdm_naming is on, and for a Slack
// Connect group DM (is_ext_shared, 5DYq `ju`) sc_mpdm_naming is on too.
// Slack's setMpimName keeps the server's name for such a group DM instead
// of listing its members, so every place that names the conversation
// (sidebar, header, composer placeholder, switcher) shows it.
func (a *App) MpdmCustomName(id string) (string, bool) {
	c, ok := a.S.Conv(id)
	if !ok || !c.IsMPIM || c.Name == "" || !c.HasCustomMpdmName() {
		return "", false
	}
	if a.experiment("mpdm_naming") != "on" || (c.IsExtShared && a.experiment("sc_mpdm_naming") != "on") {
		return "", false
	}
	return c.Name, true
}

// MpdmHasCustomName is tpcx Ry: the group DM carries a custom name
// (properties.has_custom_mpdm_name), whatever the experiments say.
func (a *App) MpdmHasCustomName(id string) bool {
	c, ok := a.S.Conv(id)
	return ok && c.IsMPIM && c.HasCustomMpdmName()
}

// CanNameMpdm is when Slack offers to name a group DM: the header's
// EditMpdmNameButton and the details' MpdmConversationNameField both
// need mpdm_naming (tpcx kH), a group DM, a viewer who isn't a frontline
// worker (Fpkn q: restricted_user_type != "frontline_worker") and, for a
// Slack Connect group DM, sc_mpdm_naming (tpcx QW).
func (a *App) CanNameMpdm(id string) bool {
	c, ok := a.S.Conv(id)
	if !ok || !c.IsMPIM || a.experiment("mpdm_naming") != "on" {
		return false
	}
	if c.IsExtShared && a.experiment("sc_mpdm_naming") != "on" {
		return false
	}
	return !a.viewerFrontline()
}

// viewerFrontline is KBR4 q (u+N9 W): restricted_user_type is
// "frontline_worker".
func (a *App) viewerFrontline() bool {
	u, ok := a.S.User(a.S.SelfID())
	return ok && u.RestrictedUserType == "frontline_worker"
}

// MpdmNameMaxLen is PvOQ getMaxLength: names count up to 80 graphemes
// (dZPS getStringLength).
const MpdmNameMaxLen = 80

// HasMpdmWithDuplicateName is LHz7 hasMpdmWithDuplicateName: under
// mpdm_naming another of the viewer's group DMs already carries this
// custom name (trimmed, case-folded).
func (a *App) HasMpdmWithDuplicateName(name, except string) bool {
	n := foldName(name)
	if n == "" || a.experiment("mpdm_naming") != "on" {
		return false
	}
	a.S.RLock()
	defer a.S.RUnlock()
	for _, c := range a.S.Convs { // 6Z4k FS: every group DM the store has
		if c == nil || c.ID == except || !c.IsMPIM || !c.HasCustomMpdmName() {
			continue
		}
		if foldName(c.Name) == n {
			return true
		}
	}
	return false
}

// RenameMpdm is RenameMpdmDialog's save: renameChannelApi (Z3cFC) calls
// conversations.rename {channel, name, validate: true}; on ok, with
// mpdm_naming on, the returned group DM gets properties.has_custom_mpdm_name
// = (name != "") before it replaces the stored one. An empty name removes
// the custom name. Errors come back as Slack's error ids (invalid_name,
// restricted_action, …) for the dialog to word.
func (a *App) RenameMpdm(ctx context.Context, ch, name string) error {
	var res convResult
	if err := a.C.Call(ctx, "conversations.rename", url.Values{"channel": {ch}, "name": {name}, "validate": {"true"}}, &res); err != nil {
		return err
	}
	if res.Channel.ID != "" && a.experiment("mpdm_naming") == "on" && res.Channel.IsMPIM {
		res.Channel.Properties = withProperty(res.Channel.Properties, "has_custom_mpdm_name", name != "")
	}
	a.convChanged(ctx, ch, &res)
	return nil
}

// withProperty sets one key of a conversation's properties object.
func withProperty(raw json.RawMessage, key string, v any) json.RawMessage {
	m := map[string]json.RawMessage{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	b, _ := json.Marshal(v)
	m[key] = b
	out, _ := json.Marshal(m)
	return out
}

// foldName is LHz7's trim().toLowerCase().
func foldName(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
