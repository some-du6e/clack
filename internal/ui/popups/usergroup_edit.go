package popups

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"image"
	"image/color"
	"image/png"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The User Group pane's editor dialogs, as Slack's web client draws them
// (build 32bb3cc8205b, 2026-11-13; chunks client-channel-user-group-view
// oXTE and shared-js_modern_components_flexpane_user-groups_user-group-
// edit-create-dialog_tsx aAKr, Ou/6, sk61, 9CgL). All three are
// DeprecatedDialog (boot-shared zMub) c-dialogs.
//
//   - Edit User Group (UserGroupEditCreateDialog in its EDIT step, 580px
//     wide): Cancel and Save, the footer's "User Group ID: {id}" with a
//     copy button ("Copy user group id", "Copied!" for 2 s). The form
//     (UserGroupEditCreateForm): an error alert (a failed save: "We’re not
//     sure why, but that didn’t work. Try again?", or for
//     too_many_linked_channels "User groups can’t have more than 100
//     default channels."), the auto-generated group's info alert, Name
//     ("e.g. Accounts Team"), Handle (the mentions icon, "e.g.
//     accounts-team", its hint; spaces are dropped as you type), Purpose
//     (optional, "What is this group about?", 140 characters), Channels
//     (the default channel picker, "Group members are added to these
//     channels automatically.") and the "Add group channels as a section
//     in Home" switch with its illustration (a "New" tag while
//     seen_usergroup_section_new_badge is under 4; opening the dialog
//     counts one). Name and handle are disabled for an auto-generated
//     group. Each edit re-validates its field 300 ms later
//     (validateUserGroupName / validateUserGroupHandle, below); Save is
//     disabled while a field has an error, the name or handle is empty,
//     or the purpose is over 140. Save validates both again, then sends
//     updateUserGroup (core.UpdateUsergroup) and toasts "Group has been
//     updated.". Closing it any other way asks first ("Your work won’t
//     be saved - are you sure you want to close?", Keep Editing / Close):
//     Slack compares the form with detailsFormValues, which carries
//     additionalChannels the form never has, so it always asks.
//   - Edit members (UserGroupMemberManagementDialog): a spinner until
//     usergroups.users.list answers, then the members select (large,
//     "Search by name", up to 148px of tokens), "User groups need to have
//     at least one member." once it is empty; Save (disabled while
//     empty, Enter doesn't press it) sends bulkEditUserGroupMembers
//     (core.SetUsergroupUsers) and toasts "Group has been updated.". A
//     failed save keeps the dialog open.
//   - The confirmations (UserGroupConfirmationDialog): "Remove from
//     group?" / "Remove default channel?" / "Deactivate {groupName}?" with
//     a danger "Remove" or "Deactivate".

var nsUG = i18n.NS("user_groups")

//go:embed assets/user-group-section-group-3de1100.png
var ugSectionPNG []byte

var (
	ugSectionOnce sync.Once
	ugSectionImg  paint.ImageOp
	ugSectionOK   bool
)

// ugSectionImage is the switch's illustration (user-group-section-group-
// 3de1100.png, 88×125, from Slack's CDN).
func ugSectionImage() (paint.ImageOp, bool) {
	ugSectionOnce.Do(func() {
		img, err := png.Decode(bytes.NewReader(ugSectionPNG))
		if err != nil {
			return
		}
		ugSectionImg, ugSectionOK = paint.NewImageOp(img), true
	})
	return ugSectionImg, ugSectionOK
}

// ---- validation (boot-shared HrU4, MxSC) ----

var (
	ugHandleSpecials = regexp.MustCompile(`[ ,;'` + "`" + `~!@#$%^&*+=\\:"<>?|]`)
	ugHandlePunct    = regexp.MustCompile(`^[-|_]+$`)
	ugHandleUpper    = regexp.MustCompile(`[A-Z]`)
)

// ugValidateName is validateUserGroupName: "" when name may be used.
func ugValidateName(ctx context.Context, c *core.App, name, prev, team string) string {
	if strings.TrimSpace(name) == "" {
		return nsUG.T("Please enter a name for this group.")
	}
	d := strings.ToLower(strings.TrimSpace(name))
	if prev != "" && d == strings.ToLower(strings.TrimSpace(prev)) {
		return ""
	}
	if d == strings.ToLower(nsUG.T("Team Admins")) || d == strings.ToLower(nsUG.T("Team Owners")) {
		return nsUG.T(`The name "{name}" is reserved and cannot be used.`, "name", name)
	}
	if GraphemeLen(name) > 80 {
		return nsUG.T("Names can’t be more than {maxLength} characters.", "maxLength", 80)
	}
	code, err := c.CheckUsergroupName(ctx, name, team)
	switch {
	case err == nil:
		return ""
	case code == "name_already_exists":
		return nsUG.T("This name is already in use by another group.")
	}
	return nsUG.T("Sorry, something went wrong. Please try a different name.")
}

