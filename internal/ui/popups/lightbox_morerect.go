package popups

import "image"

// MoreRect is the footer's More actions button as last drawn (window
// coordinates; tests).
func (lb *Lightbox) MoreRect() image.Rectangle { return lb.moreRect }
