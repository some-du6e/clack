package views

// Channel details, the parts read from Slack's web client (gantry-v2
// build 407aff01646b, 2026-11-10):
//
//   - Settings: AdminChannelModal (chunk shared-…admin-channel-modal_tsx,
//     module oWlM) with the sections of its settings-sections selector
//     (jf5b): rename, permissions (AI use, Guest access, Huddles),
//     tab-permissions, channel-permissions (Member / Guest permissions),
//     add-member-suggestions, copy-members. Rows are EditableField
//     (boot-shared wfkn), TextField (wzxM) and ButtonField (a5gu).
//   - The tab only shows when there is a section (BaseAboutChannelModal's
//     isSettingsVisible), never for guests.
//   - Stacked dialogs: LeaveChannel / BaseArchiveDialog (maybeLeaveChannel,
//     boot-shared Wlp6), AiExclusionsModal, GuestRestrictionsModal,
//     EditChannelPermissionsModal, HideUserConfirmationModal (N0zR) and
//     MpdmAddPeopleModal (TWss).
//   - The 1:1 header's VIP (77my) and Hide (N0zR hideOrUnhideUserById).

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Settings sections (jf5b), in Slack's order.
const (
	cdSecRename          = "rename"
	cdSecPermissions     = "permissions"
	cdSecTabPermissions  = "tab-permissions"
	cdSecChannelPerms    = "channel-permissions"
	cdSecMemberSuggest   = "add-member-suggestions"
	cdSecCopyMembers     = "copy-members"
	cdCopyMembersMax     = 1000 // CopyMembers: more members can't be copied
	cdMemberSuggestPref  = "non_member_mention_invite_opt_out"
	cdHuddlesHelpArticle = "https://slack.com/help/articles/4402059015315"
	cdAIHelpArticle      = "https://slack.com/help/articles/28244420881555"
)

// cdSettingsPerms are the permissions.info checks the Settings tab needs.
var cdSettingsPerms = []string{core.PermRenameChannel, core.PermManageChannelPermission,
	core.PermEditChannelAIExclusions, core.PermManageChannelTabs, core.PermManageChannelPosting,
	core.PermConvertPrivateToPublic, core.PermArchiveChannel, core.PermDeleteChannel,
	core.PermConvertToPrivate, core.PermConvertToPrivateUnderPrefs, core.PermUnarchiveChannel}

// Stacked dialog kinds.
const (
	cdDlgNone = iota
	cdDlgLeave
	cdDlgLeaveArchive
	cdDlgAIUse
	cdDlgGuests
	cdDlgPerms
	cdDlgHide
	cdDlgAddPeople
	cdDlgSharing
	cdDlgPosting  // PostingPermissionsModal (details_posting.go)
	cdDlgToPublic // ConvertChannelToPublicModal (details_convert_public.go)
	// managing-this-channel's modals (details_managing.go)
	cdDlgToPrivate // ConvertChannelToPrivateModal
	cdDlgArchive   // ArchiveChannelModal
	cdDlgDelete    // DeleteChannelModal
)

// cdExtra is the dialog state behind the Slack parts above.
type cdExtra struct {
	perms    async[map[string]bool]
	canProps async[map[string]core.PropCan]
	acct     async[map[string][]core.AccountTypePermission]
	emails   async[cdMemberList] // the members Copy reads (CopyMembers' fetch)

	aiRow, guestRow, huddleRow    widget.Clickable
	sharingRow                    widget.Clickable // Sharing & file downloads
	memPermRow, guestPermRow      widget.Clickable
	suggest, huddleLearn, aiLearn widget.Clickable
	copyNames, copyEmails         widget.Clickable
	toPublicRow                   widget.Clickable // Change to a public channel (details_convert_public.go)
	// managing-this-channel's other rows (details_managing.go)
	toPrivateRow, archiveRow widget.Clickable
	unarchiveRow, deleteRow  widget.Clickable
	teamPerms                async[map[string]bool] // the team's CREATE_PRIVATE_CHANNEL
	unarch                   async[bool]            // submitUnarchive
	unarchiving              bool
	hidePrefs                widget.Clickable // HideUserConfirmationModal's "preferences"
	mpdmName                 widget.Clickable // About's "Conversation name" Edit (details_mpdm.go)
	postRow, postLearn       widget.Clickable // Posting permissions (details_posting.go)
	postSub                  async[core.PostingPrefs]
	helpTips                 map[string]*cdTip

	suggestOp      async[bool]
	suggestPending bool
	suggestOv      bool // shown while the pref write runs

	tabsOp      async[bool]
	tabsPending bool
	tabsPrev    string // who_can_manage_tabs before the write (reverted on failure)
	shield      int    // input shield over the disabled tabs select

	copyOp      async[cdCopied]
	copyPending string // "names" | "emails" while fetching
	copied      string // "names" | "emails": "Copied ✓" for 2 s
	copiedAt    time.Time

	leaveCount async[core.MemberCounts] // maybeLeaveChannel's fresh count

	vipOp       async[bool]
	vipPending  bool
	vipOv       bool
	vipName     string
	hideOp      async[bool]
	hidePending bool

	dlg cdDialog
}

// cdTip is a disabled field's help icon with its tooltip.
type cdTip struct {
	c  widget.Clickable
	st popups.TipState
}

type cdMemberList struct {
	users []core.User
	more  bool // stopped at cdCopyMembersMax
}

type cdCopied struct {
	what, text string
}

// cdDialog is the stacked dialog over channel details.
type cdDialog struct {
	kind                    int
	scrim, box, close, ok   widget.Clickable
	cancel, back            widget.Clickable
	op                      async[bool]
	pending                 bool
	err                     string
	acct                    string                       // cdDlgPerms: FULL_MEMBER | MULTI_CHANNEL_GUEST
	edit                    []core.AccountTypePermission // cdDlgPerms working copy
	checks                  map[string]*widget.Clickable
	userID, userName, dmID  string // cdDlgHide
	add                     *cdAddPeople
	post                    *cdPosting // cdDlgPosting
	addOp                   async[core.Conv]
	ptr                     int
	focusIn                 int
	resultsAt               image.Point // the people picker's results menu (window coords inside the dialog)
	resultsW                int
	resultsShow             bool
	confirmAll, confirmBack widget.Clickable
	confirmed               bool   // cdDlgDelete: "Yes, permanently delete the channel"
	subtitle                string // the title bar's subtitle (c-sk-modal_title_bar__subtitle)
	titleH                  int    // the title bar's height this frame (px)
}

func (d *ChannelDetails) openDialog(kind int) {
	d.x.dlg = cdDialog{kind: kind, checks: map[string]*widget.Clickable{}}
	d.editing, d.confirmLeave = false, false
	d.h.Invalidate()
}

func (d *ChannelDetails) closeDialog() {
	d.x.dlg = cdDialog{} // a late answer lands in the dropped state
	d.h.Invalidate()
}

// ---- Settings sections ----

// cdConvType is the selector's conversation type: channel | dm | mpdm.
func (d *ChannelDetails) cdConvType() string {
	switch cdGroup(d.kind) {
	case "im":
		return "dm"
	case "mpim":
		return "mpdm"
	}
	return "channel"
}

// settingsSections is jf5b's list for this conversation, with the
// sections Clack can draw. Guests never see Settings.
func (d *ChannelDetails) settingsSections(inf core.ChannelDetails) []string {
	c := d.h.Core()
	if c == nil {
		return nil
	}
	if u, ok := c.User(d.selfID()); ok && u.Guest {
		return nil
	}
	cv := d.conv(inf, true)
	if cv.IsArchived { // jf5b: managing-this-channel alone (details_managing.go)
		if d.manageOpts(inf).any() {
			return []string{cdSecManaging}
		}
		return nil
	}
	e := d.cdConvType()
	var out []string
	if e == "channel" {
		perms, _, _ := d.x.perms.get()
		if perms[core.PermRenameChannel] {
			out = append(out, cdSecRename)
		}
		out = append(out, cdSecPermissions)
		if c.Experiment("move_channel_tabs_settings") == "on" {
			out = append(out, cdSecTabPermissions)
		}
		out = append(out, cdSecChannelPerms)
		if inf.IsMember {
			out = append(out, cdSecMemberSuggest)
		}
	}
	// copy-members: public/private channels and group DMs, on a paid team
	// (CopyMembers' isPaidTeam; every Enterprise Grid org is)
	if (e == "channel" || e == "mpdm") && c.C != nil && c.C.Creds.EnterpriseID != "" {
		out = append(out, cdSecCopyMembers)
	}
	// managing-this-channel (details_managing.go, details_convert_public.go)
	if o := d.manageOpts(inf); o.archive || o.del || o.toPrivate || d.toPublicOpt(inf).show {
		out = append(out, cdSecManaging)
	}
	return out
}

// loadSettings starts the Settings tab's reads: the viewer's permissions,
// canSetProperties for Guest access, Member / Guest permissions and the
// members Copy needs.
func (d *ChannelDetails) loadSettings(inf core.ChannelDetails) {
	c, id := d.h.Core(), d.id
	if c == nil || cdGroup(d.kind) == "im" {
		return
	}
	if cdGroup(d.kind) == "channel" {
		if !d.x.perms.started() {
			d.x.perms.load(d.h, false, func(ctx context.Context) (map[string]bool, error) {
				return c.ChannelPermissions(ctx, id, cdSettingsPerms...)
			})
		}
		if !d.x.teamPerms.started() { // 2v1U Rt: the team's CREATE_PRIVATE_CHANNEL
			d.x.teamPerms.load(d.h, false, func(ctx context.Context) (map[string]bool, error) {
				return c.ChannelPermissions(ctx, "", core.PermCreatePrivateChannel)
			})
		}
		if !d.x.canProps.started() && c.Experiment("enable_new_channel_settings") == "on" {
			d.x.canProps.load(d.h, false, func(ctx context.Context) (map[string]core.PropCan, error) {
				return c.CanSetProperties(ctx, id, "restrict_guests", "restrict_message_and_file_sharing")
			})
		}
		if !d.x.acct.started() && d.showChannelPerms(inf) {
			d.x.acct.load(d.h, false, func(ctx context.Context) (map[string][]core.AccountTypePermission, error) {
				return c.AccountTypePermissions(ctx, id)
			})
		}
	}
	// CopyMembers reads the members only for channels of up to 1,000
	// (past that the rows are disabled), so wait for the count
	if !d.x.emails.started() {
		mc, counted, err := d.counts.get()
		small := cdGroup(d.kind) == "mpim" || counted && err == nil && mc.People <= cdCopyMembersMax
		if small {
			d.x.emails.load(d.h, false, func(ctx context.Context) (cdMemberList, error) { return cdFetchMembers(ctx, c, id) })
		}
	}
}

// cdFetchMembers pages the members (people) up to CopyMembers' limit.
func cdFetchMembers(ctx context.Context, c *core.App, id string) (cdMemberList, error) {
	var out cdMemberList
	marker := ""
	for {
		us, next, err := c.Members(ctx, id, marker, true)
		if err != nil {
			return out, err
		}
		out.users = append(out.users, us...)
		if next == "" {
			return out, nil
		}
		if len(out.users) >= cdCopyMembersMax {
			out.more = true
			return out, nil
		}
		marker = next
	}
}

// showChannelPerms is canUserViewChannelPermissions (EpxP): a public or
// private channel on an Enterprise Grid org, for an admin or a member.
func (d *ChannelDetails) showChannelPerms(inf core.ChannelDetails) bool {
	c := d.h.Core()
	if c == nil || c.C == nil || c.C.Creds.EnterpriseID == "" || cdGroup(d.kind) != "channel" {
		return false
	}
	_, admin, _ := d.selfRoles()
	return admin || inf.IsMember
}

