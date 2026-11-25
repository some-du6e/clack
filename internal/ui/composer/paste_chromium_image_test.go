package composer

import (
	"testing"
	"time"

	"gioui.org/layout"
)

// A paste of what Slack's desktop app (Chromium's WriteImageWithTag) puts
// on the clipboard for Copy Image, which Clack's Copy Image puts there
// too: the image's first frame, and beside it `<img src alt/>` markup (and
// on Windows and Linux a bookmark of the URL), no text. Slack's composer
// takes the image as the paste's file (useUploadPastedFiles: image.png)
// and inserts nothing: Texty's shouldSkipOnPaste skips a paste with no
// text/plain, and the composer's getPastedText of the markup is "" (an
// <img> has no textContent). Clack's does the same, whether or not the
// file probe took the image.
func TestPasteChromiumImageInsertsNothing(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	at := func(d time.Duration) layout.Context { return layout.Context{Now: t0.Add(d)} }
	const markup = `<img src="https://slack-imgs.com/?c=1&amp;o1=ro&amp;url=https%3A%2F%2Fmedia3.giphy.com%2Fmedia%2Fx%2Fgiphy.webp" alt="A test GIF"/>`
	for _, took := range []bool{true, false} {
		e := NewEditor()
		e.richRead, e.filesRead = &clipRead{done: make(chan struct{}), html: markup}, newFilePaste()
		close(e.richRead.done)
		e.filesRead.decide(took)
		e.receivePaste(at(0), "")
		e.flushPaste(at(10 * time.Millisecond))
		if !e.doc.Empty() || e.pendPaste != nil {
			t.Errorf("file probe took the image %v: the paste inserted %q (pending %v)", took, e.doc.Text(), e.pendPaste != nil)
		}
	}
	// the markup is the HTML read's whole answer: the alt never becomes text
	e := NewEditor()
	e.pasteHTML("", markup)
	if !e.doc.Empty() {
		t.Errorf("the image's markup pasted %q", e.doc.Text())
	}
	if got := attPastedName("image/png"); got != "image.png" {
		t.Errorf("the pasted image is named %q, want Chromium's image.png", got)
	}
}
