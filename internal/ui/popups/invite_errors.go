package popups

import "github.com/some-du6e/clack/internal/i18n"

// InviteFailedText is the add people modal's SUBMIT_ERROR for n invitees
// that conversations.invite refused with code, team the viewer's
// workspace: getInviteErrorText's words (inviteErrorText), else the
// modal's fallback, "Couldn’t add {n} people to the channel. Please try
// again."
func InviteFailedText(code string, n int, team string) string {
	if s := inviteErrorText(code, n, team); s != "" {
		return s
	}
	return i18n.T("channel_invite", "Couldn’t add {erroredCount, plural, =1 {# person} other {# people}} to the channel. Please try again.", "erroredCount", max(n, 1))
}
