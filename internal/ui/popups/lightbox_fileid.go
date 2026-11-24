package popups

// FileID is the file id of the item the viewer shows ("" for an image
// that isn't a file: an unfurl's, an attachment's, an image block's):
// bDav's iR, the current queue item's id, which the Details tab and the
// file's actions go by (tests and the inspect tool).
func (lb *Lightbox) FileID() string {
	if lb == nil || lb.idx < 0 || lb.idx >= len(lb.items) {
		return ""
	}
	if f := lb.cur().file; f != nil {
		return f.ID
	}
	return ""
}
