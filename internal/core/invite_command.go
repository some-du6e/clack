package core

// Slack's local /invite (slash-commands-local QAQd inviteCommand, CDN
// build fa938a086569, 2026-11-13). The command never reaches
// chat.command in a channel: the client reads the people, user groups and
// channel it names and invites them itself.
//
//   - invite_slash_command_in_threads (7R+y Z) on: the first channel
//     named is the target (else this one; another workspace's channel
//     fails with "*/invite* can’t invite people to channels outside this
//     workspace."), and only the first person or group is invited unless
//     invite_slash_command_multi (7R+y w) is also on. Off: one person and
//     one channel at most, with Slack's texts for more.
//   - The permission checks first: canUserInviteToChannel (Qpr8 D),
//     canUserInviteInternalUsersToChannel (XMW3) and
//     canUserInviteExternalUsersToChannel (c2T7): in a private channel a
//     member needs INVITE_TO_CHANNEL / INVITE_EXTERNAL_TO_CHANNEL
//     (permissions.info) unless an admin; in a public one a member or an
//     admin may. Each refusal has its text.
//   - Nobody named: "A valid member name is required." (off), or the
//     channel's invite modal (on; Clack: Add people in the channel's
//     details).
//   - The Channel Expert (UCHANNELEXPERT): "*{agent}* can be added in
//     Channel Settings."
//   - One person: an app joins through apps.join-style add (Clack keeps
//     chat.command for apps), a private channel's member opens the invite
//     modal with them picked, anyone else is conversations.invite {channel,
//     users}; a user group is conversations.invite {channel, subteams}.
//     More (multi): a private channel's people go to the invite modal;
//     elsewhere each is invited, the failures joined into one text, thrown
//     when everyone failed and otherwise posted as Slackbot's sad_surprise
//     message.
//   - A failure's text is toFeedbackError's for the API's code (the
//     invite errors table below).
//
// A DM or a group DM keeps chat.command: Slack opens the group DM's
// add-people modal there, which Clack doesn't have from the composer.

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

// CmdCustom is Slack's CUSTOM_ERROR: a command whose failure text the
// client wrote (Feedback, shown as it is).
const CmdCustom = "CUSTOM_ERROR"

// Invite permissions (permissions.info).
const (
	PermInviteToChannel         = "INVITE_TO_CHANNEL"
	PermInviteExternalToChannel = "INVITE_EXTERNAL_TO_CHANNEL"
)

// channelExpertID is the Channel Expert agent (s8g3 jR).
const channelExpertID = "UCHANNELEXPERT"

var (
	inviteUserRe    = regexp.MustCompile(`<@([UW][A-Z0-9]+)(?:\|[^>]*)?>`)
	inviteGroupRe   = regexp.MustCompile(`<!subteam\^(S[A-Z0-9]+)(?:\|[^>]*)?>`)
	inviteChannelRe = regexp.MustCompile(`<#([CG][A-Z0-9]+)(?:\|[^>]*)?>`)
	inviteMentionRe = regexp.MustCompile(`<@[UW][A-Z0-9]+(?:\|[^>]*)?>|<!subteam\^S[A-Z0-9]+(?:\|[^>]*)?>`)
)

// InviteModalRequest is what the local /invite asks the shell for besides its
// calls: AddPeople opens the channel's invite modal (Add people) with
// AddUsers picked.
type InviteModalRequest struct {
	AddPeople string
	AddUsers  []string
}

func customErr(cmd, text string) *CommandError {
	return &CommandError{Command: cmd, Kind: CmdCustom, Feedback: text}
}

// isLocalInvite reports whether /invite runs locally in ch: a channel.
func (a *App) isLocalInvite(ch string) bool {
	k := a.kindOf(ch)
	return k == "channel" || k == "private"
}

