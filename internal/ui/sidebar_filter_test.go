package ui

import (
	"image"
	"net/url"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// filterEnv boots Clack over a small fake; setup runs on the fake first.
func filterEnv(t *testing.T, setup func(fs *fakeslack.Server)) (*perfEnv, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	if setup != nil {
		setup(fs)
	}
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	e.settle(300 * time.Millisecond)
	return e, fs
}

// filterPoint is inside the sidebar's text filter input.
func filterPoint() image.Point {
	return image.Pt(int(theme.RailW)+8+60, int(theme.TopNavH)+49+14)
}

// typeFilter focuses the input with a click and types s into it.
func (e *perfEnv) typeFilter(t *testing.T, s string) {
	t.Helper()
	e.click(filterPoint())
	e.until("filter focused", 5*time.Second, func() bool { return e.a.sidebar.filter.focused })
	e.typeText(s)
	e.frame()
}

// filterRows is the sidebar's rows as "kind:key".
func filterRows(a *App) []string {
	var out []string
	for _, r := range a.sidebar.rows {
		switch r.kind {
		case sbLink:
			out = append(out, "link:"+r.key)
		case sbHeading:
			out = append(out, "heading:"+r.key)
		case sbConv:
			out = append(out, "conv:"+r.key)
		case sbAction:
			out = append(out, "action:"+r.key)
		case sbNote:
			out = append(out, "note")
		case sbNoResults:
			out = append(out, "noresults")
		}
	}
	return out
}

// wantFilterRows is what Slack's B7kx builds for q over the model: each
// matching section's heading, then its matches (core.FilterSidebar).
func wantFilterRows(a *App, q string, m2, m3 bool) []string {
	m := a.model()
	res := core.FilterSidebar(sbFilterSections(m), q, m2, m3, m.FilterNames)
	var out []string
	for _, s := range res {
		var convs []string
		for _, id := range s.Rows {
			if m.conv(id) != nil {
				convs = append(convs, "conv:"+id)
			}
		}
		if len(convs) > 0 {
			out = append(out, "heading:"+s.ID)
			out = append(out, convs...)
		}
	}
	if len(out) == 0 {
		return []string{"noresults"}
	}
	return out
}

// a channel name fragment that matches something
func someChannelFragment(t *testing.T, a *App) string {
	t.Helper()
	for _, s := range a.model().Sections {
		for _, id := range s.Rows {
			if c := a.model().conv(id); c != nil && c.Kind == "channel" && len(c.Name) > 6 {
				return c.Name[:4]
			}
		}
	}
	t.Fatal("no channel to filter for")
	return ""
}

// sidebar_text_filter: what is typed reaches the rows 150 ms later; the
// rows are the matching sections' headings and matches only (no page
// links, divider or "Add channels"); nothing matching is the no-results
// row, whose "Search {team}" runs the search and clears the filter;
// Escape clears it and keeps "Close / mark as read" away.
func TestSidebarTextFilter(t *testing.T) {
	e, _ := filterEnv(t, nil)
	a := e.a
	if !a.sbFilterOn() {
		t.Fatal("sidebar_text_filter isn't on for the fake account")
	}
	before := filterRows(a)
	if len(before) == 0 || !strings.HasPrefix(before[0], "link:") {
		t.Fatalf("unfiltered sidebar starts %v", before)
	}
	q := someChannelFragment(t, a)
	e.typeFilter(t, q)
	if got := a.sidebar.filter.text; got != q {
		t.Fatalf("input holds %q, want %q", got, q)
	}
	if a.sidebar.fq != "" {
		t.Error("the rows followed the text before the 150 ms debounce")
	}
	e.until("debounced filter", 3*time.Second, func() bool { return a.sidebar.fq == q })
	got, want := filterRows(a), wantFilterRows(a, q, true, true)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("filtered rows:\n got %v\nwant %v", got, want)
	}
	for _, r := range got {
		if strings.HasPrefix(r, "link:") || strings.HasPrefix(r, "action:") {
			t.Errorf("filtered sidebar kept %s", r)
		}
	}
	for _, r := range a.sidebar.rows {
		if r.kind == sbHeading && !r.open {
			t.Errorf("heading %s isn't drawn expanded while filtering", r.key)
		}
	}

	// Escape empties the input (FilterInputBase + the keydown's clear)
	e.key(key.NameEscape, 0)
	e.settle(50 * time.Millisecond)
	if a.sidebar.filter.text != "" || a.sidebar.filter.ed.Text() != "" || a.sidebar.fq != "" {
		t.Errorf("Escape left %q / %q / rows %q", a.sidebar.filter.text, a.sidebar.filter.ed.Text(), a.sidebar.fq)
	}
	if rows := filterRows(a); len(rows) == 0 || !strings.HasPrefix(rows[0], "link:") {
		t.Errorf("clearing didn't bring the whole sidebar back: %v", rows)
	}

	// nothing matches: TextFilterNoResultsRow, and its search handoff
	e.typeFilter(t, "zzqqxxvv")
	e.until("no results", 3*time.Second, func() bool { return a.sidebar.fq == "zzqqxxvv" })
	if got := filterRows(a); strings.Join(got, " ") != "noresults" {
		t.Fatalf("no-match rows %v", got)
	}
	top := int(theme.TopNavH) + 49 + 28 + 10
	btn := image.Pt(int(theme.RailW)+a.sidebar.width/2, top+(130-23-16-28)/2+23+16+14)
	e.click(btn)
	e.settle(100 * time.Millisecond)
	if r := string(a.router.Current()); r != "search?q="+url.QueryEscape("zzqqxxvv") {
		t.Errorf("Search {team} went to %q", r)
	}
	if a.sidebar.filter.text != "" || a.sidebar.fq != "" {
		t.Error("the search handoff didn't clear the filter")
	}
}

