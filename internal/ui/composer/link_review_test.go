package composer

import (
	"image"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"gioui.org/io/key"
)

// The light review of composer-link-edit (bbd0d209, 2026-11-23): three
// blockers, through real input on the real composer (lkEnv).
//
//  1. The edit-link popover takes the key focus, as Slack's coachmark
//     does (DeprecatedCoachmarkCloseButton focuses its button on mount:
//     Remove): typing over it doesn't replace the selected link, Enter
//     presses Remove, Shift+Tab reaches Edit, Escape closes it.
//  2. A popover whose link went away (the message sent, the text edited
//     under it) closes; it doesn't hide and keep eating keys and clicks.
//  3. The link dialog acts on its range as it is when it saves: a pasted
//     URL's slug answer landing meanwhile moves the text, and Save links
//     the words it was opened over.

// typeOver types s over the editor's selection, as the window sends it
// to whatever has the key focus: an EditEvent, then the selection after.
func (e *lkEnv) typeOver(s string) {
	a, b := e.c.ed.Selection()
	lo, hi := min(a, b), max(a, b)
	n := lo + len([]rune(s))
	e.r.Queue(key.EditEvent{Range: key.Range{Start: lo, End: hi}, Text: s}, key.SelectionEvent{Start: n, End: n})
	e.frame()
	e.frame()
}

// freshClick clicks p a second past the last click, so no double-click
// window (Clack's 500 ms) joins them.
func (e *lkEnv) freshClick(p image.Point) {
	e.ptr += time.Second
	e.click(p)
}

// popover is the last popover, or the test's end.
func (e *lkEnv) popover(what string) *linkPopover {
	e.t.Helper()
	if e.pop == nil {
		e.t.Fatalf("%s opened no popover", what)
	}
	return e.pop
}

func TestLinkPopoverHoldsTheKeys(t *testing.T) {
	e := newLinkEnv(t)
	e.setDoc(lkDoc())
	e.freshClick(e.runeAt(7))
	p := e.popover("a click in the link")
	caret := p.initHi
	e.typeOver("s")
	e.key(key.NameDeleteBackward, 0)
	if d := e.c.ed.Doc(); d.Text() != "see the docs now" || !slices.Equal(linkRuns(d), []string{"the docs→" + lkURL}) {
		t.Fatalf("typing with the popover up changed the draft: %q %v", d.Text(), linkRuns(d))
	}
	// Enter: the focused Remove
	e.key(key.NameReturn, 0)
	if d := e.c.ed.Doc(); d.Text() != "see the docs now" || len(linkRuns(d)) != 0 {
		t.Fatalf("Enter on the popover: %q %v, want the link removed", d.Text(), linkRuns(d))
	}
	if !p.Dismissed() {
		t.Error("the popover is still open after Remove")
	}
	if lo, hi := e.c.ed.Selection(); lo != caret || hi != caret {
		t.Errorf("caret %d-%d, want where the click left it (%d)", lo, hi, caret)
	}

	// Ctrl+Shift+U with the caret in a link: the popover, whose Shift+Tab
	// from Remove is Edit, and Enter there opens the dialog
	e.setDoc(lkDoc())
	e.freshClick(e.runeAt(1))
	e.c.ed.SetSelection(7, 7)
	e.pop, e.dlg = nil, nil
	e.key("U", key.ModShortcut|key.ModShift)
	if e.pop == nil {
		t.Fatal("Ctrl+Shift+U in a link opened no popover")
	}
	e.key(key.NameTab, key.ModShift)
	e.key(key.NameReturn, 0)
	if e.dlg == nil || e.dlg.link.Text() != lkURL {
		t.Fatalf("Shift+Tab, Enter: dialog %+v, want Edit's", e.dlg)
	}
	e.dlg.modal.Dismiss()
	e.frame()

	// Escape closes it and puts the click's caret back
	e.setDoc(lkDoc())
	e.pop = nil
	e.freshClick(e.runeAt(7))
	p = e.popover("a click in the link")
	at := p.initHi
	e.key(key.NameEscape, 0)
	if !p.Dismissed() {
		t.Fatal("Escape left the popover open")
	}
	if lo, hi := e.c.ed.Selection(); lo != at || hi != at {
		t.Errorf("Escape: selection %d-%d, want the click's caret %d", lo, hi, at)
	}
}

func TestLinkPopoverClosesWithItsText(t *testing.T) {
	e := newLinkEnv(t)
	e.setDoc(lkDoc())
	e.freshClick(e.runeAt(7))
	p := e.popover("a click in the link")
	e.c.submit() // the draft goes, the link with it
	e.frame()
	e.frame()
	if !p.Dismissed() {
		t.Error("the popover outlived its link (sent)")
	}

	e.setDoc(lkDoc())
	e.pop = nil
	e.freshClick(e.runeAt(7))
	p = e.popover("a click in the link")
	d := e.c.ed.Doc()
	e.c.ed.SetSelection(d.Len(), d.Len())
	e.c.ed.InsertText("!") // the text changed under it
	e.frame()
	e.frame()
	if !p.Dismissed() {
		t.Error("the popover stayed open over a changed document")
	}
}

func TestLinkDialogFollowsSlugAnswer(t *testing.T) {
	e := newLinkEnv(t)
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	e.fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method == "chat.slugifyUrl" {
			<-release // the answer waits until the dialog is open
		}
		return nil, false
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	d := NewDoc()
	d.Insert(0, " docs here", Attr{})
	e.setDoc(d)
	e.freshClick(e.runeAt(8)) // the editor focused
	e.c.ed.SetSelection(0, 0)
	e.c.PasteText(lsURL)
	e.frame()
	n := len([]rune(lsURL))
	if got := e.c.ed.Doc().Text(); got != lsURL+" docs here" {
		t.Fatalf("after the paste: %q", got)
	}
	e.c.ed.SetSelection(n+1, n+5) // "docs"
	e.key("U", key.ModShortcut|key.ModShift)
	if e.dlg == nil || e.dlg.text.Text() != "docs" {
		t.Fatalf("dialog %+v", e.dlg)
	}
	e.fill(nil, sp("example.com/docs"))
	free()
	waitUntil(t, "the slug's answer", func() bool {
		e.frame()
		runRestores()
		a := e.c.ed.Doc().Attrs
		return len(a) > 0 && a[0].Atom != nil && a[0].Atom.Kind == AtomLink
	})
	e.save()
	got := e.c.ed.Doc()
	if got.Len() != 11 || got.Attrs[0].Atom == nil || got.String(1, got.Len()) != " docs here" {
		t.Fatalf("after Save: %q (%d runes), want the slug, then \" docs here\"", got.Text(), got.Len())
	}
	if !slices.Equal(linkRuns(got)[1:], []string{"docs→http://example.com/docs"}) {
		t.Errorf("links %v, want the slug and \"docs\" linked where it is now", linkRuns(got))
	}
}
