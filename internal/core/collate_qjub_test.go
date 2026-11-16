package core

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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

// A snapshot of 1700 conversations (300 group DMs, every sort and filter
// path) reuses QJub's collators: collate.New per group DM was α.89's 2 s
// freeze. The pool makes one per goroutine using one at a time, and again
// only when a goroutine moved to another processor or a collection
// emptied it, so a handful at most.
func TestQjubCollatorBuiltOnce(t *testing.T) {
	a := bigApp(t)
	n0 := qjubMade.Load()
	s := a.snapshotNow()
	for _, c := range s.Convs {
		a.sortName(c)
	}
	qjubSortStrings(slices.Clone(qjubNames))
	if n := qjubMade.Load() - n0; n > 8 {
		t.Errorf("%d collators built for one snapshot and %d group DM names, want a handful", n, len(s.Convs))
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

// RecentSnapshot builds the first one on its caller, then hands the
// newest one out: an event that can change it, or InvalidateSnapshot,
// starts one build in the background, which wakes the window when it
// lands; until then the reader keeps the one it had. Events about what it
// doesn't hold keep it, and it never expires with time.
func TestRecentSnapshotReused(t *testing.T) {
	a, _ := testApp(t)
	close(a.ready)
	old := recentRebuildGap
	recentRebuildGap = 0
	t.Cleanup(func() { recentRebuildGap = old })
	var wakes atomic.Int32
	a.SetSnapshotWake(func() { wakes.Add(1) })
	b0 := a.SnapshotBuilds()
	builds := func() int64 { return a.SnapshotBuilds() - b0 }
	s := a.RecentSnapshot()
	for i := 0; i < 10; i++ {
		a.RecentSnapshot()
	}
	if n := builds(); n != 1 {
		t.Fatalf("%d builds for 11 reads, want 1", n)
	}
	if len(s.Convs) != 2 {
		t.Fatalf("%d convs", len(s.Convs))
	}
	settled := func(what string, want int64) {
		t.Helper()
		for dl := time.Now().Add(5 * time.Second); time.Now().Before(dl); time.Sleep(time.Millisecond) {
			a.RecentSnapshot()
			a.recent.mu.Lock()
			fresh, running := a.recent.ok && a.recent.built == a.recent.gen, a.recent.running
			a.recent.mu.Unlock()
			if fresh && !running {
				break
			}
		}
		if n := builds(); n != want {
			t.Errorf("%s: %d builds, want %d", what, n, want)
		}
	}

	// quiet: typing, a message, a profile change, a lookup and a new
	// emoji about nothing it holds, a channel the viewer isn't in
	a.emit(Event{Type: "typing", Channel: "C1"})
	a.emit(Event{Type: "message", Channel: "C1"})
	a.emit(Event{Type: "user", UserID: "U9STRANGER"})
	a.emit(Event{Type: "names", Users: []string{"U9STRANGER", "C9ELSEWHERE"}})
	a.emit(Event{Type: "emoji_changed", Name: "partyparrot"})
	a.emit(Event{Type: "conv", Channel: "C9ELSEWHERE", Conv: &Conv{ID: "C9ELSEWHERE", Preview: true}})
	a.emit(Event{Type: "badge_counts_updated"})
	a.RecentSnapshot()
	if n := builds(); n != 1 {
		t.Errorf("quiet events rebuilt it: %d builds", n)
	}
	// a DM partner's profile isn't quiet
	a.emit(Event{Type: "user", UserID: "U1"})
	settled("after the DM partner's profile changed", 2)

	a.S.PutConv(slack.Conversation{ID: "C2", Name: "random", IsChannel: true})
	w := wakes.Load()
	a.emit(Event{Type: "conv", Channel: "C2"})
	if got := a.RecentSnapshot(); len(got.Convs) != 2 {
		t.Errorf("the read after a conv event got %d convs: built on the caller, want the old one", len(got.Convs))
	}
	settled("after a conv event", 3)
	if got := a.RecentSnapshot(); len(got.Convs) != 3 {
		t.Errorf("after the rebuild: %d convs, want 3", len(got.Convs))
	}
	if wakes.Load() == w {
		t.Error("the rebuild didn't wake the window")
	}

	a.InvalidateSnapshot()
	settled("after InvalidateSnapshot", 4)

	time.Sleep(1200 * time.Millisecond) // older than the old 1 s expiry
	a.RecentSnapshot()
	time.Sleep(50 * time.Millisecond)
	if n := builds(); n != 4 {
		t.Errorf("rebuilt with nothing changed after 1.2 s: %d builds", n)
	}

	// Snapshot always builds a fresh one, and its build is the readers'
	a.S.PutConv(slack.Conversation{ID: "C3", Name: "third", IsChannel: true})
	a.emit(Event{Type: "conv", Channel: "C3"})
	a.Snapshot()
	if n := builds(); n != 5 {
		t.Errorf("Snapshot: %d builds, want 5", n)
	}
	if got := a.RecentSnapshot(); len(got.Convs) != 4 {
		t.Errorf("after Snapshot built one: RecentSnapshot has %d convs, want 4", len(got.Convs))
	}
	time.Sleep(50 * time.Millisecond)
	if n := builds(); n != 5 {
		t.Errorf("RecentSnapshot rebuilt what Snapshot had just built: %d builds", n)
	}
}