// ugValidateHandle is validateUserGroupHandle: "" when handle may be
// used.
func ugValidateHandle(ctx context.Context, c *core.App, handle, prev, team string) string {
	if strings.TrimSpace(handle) == "" {
		return nsUG.T("Please enter a handle for this group.")
	}
	if handle == prev {
		return ""
	}
	if l := strings.ToLower(handle); l == strings.ToLower(nsUG.T("admins")) || l == strings.ToLower(nsUG.T("owners")) {
		return nsUG.T("The group name @{handle} is reserved and cannot be used.", "handle", handle)
	}
	switch {
	case ugHandleSpecials.MatchString(strings.TrimSpace(handle)), ugHandlePunct.MatchString(handle):
		return nsUG.T("Handles can only contain letters or numbers.")
	case ugHandleUpper.MatchString(handle):
		return nsUG.T("Handles can only be in lowercase.")
	case GraphemeLen(handle) > 80:
		return nsUG.T("Handles can’t be more than {maxLength} characters.", "maxLength", 80)
	}
	code, err := c.CheckUsergroupHandle(ctx, handle, team)
	switch {
	case err == nil:
		return ""
	case code == "handle_already_exists":
		return nsUG.T("This handle is already in use by a member, channel, or another group.")
	case code == "bad_handle":
		return nsUG.T("Handles can only contain letters or numbers.")
	}
	return nsUG.T("Sorry, something went wrong. Please try a different handle.")
}

// ---- the confirmations (UserGroupConfirmationDialog) ----

// ugConfirm opens the confirmation for action ("remove_member",
// "remove_channel", "disable_group") about label in groupName.
func ugConfirm(h Host, action, groupName, label string, onSubmit func()) *Modal {
	th := h.Theme()
	var title, goText string
	var body []i18n.Part
	switch action {
	case "disable_group":
		title = nsUG.T("Deactivate {groupName}?", "groupName", groupName)
		goText = nsUG.T("Deactivate")
		body = nsUG.RT("If you deactivate this user group, you won’t be able to use <strong>{groupName}</strong> or view a list of its members. You can reactivate this group at any time.", "groupName", groupName)
	case "remove_member":
		title, goText = nsUG.T("Remove from group?"), nsUG.T("Remove")
		body = nsUG.RT("Are you sure you want to remove <strong>{label}</strong> from the <strong>{groupName}</strong> user group?", "label", label, "groupName", groupName)
	case "remove_channel":
		title, goText = nsUG.T("Remove default channel?"), nsUG.T("Remove")
		body = nsUG.RT("Are you sure you want to remove <strong>{label}</strong> as a default channel for the <strong>{groupName}</strong> user group?", "label", label, "groupName", groupName)
	}
	ok := &ModalButton{Label: goText, Kind: BtnDanger, Do: func() bool {
		onSubmit()
		return true
	}}
	m := NewModal(h, ModalOpts{
		Title:        title,
		EnterSubmits: true, // goOnEnterPressed
		Body: func(gtx layout.Context) layout.Dimensions {
			return richPara(gtx, th, colorsOf(th).Fg, body)
		},
		Buttons: []*ModalButton{{Label: nsDialog.T("Cancel"), Kind: BtnOutline}, ok},
	}).OldDialog()
	h.ShowOverlay(m)
	return m
}

// ---- Edit members (UserGroupMemberManagementDialog) ----

// UserGroupMembersModal is Edit members.
type UserGroupMembersModal struct {
	h       Host
	g       core.Usergroup
	m       *Modal
	sel     *ugSelect
	loaded  bool // haveUpsertedExistingMembers
	noUsers bool // displayNoMemberError
	off     bool // disableGoButton
	saving  bool
	save    *ModalButton
}

// OpenUserGroupMembers opens Edit members for g.
func OpenUserGroupMembers(h Host, g core.Usergroup) *UserGroupMembersModal {
	d := &UserGroupMembersModal{h: h, g: g}
	d.sel = newUGSelect(h, true, nsUG.T("Search by name"))
	d.sel.member, d.sel.allowEmpty, d.sel.maxH = true, true, 148
	c := h.Core()
	d.sel.search = func(ctx context.Context, q string) ([]ugOption, error) {
		us, err := c.UsergroupMemberCandidates(ctx, q)
		out := make([]ugOption, len(us))
		for i, u := range us {
			out[i] = ugMemberOption(u)
		}
		return out, err
	}
	d.sel.changed = func(ids []string) { // UserGroupMemberManagement's and the dialog's onSelectedItemsChange
		d.noUsers = len(ids) == 0
		d.off = len(ids) == 0
	}
	if c != nil {
		if us, ok := c.CachedUsergroupUsers(g.ID, false); ok { // userGroupMemberIds
			d.sel.SetPicked(ugMemberOptions(us))
		}
	}
	d.save = &ModalButton{Label: nsUG.T("Save"), Kind: BtnPrimary,
		Enabled: func() bool { return !d.off && !d.saving },
		Do:      func() bool { d.Save(); return false }} // shouldCloseOnGo: false
	d.m = NewModal(h, ModalOpts{
		Title:      nsUG.T("Edit members"),
		Body:       d.layout,
		Buttons:    []*ModalButton{{Label: nsDialog.T("Cancel"), Kind: BtnOutline}, d.save},
		Border:     true, // p-user_group_member_management__dialog_body's bottom border
		FocusStops: func() []event.Tag { return []event.Tag{&d.sel.ed} },
	}).OldDialog()
	d.fetch()
	h.ShowOverlay(d)
	return d
}

