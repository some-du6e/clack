package popups

import (
	"context"
	"errors"
	"image"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
)

// Create User Group: UserGroupEditCreateDialog's create flow, as Slack's
// web client draws it (build a09b625d90aa, 2026-11-22; chunk shared-js_
// modern_components_flexpane_user-groups_user-group-edit-create-dialog_tsx
// YQeV, Ou/6 UserGroupEditCreateForm, Tm9m UserGroupMemberManagement),
// opened by the user group browser's CreateUserGroupButton and its
// banner's "Create a user group" (focusKey "": the name has the caret).
//
//   - DETAILS ("Create User Group", Next, Cancel, "Step 1 of 2"): the
//     Edit dialog's form (Name, Handle, Purpose, Channels, the section
//     switch), its 300 ms validations against the form's workspaceId (the
//     current team; the Workspace select is the unified grid's only).
//     Next is disabled while a field has an error, the name or handle is
//     empty, the purpose is over 140, or the workspace isn't one the
//     viewer may create groups in (getWorkspaceIdsWhereUserCanCreate
//     UserGroups); then the name and handle are disabled too, and without
//     the edit permission either the form says "You do not have
//     permission to create user groups in this workspace.". Next
//     validates the name and handle again and moves on. Cancel and the
//     close button ask first ("Your work won’t be saved - are you sure you
//     want to close?"): Slack compares the form with detailsFormValues,
//     which carry additionalChannels the form never has.
//   - MEMBERS ("Add members", Create Group, Back, "Step 2 of 2"): the
//     members select (large, "Search by name", up to 148px of tokens,
//     the people on the team, frecency first), empty at first. Back goes
//     to DETAILS with the form as it was; closing doesn't ask. Create
//     Group with nobody picked shows "User groups need to have at least
//     one member." and stays disabled until someone is; else
//     createUserGroup (core.CreateUsergroup: usergroups.create, then
//     usergroups.users.update), and the dialog closes either way
//     (shouldCloseOnGo): on success the group's User Group pane opens
//     (showUserGroupDetails) and "User group created!" toasts; on a
//     failure "We’re not sure why, but that didn’t work. Try again?".
//
// The frontline radio (renderFrontlineOnlyRadio) needs the frontline_users
// experiment and the frontline__access feature (canCreateFrontlineOnly
// Group). Hack Club has the experiment but not the feature
// (addon_not_provisioned, 2026-11-22), so like Slack there it isn't drawn.

// OpenUserGroupCreate opens Create User Group's first step.
func OpenUserGroupCreate(h Host) *UserGroupEditModal {
	c := h.Core()
	e := &UserGroupEditModal{h: h, create: true}
	if c != nil {
		e.team = c.C.Creds.Team
	}
	for _, ed := range []*widget.Editor{&e.name, &e.handle} {
		ed.SingleLine = true
	}
	e.chans = newUGSelect(h, false, "")
	e.chans.search = func(ctx context.Context, q string) ([]ugOption, error) {
		hs, err := c.UsergroupChannelCandidates(ctx, q)
		out := make([]ugOption, len(hs))
		for i, hh := range hs {
			out[i] = ugOption{ID: hh.ID, Label: hh.Name, Conv: core.Conv{ID: hh.ID, Name: hh.Name, Kind: hh.Kind}}
		}
		return out, err
	}
	e.members = newUGSelect(h, true, nsUG.T("Search by name"))
	e.members.member, e.members.allowEmpty, e.members.maxH = true, true, 148
	e.members.search = func(ctx context.Context, q string) ([]ugOption, error) {
		us, err := c.UsergroupMemberCandidates(ctx, q)
		out := make([]ugOption, len(us))
		for i, u := range us {
			out[i] = ugMemberOption(u)
		}
		return out, err
	}
	e.members.changed = func(ids []string) { // the member management's and the dialog's onSelectedItemsChange
		e.noUsers = len(ids) == 0
		e.reqEmpty = len(ids) == 0
	}
	e.seenSectionBadge()
	if c != nil {
		if r, ok := c.CachedUsergroupRights(e.team); ok {
			e.rights, e.rightsKnown = r, true
		} else {
			team := e.team
			run(h, func(ctx context.Context) (core.UsergroupRights, error) { return c.UsergroupRights(ctx, team), nil },
				func(r core.UsergroupRights, _ error) { e.rights, e.rightsKnown = r, true })
		}
	}
	e.save = &ModalButton{Label: nsUG.T("Next"), Kind: BtnPrimary,
		Enabled: func() bool { return e.CanSave() && !e.pending },
		Do:      func() bool { e.Save(); return false }} // shouldCloseOnGo: false; MEMBERS closes itself
	e.cancel = &ModalButton{Label: nsUG.T("Cancel"), Kind: BtnOutline, Do: func() bool {
		if e.step == 1 { // Back: setDialogStep(DETAILS)
			e.back()
			return false
		}
		e.bump = true // shouldCloseOnCancel, behind the speed bump
		return false
	}}
	e.keep = &ModalButton{Label: nsUG.T("Keep Editing"), Kind: BtnOutline}
	e.cls = &ModalButton{Label: nsUG.T("Close"), Kind: BtnDanger}
	e.m = NewModal(h, ModalOpts{
		TitleFunc: func() string {
			if e.step == 1 {
				return nsUG.T("Add members")
			}
			return nsUG.T("Create User Group")
		},
		Width:   580,
		Body:    e.layout,
		Buttons: []*ModalButton{e.cancel, e.save},
		FocusStops: func() []event.Tag {
			if e.step == 1 {
				return []event.Tag{&e.members.ed}
			}
			return []event.Tag{&e.name, &e.handle, &e.purpose, &e.chans.ed, &e.sectionClk}
		},
		CloseGuard: func() bool { // getShouldShowSpeedBump: not on MEMBERS
			if e.step == 1 {
				return true
			}
			e.bump = true
			return false
		},
		OnEscape: ugEscape(e.chans, e.members), // MultiSelectInput onEscape: the open list first (Channels, then the members)
		Over:     e.over,
	}).OldDialog()
	e.sync()
	h.ShowOverlay(e)
	return e
}

