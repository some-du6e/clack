package views

// Settings' managing-this-channel, the rest of its rows (CDN build
// fa938a086569, 2026-11-13; boot-shared Xihp additionalOptionsToShow,
// admin-channel-modal oWlM AdminChannelModal; details_convert_public.go
// has Change to a public channel). In AdminChannelModal's order:
//
//   - Change to a private channel (lock): a public channel the viewer
//     may convert (8gbq M: not #general, not an org default channel, not
//     archived; CONVERT_TO_PRIVATE, or CONVERT_TO_PRIVATE_UNDER_PREFS
//     with the team's CREATE_PRIVATE_CHANNEL). It opens
//     ConvertChannelToPrivateModal (3cXf): "Change to a private
//     channel?", what changes, Cancel / Change to Private (danger;
//     conversations.convertToPrivate {channel}); a failure shows
//     "<b>Something went wrong.</b> …".
//   - Change to a public channel (details_convert_public.go).
//   - Archive channel for everyone (archive, danger; UCGZ): 0spU's rule
//     (a public or private channel, not #general or org-mandatory, not
//     archived, not hosted by another org, the viewer a member or an
//     admin, and ARCHIVE_CHANNEL; an externally shared channel this org
//     hosts also needs 12SF's disconnect rule, which Clack reads as the
//     viewer being an admin). It opens ArchiveChannelModal (wpma, 1GTg):
//     "Archive this channel?", what happens, Cancel / Archive Channel
//     (danger; conversations.archive {channel}) and the error texts by
//     code; done, the details close.
//   - Unarchive this channel (unarchive): an archived channel where
//     permissions.info grants UNARCHIVE_CHANNEL (Tq8O). No modal: submitUnarchive
//     (conversations.unarchive), then the details close. An archived
//     channel's Settings is this section alone (jf5b).
//   - Delete this channel (trash, danger): a public or private channel,
//     not #general or org-mandatory, not an org default channel, with
//     DELETE_CHANNEL; disabled while externally shared ("To delete this
//     channel, first make sure it’s not connected to another
//     organization."). DeleteChannelModal (ew/6): "Delete this channel?",
//     what happens ("You can archive a channel instead …" when Archive
//     shows), the "Yes, permanently delete the channel" checkbox that
//     enables Delete Channel (danger; conversations.delete {channel}).
//
// Not here: the group DM conversions (convertMpimToPrivate, the Slack
// Connect group DM's), Change limited access group and the Salesforce
// record channel rows (Clack has no record channels), and the org
// template clean-up flow that replaces the archive and delete modals
// where solution templates are allowed (WD+P g; not on this org). The
// delete row's "connected to another organization" link (the disconnect
// modal) is drawn as text.

import (
	"context"
	"strings"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
)

// cdManageOpts are additionalOptionsToShow's rows for this conversation.
type cdManageOpts struct {
	toPrivate, archive, unarchive bool
	del, delDisabled              bool
}

func (o cdManageOpts) any() bool { return o.toPrivate || o.archive || o.unarchive || o.del }

// manageOpts reads the rows' rules.
func (d *ChannelDetails) manageOpts(inf core.ChannelDetails) cdManageOpts {
	c := d.h.Core()
	var o cdManageOpts
	if c == nil || cdGroup(d.kind) != "channel" {
		return o
	}
	cv := d.conv(inf, true)
	if cv.Kind != "channel" && cv.Kind != "private" {
		return o
	}
	perms, _, _ := d.x.perms.get()
	team, _, _ := d.x.teamPerms.get()
	general := cv.IsGeneral || inf.OrgMandatory // aK5v gx
	orgDefault := inf.OrgDefault && cv.Kind == "channel"
	_, admin, _ := d.selfRoles()
	if u, ok := c.User(d.selfID()); ok && u.EnterpriseAdmin {
		admin = true
	}
	if cv.Kind == "channel" && !cv.IsGeneral && !orgDefault && !cv.IsArchived {
		o.toPrivate = perms[core.PermConvertToPrivate] || perms[core.PermConvertToPrivateUnderPrefs] && team[core.PermCreatePrivateChannel]
	}
	if !general && !cv.IsArchived && !(inf.IsExtShared && d.hostedElsewhere(inf)) && (inf.IsMember || admin) {
		if !inf.IsExtShared || admin {
			o.archive = perms[core.PermArchiveChannel]
		}
	}
	o.unarchive = cv.IsArchived && perms[core.PermUnarchiveChannel] // Tq8O y8
	if !general && !orgDefault && perms[core.PermDeleteChannel] {
		o.del, o.delDisabled = true, inf.IsExtShared
	}
	return o
}

