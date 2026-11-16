package core

import (
	"slices"
	"testing"
	"time"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/some-du6e/clack/internal/slack"
)

// A stale RecentSnapshot read returns at once with the snapshot it had,
// even while a writer holds the store: the new one is built in the
// background, never on the reader (the UI goroutine, every frame Unreads
// or Activity draws), and the readers get it when it lands.
func TestRecentSnapshotNeverBuildsOnTheReader(t *testing.T) {
	a, _ := testApp(t)
	close(a.ready)
	first := a.RecentSnapshot() // the first one is built on its caller
	a.S.PutConv(slack.Conversation{ID: "C2", Name: "random", IsChannel: true})
	a.emit(Event{Type: "conv", Channel: "C2"})

	a.S.Lock() // a writer: any build waits for it
	got := make(chan Snapshot, 1)
	go func() { got <- a.RecentSnapshot() }()
	select {
	case s := <-got:
		a.S.Unlock()
		if len(s.Convs) != len(first.Convs) {
			t.Errorf("the stale read got %d convs, want the %d it had", len(s.Convs), len(first.Convs))
		}
	case <-time.After(3 * time.Second):
		a.S.Unlock()
		<-got
		t.Fatal("a stale read waited for the store's writer: it built the snapshot on the reader")
	}

	// the background build lands
	dl := time.Now().Add(10 * time.Second)
	for len(a.RecentSnapshot().Convs) != len(first.Convs)+1 {
		if time.Now().After(dl) {
			t.Fatalf("the new conversation never reached RecentSnapshot (%d convs)", len(a.RecentSnapshot().Convs))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// No sort waits for another goroutine's: with one collator shared under
// a lock, the UI goroutine's sidebar filter (and its snapshots) waited
// for a background build's whole section sort.
func TestQjubSortsDontWaitForEachOther(t *testing.T) {
	hold, release := make(chan struct{}), make(chan struct{})
	go withQjub(func(*collate.Collator) {
		close(hold)
		<-release // a long sort on another goroutine
	})
	<-hold
	defer close(release)
	want := slices.Clone(qjubNames)
	collate.New(language.AmericanEnglish, collate.Numeric).SortStrings(want)
	done := make(chan []string, 1)
	go func() {
		names := slices.Clone(qjubNames)
		qjubSortStrings(names)
		done <- names
	}()
	select {
	case got := <-done:
		if !slices.Equal(got, want) {
			t.Errorf("sorted %q\nwant %q", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a sort waited for another goroutine's collator")
	}
}