func ugMemberOptions(us []core.User) []ugOption {
	out := make([]ugOption, len(us))
	for i, u := range us {
		out[i] = ugMemberOption(u)
	}
	return out
}

// fetch is componentDidMount's fetchAndUpsertUserGroupMembers: the
// answer's members replace the picked ones when there are any. A failed
// fetch leaves the spinner (Slack's .then has no catch).
func (d *UserGroupMembersModal) fetch() {
	c := d.h.Core()
	if c == nil {
		return
	}
	g := d.g
	run(d.h, func(ctx context.Context) ([]core.User, error) {
		us, _, err := c.UsergroupUsers(ctx, g, "")
		return us, err
	}, func(us []core.User, err error) {
		if err != nil {
			return
		}
		d.loaded = true
		if len(us) > 0 {
			stored, _ := c.CachedUsergroupUsers(g.ID, false) // the answer's order, not the pane's
			d.sel.SetPicked(ugMemberOptions(stored))
			d.sel.notify()
		}
		d.sel.Focus() // focusOnMount
		d.h.Invalidate()
	})
}

// Layout and Dismissed make it an Overlay.
func (d *UserGroupMembersModal) Layout(gtx layout.Context) layout.Dimensions { return d.m.Layout(gtx) }
func (d *UserGroupMembersModal) Dismissed() bool                             { return d.m.Dismissed() }

// Dismiss closes it.
func (d *UserGroupMembersModal) Dismiss() { d.m.Dismiss() }

// Select is the members select.
func (d *UserGroupMembersModal) Select() *ugSelect { return d.sel }

// Loaded reports the members fetch finished.
func (d *UserGroupMembersModal) Loaded() bool { return d.loaded }

// NoMembersError reports the "at least one member" alert is up.
func (d *UserGroupMembersModal) NoMembersError() bool { return d.noUsers }

// CanSave is the Save button's state.
func (d *UserGroupMembersModal) CanSave() bool { return d.save.enabled() }

// Save is onSave: at least one member, then usergroups.users.update.
func (d *UserGroupMembersModal) Save() {
	if d.saving || !d.loaded && d.g.ID != "" {
		return
	}
	ids := d.sel.IDs()
	if len(ids) == 0 { // validateUpdatedGroupMemberCount
		d.noUsers = true
		return
	}
	c := d.h.Core()
	if c == nil {
		return
	}
	d.saving = true
	g := d.g
	run(d.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, c.SetUsergroupUsers(ctx, g, ids) },
		func(_ struct{}, err error) {
			d.saving = false
			if err != nil { // onUpsertFailure only logs
				return
			}
			d.h.Toast(nsUG.T("Group has been updated."))
			d.m.Close()
		})
}

func (d *UserGroupMembersModal) layout(gtx layout.Context) layout.Dimensions {
	th := d.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	// the body has no side padding; the selector's margin is 4px 24px 24px
	x, iw := dp(gtx, 28)-dp(gtx, 24), w+2*(dp(gtx, 28)-dp(gtx, 24))
	x = -x
	y := dp(gtx, 4)
	if !d.loaded {
		// p-user_group_member_selector_loading_spinner_container: 148px,
		// a jumbo spinner 16px down
		sp := dp(gtx, 32)
		at(gtx, image.Pt((w-sp)/2, y+dp(gtx, 16)), func(gtx layout.Context) layout.Dimensions {
			epSpinner(gtx, sp, cl.Fg2)
			return layout.Dimensions{Size: image.Pt(sp, sp)}
		})
		return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 148)+dp(gtx, 24))}
	}
	if d.noUsers {
		dd := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			return ugAlert(gtx, th, cl, "error", nsUG.T("User groups need to have at least one member."), iw)
		})
		y += dd.Size.Y + dp(gtx, 8)
	}
	sd := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(iw, 1<<20)}
		return d.sel.layout(gtx, th, cl)
	})
	y += sd.Size.Y + dp(gtx, 24)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// ugAlert is an Alert (VvO8) of type inline: no icon, 13/18
// content-pry text, padding 8px 16px, radius 8, on the level's fill
// ("error": surf-imp, "info": surf-hgl-1).
func ugAlert(gtx layout.Context, th *theme.Theme, cl *Colors, level, s string, w int) layout.Dimensions {
	bg := ugSurfHgl1(th)
	if level == "error" {
		bg = theme.WithAlpha(ccErrColor(th, cl), 0x1a)
	}
	px, py := dp(gtx, 16), dp(gtx, 8)
	call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-2*px), 1<<20)}
		return para(gtx, th, 13, 18, theme.Regular, cl.Fg, s, 0)
	})
	h := d.Size.Y + 2*py
	rrect(gtx, image.Rect(0, 0, w, h), dp(gtx, 8), bg)
	macroAt(gtx, call, image.Pt(px, py))
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// ---- Edit User Group (UserGroupEditCreateDialog, EDIT) ----

