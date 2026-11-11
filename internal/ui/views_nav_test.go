package ui

// The views and navigation experiments in the shell (docs/EXPERIMENTS.md):
// the secondary column's width (secondary_width.go) and the history menu
// (history_menu.go), in every group Slack's code tells apart.

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/views"
)

func expNavApp(t *testing.T, groups map[string]string) (*App, *core.App) {
	t.Helper()
	ca := core.New(slack.New(slack.Creds{Token: "xoxc-test"}), store.New(""))
	for k, v := range groups {
		ca.SetExperimentGroup(k, v)
	}
	a := &App{}
	a.data = &backend{core: ca, app: a}
	a.data.m.Convs = map[string]*convInfo{
		"C1": {ID: "C1", Name: "general", Kind: "channel"},
		"G1": {ID: "G1", Name: "secret", Kind: "private"},
		"D1": {ID: "D1", Name: "Ada Example", Kind: "im", User: "U1"},
	}
	return a, ca
}

// aTtQ hW and the squish (xeD9) in secondaryWidth, at 1px per dp: an
// 1100px panel beside a 260px sidebar.
func TestSecondaryColumnGroups(t *testing.T) {
	gtx := layout.Context{}
	gtx.Metric.PxPerDp = 1
	cases := []struct {
		name   string
		groups map[string]string
		route  Route
		panel  int
		manual int
		want   int
	}{
		// nested + secondary_view_default_width: 40% of the region
		{"default width", map[string]string{"nested_secondary_view": "on", "secondary_view_default_width": "on"}, "c/C1", 1100, 0, 440},
		// nested alone: 30% of the space beside the rail (1360)
		{"nested", map[string]string{"nested_secondary_view": "on", "secondary_view_default_width": ""}, "c/C1", 1100, 0, 408},
		// k5 needs the nested view
		{"width without nested", map[string]string{"nested_secondary_view": "control", "secondary_view_default_width": "on"}, "c/C1", 1100, 0, 408},
		// neither: still 30%, and at least 300
		{"neither", map[string]string{"nested_secondary_view": "", "secondary_view_default_width": ""}, "c/C1", 700, 0, 300},
		// agentic_ia turns the nested view off (kl02 W7)
		{"agentic", map[string]string{"nested_secondary_view": "on", "secondary_view_default_width": "on", "agentic_ia": "on"}, "c/C1", 1100, 0, 408},
		// the squish: a nested column gives way down to 260 beside a 300 primary
		{"nested squish", map[string]string{"nested_secondary_view": "on", "secondary_view_default_width": "on"}, "c/C1", 560, 0, 260},
		// a plain one takes what is left beside 300
		{"plain squish", map[string]string{"nested_secondary_view": ""}, "c/C1", 580, 0, 280},
		// the Activity tab: half the region (Activity has no sidebar)
		{"activity", map[string]string{"nested_secondary_view": "on", "secondary_view_default_width": "on"}, "activity", 1100, 0, 550},
		// a dragged width past the region: nested, the region less the primary's 300
		{"manual nested", map[string]string{"nested_secondary_view": "on"}, "c/C1", 1100, 1400, 800},
		// plain: at most 1000, then the squish
		{"manual plain", map[string]string{"nested_secondary_view": ""}, "c/C1", 1500, 1400, 1000},
	}
	for _, c := range cases {
		a, _ := expNavApp(t, c.groups)
		a.sidebar.width = 260
		a.router.Push(c.route)
		a.secondary.width = c.manual
		if got := a.secondaryWidth(gtx, c.panel); got != c.want {
			t.Errorf("%s: width %d, want %d", c.name, got, c.want)
		}
	}
}

// historyLabels renders HistoryButtons' template.
func historyLabels(items []popups.MenuItem) string {
	var out []string
	for _, it := range items {
		switch {
		case it.Header != "":
			out = append(out, "["+it.Header+"]")
		case it.Separator:
			out = append(out, "---")
		default:
			out = append(out, it.Label)
		}
	}
	return strings.Join(out, "|")
}

