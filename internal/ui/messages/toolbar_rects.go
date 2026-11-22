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

// ListRect is the list's box as the last frame laid it out, in window
// coordinates.
func (v *ChannelView) ListRect() image.Rectangle { return v.s.listRect() }

// ListRect is ChannelView.ListRect for the thread.
func (v *ThreadView) ListRect() image.Rectangle { return v.s.listRect() }

func (s *listState) listRect() image.Rectangle {
	return image.Rectangle{Min: s.origin, Max: s.origin.Add(s.size)}
}

// ReplyBarState is the reply bar of root ts as the last frame drew it: the
// repliers' avatars it showed and whether it drew the "Unread" mark
// (renderUnreadThreadsIndicator). ok is false while no row of ts is laid
// out.
func (v *ChannelView) ReplyBarState(ts string) (avatars int, unread, ok bool) {
	r := v.s.rows[ts]
	if r == nil {
		return 0, false, false
	}
	return r.replyN, r.unreadMark, true
}
