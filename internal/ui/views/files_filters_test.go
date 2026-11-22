package views

import (
	"encoding/json"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// filesAllTypes is All files' default Types selection as type: tokens.
const filesAllTypes = "type:lists type:quip type:documents type:spreadsheets type:presentations type:pdfs type:folder_skill_set"

// filesFiltersPerson is the person the tests pick in From.
const filesFiltersPerson = "U0FAKE00002"

// lastFilesSearch waits for a Files browser search after the first n
// whose query ends with suffix (the list's own, not another part's) and
// returns the newest such.
func lastFilesSearch(t *testing.T, e *vaEnv, fs *fakeslack.Server, n int, suffix string) url.Values {
	t.Helper()
	var got url.Values
	e.until(t, "a new search.modules.files ending "+suffix, func() bool {
		ss := fs.FilesBrowserSearches()
		for i := len(ss) - 1; i >= n; i-- {
			if strings.HasSuffix(ss[i].Get("query"), suffix) {
				got = ss[i]
				return true
			}
		}
		return false
	})
	return got
}

// openFiltersModal clicks the Filters button and returns the modal.
func openFiltersModal(t *testing.T, v *Files, e *vaEnv) *popups.FilesFiltersModal {
	t.Helper()
	before := v.filtersModal
	v.filtersBtn.Click()
	e.frame()
	m := v.filtersModal
	if m == nil || m == before || m.Modal().Dismissed() {
		t.Fatal("the Filters button opened no modal")
	}
	return m
}

// pickFrom types the person's handle into From and picks them from the
// select's options.
func pickFrom(t *testing.T, v *Files, e *vaEnv, m *popups.FilesFiltersModal, id string) {
	t.Helper()
	u, ok := v.h.Core().User(id)
	if !ok {
		t.Fatalf("no user %s", id)
	}
	m.From().Type(u.Name[:min(len(u.Name), 5)])
	var ids []string
	e.until(t, "the person among From's options", func() bool {
		ids = ids[:0]
		for _, o := range m.From().Options() {
			ids = append(ids, o.ID)
			if o.ID == id {
				m.From().Pick(o)
				return true
			}
		}
		return false
	})
	if got := m.From().IDs(); !slices.Equal(got, []string{id}) {
		t.Fatalf("From picked %v (options %v)", got, ids)
	}
}

// pickDate runs the Date menu's row labelled label.
func pickDate(t *testing.T, m *popups.FilesFiltersModal, label string) {
	t.Helper()
	var labels []string
	for _, it := range m.DateItems() {
		labels = append(labels, it.Label)
		if it.Label == label {
			it.Do()
			return
		}
	}
	t.Fatalf("no %q in the Date menu %q", label, labels)
}

// The Filters button opens Slack's advanced filters (BaseAdvancedSearchModal
// for files): a person picked in From and Last 7 days become the browser's
// request as Slack writes it (from:, before:, after:, then the types; the
// first filters sort by Last updated), the button shows "2 Filters" in its
// active fill, the sort select offers Most relevant and Last updated, and
// Clear Filters then Done takes it all back.
func TestFilesViewFilters(t *testing.T) {
	v, _, e, fs := filesView(t)
	if v.filtersLabel() != "" || v.advCount != 0 {
		t.Fatalf("Filters before any: label %q", v.filtersLabel())
	}
	n := len(fs.FilesBrowserSearches())
	m := openFiltersModal(t, v, e)
	if got := m.Modal().Title(); got != "Filter by" {
		t.Errorf("title %q", got)
	}
	// Done is off until something changes (ax); Clear Filters while
	// nothing counts (disableClearBasedOnFilterCount)
	m.Modal().Press("Done")
	if m.Modal().Dismissed() || v.advCount != 0 {
		t.Fatal("Done closed the modal with nothing changed")
	}
	if m.Count() != 0 {
		t.Errorf("count %d with nothing set", m.Count())
	}
	pickFrom(t, v, e, m, filesFiltersPerson)
	pickDate(t, m, "Last 7 days")
	if m.Count() != 2 {
		t.Errorf("count %d, want 2", m.Count())
	}
	e.frame()
	m.Modal().Press("Done")
	if !m.Modal().Dismissed() {
		t.Fatal("Done left the modal open")
	}
	now := time.Now()
	tomorrow, week := now.AddDate(0, 0, 1).Format("2006-01-02"), now.AddDate(0, 0, -7).Format("2006-01-02")
	p := lastFilesSearch(t, e, fs, n, filesAllTypes)
	want := "from:<@" + filesFiltersPerson + "> before:" + tomorrow + " after:" + week + " " + filesAllTypes
	if got := p.Get("query"); got != want {
		t.Errorf("query\n got %q\nwant %q", got, want)
	}
	if p.Get("sort") != "timestamp" || p.Get("module") != "files" || p.Get("page") != "1" {
		t.Errorf("params %v", p)
	}
	// the button: "2 Filters", active (value off its default "")
	if got := v.filtersLabel(); got != "2 Filters" {
		t.Errorf("button label %q, want 2 Filters", got)
	}
	if !v.adv.Set() || v.advCount != 2 {
		t.Errorf("browser filters %+v count %d", v.adv, v.advCount)
	}
	var sorts []string
	for _, o := range v.sortOptions() {
		sorts = append(sorts, o.label)
	}
	if got := strings.Join(sorts, "|"); got != "Most relevant|Last updated" || v.sortLabel() != "Last updated" {
		t.Errorf("sort options %s, label %q", got, v.sortLabel())
	}
	e.frame()

	// reopened, it holds the filters; Clear Filters, then Done: the
	// default request and sort again, the button plain
	n = len(fs.FilesBrowserSearches())
	m = openFiltersModal(t, v, e)
	if got := m.From().IDs(); !slices.Equal(got, []string{filesFiltersPerson}) || m.Count() != 2 {
		t.Fatalf("reopened: From %v, count %d", got, m.Count())
	}
	if got := popups.FilesDateLabel(m.Filters().After, m.Filters().Before, now); got != "Last 7 days" {
		t.Errorf("Date trigger %q", got)
	}
	m.Modal().Press("Clear Filters")
	if m.Modal().Dismissed() || m.Count() != 0 {
		t.Fatalf("Clear Filters: dismissed %v, count %d", m.Modal().Dismissed(), m.Count())
	}
	m.Modal().Press("Done")
	p = lastFilesSearch(t, e, fs, n, filesAllTypes)
	if got := p.Get("query"); got != filesAllTypes || p.Get("sort") != "last_engaged" {
		t.Errorf("cleared: query %q sort %q", got, p.Get("sort"))
	}
	if v.filtersLabel() != "" || v.adv.Set() {
		t.Errorf("cleared: label %q, filters %+v", v.filtersLabel(), v.adv)
	}
}

// Under Created by you the modal has no From (filtersExcluded), and the
// filters stay through the segment change.
func TestFilesViewFiltersCreatedByYou(t *testing.T) {
	v, _, e, fs := filesView(t)
	v.segClicks[1].Click()
	e.frame()
	n := len(fs.FilesBrowserSearches())
	m := openFiltersModal(t, v, e)
	pickDate(t, m, "Yesterday")
	m.Modal().Press("Done")
	now := time.Now()
	p := lastFilesSearch(t, e, fs, n, "creator:<@"+fakeslack.SelfID+">")
	want := "before:" + now.Format("2006-01-02") + " after:" + now.AddDate(0, 0, -2).Format("2006-01-02") +
		" type:lists type:quip type:documents creator:<@" + fakeslack.SelfID + ">"
	if got := p.Get("query"); got != want {
		t.Errorf("query\n got %q\nwant %q", got, want)
	}
	if len(m.Filters().From) != 0 {
		t.Errorf("From under Created by you: %v", m.Filters().From)
	}
	if got := v.filtersLabel(); got != "1 Filter" {
		t.Errorf("label %q", got)
	}
}

// The Date trigger's labels and the menu's selected row, as Slack's
// +hyT and CniK give them (getDatePresetStringValuesMap's ranges).
func TestFilesViewFiltersDates(t *testing.T) {
	now := time.Date(2026, 11, 21, 15, 4, 0, 0, time.Local)
	for _, c := range []struct{ after, before, want string }{
		{"", "", "Any time"},
		{"", "2026-11-22", "Any time"}, // the menu's Any time
		{"2026-11-20", "2026-11-22", "Today"},
		{"2026-11-19", "2026-11-21", "Yesterday"},
		{"2026-11-14", "2026-11-22", "Last 7 days"},
		{"2026-10-22", "2026-11-22", "Last 30 days"},
		{"2026-08-21", "2026-11-22", "Last 3 months"},
		{"2025-11-21", "2026-11-22", "Last 12 months"},
		{"2023-11-21", "2026-11-22", "Last 3 years"},
		{"2023-11-20", "2026-11-23", "Last 3 years"}, // within two days
		{"2026-11-02", "2026-11-04", "On Nov 3, 2026"},
		{"2026-11-02", "", "After Nov 2, 2026"},
		{"", "2026-11-10", "Before Nov 10, 2026"},
		{"2026-11-02", "2026-11-11", "Nov 3 - Nov 10"},
		{"2025-12-30", "2026-01-05", "Dec 31, 2025 - Jan 4"},
	} {
		if got := popups.FilesDateLabel(c.after, c.before, now); got != c.want {
			t.Errorf("after %q before %q: %q, want %q", c.after, c.before, got, c.want)
		}
	}
	// Slack's 3-month preset clamps like dayjs (May 31 less 3 months is
	// Feb 28)
	may31 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.Local)
	if got := popups.FilesDateLabel("2026-02-28", "2026-06-01", may31); got != "Last 3 months" {
		t.Errorf("May 31's 3 months: %q", got)
	}
	f := popups.FilesFilters{From: []string{"U1"}, Before: "2026-11-22"}
	if !f.Set() || f.Count() != 2 || (popups.FilesFilters{}).Set() {
		t.Errorf("Set/Count: %v %d", f.Set(), f.Count())
	}
}