// HistoryButtons: "Recent", one row per place, newest first (the current
// one too), 15 places, or 25 with desktop_history_menu, then "Show more"
// when there are more; the empty Home view isn't a place.
func TestHistoryMenuGroups(t *testing.T) {
	for _, g := range []string{"on", "", "control"} {
		a, _ := expNavApp(t, map[string]string{"desktop_history_menu": g})
		for _, r := range []Route{"home", "c/C1", "c/C1/t/1.2", "activity", "c/D1", "c/G1", "threads", "search?q=lunch", "c/C1", "c/UNKNOWN"} {
			a.router.Push(r)
		}
		want := "[Recent]|#general|lunch|All Threads|#secret|Ada Example|Activity"
		if got := historyLabels(a.historyItems()); got != want {
			t.Errorf("%q: %s, want %s", g, got, want)
		}
		for i := range 30 {
			a.router.Push(Route("search?q=s" + strconv.Itoa(i)))
		}
		max := 15
		if g == "on" {
			max = 25
		}
		items := a.historyItems()
		if n := len(items) - 3; n != max || items[len(items)-1].Label != "Show more" || !items[len(items)-2].Separator {
			t.Errorf("%q: %d places (%s), want %d and Show more", g, n, historyLabels(items), max)
		}
		if items[1].Label != "s29" {
			t.Errorf("%q: first place %q", g, items[1].Label)
		}
	}
}

// ModalMoreHistory: every place under its day's {date_pretty} header, a
// separator between days; Clear history empties the recents.
func TestHistoryDialogRows(t *testing.T) {
	a, _ := expNavApp(t, nil)
	t0 := time.Now()
	now := time.Date(t0.Year(), t0.Month(), t0.Day(), 12, 0, 0, 0, t0.Location()) // today, whatever the hour
	day := 24 * time.Hour
	a.router.recents = []visit{
		{Route: "c/C1", At: now},
		{Route: "activity", At: now.Add(-time.Minute)},
		{Route: "c/D1", At: now.Add(-day)},
		{Route: "c/C1/t/1.2", At: now.Add(-day)}, // the same place as c/C1: not again
		{Route: "drafts", At: now.Add(-3 * day)},
	}
	var got []string
	for _, r := range a.historyDialogRows(a.historyPlaces(0)) {
		switch {
		case r.sep:
			got = append(got, "---")
		case r.header != "":
			got = append(got, "["+r.header+"]")
		default:
			got = append(got, r.entry.label)
		}
	}
	old := now.Add(-3 * day)
	want := "[Today]|#general|Activity|---|[Yesterday]|Ada Example|---|[" + old.Format("January ") + strconv.Itoa(old.Day())
	if j := strings.Join(got, "|"); !strings.HasPrefix(j, want) || !strings.HasSuffix(j, "]|Drafts & sent") {
		t.Errorf("rows %s", j)
	}
	a.router.ClearRecents()
	if len(a.historyPlaces(0)) != 0 {
		t.Error("Clear history left places")
	}
}

// The desktop History menu's labels: past 70 characters, the first 70 and
// "...".
func TestHistoryOSLabels(t *testing.T) {
	long := strings.Repeat("é", 71)
	if got := historyOSLabelText(long); got != strings.Repeat("é", 70)+"..." {
		t.Errorf("label %q", got)
	}
	if got := historyOSLabelText("short"); got != "short" {
		t.Errorf("label %q", got)
	}
}

// ⌘/ (Ctrl+/) toggles Slack's Keyboard Shortcuts pane in the secondary
// column; a row with an id runs its shortcut (executeKeyboardShortcut).
func TestKeyboardShortcutsPane(t *testing.T) {
	a, _ := expNavApp(t, nil)
	bound := false
	for _, b := range a.defaultShortcuts().list {
		if b.name == "/" && b.mods == shortcut.Mod() && b.desc == "Keyboard shortcuts" {
			bound = true
		}
	}
	if !bound {
		t.Error("no Mod+/ binding")
	}
	a.showShortcutsHelp() // Help > Keyboard Shortcuts
	if a.secondary.kind != secShortcuts || a.secondary.view == nil {
		t.Fatalf("secondary %v after Keyboard Shortcuts", a.secondary.kind)
	}
	if _, ok := a.secondary.view.(*views.KeyboardShortcuts); !ok {
		t.Errorf("view %T", a.secondary.view)
	}
	a.toggleKeyboardShortcuts()
	if a.secondary.kind != secNone {
		t.Errorf("a second toggle left %v", a.secondary.kind)
	}
	o := a.shortcutListOpts()
	if o.ActivityTab == nil || !o.ActivityTab("activity-inbox") || o.UpToEdit {
		t.Errorf("list opts %+v", o)
	}
	a.RunShortcut("open_all_threads", "")
	if a.router.Current() != "threads" {
		t.Errorf("open_all_threads went to %q", a.router.Current())
	}
}
