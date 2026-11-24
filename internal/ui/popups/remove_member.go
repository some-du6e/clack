package popups

// RemoveMemberDialog (gantry-v2-async-remove-member-dialog LUCM, build
// c07ab895506f), which openRemoveMemberDialog (boot-shared LBiM) opens
// from a channel details roster row's "Remove from channel"
// (views/details_agents_menu.go). It is a DeprecatedDialog (zMub iM):
//
//   - the title "Remove {memberName} from {channelName}?" (the member's
//     getPrefCompliantMemberName, vUCu sE; the channel as InlineChannelEntity
//     draws it: "#name", a private channel's bare name);
//   - one paragraph, the first that applies: with sc_remove_full_access_user
//     and an external member, RemoveMemberDialogContent ("{memberName} will
//     be removed from {channel}.", "Note that:" and its two points); in a
//     private record channel, "… may need to request to rejoin later."; in a
//     private channel (or for an external limited member or a channel
//     visitor, which Clack doesn't load), "… can only rejoin by invitation.";
//     otherwise "They’ll still be able to rejoin, or be added to the
//     conversation." Under agent_channel_header (M/qt Zc) the private
//     channel lines start "They’ll" where the off group says "This person
//     will", since the member may be an agent or app;
//   - Cancel and the danger "Remove". Remove runs submitKickFromChannel
//     (conversations.kick) and keeps the dialog open (shouldCloseOnGo
//     false, goButtonIsPending) until it answers: done, onRemove runs and
//     the dialog closes; failed, an error Alert under the paragraph says
//     why (renderError: restricted_action, not_in_channel, anything else)
//     and Remove stays disabled (goButtonIsDisabled: !!error).
//
// No go button is focused by autoFocus: DeprecatedDialog's
// focusOnFormElements finds no field and focuses the go button.
// isAgentforceChannelExpertAgent's agent dialog ("Remove agent from
// channel?") needs the Agentforce channel expert's app, which Clack
// doesn't read.

import (
	"context"
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/chanent"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var (
	// nsShared is RemoveMemberDialog's I18n namespace (LUCM w).
	nsShared = i18n.NS("shared")
	// nsEntDashboard is RemoveMemberDialogContent's (LUCM c).
	nsEntDashboard = i18n.NS("enterprise_dashboard")
)

// RemoveMemberCopy is the dialog's text.
type RemoveMemberCopy struct {
	Title string
	// Para is the paragraph; External content has Header, NoteHeader and
	// Notes instead.
	Para               string
	Header, NoteHeader string
	Notes              []string
	Go                 string
}

// RemoveMemberText picks the dialog's text for f (LUCM render).
func RemoveMemberText(f core.RemoveMemberFacts, memberName, channelName string) RemoveMemberCopy {
	c := RemoveMemberCopy{
		Title: nsShared.T("Remove {memberName} from {channelName}?", "memberName", memberName, "channelName", channelName),
		Go:    nsShared.T("Remove"),
	}
	switch {
	case f.ScRemoveFull && f.External: // renderExternalChannelMessage
		c.Header = nsEntDashboard.T("{memberName} will be removed from {channel}.", "memberName", memberName, "channel", channelName)
		c.NoteHeader = nsEntDashboard.T("Note that:")
		c.Notes = []string{
			nsEntDashboard.T("They can be re-invited to this channel."),
			nsEntDashboard.T("If this channel is public for {memberName}'s org, and they are still connected, they can rejoin.", "memberName", memberName),
		}
	case f.SfChannel && f.Private: // renderLimitedAccessRecordChannelMessage
		if f.AgentHeader {
			c.Para = nsShared.T("They’ll no longer have access to the channel and may need to request to rejoin later.")
		} else {
			c.Para = nsShared.T("This person will no longer have access to the channel and may need to request to rejoin later.")
		}
	case f.Private: // renderPrivateMessage
		if f.AgentHeader {
			c.Para = nsShared.T("They’ll no longer have access to the channel and can only rejoin by invitation.")
		} else {
			c.Para = nsShared.T("This person will no longer have access to the channel and can only rejoin by invitation.")
		}
	default: // renderPublicChannelMessage
		c.Para = nsShared.T("They’ll still be able to rejoin, or be added to the conversation.")
	}
	return c
}

