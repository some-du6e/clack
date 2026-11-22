package messages

import "weak"

// toolbarHold is MessageActions' popover hold (client-boot-shared RwAt
// setPopoverHold, MessageHover mDIZ setHover): while a popover a toolbar
// button opened is up (the emoji picker from Add reaction…, the More
// actions menu), the row it was opened from stays hovered and keeps its
// toolbar, wherever the pointer is; the hold goes when the popover
// closes. A modal (Forward message…, Add to list…) holds nothing: it
// covers the row until it closes.
//
// The hold reads the popover through a weak pointer: a list that isn't
// laid out again (its channel left by a notification click) never looks
// at its hold, and a strong reference there kept a closed picker, and the
// shared emoji index it holds, past the picker's release.
type toolbarHold struct {
	key  string      // the row's item key
	open func() bool // the popover is still up (false once it's dismissed or gone)
}

// dismissable is a popover the hold can watch.
type dismissable[T any] interface {
	*T
	Dismissed() bool
}

// holdHover holds the toolbar's row (the one drawn hovered this frame)
// while o is open.
func holdHover[T any, P dismissable[T]](s *listState, o P) {
	if o == nil || s.hoverIdx < 0 || s.hoverIdx >= len(s.items) {
		return
	}
	w := weak.Make((*T)(o))
	s.tbHold = toolbarHold{key: s.items[s.hoverIdx].key, open: func() bool {
		p := w.Value()
		return p != nil && !P(p).Dismissed()
	}}
}

// row is the held row's item index in this frame's items, false when
// nothing holds one (the popover closed, or its row is gone).
func (h *toolbarHold) row(s *listState) (int, bool) {
	if h.open == nil {
		return -1, false
	}
	if !h.open() {
		*h = toolbarHold{}
		return -1, false
	}
	i, ok := s.sel.keyIdx[h.key]
	return i, ok
}