// teamName is the workspace's name (ConvertChannelToPrivateModal's
// teamName: the current team).
func (d *ChannelDetails) teamName() string {
	c := d.h.Core()
	if c == nil || c.C == nil {
		return ""
	}
	if n, _, ok := c.Team(c.C.Creds.Team, 0); ok && n != "" {
		return n
	}
	return c.S.TeamName
}

// managingRows are the section's ButtonFields, in AdminChannelModal's
// order.
func (d *ChannelDetails) managingRows(inf core.ChannelDetails) []W {
	o := d.manageOpts(inf)
	var rows []W
	if o.toPrivate {
		rows = append(rows, func(gtx C) D {
			if d.x.toPrivateRow.Clicked(gtx) {
				d.openDialog(cdDlgToPrivate)
			}
			return d.cdFieldOf(gtx, &d.x.toPrivateRow, cdFieldSpec{titleText: nsChannelDetails.T("Change to a private channel"), titleIcon: "lock", button: true})
		})
	}
	if d.toPublicOpt(inf).show {
		rows = append(rows, d.toPublicRow(inf))
	}
	if o.archive {
		rows = append(rows, func(gtx C) D {
			if d.x.archiveRow.Clicked(gtx) {
				d.openDialog(cdDlgArchive)
			}
			return d.cdFieldOf(gtx, &d.x.archiveRow, cdFieldSpec{titleText: nsChannelDetails.T("Archive channel for everyone"), titleIcon: "archive", button: true, danger: true})
		})
	}
	if o.unarchive {
		rows = append(rows, func(gtx C) D {
			if d.x.unarchiveRow.Clicked(gtx) && !d.x.unarch.busy() {
				d.unarchive()
			}
			d.unarchiveDone()
			return d.cdFieldOf(gtx, &d.x.unarchiveRow, cdFieldSpec{titleText: nsChannelDetails.T("Unarchive this channel"), titleIcon: "unarchive", button: true})
		})
	}
	if o.del {
		rows = append(rows, func(gtx C) D {
			if !o.delDisabled && d.x.deleteRow.Clicked(gtx) {
				d.openDialog(cdDlgDelete)
			}
			spec := cdFieldSpec{titleText: nsChannelDetails.T("Delete this channel"), titleIcon: "trash", button: true, danger: true, disabled: o.delDisabled}
			if o.delDisabled {
				spec.text = func(gtx C) D {
					return d.para(gtx, 15, 22, wReg, cdDim(d.p.Text), 0, nsChannelDetails.T("To delete this channel, first make sure it’s not connected to another organization."))
				}
			}
			return d.cdFieldOf(gtx, &d.x.deleteRow, spec)
		})
	}
	return rows
}

// unarchive is the Unarchive row: submitUnarchive, then the details
// close (a failure is a toast).
func (d *ChannelDetails) unarchive() {
	c, id := d.h.Core(), d.id
	if c == nil {
		return
	}
	d.x.unarchiving = true
	d.x.unarch.load(d.h, false, func(ctx context.Context) (bool, error) { return true, c.Unarchive(ctx, id) })
}

func (d *ChannelDetails) unarchiveDone() {
	if !d.x.unarchiving || d.x.unarch.busy() {
		return
	}
	_, done, err := d.x.unarch.get()
	if !done {
		return
	}
	d.x.unarchiving = false
	if err != nil {
		d.h.Toast("Couldn’t unarchive the channel: " + cdErrCode(err))
		return
	}
	d.closed = true
	d.h.Invalidate()
}

// archiveBody is ArchiveChannelModalContents' text and error.
func (d *ChannelDetails) archiveBody() []W {
	body := []W{
		d.cdPara("arch-all1", nsChannelDetails.T("When you archive a channel, it’s archived for everyone. That means…")),
		cdSpace(8),
		d.cdBullets(nsChannelDetails.T("No one will be able to send messages to the channel"),
			nsChannelDetails.T("Any apps installed in the channel will be disabled"),
			nsChannelDetails.T("If there are external people in this channel, they will be removed. They’ll still have access to the chat history.")),
		cdSpace(16),
		d.cdPara("arch-all2", nsChannelDetails.T("You’ll still be able to find the channel’s contents via search. And you can always unarchive the channel in the future, if you’d like.")),
	}
	if e := d.x.dlg.err; e != "" {
		body = append(body, cdSpace(16), d.cdAlert(e, false))
	}
	return body
}

