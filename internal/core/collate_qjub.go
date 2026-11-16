package core

import (
	"sync"
	"sync/atomic"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// qjub is QJub's sorter, Intl.Collator("en-US", {numeric: true}).
// collate.New matches the locale against every table and sets up a new
// collator on each call (about 3 µs and 10 KB on a CI runner): sortName
// called it per group DM in every snapshot, which α.89's freeze report
// caught on a per-frame snapshot (BenchmarkSortName1700: 1.77 ms to
// 0.77 ms). A Collator keeps its iterators on itself, so it isn't safe
// for concurrent use. They come from a pool, one per user at a time: a
// shared one under a lock made every snapshot's sorts wait for each
// other's, so the UI goroutine's sidebar filter waited on a background
// build's whole section sort.
var qjubPool = sync.Pool{New: func() any {
	qjubMade.Add(1)
	return collate.New(language.AmericanEnglish, collate.Numeric)
}}

// qjubMade counts the collators made, for the tests.
var qjubMade atomic.Int64

// withQjub runs f with a collator of its own (no other goroutine uses it
// meanwhile). f must not keep it.
func withQjub(f func(col *collate.Collator)) {
	col := qjubPool.Get().(*collate.Collator)
	defer qjubPool.Put(col)
	f(col)
}

// qjubSortStrings sorts names as QJub's sorter does.
func qjubSortStrings(names []string) {
	if len(names) < 2 {
		return
	}
	withQjub(func(col *collate.Collator) { col.SortStrings(names) })
}