// inviteCommand is inviteCommand for ch's channel kinds.
func (a *App) inviteCommand(ctx context.Context, o CommandOpts, cmd string) (CommandResult, error) {
	if o.ThreadTS != "" && !a.threadCommandOK(o.Channel, cmd, slack.Command{}) {
		// executeCommand's isSlashCommandEnabled, before any local command
		return CommandResult{}, &CommandError{Command: cmd, Kind: CmdUnsupportedInThread}
	}
	text := o.Text
	var ids []string // q: people and user groups, in order, once each
	seen := map[string]bool{}
	for _, m := range inviteMentionRe.FindAllString(text, -1) {
		id := ""
		if s := inviteUserRe.FindStringSubmatch(m); s != nil {
			id = s[1]
		} else if s := inviteGroupRe.FindStringSubmatch(m); s != nil {
			id = s[1]
		}
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	var chans []string // J
	for _, s := range inviteChannelRe.FindAllStringSubmatch(text, -1) {
		if !contains(chans, s[1]) {
			chans = append(chans, s[1])
		}
	}
	inThreads := a.experiment("invite_slash_command_in_threads") == "on"
	multi := len(ids) > 1 && inThreads && a.experiment("invite_slash_command_multi") == "on"
	el := ids
	if inThreads && !multi && len(el) > 1 {
		el = el[:1]
	}
	target := o.Channel
	if inThreads && len(chans) > 0 {
		target = chans[0]
	}
	otherWorkspace := func() *CommandError {
		if len(chans) == 0 {
			return nil
		}
		if c, ok := a.convInfo(chans[0]); ok && c.otherTeam {
			return customErr(cmd, "*"+cmd+"* can’t invite people to channels outside this workspace.")
		}
		return nil
	}
	if inThreads {
		if e := otherWorkspace(); e != nil {
			return CommandResult{}, e
		}
	}
	tc, known := a.convInfo(target)
	teamName := a.S.TeamName
	noExt := "You don’t have permission to invite external people to this channel."
	if known || !inThreads {
		perms := a.invitePerms(ctx, target, tc)
		canAny, canInt, canExt := a.canInvite(tc, perms)
		private := tc.private
		if private && !canInt && !canExt {
			return CommandResult{}, customErr(cmd, "You don’t have permission to invite people to this channel.")
		}
		if !inThreads {
			if e := legacyInviteChecks(a, cmd, el, chans, private, canInt, canExt, noExt, teamName); e != nil {
				return CommandResult{}, e
			}
		}
		if !canExt && anyOf(el, a.isExternalID) {
			return CommandResult{}, customErr(cmd, noExt)
		}
		if !canInt && anyOf(el, func(id string) bool { return !a.isExternalID(id) }) {
			return CommandResult{}, customErr(cmd, "You don’t have permission to invite people from *"+teamName+"* to this channel.")
		}
		if !canAny {
			return CommandResult{}, customErr(cmd, "You don’t have permission to invite people to this channel.")
		}
	}
	if !inThreads {
		if e := otherWorkspace(); e != nil {
			return CommandResult{}, e
		}
	}
	if len(el) == 0 {
		if !inThreads {
			return CommandResult{}, customErr(cmd, a.inviteErrorText("user_not_found", "", target))
		}
		// csQx $: a member who may invite, not a guest
		perms := a.invitePerms(ctx, target, tc)
		if any, _, _ := a.canInvite(tc, perms); !any || !tc.member || a.Self().Guest {
			return CommandResult{}, customErr(cmd, "You don’t have permission to invite people to this channel.")
		}
		return CommandResult{Invite: &InviteModalRequest{AddPeople: target}}, nil
	}
	for _, id := range el {
		if id == channelExpertID {
			name := "this agent"
			if u, ok := a.User(id); ok && u.IsBot && u.Name != "" {
				name = firstNonEmptyS(u.DisplayName, u.Name)
			}
			return CommandResult{}, customErr(cmd, "*"+name+"* can be added in Channel Settings.")
		}
	}
	invite := func(id string) (*InviteModalRequest, error) {
		if strings.HasPrefix(id, "S") {
			return nil, a.inviteGroup(ctx, cmd, target, id)
		}
		u, ok := a.User(id)
		if !ok {
			return nil, customErr(cmd, a.inviteErrorText("user_not_found", id, target))
		}
		switch {
		case u.Deleted:
			return nil, customErr(cmd, a.inviteErrorText("deactivated_user", id, target))
		case u.IsBot:
			// an app: Slack's addAppToChannel; Clack leaves it to the server
			_, err := a.serverCommand(ctx, cmd, CommandOpts{Channel: o.Channel, Command: cmd, Text: "<@" + id + ">", ThreadTS: o.ThreadTS})
			return nil, err
		case tc.private:
			return &InviteModalRequest{AddPeople: target, AddUsers: []string{id}}, nil
		}
		err := a.C.Call(ctx, "conversations.invite", url.Values{"channel": {target}, "users": {id}}, nil)
		if err != nil {
			return nil, customErr(cmd, a.inviteErrorText(ErrCode(err), id, target))
		}
		return nil, nil
	}
	if len(el) == 1 {
		r, err := invite(el[0])
		return CommandResult{Invite: r}, err
	}
	var people []string // eC: a private channel's people (not apps)
	if tc.private {
		for _, id := range el {
			if u, ok := a.User(id); ok && !u.IsBot && strings.HasPrefix(id, "U") || strings.HasPrefix(id, "W") {
				people = append(people, id)
			}
		}
	}
	if multi && len(people) > 0 && len(people) < len(el) {
		return CommandResult{}, customErr(cmd, "To add people to a private channel, invite them separately from apps and user groups.")
	}
	var fails []string
	var res *InviteModalRequest
	if len(people) > 0 {
		var ok []string
		for _, id := range people {
			if u, have := a.User(id); !have {
				fails = append(fails, a.inviteErrorText("user_not_found", id, target))
			} else if u.Deleted {
				fails = append(fails, a.inviteErrorText("deactivated_user", id, target))
			} else {
				ok = append(ok, id)
			}
		}
		if len(ok) > 0 {
			res = &InviteModalRequest{AddPeople: target, AddUsers: ok}
		}
	} else {
		var mu sync.Mutex
		var wg sync.WaitGroup
		errs := make([]error, len(el))
		for i, id := range el {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := invite(id)
				mu.Lock()
				errs[i] = err
				mu.Unlock()
			}()
		}
		wg.Wait()
		for _, err := range errs {
			if err == nil {
				continue
			}
			var ce *CommandError
			if errors.As(err, &ce) && ce.Feedback != "" {
				fails = append(fails, ce.Feedback)
			} else {
				fails = append(fails, "")
			}
		}
	}
	if len(fails) == 0 {
		return CommandResult{Invite: res}, nil
	}
	var uniq []string
	for _, f := range fails {
		if f != "" && !contains(uniq, f) {
			uniq = append(uniq, f)
		}
	}
	joined := strings.Join(uniq, "\n")
	if len(fails) == len(el) {
		return CommandResult{Invite: res}, customErr(cmd, joined)
	}
	// some went in: Slackbot's message with sad_surprise (local)
	a.LocalEphemeral(o.Channel, o.ThreadTS, joined)
	return CommandResult{Invite: res}, nil
}