// canEditChannelPerms is gcM+.f: an admin, or a member with
// MANAGE_CHANNEL_PERMISSIONS.
func (d *ChannelDetails) canEditChannelPerms(inf core.ChannelDetails) bool {
	_, admin, _ := d.selfRoles()
	perms, _, _ := d.x.perms.get()
	return d.showChannelPerms(inf) && (admin || perms[core.PermManageChannelPermission] && inf.IsMember)
}

// hostedElsewhere is 2ES1.b: the channel's host is another org.
func (d *ChannelDetails) hostedElsewhere(inf core.ChannelDetails) bool {
	c := d.h.Core()
	if c == nil || c.C == nil || inf.HostID == "" {
		return false
	}
	return inf.HostID != c.C.Creds.Team && inf.HostID != c.C.Creds.EnterpriseID
}

// slackSettingsItems draws the sections in order, each its own field group.
func (d *ChannelDetails) slackSettingsItems(inf core.ChannelDetails, name W) []W {
	d.loadSettings(inf)
	var items []W
	card := func(rows ...W) {
		if len(rows) > 0 {
			items = append(items, cdGap(16), d.cdCard(rows...))
		}
	}
	for _, s := range d.settingsSections(inf) {
		switch s {
		case cdSecRename:
			card(name)
		case cdSecPermissions:
			card(d.permissionsRows(inf)...)
		case cdSecTabPermissions:
			card(func(gtx C) D { return d.tabPermsField(gtx, inf) })
		case cdSecChannelPerms:
			if d.showChannelPerms(inf) {
				card(func(gtx C) D { return d.channelPermsField(gtx, inf, core.AccountFullMember) },
					func(gtx C) D { return d.channelPermsField(gtx, inf, core.AccountMultiChannelGuest) })
			}
		case cdSecMemberSuggest:
			card(d.memberSuggestField)
		case cdSecCopyMembers:
			card(d.copyMembersRows()...)
		case cdSecManaging:
			card(d.managingRows(inf)...)
		}
	}
	if len(items) == 0 {
		return nil
	}
	return append(items, cdGap(24))
}

// permissionsRows is the "permissions" field group: Posting permissions
// (details_posting.go), AI use, Guest access, Huddles. (Message history
// needs a modal Clack doesn't have: docs/CHANNELS-AUDIT.md.)
func (d *ChannelDetails) permissionsRows(inf core.ChannelDetails) []W {
	c := d.h.Core()
	var rows []W
	if vis := d.postingVisibility(inf); vis.Show {
		rows = append(rows, func(gtx C) D { return d.postingField(gtx, inf, vis) })
	}
	if c.Experiment("ai_channel_exclusions") == "on" {
		rows = append(rows, func(gtx C) D { return d.aiUseField(gtx, inf) })
	}
	if st, show := d.guestAccessState(); show {
		rows = append(rows, func(gtx C) D { return d.guestAccessField(gtx, inf, st) })
	}
	if st, show := d.sharingState(); show {
		rows = append(rows, func(gtx C) D { return d.sharingField(gtx, inf, st) })
	}
	rows = append(rows, d.huddlesField)
	return rows
}

// ---- Slack's field components ----

// cdField is Slack's EditableField (wfkn) / ButtonField (a5gu): padding
// 16px 20px; an optional 20px title icon 12px before; the 15/22 bold
// title, the text and content under it; "Edit" (or editText) 13/18 bold
// link at the top right. Disabled (p-field--disabled) the text is
// #1d1c1d@0.7, there's no hover and the help icon takes Edit's place,
// its tooltip the disabledText. The whole row clicks Edit.
type cdFieldSpec struct {
	title        W
	titleText    string
	titleIcon    string
	text         W
	content      W
	edit         string // "" = Edit; "-" = no edit link (TextField, ButtonField)
	disabled     bool
	disabledText string
	danger       bool
	button       bool // ButtonField: hover and click, no Edit link
}

func (d *ChannelDetails) cdFieldOf(gtx C, c *widget.Clickable, f cdFieldSpec) D {
	draw := func(gtx C) D {
		return layout.Inset{Top: 16, Bottom: 16, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			textCol := d.p.Text
			if f.disabled {
				textCol = cdDim(d.p.Text)
			} else if f.danger {
				textCol = d.p.Red
			}
			body := func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						return cdLine22(gtx, func(gtx C) D {
							return row(gtx, layout.Start,
								layout.Flexed(1, func(gtx C) D {
									return dirFill(gtx, func(gtx C) D {
										if f.title != nil {
											return f.title(gtx)
										}
										return d.para(gtx, 15, 22, wBold, textCol, 0, f.titleText)
									})
								}),
								layout.Rigid(func(gtx C) D {
									switch {
									case f.disabled && f.disabledText != "":
										return layout.Inset{Left: 8, Top: 2}.Layout(gtx, func(gtx C) D { return d.helpTip(gtx, f.titleText, f.disabledText) })
									case f.disabled || f.button || f.edit == "-":
										return D{}
									}
									label := f.edit
									if label == "" {
										label = "Edit"
									}
									return layout.Inset{Left: 8, Top: 2}.Layout(gtx, func(gtx C) D { return cdLink(&d.base, gtx, c, 13, label) })
								}),
							)
						})
					}),
					layout.Rigid(func(gtx C) D {
						if f.text == nil {
							return D{}
						}
						return cdLine22(gtx, func(gtx C) D { return dirFill(gtx, f.text) })
					}),
					layout.Rigid(func(gtx C) D {
						if f.content == nil {
							return D{}
						}
						return dirFill(gtx, f.content)
					}),
				)
			}
			if f.titleIcon == "" {
				return body(gtx)
			}
			return row(gtx, layout.Start,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 1, Right: 12}.Layout(gtx, func(gtx C) D { return d.icon(gtx, f.titleIcon, 20, textCol) })
				}),
				layout.Flexed(1, body),
			)
		})
	}
	if f.disabled || c == nil || (f.edit == "-" && !f.button) {
		return draw(gtx)
	}
	return d.cdButtonField(gtx, c, draw)
}

// cdDim is p-field--disabled's #1d1c1d@0.7.
func cdDim(c color.NRGBA) color.NRGBA {
	c.A = 0xb3
	return c
}

// helpTip is the disabled field's 16px help icon; hovering shows why.
func (d *ChannelDetails) helpTip(gtx C, key, tip string) D {
	if d.x.helpTips == nil {
		d.x.helpTips = map[string]*cdTip{}
	}
	t := d.x.helpTips[key]
	if t == nil {
		t = &cdTip{}
		t.st.MaxWidth = 240
		d.x.helpTips[key] = t
	}
	dims := t.c.Layout(gtx, func(gtx C) D { return d.icon(gtx, "help", 16, d.p.Text) })
	popups.Tooltip(gtx, d.th, &t.st, t.c.Hovered(), dims.Size, popups.Above, tip, "")
	return dims
}

// cdIconText is a field text led by a 16px status icon (check-filled in
// content.hgl.2 green, close-filled in base.inv.imp red), padding_right_50.
func (d *ChannelDetails) cdIconText(gtx C, ok bool, text string, col color.NRGBA) D {
	icon, ic := "check-filled", rgb(0x007a5a)
	if !ok {
		icon, ic = "close-filled", rgb(0xe01e5a)
	}
	return row(gtx, layout.Start,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 3, Right: 8}.Layout(gtx, func(gtx C) D { return d.icon(gtx, icon, 16, ic) })
		}),
		layout.Flexed(1, func(gtx C) D { return dirFill(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wReg, col, 0, text) }) }),
	)
}

// ---- permissions group ----

// aiUseField is AiExclusionsField: properties.exclude_from_slack_ai,
// editable with EDIT_CHANNEL_AI_EXCLUSIONS on a channel hosted here.
func (d *ChannelDetails) aiUseField(gtx C, inf core.ChannelDetails) D {
	if d.x.aiRow.Clicked(gtx) {
		d.openDialog(cdDlgAIUse)
	}
	perms, _, _ := d.x.perms.get()
	excluded := inf.Props.ExcludeFromAI
	text := "Channel content can be used by AI features outside of this channel."
	edit := "Restrict"
	if excluded {
		text, edit = "Channel content cannot be used by AI features outside of this channel.", "Allow"
	}
	disabled := !perms[core.PermEditChannelAIExclusions] || d.hostedElsewhere(inf)
	col := d.p.Text
	if disabled {
		col = cdDim(col)
	}
	return d.cdFieldOf(gtx, &d.x.aiRow, cdFieldSpec{titleText: "AI use", edit: edit, disabled: disabled,
		disabledText: "You don’t have permission to change the AI use setting.",
		text:         func(gtx C) D { return d.cdIconText(gtx, !excluded, text, col) }})
}

// guestAccessState is AdminChannelModal's canSetProperties answer for
// restrict_guests: shown when settable, or when not with a reason other
// than disallowed_action (then read-only, reason kept).
func (d *ChannelDetails) guestAccessState() (core.PropCan, bool) {
	return d.propState("restrict_guests")
}

// sharingState is the same answer for restrict_message_and_file_sharing
// (AdminChannelModal's ei / en).
func (d *ChannelDetails) sharingState() (core.PropCan, bool) {
	return d.propState("restrict_message_and_file_sharing")
}

func (d *ChannelDetails) propState(prop string) (core.PropCan, bool) {
	if d.h.Core().Experiment("enable_new_channel_settings") != "on" {
		return core.PropCan{}, false
	}
	m, ok, err := d.x.canProps.get()
	if !ok || err != nil {
		return core.PropCan{}, false
	}
	p, has := m[prop]
	if !has || (!p.CanSet && (p.Reason == "" || p.Reason == "disallowed_action")) {
		return core.PropCan{}, false
	}
	return p, true
}

// sharingField is MessageAndFileSharingRestrictionsField (admin channel
// modal oWlM, enable_new_channel_settings): "Sharing & file downloads",
// properties.restrict_message_and_file_sharing with a check-filled or
// close-filled line, Restrict / Allow; read-only (70%, the help icon "You
// don’t have permission to change this setting.") when it can't be set or
// another org hosts the channel.
func (d *ChannelDetails) sharingField(gtx C, inf core.ChannelDetails, st core.PropCan) D {
	if d.x.sharingRow.Clicked(gtx) {
		d.openDialog(cdDlgSharing)
	}
	restricted := inf.Props.RestrictSharing
	text, edit := "Sharing and downloading are allowed in this channel.", "Restrict"
	if restricted {
		text, edit = "Sharing and downloading are not allowed in this channel.", "Allow"
	}
	disabled := !st.CanSet || d.hostedElsewhere(inf)
	col := d.p.Text
	if disabled {
		col = cdDim(col)
	}
	return d.cdFieldOf(gtx, &d.x.sharingRow, cdFieldSpec{titleText: "Sharing & file downloads", edit: edit, disabled: disabled,
		disabledText: "You don’t have permission to change this setting.",
		text:         func(gtx C) D { return d.cdIconText(gtx, !restricted, text, col) }})
}

// guestAccessField is GuestRestrictionsField (properties.restrict_guests).
func (d *ChannelDetails) guestAccessField(gtx C, inf core.ChannelDetails, st core.PropCan) D {
	if d.x.guestRow.Clicked(gtx) {
		d.openDialog(cdDlgGuests)
	}
	restricted := inf.Props.RestrictGuests
	text, edit := "Guests are allowed in this channel.", "Restrict"
	if restricted {
		text, edit = "Guests are not allowed in this channel.", "Allow"
	}
	why := "You don’t have permission to change this setting."
	if st.Reason == "scg_present" {
		why = "You cannot change this setting when the channel contains single-channel guests."
	}
	disabled := !st.CanSet || d.hostedElsewhere(inf)
	col := d.p.Text
	if disabled {
		col = cdDim(col)
	}
	return d.cdFieldOf(gtx, &d.x.guestRow, cdFieldSpec{titleText: "Guest access", edit: edit, disabled: disabled, disabledText: why,
		text: func(gtx C) D { return d.cdIconText(gtx, !restricted, text, col) }})
}