// TestFilesViewFiltersShots saves the Filters modal with a person and a
// date picked, and its Date menu open, light and dark (VIEWS_SHOTS_DIR:
// files-view; FILES_SHOTS_DIR: files-shots).
func TestFilesViewFiltersShots(t *testing.T) {
	if os.Getenv("VIEWS_SHOTS_DIR") == "" && os.Getenv("FILES_SHOTS_DIR") == "" {
		t.Skip("VIEWS_SHOTS_DIR / FILES_SHOTS_DIR not set")
	}
	for _, dark := range []bool{false, true} {
		v, h, e, _ := filesView(t)
		h.th = theme.New(dark)
		e.until(t, "the templates", func() bool { return len(v.templateItems()) == 3 })
		m := openFiltersModal(t, v, e)
		e.frame()
		shot := func(name string) {
			vaShotTo(t, e, "VIEWS_SHOTS_DIR", name, dark)
			vaShotTo(t, e, "FILES_SHOTS_DIR", name, dark)
		}
		shot("files-filters-empty")
		pickFrom(t, v, e, m, filesFiltersPerson)
		pickDate(t, m, "Last 30 days")
		e.frame()
		shot("files-filters-open")
		m.OpenDateMenu()
		e.frame()
		shot("files-filters-date-menu")
		for _, o := range h.open() {
			if mm, ok := o.(*popups.Menu); ok {
				mm.Dismiss()
			}
		}
		m.Modal().Press("Done")
		e.frame()
		e.frame()
		shot("files-filters-applied")
	}
}