// The filter clears when the switcher opens a conversation (pqxE's
// "sidebar-text-filter-reset") and on a Home click while on Home.
func TestSidebarTextFilterResets(t *testing.T) {
	e, _ := filterEnv(t, nil)
	a := e.a
	q := someChannelFragment(t, a)
	e.typeFilter(t, q)
	e.until("debounced filter", 3*time.Second, func() bool { return a.sidebar.fq == q })
	if popups.OnConversationPicked == nil {
		t.Fatal("the switcher has no reset hook")
	}
	popups.OnConversationPicked()
	e.frame()
	if a.sidebar.filter.text != "" || a.sidebar.fq != "" {
		t.Error("a switcher pick kept the filter")
	}
	e.typeFilter(t, q)
	e.until("debounced filter", 3*time.Second, func() bool { return a.sidebar.fq == q })
	e.openChannel(fakeslackMemberChannel(e))
	a.railClick("home", image.Point{})
	e.frame()
	if a.sidebar.filter.text != "" {
		t.Error("Home on Home kept the filter")
	}
}

// Without sidebar_text_filter there's no input: the list starts under the
// header and nothing is filtered; turning it back on brings the input.
func TestSidebarTextFilterFollowsExperiment(t *testing.T) {
	e, _ := filterEnv(t, nil)
	a := e.a
	c := a.Core()
	c.SetExperimentGroup(core.ExpSidebarTextFilter, "off")
	e.settle(400 * time.Millisecond)
	if a.sbFilterOn() {
		t.Fatal("still on after forcing off")
	}
	rows := filterRows(a)
	if len(rows) == 0 || !strings.HasPrefix(rows[0], "link:") {
		t.Fatalf("rows without the filter: %v", rows)
	}
	// the input's spot is the list's first row now (the list starts 6px
	// under the header): a click there opens that page
	e.click(filterPoint())
	e.settle(150 * time.Millisecond)
	if a.sidebar.filter.focused {
		t.Error("the input is there with sidebar_text_filter off")
	}
	if got, want := string(a.router.Current()), strings.TrimPrefix(rows[0], "link:"); got != want {
		t.Errorf("a click under the header opened %q, want the first row's %q", got, want)
	}
	c.SetExperimentGroup(core.ExpSidebarTextFilter, "")
	e.settle(400 * time.Millisecond)
	if a.sbFilterOn() {
		t.Error("not assigned should read as off (Slack's null)")
	}
	c.ClearExperimentGroup(core.ExpSidebarTextFilter)
	e.settle(400 * time.Millisecond)
	q := someChannelFragment(t, a)
	e.typeFilter(t, q)
	e.until("debounced filter", 3*time.Second, func() bool { return a.sidebar.fq == q })
}