// huddlesField is the Huddles EditableField: HuddlesPermissionSubtitle and
// "Learn more", then HuddlesLinkCTAs. Only admins may change who can
// huddle (canUserManageHuddlesPermissions); Clack doesn't have
// HuddlesPermissionModal, so for them the row has no Edit.
func (d *ChannelDetails) huddlesField(gtx C) D {
	if d.x.huddleLearn.Clicked(gtx) {
		hostHandler{d.h}.OpenURL(cdHuddlesHelpArticle)
	}
	_, admin, _ := d.selfRoles()
	col := d.p.Text
	if !admin {
		col = cdDim(col)
	}
	return d.cdFieldOf(gtx, &d.x.huddleRow, cdFieldSpec{titleText: "Huddles", edit: "-", disabled: !admin,
		disabledText: "You don’t have permission to change this setting for this channel",
		text: func(gtx C) D {
			return row(gtx, layout.Baseline,
				layout.Rigid(func(gtx C) D {
					return d.para(gtx, 15, 22, wReg, col, 1, "Members can start and join huddles in this channel. ")
				}),
				layout.Rigid(func(gtx C) D { return cdLinkReg(&d.base, gtx, &d.x.huddleLearn, 15, "Learn more") }),
			)
		},
		content: func(gtx C) D {
			return layout.Inset{Top: 12}.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return cdBtn(&d.base, gtx, &d.huddleStart, "headphones", "Start Huddle", false, 0) }),
					hgap(12),
					layout.Rigid(func(gtx C) D { return cdBtn(&d.base, gtx, &d.huddleLink, "link", "Copy Huddle Link", false, 0) }),
				)
			})
		}})
}

// ---- tab permissions ----

// tabPermsField is ManageChannelTabsPermissions: "Choose who can add,
// remove, and reorder tabs", Everyone / Channel managers from
// properties.who_can_manage_tabs, settable by a channel manager or admin
// (conversations.setProperties).
func (d *ChannelDetails) tabPermsField(gtx C, inf core.ChannelDetails) D {
	dd := &d.tabsPerm
	if len(dd.labels) != 2 {
		dd.labels = []string{"Everyone", "Channel managers"}
	}
	if !d.x.tabsPending {
		dd.sel = 0
		if inf.Props.WhoCanManageTabs == "channel_managers" {
			dd.sel = 1
		}
	}
	mgr, admin, _ := d.selfRoles()
	editable := mgr || admin
	return d.cdField(gtx, "Choose who can add, remove, and reorder tabs", nil, func(gtx C) D {
		return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
			if !editable {
				// BasicSelect isDisabled: the value, dimmed, no menu
				m := op.Record(gtx.Ops)
				dims := dd.layoutSelect(&d.base, gtx, 224)
				call := m.Stop()
				defer clip.Rect{Max: dims.Size}.Push(gtx.Ops).Pop()
				call.Add(gtx.Ops)
				fill(gtx, image.Rectangle{Max: dims.Size}, theme.WithAlpha(d.p.Surface, 0x80))
				event.Op(gtx.Ops, &d.x.shield) // a never-read tag: the select takes no clicks
				dd.open = false
				return dims
			}
			return dd.layoutSelect(&d.base, gtx, 224)
		})
	})
}

func (d *ChannelDetails) updateTabPerms(gtx C, inf core.ChannelDetails) {
	if !d.tabsPerm.update(gtx) {
		return
	}
	mgr, admin, _ := d.selfRoles()
	if !(mgr || admin) || d.x.tabsPending {
		return
	}
	v := "everyone"
	if d.tabsPerm.sel == 1 {
		v = "channel_managers"
	}
	prev := inf.Props.WhoCanManageTabs
	d.info.mutate(func(i *core.ChannelDetails) { i.Props.WhoCanManageTabs = v })
	c, id := d.h.Core(), d.id
	d.x.tabsPending = true
	d.x.tabsOp.load(d.h, false, func(ctx context.Context) (bool, error) {
		return true, c.SetChannelProperties(ctx, id, "set_channel_tab_who_can_manage", map[string]any{"who_can_manage_tabs": v})
	})
	d.x.tabsPrev = prev
}

// ---- Member / Guest permissions ----

// channelPermsField is ViewChannelPermissions for one account type: the
// allowed permissions with green checks, or "… don’t have any extra
// permissions yet."; editable (EditChannelPermissionsModal) by whoever
// may manage channel permissions, when there are permissions to show.
func (d *ChannelDetails) channelPermsField(gtx C, inf core.ChannelDetails, acct string) D {
	c := &d.x.memPermRow
	title, empty := "Member permissions", "Members don’t have any extra permissions yet."
	if acct == core.AccountMultiChannelGuest {
		c, title, empty = &d.x.guestPermRow, "Guest permissions", "Guests don’t have any extra permissions yet."
	}
	all, loaded, err := d.x.acct.get()
	perms := all[acct]
	canEdit := d.canEditChannelPerms(inf) && len(perms) > 0
	if c.Clicked(gtx) && canEdit {
		d.openDialog(cdDlgPerms)
		d.x.dlg.acct = acct
		d.x.dlg.edit = append([]core.AccountTypePermission(nil), perms...)
	}
	col := d.p.Text
	if !canEdit {
		col = cdDim(col)
	}
	var allowed []core.AccountTypePermission
	for _, p := range perms {
		if p.Allowed {
			allowed = append(allowed, p)
		}
	}
	return d.cdFieldOf(gtx, c, cdFieldSpec{titleText: title, disabled: !canEdit,
		content: func(gtx C) D {
			switch {
			case !loaded || err != nil:
				return D{Size: image.Pt(0, gtx.Dp(8))} // p-view_channel_permissions--error: an empty span
			case len(allowed) == 0:
				return cdLine22(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wReg, col, 0, empty) })
			}
			ch := make([]layout.FlexChild, 0, len(allowed))
			for i, p := range allowed {
				p, last := p, i == len(allowed)-1
				ch = append(ch, layout.Rigid(func(gtx C) D {
					bottom := unit.Dp(4) // c-label--inline margin-bottom, none on the last
					if last {
						bottom = 0
					}
					return layout.Inset{Bottom: bottom}.Layout(gtx, func(gtx C) D { return d.cdIconText(gtx, true, p.Name, col) })
				}))
			}
			return col2(gtx, ch...)
		}})
}

// col2 is col without the name clash with the color variables above.
func col2(gtx C, ch ...layout.FlexChild) D { return col(gtx, ch...) }

// ---- Channel member suggestions ----

// memberSuggestField is AddMemberSuggestionsField: a TextField with the
// checkbox "Get suggestions…", checked unless the channel is in the
// non_member_mention_invite_opt_out pref ({channel: true}).
func (d *ChannelDetails) memberSuggestField(gtx C) D {
	on := !d.suggestOptedOut()
	if d.x.suggestPending {
		on = d.x.suggestOv
	}
	if d.x.suggest.Clicked(gtx) && !d.x.suggestPending {
		d.toggleMemberSuggest(!on)
		on = !on
	}
	return d.cdFieldOf(gtx, nil, cdFieldSpec{titleText: "Channel member suggestions", edit: "-",
		text: func(gtx C) D {
			return d.x.suggest.Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				return row(gtx, layout.Start,
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Top: 3, Right: 8}.Layout(gtx, func(gtx C) D { return cdCheckbox(&d.base, gtx, on, false) })
					}),
					layout.Flexed(1, func(gtx C) D {
						return d.para(gtx, 15, 22, wReg, d.p.Text, 0, "Get suggestions to add people who aren’t in the channel when you @mention them.")
					}),
				)
			})
		}})
}

// suggestOptOuts reads the pref: Slack stores it as a JSON object, the
// prefs API may hand it back as that object or as its JSON text.
func (d *ChannelDetails) suggestOptOuts() map[string]bool {
	out := map[string]bool{}
	raw, ok := d.h.Core().Pref(cdMemberSuggestPref)
	if !ok {
		return out
	}
	if json.Unmarshal(raw, &out) == nil {
		return out
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		_ = json.Unmarshal([]byte(s), &out)
	}
	return out
}

func (d *ChannelDetails) suggestOptedOut() bool { return d.suggestOptOuts()[d.id] }

// toggleMemberSuggest saves the pref with this channel in or out
// (AddMemberSuggestionsField's onChange; Slack reverts on failure).
func (d *ChannelDetails) toggleMemberSuggest(on bool) {
	m := d.suggestOptOuts()
	if on {
		delete(m, d.id)
	} else {
		m[d.id] = true
	}
	c := d.h.Core()
	d.x.suggestOv, d.x.suggestPending = on, true
	d.x.suggestOp.load(d.h, false, func(ctx context.Context) (bool, error) {
		return true, c.SetPref(ctx, cdMemberSuggestPref, m)
	})
}

// cdCheckbox is c-input_checkbox: 16px, radius 4, a 1px #1d1c1d@0.3 border;
// checked, #1264a3 with a white check.
func cdCheckbox(b *base, gtx C, checked, disabled bool) D {
	s := gtx.Dp(16)
	r := image.Rect(0, 0, s, s)
	rad := gtx.Dp(4)
	if checked {
		bg := b.p.Link
		if disabled {
			bg = cdDim(bg)
		}
		rrect(gtx, r, rad, bg)
		g := gtx
		g.Constraints = layout.Exact(r.Max)
		layout.Center.Layout(g, func(gtx C) D { return b.icon(gtx, "check", 12, rgb(0xffffff)) })
	} else {
		rrect(gtx, r, rad, b.p.Surface)
		bc := b.p.BorderStr
		if disabled {
			bc = b.p.Border
		}
		border(gtx, r, rad, max(1, gtx.Dp(1)), bc)
	}
	return D{Size: r.Max}
}

// ---- Copy members ----

// copyMembersRows is CopyMembers: "Copy member names" and "Copy member
// email addresses" ButtonFields ("Copied ✓" in #007a5a for 2 s after a
// copy); one disabled "Copy channel members" past 1,000 members. Email
// addresses are disabled unless every member's address is visible.
func (d *ChannelDetails) copyMembersRows() []W {
	ml, loaded, _ := d.x.emails.get()
	count := len(ml.users)
	if mc, ok, _ := d.counts.get(); ok && mc.People > 0 {
		count = mc.People
	}
	if count > cdCopyMembersMax || ml.more {
		return []W{func(gtx C) D {
			return d.cdFieldOf(gtx, nil, cdFieldSpec{titleText: "Copy channel members", button: true, disabled: true,
				text: func(gtx C) D {
					return d.para(gtx, 15, 22, wReg, cdDim(d.p.Text), 0, "Copying member names and email addresses isn’t possible for channels with more than 1,000 members.")
				}})
		}}
	}
	emailsOK := loaded && len(ml.users) > 0
	for _, u := range ml.users {
		if !strings.Contains(u.Email, "@") {
			emailsOK = false
			break
		}
	}
	title := func(c *widget.Clickable, label, what string, disabled bool) W {
		return func(gtx C) D {
			col := d.p.Text
			if disabled {
				col = cdDim(col)
			}
			return row(gtx, layout.Middle,
				layout.Flexed(1, func(gtx C) D { return dirFill(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wBold, col, 1, label) }) }),
				layout.Rigid(func(gtx C) D {
					switch {
					case d.x.copyPending == what:
						return d.label(gtx, 13, wReg, d.p.Muted, "…")
					case d.x.copied == what && gtx.Now.Sub(d.x.copiedAt) < 2*time.Second:
						gtx.Execute(op.InvalidateCmd{At: d.x.copiedAt.Add(2 * time.Second)})
						return row(gtx, layout.Middle,
							layout.Rigid(func(gtx C) D { return d.label(gtx, 15, wReg, rgb(0x007a5a), "Copied") }),
							hgap(8),
							layout.Rigid(func(gtx C) D { return d.icon(gtx, "check-circle-filled", 16, rgb(0x007a5a)) }),
						)
					}
					return D{}
				}),
			)
		}
	}
	return []W{
		func(gtx C) D {
			return d.cdFieldOf(gtx, &d.x.copyNames, cdFieldSpec{button: true, title: title(&d.x.copyNames, "Copy member names", "names", false), titleText: "Copy member names"})
		},
		func(gtx C) D {
			var text W
			if !emailsOK {
				text = func(gtx C) D {
					return d.para(gtx, 15, 22, wReg, cdDim(d.p.Text), 0, "You don’t have permission to see everyone’s email addresses, so this is disabled.")
				}
			}
			return d.cdFieldOf(gtx, &d.x.copyEmails, cdFieldSpec{button: true, disabled: !emailsOK, text: text,
				title: title(&d.x.copyEmails, "Copy member email addresses", "emails", !emailsOK), titleText: "Copy member email addresses"})
		},
	}
}

