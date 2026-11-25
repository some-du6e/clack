package canvasview

// The settle tests read the view through these (fields both the old code
// and the fix have).

// previewSettled: the first card's canvas preview is known to have
// nothing (the store answered with no preview).
func previewSettled(v *View) bool {
	b := embedBlk(v)
	return b != nil && b.card.cv.havePrev && len(b.card.cv.preview) == 0
}

// previewShown: the first card's canvas preview has blocks.
func previewShown(v *View) bool {
	b := embedBlk(v)
	return b != nil && b.card.cv.havePrev && len(b.card.cv.preview) > 0
}
