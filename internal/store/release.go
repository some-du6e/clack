package store

// Release drops a finished store's entries in the package's maps keyed
// by *Store (listRecordRefsOf, snippetsOf, canvasRefsOf), so the store can
// be collected; core.App.Release calls it. A map of that kind added here
// goes in this list too.
func Release(s *Store) {
	// first: a reader after it keeps nothing, and one in between drops
	// what it stored (the accessors check again after LoadOrStore). A late
	// goroutine's lookup used to put the store back, and with it the
	// session (ci-speed-3: canvasRefsOf and listRecordRefsOf held finished
	// tests' sessions)
	s.released.Store(true)
	listRecordRefsOf.Delete(s)
	snippetsOf.Delete(s)
	canvasRefsOf.Delete(s)
}