// UserGroupEditModal is Edit User Group.
type UserGroupEditModal struct {
	h     Host
	g     core.UsergroupProfile
	m     *Modal
	focus string // focusKey: "channels" or the name

	name, handle, purpose widget.Editor
	chans                 *ugSelect
	section               bool
	sectionClk            widget.Clickable
	nameErr, handleErr    string
	submitErr             string
	pending               bool // formSubmitPending
	nameAt, handleAt      time.Time
	nameQ, handleQ        string
	nameGen, handleGen    int
	focused               bool
	badgeCount            int

	copyClk  widget.Clickable
	copyTip  TipState
	copiedAt time.Time

	bump      bool
	keep, cls *ModalButton
	veil      int
	save      *ModalButton
}

// OpenUserGroupEdit opens Edit User Group for g; focus "channels" puts
// the caret in the channel picker (Edit Channels), else in the name.
func OpenUserGroupEdit(h Host, g core.UsergroupProfile, focus string) *UserGroupEditModal {
	e := &UserGroupEditModal{h: h, g: g, focus: focus, section: g.IsSection}
	for _, ed := range []*widget.Editor{&e.name, &e.handle} {
		ed.SingleLine = true
	}
	e.name.SetText(g.Name)
	e.handle.SetText(g.Handle)
	e.purpose.SetText(g.Description)
	e.nameQ, e.handleQ = g.Name, g.Handle
	c := h.Core()
	e.chans = newUGSelect(h, false, "")
	e.chans.search = func(ctx context.Context, q string) ([]ugOption, error) {
		hs, err := c.UsergroupChannelCandidates(ctx, q)
		out := make([]ugOption, len(hs))
		for i, hh := range hs {
			out[i] = ugOption{ID: hh.ID, Label: hh.Name, Conv: core.Conv{ID: hh.ID, Name: hh.Name, Kind: hh.Kind}}
		}
		return out, err
	}
	var picked []ugOption
	for _, id := range g.DefaultChannels() {
		picked = append(picked, ugChannelOption(c, id))
	}
	e.chans.SetPicked(picked)
	if c != nil { // seenUserGroupSectionsNewBadge: under 4, count this one
		if raw, ok := c.Pref("seen_usergroup_section_new_badge"); ok {
			e.badgeCount = int(prefNumber(raw))
		}
		if e.badgeCount < 4 {
			n := e.badgeCount + 1
			go func() { _ = c.SetPref(context.Background(), "seen_usergroup_section_new_badge", n) }()
		}
	}
	e.save = &ModalButton{Label: nsUG.T("Save"), Kind: BtnPrimary,
		Enabled: func() bool { return e.CanSave() && !e.pending },
		Do:      func() bool { e.Save(); return false }}
	e.keep = &ModalButton{Label: nsUG.T("Keep Editing"), Kind: BtnOutline} // speedBumpCancelButtonText
	e.cls = &ModalButton{Label: nsUG.T("Close"), Kind: BtnDanger}          // speedBumpContinueButtonText
	e.m = NewModal(h, ModalOpts{
		Title: nsUG.T("Edit User Group"),
		Width: 580,
		Body:  e.layout,
		Buttons: []*ModalButton{{Label: nsUG.T("Cancel"), Kind: BtnOutline,
			Do: func() bool { e.bump = true; return false }}, e.save}, // onRequestCancel: the speed bump
		FooterLeft: e.footer,
		FocusStops: func() []event.Tag {
			return []event.Tag{&e.name, &e.handle, &e.purpose, &e.chans.ed, &e.sectionClk}
		},
		CloseGuard: func() bool { // getShouldShowSpeedBump: always, see above
			e.bump = true
			return false
		},
		Over: e.over,
	}).OldDialog()
	h.ShowOverlay(e)
	return e
}

// prefNumber reads a pref that is a number (or a numeric string).
func prefNumber(raw []byte) float64 {
	s := strings.Trim(string(raw), `"`)
	var n float64
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + float64(r-'0')
	}
	return n
}

// Layout and Dismissed make it an Overlay.
func (e *UserGroupEditModal) Layout(gtx layout.Context) layout.Dimensions { return e.m.Layout(gtx) }
func (e *UserGroupEditModal) Dismissed() bool                             { return e.m.Dismissed() }

// Dismiss closes it the way the close button does (the speed bump).
func (e *UserGroupEditModal) Dismiss() { e.m.Dismiss() }

// SpeedBump reports the speed bump is up.
func (e *UserGroupEditModal) SpeedBump() bool { return e.bump }

// Fields' setters and state (tests).
func (e *UserGroupEditModal) SetName(s string) {
	e.name.SetText(s)
	e.nameChanged(time.Now())
}
func (e *UserGroupEditModal) SetHandle(s string) {
	e.handle.SetText(s)
	e.handleChanged(time.Now())
}
func (e *UserGroupEditModal) SetPurpose(s string)      { e.purpose.SetText(s) }
func (e *UserGroupEditModal) Channels() *ugSelect      { return e.chans }
func (e *UserGroupEditModal) Errors() (string, string) { return e.nameErr, e.handleErr }
func (e *UserGroupEditModal) SubmitError() string      { return e.submitErr }

