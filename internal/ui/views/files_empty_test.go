package views

import (
	"os"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The unified files browser's empty states, as Slack words them
// (useUnifiedFilesExplorer, the canvases explorer, the lists NUX header).
func TestFilesEmptyCopy(t *testing.T) {
	for _, tc := range []struct {
		kind             string
		search, adv, all bool
		title, desc      string
		icon, button     string
	}{
		{"", false, false, true, "No files yet", "Your recently viewed or updated files will be shown here.", "", ""},
		{"", true, false, true, "No results", "Your recently viewed or updated files will be shown here.", "", ""},
		{"", true, false, false, "No results", "You may want to try adjusting your filters.", "", "Clear Filters"},
		{"", false, false, false, "No files found", "It may be that you don’t have any files yet, or that there aren’t any that fit your current filters.", "", "Clear Filters"},
		// advanced filters (hasAdvancedFiltersSet) count as filtering
		// with every type on, with or without a search
		{"", false, true, true, "No results", "You may want to try adjusting your filters.", "", "Clear Filters"},
		{"", true, true, true, "No results", "You may want to try adjusting your filters.", "", "Clear Filters"},
		{"", false, true, false, "No results", "You may want to try adjusting your filters.", "", "Clear Filters"},
		{"canvases", false, false, true, "No canvases yet", "Your recently viewed or updated canvases will show up here.", "plus", "New Canvas"},
		{"canvases", true, false, true, "No results", "You may want to try adjusting your filters.", "", ""},
		{"canvases", false, true, true, "No results", "You may want to try adjusting your filters.", "", ""},
		{"lists", false, false, true, "No lists yet", "Your recently viewed or updated lists will be shown here.", "plus", "New List"},
		{"lists", true, false, true, "No results", "You may want to try adjusting your filters.", "", ""},
		{"lists", false, true, true, "No results", "You may want to try adjusting your filters.", "", ""},
	} {
		got := filesEmptyCopy(tc.kind, tc.search, tc.adv, tc.all)
		want := filesEmptyState{tc.title, tc.desc, tc.icon, tc.button}
		if got != want {
			t.Errorf("%q search=%v adv=%v all=%v:\n got %+v\nwant %+v", tc.kind, tc.search, tc.adv, tc.all, got, want)
		}
	}
	// Starred's empty row: Slack's sentence, the star a character between
	// two spaces (the theme draws it as an emoji image in its own slot)
	for _, s := range []string{starHintText, starHintTextFiles} {
		if !strings.Contains(s, "Click the ⭐ star on any ") || !strings.HasSuffix(s, " to add it here for later.") {
			t.Errorf("starred hint %q", s)
		}
	}
}

// The sidebar's Starred part is the starred file collection's header row,
// which a click collapses (the client store's toggleCollectionCollapse),
// never the old plain "Starred" sub-header: Slack dropped
// custom_file_sections and kept its on path (the 2026-11-21 drift), so the
// old gate forced off changes nothing.
func TestFilesSidebarStarred(t *testing.T) {
	v, h, e, _ := filesView(t)
	app := h.Core()
	app.SetExperimentGroup("custom_file_sections", "off")
	e.until(t, "files.collections.list", func() bool { _, ok := app.FileCollections(); return ok })
	e.frame()
	if !v.fileCollections() || hasRow(v, "starred-canvas-header") {
		t.Fatalf("custom_file_sections forced off: rows %s", rowKeys(v))
	}
	key := "coll:" + fakeslack.StarredCollectionID
	if !hasRow(v, key) {
		t.Fatalf("no starred collection header: %s", rowKeys(v))
	}
	v.rowState(key).row.Click()
	e.frame()
	if !app.FileCollectionCollapsed(fakeslack.StarredCollectionID) || hasRow(v, "empty-collection-"+fakeslack.StarredCollectionID) {
		t.Error("clicking the starred collection's header didn't collapse it")
	}
	v.rowState(key).row.Click()
	e.frame()
	if app.FileCollectionCollapsed(fakeslack.StarredCollectionID) {
		t.Error("a second click didn't open it again")
	}
}

// TestFilesEmptyShots saves the sidebar's empty Starred row in both
// sidebar layouts and the browser's filtered empty state, light and dark
// (VIEWS_SHOTS_DIR, the files-view workflow).
func TestFilesEmptyShots(t *testing.T) {
	if os.Getenv("VIEWS_SHOTS_DIR") == "" {
		t.Skip("VIEWS_SHOTS_DIR not set")
	}
	for _, dark := range []bool{false, true} {
		v, h, e, _ := filesView(t)
		h.th = theme.New(dark)
		app := h.Core()
		app.SetExperimentGroup("unified_files_3p_primary_custom_sections", "on")
		e.until(t, "files.collections.list", func() bool { _, ok := app.FileCollections(); return ok })
		e.frame()
		vaShot(t, e, "files-sidebar-collections", dark)
		app.SetExperimentGroup("unified_files_3p_primary_custom_sections", "off")
		e.frame()
		vaShot(t, e, "files-sidebar-starred", dark)
		for i := range v.typeOn { // every type off: "No files found" and Clear Filters
			v.typeOn[i] = false
		}
		e.frame()
		e.frame()
		vaShot(t, e, "files-empty-filtered", dark)
	}
}
