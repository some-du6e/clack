package popups

// Leaving a conversation as Slack's maybeLeaveChannel does it (boot-shared
// AdX+, web client 66e72b57ade1; core.LeaveChannelAction decides, the
// channel menus' Leave and /leave alike):
//
//   - a refusal (canUserLeaveChannelWithReason) is LeaveChannelError's
//     alert for its reason (leave_error.go);
//   - "leave" is leaveConversationApi (core.Leave: conversations.leave),
//     whose failure with an error code is LeaveChannelError's alert for
//     that code;
//   - "close" is closeChannelApi (core.CloseConversation);
//   - "confirm_and_leave" is LeaveChannel's confirmation ("Leave {group}");
//   - "confirm_leave_and_archive" is the archive dialog with "Leave and
//     archive" (openArchiveDialog({andLeave: true}), BaseArchiveDialog).
//
// Slack's leave and close never navigate (leaveChannel marks the channel
// left, channel_left drops it from the sidebar); Clack's take the viewer
// to Threads only from the conversation the primary view shows
// (PrimaryChannel).

import (
	"context"
	"errors"
	"image"
	"log"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// primaryHost is optional on the Host: the channel the primary view
// shows ("" when it isn't a channel).
type primaryHost interface{ PrimaryChannel() string }

// inView reports whether convID is the conversation the primary view
// shows.
func inView(h Host, convID string) bool {
	p, ok := h.(primaryHost)
	return ok && convID != "" && p.PrimaryChannel() == convID
}

// LeaveConversation is /leave's submitLeave for convID (core.LeaveCommand
// picked it): close, for a DM or group DM, is closeChannelApi; a channel
// goes through maybeLeaveChannel.
func LeaveConversation(h Host, convID string, close bool) {
	if close {
		actClose(h, convID)
		return
	}
	leaveChannel(h, convID)
}

// leaveChannel is maybeLeaveChannel for convID: its decision, at once
// from the store's facts, or off the UI goroutine once it needs the
// channel's membership counts (a private channel's, an agent's), then
// what it says.
func leaveChannel(h Host, convID string) {
	a := h.Core()
	if a == nil {
		h.Toast("Not connected yet")
		return
	}
	act, reason, counts, err := a.LeaveChannelActionNow(convID)
	if err != nil {
		return // logAndReject: a channel the store doesn't hold
	}
	if !counts {
		leaveDo(h, convID, act, reason)
		return
	}
	run(h, func(ctx context.Context) (string, error) {
		act, _, err := a.LeaveChannelAction(ctx, convID)
		return act, err
	}, func(act string, err error) {
		if err == nil {
			leaveDo(h, convID, act, "")
		}
	})
}

// leaveDo does maybeLeaveChannel's action for convID (reason: a refusal's).
func leaveDo(h Host, convID, action, reason string) {
	a := h.Core()
	if a == nil {
		return
	}
	switch action {
	case "":
		_, name := leaveTitleEntity(h, convID)
		h.ShowOverlay(LeaveChannelError(h, name, reason, func(ctx context.Context) error { return a.Leave(ctx, convID) }))
	case core.LeaveNow:
		actLeave(h, convID)
	case core.LeaveClose:
		actClose(h, convID)
	case core.LeaveConfirm:
		leaveChannelConfirm(h, convID)
	case core.LeaveConfirmArchive:
		h.ShowOverlay(NewLeaveArchiveDialog(h, convID))
	}
}

// leaveTitleEntity is the channel as the dialogs' titles draw it
// (InlineChannelEntity): its icon and name.
func leaveTitleEntity(h Host, convID string) (icon, name string) {
	var c core.Conv
	if a := h.Core(); a != nil {
		c, _ = a.Conv(convID)
	}
	name = convTitle(h, c)
	if name == "" {
		name = convID
	}
	icon = "hash"
	if c.Kind == "private" {
		icon = "lock"
	}
	return icon, name
}

// leaveParas draws paragraphs and a bulleted list after them (15/22).
func leaveParas(gtx layout.Context, th *theme.Theme, paras, points []string) layout.Dimensions {
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := 0
	line := func(x int, s string) int {
		d := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(w-x, 1), 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, s, 0)
		})
		return d.Size.Y
	}
	for i, p := range paras {
		if i > 0 {
			y += dp(gtx, 16)
		}
		y += line(0, p)
	}
	if len(points) > 0 {
		y += dp(gtx, 8)
	}
	for _, p := range points {
		at(gtx, image.Pt(dp(gtx, 8), y), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Regular, cl.Fg, "•")
		})
		y += line(dp(gtx, 24), p)
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// leaveChannelConfirm is LeaveChannel (the groups I18n): a c-sk-modal
// whose ModalTitleBar title is "Leave {group}" with the channel's
// InlineChannelEntity, its paragraph (a record channel's, else the
// private channel's; an externally limited member's, which Clack doesn't
// load, never), Cancel and the autofocused danger Leave Channel:
// leaveConversationApi, the dialog closing once it answers, or
// LeaveChannelError for its code.
func leaveChannelConfirm(h Host, convID string) {
	a := h.Core()
	icon, name := leaveTitleEntity(h, convID)
	pre, post := entityTitle(nsGroups.RT("Leave {group}", "group", "#"), "group")
	body := nsGroups.T("When you leave a private channel, you’ll no longer be able to see any of its messages. To rejoin this channel later, you’ll need to be invited.")
	if a != nil && a.IsRecordChannel(convID) {
		body = nsGroups.T("By leaving this channel, you may need to request to rejoin later.")
	}
	var self *Modal
	pending := false
	ok := &ModalButton{Label: nsGroups.T("Leave Channel"), Kind: BtnDanger, Enabled: func() bool { return !pending },
		Do: func() bool {
			if a == nil || pending {
				return false
			}
			pending = true
			leave := func(ctx context.Context) error { return a.Leave(slack.NoBackoff(ctx), convID) }
			run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, leave(ctx) },
				func(_ struct{}, err error) {
					pending = false
					self.Dismiss()
					if err != nil {
						LeaveFailed(h, convID, name, err, leave) // the code's LeaveChannelError, else logged
						return
					}
					leftView(h, convID)
				})
			return false
		}}
	th := h.Theme()
	self = NewModal(h, ModalOpts{Title: pre, Name: "LeaveChannel", TitleIcon: icon, TitleAfter: name + post, AutoFocus: ok,
		Body: func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Regular, colorsOf(th).Fg, body, 0)
		},
		Buttons: []*ModalButton{{Label: nsDialog.T("Cancel"), Kind: BtnOutline}, ok}})
	h.ShowOverlay(self)
}

