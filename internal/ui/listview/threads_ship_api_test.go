package listview

// The threads-ship test's reads of what the fix added
// (threads_ship_test.go calls only these, so an evidence run on the code
// before can swap them).

// itemCommentsLabel is the comment button's text.
func itemCommentsLabel(it *Item) string {
	it.mu.Lock()
	root, comments := it.croot, it.comments
	it.mu.Unlock()
	l, _ := commentsLabel(root, comments)
	return l
}

// itemComposerShown reports whether the pane draws its composer (Slack's
// input row: the comments reach the thread's end).
func itemComposerShown(it *Item) bool {
	it.mu.Lock()
	defer it.mu.Unlock()
	return !it.commentsShort()
}