// seenSectionBadge is the form's componentDidMount: under 4, the
// section switch's "New" tag counts one more showing.
func (e *UserGroupEditModal) seenSectionBadge() {
	c := e.h.Core()
	if c == nil {
		return
	}
	if raw, ok := c.Pref("seen_usergroup_section_new_badge"); ok {
		e.badgeCount = int(prefNumber(raw))
	}
	if e.badgeCount < 4 {
		n := e.badgeCount + 1
		go func() { _ = c.SetPref(context.Background(), "seen_usergroup_section_new_badge", n) }()
	}
}

// sync is getDialogProps for the step: the go button's text, Cancel or
// Back, the footer's step, the member body's border.
func (e *UserGroupEditModal) sync() {
	o := &e.m.o
	if e.step == 1 {
		e.save.Label, e.cancel.Label = nsUG.T("Create Group"), nsUG.T("Back")
		o.FooterNote, o.Border = nsUG.T("Step 2 of 2"), true // p-user_group_member_management__dialog_body
		return
	}
	e.save.Label, e.cancel.Label = nsUG.T("Next"), nsUG.T("Cancel")
	o.FooterNote, o.Border = nsUG.T("Step 1 of 2"), false
}

// permissionError is maybeRenderError's permission case: Create's
// workspace is neither one the viewer may create nor edit groups in.
func (e *UserGroupEditModal) permissionError() string {
	if !e.create || !e.rightsKnown || e.team == "" || e.rights.Create || e.rights.Edit {
		return ""
	}
	return nsUG.T("You do not have permission to create user groups in this workspace.")
}

// createGo is the step's go button: DETAILS' onDetailsStepComplete
// (validate, then MEMBERS), MEMBERS' onCreateDialogSubmit.
func (e *UserGroupEditModal) createGo() {
	c := e.h.Core()
	if c == nil || e.pending || !e.CanSave() {
		return
	}
	if e.step == 1 {
		e.submitCreate()
		return
	}
	e.pending = true
	// getFormValues at the click: what validate checks and what
	// setDetailsFormValues keeps for MEMBERS' create and for Back. Edits
	// made while validate runs don't reach either (the form unmounts).
	snap := ugDetails{name: e.name.Text(), handle: e.handle.Text(), purpose: e.purpose.Text(),
		chans: append([]ugOption(nil), e.chans.picked...), section: e.section}
	team := e.team
	type res struct{ nameErr, handleErr string }
	run(e.h, func(ctx context.Context) (res, error) {
		return res{ugValidateName(ctx, c, snap.name, "", team), ugValidateHandle(ctx, c, snap.handle, "", team)}, nil
	}, func(r res, _ error) {
		e.pending = false // validate's finally
		e.nameErr, e.handleErr = r.nameErr, r.handleErr
		if r.nameErr != "" || r.handleErr != "" {
			return
		}
		e.details = snap // setDetailsFormValues, setDialogStep(MEMBERS)
		e.step = 1
		e.nameAt, e.handleAt = time.Time{}, time.Time{} // the unmounted form's pending checks
		e.nameGen++
		e.handleGen++
		e.sync()
		e.members.Focus() // focusOnMount
		e.h.Invalidate()
	})
}

