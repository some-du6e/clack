package messages

import (
	"image"
	"time"

	"gioui.org/layout"
)

// ChannelView is a conversation's message list, anchored at the bottom.
// The ui root embeds the composer below it.
type ChannelView struct {
	h  Host
	ch string
	s  *listState

	// Origin is the view's top-left in window coordinates, used for popup
	// anchors when the Host doesn't implement PointerHost.
	Origin image.Point
}

// NewChannelView returns the view for channelID. Views are cheap: rows,
// loaded history and scroll position are cached per channel, so creating a
// view for a channel seen before is instant.
func NewChannelView(h Host, channelID string) *ChannelView {
	v := &ChannelView{h: h, ch: channelID, s: stateFor(h, channelID, "")}
	v.s.hb.ensureLoaded(v.s.m)
	return v
}

// Channel returns the conversation id.
func (v *ChannelView) Channel() string { return v.ch }

// Layout draws the list filling the constraints.
func (v *ChannelView) Layout(gtx layout.Context) layout.Dimensions {
	return v.s.layoutList(gtx, v.h, v.Origin)
}

// Typing returns the "X is typing…" text for this channel ("" if nobody).
func (v *ChannelView) Typing() string { return typingText(v.s.snap.typing) }

// ScrollToBottom jumps to the newest message (e.g. after sending).
func (v *ChannelView) ScrollToBottom() { v.s.list.Position = layout.Position{} }

// AtBottom reports whether the newest message is visible.
func (v *ChannelView) AtBottom() bool { return !v.s.list.Position.BeforeEnd }

// Highlight scrolls to ts and flashes it (permalinks, search results).
func (v *ChannelView) Highlight(ts string) {
	v.s.highlight, v.s.highlightAt = ts, time.Now()
	v.s.positioned = true
	for i, it := range v.s.items {
		if it.kind == itMsg && v.s.snap.msgs[it.idx].m.TS == ts {
			v.s.list.Position = layout.Position{BeforeEnd: true, First: max(0, i-2)}
			return
		}
	}
	v.s.pendingJump = ts
}

// Refresh refetches the newest messages in the background.
func (v *ChannelView) Refresh() {
	v.s.m.mu.Lock()
	v.s.m.fetched = time.Time{}
	v.s.m.mu.Unlock()
	v.s.hb.ensureLoaded(v.s.m)
}