// inviteGroup is inviteUserGroupToChannel: conversations.invite
// {channel, subteams}; already-in and self errors are fine.
func (a *App) inviteGroup(ctx context.Context, cmd, ch, group string) error {
	var res struct {
		Errors []struct {
			Error string `json:"error"`
		} `json:"errors"`
	}
	err := a.C.Call(ctx, "conversations.invite", url.Values{"channel": {ch}, "subteams": {group}}, &res)
	if err != nil {
		return customErr(cmd, a.inviteErrorText(ErrCode(err), group, ch))
	}
	for _, e := range res.Errors {
		switch e.Error {
		case "cant_invite_self", "already_in_channel":
			continue
		case "cant_invite_to_managed_channel":
			return customErr(cmd, a.inviteErrorText(e.Error, group, ch))
		case "org_user_not_in_team", "invitee_is_not_a_member_of_this_workspace":
			return customErr(cmd, a.inviteErrorText("some_users_were_not_added", group, ch))
		}
		return customErr(cmd, a.inviteErrorText(e.Error, group, ch))
	}
	return nil
}

// legacyInviteChecks are the checks without invite_slash_command_in_threads.
func legacyInviteChecks(a *App, cmd string, ids, chans []string, private, canInt, canExt bool, noExt, team string) *CommandError {
	someExt := anyOf(ids, a.isExternalID)
	someInt := anyOf(ids, func(id string) bool { return !a.isExternalID(id) })
	allExt := len(ids) > 0 && !someInt
	switch {
	case private && !canInt && canExt && len(ids) > 1 && someExt && someInt:
		return customErr(cmd, "You don’t have permission to invite people from *"+team+"* to this channel. If you would like to invite external people to this channel, please try again with one person at a time.")
	case len(ids) > 1 && !canExt:
		if allExt {
			return customErr(cmd, noExt)
		}
		return customErr(cmd, "You don’t have permission to invite external people to this channel. If you would like to invite people from *"+team+"* to this channel, please try again with one person at a time.")
	case len(ids) > 1 && !a.isExternalID(ids[0]) && !canInt:
		return customErr(cmd, "You don’t have permission to invite people from *"+team+"* to this channel.")
	case len(ids) > 1:
		return customErr(cmd, "*"+cmd+"* can only invite one user. Try the invite shortcut to add multiple users!")
	case len(chans) > 1:
		return customErr(cmd, "*"+cmd+"* can only invite a user to a single channel.")
	}
	return nil
}

