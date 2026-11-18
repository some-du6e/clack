package messages

import "image"

// RowRect is a message row on screen (its .c-virtual_list__item), in
// window coordinates.
type RowRect struct {
	TS string // the message (a joins summary: its newest message's key)
	R  image.Rectangle
}

// RowRects lists the visible message rows as of the last frame, in
// window coordinates (the same mapping FileRects uses).
func (v *ChannelView) RowRects() []RowRect { return v.s.rowRects() }

// RowRects is ChannelView.RowRects for the thread.
func (v *ThreadView) RowRects() []RowRect { return v.s.rowRects() }

func (s *listState) rowRects() []RowRect {
	var out []RowRect
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].kind != itMsg || i >= len(s.heights) || s.heights[i] <= 0 {
			continue
		}
		y := s.origin.Y + s.pos[i]
		out = append(out, RowRect{TS: s.items[i].key, R: image.Rect(s.origin.X, y, s.origin.X+s.size.X, y+s.heights[i])})
	}
	return out
}

// HoverToolbar is the hover toolbar (MessageActions) the last frame
// drew: the message it was drawn for and its card, in window
// coordinates; ok is false when none was drawn.
func (v *ChannelView) HoverToolbar() (ts string, r image.Rectangle, ok bool) {
	return v.s.hoverToolbar()
}

// HoverToolbar is ChannelView.HoverToolbar for the thread.
func (v *ThreadView) HoverToolbar() (ts string, r image.Rectangle, ok bool) {
	return v.s.hoverToolbar()
}

func (s *listState) hoverToolbar() (string, image.Rectangle, bool) {
	i := s.tbFor
	if i < 0 || i >= len(s.items) {
		return "", image.Rectangle{}, false
	}
	return s.items[i].key, s.tbRect.Add(s.origin), true
}

// ListRect is the list's box as the last frame laid it out, in window
// coordinates.
func (v *ChannelView) ListRect() image.Rectangle { return v.s.listRect() }

// ListRect is ChannelView.ListRect for the thread.
func (v *ThreadView) ListRect() image.Rectangle { return v.s.listRect() }

func (s *listState) listRect() image.Rectangle {
	return image.Rectangle{Min: s.origin, Max: s.origin.Add(s.size)}
}