// locked is the auto-generated group's disabled name and handle.
func (e *UserGroupEditModal) locked() bool { return e.g.AutoGenerated() }

// CanSave is !goButtonIsDisabled: no field error, name and handle set,
// the purpose within 140.
func (e *UserGroupEditModal) CanSave() bool {
	return e.nameErr == "" && e.handleErr == "" && e.name.Text() != "" && e.handle.Text() != "" && jsLen(e.purpose.Text()) <= 140
}

// jsLen is a JavaScript string's length (UTF-16 code units), what
// onPurposeChange and the textarea's maxCharacterLimit count.
func jsLen(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}

// nsFormInputs is T+c6's namespace (the inputs' length alerts), and
// nsAppDialog the c-label's ("(optional)", P7kL).
var (
	nsFormInputs = i18n.NS("form_inputs")
	nsAppDialog  = i18n.NS("app_dialog")
)

// Save is onDetailsStepComplete then onEditDialogSubmit.
func (e *UserGroupEditModal) Save() {
	c := e.h.Core()
	if c == nil || e.pending || !e.CanSave() {
		return
	}
	e.pending = true
	g := e.g
	name, handle, purpose := e.name.Text(), e.handle.Text(), e.purpose.Text()
	chans := e.chans.IDs()
	section := e.section
	type res struct{ nameErr, handleErr string }
	run(e.h, func(ctx context.Context) (res, error) {
		return res{ugValidateName(ctx, c, name, g.Name, g.TeamID), ugValidateHandle(ctx, c, handle, g.Handle, g.TeamID)}, nil
	}, func(r res, _ error) {
		e.nameErr, e.handleErr = r.nameErr, r.handleErr
		e.pending = false // validate's finally
		if r.nameErr != "" || r.handleErr != "" {
			return
		}
		e.pending = true
		run(e.h, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.UpdateUsergroup(ctx, core.UsergroupUpdate{ID: g.ID, TeamID: g.TeamID, Name: strings.TrimSpace(name),
				Handle: handle, Description: purpose, Channels: chans, AdditionalChannels: g.AdditionalChannels, EnableSection: section})
		}, func(_ struct{}, err error) {
			e.pending = false
			if err != nil { // onSubmitFailure: setApiError(e.name ?? e)
				e.submitErr = "error"
				var se *slack.Error
				if errors.As(err, &se) {
					e.submitErr = se.Code
				}
				return
			}
			e.submitErr = ""
			e.h.Toast(nsUG.T("Group has been updated."))
			e.m.Close()
		})
	})
}

func (e *UserGroupEditModal) events(gtx layout.Context) {
	c := e.h.Core()
	if !e.focused {
		e.focused = true
		if e.focus == "channels" {
			e.chans.Focus()
		} else {
			gtx.Execute(key.FocusCmd{Tag: &e.name})
		}
	}
	for {
		ev, ok := e.name.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.ChangeEvent); ok {
			e.nameChanged(gtx.Now)
		}
	}
	for {
		ev, ok := e.handle.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.ChangeEvent); ok {
			e.handleChanged(gtx.Now)
		}
	}
	for {
		if _, ok := e.purpose.Update(gtx); !ok {
			break
		}
	}
	if e.sectionClk.Clicked(gtx) {
		e.section = !e.section // onUserGroupSectionOptionChange
	}
	// the 300 ms debounced validations
	due := func(at time.Time) (bool, time.Duration) {
		w := at.Add(300 * time.Millisecond).Sub(gtx.Now)
		return w <= 0, w
	}
	if !e.nameAt.IsZero() && c != nil {
		if ok, wait := due(e.nameAt); ok {
			e.nameAt = time.Time{}
			gen, name, g := e.nameGen, e.nameQ, e.g
			run(e.h, func(ctx context.Context) (string, error) { return ugValidateName(ctx, c, name, g.Name, g.TeamID), nil },
				func(s string, _ error) {
					if gen == e.nameGen {
						e.nameErr = s
					}
				})
		} else {
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(wait)})
		}
	}
	if !e.handleAt.IsZero() && c != nil {
		if ok, wait := due(e.handleAt); ok {
			e.handleAt = time.Time{}
			gen, handle, g := e.handleGen, e.handleQ, e.g
			run(e.h, func(ctx context.Context) (string, error) {
				return ugValidateHandle(ctx, c, handle, g.Handle, g.TeamID), nil
			},
				func(s string, _ error) {
					if gen == e.handleGen {
						e.handleErr = s
					}
				})
		} else {
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(wait)})
		}
	}
	if e.copyClk.Clicked(gtx) { // CopyUserGroupId
		copyText(gtx, e.g.ID)
		e.copiedAt = gtx.Now
	}
}

// nameChanged is onNameChange: validateUserGroupName, debounced 300 ms.
func (e *UserGroupEditModal) nameChanged(now time.Time) {
	if t := e.name.Text(); t != e.nameQ {
		e.nameQ, e.nameAt = t, now
		e.nameGen++
	}
}

