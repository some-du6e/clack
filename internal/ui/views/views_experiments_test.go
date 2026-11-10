package views

// The views and navigation experiments (core/views_experiments.go,
// docs/EXPERIMENTS.md), each group Slack's code tells apart, on
// internal/fakeslack's views fixtures.

import (
	"image"
	"strings"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ThreadsViewHeader (client-threads-view auBp): with nested_secondary_view
// the header always has its inset line; without it there is none until,
// with view_header_overflow_borders, the list has scrolled more than 15px
// under it. Without either the line never shows.
func TestThreadsHeaderGroups(t *testing.T) {
	app, _ := vaFake(t)
	v := NewThreads(&slickHost{app: app, th: theme.New(false)})
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	e.until(t, "the followed threads", func() bool { p, ok, _ := v.data.get(); return ok && len(p.items) == 2 })
	gtx := layout.Context{}
	gtx.Metric.PxPerDp = 1
	cases := []struct {
		nested, overflow string
		first, offset    int
		want             int
	}{
		{"on", "on", 0, 0, thrBorderInset},
		{"on", "", 1, 0, thrBorderInset},
		{"", "on", 0, 0, thrBorderNone},
		{"", "on", 0, 15, thrBorderNone}, // isScrolled is scrollTop > 15
		{"", "on", 0, 16, thrBorderBelow},
		{"", "on", 1, 0, thrBorderBelow},
		{"", "", 2, 40, thrBorderNone}, // maybeSetIsScrolled does nothing without the experiment
		{"control", "off", 1, 0, thrBorderNone},
	}
	for _, c := range cases {
		app.SetExperimentGroup("nested_secondary_view", c.nested)
		app.SetExperimentGroup("view_header_overflow_borders", c.overflow)
		v.list.Position = layout.Position{First: c.first, Offset: c.offset}
		if got := v.headBorder(gtx); got != c.want {
			t.Errorf("nested %q, overflow %q, scrolled %d/%d: border %d, want %d", c.nested, c.overflow, c.first, c.offset, got, c.want)
		}
	}
	// agentic_ia turns the nested view off (kl02 W7)
	app.SetExperimentGroup("nested_secondary_view", "on")
	app.SetExperimentGroup("agentic_ia", "on")
	v.list.Position = layout.Position{}
	if got := v.headBorder(gtx); got != thrBorderNone {
		t.Errorf("under agentic_ia: border %d, want none", got)
	}
	app.ClearExperimentGroup("agentic_ia")
	// and both groups draw
	for _, g := range []string{"on", ""} {
		app.SetExperimentGroup("nested_secondary_view", g)
		e.frame()
		vaShot(t, e, "threads-header-nested-"+map[string]string{"on": "on", "": "off"}[g], false)
	}
}

// The Keyboard Shortcuts pane: with keyboard_shortcuts_search its search
// field filters the rows by title and a search with none shows one of
// Slack's three empty states; without it there is no field and the list
// stays whole whatever was typed.
func TestKeyboardShortcutsSearchGroups(t *testing.T) {
	app, _ := vaFake(t)
	opts := func() shortcut.ListOpts {
		return shortcut.ListOpts{GOOS: "darwin", KeyboardLocale: "us", ActivityLabel: "Activity", LaterTodos: true}
	}
	for _, g := range []string{"on", ""} {
		app.SetExperimentGroup("keyboard_shortcuts_search", g)
		v := NewKeyboardShortcuts(&slickHost{app: app, th: theme.New(false)}, opts)
		e := &vaEnv{size: image.Pt(420, 900)}
		e.draw = func(gtx layout.Context) { v.Layout(gtx) }
		e.frame()
		all := v.Rows()
		if all[0] != "(subtitle)" || all[1] != "## Basics" {
			t.Fatalf("%q: rows start %v", g, all[:2])
		}
		vaShot(t, e, "keyboard-shortcuts-search-"+map[string]string{"on": "on", "": "off"}[g], false)
		v.SetQuery("thread")
		e.frame()
		got := v.Rows()
		if g == "" {
			if len(got) != len(all) {
				t.Errorf("without the experiment the query filtered: %d rows of %d", len(got), len(all))
			}
			continue
		}
		if len(got) >= len(all) || len(got) < 3 {
			t.Fatalf("filtered rows %v", got)
		}
		for _, r := range got {
			if strings.HasPrefix(r, "(") || strings.HasPrefix(r, "## ") {
				continue
			}
			if !strings.Contains(strings.ToLower(r[:strings.Index(r, " = ")]), "thread") {
				t.Errorf("%q kept for thread", r)
			}
		}
		vaShot(t, e, "keyboard-shortcuts-search-thread", false)
		v.SetQuery("zzzz")
		e.frame()
		if got := v.Rows(); len(got) != 1 || !strings.HasPrefix(got[0], "(empty ") {
			t.Errorf("no match: %v", got)
		}
		vaShot(t, e, "keyboard-shortcuts-search-empty", false)
	}
}