// The modal's count is BaseAdvancedSearchModal's aI: with the
// search_exclude_bots pref on, excludeBots counts too, so a date alone
// makes "2 Filters" and Clear Filters is on from the start; Clear Filters
// turns it off for this save (the pref stays).
func TestFilesViewFiltersExcludeBotsCount(t *testing.T) {
	v, h, e, _ := filesView(t)
	h.Core().S.SetRawPrefs(map[string]json.RawMessage{"search_exclude_bots": json.RawMessage("true")})
	m := openFiltersModal(t, v, e)
	if m.Count() != 1 {
		t.Fatalf("count %d with the pref on, want 1", m.Count())
	}
	pickDate(t, m, "Today")
	m.Modal().Press("Done")
	e.frame()
	if got := v.filtersLabel(); got != "2 Filters" || v.adv.Count() != 1 {
		t.Errorf("label %q, filters %+v", got, v.adv)
	}
	m = openFiltersModal(t, v, e)
	m.Modal().Press("Clear Filters")
	m.Modal().Press("Done")
	e.frame()
	if got := v.filtersLabel(); got != "" || v.adv.Set() {
		t.Errorf("cleared: label %q, filters %+v", got, v.adv)
	}
	if !h.Core().PrefBool("search_exclude_bots", false) {
		t.Error("Clear Filters changed the pref")
	}
}
