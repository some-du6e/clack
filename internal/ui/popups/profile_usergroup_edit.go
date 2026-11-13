package popups

import (
	"context"
	"errors"
	"image"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The classic User Group pane's editor parts (client-channel-user-group-
// view oXTE, build 32bb3cc8205b), drawn for a viewer who may edit the
// group (core.UsergroupRights; the dialogs are in usergroup_edit.go):
//
//   - userGroupIsEditable: the group is active and the viewer has the
//     workspace's edit, delete or create right. Only then the action bar
//     (UserGroupsDetailsActionBar, a .p-user_group_details__section under
//     the header) and the rows' ⋮ buttons (tooltip "More actions").
//   - The bar (.p-user_group_details__actions, flex, space-between):
//     for an editor "Edit Members" and "Edit Channels" (outline, sharing
//     the width, 8px apart), or "Edit Details" alone for an
//     auto-generated group; for a creator or deleter who can't edit,
//     "Copy Group ID"; then the 36px ⋮ "More actions" (8px margin, a
//     300 ms tooltip) when the viewer can create or edit, or delete an
//     active group. Edit Channels opens Edit User Group with the channel
//     picker focused (focusKey CHANNELS), Edit Details with the name.
//   - The ⋮ menu (UserGroupContextMenu, isDetailsView): "Edit group
//     details..." (not for an auto-generated group) and "Copy group ID"
//     while the group is active and the viewer can edit or create in any
//     workspace; "Enable group" for a creator on a deactivated group;
//     "Deactivate group" (danger, confirmed) for a deleter on an active
//     one.
//   - A member row's ⋮: "View profile", and "Remove from group…" (danger,
//     confirmed: removeUserGroupMember, toasts "Member removed from
//     group." or, for the last member, "User groups need to have at least
//     one member.") unless the group is auto-generated. A channel row's ⋮:
//     "Open channel" and "Remove from group…" (removeUserGroupChannel,
//     "Channel removed from group.").
//   - Empty, for an editor: "You can add a few people with the button
//     below." with "Add Members" (Edit members), and "Add Default
//     Channels" (Edit User Group, the name focused).
//   - A deactivated group shows a creator "Enable group" (full width,
//     outline) under the header.
//
// Nothing changes on screen before the server answers: Slack's thunks
// send the call and the store follows the answer (bulkEditUserGroupMembers,
// updateDescription) or subteam_updated, which makes the pane refetch the
// members (UserGroupView.watch). A failed call leaves everything as it was.
//
// The org-level pane edits nothing; for a viewer with the org dashboard's
// Groups page (canUserAccessGroupsInOrgDash) ManageUserGroupLink reads "To
// manage this group, go to the Groups Dashboard ↗." and the empty states
// point there too.

// editable is userGroupIsEditable.
func (v *UserGroupView) editable() bool {
	return !v.org && v.rightsOK && !v.g.Disabled && v.rights.Editable()
}

// hit records widget key's rect r (body coordinates) in pane
// coordinates.
func (v *UserGroupView) hit(key string, r image.Rectangle) {
	if v.hits == nil {
		v.hits = map[string]image.Rectangle{}
	}
	v.hits[key] = r
}

// Hit is where widget key was drawn last frame, in pane coordinates
// ("edit-members", "options", "more:<user>", "chmore:<channel>", …).
func (v *UserGroupView) Hit(key string) image.Rectangle { return v.hits[key] }

func (v *UserGroupView) rowTip(key string) *TipState {
	if v.rowTips == nil {
		v.rowTips = map[string]*TipState{}
	}
	t := v.rowTips[key]
	if t == nil {
		t = &TipState{}
		v.rowTips[key] = t
	}
	return t
}

// editorClicks runs the bar's and the empty states' buttons.
func (v *UserGroupView) editorClicks(gtx layout.Context) {
	if v.editMembers.Clicked(gtx) || v.addMembers.Clicked(gtx) {
		OpenUserGroupMembers(v.h, v.g.Usergroup)
	}
	if v.editChannels.Clicked(gtx) {
		OpenUserGroupEdit(v.h, v.g, "channels")
	}
	if v.editDetails.Clicked(gtx) || v.addChannels.Clicked(gtx) {
		OpenUserGroupEdit(v.h, v.g, "")
	}
	if v.copyID.Clicked(gtx) {
		copyText(gtx, v.g.ID)
	}
	if v.enableBtn.Clicked(gtx) { // handleEnableUserGroup: with the team
		v.enable(v.g.TeamID)
	}
	if v.dashLink.Clicked(gtx) {
		_ = openurl.Open(v.dashURL())
	}
}

// actionBar draws UserGroupsDetailsActionBar's section at y and returns
// its height (margins included).
func (v *UserGroupView) actionBar(gtx layout.Context, th *theme.Theme, cl *Colors, w, y int) int {
	pad := dp(gtx, 16)
	top := y + dp(gtx, 12)
	bh := dp(gtx, 36)
	gap := dp(gtx, 8)
	r := v.rights
	opts := r.Create || r.Edit || (r.Delete && !v.g.Disabled)
	right := w - pad
	if opts {
		right -= bh + gap
	}
	inner := maxInt(1, right-pad)
	px := func(n int) float32 { return float32(n) / gtx.Metric.PxPerDp }
	off := image.Pt(0, dp(gtx, 49)-v.list.Position.Offset)
	btn := func(c *widget.Clickable, x, bw int, label string) {
		v.hit(map[*widget.Clickable]string{&v.editDetails: "edit-details", &v.editMembers: "edit-members",
			&v.editChannels: "edit-channels", &v.copyID: "copy-id"}[c], image.Rect(x, top, x+bw, top+bh).Add(off))
		at(gtx, image.Pt(x, top), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(bw, bh), Max: image.Pt(bw, bh)}
			return Button(gtx, th, c, BtnOutline, BtnMedium, label, "", px(bw))
		})
	}
	switch {
	case r.Edit && v.g.AutoGenerated():
		btn(&v.editDetails, pad, inner, nsUG.T("Edit Details"))
	case r.Edit:
		half := (inner - gap) / 2
		btn(&v.editMembers, pad, half, nsUG.T("Edit Members"))
		btn(&v.editChannels, pad+half+gap, inner-half-gap, nsUG.T("Edit Channels"))
	case r.Create || r.Delete:
		btn(&v.copyID, pad, inner, nsUG.T("Copy Group ID"))
	}
	if opts {
		ox := w - pad - bh
		v.hit("options", image.Rect(ox, top, ox+bh, top+bh).Add(off))
		if v.options.Clicked(gtx) {
			box := image.Rect(ox, top, ox+bh, top+bh).Add(image.Pt(0, dp(gtx, 49)-v.list.Position.Offset))
			v.h.ShowOverlay(&pfPaneMenu{h: v.h, plain: true, paneW: v.paneW, box: box, items: v.optionItems()})
		}
		at(gtx, image.Pt(ox, top), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(bh, bh)}
			d := Button(gtx, th, &v.options, BtnOutline, BtnMedium, "", "kebab", 0)
			v.optionsTip.Delay = 300e6 // delay 300
			Tooltip(gtx, th, &v.optionsTip, v.options.Hovered(), d.Size, Above, nsUG.T("More actions"), "")
			return d
		})
	}
	return dp(gtx, 12) + bh + dp(gtx, 12)
}