// updateCopy runs the Copy rows: the members (CopyMembers' fetch), then
// the real names or the addresses joined with ", " on the clipboard.
func (d *ChannelDetails) updateCopy(gtx C) {
	start := func(what string) {
		if d.x.copyPending != "" {
			return
		}
		c, id := d.h.Core(), d.id
		d.x.copyPending = what
		d.x.copyOp.load(d.h, false, func(ctx context.Context) (cdCopied, error) {
			ml, err := cdFetchMembers(ctx, c, id)
			if err != nil {
				return cdCopied{what: what}, err
			}
			parts := make([]string, 0, len(ml.users))
			for _, u := range ml.users {
				if what == "emails" {
					parts = append(parts, u.Email)
				} else if n := dirFirst(u.RealName, displayName(u)); n != "" {
					parts = append(parts, n) // getMemberRealName
				}
			}
			return cdCopied{what: what, text: strings.Join(parts, ", ")}, nil
		})
	}
	if d.x.copyNames.Clicked(gtx) {
		start("names")
	}
	if d.x.copyEmails.Clicked(gtx) {
		start("emails")
	}
	if d.x.copyPending != "" {
		if v, done, err := d.x.copyOp.get(); done && !d.x.copyOp.busy() {
			d.x.copyPending = ""
			if err == nil && v.text != "" {
				cdCopy(gtx, v.text)
				d.x.copied, d.x.copiedAt = v.what, gtx.Now
			}
		}
	}
}

// ---- stacked dialogs ----

// skModal draws Slack's c-sk-modal stacked over channel details: the
// #1d1c1d@0.6 scrim, the 520px box (radius 8, the 1px #1d1c1d@0.13 ring,
// 0 18px 48px #00000059), the title bar (min 70px, padding 20px 28px,
// 22/30 black, the 36px close button), the content (padding 0 28px) and
// the footer (84px, padding 24px 28px, buttons at the right 12px apart).
func (d *ChannelDetails) skModal(gtx C, full image.Point, title string, closeBtn bool, body []W, footer []W) {
	dl := &d.x.dlg
	scrim := theme.LinearAlpha(argb(0x991d1c1d), d.p.Bg)
	dl.scrim.Layout(gtx, func(gtx C) D {
		fill(gtx, image.Rectangle{Max: full}, scrim)
		return D{Size: full}
	})
	if dl.scrim.Clicked(gtx) || dl.close.Clicked(gtx) {
		d.closeDialog()
		return
	}
	w := min(gtx.Dp(520), full.X-gtx.Dp(32))
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, max(0, full.Y-gtx.Dp(32)))}
	dims := layout.Flex{Axis: layout.Vertical}.Layout(g,
		layout.Rigid(func(gtx C) D {
			return dl.titleBar(layout.Inset{Top: 20, Bottom: 20, Left: 28, Right: 28}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				gtx.Constraints.Min.Y = gtx.Dp(30)
				return row(gtx, layout.Middle,
					layout.Flexed(1, func(gtx C) D {
						return dirFill(gtx, func(gtx C) D {
							if dl.subtitle == "" {
								return d.para(gtx, 22, 30, wBlack, d.p.Text, 0, title)
							}
							// 13/18 content-pry, 24px from the close button, two lines at most
							return col(gtx,
								layout.Rigid(func(gtx C) D { return d.para(gtx, 22, 30, wBlack, d.p.Text, 0, title) }),
								layout.Rigid(func(gtx C) D {
									return layout.Inset{Right: 24}.Layout(gtx, func(gtx C) D { return d.para(gtx, 13, 18, wReg, d.p.Text, 2, dl.subtitle) })
								}),
							)
						})
					}),
					layout.Rigid(func(gtx C) D {
						if !closeBtn {
							return D{}
						}
						return layout.Inset{Left: 8, Right: -8}.Layout(gtx, func(gtx C) D { return d.iconButton(gtx, &dl.close, "close", 36, 20, d.p.Sec) })
					}),
				)
			}))
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 28, Right: 28}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				ch := make([]layout.FlexChild, len(body))
				for i, b := range body {
					b := b
					ch[i] = layout.Rigid(func(gtx C) D { gtx.Constraints.Min.X = gtx.Constraints.Max.X; return b(gtx) })
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
			})
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 24, Bottom: 24, Left: 28, Right: 28}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.E.Layout(gtx, func(gtx C) D {
					ch := make([]layout.FlexChild, 0, 2*len(footer))
					for i, f := range footer {
						if i > 0 {
							ch = append(ch, hgap(12))
						}
						ch = append(ch, layout.Rigid(f))
					}
					return row(gtx, layout.Middle, ch...)
				})
			})
		}),
	)
	call := m.Stop()
	h := dims.Size.Y
	org := image.Pt((full.X-w)/2, max(gtx.Dp(16), (full.Y-h)/2))
	defer op.Offset(org).Push(gtx.Ops).Pop()
	r := image.Rect(0, 0, w, h)
	rad := gtx.Dp(8)
	theme.Shadow(gtx, r, rad, gtx.Dp(24), argb(0x59000000))
	one := max(1, gtx.Dp(1))
	rrect(gtx, r.Inset(-one), rad+one, dirRing(&d.base, d.p.Bg))
	rrect(gtx, r, rad, d.p.Surface)
	gg := gtx
	gg.Constraints = layout.Exact(r.Max)
	dl.box.Layout(gg, func(gtx C) D { return D{Size: r.Max} })
	func() {
		defer clip.UniformRRect(r, rad).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}()
	if dl.resultsShow {
		d.addPeopleResults(gtx)
	}
}

// titleBar notes the title bar's height (the agents invite's results
// menu opens under its picker, below it).
func (dl *cdDialog) titleBar(dims D) D {
	dl.titleH = dims.Size.Y
	return dims
}

// cdPara is a 15/22 dialog paragraph with bold runs: parts alternate
// plain and bold, starting plain.
func (d *ChannelDetails) cdPara(key string, parts ...string) W {
	return func(gtx C) D {
		if len(parts) == 1 {
			return d.para(gtx, 15, 22, wReg, d.p.Text, 0, parts[0])
		}
		spans := make([]richtext.Span, 0, len(parts))
		for i, p := range parts {
			if p == "" {
				continue
			}
			s := richtext.Span{Kind: richtext.Text, Text: p}
			if i%2 == 1 {
				s.Style = richtext.Bold
			}
			spans = append(spans, s)
		}
		return d.richDoc(gtx, "dlg/"+key+"/"+d.id, strings.Join(parts, ""), richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: spans}}}, richview.Options{}, 0)
	}
}

// cdBullets is a ul: each item a 15/22 line after a bullet, 16px in.
func (d *ChannelDetails) cdBullets(items ...string) W {
	return func(gtx C) D {
		ch := make([]layout.FlexChild, 0, len(items))
		for _, it := range items {
			it := it
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return row(gtx, layout.Start,
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Left: 8, Right: 8}.Layout(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 1, "•") })
					}),
					layout.Flexed(1, func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 0, it) }),
				)
			}))
		}
		return col(gtx, ch...)
	}
}

// cdAlert is Slack's error alert (VvO8 level error): #e01e5a text on
// #e01e5a@0.1 with a 1px border, radius 8, padding 12px 16px.
func (d *ChannelDetails) cdAlert(text string, info bool) W {
	return func(gtx C) D {
		fg, bg := d.p.Red, flatten(argb(0x1ae01e5a), d.p.Surface)
		if info {
			fg, bg = d.p.Text, flatten(argb(0x1a1d9bd1), d.p.Surface)
		}
		m := op.Record(gtx.Ops)
		dims := layout.Inset{Top: 12, Bottom: 12, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return d.para(gtx, 15, 22, wReg, fg, 0, text)
		})
		call := m.Stop()
		r := image.Rectangle{Max: dims.Size}
		rrect(gtx, r, gtx.Dp(8), bg)
		call.Add(gtx.Ops)
		return dims
	}
}

func cdSpace(v unit.Dp) W { return func(gtx C) D { return D{Size: image.Pt(0, gtx.Dp(v))} } }

// cdDlgButton is a footer button; busy shows Slack's loading state (the
// label kept, the click ignored) and disabled c-button--disabled.
func (d *ChannelDetails) cdDlgButton(c *widget.Clickable, kind btnKind, label string, disabled bool) W {
	return func(gtx C) D {
		if !disabled {
			return d.button(gtx, c, kind, false, label)
		}
		tw := d.th.Measure(gtx, 15, wBold, label)
		sz := image.Pt(tw+2*gtx.Dp(16), gtx.Dp(36))
		rrect(gtx, image.Rectangle{Max: sz}, gtx.Dp(8), d.p.Track)
		g := gtx
		g.Constraints = layout.Exact(sz)
		layout.Center.Layout(g, func(gtx C) D { return d.label(gtx, 15, wBold, d.p.Muted, label) })
		return D{Size: sz}
	}
}

