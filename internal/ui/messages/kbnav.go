package messages

import (
	"image"
	"unicode"
	"unicode/utf8"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
)

// Keyboard focus in the message list, for Accessibility > "Press ↑ in the
// empty message field to: Move focus to the message list" ("The most
// recent message in the list will be selected"). The selected message
// draws as hovered (its highlight and hover actions); ↑/↓ move it, ↓ past
// the newest message or Escape hands focus back to the composer, Enter or
// E edits a message of yours, and V is its "Add to list…" (6r+G's
// shortcut, EtVr P2.ADD_TO_LIST).

type kbNav struct {
	on       bool   // the list has keyboard focus
	ts       string // the selected message
	focusReq bool
	want     string // FocusMessage's message, until the list has it
	newest   bool   // focusNewest left a window: select the newest message once it's drawn
	leave    func() // back to the composer
	// page: a PageUp/PageDown/Home/End scrolled the list; the next layout
	// selects the first (PageUp, Home) or last message in view (pageKey)
	page key.Name
	// firstInView: the next layout selects the first message in view
	// (FocusFirstInView)
	firstInView bool
	// steal is the host's way to the conversation's input for a key the
	// list doesn't take (SetSteal): it types the key there and returns the
	// input's focus tag
	steal func(text string, k key.Event) event.Tag
}

// msgActionKeys are a selected message's own keys (EtVr P2's single
// letters: edit, react, thread, pin, share, save, mark unread, remind,
// copy link, add to list): its key commands take them, so the input
// doesn't (shouldInputStealKeyEvent's defaultPrevented).
const msgActionKeys = "ertpsfaumlv"

// stealsKey is shouldInputStealKeyEvent for typed text a list has the
// focus for: one printable character, not Space, nor one of the selected
// message's own keys.
func stealsKey(s string) bool {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 || n != len(s) || r == ' ' || !unicode.IsPrint(r) {
		return false
	}
	for _, k := range msgActionKeys {
		if r == k {
			return false
		}
	}
	return true
}

// kbWheelBlur is BaseKeyboardNavigableList's onScroll after an onWheel:
// a wheel (the list's own scrolling, Gio patch 50's ScrollDelta, not a
// drag of it) that leaves the selected row wholly above or below the view
// (isItemAboveView, isItemBelowView) blurs it when the list has the
// keyboard focus (shouldBlurItemScrolledOutOfView: always for a channel's
// list, a thread's unless its reply box has the focus, which the list
// then hasn't). The focus goes to nothing (activeElement.blur()); a key
// typed then goes to the channel's input (typeSteal). PageUp and a drag of
// the scrollbar keep it.
func (s *listState) kbWheelBlur(gtx layout.Context, size image.Point) {
	if !s.kb.on || s.kb.ts == "" || s.list.ScrollDelta() == 0 || s.list.Dragging() {
		return
	}
	for _, r := range s.laid {
		if r.key == s.kb.ts && r.y+r.h >= 0 && r.y <= size.Y {
			return // some of it still in view
		}
	}
	s.kb.on = false
	gtx.Execute(key.FocusCmd{Tag: nil})
}

// kbSteal hands a key the list has the focus for to the conversation's
// input, which takes the focus at once (so the keys after it go there
// too): for a channel's list MessagePaneInput's handleKeyDown, which
// steals a list item's key as any other outside the thread pane; for a
// thread's ThreadsFlexpane onKeyDown, whose focusOnKey("input") moves the
// focus to the reply box with the key.
func (s *listState) kbSteal(gtx layout.Context, text string, k key.Event) {
	if s.kb.steal == nil {
		return
	}
	t := s.kb.steal(text, k)
	if t == nil {
		return
	}
	s.kb.on = false
	gtx.Execute(key.FocusCmd{Tag: t})
}

// focusNewest selects the newest message and takes keyboard focus. A
// window short of the newest messages goes to them first, as Slack's
// scrollToMostRecentMessage(true) does (jumpToTimestamp({ts: ""}) with
// !reachedEnd, AEjl), and its onJump focuses the last row once they're
// drawn (kbWant).
func (s *listState) focusNewest() bool {
	if s.win != nil && s.snap.newestMissing {
		s.jumpToLatest()
		s.kb.want, s.kb.newest = "", true
		return true
	}
	for i := len(s.items) - 1; i >= 0; i-- {
		if s.items[i].kind == itMsg {
			s.kb.ts, s.kb.focusReq = s.items[i].key, true
			s.scrollTo(s.kb.ts)
			return true
		}
	}
	return false
}

// kbIndex is the selected message's item index (-1 when not focused).
func (s *listState) kbIndex() int {
	if !s.kb.on {
		return -1
	}
	for i, it := range s.items {
		if it.kind == itMsg && it.key == s.kb.ts {
			return i
		}
	}
	return -1
}

