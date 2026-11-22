package composer

import "testing"

// The fallback file browser counts a double click only on one entry: a
// folder opens on its first click, so a double click's second click lands
// on whatever entry the folder's listing (or a list that scrolled) puts
// under the pointer, and the window counts that click as 2. It used to
// attach that file and close the dialog.
func TestAttBrowserDoubleClickNeedsOneEntry(t *testing.T) {
	b := &attBrowser{sel: map[string]bool{}}
	b.entries = []attEntry{{name: "sub", path: "/x/sub", dir: true}}
	if b.doubleClicked(0, 1) {
		t.Error("a first click counted as a double click")
	}
	b.entries = []attEntry{{name: "a.txt", path: "/x/sub/a.txt"}} // the folder's listing
	if b.doubleClicked(0, 2) {
		t.Error("the folder's second click, on the listing's file, double-clicked the file")
	}
	// a double click on the file itself
	if b.doubleClicked(0, 1) || !b.doubleClicked(0, 2) {
		t.Error("a double click on one file didn't count")
	}
}
