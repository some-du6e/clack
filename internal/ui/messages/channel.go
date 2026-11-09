package messages

import (
	"hash/fnv"
	"image"
	"strconv"
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
	// Preview disables marking the channel read (channel previews,
	// unjoined channels).
	Preview bool
}

// NewChannelView returns the view for channelID. Views are cheap: rows,
// loaded history and scroll position are cached per channel, so creating a
// view for a channel seen before is instant.
func NewChannelView(h Host, channelID string) *ChannelView {
	v := &ChannelView{h: h, ch: channelID, s: stateFor(h, channelID, "")}
	v.s.hb.ensureLoaded(v.s.m)
	v.s.closeEdits() // an editor left open when you navigated away is gone, like Slack
	// Each open places the "New" line at the read cursor as it is now.
	if app := h.Core(); app != nil {
		cnt := app.Count(channelID)
		v.s.openLastRead, v.s.openUnread = cnt.LastRead, cnt.Unread
		v.s.opened = true
		v.s.built = 0
	}
	return v
}

// Channel returns the conversation id.
func (v *ChannelView) Channel() string { return v.ch }

// Layout draws the list filling the constraints.
func (v *ChannelView) Layout(gtx layout.Context) layout.Dimensions {
	v.s.noMark = v.Preview
	shown(&lastChannel, v.s)
	st := paneClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	if st.Active() {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return v.s.layoutList(gtx, v.h, v.Origin) })
	}
	return v.s.layoutList(gtx, v.h, v.Origin)
}

// Leave closes any inline editor (the ui root may call it when navigating
// away; switching to another conversation does it automatically).
func (v *ChannelView) Leave() { v.s.closeEdits() }

// lastChannel/lastThread are the conversations drawn most recently; when
// another one is drawn, the previous one was left and its inline editor is
// discarded, like Slack (views themselves are cached by the ui root).
var lastChannel, lastThread *listState

func shown(last **listState, s *listState) {
	if *last != s {
		if *last != nil {
			(*last).closeEdits()
		}
		*last = s
	}
}

// EditLast starts inline editing of the viewer's most recent message here
// (the composer's Up-arrow hook, composer.OnEditLast). It reports whether
// there was one.
func (v *ChannelView) EditLast() bool { return v.s.editLast(v.h) }

// Edit starts inline editing of message ts (message menu "Edit message").
func (v *ChannelView) Edit(ts string) { v.s.startEdit(v.h, ts) }

// Typing returns the "X is typing…" text for this channel ("" if nobody).
func (v *ChannelView) Typing() string { return typingText(v.s.snap.typing) }

// ScrollToBottom jumps to the newest message (e.g. after sending).
func (v *ChannelView) ScrollToBottom() { v.s.list.Position = layout.Position{} }

// AtBottom reports whether the newest message is visible.
func (v *ChannelView) AtBottom() bool { return !v.s.list.Position.BeforeEnd }

// Highlight scrolls to ts and flashes it (permalinks, search results,
// route "c/<id>/m/<ts>"). Older messages are paged in until ts is loaded.
func (v *ChannelView) Highlight(ts string) {
	v.s.highlight, v.s.highlightAt = ts, time.Now()
	v.s.positioned = true
	v.s.jumpPages = 0
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

// LayoutSig describes what the list drew in its last layout, for the ui
// root's startup timeline: how many messages it holds, whether the first
// page arrived, and a hash of the visible rows' keys, positions and
// heights, which changes whenever a painted row moves or resizes.
func (v *ChannelView) LayoutSig() (msgs int, loaded bool, sig uint64) {
	s := v.s
	h := fnv.New64a()
	var buf []byte
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items) && i < len(s.pos) && i < len(s.heights); i++ {
		buf = append(buf[:0], s.items[i].key...)
		buf = append(buf, '|')
		buf = strconv.AppendInt(buf, int64(s.pos[i]), 10)
		buf = append(buf, '|')
		buf = strconv.AppendInt(buf, int64(s.heights[i]), 10)
		buf = append(buf, ';')
		h.Write(buf)
	}
	return len(s.snap.msgs), s.snap.loaded, h.Sum64()
}
