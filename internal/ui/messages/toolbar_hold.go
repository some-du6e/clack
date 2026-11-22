package messages

import "github.com/some-du6e/clack/internal/ui/popups"

// toolbarHold is MessageActions' popover hold (client-boot-shared RwAt
// setPopoverHold, MessageHover mDIZ setHover): while a popover a toolbar
// button opened is up (the emoji picker from Add reaction…, the More
// actions menu), the row it was opened from stays hovered and keeps its
// toolbar, wherever the pointer is; the hold goes when the popover
// closes. A modal (Forward message…, Add to list…) holds nothing: it
// covers the row until it closes.
type toolbarHold struct {
	key string // the row's item key
	o   popups.Overlay
}

// holdHover holds the toolbar's row (the one drawn hovered this frame)
// while o is open.
func (s *listState) holdHover(o popups.Overlay) {
	if o == nil || s.hoverIdx < 0 || s.hoverIdx >= len(s.items) {
		return
	}
	s.tbHold = toolbarHold{key: s.items[s.hoverIdx].key, o: o}
}

// row is the held row's item index in this frame's items, false when
// nothing holds one (the popover closed, or its row is gone).
func (h *toolbarHold) row(s *listState) (int, bool) {
	if h.o == nil {
		return -1, false
	}
	if h.o.Dismissed() {
		*h = toolbarHold{}
		return -1, false
	}
	i, ok := s.sel.keyIdx[h.key]
	return i, ok
}