// archiveModalError is ArchiveChannelModalContents' error text for code
// (andLeave false).
func archiveModalError(err error, admin, private bool) string {
	code := cdErrCode(err)
	switch code {
	case "last_ra_channel":
		if admin {
			return nsChannelDetails.T("Sorry, you can’t archive this channel because it is the only channel one of the guest account members belongs to. If you first disable the guest account, you will then be able to archive the channel.")
		}
		return nsChannelDetails.T("Sorry, you can’t archive this channel because it is the only channel one of the guest account members belongs to.")
	case "restricted_action", "user_is_restricted":
		if private {
			return nsChannelDetails.T("You don’t have permission to archive private channels. Talk to your Workspace Owner.")
		}
		return nsChannelDetails.T("You don’t have permission to archive channels. Talk to your Workspace Owner.")
	case "already_archived":
		return nsChannelDetails.T("This channel was already archived.")
	case "cant_archive_general":
		return nsChannelDetails.T("This channel cannot be archived.")
	}
	return nsChannelDetails.T("Archiving failed with error: {error}", "error", code)
}

// toPrivateBody is ConvertChannelToPrivateModalContents' text.
func (d *ChannelDetails) toPrivateBody(name string) []W {
	body := []W{
		d.cdPara("priv1", "Keep in mind that when you make ", name, " a private channel:"),
		cdSpace(8),
		d.cdBullets(nsChannelDetails.T("No changes will be made to the channel’s history or members"),
			"All files shared in this channel up until this point will be publicly accessible to everyone in "+d.teamName()),
	}
	if d.x.dlg.err != "" {
		body = append(body, cdSpace(16), d.cdAlert(cdSomethingWrong, false))
	}
	return body
}

// cdSomethingWrong is the modals' generic error ("<b>Something went
// wrong.</b> …", drawn plain in the alert).
const cdSomethingWrong = "Something went wrong. Sorry about that — please try again later, and contact Slack if this problem persists."

// deleteBody is DeleteChannelModalContents' text: the archive line when
// Archive shows (canBeArchived) and the channel isn't archived.
func (d *ChannelDetails) deleteBody(inf core.ChannelDetails) []W {
	items := []string{nsChannelDetails.T("Any files uploaded to this channel won’t be removed")}
	body := []W{
		d.cdPara("del1", "When you delete a channel, all messages from this channel will be removed from Slack immediately. ", "This can’t be undone."),
		cdSpace(16),
		d.cdPara("del2", nsChannelDetails.T("Keep in mind that:")),
		cdSpace(8),
		d.cdBullets(items...),
	}
	if o := d.manageOpts(inf); o.archive && !d.conv(inf, true).IsArchived {
		body = append(body, cdSpace(4), d.cdLinkBullet("del-arch", "You can ", "archive a channel instead", " without removing its messages", &d.x.dlg.back))
	}
	if d.x.dlg.err != "" {
		body = append(body, cdSpace(16), d.cdAlert(cdSomethingWrong, false))
	}
	return body
}

// deleteFooter is the modal's footer: the checkbox at the left, Cancel
// and Delete Channel (enabled once it's checked).
func (d *ChannelDetails) deleteFooter() []W {
	dl := &d.x.dlg
	return []W{func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Flexed(1, d.cdCheckRow(&dl.confirmAll, dl.confirmed, false, nsChannelDetails.T("Yes, permanently delete the channel"), nil)),
			layout.Rigid(d.cdDlgButton(&dl.cancel, btnOutline, "Cancel", false)),
			hgap(12),
			layout.Rigid(d.cdDlgButton(&dl.ok, btnDanger, nsChannelDetails.T("Delete Channel"), !dl.confirmed || dl.pending)),
		)
	}}
}

// isPrivateConv reports a private channel (ArchiveChannelModalContents'
// channelType "private").
func isPrivateConv(cv core.Conv) bool { return strings.EqualFold(cv.Kind, "private") }

// cdLinkBullet is a bullet whose middle part is a link (Slack's rt with
// an <a> its onClick handles): before, the link in the link colour, after.
func (d *ChannelDetails) cdLinkBullet(key, before, link, after string, c *widget.Clickable) W {
	return func(gtx C) D {
		return row(gtx, layout.Start,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 8, Right: 8}.Layout(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 1, "•") })
			}),
			layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 1, before) }),
			layout.Rigid(func(gtx C) D {
				return c.Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					return d.para(gtx, 15, 22, wReg, d.p.Link, 1, link)
				})
			}),
			layout.Flexed(1, func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 0, after) }),
		)
	}
}
