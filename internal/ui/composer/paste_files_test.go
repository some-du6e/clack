package composer

import (
	"testing"
	"time"

	"gioui.org/layout"
)

// A paste whose files go to the composer (filePaste) inserts none of its
// text, as Slack's useUploadPastedFiles preventDefaults it: Finder's ⌘C
// puts the files' names on the pasteboard as plain text. Without files,
// or when the probe answers too late, the text goes in as before.
func TestPasteTextWaitsForFiles(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	at := func(d time.Duration) layout.Context { return layout.Context{Now: t0.Add(d)} }
	names := "notes.txt\rphoto.png"

	// The probe took files: no text, whether it answered before the text
	// arrived or after.
	for _, early := range []bool{true, false} {
		e := NewEditor()
		e.richRead, e.filesRead = &clipRead{done: make(chan struct{})}, newFilePaste()
		close(e.richRead.done)
		if early {
			e.filesRead.decide(true)
		}
		e.receivePaste(at(0), names)
		if !early {
			if e.pendPaste == nil {
				t.Fatal("the text didn't wait for the file probe")
			}
			e.flushPaste(at(50 * time.Millisecond))
			e.filesRead.decide(true)
			e.flushPaste(at(60 * time.Millisecond))
		}
		if !e.doc.Empty() || e.pendPaste != nil || e.filesRead != nil {
			t.Errorf("early %v: files pasted text %q (pending %v)", early, e.doc.Text(), e.pendPaste != nil)
		}
	}

	// No files: the text, after the probe said so.
	e := NewEditor()
	e.filesRead = newFilePaste()
	e.receivePaste(at(0), "just text")
	if !e.doc.Empty() {
		t.Fatal("pasted before the file probe answered")
	}
	e.filesRead.decide(false)
	e.flushPaste(at(10 * time.Millisecond))
	phCheck(t, "no files", e.doc, "p just text")

	// A probe that never answers holds the text for clipFilesWait.
	e = NewEditor()
	e.filesRead = newFilePaste()
	e.receivePaste(at(0), "late")
	e.flushPaste(at(clipFilesWait - time.Millisecond))
	if !e.doc.Empty() {
		t.Fatal("pasted before clipFilesWait")
	}
	e.flushPaste(at(clipFilesWait))
	phCheck(t, "probe timed out", e.doc, "p late")

	// No probe (a field without files, or one already running): at once.
	e = NewEditor()
	e.receivePaste(at(0), "now")
	phCheck(t, "no probe", e.doc, "p now")

	// The first answer wins.
	f := newFilePaste()
	f.decide(true)
	f.decide(false)
	if took, done := f.result(); !took || !done {
		t.Errorf("result %v, %v after decide(true), decide(false)", took, done)
	}
}