// layoutDialog draws the open stacked dialog, if any.
func (d *ChannelDetails) layoutDialog(gtx C, full image.Point, inf core.ChannelDetails) {
	dl := &d.x.dlg
	name, _ := d.title(d.conv(inf, true))
	if cdGroup(d.kind) == "channel" {
		name = "#" + name
	}
	cancel := d.cdDlgButton(&dl.cancel, btnOutline, "Cancel", false)
	switch dl.kind {
	case cdDlgLeave:
		// LeaveChannel: the private-channel text (record channels and
		// connected channels have their own)
		d.skModal(gtx, full, "Leave "+name, true, []W{
			d.cdPara("leave", "When you leave a private channel, you’ll no longer be able to see any of its messages. To rejoin this channel later, you’ll need to be invited."),
		}, []W{cancel, d.cdDlgButton(&dl.ok, btnDanger, "Leave Channel", dl.pending)})
	case cdDlgLeaveArchive:
		body := []W{
			d.cdPara("arch1", "Archiving is useful if you don’t think you’ll need this channel any longer. Here’s what will happen when you archive this channel:"),
			cdSpace(8),
			d.cdBullets("No one will be able to send messages to it", "Apps installed in this channel will be disabled", "A message will be posted that you archived the channel"),
			cdSpace(16),
			d.cdPara("arch2", "Messages from archived channels will still show up in search and archives, the same way they do today. You can always unarchive an archived channel later, if you need it again."),
			cdSpace(16),
			d.cdPara("arch3", "Are you sure you want to archive "+name+"?"),
		}
		if dl.err != "" {
			body = append(body, cdSpace(16), d.cdAlert(dl.err, false))
		}
		d.skModal(gtx, full, "Leave and archive "+name, true, body,
			[]W{cancel, d.cdDlgButton(&dl.ok, btnPrimary, "Yes, Leave & Archive the Channel", dl.pending || dl.err != "")})
	case cdDlgAIUse:
		excluded := inf.Props.ExcludeFromAI
		title, can, will, btn, kind := "Restrict AI use of this channel’s content", "cannot", "will not be", "Restrict AI Use", btnDanger
		if excluded {
			title, can, will, btn, kind = "Allow AI use of this channel’s content", "can", "will be", "Allow AI Use", btnPrimary
		}
		var body []W
		if dl.err != "" {
			body = append(body, d.cdAlert(dl.err, false), cdSpace(12))
		}
		users := []string{"Slackbot", "Search AI answers", "Canvas AI"}
		if d.h.Core().Experiment("mcp_ai_channel_exclusions") == "on" {
			users = append(users, "Slack MCP Server")
		}
		body = append(body,
			func(gtx C) D {
				return row(gtx, layout.Baseline,
					layout.Flexed(1, func(gtx C) D {
						return d.cdPara("ai1", "Content from ", name, " "+can+" be used by AI features outside of this channel. ")(gtx)
					}),
				)
			},
			func(gtx C) D { return cdLinkReg(&d.base, gtx, &d.x.aiLearn, 15, "Learn more about AI restrictions.") },
			cdSpace(16),
			d.cdPara("ai2", "This channel’s content, including canvases shared only in this channel, ", will, " used by:"),
			cdSpace(8),
			d.cdBullets(users...),
			cdSpace(8),
			func(gtx C) D {
				return d.para(gtx, 15, 22, wReg, d.p.Text, 0, "Heads up — app AI access is managed by app developers.")
			},
		)
		d.skModal(gtx, full, title, true, body, []W{cancel, d.cdDlgButton(&dl.ok, kind, btn, dl.pending)})
	case cdDlgGuests:
		restricted := inf.Props.RestrictGuests
		title, btn, kind := "Restrict guest access", "Restrict", btnDanger
		text := d.cdPara("g1", "Guests cannot be invited or join ", name, ". All guests will be removed from the channel.")
		if restricted {
			title, btn, kind = "Allow guests", "Allow", btnPrimary
			text = d.cdPara("g2", "Guests can be invited or join ", name, ".")
		}
		var body []W
		if dl.err != "" {
			body = append(body, d.cdAlert(dl.err, false), cdSpace(12))
		}
		d.skModal(gtx, full, title, true, append(body, text), []W{cancel, d.cdDlgButton(&dl.ok, kind, btn, dl.pending)})
	case cdDlgSharing: // MessageAndFileSharingRestrictionsModal
		restricted := inf.Props.RestrictSharing
		title, btn, kind := "Restrict sharing & file downloads", "Restrict", btnDanger
		text := d.cdPara("s1", "Messages and files posted in ", name, " can’t be shared to another channel or in a DM.")
		if restricted {
			title, btn, kind = "Allow sharing & file downloads", "Allow", btnPrimary
			text = d.cdPara("s2", "Messages and files posted in ", name, " can be shared to another channel or in a DM.")
		}
		var body []W
		if dl.err != "" {
			body = append(body, d.cdAlert(dl.err, false), cdSpace(12))
		}
		d.skModal(gtx, full, title, true, append(body, text), []W{cancel, d.cdDlgButton(&dl.ok, kind, btn, dl.pending)})
	case cdDlgPerms:
		d.permsDialog(gtx, full, cancel)
	case cdDlgHide:
		d.skModal(gtx, full, "Hide "+dl.userName+"?", true, []W{
			d.hideLine("user-hide", "Their messages in channels will be hidden from you, and you won’t be notified when they mention or DM you."),
			cdSpace(12),
			d.hideLinkLine(),
			cdSpace(12),
			d.hideLine("notifications-off", "They won’t be notified that you’ve hidden them."),
		}, []W{cancel, d.cdDlgButton(&dl.ok, btnDanger, "Hide This Person", dl.pending)})
	case cdDlgAddPeople:
		if a := dl.add; a != nil && a.agents {
			d.agentInviteDialog(gtx, full, inf, a) // details_agents.go
		} else {
			d.addPeopleDialog(gtx, full, inf)
		}
	case cdDlgPosting:
		d.postingDialog(gtx, full, inf)
	case cdDlgToPublic:
		d.skModal(gtx, full, "Change to a public channel?", true, d.toPublicBody(inf, name),
			[]W{cancel, d.cdDlgButton(&dl.ok, btnPrimary, "Change to Public", dl.pending)})
	case cdDlgToPrivate:
		d.skModal(gtx, full, nsChannelDetails.T("Change to a private channel?"), true, d.toPrivateBody(name),
			[]W{cancel, d.cdDlgButton(&dl.ok, btnDanger, nsChannelDetails.T("Change to Private"), dl.pending)})
	case cdDlgArchive:
		d.skModal(gtx, full, nsChannelDetails.T("Archive this channel?"), true, d.archiveBody(),
			[]W{cancel, d.cdDlgButton(&dl.ok, btnDanger, nsChannelDetails.T("Archive Channel"), dl.pending)})
	case cdDlgDelete:
		d.skModal(gtx, full, nsChannelDetails.T("Delete this channel?"), true, d.deleteBody(inf), d.deleteFooter())
	}
}

// cdHiddenHelpArticle is HideUserConfirmationModal's and Privacy &
// visibility's HelpLink (articleId 0xf60181da113).
const cdHiddenHelpArticle = "https://slack.com/help/articles/16905395872019"

// hideLinkLine is the modal's settings row: "You can always unhide them
// later in <Link>preferences</Link>.", the link a HelpLink, underlined
// under a11y_underline_button_link with underline_links (CX15 bF).
func (d *ChannelDetails) hideLinkLine() W {
	return func(gtx C) D {
		if d.x.hidePrefs.Clicked(gtx) {
			hostHandler{d.h}.OpenURL(cdHiddenHelpArticle)
		}
		ul := d.h.Core().UnderlineButtonLinks()
		return row(gtx, layout.Start,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 1, Right: 12}.Layout(gtx, func(gtx C) D { return d.icon(gtx, "settings", 20, d.p.Sec) })
			}),
			layout.Flexed(1, func(gtx C) D {
				return row(gtx, layout.Baseline,
					layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 1, "You can always unhide them later in ") }),
					layout.Rigid(func(gtx C) D { return cdHelpLink(&d.base, gtx, &d.x.hidePrefs, 15, "preferences", ul) }),
					layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 1, ".") }),
				)
			}),
		)
	}
}

// cdHelpLink is HelpLink's BaseLink: the link blue, underlined on hover,
// or always with underline (underlineStyle "underline").
func cdHelpLink(b *base, gtx C, c *widget.Clickable, size unit.Sp, label string, underline bool) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		dims := b.para(gtx, size, 22, wReg, b.p.Link, 1, label)
		if underline || c.Hovered() { // 1px, just under the baseline
			y := dims.Size.Y - dims.Baseline + gtx.Dp(1)
			fill(gtx, image.Rect(0, y, dims.Size.X, y+gtx.Dp(1)), b.p.Link)
		}
		return dims
	})
}

// hideLine is one of HideUserConfirmationModal's icon rows.
func (d *ChannelDetails) hideLine(icon, text string) W {
	return func(gtx C) D {
		return row(gtx, layout.Start,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 1, Right: 12}.Layout(gtx, func(gtx C) D { return d.icon(gtx, icon, 20, d.p.Sec) })
			}),
			layout.Flexed(1, func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 0, text) }),
		)
	}
}

// permsDialog is EditChannelPermissionsModal: no close button; "Some
// permissions are locked by your workspace or organization." when some
// aren't configurable; "Members of this channel can:" and a checkbox per
// permission; Save once something changed.
func (d *ChannelDetails) permsDialog(gtx C, full image.Point, cancel W) {
	dl := &d.x.dlg
	title, lead := "Manage member permissions", "Members of this channel can:"
	if dl.acct == core.AccountMultiChannelGuest {
		title, lead = "Manage guest permissions", "Guests of this channel can:"
	}
	all, _, _ := d.x.acct.get()
	orig := map[string]bool{}
	locked := false
	for _, p := range all[dl.acct] {
		orig[p.Permission] = p.Allowed
		locked = locked || !p.Configurable
	}
	changed := false
	for _, p := range dl.edit {
		changed = changed || orig[p.Permission] != p.Allowed
	}
	var body []W
	if dl.err != "" {
		body = append(body, d.cdAlert(dl.err, false), cdSpace(8))
	}
	if locked {
		body = append(body, d.cdAlert("Some permissions are locked by your workspace or organization.", true), cdSpace(20))
	}
	body = append(body, func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 0, lead) }, cdSpace(8))
	for i := range dl.edit {
		i := i
		p := dl.edit[i]
		c := dirClick(dl.checks, p.Permission)
		if c.Clicked(gtx) && p.Configurable && !dl.pending {
			dl.edit[i].Allowed = !dl.edit[i].Allowed
			p = dl.edit[i]
		}
		body = append(body, func(gtx C) D {
			return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx C) D {
				draw := func(gtx C) D {
					fg := d.p.Text
					if !p.Configurable {
						fg = cdDim(fg)
					}
					return row(gtx, layout.Start,
						layout.Rigid(func(gtx C) D {
							return layout.Inset{Top: 3, Right: 8}.Layout(gtx, func(gtx C) D { return cdCheckbox(&d.base, gtx, p.Allowed, !p.Configurable) })
						}),
						layout.Flexed(1, func(gtx C) D { return d.para(gtx, 15, 22, wReg, fg, 0, p.Name) }),
					)
				}
				if !p.Configurable {
					return draw(gtx)
				}
				return c.Layout(gtx, func(gtx C) D { pointerCursor(gtx); return draw(gtx) })
			})
		})
	}
	d.skModal(gtx, full, title, false, body, []W{cancel, d.cdDlgButton(&dl.ok, btnPrimary, "Save", dl.pending || !changed)})
}

