package store

// Release drops a finished store's entries in the package's maps keyed
// by *Store (listRecordRefsOf, snippetsOf, canvasRefsOf), so the store can
// be collected; core.App.Release calls it. A map of that kind added here
// goes in this list too.
func Release(s *Store) {
	listRecordRefsOf.Delete(s)
	snippetsOf.Delete(s)
	canvasRefsOf.Delete(s)
}