// handleChanged is onHandleChange: the spaces dropped, then
// validateUserGroupHandle (debounced 300 ms) unless the text starts with
// a space.
func (e *UserGroupEditModal) handleChanged(now time.Time) {
	t := e.handle.Text()
	if t == e.handleQ {
		return
	}
	if s := strings.ReplaceAll(t, " ", ""); s != t {
		ss, _ := e.handle.Selection()
		e.handle.SetText(s)
		e.handle.SetCaret(maxInt(0, ss-1), maxInt(0, ss-1))
		if strings.HasPrefix(t, " ") {
			e.handleQ = s
			return
		}
		t = s
	}
	e.handleQ, e.handleAt = t, now
	e.handleGen++
}

// footer is renderFooterText: CopyUserGroupId.
func (e *UserGroupEditModal) footer(gtx layout.Context) layout.Dimensions {
	th := e.h.Theme()
	cl := colorsOf(th)
	s := nsUG.T("User Group ID: {id}", "id", e.g.ID)
	bh := dp(gtx, 28)
	space := textW(gtx, th, 13, theme.Regular, " ")
	tw := minInt(textW(gtx, th, 13, theme.Regular, s), maxInt(1, gtx.Constraints.Max.X-space-bh)) // the button stays in the room left of the footer's buttons
	at(gtx, image.Pt(0, (bh-dp(gtx, 18))/2), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(tw, dp(gtx, 18))}
		return para(gtx, th, 13, 18, theme.Regular, cl.Fg2, s, 1)
	})
	bx := tw + space
	at(gtx, image.Pt(bx, 0), func(gtx layout.Context) layout.Dimensions {
		d := iconButton(gtx, &e.copyClk, "copy", 28, 16, cl.Fg2, cl.BgHover, 6)
		tip := nsUG.T("Copy user group id")
		if !e.copiedAt.IsZero() && gtx.Now.Sub(e.copiedAt) < 2*time.Second {
			tip = nsUG.T("Copied!")
			gtx.Execute(op.InvalidateCmd{At: e.copiedAt.Add(2 * time.Second)})
		}
		Tooltip(gtx, th, &e.copyTip, e.copyClk.Hovered(), d.Size, Above, tip, "")
		return d
	})
	return layout.Dimensions{Size: image.Pt(bx+bh, bh)}
}

// over is the speed bump (DialogSpeedBump, c-dialog_speed_bump): the
// card veiled with base-pry at 70%, a sheet at the bottom (padding 20px
// 24px, a 1px top border) with the question (an h3, 1rem under it) and
// Keep Editing (focused) / Close (danger), 12px apart at the right.
func (e *UserGroupEditModal) over(gtx layout.Context) {
	if !e.bump {
		return
	}
	th := e.h.Theme()
	cl := colorsOf(th)
	if e.keep.click.Clicked(gtx) {
		e.bump = false
		return
	}
	if e.cls.click.Clicked(gtx) { // onContinue: onCancel, the dialog closes
		e.bump = false
		e.m.Close()
		return
	}
	sz := gtx.Constraints.Max
	func() {
		rad := dp(gtx, 8)
		defer clip.RRect{Rect: image.Rectangle{Max: sz}, NE: rad, NW: rad, SE: rad, SW: rad}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &e.veil)
		for {
			if _, ok := gtx.Event(pointer.Filter{Target: &e.veil, Kinds: pointer.Press | pointer.Release | pointer.Scroll,
				ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}}); !ok {
				break
			}
		}
		fill(gtx, image.Rectangle{Max: sz}, theme.WithAlpha(BasePryOf(th), 179))
	}()
	px, py := dp(gtx, 24), dp(gtx, 20)
	iw := sz.X - 2*px
	call, td := record(loose(gtx, image.Pt(iw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return para(gtx, th, 15, 22, theme.Bold, cl.Fg, nsUG.T("Your work won’t be saved - are you sure you want to close?"), 0)
	})
	bh := dp(gtx, 36)
	sheetH := py + td.Size.Y + dp(gtx, 16) + bh + py
	top := sz.Y - sheetH
	sheet := image.Rect(0, top, sz.X, sz.Y)
	rad := dp(gtx, 8)
	func() {
		defer clip.RRect{Rect: sheet, SE: rad, SW: rad}.Push(gtx.Ops).Pop()
		fill(gtx, sheet, BasePryOf(th))
	}()
	fill(gtx, image.Rect(0, top, sz.X, top+border1(gtx)), theme.OtlTer(th))
	macroAt(gtx, call, image.Pt(px, top+py))
	by := top + py + td.Size.Y + dp(gtx, 16)
	x := sz.X - px
	for _, b := range []*ModalButton{e.cls, e.keep} {
		ring := b == e.keep
		c, d := record(loose(gtx, image.Pt(sz.X, bh)), func(gtx layout.Context) layout.Dimensions {
			return b.draw(gtx, th, b.Kind, ring, grpNone)
		})
		x -= d.Size.X
		macroAt(gtx, c, image.Pt(x, by))
		b.rect = image.Rectangle{Min: image.Pt(x, by), Max: image.Pt(x, by).Add(d.Size)}.Add(e.m.rect.Min)
		x -= dp(gtx, 12)
	}
}

