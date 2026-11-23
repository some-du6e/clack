package core

// Settings' Message history and Export rows (CDN build 35b42de8b870,
// 2026-11-21: boot-shared uUSj additionalOptionsToShow's dataRetention and
// exportHistory, client-boot wqjU's getDataRetention / setDataRetention /
// submitChannelExport, admin-channel-modal oWlM's MessageRetentionModal
// and ExportChannelHistoryModal; views/details_retention.go draws them).
//
//   - dataRetention (Message history, last in the permissions section;
//     a DM or group DM gets the section for it alone, i+kN): the viewer's
//     team is paid (DZwJ isPaidTeam). On an Enterprise org (t5X0 r7), a
//     DM, group DM or org-shared channel (XDd5 M) takes the org's
//     unlocked allow_retention_override when it's set (TI4y D1
//     getUnlockedOrgPref), unless
//     enterprise_retention_allow_override_on_org_level_channels is on.
//     Otherwise: the team pref allow_retention_override isn't false for
//     the channel's workspaces (t1L8 JF canUserDoEnableStyleTeamPrefFor
//     Channel; oMux p reads the org pref, then the locked one, then the
//     team's, and unset counts as allowed), a public channel needs a
//     workspace owner or org admin (JM7a yG), the viewer isn't a guest
//     (HkVr DM: is_restricted) and the channel isn't archived (+NpH).
//   - exportHistory (the "export" section): channel_exports on, not a
//     guest, and a workspace admin of the channel's team (Bn/N Zo:
//     is_admin) or a conversation that isn't a public channel.
//   - the row's text (oWlM tU): getDataRetention (conversations.
//     getRetention {channel} → retention.retention_type and
//     retention_duration, parseInt, 0 when missing). The channel's own
//     setting (type ≠ 0) is shown when isChannelRetentionOverrideEnforced,
//     else the team's for the conversation type: group_retention_* for a
//     private channel, dm_retention_* for a DM or group DM, retention_*.
//   - setDataRetention: conversations.setRetention {channel,
//     retention_type, and retention_duration (days) only for type 1}.
//   - submitChannelExport: conversations.export {channel}; Slackbot
//     tells the viewer when the file is ready.
//
// Clack has one workspace's prefs, so "the channel's workspaces" is the
// viewer's.

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// The experiments the two rows read; the account is in neither (Slack's
// null, so no default).
const (
	ExpChannelExports    = "channel_exports"
	ExpRetentionOrgLevel = "enterprise_retention_allow_override_on_org_level_channels"
)

func init() {
	experimentReads[ExpChannelExports] = on
	experimentReads[ExpRetentionOrgLevel] = on
}

// Retention is a retention setting: a channel's (Type 0 the team's
// default, 1 delete after Duration days) or a team's (0 keep messages but
// not revisions, 1 keep everything, 2 delete after Duration days, 3 the
// same with compliance export).
type Retention struct {
	Type, Duration int
}

// ChannelRetention is getDataRetention: conversations.getRetention.
func (a *App) ChannelRetention(ctx context.Context, ch string) (Retention, error) {
	var r struct {
		Retention struct {
			Type     json.RawMessage `json:"retention_type"`
			Duration json.RawMessage `json:"retention_duration"`
		} `json:"retention"`
	}
	if err := a.C.Call(ctx, "conversations.getRetention", url.Values{"channel": {ch}}, &r); err != nil {
		return Retention{}, err
	}
	return Retention{Type: jsParseInt(r.Retention.Type), Duration: jsParseInt(r.Retention.Duration)}, nil
}

// SetChannelRetention is setDataRetention: conversations.setRetention.
// days is sent only for a custom period (type 1).
func (a *App) SetChannelRetention(ctx context.Context, ch string, typ, days int) error {
	v := url.Values{"channel": {ch}, "retention_type": {strconv.Itoa(typ)}}
	if typ == 1 {
		v.Set("retention_duration", strconv.Itoa(days))
	}
	return a.C.Call(ctx, "conversations.setRetention", v, nil)
}

// ExportChannel is submitChannelExport: conversations.export.
func (a *App) ExportChannel(ctx context.Context, ch string) error {
	return a.C.Call(ctx, "conversations.export", url.Values{"channel": {ch}}, nil)
}

// jsParseInt is parseInt(v, 10) over a JSON number or string (0 for
// anything else, where Slack would get NaN or the 0 default).
func jsParseInt(raw json.RawMessage) int {
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		return int(n)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		s = strings.TrimSpace(s)
		end := 0
		for end < len(s) && (s[end] >= '0' && s[end] <= '9' || end == 0 && (s[0] == '-' || s[0] == '+')) {
			end++
		}
		if i, err := strconv.Atoi(s[:end]); err == nil {
			return i
		}
	}
	return 0
}