// updateDialog runs the open dialog's buttons and finished writes.
func (d *ChannelDetails) updateDialog(gtx C, inf core.ChannelDetails) {
	dl := &d.x.dlg
	if dl.kind == cdDlgNone {
		return
	}
	if dl.cancel.Clicked(gtx) {
		d.closeDialog()
		return
	}
	if d.x.aiLearn.Clicked(gtx) {
		hostHandler{d.h}.OpenURL(cdAIHelpArticle)
	}
	c, id := d.h.Core(), d.id
	start := func(f func(ctx context.Context) error) {
		dl.pending, dl.err = true, ""
		dl.op.load(d.h, false, func(ctx context.Context) (bool, error) { return true, f(ctx) })
	}
	if dl.kind == cdDlgAddPeople {
		if a := dl.add; a != nil && a.agents {
			d.updateAgentInvite(gtx, a) // details_agents.go
			if dl.kind != cdDlgAddPeople {
				return
			}
		}
		d.updateAddPeople(gtx, inf)
		return
	}
	if dl.kind == cdDlgPosting {
		d.updatePosting(gtx, inf)
		return
	}
	if dl.kind == cdDlgDelete {
		if dl.confirmAll.Clicked(gtx) {
			dl.confirmed = !dl.confirmed
		}
		if dl.back.Clicked(gtx) { // "archive a channel instead": this modal closes, the archive one opens
			d.openDialog(cdDlgArchive)
			return
		}
	}
	if dl.ok.Clicked(gtx) && !dl.pending && (dl.kind != cdDlgDelete || dl.confirmed) {
		switch dl.kind {
		case cdDlgLeave:
			start(func(ctx context.Context) error { return c.Leave(ctx, id) })
			d.leavePending = true
		case cdDlgLeaveArchive:
			if dl.err == "" {
				start(func(ctx context.Context) error { return c.Archive(ctx, id) })
			}
		case cdDlgAIUse:
			v := !inf.Props.ExcludeFromAI
			start(func(ctx context.Context) error {
				return c.SetChannelProperties(ctx, id, "set_channel_ai_exclusion_from_channel_settings", map[string]any{"exclude_from_slack_ai": v})
			})
		case cdDlgGuests:
			v := !inf.Props.RestrictGuests
			start(func(ctx context.Context) error {
				return c.SetChannelProperties(ctx, id, "set_guest_restriction_from_channel_settings", map[string]any{"restrict_guests": v})
			})
		case cdDlgSharing:
			v := !inf.Props.RestrictSharing
			start(func(ctx context.Context) error {
				return c.SetChannelProperties(ctx, id, "set_message_and_file_sharing_restriction_from_channel_settings",
					map[string]any{"restrict_message_and_file_sharing": v})
			})
		case cdDlgPerms:
			all, _, _ := d.x.acct.get()
			orig := map[string]bool{}
			for _, p := range all[dl.acct] {
				orig[p.Permission] = p.Allowed
			}
			var changed []core.AccountTypePermission
			for _, p := range dl.edit {
				if orig[p.Permission] != p.Allowed {
					changed = append(changed, p)
				}
			}
			acct := dl.acct
			start(func(ctx context.Context) error { return c.SetAccountTypePermissions(ctx, id, acct, changed) })
		case cdDlgHide:
			uid, dm := dl.userID, dl.dmID
			start(func(ctx context.Context) error { return c.HideUser(ctx, uid, dm) })
		case cdDlgToPublic:
			start(func(ctx context.Context) error { return c.ConvertToPublic(ctx, id) })
		case cdDlgToPrivate:
			start(func(ctx context.Context) error { return c.ConvertToPrivate(ctx, id) })
		case cdDlgArchive:
			start(func(ctx context.Context) error { return c.Archive(ctx, id) })
		case cdDlgDelete:
			start(func(ctx context.Context) error { return c.DeleteChannel(ctx, id) })
		}
	}
	if !dl.pending {
		return
	}
	_, done, err := dl.op.get()
	if !done || dl.op.busy() {
		return
	}
	dl.pending = false
	switch dl.kind {
	case cdDlgLeave:
		d.leavePending = false
		if err != nil {
			d.closeDialog()
			d.h.Toast("Couldn’t leave #" + inf.Conv.Name + ": " + dirErr(err))
			return
		}
		d.closeDialog()
		d.h.Toast("You left #" + inf.Conv.Name)
		d.closed = true
		d.h.Navigate("activity")
	case cdDlgLeaveArchive:
		if err != nil {
			dl.err = cdArchiveError(err)
			return
		}
		d.closeDialog()
		d.closed = true
		d.h.Navigate("activity")
	case cdDlgAIUse, cdDlgGuests, cdDlgSharing:
		if err != nil {
			switch dl.kind {
			case cdDlgGuests:
				dl.err = "Unable to update guest access. Please try again later."
			case cdDlgSharing:
				dl.err = "Unable to update sharing restriction. Please try again later."
			default:
				dl.err = "Unable to update AI settings. Please try again later."
			}
			return
		}
		kind := dl.kind
		d.info.mutate(func(v *core.ChannelDetails) {
			switch kind {
			case cdDlgAIUse:
				v.Props.ExcludeFromAI = !v.Props.ExcludeFromAI
			case cdDlgGuests:
				v.Props.RestrictGuests = !v.Props.RestrictGuests
			case cdDlgSharing:
				v.Props.RestrictSharing = !v.Props.RestrictSharing
			}
		})
		d.closeDialog()
		d.refreshInfo()
	case cdDlgPerms:
		// saveChannelManagementPermissions, then the list is fetched again
		// either way (edit_channel_permissions_refresh)
		d.x.acct.load(d.h, true, func(ctx context.Context) (map[string][]core.AccountTypePermission, error) {
			return c.AccountTypePermissions(ctx, id)
		})
		if err != nil {
			if cdNetErr(err) {
				dl.err = "Changes couldn’t be saved. Looks like there was a problem with your network connection."
			} else {
				dl.err = "There was a problem saving these changes. Give it another try in a few minutes."
			}
			return
		}
		d.closeDialog()
	case cdDlgHide:
		d.closeDialog()
		if err != nil {
			d.h.Toast("Couldn’t hide " + dl.userName + ": " + friendlyErr(err))
			return
		}
		d.closed = true
		d.h.Navigate("dms")
	case cdDlgToPublic:
		if err != nil {
			dl.err = toPublicError(err)
			return
		}
		d.closeDialog()
		d.refreshInfo()
	case cdDlgToPrivate:
		if err != nil {
			dl.err = cdErrCode(err)
			return
		}
		d.closeDialog()
		d.refreshInfo()
	case cdDlgArchive:
		if err != nil {
			_, admin, _ := d.selfRoles()
			dl.err = archiveModalError(err, admin, isPrivateConv(d.conv(inf, true)))
			return
		}
		d.closeDialog() // onSubmit's closeAll: the details close
		d.closed = true
	case cdDlgDelete:
		if err != nil {
			dl.err = cdErrCode(err)
			return
		}
		d.closeDialog()
		d.closed = true
		d.h.Navigate("activity")
	}
}

// cdNetErr reports a failure that never got a Slack error code.
func cdNetErr(err error) bool {
	s := err.Error()
	return !strings.Contains(s, "_") && !strings.Contains(s, "slack:")
}

// cdArchiveError is BaseArchiveDialog's renderError with andLeave set.
func cdArchiveError(err error) string {
	s := err.Error()
	has := func(code string) bool { return strings.Contains(s, code) }
	switch {
	case has("last_ra_channel"):
		return "Sorry, you can’t archive this channel because it is the only channel one of the guest account members belongs to."
	case has("restricted_action"), has("user_is_restricted"):
		return "Since you’re the last one here, you’ll need to invite an admin and ask them to archive the channel."
	case has("already_archived"):
		return "This channel was already archived."
	case has("cant_archive_general"):
		return "This channel cannot be archived."
	}
	return "Archiving failed with error: " + cdErrCode(err)
}

// cdErrCode is the Slack error code in err (or its text).
func cdErrCode(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && !strings.Contains(s[i+2:], " ") {
		return s[i+2:]
	}
	return s
}

// ---- leaving (maybeLeaveChannel) ----

// maybeLeave is Slack's maybeLeaveChannel from details: a public channel is
// left at once; an Slack Connect private channel asks (LeaveChannel); any
// other private channel first reads fresh member counts: the last member
// gets the archive dialog (confirm_leave_and_archive), the others
// LeaveChannel.
func (d *ChannelDetails) maybeLeave(inf core.ChannelDetails) {
	if d.leavePending {
		return
	}
	if inf.Conv.Kind != "private" {
		d.startLeave()
		return
	}
	if inf.IsExtShared {
		d.openDialog(cdDlgLeave)
		return
	}
	c, id := d.h.Core(), d.id
	d.x.leaveCount.load(d.h, false, func(ctx context.Context) (core.MemberCounts, error) { return c.MemberCounts(ctx, id) })
}

// updateLeave opens the dialog maybeLeaveChannel picked once the counts
// arrive (an unreadable count falls back to LeaveChannel, as Slack does).
func (d *ChannelDetails) updateLeave() {
	if !d.x.leaveCount.started() || d.x.leaveCount.busy() {
		return
	}
	mc, done, err := d.x.leaveCount.get()
	if !done {
		return
	}
	d.x.leaveCount = async[core.MemberCounts]{}
	if err == nil && mc.People == 1 {
		d.openDialog(cdDlgLeaveArchive)
		return
	}
	d.openDialog(cdDlgLeave)
}

// ---- header: VIP and Hide (1:1 DMs) ----

// canToggleVIP: a person other than yourself (not a bot or Slackbot).
func (d *ChannelDetails) dmPerson(inf core.ChannelDetails) (core.User, bool) {
	uid := d.dmUser(inf)
	if uid == "" || uid == d.selfID() || uid == "USLACKBOT" {
		return core.User{}, false
	}
	u, ok := d.user(uid)
	if !ok || u.IsBot || u.AppUser || u.Workflow {
		return core.User{}, false
	}
	return u, true
}

func (d *ChannelDetails) isVIP(uid string) bool {
	if d.x.vipPending {
		return d.x.vipOv
	}
	for _, id := range d.h.Core().VIPs() {
		if id == uid {
			return true
		}
	}
	return false
}

// updateHeaderDM runs VIP (Add/remove user from priority status) and Hide
// (hideOrUnhideUserById: unhide at once, else the confirmation).
func (d *ChannelDetails) updateHeaderDM(gtx C, inf core.ChannelDetails) {
	c := d.h.Core()
	u, ok := d.dmPerson(inf)
	if d.vip.Clicked(gtx) && ok && !d.x.vipPending {
		on := !d.isVIP(u.ID)
		uid, name := u.ID, displayName(u)
		d.x.vipOv, d.x.vipPending = on, true
		d.x.vipOp.load(d.h, false, func(ctx context.Context) (bool, error) {
			if on {
				return true, c.AddVIP(ctx, uid)
			}
			return true, c.RemoveVIP(ctx, uid)
		})
		d.x.vipName = name
	}
	if d.x.vipPending && !d.x.vipOp.busy() {
		if _, done, err := d.x.vipOp.get(); done {
			d.x.vipPending = false
			switch {
			case err != nil && strings.Contains(err.Error(), "too_many_vips"):
				d.h.Toast(d.x.vipName + " could not be added to VIP. You’ve reached the maximum number of VIPs.")
			case err != nil && strings.Contains(err.Error(), "user_not_found"):
				d.h.Toast("User is not eligible to be added as VIP.")
			case err != nil:
				d.h.Toast("Hmm, something went wrong. Try again?")
			case d.x.vipOv:
				d.h.Toast("Added " + d.x.vipName + " to VIPs")
			default:
				d.h.Toast("Removed " + d.x.vipName + " from VIPs")
			}
		}
	}
	if d.hide.Clicked(gtx) && ok && !d.x.hidePending {
		if c.UserHidden(u.ID) {
			uid := u.ID
			d.x.hidePending = true
			d.x.hideOp.load(d.h, false, func(ctx context.Context) (bool, error) { return true, c.UnhideUser(ctx, uid) })
		} else {
			d.openDialog(cdDlgHide)
			d.x.dlg.userID, d.x.dlg.userName, d.x.dlg.dmID = u.ID, displayName(u), d.id
		}
	}
	if d.x.hidePending && !d.x.hideOp.busy() {
		if _, done, err := d.x.hideOp.get(); done {
			d.x.hidePending = false
			if err != nil {
				d.h.Toast("Couldn’t unhide " + displayName(u) + ": " + friendlyErr(err))
			}
		}
	}
}

// cdHeaderBtn is the details header's outline small button with a leading
// 16px icon (VIP: user-add, or user-hide once a VIP; Hide: user-hide).
func (d *ChannelDetails) cdHeaderBtn(gtx C, c *widget.Clickable, icon, label string) D {
	return cdBtn(&d.base, gtx, c, icon, label, false, 0)
}

// ---- Escape ----

// escapeDialog closes the stacked dialog (Escape goes to the topmost
// modal), reporting whether one was open.
func (d *ChannelDetails) escapeDialog() bool {
	if d.x.dlg.kind == cdDlgNone {
		return false
	}
	d.closeDialog()
	return true
}

// ---- Add people to this conversation (MpdmAddPeopleModal) ----

// cdMaxGroupDM is the people a group DM holds, you included.
const cdMaxGroupDM = 9

// Conversation history choices (hLnn): none, today, all, custom
// (details_history.go).
const (
	cdHistNone = iota
	cdHistToday
	cdHistAll
	cdHistCustom
)

