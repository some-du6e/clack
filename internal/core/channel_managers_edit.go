package core

// The channel details roster's channel manager actions, from Slack's web
// client build 846030e1ce97 (2026-11-17, the public CDN without a cookie;
// docs/CHANNELS-AUDIT.md "Member actions"):
//
//   - IaDetailsPopoverMemberItemActionsMenu (ia-details-popover-members
//     chunk VSHD) offers "Remove from Channel Managers" when the viewer
//     may edit the channel's managers (canCurrentUserEditChannelManagers:
//     client-boot yfw4 B, channel_managers.go's canEditChannelManagers)
//     and the member is one (isMemberChannelManager, DIay le: the managers
//     store), and "Make Channel Manager" when the viewer may and the member
//     isn't one, isn't a guest (p+Mq P: is_restricted), isn't external
//     (QjNl g) and isn't a channel visitor here (3Wpu Oh). Under
//     agent_channel_header (0TY2 Zc) neither shows for an app, a bot
//     (id+5 isAppOrBot) or the native channel expert (FbgX h).
//   - submitAddChannelManagers (rwVl L) sends admin.roles.addMembers
//     (boot-vendors Fnkp) {role_id Rl0A, role_scopes [channel], user_ids,
//     reason add-channel-managers}, ten users a request; the users each
//     request took join the store (addChannelManagers, zbvs $: the union),
//     and when any failed (failed_for_some_users names them in
//     invalid_user_ids, any other error fails the request's users) it
//     rejects with UpdateChannelManagersError.
//   - submitRemoveChannelManagers (HVho C) is the same over
//     admin.roles.removeMembers (bUDL) with reason remove-channel-managers;
//     the users it took leave the store (removeChannelManagers, zbvs Co).
//
// Slack's fetcher (G8am createApiMethodFetcher) snake-cases the args and
// getFormData (L9gf) appends a string array as its comma-joined text.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Channel manager writes' reasons (rwVl, HVho) and the batch size their
// chunking (nz/X A(userIds, 10)) uses.
const (
	addChannelManagersReason    = "add-channel-managers"
	removeChannelManagersReason = "remove-channel-managers"
	channelManagersBatch        = 10
)

// ExpHallPass is fvOF uE's experiment: channel visitors.
const ExpHallPass = "hall_pass"

// CanEditChannelManagers is canCurrentUserEditChannelManagers for ch
// (yfw4 B over the stored channel): never blocks; false until the
// ASSIGN_CHANNEL_MANAGER grant answers, unless the viewer acts as a
// workspace admin there.
func (a *App) CanEditChannelManagers(ch string) bool {
	c, ok := a.removeConv(ch)
	if !ok {
		return false
	}
	return a.canEditChannelManagers(ch, a.isMemberConv(c))
}

// IsMemberChannelManager is isMemberChannelManager (DIay le): the
// managers store lists member for ch.
func (a *App) IsMemberChannelManager(ch, member string) bool {
	return member != "" && slices.Contains(a.ChannelManagerIDs(ch), member)
}

// isChannelVisitor is isMemberChannelVisitorInChannel (3Wpu Oh): under
// hall_pass, the channel's properties.channel_visitor_ids lists member.
func (a *App) isChannelVisitor(c *slack.Conversation, member string) bool {
	if a.experiment(ExpHallPass) != "on" || c == nil || member == "" || len(c.Properties) == 0 {
		return false
	}
	var p struct {
		Visitors []string `json:"channel_visitor_ids"`
	}
	_ = json.Unmarshal(c.Properties, &p)
	return slices.Contains(p.Visitors, member)
}

// managerItemsHidden is the actions menu's y && j: agent_channel_header
// on and the member an app, a bot or the native channel expert.
func (a *App) managerItemsHidden(member string, mem *slack.User) bool {
	return a.AgentChannelHeader() && (member == channelExpertID || isAppOrBotUser(mem))
}

// CanRemoveFromChannelManagers is the actions menu's q for member in ch:
// "Remove from Channel Managers".
func (a *App) CanRemoveFromChannelManagers(ch, member string) bool {
	if !a.CanEditChannelManagers(ch) || !a.IsMemberChannelManager(ch, member) {
		return false
	}
	mem, _ := a.S.User(member)
	return !a.managerItemsHidden(member, mem)
}

