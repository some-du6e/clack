package richview

// The option the search tests (overlimit_search_test.go) set: one place
// for the red tree to stand in for.

// noAttachmentLimit is o with every attachment drawn (a search result's).
func noAttachmentLimit(o Options) Options {
	o.NoAttachmentLimit = true
	return o
}
