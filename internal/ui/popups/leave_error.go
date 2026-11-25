package popups

import (
	"context"
	"errors"
	"image"
	"log"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// APICode is the error code of a Slack API answer with ok:false, and ""
// for any other error (a connection that failed, a timeout): Slack's
// `e?.data?.error`.
func APICode(err error) string {
	var se *slack.Error
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}

// LeaveFailed is what maybeLeaveChannel's LeaveChannel does when
// conversations.leave fails (boot-shared AdX+, build 83aff18a): an answer
// with an error code opens LeaveChannelError; anything else is thrown
// again, so nothing is shown (Clack logs it). leave is the call again, for
// the alert's Retry.
func LeaveFailed(h Host, convID, name string, err error, leave func(ctx context.Context) error) {
	code := APICode(err)
	if code == "" {
		log.Printf("popups: conversations.leave: %v", err)
		return
	}
	h.ShowOverlay(LeaveChannelError(h, name, code, leave))
}

// LeaveChannelError is Slack's LeaveChannelError (the channels I18n), in a
// DeprecatedAlertDialog (OK only, the title not truncated), by the leave's
// error code: last_member ("To leave this channel, it needs to be
// unshared"), cant_leave_general and cant_leave_mandatory_shared_channel
// ("Membership is required in {channelName}"), user_is_restricted,
// user_is_frontline, user_is_restricted_last_channel and
// user_is_ultra_restricted; any other code is the default, "Couldn’t
// leave {channelName}" with Cancel and Retry, which calls leave again
// (pending, the alert kept open) and closes it once that works, or shows
// the new code's alert.
func LeaveChannelError(h Host, name, code string, leave func(ctx context.Context) error) *Modal {
	th := h.Theme()
	reason, pending := code, false
	channel := name
	if channel != "" && channel[0] != '#' {
		channel = "#" + channel
	}
	var self core.User
	team, org := "", ""
	if a := h.Core(); a != nil {
		self = a.Self()
		a.S.RLock()
		team = a.S.TeamName
		a.S.RUnlock()
		org = a.SlackConnectTeamName() // the org's name on a grid
	}
	frontline := false
	if a := h.Core(); a != nil {
		frontline = a.Experiment("frontline_users") == "on"
	}
	text := func() leaveErrText {
		return leaveErrorText(reason, channel, team, org, self.IsAdmin || self.IsOwner, frontline)
	}
	isDefault := func() bool { return text().retry }
	title := func() string { return text().title }
	cl := colorsOf(th)
	p := func(gtx layout.Context, s string) layout.Dimensions {
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg, s, 0)
	}
	li := func(s string) layout.FlexChild {
		return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return para(loose(gtx, image.Pt(dp(gtx, 24), dp(gtx, 22))), th, 15, 22, theme.Regular, cl.Fg, "•", 1)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return p(gtx, s) }),
			)
		})
	}
	body := func(gtx layout.Context) layout.Dimensions {
		t := text()
		var kids []layout.FlexChild
		if t.body != "" {
			kids = append(kids, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return p(gtx, t.body) }))
		}
		if len(t.steps) > 0 {
			kids = append(kids, layout.Rigid(layout.Spacer{Height: 16}.Layout))
			for _, st := range t.steps {
				kids = append(kids, li(st))
			}
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, kids...)
	}
	var m *Modal
	retry := &ModalButton{Label: nsChannels.T("Retry"), Kind: BtnPrimary,
		Hidden:  func() bool { return !isDefault() },
		Loading: func() bool { return pending },
		Do: func() bool {
			if pending || leave == nil {
				return false
			}
			pending = true
			run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, leave(ctx) }, func(_ struct{}, err error) {
				pending = false
				if err == nil {
					m.Dismiss()
					return
				}
				if c := APICode(err); c != "" {
					reason = c
				} else {
					reason = "UNKNOWN" // onRetry: e?.data?.error || "UNKNOWN"
				}
			})
			return false // shouldCloseOnGo: false
		}}
	ok := &ModalButton{Label: nsDialog.T("OK"), Kind: BtnPrimary, Hidden: isDefault, Do: func() bool { return true }}
	m = NewModal(h, ModalOpts{
		TitleFunc: title, Name: "Connect(LeaveChannelError)", EnterSubmits: true,
		Body: body,
		Buttons: []*ModalButton{
			{Label: nsDialog.T("Cancel"), Kind: BtnOutline, Hidden: func() bool { return !isDefault() }},
			retry, ok,
		},
	})
	return m
}

// leaveErrText is what LeaveChannelError says for one reason: its title,
// its paragraph, a list of steps (last_member without admin rights), and
// whether it is the default alert (Cancel and Retry; else OK only).
type leaveErrText struct {
	title, body string
	steps       []string
	retry       bool
}

// leaveErrorText is LeaveChannelError's render by reason (the channels
// I18n): channel as "#name", team and org the viewer's workspace and
// org names, admin the viewer's rights, frontline the frontline_users
// experiment.
func leaveErrorText(reason, channel, team, org string, admin, frontline bool) leaveErrText {
	required := nsChannels.T("Membership is required in {channelName}", "channelName", channel)
	couldnt := nsChannels.T("Couldn’t leave {channelName}", "channelName", channel)
	switch reason {
	case "last_member":
		t := leaveErrText{title: nsChannels.T("To leave this channel, it needs to be unshared")}
		if admin {
			t.body = nsChannels.T("Since you’re the last person from {teamName}, you’ll need to stop sharing this channel before you can leave it.", "teamName", team)
		} else {
			t.body = nsChannels.T("Since you’re the last person from {teamName}, you’ll need an admin’s help to leave. It’s just a couple steps:", "teamName", team)
			t.steps = []string{nsChannels.T("Invite an admin to this channel."), nsChannels.T("Ask them to unshare and archive the channel.")}
		}
		return t
	case "cant_leave_general":
		return leaveErrText{title: required, body: nsChannels.T("Every workspace has one channel that contains all the members of this workspace — this is that channel for you.")}
	case "cant_leave_mandatory_shared_channel":
		return leaveErrText{title: required, body: nsChannels.T("Everyone in {orgName} is required to be a member of this channel.", "orgName", org)}
	case "user_is_restricted":
		return leaveErrText{title: couldnt, body: nsChannels.T("As a guest you can leave private channels, but you’ll need to ask an admin to remove you from any public channels.")}
	case "user_is_frontline":
		t := leaveErrText{title: couldnt}
		if frontline {
			t.body = nsChannels.T("As a frontline member you’ll need to ask an admin to remove you from channels.")
		}
		return t
	case "user_is_restricted_last_channel", "user_is_ultra_restricted":
		return leaveErrText{title: couldnt, body: nsChannels.T("On a guest account, you need to be a member of at least one channel.")}
	}
	return leaveErrText{title: couldnt, body: nsChannels.T("Hmm, something went wrong. Try again?"), retry: true}
}

// Toaster is any host's Toast.
type Toaster interface{ Toast(text string) }

// FailedToast is a write's failure where Slack's code has no text of its
// own for it: the error goes to the log (with what failed) and the toast
// is Slack's generic "Something went wrong. Please try again." (its most
// used failure text, as later_todos' BulkActionErrorToast and the agent
// task actions show it), never the error chain.
func FailedToast(h Toaster, what string, err error) {
	log.Printf("%s: %v", what, err)
	if h != nil {
		h.Toast(i18n.T("general", "Something went wrong. Please try again."))
	}
}