// CanMakeChannelManager is the actions menu's T for member in ch: "Make
// Channel Manager".
func (a *App) CanMakeChannelManager(ch, member string) bool {
	if member == "" || !a.CanEditChannelManagers(ch) || a.IsMemberChannelManager(ch, member) {
		return false
	}
	c, _ := a.removeConv(ch)
	mem, _ := a.S.User(member)
	if mem != nil && (mem.IsRestricted || a.userOf(mem).External) {
		return false
	}
	return !a.isChannelVisitor(c, member) && !a.managerItemsHidden(member, mem)
}

// UpdateChannelManagersError is the thunks' rejection
// ({name: "UpdateChannelManagersError", message, failedUserIds,
// succededUserIds}).
type UpdateChannelManagersError struct {
	Message   string // error_adding_channel_managers | error_removing_channel_managers
	Failed    []string
	Succeeded []string
	Err       error // the last request's error
}

func (e *UpdateChannelManagersError) Error() string { return e.Message }
func (e *UpdateChannelManagersError) Unwrap() error { return e.Err }

// AddChannelManagers is submitAddChannelManagers (rwVl L).
func (a *App) AddChannelManagers(ctx context.Context, ch string, users []string) error {
	ok, err := a.channelManagerWrites(ctx, "admin.roles.addMembers", addChannelManagersReason, ch, users)
	a.updateChannelManagers(ch, func(cur []string) []string { return uniqStrings(append(cur, ok...)) })
	if err != nil {
		err.Message, err.Succeeded = "error_adding_channel_managers", ok
		return err
	}
	return nil
}

// RemoveChannelManagers is submitRemoveChannelManagers (HVho C).
func (a *App) RemoveChannelManagers(ctx context.Context, ch string, users []string) error {
	ok, err := a.channelManagerWrites(ctx, "admin.roles.removeMembers", removeChannelManagersReason, ch, users)
	if len(ok) > 0 {
		a.updateChannelManagers(ch, func(cur []string) []string {
			return slices.DeleteFunc(cur, func(u string) bool { return slices.Contains(ok, u) })
		})
	}
	if err != nil {
		err.Message, err.Succeeded = "error_removing_channel_managers", ok
		return err
	}
	return nil
}

// channelManagerWrites sends method for users in batches and sorts them
// into those the requests took and those they failed (the thunks'
// allSettled pass); err is nil when none failed.
func (a *App) channelManagerWrites(ctx context.Context, method, reason, ch string, users []string) ([]string, *UpdateChannelManagersError) {
	var ok, failed []string
	var last error
	for i := 0; i < len(users); i += channelManagersBatch {
		batch := users[i:min(i+channelManagersBatch, len(users))]
		p := url.Values{"role_id": {channelManagerRole}, "role_scopes": {ch}, "user_ids": {strings.Join(batch, ",")}, "reason": {reason}}
		err := a.C.Call(ctx, method, p, nil)
		if err == nil {
			ok = append(ok, batch...)
			continue
		}
		last = err
		var se *slack.Error
		if errors.As(err, &se) && se.Code == "failed_for_some_users" {
			var body struct {
				Invalid []string `json:"invalid_user_ids"`
			}
			_ = json.Unmarshal(se.Body, &body)
			failed = append(failed, body.Invalid...)
			for _, u := range batch {
				if !slices.Contains(body.Invalid, u) {
					ok = append(ok, u)
				}
			}
			continue
		}
		failed = append(failed, batch...)
	}
	if len(failed) > 0 {
		return ok, &UpdateChannelManagersError{Failed: failed, Err: last}
	}
	return ok, nil
}

// updateChannelManagers rewrites ch's managers in the store (zbvs's
// addChannelManagers and removeChannelManagers) through setChannelManagers.
func (a *App) updateChannelManagers(ch string, f func(cur []string) []string) {
	a.setChannelManagers(ch, f(a.ChannelManagerIDs(ch)))
}

// uniqStrings keeps each string's first occurrence (lodash uniq).
func uniqStrings(s []string) []string {
	out := s[:0:0]
	for _, v := range s {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}
