package popups

import "github.com/some-du6e/clack/internal/i18n"

var nsInviteErrors = i18n.NS("invite-errors")

// InviteErrorText is getInviteErrorText (boot-shared xzuI, build 83aff18a;
// the invite-errors I18n) for what conversations.invite answers, as the
// add people modal's SUBMIT_ERROR shows it (the modal's call: not the
// admin tool, not an agent): n invitees that failed with code alike,
// team the viewer's workspace. "" for a code it has no text for: the
// modal's fallback, "Couldn’t add {n} people to the channel. Please try
// again." (InviteFailedText). Links are their text (the help articles,
// "Reach out to your admins").
func InviteErrorText(code string, n int, team string) string {
	if n < 1 {
		n = 1
	}
	switch code {
	case "not_enough_channels":
		return nsInviteErrors.T("This person is a Single-Channel Guest and can’t be added to a second channel.")
	case "already_in_channel":
		return nsInviteErrors.T("{inviteesCount, plural, =1 {User is} other {Users are}} already in this channel.", "inviteesCount", n)
	case "channel_not_found", "method_not_supported_for_channel_type":
		return nsInviteErrors.T("A valid channel name is required.")
	case "user_not_found":
		return nsInviteErrors.T("This user can’t be added.")
	case "user_is_restricted":
		return nsInviteErrors.T("{inviteesCount, plural, =1 {User is a Multi-Channel Guest} other {Users are Multi-Channel Guests}}. Only your Workspace Admin can add them to a channel.", "inviteesCount", n)
	case "org_user_not_in_team", "invitee_is_not_a_member_of_this_workspace":
		return nsInviteErrors.T("{inviteesCount, plural, =1 {This person isn’t} other {These people aren’t}} a member of any workspaces where this channel lives, so they can’t be added to it. You might want to try again from channel details.", "inviteesCount", n)
	case "too_many_users":
		return nsInviteErrors.T("You can only add up to 1,000 users at a time.")
	case "too_many_members":
		return i18n.Plain(nsInviteErrors.RT("This channel is only allowed a certain number of members, so you can’t add more people right now. <a>Reach out to your admins</a> if you have questions."))
	case "no_perms":
		return nsInviteErrors.T("You can’t invite {inviteesCount, plural, =1 {this person} other {these people}} based on your organization’s settings.", "inviteesCount", n)
	case "no_permission":
		return nsInviteErrors.T("You don’t have permission to invite people to this channel.")
	case "cant_invite":
		return nsInviteErrors.T("{inviteesCount, plural, =1 {This person} other {These people}} can’t be added to the channel because they may not have Slack Connect enabled, or may not have permission to use it.", "inviteesCount", n)
	case "restricted_action":
		return i18n.Plain(nsInviteErrors.RT("Your organization’s settings prevent you from using <strong>Slack Connect</strong> to invite people outside of {currentTeamName}.", "currentTeamName", team))
	case "not_owner":
		return nsInviteErrors.T("Only the channel owner can add people to this channel.")
	case "fatal_error":
		return i18n.Plain(nsInviteErrors.RT("<b>Something went wrong.</b> Sorry about that — please try again later, and contact Slack if this problem persists."))
	}
	return ""
}

// InviteFailedText is the add people modal's SUBMIT_ERROR for n invitees
// that failed with code: getInviteErrorText's, else its fallback.
func InviteFailedText(code string, n int, team string) string {
	if s := InviteErrorText(code, n, team); s != "" {
		return s
	}
	return i18n.T("channel_invite", "Couldn’t add {erroredCount, plural, =1 {# person} other {# people}} to the channel. Please try again.", "erroredCount", max(n, 1))
}
