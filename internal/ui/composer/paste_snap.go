package composer

// pasteSnap is one paste's read of the clipboard where the platform reads
// all of it at once (Windows: one session, winclip.Read, for the text,
// the rich text, the files and the picture), so each comes from the same
// copy; elsewhere startPasteSnap is nil and the paste reads each its own
// way (Gio's text, startClipHTML, the file probe). Windows' clipboard is
// one global resource: one paste's several sessions let another app's
// copy land between them, and two at once let one's close free the data
// the other was reading (Karim's 2026-11-21 crash, CLACK_PATCHES.md §62).
type pasteSnap struct {
	done chan struct{}
	text string    // CF_UNICODETEXT ("" with none)
	rich *clipRead // the rich text, done with the snapshot (nil for a plain paste)
	// the file probe's answer: files copied in Explorer, else the picture
	files   []string
	img     []byte
	imgMime string
	imgErr  error // errAttNoClipImage with none
}

// ready reports whether the snapshot was read.
func (s *pasteSnap) ready() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}