// inviteConv is what the checks read about a channel.
type inviteConv struct {
	private, archived, member, otherTeam bool
}

func (a *App) convInfo(ch string) (inviteConv, bool) {
	c, ok := a.S.Conv(ch)
	if !ok {
		c, ok = a.S.ExtConv(ch)
	}
	if !ok {
		return inviteConv{}, false
	}
	other := false
	if t := c.ContextTeamID; t != "" && a.C != nil && t != a.C.Creds.Team && t != a.C.Creds.EnterpriseID {
		other = true
	}
	return inviteConv{private: c.IsPrivate || c.IsGroup, archived: c.IsArchived, member: c.IsMember, otherTeam: other}, true
}

// invitePerms reads INVITE_TO_CHANNEL and INVITE_EXTERNAL_TO_CHANNEL
// (permissions.info for the channel; a failure allows nothing).
func (a *App) invitePerms(ctx context.Context, ch string, c inviteConv) map[string]bool {
	p, _ := a.ChannelPermissions(ctx, ch, PermInviteToChannel, PermInviteExternalToChannel)
	return p
}

// canInvite is Qpr8 D, XMW3 and c2T7 for c.
func (a *App) canInvite(c inviteConv, perms map[string]bool) (any, internal, external bool) {
	self := a.Self()
	admin := self.IsAdmin || self.IsOwner || self.EnterpriseAdmin
	if c.archived || self.Guest {
		return false, false, false
	}
	if c.private {
		any = admin || c.member && (perms[PermInviteToChannel] || perms[PermInviteExternalToChannel])
		internal = any && (admin || c.member && perms[PermInviteToChannel])
	} else {
		any = admin || c.member
		internal = any
	}
	external = any && (admin || c.member && perms[PermInviteExternalToChannel])
	return any, internal, external
}

// isExternalID is pdt5 i: a member from outside the org.
func (a *App) isExternalID(id string) bool {
	if strings.HasPrefix(id, "S") {
		return false
	}
	u, ok := a.User(id)
	return ok && u.External
}

// normalizeInviteError is normalizeAPIFailures: the API's code as one
// of the invite errors (s8g3 IS).
func (a *App) normalizeInviteError(code string) string {
	switch code {
	case "cant_invite_self":
		return "already_in_channel_ug"
	case "subteam_not_found", "subteam_members_not_found":
		return "user_not_found_ug"
	case "method_not_supported_for_channel_type":
		return "invalid_channel"
	case "user_is_restricted":
		return "restricted_not_admin"
	case "ura_max_channels":
		return "ultra_restricted_max"
	case "user_is_ultra_restricted":
		if s := a.Self(); s.IsAdmin || s.IsOwner {
			return "ultra_restricted_max"
		}
		return "ultra_restricted_max_not_admin"
	}
	return code
}

