package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// desktop_history_recents_read (Fpm/ Q, on for the account): the history
// menu and the History dialog read the client's recents merged with
// visits.list (qsEE B7). A visit newer than a place's newest recent moves
// the place up; one older changes nothing; a known conversation the
// recents lack is added; an unknown one, a file and anything before Clear
// history are left out. Opening the menu refreshes visits.list unless its
// last answer is under a minute old. Off, the recents alone, as before.
func TestHistoryMenuMergesVisits(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ca := core.New(slack.New(fs.Creds()), store.New(""))
	a := &App{}
	a.data = &backend{core: ca, app: a}
	a.data.m.Convs = map[string]*convInfo{
		"C1": {ID: "C1", Name: "general", Kind: "channel"},
		"G1": {ID: "G1", Name: "secret", Kind: "private"},
		"D1": {ID: "D1", Name: "Ada Example", Kind: "im", User: "U1"},
		"C2": {ID: "C2", Name: "random", Kind: "channel"},
	}
	now := time.Now()
	ms := func(d time.Duration) int64 { return now.Add(d).UnixMilli() }
	a.router.recents = []visit{
		{Route: "c/C1", At: now.Add(-time.Minute)},
		{Route: "activity", At: now.Add(-2 * time.Minute)},
		{Route: "c/G1", At: now.Add(-3 * time.Minute)},
		{Route: "c/D1", At: now.Add(-4 * time.Minute)},
	}
	fs.SetVisits(
		fakeslack.Visit{EntityID: "D1", EntityType: "dm", LastMS: ms(-10 * time.Second)},                     // newer than its recent: first
		fakeslack.Visit{EntityID: "C2", EntityType: "channel", LastMS: ms(-90 * time.Second)},                // not in the recents, known: added
		fakeslack.Visit{EntityID: "C1", EntityType: "channel", LastMS: ms(-10 * time.Minute)},                // older than its recent: no change
		fakeslack.Visit{EntityID: "C9", EntityType: "channel", LastMS: ms(-20 * time.Second)},                // unknown: left out
		fakeslack.Visit{EntityID: "F0FILE0001", EntityType: "file", LastMS: ms(-30 * time.Second)},           // a file: left out
		fakeslack.Visit{EntityID: "G1", EntityType: "thread", ThreadTS: "1.2", LastMS: ms(-5 * time.Second)}, // a thread: its channel
	)
	labels := func() string {
		var out []string
		for _, it := range a.historyItems()[1:] {
			out = append(out, it.Label)
		}
		return strings.Join(out, "|")
	}

	ca.SetExperimentGroup("desktop_history_recents_read", "on")
	if !ca.RefreshHistoryVisits(t.Context()) {
		t.Fatal("RefreshHistoryVisits didn't call visits.list with the experiment on")
	}
	if got, want := labels(), "#secret|Ada Example|#general|#random|Activity"; got != want {
		t.Errorf("merged history %q, want %q", got, want)
	}
	n := len(fs.VisitForms("visits.list"))
	if ca.RefreshHistoryVisits(t.Context()) || len(fs.VisitForms("visits.list")) != n {
		t.Error("a refresh under a minute after the last answer called visits.list again")
	}
	// the dialog reads the same merge
	var rows []string
	for _, r := range a.historyDialogRows(a.historyPlaces(0)) {
		if r.entry != nil {
			rows = append(rows, r.entry.label)
		}
	}
	if got := strings.Join(rows, "|"); got != "#secret|Ada Example|#general|#random|Activity" {
		t.Errorf("History dialog %q", got)
	}
	// Clear history: visits from before it no longer count
	a.router.ClearRecents()
	if got := labels(); got != "" {
		t.Errorf("after Clear history %q, want nothing", got)
	}

	// off: the recents alone
	a.router.recents = []visit{{Route: "c/C1", At: now.Add(-time.Minute)}, {Route: "c/D1", At: now.Add(-4 * time.Minute)}}
	a.router.cleared = time.Time{}
	ca.SetExperimentGroup("desktop_history_recents_read", "off")
	if got := labels(); got != "#general|Ada Example" {
		t.Errorf("off: %q, want the recents alone", got)
	}
	if ca.RefreshHistoryVisits(t.Context()) {
		t.Error("off: RefreshHistoryVisits called visits.list")
	}
}
