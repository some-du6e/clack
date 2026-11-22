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
