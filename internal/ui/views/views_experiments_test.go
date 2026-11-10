package views

// The views and navigation experiments (core/views_experiments.go,
// docs/EXPERIMENTS.md), each group Slack's code tells apart, on
// internal/fakeslack's views fixtures.

import (
	"image"
	"testing"

	"gioui.org/layout"

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
