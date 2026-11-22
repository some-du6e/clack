package popups

import (
	"net/url"
	"strings"
	"testing"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// ugCreateHost is the test host plus showUserGroupDetails, recorded.
type ugCreateHost struct {
	*tzHost
	opened []string
}

func (h *ugCreateHost) OpenUserGroup(id string) { h.opened = append(h.opened, id) }

// pickMember types q in the members select and presses Enter on the
// first match.
func (e *ugRT) pickMember(s *ugSelect, q string) {
	e.t.Helper()
	e.typeIn(&s.ed, q)
	e.until("the people search", func() bool { return len(s.Options()) > 0 })
	e.press(key.NameReturn)
}

// Create User Group from the user group browser (UserGroupEditCreateDialog,
// build a09b625d90aa): DETAILS with the Edit form's checks, Next, then
// MEMBERS; nobody picked keeps it open with Slack's alert; Back keeps the
// form; Create Group sends usergroups.create with the form's fields and
// usergroups.users.update with the picks, opens the new group's pane,
// toasts "User group created!" and closes.
func TestUserGroupCreateLikeSlack(t *testing.T) {
	for _, dark := range []bool{false, true} {
		e := newUGRT(t, true, dark, fakeslack.WorkspaceGroupID)
		h := &ugCreateHost{tzHost: e.h}
		m := OpenUserGroupCreate(h)
		if m == nil || e.top() != Overlay(m) {
			t.Fatalf("%s: Create User Group opened %T (toasts %q)", e.mode, e.top(), e.h.toastList())
		}
		e.until("the rights and the name's caret", func() bool { return !m.FieldsDisabled() && e.r.Source().Focused(&m.name) })
		x := e.texts()
		for _, want := range []string{"Create User Group", "Name", "Handle", "Purpose", "(optional)", "Channels", "Cancel", "Next", "Step 1 of 2",
			"Add group channels as a section in Home"} {
			if !hasText(x, want) {
				t.Errorf("%s DETAILS: %q not drawn", e.mode, want)
			}
		}
		if m.CanSave() || m.PermissionError() != "" {
			t.Errorf("%s: an empty form can go on (%v), permission alert %q", e.mode, m.CanSave(), m.PermissionError())
		}
		e.shot("usergroup-create-details")

		// a taken handle: Slack's error, Next off
		m.SetName("Design crew")
		m.SetHandle("snackcrew")
		e.until("the handle error", func() bool { _, he := m.Errors(); return he != "" })
		if _, he := m.Errors(); he != "This handle is already in use by a member, channel, or another group." || m.CanSave() {
			t.Errorf("%s: handle error %q, can go on %v", e.mode, he, m.CanSave())
		}
		m.SetHandle("designcrew")
		m.SetPurpose("Synthetic purpose")
		e.until("Next on", func() bool { return m.CanSave() })
		e.click(e.button("Next"))
		e.until("the MEMBERS step", func() bool { return m.Step() == 1 })
		for _, method := range []string{"subteams.validateName", "subteams.validateHandle"} {
			f := e.calls.get(method)
			if len(f) == 0 || f[len(f)-1].Get("target_team") != fakeslack.TeamID {
				t.Errorf("%s %s %v", e.mode, method, f)
			}
		}
		if m.Title() != "Add members" || m.ButtonLabels() != "Back|Create Group" || m.FooterNote() != "Step 2 of 2" {
			t.Errorf("%s MEMBERS: %q %q %q", e.mode, m.Title(), m.ButtonLabels(), m.FooterNote())
		}
		s := m.Members()
		e.until("the members select's caret", func() bool { return e.r.Source().Focused(&s.ed) })

		// nobody: the alert, still open, nothing sent
		e.click(e.button("Create Group"))
		x = e.texts()
		if !m.NoMembersError() || m.CanSave() || m.Dismissed() || !hasText(x, "User groups need to have at least one member.") {
			t.Errorf("%s nobody: alert %v, can go %v, dismissed %v", e.mode, m.NoMembersError(), m.CanSave(), m.Dismissed())
		}
		if n := len(e.calls.get("usergroups.create")); n != 0 {
			t.Errorf("%s: Create Group with nobody sent usergroups.create", e.mode)
		}
		e.shot("usergroup-create-members-empty")

		// Back keeps the form; Next again
		e.click(e.button("Back"))
		if m.Step() != 0 || m.name.Text() != "Design crew" || m.ButtonLabels() != "Cancel|Next" || m.Dismissed() {
			t.Fatalf("%s Back: step %d, name %q, buttons %q", e.mode, m.Step(), m.name.Text(), m.ButtonLabels())
		}
		e.click(e.button("Next"))
		e.until("the MEMBERS step again", func() bool { return m.Step() == 1 })
		e.until("the members select's caret again", func() bool { return e.r.Source().Focused(&s.ed) })
		e.pickMember(s, "a")
		e.pickMember(s, "e")
		picked := s.IDs()
		if len(picked) != 2 {
			t.Fatalf("%s: picked %v", e.mode, picked)
		}
		e.until("Create Group on", func() bool { return m.CanSave() })
		e.shot("usergroup-create-members")
		e.click(e.button("Create Group"))
		e.until("the toast", func() bool { return e.toasted("User group created!") })
		f := e.calls.get("usergroups.create")
		want := url.Values{"name": {"Design crew"}, "handle": {"designcrew"}, "description": {"Synthetic purpose"}, "team_id": {fakeslack.TeamID},
			"channels": {""}, "enable_section": {"false"}}
		if len(f) != 1 || f[0].Encode() != want.Encode() {
			t.Errorf("%s usergroups.create\n got %v\nwant %v", e.mode, f, want)
		}
		u := e.calls.get("usergroups.users.update")
		if len(u) != 1 || u[0].Get("usergroup") != "S0FAKENEW001" || u[0].Get("users") != strings.Join(picked, ",") || u[0].Has("team_id") {
			t.Errorf("%s usergroups.users.update %v, want S0FAKENEW001 with %v", e.mode, u, picked)
		}
		if !m.Dismissed() || len(h.opened) != 1 || h.opened[0] != "S0FAKENEW001" {
			t.Errorf("%s after Create Group: dismissed %v, panes opened %v", e.mode, m.Dismissed(), h.opened)
		}
		if g, members, _ := e.fs.UsergroupNow("S0FAKENEW001"); g.Handle != "designcrew" || len(members) != 2 {
			t.Errorf("%s: the fake's new group %+v with %v", e.mode, g, members)
		}
	}
}

// A viewer without CREATE_USER_GROUPS or EDIT_USER_GROUPS in the
// workspace: maybeRenderError's "You do not have permission to create user
// groups in this workspace.", the name and handle disabled, Next off.
func TestUserGroupCreateNoPermissionLikeSlack(t *testing.T) {
	e := newUGRT(t, false, false, fakeslack.WorkspaceGroupID)
	m := OpenUserGroupCreate(e.h)
	if m == nil {
		t.Fatalf("Create User Group didn't open (toasts %q)", e.h.toastList())
	}
	const msg = "You do not have permission to create user groups in this workspace."
	e.until("the rights", func() bool { return m.PermissionError() != "" })
	if x := e.texts(); m.PermissionError() != msg || !m.FieldsDisabled() || m.CanSave() || !hasText(x, msg) {
		t.Errorf("alert %q, disabled %v, can go %v", m.PermissionError(), m.FieldsDisabled(), m.CanSave())
	}
	e.shot("usergroup-create-no-permission")
}

// A failed usergroups.create: "We’re not sure why, but that didn’t work.
// Try again?" and the dialog closes (shouldCloseOnGo); no members update.
func TestUserGroupCreateFailureLikeSlack(t *testing.T) {
	e := newUGRT(t, true, false, fakeslack.WorkspaceGroupID)
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		e.calls.add(method, f)
		if method == "usergroups.create" {
			return map[string]any{"ok": false, "error": "ratelimited"}, true
		}
		return nil, false
	})
	m := OpenUserGroupCreate(e.h)
	if m == nil {
		t.Fatalf("Create User Group didn't open (toasts %q)", e.h.toastList())
	}
	e.until("the rights", func() bool { return !m.FieldsDisabled() })
	m.SetName("Design crew")
	m.SetHandle("designcrew")
	e.until("Next on", func() bool { return m.CanSave() })
	m.Save()
	e.until("the MEMBERS step", func() bool { return m.Step() == 1 })
	e.pickMember(m.Members(), "a")
	e.until("Create Group on", func() bool { return m.CanSave() })
	m.Save()
	e.until("the failure toast", func() bool { return e.toasted("We’re not sure why, but that didn’t work. Try again?") })
	if !m.Dismissed() || len(e.calls.get("usergroups.users.update")) != 0 {
		t.Errorf("dismissed %v, users.update %v", m.Dismissed(), e.calls.get("usergroups.users.update"))
	}
}
