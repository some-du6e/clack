package core

import (
	"sync"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// qjub is QJub's sorter, Intl.Collator("en-US", {numeric: true}), built
// once. collate.New matches the locale and builds its tables on every
// call (about a millisecond): sections called it once per group DM, and
// 1665 conversations held the UI goroutine for 2 s in a snapshot (α.89's
// freeze report). A Collator keeps its iterators on itself, so it isn't
// safe for concurrent use: every use holds mu.
var qjub struct {
	mu    sync.Mutex
	col   *collate.Collator
	built int // collators made, for the tests
}

// withQjub runs f with the shared collator, under its lock. f must not
// call withQjub again.
func withQjub(f func(col *collate.Collator)) {
	qjub.mu.Lock()
	defer qjub.mu.Unlock()
	if qjub.col == nil {
		qjub.col = collate.New(language.AmericanEnglish, collate.Numeric)
		qjub.built++
	}
	f(qjub.col)
}

// qjubSortStrings sorts names as QJub's sorter does.
func qjubSortStrings(names []string) {
	if len(names) < 2 {
		return
	}
	withQjub(func(col *collate.Collator) { col.SortStrings(names) })
}
