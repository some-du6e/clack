package composer

import (
	"strings"
	"testing"

	"gioui.org/io/key"
)

// A cut, then a paste in the same frame (input sent at once: SendInput,
// AutoHotkey's Send ^x^v, a remote-control tool): the paste is the cut
// text. Gio writes the clipboard only after the frame, so a read started
// in it would take what the clipboard held before; Windows' one-session
// read (pasteSnap) did, and pasted the old clipboard (quote-paste-3's
// review). Here every press is one frame's (layout.Context{}), and the
// system clipboard isn't there at all, so only the editor's own copy can
// paste. Nor does a file probe start for it (EvPasted), which would read
// the old clipboard's files or picture.
func TestPasteInTheFrameOfACut(t *testing.T) {
	e := NewEditor()
	edType(e, "moved words")
	edPress(e, "A", key.ModShortcut)
	edPress(e, "X", key.ModShortcut)
	if got := e.doc.Text(); got != "" {
		t.Fatalf("after the cut the composer holds %q", got)
	}
	e.events = nil
	edPress(e, "V", key.ModShortcut)
	if got := e.doc.Text(); got != "moved words" {
		t.Errorf("a paste in the cut's frame pasted %q, want the cut %q", got, "moved words")
	}
	if edHas(e, EvPasted) {
		t.Error("a file probe started for the editor's own copy")
	}

	// into a quote started in that frame: the quote's line, as Slack's
	// own copy pastes
	e = NewEditor()
	edType(e, "quoted words")
	edPress(e, "A", key.ModShortcut)
	edPress(e, "C", key.ModShortcut)
	edPress(e, key.NameRightArrow, 0)
	e.newline()
	e.ToggleBlock(Quote)
	edPress(e, "V", key.ModShortcut)
	if got := e.doc.Text(); !strings.HasPrefix(got, "quoted words\nquoted words") || len(e.doc.Lines) < 2 || e.doc.Lines[1].Kind != Quote {
		t.Errorf("a copy pasted into a quote in its frame: %q, lines %+v; want the copy on the quote's line", got, e.doc.Lines)
	}
}