// removeMemberError is renderError's text for a kick that failed with
// code (onError: the response's error, else "unknown_error").
func removeMemberError(code, memberName, channelName string) string {
	switch code {
	case "restricted_action":
		return nsShared.T("Hmm, looks like you don’t have permission to kick from channels. Please contact a Workspace Admin if {memberName} should no longer have access to your workspace.", "memberName", memberName)
	case "not_in_channel":
		return nsShared.T("{memberName} is not a member of {channelName}.", "memberName", memberName, "channelName", channelName)
	}
	return i18n.Plain(nsShared.RT("Something’s gone wrong, and we couldn’t remove <strong>{memberName}</strong> from {channelName}. We suspect this is only temporary. Try again in a bit?",
		"memberName", memberName, "channelName", channelName))
}

// RemoveMemberDialog is the open dialog.
type RemoveMemberDialog struct {
	*Modal
	Copy    RemoveMemberCopy
	pending bool
	err     string
}

// Error is the error Alert's text ("" when there is none).
func (d *RemoveMemberDialog) Error() string { return d.err }

// Pending reports a kick in flight.
func (d *RemoveMemberDialog) Pending() bool { return d.pending }

// OpenRemoveMemberDialog is openRemoveMemberDialog({channelId, memberId,
// onRemove}): it shows the dialog for member in ch; onRemove runs after
// the kick.
func OpenRemoveMemberDialog(h Host, ch, member string, onRemove func()) *RemoveMemberDialog {
	a := h.Core()
	var f core.RemoveMemberFacts
	memberName, channelName := member, ch
	if a != nil {
		f = a.RemoveMemberFacts(ch, member)
		if n, ok := a.S.MemberName(member); ok {
			memberName = n
		}
		channelName = chanent.Of(ch, a.InlineChannel(ch), nil).Title()
	}
	d := &RemoveMemberDialog{Copy: RemoveMemberText(f, memberName, channelName)}
	th := h.Theme()
	var self *Modal
	goBtn := &ModalButton{Label: d.Copy.Go, Kind: BtnDanger,
		Enabled: func() bool { return d.err == "" && !d.pending },
		Do: func() bool {
			if d.pending || a == nil {
				return false
			}
			d.pending = true
			run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.KickFromChannel(ctx, ch, member) },
				func(_ struct{}, err error) {
					d.pending = false
					if err != nil {
						code := core.ErrCode(err)
						if code == "" {
							code = "unknown_error"
						}
						d.err = removeMemberError(code, memberName, channelName)
						h.Invalidate()
						return
					}
					if onRemove != nil {
						onRemove()
					}
					self.Dismiss()
				})
			return false // shouldCloseOnGo false
		}}
	self = NewModal(h, ModalOpts{
		Title:      d.Copy.Title,
		Name:       "Connect(RemoveMemberDialog)",
		AutoFocus:  goBtn,
		QuietFocus: true,                // focusOnFormElements: the go button, no ring
		CloseFirst: true, CDialog: true, // DeprecatedDialog
		// no truncateTitle (gantry-v2-async-remove-member-dialog): the
		// default true cuts a long title with an ellipsis (.overflow_ellipsis)
		TruncateTitle: true,
		Body:          func(gtx layout.Context) layout.Dimensions { return d.body(gtx, th) },
		Buttons:       []*ModalButton{{Label: nsDialog.T("Cancel"), Kind: BtnOutline}, goBtn},
	})
	d.Modal = self
	h.ShowOverlay(d)
	return d
}

// body is the paragraph (or RemoveMemberDialogContent) and, after a
// failure, the error Alert (margin_top_100, margin_bottom_0).
// RemoveMemberDialogContent's styles (remove-member-dialog
// 116e25a7443ca66f.min.css): contentHeader and noteHeader 12px above what
// follows, the list (a ul, its disc markers in the 1.5rem indent) with no
// bottom margin.
func (d *RemoveMemberDialog) body(gtx layout.Context, th *theme.Theme) layout.Dimensions {
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := 0
	text := func(x int, s string) int {
		dims := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-x), 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, s, 0)
		})
		return dims.Size.Y
	}
	c := d.Copy
	if c.Header != "" {
		y += text(0, c.Header) + dp(gtx, 12)
		y += text(0, c.NoteHeader) + dp(gtx, 12)
		ind := dp(gtx, 24)
		for _, n := range c.Notes {
			r := dp(gtx, 3) // the disc marker, centred on the first line
			cx, cy := ind-dp(gtx, 12), y+dp(gtx, 11)
			rrect(gtx, image.Rect(cx-r, cy-r, cx+r, cy+r), r, cl.Fg)
			y += text(ind, n)
		}
	} else {
		y += text(0, c.Para)
	}
	if d.err != "" {
		y += dp(gtx, 16)
		y += actAlert(gtx, th, cl, 0, y, w, d.err) - dp(gtx, 16) // margin_bottom_0
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}