// kbKeys handles the list's focus and keys (layoutList, each frame).
func (s *listState) kbKeys(gtx layout.Context, h Host) {
	s.kbWant()
	if s.kb.focusReq && gtx.Source.Enabled() {
		s.kb.focusReq = false
		s.kb.on = true
		gtx.Execute(key.FocusCmd{Tag: &s.kb})
	}
	for {
		ev, ok := gtx.Event(key.FocusFilter{Target: &s.kb},
			key.Filter{Focus: &s.kb, Name: key.NameUpArrow},
			key.Filter{Focus: &s.kb, Name: key.NameDownArrow},
			key.Filter{Focus: &s.kb, Name: key.NamePageUp},
			key.Filter{Focus: &s.kb, Name: key.NamePageDown},
			key.Filter{Focus: &s.kb, Name: key.NameHome},
			key.Filter{Focus: &s.kb, Name: key.NameEnd},
			key.Filter{Focus: &s.kb, Name: key.NameEscape},
			key.Filter{Focus: &s.kb, Name: key.NameReturn},
			key.Filter{Focus: &s.kb, Name: "E"},
			key.Filter{Focus: &s.kb, Name: "V"},
			// enableShortcuts: Mod+B, Mod+I and Mod+V go to the input
			key.Filter{Focus: &s.kb, Name: "B", Required: key.ModShortcut, Optional: key.ModShift | key.ModAlt},
			key.Filter{Focus: &s.kb, Name: "I", Required: key.ModShortcut, Optional: key.ModShift | key.ModAlt},
			key.Filter{Focus: &s.kb, Name: "V", Required: key.ModShortcut, Optional: key.ModShift | key.ModAlt})
		if !ok {
			break
		}
		switch e := ev.(type) {
		case key.FocusEvent:
			s.kb.on = e.Focus
		case key.EditEvent:
			// a character typed with a message selected: the input's
			if s.kb.on && stealsKey(e.Text) {
				s.kbSteal(gtx, e.Text, key.Event{})
			}
		case key.Event:
			if e.State != key.Press {
				continue
			}
			if e.Modifiers.Contain(key.ModShortcut) {
				s.kbSteal(gtx, "", e) // Mod+B, Mod+I, Mod+V
				continue
			}
			i := s.kbIndex()
			switch e.Name {
			case key.NameUpArrow, key.NameDownArrow:
				d := 1
				if e.Name == key.NameUpArrow {
					d = -1
				}
				j := i + d
				for j >= 0 && j < len(s.items) && s.items[j].kind != itMsg {
					j += d
				}
				switch {
				case j >= 0 && j < len(s.items):
					s.kb.ts = s.items[j].key
					s.scrollTo(s.kb.ts)
				case d > 0:
					s.kbLeave(gtx)
				}
			case key.NamePageUp, key.NamePageDown, key.NameHome, key.NameEnd:
				s.pageKey(e.Name)
			case key.NameEscape:
				s.kbLeave(gtx)
			case "V": // Add to list…: a message the server has (not a local send)
				r := s.rows[s.kb.ts]
				if la, ok := h.(ListAdder); ok && r != nil && !r.src.Pending && !r.src.Failed {
					la.AddMessageToList(r.src)
				}
			default: // Enter, E: edit it when it's yours
				if r := s.rows[s.kb.ts]; r != nil && h.Core() != nil && r.m.User == h.Core().Self().ID {
					s.kbLeave(gtx)
					s.startEdit(h, s.kb.ts)
				}
			}
			h.Invalidate()
		}
	}
	event.Op(gtx.Ops, &s.kb)
}

// pageKey is BaseKeyboardNavigableList's PageUp, PageDown, Home and End
// (client-boot FOmE): a page (getPageHeight: the list's height less 21px)
// up or down, or the ends of what's loaded; with keyboard focus the
// selection then goes to the first message in view (PageUp, Home) or the
// last (PageDown, End), as Slack's focusFirstItemInView and
// getNextLastFocusableItemInView do. Each writes the scroll, so the list's
// anchoring starts from the new place. End is MessageList's onEnd,
// scrollToMostRecentMessage: a window short of the newest messages jumps
// to them (jumpToTimestamp({ts: ""}), AEjl).
func (s *listState) pageKey(name key.Name) {
	if s.kb.on {
		s.kb.page = name
	}
	if name == key.NameEnd && s.win != nil && s.snap.newestMissing {
		s.jumpToLatest()
		return
	}
	p := s.list.Position
	switch name {
	case key.NamePageUp:
		p.BeforeEnd, p.Offset = true, p.Offset-s.pageH
	case key.NamePageDown:
		p.BeforeEnd, p.Offset = true, p.Offset+s.pageH
	case key.NameHome:
		p = layout.Position{BeforeEnd: true}
	case key.NameEnd:
		p = layout.Position{} // scrollToOffset(Infinity)
		if !s.list.ScrollToEnd {
			p = layout.Position{BeforeEnd: true, First: len(s.items)}
		}
	}
	s.list.Position = p
	s.stopScroll()
}

// pageKeyNames maps the DOM's KeyboardEvent.key names that MessagePane's
// keydown checks (what the composer hands over) to Gio's key names, which
// are symbols.
var pageKeyNames = map[string]key.Name{"PageUp": key.NamePageUp, "PageDown": key.NamePageDown, "Home": key.NameHome, "End": key.NameEnd}