func (e *UserGroupEditModal) layout(gtx layout.Context) layout.Dimensions {
	e.events(gtx)
	th := e.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := 0
	put := func(d layout.Dimensions, gap int) { y += d.Size.Y + gap }
	// maybeRenderError: a failed save
	if e.submitErr != "" {
		msg := nsUG.T("We’re not sure why, but that didn’t work. Try again?")
		if e.submitErr == "too_many_linked_channels" {
			msg = nsUG.T("User groups can’t have more than 100 default channels.")
		}
		put(at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions { return ugAlert(gtx, th, cl, "error", msg, w) }), dp(gtx, 8))
	}
	// maybeRenderAutogeneratedUserGroupAlert
	if e.locked() {
		msg := nsUG.T("This user group is automatically generated for Workspace Admins.")
		if e.g.AutoType == "owner" {
			msg = nsUG.T("This user group is automatically generated for Workspace Owners.")
		}
		put(at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions { return ugAlert(gtx, th, cl, "info", msg, w) }), dp(gtx, 8))
	}
	put(e.field(gtx, th, cl, y, w, nsUG.T("Name"), "", &e.name, nsUG.T("e.g. Accounts Team"), "", e.nameErr, e.locked(), 44), 0)
	put(e.field(gtx, th, cl, y, w, nsUG.T("Handle"), "", &e.handle, nsUG.T("e.g. accounts-team"), "mentions",
		e.handleErr, e.locked(), 44), 0)
	put(e.field(gtx, th, cl, y, w, nsUG.T("Purpose"), nsAppDialog.T("(optional)"), &e.purpose, "", "", "", false, 36), 0)
	// Channels (UserGroupDefaultChannelPicker)
	lh := dp(gtx, 22)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Bold, cl.Fg, nsUG.T("Channels"))
		})
	})
	y += lh + dp(gtx, 8)
	put(at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return e.chans.layout(gtx, th, cl)
	}), dp(gtx, 4))
	put(at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 13, 18, theme.Regular, cl.Fg3, nsUG.T("Group members are added to these channels automatically."), 0)
	}), dp(gtx, 20))
	put(e.sectionToggle(gtx, th, cl, y, w), 0)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// field is a c-label with its input: the 15/700 label (and "(optional)"),
// the box (h dp; disabled: ctr-sec, no caret), the inline error or the
// hint under it, and the 20px gap Slack's form rows keep.
func (e *UserGroupEditModal) field(gtx layout.Context, th *theme.Theme, cl *Colors, y, w int, label, opt string, ed *widget.Editor,
	ph, ic, errText string, disabled bool, h float32) layout.Dimensions {
	return at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		yy := 0
		lh := dp(gtx, 22)
		lw := textW(gtx, th, 15, theme.Bold, label)
		at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Bold, cl.Fg, label) })
		})
		if opt != "" { // .c-label__optional_tag: content-ter, margin-left 4, regular
			at(gtx, image.Pt(lw+dp(gtx, 4), 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Regular, cl.Fg3, opt) })
			})
		}
		yy += lh + dp(gtx, 8) // .c-label__text padding-bottom 8
		ih := dp(gtx, h)
		at(gtx, image.Pt(0, yy), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(w, ih))
			ed.ReadOnly = disabled
			fs := float32(18)
			if h < 40 {
				fs = 15
			}
			var d layout.Dimensions
			if disabled {
				d = fieldOn(gtx, th, ed, ph, ic, h, fs, epDisabledBg(th, cl))
			} else {
				d = field(gtx, th, ed, ph, ic, h, fs)
			}
			if errText != "" {
				stroke(gtx, image.Rect(0, 0, w, ih), dp(gtx, 8), border1(gtx), ccErrColor(th, cl))
			}
			if ed == &e.purpose && gtx.Focused(ed) { // CharacterCount: the characters left, while focused
				n := 140 - jsLen(ed.Text())
				fg := cl.Fg2
				if n < 0 {
					fg = ccErrColor(th, cl)
				}
				ns := strconv.Itoa(n)
				nw := textW(gtx, th, 13, theme.Regular, ns)
				at(gtx, image.Pt(w-dp(gtx, 12)-nw, (ih-dp(gtx, 18))/2), func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, fg, ns)
				})
			}
			return d
		})
		yy += ih
		hint := ""
		switch {
		case ed == &e.handle:
			hint = nsUG.T("You’ll use the display name to get the group’s attention. It should be all lowercase, with no spaces.")
		case ed == &e.purpose:
			hint = nsUG.T("What is this group about?")
		}
		if ed == &e.purpose && jsLen(ed.Text()) > 140 { // FormTextareaInput's renderErrorMaybe
			errText = nsFormInputs.T("You can’t enter more than {maxCharacterLimit, plural, =1 {# character} other {# characters}}.", "maxCharacterLimit", 140)
		}
		if errText != "" {
			yy += dp(gtx, 4)
			d := at(gtx, image.Pt(0, yy), func(gtx layout.Context) layout.Dimensions { return epInlineError(gtx, th, cl, errText, w) })
			yy += d.Size.Y
		} else if hint != "" {
			yy += dp(gtx, 4)
			d := at(gtx, image.Pt(0, yy), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
				return para(gtx, th, 13, 18, theme.Regular, cl.Fg3, hint, 0) // .c-hint: content-ter, margin 4px 0 20px
			})
			yy += d.Size.Y
		}
		yy += dp(gtx, 20)
		return layout.Dimensions{Size: image.Pt(w, yy)}
	})
}