// leftView takes the viewer to Threads when the conversation they left
// (or closed) is the one the primary view shows.
func leftView(h Host, convID string) {
	if inView(h, convID) {
		h.Navigate("threads")
	}
}

// actClose is closeChannelApi for a DM or group DM (core.CloseConversation:
// conversations.close, submitClose's "Close an MPIM, IM, or code
// channel"): Close conversation, and /leave in one.
func actClose(h Host, convID string) {
	a := h.Core()
	if a == nil {
		h.Toast("Not connected yet")
		return
	}
	run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.CloseConversation(ctx, convID) }, func(_ struct{}, err error) {
		if errors.Is(err, core.ErrNotClosable) {
			return // closeChannelApi's catch: nothing shows
		}
		if err != nil {
			log.Printf("popups: conversations.close: %v", err) // submitClose's failure: nothing catches it, no alert
			return
		}
		h.Toast("Conversation closed")
		leftView(h, convID)
	})
}

// actLeave is maybeLeaveChannel's leaveAndHandleErrors: leaveConversationApi,
// and LeaveChannelError for a failure's code (LeaveFailed, leave_error.go).
func actLeave(h Host, convID string) {
	a := h.Core()
	if a == nil {
		h.Toast("Not connected yet")
		return
	}
	icon, name := leaveTitleEntity(h, convID)
	msg := "You left #" + name
	if icon == "lock" {
		msg = "You left " + name
	}
	leave := func(ctx context.Context) error { return a.Leave(ctx, convID) }
	run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, leave(ctx) }, func(_ struct{}, err error) {
		if err != nil {
			LeaveFailed(h, convID, name, err, leave) // the code's LeaveChannelError, else logged
			return
		}
		h.Toast(msg)
		leftView(h, convID)
	})
}

// LeaveArchiveDialog is BaseArchiveDialog with andLeave (the channels
// I18n), which maybeLeaveChannel opens for a channel's last member: a
// DeprecatedDialog titled "Leave and archive {channelName}" (the
// channel's display name: "#name", a private one's bare name), what
// archiving does, "Are you sure you want to archive {channelName}?", and
// Cancel / "Yes, Leave & Archive the Channel": submitArchive (core.Archive:
// conversations.archive), the dialog closing once it answers
// (shouldCloseOnGo false); a failure shows under the text by code and
// disables the button.
type LeaveArchiveDialog struct {
	*Modal
	h       Host
	ch      string
	private bool
	pending bool
	err     string
}

