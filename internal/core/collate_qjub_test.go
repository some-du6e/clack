package core

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// qjubNames mixes what Intl.Collator("en-US", {numeric: true}) orders
// differently from byte order: case, digits as numbers, accents,
// punctuation, an empty name, duplicates.
var qjubNames = []string{"a10", "a2", "A2", "a1", "b", "B", "é", "e", "E", "Zed", "zed", "_x", "123", "12", "#x", "-dash",
	"ẞ", "ss", "émile", "emile", "", "Ω", "alpha-9", "alpha-10", "Ünïcode", "unicode", "x y", "x-y", "a2"}

func qjubBuilt() (n int) {
	withQjub(func(*collate.Collator) { n = qjub.built })
	return n
}

// A snapshot of 1700 conversations (300 group DMs, every sort and filter
// path) builds QJub's collator once, ever: collate.New per group DM was
// α.89's 2 s freeze.
func TestQjubCollatorBuiltOnce(t *testing.T) {
	if qjubBuilt() != 1 {
		t.Fatalf("%d collators before the snapshot", qjubBuilt())
	}
	a := bigApp(t)
	s := a.snapshotNow()
	for _, c := range s.Convs {
		a.sortName(c)
	}
	qjubSortStrings(slices.Clone(qjubNames))
	if n := qjubBuilt(); n != 1 {
		t.Errorf("%d collators built, want 1", n)
	}
}

// The shared collator orders exactly as a fresh collate.New did, for
// SortStrings (group DM names, filter names), the sections' row sort and
// the filter's qjubLess.
func TestQjubSortOrderUnchanged(t *testing.T) {
	fresh := func() *collate.Collator { return collate.New(language.AmericanEnglish, collate.Numeric) }
	want := slices.Clone(qjubNames)
	fresh().SortStrings(want)
	got := slices.Clone(qjubNames)
	qjubSortStrings(got)
	if !slices.Equal(got, want) {
		t.Errorf("SortStrings\n got %q\nwant %q", got, want)
	}

	var rows []sortRow
	for i, n := range qjubNames {
		rows = append(rows, sortRow{id: fmt.Sprintf("C%02d", i), name: n, kind: "channel"})
	}
	ref := slices.Clone(rows)
	col := fresh()
	sort.SliceStable(ref, func(i, j int) bool {
		return ref[i].name != ref[j].name && col.CompareString(ref[i].name, ref[j].name) < 0
	})
	var wantIDs []string
	for _, r := range ref {
		wantIDs = append(wantIDs, r.id)
	}
	if gotIDs := sortSectionRows(slices.Clone(rows), false, "alpha", "USELF", store.Prefs{}); !slices.Equal(gotIDs, wantIDs) {
		t.Errorf("sortSectionRows\n got %v\nwant %v", gotIDs, wantIDs)
	}

	withQjub(func(shared *collate.Collator) {
		for _, x := range qjubNames {
			for _, y := range qjubNames {
				if g, w := qjubLess(shared, x, y), qjubLess(col, x, y); g != w {
					t.Errorf("qjubLess(%q, %q) = %v, want %v", x, y, g, w)
				}
			}
		}
	})

	// a group DM's name: its other members' names, sorted and joined
	a := bigApp(t)
	for _, c := range a.snapshotNow().Convs {
		if c.Kind != "mpim" {
			continue
		}
		var names []string
		for _, m := range c.Members {
			if m != "USELF" {
				names = append(names, a.S.UserName(m))
			}
		}
		fresh().SortStrings(names)
		if got, want := a.sortName(c), strings.Join(names, ","); got != want {
			t.Fatalf("sortName(%s) = %q, want %q", c.ID, got, want)
		}
	}
}

// The collator is shared across goroutines (the UI's snapshots, the
// backend's, the plugins' mirror): concurrent sorts agree (run with -race).
func TestQjubConcurrent(t *testing.T) {
	want := slices.Clone(qjubNames)
	collate.New(language.AmericanEnglish, collate.Numeric).SortStrings(want)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				got := slices.Clone(qjubNames)
				qjubSortStrings(got)
				if !slices.Equal(got, want) {
					t.Errorf("concurrent sort %q", got)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// RecentSnapshot builds once and hands the same one out until an event
// that can change the sidebar, InvalidateSnapshot or a second's age.
func TestRecentSnapshotReused(t *testing.T) {
	a, _ := testApp(t)
	close(a.ready)
	b0 := a.SnapshotBuilds()
	s := a.RecentSnapshot()
	for i := 0; i < 10; i++ {
		a.RecentSnapshot()
	}
	if n := a.SnapshotBuilds() - b0; n != 1 {
		t.Fatalf("%d builds for 11 reads, want 1", n)
	}
	if len(s.Convs) != 2 {
		t.Fatalf("%d convs", len(s.Convs))
	}
	builds := func() int64 { return a.SnapshotBuilds() - b0 }

	a.emit(Event{Type: "typing", Channel: "C1"}) // quiet: keeps it
	a.emit(Event{Type: "message", Channel: "C1"})
	a.RecentSnapshot()
	if n := builds(); n != 1 {
		t.Errorf("typing/message rebuilt it: %d builds", n)
	}

	a.S.PutConv(slack.Conversation{ID: "C2", Name: "random", IsChannel: true})
	a.emit(Event{Type: "conv", Channel: "C2"})
	if got := a.RecentSnapshot(); builds() != 2 || len(got.Convs) != 3 {
		t.Errorf("after a conv event: %d builds, %d convs", builds(), len(got.Convs))
	}

	a.InvalidateSnapshot()
	a.RecentSnapshot()
	if n := builds(); n != 3 {
		t.Errorf("after InvalidateSnapshot: %d builds", n)
	}

	a.recent.mu.Lock()
	a.recent.at = a.recent.at.Add(-2 * time.Second)
	a.recent.mu.Unlock()
	a.RecentSnapshot()
	if n := builds(); n != 4 {
		t.Errorf("past its second: %d builds", n)
	}

	// Snapshot always builds a fresh one, as before
	a.Snapshot()
	a.Snapshot()
	if n := builds(); n != 6 {
		t.Errorf("Snapshot: %d builds, want 6", n)
	}
}