// PageKey is MessagePane's PageUp, PageDown, Home or End (name, the DOM's
// key name) from the channel's empty input: the list's onKeyDownPageUp
// and the rest (AEjl's document keydown), as from the list.
func (v *ChannelView) PageKey(name string) {
	k, ok := pageKeyNames[name]
	if !ok {
		return
	}
	v.s.pageKey(k)
	v.h.Invalidate()
}

// pageSelect is pageKey's selection, after the layout that scrolled.
func (s *listState) pageSelect() {
	if s.kb.page == "" && !s.kb.firstInView {
		return
	}
	first := s.kb.firstInView || s.kb.page == key.NamePageUp || s.kb.page == key.NameHome
	s.kb.page, s.kb.firstInView = "", false
	pick := ""
	for _, r := range s.laid {
		in := r.y >= 0 && r.y+r.h <= s.size.Y
		if first && (in || pick == "") {
			pick = r.key
			if in {
				break
			}
		}
		if !first && (in || pick == "") {
			pick = r.key
		}
	}
	if pick != "" {
		s.kb.ts = pick
	}
}

func (s *listState) kbLeave(gtx layout.Context) {
	s.kb.on = false
	gtx.Execute(key.FocusCmd{Tag: nil})
	if s.kb.leave != nil {
		s.kb.leave()
	}
}

// kbWant selects FocusMessage's message once the jump to it is done (the
// history holds it and pendingJump is spent); a message the jump gave up
// on is dropped.
func (s *listState) kbWant() {
	if s.kb.newest {
		// the newest messages, rebuilt from the conversation's model
		if !s.snap.loaded || s.win != nil || s.built != s.snap.rev {
			return
		}
		s.kb.newest = false
		if i := s.newestItem(); i >= 0 {
			s.kb.ts, s.kb.focusReq = s.items[i].key, true
		}
		return
	}
	w := s.kb.want
	if w == "" || !s.snap.loaded || s.pendingJump != "" {
		return
	}
	s.kb.want = ""
	for _, it := range s.items {
		if it.kind == itMsg && it.idx < len(s.snap.msgs) && s.snap.msgs[it.idx].m.TS == w {
			s.kb.ts, s.kb.focusReq = it.key, true
			return
		}
	}
}

// FocusMessage is transitionFocusTo(MESSAGE_PANE_MESSAGE_LIST) with
// uiState.ts after a jumpToMessage (client-threads-view onItemKeyDown):
// once the list holds ts it takes keyboard focus with that message
// selected; leave runs when focus goes back.
func (v *ChannelView) FocusMessage(ts string, leave func()) {
	v.s.kb.leave, v.s.kb.want = leave, ts
	v.h.Invalidate()
}

// KeyboardSelected is the message the list's keyboard focus selects (""
// while the list doesn't have it).
func (v *ChannelView) KeyboardSelected() string {
	if !v.s.kb.on {
		return ""
	}
	return v.s.kb.ts
}

// FocusNewest moves keyboard focus to the list with its newest message
// selected; leave runs when focus goes back (↓ past the end, Escape).
func (v *ChannelView) FocusNewest(leave func()) bool {
	v.s.kb.leave = leave
	v.h.Invalidate()
	return v.s.focusNewest()
}

// SetSteal gives the list its way to the conversation's input for a key
// it has the focus for and doesn't take (kbSteal): f types the key there
// and returns the input's focus tag (nil: no input to take it).
func (v *ChannelView) SetSteal(f func(text string, k key.Event) event.Tag) { v.s.kb.steal = f }

// ListKeyFocus reports whether the thread's message list has the key
// focus (a reply selected): the shell's Escape is then the pane's, as
// from its reply box.
func (v *ThreadView) ListKeyFocus(gtx layout.Context) bool { return gtx.Focused(&v.s.kb) }

// SelectedReply is the message the thread list's keyboard focus selects
// ("" while the list doesn't have it; tests).
func (v *ThreadView) SelectedReply() string {
	if !v.s.kb.on {
		return ""
	}
	return v.s.kb.ts
}

// HasMessages reports whether the thread's list has messages to draw in
// this frame (Prepare's copy).
func (v *ThreadView) HasMessages() bool { return v.s.snap.loaded && len(v.s.snap.msgs) > 0 }

// FocusFirstInView is BaseKeyboardNavigableList.focus with no message to
// go to (keyboard mode's focus for a thread just opened): the list takes
// the keyboard focus with the first message in view selected; leave runs
// when the focus goes back (↓ past the end).
func (v *ThreadView) FocusFirstInView(leave func()) bool {
	s := v.s
	s.kb.leave = leave
	s.kb.focusReq, s.kb.firstInView = true, true
	v.h.Invalidate()
	return true
}

// SetSteal is ChannelView.SetSteal for a thread's replies: the reply box.
func (v *ThreadView) SetSteal(f func(text string, k key.Event) event.Tag) { v.s.kb.steal = f }

// FocusNewest is ChannelView.FocusNewest for a thread's replies.
func (v *ThreadView) FocusNewest(leave func()) bool {
	v.s.kb.leave = leave
	v.h.Invalidate()
	return v.s.focusNewest()
}