// optionItems is UserGroupContextMenu's getMenuItems (isDetailsView).
func (v *UserGroupView) optionItems() []MenuItem {
	r := v.rights
	active := !v.g.Disabled
	var items []MenuItem
	if (r.EditAny || r.CreateAny) && active {
		if !v.g.AutoGenerated() {
			items = append(items, MenuItem{Key: "menu-edit-details", Label: nsUG.T("Edit group details..."),
				Do: func() { OpenUserGroupEdit(v.h, v.g, "") }})
		}
		id := v.g.ID
		items = append(items, MenuItem{Key: "copy-id", Label: nsUG.T("Copy group ID"), DoCtx: func(gtx layout.Context) { copyText(gtx, id) }})
	}
	if r.CreateAny && !active {
		items = append(items, MenuItem{Key: "menu-enable", Label: nsUG.T("Enable group"), Do: func() { v.enable("") }})
	}
	if r.DeleteAny && active {
		items = append(items, MenuItem{Key: "menu-disable", Label: nsUG.T("Deactivate group"), Danger: true, Do: v.confirmDisable})
	}
	return items
}

// confirmDisable is onDisableUserGroup: the confirmation, then
// disableAndUpdateUserGroup with the group's team.
func (v *UserGroupView) confirmDisable() {
	c := v.h.Core()
	g := v.g.Usergroup
	ugConfirm(v.h, "disable_group", ugName(g), "", func() {
		if c == nil {
			return
		}
		run(v.h, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.DisableUsergroup(ctx, g.ID, g.TeamID)
		},
			func(struct{}, error) {})
	})
}