// cdAddPeople is the modal's state: step 0 picks people (SelectMembersForm),
// step 1 the history (HistoryOptions).
type cdAddPeople struct {
	// agents: the channel invite modal scoped to agents and apps
	// (details_agents.go), else the group DM's add people modal
	agents     bool
	explore    widget.Clickable // "Explore more agents and apps"
	pickY      int              // agents: the picker's top in the dialog (px)
	step       int
	q          *search
	deb        debounce
	res        async[[]core.User]
	picked     []core.User
	tokClk     map[string]*widget.Clickable
	resClk     map[string]*widget.Clickable
	members    map[string]bool // already in the conversation
	hist       int
	histClk    [4]widget.Clickable
	hx         cdHistState // Custom, the preview, the speed bump (details_history.go)
	confirm    bool        // "Include history from the beginning?"
	creating   bool
	focused    bool
	focusTries int
}

func (d *ChannelDetails) openAddPeople(inf core.ChannelDetails) {
	d.openDialog(cdDlgAddPeople)
	a := &cdAddPeople{q: newSearch("ex. Matt or @mbrewer"), tokClk: map[string]*widget.Clickable{}, resClk: map[string]*widget.Clickable{}, members: map[string]bool{}}
	for _, u := range d.dmMembers(d.conv(inf, true)) {
		a.members[u.ID] = true
	}
	a.members[d.selfID()] = true
	if uid := d.dmUser(inf); uid != "" {
		a.members[uid] = true
	}
	c := d.h.Core()
	a.hist = cdHistToday // useDefaultExcludeHistory (boot-shared J1Xx): the org pref, under the experiment
	if c.Experiment("mpdm_default_exclude_history_clients_webapp") == "on" && c.EnterpriseOrgPrefTrue("mpdm_add_people_default_exclude_history") {
		a.hist = cdHistNone
	}
	d.x.dlg.add = a
	d.histOpen(a)
}

// addRoom is how many more people fit: 9 minus everyone in it and picked
// (the channel invite's maxInviteLimit, 1,000, for agents and apps).
func (a *cdAddPeople) addRoom() int {
	if a.agents {
		return 1000 - len(a.picked)
	}
	return cdMaxGroupDM - len(a.members) - len(a.picked)
}

func (d *ChannelDetails) updateAddPeople(gtx C, inf core.ChannelDetails) {
	dl := &d.x.dlg
	a := dl.add
	if a == nil {
		return
	}
	c, id := d.h.Core(), d.id
	if a.step == 0 {
		if !a.focused && a.focusTries < 5 { // focusOnMount; the input appears this frame
			a.focusTries++
			gtx.Execute(key.FocusCmd{Tag: &a.q.ed})
			a.focused = gtx.Focused(&a.q.ed)
			gtx.Execute(op.InvalidateCmd{})
		}
		q, changed, _ := a.q.update(gtx)
		if changed {
			a.deb.set(q, gtx.Now)
		}
		if dq, due := a.deb.due(gtx, dirDebounce); due && dq != "" {
			a.res.load(d.h, true, func(ctx context.Context) ([]core.User, error) { return c.SearchUsers(ctx, dq, 10) })
		}
		for uid, t := range a.tokClk {
			if t.Clicked(gtx) {
				for i, u := range a.picked {
					if u.ID == uid {
						a.picked = append(a.picked[:i], a.picked[i+1:]...)
						break
					}
				}
			}
		}
		if dl.ok.Clicked(gtx) && len(a.picked) > 0 {
			a.step = 1
		}
		return
	}
	d.updateHistory(gtx, a)
	if d.x.dlg.kind != cdDlgAddPeople {
		return // the speed bump's Cancel closed the modal
	}
	for i := range a.histClk {
		if a.histClk[i].Clicked(gtx) && !a.hx.tooMany {
			a.hist = i
		}
	}
	if dl.back.Clicked(gtx) && !a.creating {
		a.step, a.confirm = 0, false
		a.focused, a.focusTries = false, 0
		return
	}
	if dl.confirmBack.Clicked(gtx) {
		a.confirm = false
	}
	submit := dl.ok.Clicked(gtx) && !a.creating && !a.hx.tooMany && !(a.hist == cdHistCustom && a.hx.day.IsZero())
	if submit && a.hist == cdHistAll && !a.confirm {
		a.confirm = true // "Include history from the beginning?"
		return
	}
	if (submit && a.hist != cdHistAll) || (dl.confirmAll.Clicked(gtx) && a.confirm && !a.creating) {
		ids := make([]string, len(a.picked))
		for i, u := range a.picked {
			ids[i] = u.ID
		}
		now := gtx.Now
		if now.IsZero() {
			now = time.Now()
		}
		start := histStartTS(a, now)
		a.creating = true
		dl.addOp.load(d.h, false, func(ctx context.Context) (core.Conv, error) { return c.AddToConversation(ctx, id, ids, start) })
	}
	if a.creating && !dl.addOp.busy() {
		if conv, done, err := dl.addOp.get(); done {
			a.creating = false
			switch {
			case err != nil && strings.Contains(err.Error(), "too_many_messages"):
				a.hx.tooMany = true // the "Too many messages selected" speed bump
			case err != nil:
				dl.err = "Hmm…something went wrong"
			case conv.ID != "":
				d.closeDialog()
				d.closed = true // closeAll, then the new conversation
				d.h.Navigate("c/" + conv.ID)
			}
		}
	}
}

// cdUnixDay is the start of today as Slack sends it
// (moment().startOf("day").unix(), as a string).
func cdUnixDay(sec int64) string { return strconv.FormatInt(sec, 10) }

// addPeopleDialog draws the open step.
func (d *ChannelDetails) addPeopleDialog(gtx C, full image.Point, inf core.ChannelDetails) {
	dl := &d.x.dlg
	a := dl.add
	if a == nil {
		return
	}
	dl.resultsShow = false
	if a.step == 0 {
		room := a.addRoom()
		body := []W{func(gtx C) D { return d.addPeoplePicker(gtx, a) }}
		if room > 0 {
			body = append(body, cdSpace(8), func(gtx C) D {
				return d.para(gtx, 13, 18, wReg, d.p.Muted, 0, "DMs can have up to 9 people (including you).")
			})
		} else {
			body = append(body, cdSpace(8), d.cdAlert("Only 9 people can be in a group DM (including you). Create a private channel to add more.", true))
		}
		d.skModal(gtx, full, "Add people to this conversation", true, body,
			[]W{d.cdDlgButton(&dl.ok, btnPrimary, "Next", len(a.picked) == 0)})
		return
	}
	names := make([]string, len(a.picked))
	for i, u := range a.picked {
		names[i] = displayName(u)
	}
	who := cdJoinNames(names)
	ns := nsMpdmAudience
	body := []W{
		d.cdPara("hist", "Choose whether "+who+" can see past messages and files once they’re added to the conversation."),
		cdSpace(16),
		func(gtx C) D { // SelectHistoryOptionForm spans the modal (its own 24px padding)
			return layout.Inset{Left: -28, Right: -28}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return d.historyForm(gtx, a)
			})
		},
	}
	if dl.err != "" {
		body = append(body, cdSpace(8), d.cdAlert(dl.err, false))
	}
	switch {
	case a.hx.tooMany:
		body = append(body, d.historyBump()...)
		d.skModal(gtx, full, ns.T("Include conversation history?"), true, body, []W{
			d.cdDlgButton(&a.hx.bumpCancel, btnOutline, ns.T("Cancel"), false),
			d.cdDlgButton(&a.hx.bumpOK, btnPrimary, ns.T("OK"), false),
		})
		return
	case a.confirm:
		body = append(body, cdSpace(8), func(gtx C) D {
			return d.para(gtx, 15, 22, wBold, d.p.Text, 0, ns.T("Include history from the beginning?"))
		},
			d.cdPara("histall", ns.T("This lets everyone see past messages and files once they’re added to the conversation.")))
		d.skModal(gtx, full, ns.T("Include conversation history?"), true, body, []W{
			d.cdDlgButton(&dl.confirmBack, btnOutline, ns.T("Go Back"), false),
			d.cdDlgButton(&dl.confirmAll, btnPrimary, ns.T("Confirm"), a.creating),
		})
		return
	}
	noDate := a.hist == cdHistCustom && a.hx.day.IsZero()
	d.skModal(gtx, full, ns.T("Include conversation history?"), true, body, []W{
		func(gtx C) D {
			return d.para(gtx, 13, 18, wReg, d.p.Muted, 1, ns.T("Once included, it can’t be undone."))
		},
		d.cdDlgButton(&dl.back, btnOutline, ns.T("Back"), false),
		d.cdDlgButton(&dl.ok, btnPrimary, ns.T("Done"), a.creating || noDate),
	})
}

// cdJoinNames lists names the way HistoryOptionsModalHeader does: up to
// three joined, else "A, B, and N others".
func cdJoinNames(ns []string) string {
	switch len(ns) {
	case 0:
		return ""
	case 1:
		return ns[0]
	case 2:
		return ns[0] + " and " + ns[1]
	case 3:
		return ns[0] + ", " + ns[1] + ", and " + ns[2]
	}
	return ns[0] + ", " + ns[1] + ", and " + humanCount(len(ns)-2) + " others"
}

// cdRadio is a 16px radio: a 1px ring, or #1264a3 with a white dot.
func cdRadio(b *base, gtx C, on bool) D {
	s := gtx.Dp(16)
	c := image.Pt(s/2, s/2)
	if on {
		theme.Circle(gtx, c, s, b.p.Link)
		theme.Circle(gtx, c, gtx.Dp(6), rgb(0xffffff))
	} else {
		theme.Circle(gtx, c, s, b.p.BorderStr)
		theme.Circle(gtx, c, s-2*max(1, gtx.Dp(1)), b.p.Surface)
	}
	return D{Size: image.Pt(s, s)}
}

// addPeoplePicker is SelectMembersForm's multi-select: the picked people as
// tokens, then the input; matches open in a menu under it.
func (d *ChannelDetails) addPeoplePicker(gtx C, a *cdAddPeople) D {
	dl := &d.x.dlg
	w := gtx.Constraints.Max.X
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: 6, Bottom: 6, Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		ch := make([]layout.FlexChild, 0, len(a.picked)+1)
		for _, u := range a.picked {
			u := u
			t := dirClick(a.tokClk, u.ID)
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 4, Top: 2, Bottom: 2}.Layout(gtx, func(gtx C) D {
					return t.Layout(gtx, func(gtx C) D {
						pointerCursor(gtx)
						mm := op.Record(gtx.Ops)
						td := layout.Inset{Top: 2, Bottom: 2, Left: 4, Right: 6}.Layout(gtx, func(gtx C) D {
							return row(gtx, layout.Middle,
								layout.Rigid(func(gtx C) D { return d.avatar(gtx, u.Avatar, 20) }),
								hgap(6),
								layout.Rigid(func(gtx C) D { return d.label(gtx, 15, wBold, d.p.Text, displayName(u)) }),
								hgap(4),
								layout.Rigid(func(gtx C) D { return d.icon(gtx, "close", 12, d.p.Sec) }),
							)
						})
						call := mm.Stop()
						rrect(gtx, image.Rectangle{Max: td.Size}, gtx.Dp(4), flatten(cdTileBg, d.p.Surface))
						call.Add(gtx.Ops)
						return td
					})
				})
			}))
		}
		ch = append(ch, layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Min.Y = gtx.Dp(28)
			return layout.W.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				if a.addRoom() <= 0 {
					return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(28))} // full: typing is blocked
				}
				return d.th.Editor(gtx, &a.q.ed, 15, d.p.Text, d.p.Mention)
			})
		}))
		return row(gtx, layout.Middle, ch...)
	})
	call := m.Stop()
	r := image.Rect(0, 0, w, max(dims.Size.Y, gtx.Dp(44)))
	rad := gtx.Dp(8)
	if gtx.Focused(&a.q.ed) {
		ring := gtx.Dp(5)
		rrect(gtx, r.Inset(-ring), rad+ring, flatten(theme.WithAlpha(d.p.Link, 0x4d), d.p.Surface))
		one := max(1, gtx.Dp(1))
		rrect(gtx, r.Inset(-one), rad+one, d.p.Link)
	}
	rrect(gtx, r, rad, d.p.Surface)
	if !gtx.Focused(&a.q.ed) {
		border(gtx, r, rad, max(1, gtx.Dp(1)), d.p.BorderStr)
	}
	call.Add(gtx.Ops)
	if a.q.text() == "" && len(a.picked) == 0 {
		dirAt(gtx, image.Pt(gtx.Dp(8), (r.Dy()-gtx.Sp(22))/2), func(gtx C) D { return d.label(gtx, 15, wReg, d.p.Faint, a.q.ph) })
	}
	if a.q.text() != "" {
		// the results menu opens below the field (drawn over the dialog)
		top := gtx.Dp(70)
		if a.agents {
			top = a.pickY
		}
		dl.resultsShow, dl.resultsW = true, w
		dl.resultsAt = image.Pt(gtx.Dp(28), top+r.Dy()+gtx.Dp(4))
	}
	return D{Size: r.Size()}
}