// inviteErrorText is toFeedbackError's text for an invite failure (an
// API code, normalized first).
func (a *App) inviteErrorText(code, id, ch string) string {
	code = a.normalizeInviteError(code)
	name := a.inviteName(id)
	team := a.S.TeamName
	chName := ""
	if c, ok := a.S.Conv(ch); ok {
		chName = c.Name
		if c.IsPrivate {
			chName = "#" + chName
		}
	}
	switch code {
	case "already_in_channel":
		return name + " is already in this channel."
	case "already_in_channel_ug":
		return "All of the users in that user group are already in this channel."
	case "channel_not_found", "invalid_channel":
		return "A valid channel name is required."
	case "user_not_found", "user_not_found_ug":
		return "A valid member name is required."
	case "no_permission":
		return "You don’t have permission to invite people from *" + team + "* to this channel."
	case "no_external_invite_permission":
		return "You don’t have permission to invite external people to this channel."
	case "cant_invite_to_managed_channel":
		if strings.HasPrefix(id, "S") {
			return "It looks like this user group has some users who aren’t in an IDP group connected to " + chName + ", so it’s not possible to add them to this channel."
		}
		return "It looks like *" + name + "* isn’t in an IDP group connected to " + chName + ", so it’s not possible to add them to this channel."
	case "org_user_not_in_team", "invitee_is_not_a_member_of_this_workspace":
		if strings.HasPrefix(id, "S") {
			return "We can’t add anyone from *" + name + "* right now. They’ll need to join an associated workspace before they can access this channel."
		}
		return "*" + name + "* is not a member of " + team + "."
	case "some_users_were_not_added":
		return "We only added some members of *" + name + "* to this channel. Unfortunately, we couldn’t add everyone because they need to be a member of an associated workspace."
	case "deactivated_user":
		return "That user has been deactivated."
	case "restricted_not_admin":
		return "*" + name + "* is a Multi-Channel Guest. Only your Workspace Admin can invite them to a public channel."
	case "ultra_restricted_max":
		url := ""
		if a.C != nil {
			url = a.C.Creds.TeamURL
		}
		return "*" + name + "* is a Single-Channel Guest and can’t be added to a second channel. To invite " + name + " to this channel, you’ll need to <" + url + "admin#restricted|upgrade their membership> to a Multi-Channel Guest. \u200bNote: This will add a billable seat to your workspace."
	case "ultra_restricted_max_not_admin":
		return "*" + name + "* is a Single-Channel Guest and can’t be added to a second channel. Your Workspace Admin can upgrade their membership to a Multi-Channel Guest."
	case "not_owner", "external_user_cannot_join_channel", "user_team_not_in_channel":
		return "*" + name + "* can’t be invited to this channel because they’re from outside your organization."
	case "profile_only_user":
		return "*" + name + "* can’t be invited to this channel because they’re not on Slack."
	case "app_access_restricted":
		return "You don’t have permission to add this app to the channel. Contact your workspace admin for access."
	case "app_cannot_join_channel":
		return "" // without slackforce_seamless_block_app_membership
	case "cant_invite":
		return "*" + name + "* can’t be invited to this channel right now."
	case "record_channels_not_enabled":
		return "*" + name + "* can’t be invited to this channel right now because they don’t have access to the record channels pilot feature. The product development team has been notified of this request, nothing more to do here!"
	case "restricted_and_channel_prohibits_guests":
		if a.experiment("enable_new_channel_settings") == "on" {
			return "This channel doesn’t allow guests"
		}
	}
	return "Hmm, something went wrong. Try again?"
}

// inviteName is getNameForInviteErrors: a member's name, or a user
// group's handle.
func (a *App) inviteName(id string) string {
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, "S") {
		if n := a.S.UsergroupName(id); n != "" {
			return n
		}
		return id
	}
	if u, ok := a.User(id); ok {
		return firstNonEmptyS(u.DisplayName, u.RealName, u.Name)
	}
	return id
}

func anyOf(ids []string, f func(string) bool) bool {
	for _, id := range ids {
		if f(id) {
			return true
		}
	}
	return false
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func firstNonEmptyS(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