// enable is enableAndUpdateUserGroup (the menu sends no team, the
// button the group's).
func (v *UserGroupView) enable(team string) {
	c := v.h.Core()
	if c == nil {
		return
	}
	id := v.g.ID
	run(v.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, c.EnableUsergroup(ctx, id, team) },
		func(struct{}, error) {})
}

// ugName is getUserGroupName.
func ugName(g core.Usergroup) string { return g.Name }

// memberItems is a member row's ⋮ (renderListItemOptionsMenu, or the
// org pane's MemberRow).
func (v *UserGroupView) memberItems(u core.User) []MenuItem {
	id := u.ID
	items := []MenuItem{{Label: nsUG.T("View profile"), Do: func() { pfOpenFull(v.h, id) }}}
	if !v.org && v.canRemove(true) {
		items = append(items, MenuItem{Label: nsUG.T("Remove from group…"), Danger: true, Do: func() { v.confirmRemoveMember(u) }})
	}
	return items
}

// channelItems is a channel row's ⋮.
func (v *UserGroupView) channelItems(cv core.Conv) []MenuItem {
	id := cv.ID
	items := []MenuItem{{Label: nsUG.T("Open channel"), Do: func() { v.h.Navigate("c/" + id) }}}
	if v.canRemove(false) {
		items = append(items, MenuItem{Label: nsUG.T("Remove from group…"), Danger: true, Do: func() { v.confirmRemoveChannel(cv) }})
	}
	return items
}

// canRemove is canRemoveItem: members of an auto-generated group can't
// be removed; otherwise an editor or creator may.
func (v *UserGroupView) canRemove(member bool) bool {
	if member && v.g.AutoGenerated() {
		return false
	}
	return v.rights.Edit || v.rights.Create
}

// confirmRemoveMember is onRemoveMember.
func (v *UserGroupView) confirmRemoveMember(u core.User) {
	c := v.h.Core()
	g, h := v.g.Usergroup, v.h
	ugConfirm(h, "remove_member", ugName(g), pfMemberName(u), func() {
		if c == nil {
			return
		}
		run(h, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.RemoveUsergroupMember(ctx, g.ID, u.ID)
		},
			func(_ struct{}, err error) {
				switch {
				case err == nil:
					h.Toast(nsUG.T("Member removed from group."))
				case slackCode(err) == "no_users_provided":
					h.Toast(nsUG.T("User groups need to have at least one member."))
				}
			})
	})
}

// confirmRemoveChannel is onRemoveChannel.
func (v *UserGroupView) confirmRemoveChannel(cv core.Conv) {
	c := v.h.Core()
	g, h := v.g, v.h
	ugConfirm(h, "remove_channel", ugName(g.Usergroup), ugChannelLabel(cv), func() {
		if c == nil {
			return
		}
		run(h, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.RemoveUsergroupChannel(ctx, g, cv.ID)
		},
			func(_ struct{}, err error) {
				if err == nil {
					h.Toast(nsUG.T("Channel removed from group."))
				}
			})
	})
}

// ugChannelLabel is getChannelDisplayName for the confirmation: the
// channel's name with its # (a private channel's without).
func ugChannelLabel(cv core.Conv) string {
	if cv.Kind == "private" {
		return cv.Name
	}
	return "#" + cv.Name
}

// enableSection is renderEnableGroupButton's section.
func (v *UserGroupView) enableSection(gtx layout.Context, th *theme.Theme, cl *Colors, w, y int) int {
	pad := dp(gtx, 16)
	bh := dp(gtx, 36)
	bw := maxInt(1, w-2*pad)
	v.hit("enable", image.Rect(pad, y+dp(gtx, 12), pad+bw, y+dp(gtx, 12)+bh).Add(image.Pt(0, dp(gtx, 49)-v.list.Position.Offset)))
	at(gtx, image.Pt(pad, y+dp(gtx, 12)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(bw, bh), Max: image.Pt(bw, bh)}
		return Button(gtx, th, &v.enableBtn, BtnOutline, BtnMedium, nsUG.T("Enable group"), "", float32(bw)/gtx.Metric.PxPerDp)
	})
	return dp(gtx, 12) + bh + dp(gtx, 12)
}