// Error is the archive's failure text ("" for none).
func (d *LeaveArchiveDialog) Error() string { return d.err }

// NewLeaveArchiveDialog builds the dialog for ch; show it with
// h.ShowOverlay.
func NewLeaveArchiveDialog(h Host, ch string) *LeaveArchiveDialog {
	d := &LeaveArchiveDialog{h: h, ch: ch}
	icon, name := leaveTitleEntity(h, ch)
	d.private = icon == "lock"
	display := name
	if !d.private {
		display = "#" + name
	}
	goBtn := &ModalButton{Label: nsChannels.T("Yes, Leave & Archive the Channel"), Kind: BtnPrimary,
		Enabled: func() bool { return d.err == "" && !d.pending },
		Do:      func() bool { d.archive(); return false }}
	th := h.Theme()
	d.Modal = NewModal(h, ModalOpts{Title: nsChannels.T("Leave and archive {channelName}", "channelName", display), Name: "Connect(BaseArchiveDialog)",
		CDialog: true, CloseFirst: true, TruncateTitle: true, AutoFocus: goBtn, QuietFocus: true,
		Body: func(gtx layout.Context) layout.Dimensions {
			dims := leaveParas(gtx, th, []string{
				nsChannels.T("Archiving is useful if you don’t think you’ll need this channel any longer. Here’s what will happen when you archive this channel:"),
			}, []string{
				nsChannels.T("No one will be able to send messages to it"),
				nsChannels.T("Apps installed in this channel will be disabled"),
				nsChannels.T("A message will be posted that you archived the channel"),
			})
			y := dims.Size.Y + dp(gtx, 16)
			w := gtx.Constraints.Max.X
			for _, p := range []string{
				nsChannels.T("Messages from archived channels will still show up in search and archives, the same way they do today. You can always unarchive an archived channel later, if you need it again."),
				nsChannels.T("Are you sure you want to archive {channelName}?", "channelName", display),
			} {
				pd := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(w, 1), 1<<20)}
					return para(gtx, th, 15, 22, theme.Regular, colorsOf(th).Fg, p, 0)
				})
				y += pd.Size.Y + dp(gtx, 16)
			}
			y -= dp(gtx, 16) // the last is no_margin
			if d.err != "" {
				y += dp(gtx, 16) // margin_top_100
				ad := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
					return renameAlert(gtx, th, colorsOf(th), d.err, w)
				})
				y += ad.Size.Y
			}
			return layout.Dimensions{Size: image.Pt(w, y)}
		},
		Buttons: []*ModalButton{{Label: nsDialog.T("Cancel"), Kind: BtnOutline}, goBtn}})
	return d
}

// archive is handleGo: submitArchive, then closeModal, or the error.
func (d *LeaveArchiveDialog) archive() {
	a := d.h.Core()
	if a == nil || d.pending {
		return
	}
	d.pending = true
	ch := d.ch
	run(d.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.Archive(slack.NoBackoff(ctx), ch) },
		func(_ struct{}, err error) {
			d.pending = false
			if err == nil {
				d.Modal.Dismiss()
				return
			}
			d.err = LeaveArchiveErrorText(slackErrID(err), a.SelfIsWorkspaceAdmin(), d.private)
			d.h.Invalidate()
		})
}

// LeaveArchiveErrorText is BaseArchiveDialog's renderError with andLeave.
func LeaveArchiveErrorText(code string, admin, private bool) string {
	switch code {
	case "last_ra_channel":
		if admin {
			return nsChannels.T("Sorry, you can’t archive this channel because it is the only channel one of the guest account members belongs to. If you first disable the guest account, you will then be able to archive the channel.")
		}
		return nsChannels.T("Sorry, you can’t archive this channel because it is the only channel one of the guest account members belongs to.")
	case "restricted_action", "user_is_restricted":
		return nsChannels.T("Since you’re the last one here, you’ll need to invite an admin and ask them to archive the channel.")
	case "already_archived":
		return nsChannels.T("This channel was already archived.")
	case "cant_archive_general":
		return nsChannels.T("This channel cannot be archived.")
	}
	return nsChannels.T("Archiving failed with error: {error}", "error", code)
}
