package messages

import (
	"image"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/composer"
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
	return NewChannelViewIn(h, channelID, "")
}

// NewChannelViewIn is NewChannelView for the list in view container pane
// ("split": Slack's split view; "" is NewChannelView): its rows, scroll
// position and inline editors are its own.
func NewChannelViewIn(h Host, channelID, pane string) *ChannelView {
	v := &ChannelView{h: h, ch: channelID, s: stateIn(h, channelID, "", pane)}
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

// EditComposer is the message being edited inline here, or nil
// (FileDragDrop's isMessageEdited / messageEditData).
func (v *ChannelView) EditComposer() *composer.Composer { return v.s.editComposer() }

// lastChannel/lastThread are the conversations drawn most recently; when
// another one is drawn, the previous one was left and its inline editor is
// discarded, like Slack (views themselves are cached by the ui root).
var lastChannel, lastThread *listState

// paneLast holds lastChannel/lastThread for the other view containers
// (stateIn's pane), so the split view's lists don't close the main
// views' editors every frame.
var paneLast = map[string]**listState{}

func shown(last **listState, s *listState) {
	if s.container != "" {
		k := s.container
		if last == &lastThread {
			k += "/thread"
		}
		if paneLast[k] == nil {
			paneLast[k] = new(*listState)
		}
		last = paneLast[k]
	}
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

// Typing returns the "X is typing" text for this channel ("" if nobody).
func (v *ChannelView) Typing() string { return typingText(v.s.snap.typing) }

// ScrollToBottom jumps to the newest message (e.g. after sending).
func (v *ChannelView) ScrollToBottom() { v.s.list.Position = layout.Position{} }

// AtBottom reports whether the newest message is visible.
func (v *ChannelView) AtBottom() bool { return !v.s.list.Position.BeforeEnd }

// NewestRowBottom is the window y of the newest message row's bottom edge
// as last drawn, and whether that row was on screen (the gap above the
// composer, ui's TestComposerGap).
func (v *ChannelView) NewestRowBottom() (int, bool) { return v.s.newestBottom() }

// Highlight scrolls to ts and flashes it (permalinks, search results,
// route "c/<id>/m/<ts>"). Older messages are paged in until ts is loaded.
// The jump stays pending until the list's next sync places it: a list
// mounted in the background (the thread header's channel link with the
// channel not on screen) rebuilds with the messages that arrived since it
// last showed, and that rebuild's scroll anchoring, or an eviction's
// reset, would otherwise put it back where it was last left.
func (v *ChannelView) Highlight(ts string) {
	v.s.highlight, v.s.highlightAt = ts, time.Now()
	v.s.positioned = true
	v.s.jumpPages = 0
	v.s.pendingJump = ts
	for i, it := range v.s.items {
		if it.kind == itMsg && it.idx < len(v.s.snap.msgs) && v.s.snap.msgs[it.idx].m.TS == ts {
			// there now; the next sync places it again after any rebuild
			v.s.list.Position = layout.Position{BeforeEnd: true, First: max(0, i-2)}
			break
		}
	}
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
	h := uint64(14695981039346656037) // FNV-1a, inline: no allocation per frame
	mix := func(b byte) { h ^= uint64(b); h *= 1099511628211 }
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items) && i < len(s.pos) && i < len(s.heights); i++ {
		k := s.items[i].key
		for j := 0; j < len(k); j++ {
			mix(k[j])
		}
		for _, v := range [2]int{s.pos[i], s.heights[i]} {
			for j := 0; j < 8; j++ {
				mix(byte(v >> (8 * j)))
			}
		}
	}
	return len(s.snap.msgs), s.snap.loaded, h
}

// Loading reports whether the list is fetching (its first page, a refresh
// or an older page it asked for to fill the view): the ui root's reveal
// waits for it so the first visible frame doesn't move.
func (v *ChannelView) Loading() bool { return v.s.snap.loading || v.s.snap.older }

// RowY is an item's key and top edge in the last layout.
type RowY struct {
	Key string
	Y   int
}

// VisibleRows lists the items on screen in the last layout, top to bottom
// (the wheel frame probe measures scrolled distance with it).
func (v *ChannelView) VisibleRows() []RowY {
	s := v.s
	var out []RowY
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items) && i < len(s.pos); i++ {
		out = append(out, RowY{Key: s.items[i].key, Y: s.pos[i]})
	}
	return out
}