// emptyAction is EmptyState with its primary action: the header, the
// description and a primary button 16px under it.
func (v *UserGroupView) emptyAction(gtx layout.Context, th *theme.Theme, cl *Colors, w, y int, head string, sub []ugWord, action string, c *widget.Clickable) layout.Dimensions {
	d := v.empty(gtx, th, cl, w, y, head, sub)
	by := d.Size.Y
	bw := buttonW(gtx, th, action)
	key := "add-members"
	if c == &v.addChannels {
		key = "add-channels"
	}
	v.hit(key, image.Rect((w-bw)/2, by, (w+bw)/2, by+dp(gtx, 36)).Add(image.Pt(0, dp(gtx, 49)-v.list.Position.Offset)))
	at(gtx, image.Pt((w-bw)/2, by), func(gtx layout.Context) layout.Dimensions {
		return Button(gtx, th, c, BtnPrimary, BtnMedium, action, "", 0)
	})
	return layout.Dimensions{Size: image.Pt(w, by+dp(gtx, 36)+dp(gtx, 16))}
}

// buttonW is a medium c-button's width for label (padding 0 12, 15/700,
// min-width 80).
func buttonW(gtx layout.Context, th *theme.Theme, label string) int {
	return maxInt(dp(gtx, 80), textW(gtx, th, 15, theme.Bold, label)+2*dp(gtx, 12))
}

// dashURL is the org dashboard's page for the group
// (`${getManageUrl}/people/groups/{id}/details`; the desktop client's
// origin is app.slack.com, a gantry URL, so the manage URL names the org).
func (v *UserGroupView) dashURL() string {
	ent := ""
	if c := v.h.Core(); c != nil {
		ent = c.C.Creds.EnterpriseID
	}
	base := "https://app.slack.com/manage"
	if ent != "" {
		base += "/" + ent
	}
	return base + "/people/groups/" + v.g.ID + "/details"
}

// dashWords is "To manage this group, go to the <Link>Groups
// Dashboard</Link>" with its new-window link and the period after it.
func (v *UserGroupView) dashWords() []ugWord {
	var out []ugWord
	for _, p := range nsUG.RT("To manage this group, go to the <Link>Groups Dashboard</Link>") {
		for _, f := range splitWords(p.Text) {
			if p.Tag != "" {
				out = append(out, ugWord{s: f, link: &v.dashLink})
			} else {
				out = append(out, ugWord{s: f})
			}
		}
	}
	if n := len(out); n > 0 {
		out[n-1].s += " ↗"
		out = append(out, ugWord{s: "."})
	}
	return out
}

// dashAlert is ManageUserGroupLink's alert (level default, no icon) for
// an org dashboard viewer; it returns its height.
func (v *UserGroupView) dashAlert(gtx layout.Context, th *theme.Theme, cl *Colors, x, y, w int) int {
	words := v.dashWords()
	pad, padX := dp(gtx, 12), dp(gtx, 16) // c-alert--boxed: padding 12px 16px
	lh := dp(gtx, 22)
	space := textW(gtx, th, 15, theme.Regular, " ")
	lines, lw := 1, 0
	inner := maxInt(1, w-2*padX)
	type spot struct{ x, y int }
	spots := make([]spot, len(words))
	for i, wd := range words {
		ww := textW(gtx, th, 15, theme.Regular, wd.s)
		if lw > 0 && lw+space+ww > inner {
			lines++
			lw = 0
		} else if lw > 0 {
			lw += space
		}
		spots[i] = spot{lw, (lines - 1) * lh}
		lw += ww
	}
	h := lines*lh + 2*pad
	box := image.Rect(x, y, x+w, y+h)
	if th.Dark { // c-alert--level_default: base-ter, light-dark(transparent, otl-sec) border
		rrect(gtx, box, dp(gtx, 8), cl.InputBorder)
		rrect(gtx, box.Inset(border1(gtx)), dp(gtx, 8)-border1(gtx), epDisabledBg(th, cl))
	} else {
		rrect(gtx, box, dp(gtx, 8), epDisabledBg(th, cl))
	}
	for i, wd := range words {
		wd, sp := wd, spots[i]
		ww := textW(gtx, th, 15, theme.Regular, wd.s)
		at(gtx, image.Pt(x+padX+sp.x, y+pad+sp.y), func(gtx layout.Context) layout.Dimensions {
			draw := func(gtx layout.Context) layout.Dimensions {
				fg := cl.Fg
				if wd.link != nil {
					fg = cl.Link
				}
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Regular, fg, wd.s) })
			}
			if wd.link == nil {
				return draw(gtx)
			}
			return clickBox(gtx, wd.link, nil, func(gtx layout.Context) layout.Dimensions {
				draw(gtx)
				return layout.Dimensions{Size: image.Pt(ww, lh)}
			})
		})
	}
	return h
}

// splitWords splits s at spaces, keeping a leading or trailing comma or
// period with its word.
func splitWords(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// slackCode is a Slack API error's code ("" for anything else).
func slackCode(err error) string {
	var se *slack.Error
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}