// addPeopleResults is the select's menu: up to 10 people (no bots, no
// deactivated, not you or Slackbot) with avatar and presence, those already
// in the conversation disabled as "Already in this conversation".
func (d *ChannelDetails) addPeopleResults(gtx C) {
	dl := &d.x.dlg
	a := dl.add
	if a == nil {
		return
	}
	res, loaded, err := a.res.get()
	self := d.selfID()
	var us []core.User
	for _, u := range res {
		if a.agents { // isAgentsAppsRoster: agents and apps only
			if !cdIsAgentOrApp(u) || u.Deleted || u.ID == core.SlackbotUserID {
				continue
			}
		} else if u.IsBot || u.Deleted || u.ID == self || u.ID == "USLACKBOT" {
			continue
		}
		us = append(us, u)
		if len(us) == 10 {
			break
		}
	}
	defer op.Offset(dl.resultsAt).Push(gtx.Ops).Pop() // skModal has offset to the dialog
	w := dl.resultsW
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Dp(400))}
	dims := layout.Inset{Top: 12, Bottom: 12}.Layout(g, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		if !loaded || err != nil || len(us) == 0 {
			msg := "No matches found"
			if a.agents && loaded && !a.res.busy() { // getNoResultsText under Io
				return layout.Inset{Left: 24, Right: 24, Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
					return d.cdRichCentered(gtx, "agents-none", nsChannelInvite.RT("No agents or apps found matching {query}", "query", a.q.text()), "query")
				})
			}
			if !loaded || a.res.busy() {
				msg = "Searching…"
			}
			return layout.Inset{Left: 24, Right: 24, Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Muted, 1, msg) })
		}
		ch := make([]layout.FlexChild, 0, len(us))
		for _, u := range us {
			u := d.viewUser(u)
			in := a.members[u.ID]
			picked := false
			for _, p := range a.picked {
				picked = picked || p.ID == u.ID
			}
			c := dirClick(a.resClk, u.ID)
			if c.Clicked(gtx) && !in && !picked && a.addRoom() > 0 {
				a.picked = append(a.picked, u)
				a.q.ed.SetText("")
				a.q.last = ""
				a.res = async[[]core.User]{}
				gtx.Execute(key.FocusCmd{Tag: &a.q.ed})
			}
			ch = append(ch, layout.Rigid(func(gtx C) D {
				draw := func(gtx C) D {
					return layout.Inset{Top: 4, Bottom: 4, Left: 24, Right: 24}.Layout(gtx, func(gtx C) D {
						fg := d.p.Text
						if in {
							fg = cdDim(fg)
						}
						if p := d.h.Core().UserPresence(u.ID); p != "" {
							u.Presence = p
						}
						return row(gtx, layout.Middle,
							layout.Rigid(func(gtx C) D { return d.avatar(gtx, u.Avatar, 20) }),
							hgap(8),
							layout.Rigid(func(gtx C) D { return d.label(gtx, 15, wBold, fg, displayName(u)) }),
							layout.Rigid(func(gtx C) D {
								if u.Presence == "" {
									return D{}
								}
								return dirPresence(&d.base, gtx, u.Presence)
							}),
							layout.Flexed(1, func(gtx C) D {
								return dirFill(gtx, func(gtx C) D {
									if !in {
										return D{}
									}
									already := "Already in this conversation"
									if a.agents {
										already = nsChannelInvite.T("Already in this channel") // getChannelInviteExceptionReason
									}
									return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D {
										return d.label(gtx, 13, wReg, d.p.Muted, already)
									})
								})
							}),
						)
					})
				}
				if in {
					return draw(gtx)
				}
				return d.hoverable(gtx, c, false, 0, draw)
			}))
		}
		return col(gtx, ch...)
	})
	call := m.Stop()
	r := image.Rectangle{Max: dims.Size}
	rad := gtx.Dp(8)
	theme.Shadow(gtx, r, rad, gtx.Dp(12), argb(0x26000000))
	one := max(1, gtx.Dp(1))
	rrect(gtx, r.Inset(-one), rad+one, dirRing(&d.base, d.p.Bg))
	rrect(gtx, r, rad, d.p.Surface)
	func() {
		defer clip.UniformRRect(r, rad).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}()
}

// settingsVisible is isSettingsVisible for the tab strip: channels always
// have the permissions section, group DMs Copy members (on a paid team),
// 1:1 DMs none of Clack's sections; guests never see the tab.
func (d *ChannelDetails) settingsVisible() bool {
	c := d.h.Core()
	if c == nil {
		return false
	}
	if u, ok := c.User(d.selfID()); ok && u.Guest {
		return false
	}
	switch cdGroup(d.kind) {
	case "im":
		return false
	case "mpim":
		return c.C != nil && c.C.Creds.EnterpriseID != ""
	}
	return true
}

// updateAddApps opens the apps browser (popups.OpenAppsBrowser, Slack's
// openAppsBrowserModal with addAppsVariant) from the Integrations tab's
// "Add an App" and from Agents & apps' "Add agents and apps" while
// agents_in_channels_improvements is off (on, the reworked tab opens the
// channel invite modal scoped to agents and apps; details_agents.go).
func (d *ChannelDetails) updateAddApps(gtx C) {
	if !d.addApp.Clicked(gtx) {
		return
	}
	if ph, ok := d.h.(popups.Host); ok {
		popups.OpenAppsBrowser(ph, d.id)
	}
}

// updateSlackParts runs the Slack parts' clicks and finished writes.
func (d *ChannelDetails) updateSlackParts(gtx C, inf core.ChannelDetails, loaded bool) {
	d.updateDialog(gtx, inf)
	if loaded {
		d.updateTabPerms(gtx, inf)
	} else {
		d.tabsPerm.update(gtx)
	}
	if d.x.tabsPending && !d.x.tabsOp.busy() {
		if _, done, err := d.x.tabsOp.get(); done {
			d.x.tabsPending = false
			if err != nil { // ManageChannelTabsPermissions puts the old value back
				prev := d.x.tabsPrev
				d.info.mutate(func(v *core.ChannelDetails) { v.Props.WhoCanManageTabs = prev })
			}
		}
	}
	if d.x.suggestPending && !d.x.suggestOp.busy() {
		if _, done, _ := d.x.suggestOp.get(); done {
			d.x.suggestPending = false // a failed write leaves the pref as it was
		}
	}
	d.updateCopy(gtx)
	if cdGroup(d.kind) == "im" && loaded {
		d.updateHeaderDM(gtx, inf)
	}
	// HuddlesLinkCTAs: start the huddle or copy its link, then close
	if d.huddleStart.Clicked(gtx) {
		startHuddle(d.h, d.tt, startHuddleArgs{ChannelID: d.id})
		d.closed = true
	}
	if d.huddleLink.Clicked(gtx) {
		if hl, ok := d.h.(interface{ CopyHuddleLink(channel string) }); ok {
			hl.CopyHuddleLink(d.id)
		}
		d.closed = true
	}
	// Tabs > New Tab: AddTabMenuTrigger opens the tab bar's AddTabMenu
	if d.newTab.Clicked(gtx) {
		if m, ok := d.h.(interface{ AddTabMenu(channel string) }); ok {
			m.AddTabMenu(d.id)
		}
	}
}

// ---- test hooks (the ui package's tests and the ch-details-shots set) ----

// Press clicks a control by name: leave, add-people (About in a DM, the
// Members row in a group DM), vip, hide, ai-use, guests, member-perms,
// suggest, new-tab, ok, cancel, back, conversation-name (a group DM's
// "Conversation name" Edit), posting (Settings › Posting permissions),
// save, add-app ("Add an App" / "Add agents and apps"), to-public
// (Settings › Change to a public channel), to-private, archive,
// unarchive, delete, delete-confirm, archive-instead (managing-this-channel's
// other rows and the delete modal).
func (d *ChannelDetails) Press(name string) {
	cs := map[string]*widget.Clickable{
		"leave": &d.leaveRow, "vip": &d.vip, "hide": &d.hide, "ai-use": &d.x.aiRow, "guests": &d.x.guestRow,
		"member-perms": &d.x.memPermRow, "suggest": &d.x.suggest, "new-tab": &d.newTab,
		"ok": &d.x.dlg.ok, "cancel": &d.x.dlg.cancel, "back": &d.x.dlg.back,
		"conversation-name": &d.x.mpdmName, "sharing": &d.x.sharingRow,
		"posting": &d.x.postRow, "save": &d.x.dlg.ok, "add-app": &d.addApp,
		"to-public": &d.x.toPublicRow,
		// managing-this-channel's other rows (details_managing.go) and the
		// delete modal's checkbox and "archive a channel instead"
		"to-private": &d.x.toPrivateRow, "archive": &d.x.archiveRow, "unarchive": &d.x.unarchiveRow,
		"delete": &d.x.deleteRow, "delete-confirm": &d.x.dlg.confirmAll, "archive-instead": &d.x.dlg.back,
	}
	if name == "add-people" {
		if cdGroup(d.kind) == "im" {
			d.addPeople.Click()
		} else {
			d.addPeopleRow.Click()
		}
		d.h.Invalidate()
		return
	}
	if c, ok := cs[name]; ok {
		c.Click()
		d.h.Invalidate()
	}
}

// AddPerson puts u on the add-people modal's picked list, as choosing
// them from the results does.
func (d *ChannelDetails) AddPerson(u core.User) {
	if a := d.x.dlg.add; a != nil && a.addRoom() > 0 {
		a.picked = append(a.picked, u)
		d.h.Invalidate()
	}
}

// Dialog names the open stacked dialog ("" when none).
func (d *ChannelDetails) Dialog() string {
	switch d.x.dlg.kind {
	case cdDlgLeave:
		return "leave"
	case cdDlgLeaveArchive:
		return "leave-archive"
	case cdDlgAIUse:
		return "ai-use"
	case cdDlgGuests:
		return "guests"
	case cdDlgSharing:
		return "sharing"
	case cdDlgPerms:
		return "perms"
	case cdDlgPosting:
		return "posting"
	case cdDlgHide:
		return "hide"
	case cdDlgToPublic:
		return "to-public"
	case cdDlgToPrivate:
		return "to-private"
	case cdDlgArchive:
		return "archive"
	case cdDlgDelete:
		return "delete"
	case cdDlgAddPeople:
		if d.x.dlg.add != nil && d.x.dlg.add.step == 1 {
			return "add-people-history"
		}
		return "add-people"
	}
	return ""
}

// ScrollSettingsTo shows a Settings section ("channel-permissions"…).
func (d *ChannelDetails) ScrollSettingsTo(sec string) {
	inf, _, _ := d.info.get()
	for i, s := range d.settingsSections(inf) {
		if s == sec {
			d.list(cdSettings).ScrollTo(2 * i)
			d.h.Invalidate()
			return
		}
	}
}
