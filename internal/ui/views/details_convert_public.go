package views

// Settings' "managing-this-channel" section with its "Change to a public
// channel" row and ConvertChannelToPublicModal (CDN build fa938a086569):
//
//   - additionalOptionsToShow (boot-shared Xihp) convertToPublic: under
//     fe_private_to_public_slack_connect_and_visual_updates (fNVg) it
//     shows for every private channel, disabled unless the viewer can
//     convert it (or when the channel's host org set it private); off, it
//     shows only for those who can. Can is vcMK YD: a private channel the
//     viewer isn't external-limited in, with CONVERT_PRIVATE_TO_PUBLIC
//     (permissions.info) and membership, or an enterprise admin or
//     workspace owner (7RIn yG).
//   - the settings-sections selector (jf5b) adds managing-this-channel
//     last when the row shows for a private channel;
//   - AdminChannelModal draws it as a ButtonField (a5gu) with the
//     "channel" icon; disabled, getDisabledTextPrivateToPublic is its text
//     ("You don’t have permission to change this channel to public");
//   - the modal: "Change to a public channel?", what changes (the
//     sharing line when enable_new_channel_settings is on and the channel
//     restricts sharing), Cancel and Change to Public (conversations.
//     convertToPublic); an is_idp_managed failure has its own text, any
//     other the generic one.
//
// The section's other rows are details_managing.go's (Change to a
// private channel, Archive channel for everyone, Unarchive, Delete this
// channel); the group DM and Salesforce conversions aren't drawn. A
// Slack Connect channel hosted by
// another org keeps today's Settings: Clack doesn't load the host's
// privacy (fedg s3), which picks the row's text there. Slack's thunk then
// marks the stored channel private (lfpR convertToPrivate, a slip the
// server's update corrects); Clack re-reads conversations.info instead.

import (
	"strings"

	"github.com/some-du6e/clack/internal/core"
)

const cdSecManaging = "managing-this-channel"

// cdToPublicOpt is convertToPublic for this channel.
type cdToPublicOpt struct {
	show, disabled bool
	text           string // the disabled row's reason
}

// toPublicOpt is additionalOptionsToShow's convertToPublic with
// AdminChannelModal's disabled text.
func (d *ChannelDetails) toPublicOpt(inf core.ChannelDetails) cdToPublicOpt {
	c := d.h.Core()
	if c == nil || cdGroup(d.kind) != "channel" {
		return cdToPublicOpt{}
	}
	cv := d.conv(inf, true)
	if cv.Kind != "private" || cv.IsArchived {
		return cdToPublicOpt{}
	}
	if inf.IsExtShared && d.hostedElsewhere(inf) {
		return cdToPublicOpt{} // the host's privacy isn't loaded (see above)
	}
	can := d.canConvertToPublic(inf)
	if !c.PrivateToPublicVisualUpdates() {
		return cdToPublicOpt{show: can}
	}
	o := cdToPublicOpt{show: true, disabled: !can}
	if o.disabled {
		o.text = "You don’t have permission to change this channel to public"
	}
	return o
}

// canConvertToPublic is vcMK YD for a private channel of the viewer's
// org.
func (d *ChannelDetails) canConvertToPublic(inf core.ChannelDetails) bool {
	c := d.h.Core()
	if u, ok := c.User(d.selfID()); ok && (u.EnterpriseAdmin || u.IsOwner) {
		return true
	}
	perms, _, _ := d.x.perms.get()
	return perms[core.PermConvertPrivateToPublic] && inf.IsMember
}

// toPublicRow is the section's ButtonField.
func (d *ChannelDetails) toPublicRow(inf core.ChannelDetails) W {
	return func(gtx C) D {
		o := d.toPublicOpt(inf)
		if !o.disabled && d.x.toPublicRow.Clicked(gtx) {
			d.openDialog(cdDlgToPublic)
		}
		spec := cdFieldSpec{titleText: "Change to a public channel", titleIcon: "channel", button: true, disabled: o.disabled}
		if o.text != "" {
			spec.text = func(gtx C) D { return d.para(gtx, 15, 22, wReg, cdDim(d.p.Text), 0, o.text) }
		}
		return d.cdFieldOf(gtx, &d.x.toPublicRow, spec)
	}
}

// toPublicBody is ConvertChannelToPublicModalContents' text.
func (d *ChannelDetails) toPublicBody(inf core.ChannelDetails, name string) []W {
	items := []string{"Anyone from your workspace (except guests) can see its message history and join."}
	if d.h.Core().Experiment("enable_new_channel_settings") == "on" && inf.Props.RestrictSharing {
		items = append(items, "Members will be able to share messages and download files from this channel.")
	}
	items = append(items, "If you make the channel private again, it’ll be visible to anyone who’s joined the channel up until that point.")
	body := []W{
		d.cdPara("pub1", "Keep in mind that when you make ", name, " a public channel:"),
		cdSpace(8),
		d.cdBullets(items...),
	}
	if e := d.x.dlg.err; e != "" {
		body = append(body, cdSpace(16), d.cdAlert(e, false))
	}
	return body
}

// toPublicError is the modal's error alert for a failed call.
func toPublicError(err error) string {
	if strings.Contains(err.Error(), "is_idp_managed") {
		return "This channel cannot be changed to public because it’s managed by an IDP group."
	}
	return "Something went wrong. Sorry about that — please try again later, and contact Slack if this problem persists."
}
