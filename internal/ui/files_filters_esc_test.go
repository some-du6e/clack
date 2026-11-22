package ui

import (
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The Files browser's Filters modal through the real shell, whose key
// bindings read Escape before the overlays lay out: with From's list open,
// Escape closes the list and leaves the modal (MultiSelectInput's
// onEscape; the select claims plain Escape while expanded, so the shell's
// Close binding steps aside for the modal's OnEscape). Enter then picks
// nothing from the hidden list, and a second Escape closes the modal.
func TestFilesFiltersEscapeThroughShell(t *testing.T) {
	e, _ := sectionEnv(t)
	a := e.a
	var name, id string
	for _, u := range a.data.core.KnownUsers() {
		if u.ID != fakeslack.SelfID && len(u.Name) >= 4 && !u.IsBot && !u.Deleted {
			name, id = u.Name, u.ID
			break
		}
	}
	if id == "" {
		t.Fatal("no person to search for")
	}
	m := popups.OpenFilesFilters(a, popups.FilesFilters{}, popups.FilesFiltersOpts{})
	e.frame()
	m.From().Focus()
	e.frame()
	e.frame()
	m.From().Type(name[:4])
	e.until("From's list", 10*time.Second, func() bool { return m.From().Expanded() })
	e.press(key.NameEscape, 0)
	if m.Modal().Dismissed() || m.From().Expanded() {
		t.Fatalf("first Escape: modal dismissed %v, list open %v", m.Modal().Dismissed(), m.From().Expanded())
	}
	e.press(key.NameReturn, 0)
	if got := m.From().IDs(); len(got) != 0 {
		t.Errorf("Enter with the list closed picked %v", got)
	}
	e.press(key.NameEscape, 0)
	if !m.Modal().Dismissed() {
		t.Error("the second Escape left the modal open")
	}
}

// Edit User Group through the real shell: Escape with the Channels list
// open closes only the list (its MultiSelectInput's onEscape), the dialog
// stays with the caret in Channels and no speed bump; with the list
// closed, Escape asks the CloseGuard (the speed bump) and the dialog stays
// drawn. On f0835733 the select claimed Escape for a dialog that didn't
// ask it, so the modal's own Escape dismissed into the guard and drew
// nothing that frame.
func TestUserGroupEditEscapeThroughShell(t *testing.T) {
	e, fs := sectionEnv(t)
	a := e.a
	c := a.data.core
	fs.UsergroupEditor = true
	gs, err := c.C.Usergroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c.S.SetUsergroups(gs)
	g, err := c.UsergroupProfile(t.Context(), fakeslack.WorkspaceGroupID)
	if err != nil {
		t.Fatal(err)
	}
	var prefix string
	for _, cv := range c.RecentSnapshot().Convs {
		if cv.Kind == "channel" && !cv.IsArchived && len(cv.Name) >= 3 {
			prefix = cv.Name[:3]
			break
		}
	}
	if prefix == "" {
		t.Fatal("no channel to search for")
	}
	m := popups.OpenUserGroupEdit(a, g, "channels")
	sel := m.Channels()
	e.until("the Channels focus", 10*time.Second, func() bool { return e.r.Source().Focused(sel.Tag()) })
	sel.Type(prefix)
	e.until("the Channels list", 10*time.Second, func() bool { return sel.Expanded() })
	e.press(key.NameEscape, 0)
	if m.Dismissed() || m.SpeedBump() || sel.Expanded() || !e.r.Source().Focused(sel.Tag()) {
		t.Fatalf("first Escape: dismissed %v, speed bump %v, list open %v, caret in Channels %v",
			m.Dismissed(), m.SpeedBump(), sel.Expanded(), e.r.Source().Focused(sel.Tag()))
	}
	e.press(key.NameEscape, 0)
	if m.Dismissed() || !m.SpeedBump() {
		t.Errorf("second Escape: dismissed %v, speed bump %v", m.Dismissed(), m.SpeedBump())
	}
}