// sectionToggle is renderEnableSectionCheckbox: the bordered box (1px
// otl-ter, radius 8, margin-bottom 8), its body (padding 12px 20px 8px
// 12px: the bold title with the "New" tag, the secondary subtext with
// 12px under it, the switch) and the illustration panel (plt-indigo-40,
// padding 5px 26px 4.75px).
func (e *UserGroupEditModal) sectionToggle(gtx layout.Context, th *theme.Theme, cl *Colors, y, w int) layout.Dimensions {
	img, ok := ugSectionImage()
	iw := 0
	if ok {
		iw = dp(gtx, 88) + 2*dp(gtx, 26)
	}
	bw := maxInt(1, w-iw-dp(gtx, 12)-dp(gtx, 20))
	title := nsUG.T("Add group channels as a section in Home")
	sub := nsUG.T("Organize the channels into a section in the Home sidebar for all group members.")
	// __title is a flex row (gap 4, align-items center): the text shrinks
	// beside the micro "New" tag
	tc, td := record(gtx, func(gtx layout.Context) layout.Dimensions {
		newTag := e.badgeCount < 4
		tagW := 0
		if newTag {
			tagW = menuTagWidth(gtx, th, nsUG.T("New")) + dp(gtx, 4)
		}
		textMax := maxInt(1, bw-tagW)
		gtx.Constraints = layout.Constraints{Max: image.Pt(textMax, 1<<20)}
		d := para(gtx, th, 15, 22, theme.Bold, cl.Fg, title, 0)
		if newTag {
			tw := minInt(textMax, textW(gtx, th, 15, theme.Bold, title))
			if d.Size.Y > dp(gtx, 22) {
				tw = textMax // a wrapped text fills its flex item
			}
			at(gtx, image.Pt(0, 0), func(gtx layout.Context) layout.Dimensions {
				drawMenuTag(gtx, th, &MenuTag{Label: nsUG.T("New"), Style: "education"}, false, tw+tagW, d.Size.Y)
				return layout.Dimensions{}
			})
		}
		return layout.Dimensions{Size: image.Pt(bw, d.Size.Y)}
	})
	sc, sd := record(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(bw, 1<<20)}
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg2, sub, 0)
	})
	bodyH := dp(gtx, 12) + td.Size.Y + sd.Size.Y + dp(gtx, 12) + dp(gtx, 16) + dp(gtx, 8)
	imgH := 0
	if ok {
		imgH = dp(gtx, 5) + dp(gtx, 125) + dp(gtx, 4.75)
	}
	h := maxInt(bodyH, imgH)
	return at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		box := image.Rect(0, 0, w, h)
		rrect(gtx, box, dp(gtx, 8), cl.InputBorder)
		rrect(gtx, box.Inset(border1(gtx)), dp(gtx, 8)-border1(gtx), cl.Bg)
		at(gtx, image.Pt(dp(gtx, 12), dp(gtx, 12)), func(gtx layout.Context) layout.Dimensions {
			tc.Add(gtx.Ops)
			at(gtx, image.Pt(0, td.Size.Y), func(gtx layout.Context) layout.Dimensions {
				sc.Add(gtx.Ops)
				return sd
			})
			at(gtx, image.Pt(0, td.Size.Y+sd.Size.Y+dp(gtx, 12)), func(gtx layout.Context) layout.Dimensions {
				return e.sectionClk.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return Toggle(gtx, th, e.section, false)
				})
			})
			return layout.Dimensions{}
		})
		if ok {
			p := image.Rect(w-iw, 0, w, h)
			func() {
				defer clipRRect(gtx, p, 0).Pop()
				rrect(gtx, image.Rect(p.Min.X-dp(gtx, 8), 0, w, h).Inset(border1(gtx)), dp(gtx, 8)-border1(gtx), ugIndigo40(th))
				fill(gtx, image.Rect(p.Min.X, border1(gtx), p.Min.X+dp(gtx, 8), h-border1(gtx)), ugIndigo40(th))
			}()
			isz := image.Pt(dp(gtx, 88), dp(gtx, 125))
			at(gtx, image.Pt(p.Min.X+dp(gtx, 26), (h-isz.Y)/2), func(gtx layout.Context) layout.Dimensions {
				return containImage(gtx, img, isz)
			})
		}
		return layout.Dimensions{Size: image.Pt(w, h+dp(gtx, 8))}
	})
}

// ugIndigo40 is --dt_color-plt-indigo-40.
func ugIndigo40(th *theme.Theme) color.NRGBA {
	return rgb(0x8193f9)
}