// enterpriseOrg is isEnterpriseOrEnterpriseWorkspace for the viewer's team.
func (a *App) enterpriseOrg() bool { return a.C != nil && a.C.Creds.EnterpriseID != "" }

// teamPrefNotFalse is canUserDoEnableStyleTeamPrefForChannel for the
// viewer's workspace: oMux p's value (the org pref on an Enterprise
// workspace, then the locked one, then the team's) isn't false.
func (a *App) teamPrefNotFalse(name string) bool {
	var v json.RawMessage
	if a.enterpriseOrg() {
		if o := a.EnterpriseOrgPref(name); o != nil && string(o) != "null" {
			v = o
		} else if l := a.EnterpriseLockedPref(name); l != nil && string(l) != "null" {
			v = l
		}
	}
	if v == nil {
		v, _ = a.TeamPref(name)
	}
	return strings.TrimSpace(string(v)) != "false"
}

// MessageHistoryShown is dataRetention's shouldShow for inf.
func (a *App) MessageHistoryShown(inf ChannelDetails) bool {
	if !a.paidTeam() {
		return false
	}
	k := inf.Conv.Kind
	if a.enterpriseOrg() && (k == "im" || k == "mpim" || inf.IsOrgShared) && a.experiment(ExpRetentionOrgLevel) != "on" {
		if v := a.EnterpriseUnlockedPref("allow_retention_override"); v != nil { // !isUndefined
			return jsTruthy(v)
		}
	}
	if !a.teamPrefNotFalse("allow_retention_override") {
		return false
	}
	u, _ := a.User(a.S.SelfID())
	if k == "channel" && !(u.IsOwner || u.EnterpriseAdmin) {
		return false
	}
	return !u.Guest && !inf.Conv.IsArchived
}

// ExportHistoryShown is exportHistory's shouldShow for inf.
func (a *App) ExportHistoryShown(inf ChannelDetails) bool {
	if a.experiment(ExpChannelExports) != "on" {
		return false
	}
	u, _ := a.User(a.S.SelfID())
	if u.Guest {
		return false
	}
	return u.IsAdmin || inf.Conv.Kind != "channel"
}

// RetentionOverrideEnforced is isChannelRetentionOverrideEnforced: a
// channel's own retention applies (DMs, group DMs and org-shared
// channels always; else when the team lets admins override it or
// doesn't forbid overrides).
func (a *App) RetentionOverrideEnforced(inf ChannelDetails) bool {
	k := inf.Conv.Kind
	if k == "im" || k == "mpim" || inf.IsOrgShared || a.experiment(ExpRetentionOrgLevel) == "on" {
		return true
	}
	if v, _ := a.TeamPref("allow_admin_retention_override"); strings.TrimSpace(string(v)) == "1" {
		return true
	}
	v, _ := a.TeamPref("allow_retention_override")
	return strings.TrimSpace(string(v)) != "false"
}

// TeamRetention is useRetentionPrefs' team setting for a conversation of
// kind (channel | private | im | mpim).
func (a *App) TeamRetention(kind string) Retention {
	pre := ""
	switch kind {
	case "private":
		pre = "group_"
	case "im", "mpim":
		pre = "dm_"
	}
	t, _ := a.TeamPrefInt(pre + "retention_type")
	d, _ := a.TeamPrefInt(pre + "retention_duration")
	return Retention{Type: t, Duration: d}
}

// ShownRetention is the Message history row's setting: the channel's own
// when it has one and it's enforced, else the team's (oWlM tU).
func (a *App) ShownRetention(inf ChannelDetails, own Retention) Retention {
	if own.Type != 0 && a.RetentionOverrideEnforced(inf) {
		return own
	}
	return a.TeamRetention(inf.Conv.Kind)
}

// IsWorkspaceAdmin is isCurrentUserWorkspaceAdmin (Bn/N h3): the
// retention modal's memberIsAdminOrOwner.
func (a *App) IsWorkspaceAdmin() bool {
	u, _ := a.User(a.S.SelfID())
	return u.IsAdmin
}

// SetTeamPlan sets team.info's plan and enterprise id as boot reads them
// (isPaidTeam; tests outside core set them here).
func (a *App) SetTeamPlan(plan, enterpriseID string) { a.setTeamPlan(plan, enterpriseID) }