// sidebar_text_filter_m2: closed DMs whose person matches join Direct
// messages; without m2 they don't. Focusing the input loads sidebar.dms
// when it hasn't answered yet.
func TestSidebarTextFilterClosedDMs(t *testing.T) {
	const closed = "D0CLOSED0001"
	const user = "U0FAKE00050"
	e, fs := filterEnv(t, func(fs *fakeslack.Server) {
		fs.AddClosedDM(closed, user)
		fs.SetFault("sidebar.dms", fakeslack.Fault{Error: "fatal_error", Times: 1}) // boot's call fails
	})
	a := e.a
	c := a.Core()
	if c.SidebarDMsKnown() {
		t.Fatal("sidebar.dms answered at boot despite the fault")
	}
	n0 := fs.CallCount("sidebar.dms")
	e.click(filterPoint())
	e.until("sidebar.dms on focus", 10*time.Second, func() bool { return c.SidebarDMsKnown() })
	if fs.CallCount("sidebar.dms") != n0+1 {
		t.Errorf("focus made %d sidebar.dms calls", fs.CallCount("sidebar.dms")-n0)
	}
	e.settle(400 * time.Millisecond) // the snapshot with the closed DM
	if e.sidebarHas(closed) {
		t.Fatal("the closed DM is in the sidebar")
	}
	u, ok := c.User(user)
	if !ok || u.Name == "" {
		t.Fatalf("no name for %s", user)
	}
	q := strings.ToLower(strings.Fields(u.Name)[0])
	if len(q) > 6 {
		q = q[:6]
	}
	e.typeText(q)
	e.until("debounced filter", 3*time.Second, func() bool { return a.sidebar.fq == q })
	has := func() bool {
		for _, r := range a.sidebar.rows {
			if r.kind == sbConv && r.key == closed {
				return true
			}
		}
		return false
	}
	if !has() {
		t.Fatalf("m2: the closed DM (%q) didn't match %q: %v", u.Name, q, filterRows(a))
	}
	if got, want := filterRows(a), wantFilterRows(a, q, true, true); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("m2 rows:\n got %v\nwant %v", got, want)
	}
	c.SetExperimentGroup(core.ExpSidebarTextFilterM2, "off")
	e.settle(400 * time.Millisecond)
	if has() {
		t.Error("without m2 the closed DM still matched")
	}
}

// sidebar_text_filter_m3: the fuzzy filter matches a channel by its
// words' initials, which the plain filter (a substring) doesn't.
func TestSidebarTextFilterFuzzy(t *testing.T) {
	e, _ := filterEnv(t, nil)
	a := e.a
	var q, id string
	for _, s := range a.model().Sections {
		for _, cid := range s.Rows {
			c := a.model().conv(cid)
			if c == nil || c.Kind != "channel" || !strings.Contains(c.Name, "-") {
				continue
			}
			parts := strings.Split(c.Name, "-")
			if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
				continue
			}
			ini := parts[0][:1] + parts[1][:1]
			if !strings.Contains(strings.ReplaceAll(c.Name, "-", ""), ini) {
				q, id = ini, cid
				break
			}
		}
		if q != "" {
			break
		}
	}
	if q == "" {
		t.Skip("no channel whose initials aren't a substring")
	}
	matched := func() bool {
		for _, r := range a.sidebar.rows {
			if r.kind == sbConv && r.key == id {
				return true
			}
		}
		return false
	}
	e.typeFilter(t, q)
	e.until("debounced filter", 3*time.Second, func() bool { return a.sidebar.fq == q })
	if !matched() {
		t.Errorf("m3: %q didn't match %s by its initials", q, a.model().conv(id).Name)
	}
	a.Core().SetExperimentGroup(core.ExpSidebarTextFilterM3, "off")
	e.settle(400 * time.Millisecond)
	if matched() {
		t.Errorf("without m3, %q still matched %s", q, a.model().conv(id).Name)
	}
}