// ugDetails is the dialog's detailsFormValues: the DETAILS form as Next
// took it.
type ugDetails struct {
	name, handle, purpose string
	chans                 []ugOption
	section               bool
}

// back is MEMBERS' Back: DETAILS again, the form remounted with the
// values it had (the caret in the name, the "New" tag counted again).
func (e *UserGroupEditModal) back() {
	e.step = 0
	e.focused = false
	d := e.details // the form's constructor: its state from detailsFormValues
	e.name.SetText(d.name)
	e.handle.SetText(d.handle)
	e.purpose.SetText(d.purpose)
	e.chans.SetPicked(d.chans)
	e.section = d.section
	e.nameQ, e.handleQ = d.name, d.handle
	e.seenSectionBadge()
	e.sync()
}

// submitCreate is onCreateDialogSubmit: at least one member
// (validateUpdatedGroupMemberCount), then createUserGroup; the dialog
// closes once it settles.
func (e *UserGroupEditModal) submitCreate() {
	c := e.h.Core()
	ids := e.members.IDs()
	if len(ids) == 0 {
		e.noUsers, e.reqEmpty = true, true
		return
	}
	e.pending = true
	d := e.details
	chans := make([]string, len(d.chans))
	for i, o := range d.chans {
		chans[i] = o.ID
	}
	in := core.UsergroupCreate{TeamID: e.team, Name: d.name, Handle: d.handle, Description: d.purpose,
		Channels: chans, EnableSection: d.section, Users: ids}
	run(e.h, func(ctx context.Context) (core.Usergroup, error) { return c.CreateUsergroup(ctx, in) }, func(g core.Usergroup, err error) {
		e.pending = false
		if err != nil { // onSubmitFailure: setApiError(e.name ?? e)
			e.submitErr = "error"
			var se *slack.Error
			if errors.As(err, &se) {
				e.submitErr = se.Code
			}
			e.h.Toast(nsUG.T("We’re not sure why, but that didn’t work. Try again?"))
			e.m.Close()
			return
		}
		if o, ok := e.h.(interface{ OpenUserGroup(id string) }); ok { // showUserGroupDetails
			o.OpenUserGroup(g.ID)
		}
		e.h.Toast(nsUG.T("User group created!"))
		e.submitErr = ""
		e.m.Close()
	})
}

// membersLayout is renderMemberSelector: UserGroupMemberManagement with
// no group yet, so the select at once (no spinner), the no-members alert
// above it.
func (e *UserGroupEditModal) membersLayout(gtx layout.Context) layout.Dimensions {
	th := e.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	// the body has no side padding; the selector's margin is 4px 24px 24px
	x, iw := dp(gtx, 28)-dp(gtx, 24), w+2*(dp(gtx, 28)-dp(gtx, 24))
	x = -x
	y := dp(gtx, 4)
	if e.noUsers {
		dd := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			return ugAlert(gtx, th, cl, "error", nsUG.T("User groups need to have at least one member."), iw)
		})
		y += dd.Size.Y + dp(gtx, 8)
	}
	sd := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(iw, 1<<20)}
		return e.members.layout(gtx, th, cl)
	})
	y += sd.Size.Y + dp(gtx, 24)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// Create's state (tests).

// Creating reports a Create User Group dialog.
func (e *UserGroupEditModal) Creating() bool { return e.create }

// Step is the create flow's step: 0 DETAILS, 1 MEMBERS.
func (e *UserGroupEditModal) Step() int { return e.step }

// Members is the MEMBERS step's select.
func (e *UserGroupEditModal) Members() *ugSelect { return e.members }

// NoMembersError reports the "at least one member" alert.
func (e *UserGroupEditModal) NoMembersError() bool { return e.noUsers }

// Back presses MEMBERS' Back.
func (e *UserGroupEditModal) Back() {
	if e.create && e.step == 1 {
		e.back()
	}
}

// Title is the dialog's title for its step.
func (e *UserGroupEditModal) Title() string { return e.m.Title() }

// FooterNote is the footer's step text.
func (e *UserGroupEditModal) FooterNote() string { return e.m.o.FooterNote }

// ButtonLabels are the footer's buttons, left to right.
func (e *UserGroupEditModal) ButtonLabels() string {
	var out []string
	for _, b := range e.m.o.Buttons {
		out = append(out, b.Label)
	}
	return strings.Join(out, "|")
}

// PermissionError is maybeRenderError's permission message ("" none).
func (e *UserGroupEditModal) PermissionError() string { return e.permissionError() }

// FieldsDisabled reports the name and handle inputs disabled.
func (e *UserGroupEditModal) FieldsDisabled() bool { return e.fieldsOff() }
