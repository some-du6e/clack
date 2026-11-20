package messages

import (
	"image"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/slackc"
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
	// the load starts as the list first lays out (ensure): a route at a
	// message highlights it first, and a conversation not loaded yet then
	// loads only the window around it
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

// PaneID is the list's slackc pane: the conversation's composer draws its
// plugin components in it too (composer.LayoutIn).
func (v *ChannelView) PaneID() string { return v.s.paneID }

// Layout draws the list filling the constraints.
func (v *ChannelView) Layout(gtx layout.Context) layout.Dimensions {
	defer slackc.EndPane(slackc.Pane(v.s.paneID))
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

// ScrollToBottom jumps to the newest message.
func (v *ChannelView) ScrollToBottom() { v.s.list.Position = layout.Position{} }

// ScrollPx moves the list by px before its next layout (negative: toward
// older messages), as a frame of a wheel's scroll does (ui's
// TestRealWindowCaretCost scrolls the real window with it).
func (v *ChannelView) ScrollPx(px int) {
	v.s.list.Position.BeforeEnd = true
	v.s.list.Position.Offset += px
	v.h.Invalidate()
}

// JumpToOwnSend is the composer under the channel sending the viewer's
// message (composer.OnSend), cid its client_msg_id: the sendMessage
// thunk's jumpToTimestamp to it, which placeSendJump makes once the
// message is in the list, from wherever the list was (the unread line,
// history read back). A message with files (sendDraftFiles) leaves the
// list where it is.
func (v *ChannelView) JumpToOwnSend(cid string, files bool) {
	if files || cid == "" {
		return
	}
	s := v.s
	s.sendCID, s.sendAt = cid, time.Now()
	// sendMessage's jumpToTimestamp: a window short of the newest message
	// gives way to the newest messages, where the send goes
	s.jumpToLatest()
	// the navigation replaces the view's startTs and highlightTs: a jump
	// still paging back is dropped and a flashing message stops
	s.pendingJump, s.pendingDate = "", time.Time{}
	s.place = jumpPlace{}
	s.highlight = ""
	v.h.Invalidate()
}

// AtBottom reports whether the newest message is visible.
func (v *ChannelView) AtBottom() bool { return !v.s.list.Position.BeforeEnd }

// NewestRowBottom is the window y of the newest message row's bottom edge
// as last drawn, and whether that row was on screen (the gap above the
// composer, ui's TestComposerGap).
func (v *ChannelView) NewestRowBottom() (int, bool) { return v.s.newestBottom() }

// Highlight scrolls to ts and flashes it (permalinks, search results,
// route "c/<id>/m/<ts>"): in the conversation's newest messages, else in
// the window around it (openWindow). The jump stays pending until the
// list's next sync places it: a list mounted in the background (the
// thread header's channel link with the channel not on screen) rebuilds
// with the messages that arrived since it last showed, and that rebuild's
// scroll anchoring, or an eviction's reset, would otherwise put it back
// where it was last left.
func (v *ChannelView) Highlight(ts string) { v.highlight(ts, false) }

// HighlightLazy is Highlight for highlightRoot (jumpToThreadReply's
// highlightLazy): a message whose top is on screen already flashes where
// it is; one off screen or not loaded is scrolled to as Highlight's.
func (v *ChannelView) HighlightLazy(ts string) { v.highlight(ts, true) }

func (v *ChannelView) highlight(ts string, lazy bool) {
	// a repeat jump to a message still flashing doesn't restart it (its
	// highlight prop never turns from false to true: rFp4)
	s := v.s
	flashing := s.highlight == ts && time.Since(s.highlightAt) < highlightFor && s.mounted()
	if !flashing {
		s.highlight, s.highlightAt, s.hlSeen = ts, time.Now(), false
	}
	s.hlRestart = !flashing
	v.s.highlightReq = ts
	v.s.positioned = true
	v.s.aroundFor = ""
	v.s.pendingJump, v.s.jumpLazy = ts, lazy
	v.s.place = jumpPlace{} // the next sync places it (placeJump)
}

// Highlighted is the message the list highlights now (Highlight's flash,
// 3 s from when the jump was placed), "" for none.
func (v *ChannelView) Highlighted() string {
	if s := v.s; s.highlight != "" && time.Since(s.highlightAt) < highlightFor {
		return s.highlight
	}
	return ""
}

// JumpPending is the message a Highlight waits to place ("" for none).
func (v *ChannelView) JumpPending() string { return v.s.pendingJump }

// HighlightRequested is the message the list was last asked to highlight,
// placed or not (a jump to a message it never holds gives up, but the
// ui root still knows what it asked for: the hand-over's root highlight).
func (v *ChannelView) HighlightRequested() string { return v.s.highlightReq }

// InWindow reports whether the list shows a window away from the
// conversation's newest messages.
func (v *ChannelView) InWindow() bool { return v.s.win != nil }

// LatestMessagesRect is the "Latest messages" pill (ArchivesBanner) as
// drawn last frame, in window coordinates; empty while it isn't drawn.
func (v *ChannelView) LatestMessagesRect() image.Rectangle {
	if v.s.latestR.Empty() {
		return image.Rectangle{}
	}
	return v.s.latestR.Add(v.s.origin)
}

// Entered is a navigation to the conversation with no message to start at
// (a sidebar click, Back, a thread's route): Slack's determineTimestamps
// (client-boot) clears the channel's scrollmark when it has unread
// messages, so the pane shows its newest messages, where they are; else
// the pane opens at the scrollmark, where it was left (MessagePane:
// startTs || getScrollmark(state, channel).mark), a window too. The list
// keeps its place the same way: it leaves a window for the newest
// messages only when the conversation has unreads.
func (v *ChannelView) Entered() {
	app := v.h.Core()
	if app == nil || !app.Count(v.ch).Unread {
		return // where it was left: the row on the anchor line, the window too
	}
	// no scrollmark (determineTimestamps clears an unread conversation's,
	// client-boot eCxs) and, with Mark as read's default prefs, no
	// startTs: it opens at its newest messages, at their end
	s := v.s
	s.leaveWindow()
	s.pendingJump, s.aroundFor, s.place, s.jumpLazy = "", "", jumpPlace{}, false
	s.list.Position = layout.Position{}
	s.stopScroll()
	s.positioned = true
	v.h.Invalidate()
}

// Refresh refetches the newest messages in the background. A list still
// to open the window around a jump (core's names landed before its first
// frame) opens the window instead, through the list's own load: the
// conversation's newest page isn't the jump's (ensure).
func (v *ChannelView) Refresh() {
	v.s.m.mu.Lock()
	v.s.m.fetched = time.Time{}
	v.s.m.mu.Unlock()
	v.s.ensure()
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

// RowText is the text message ts's row drew in its last layout ("" with
// no row): what a test of the whole window reads of a row (the draw hook
// doesn't see richview's text).
func (v *ChannelView) RowText(ts string) string {
	r := v.s.rows[ts]
	if r == nil || r.rv == nil {
		return ""
	}
	return r.rv.TextRange(0, r.rv.PlainLen())
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
