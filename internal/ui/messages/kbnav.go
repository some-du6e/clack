package messages

import (
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
			key.Filter{Focus: &s.kb, Name: "V"})
		if !ok {
			break
		}
		switch e := ev.(type) {
		case key.FocusEvent:
			s.kb.on = e.Focus
		case key.Event:
			if e.State != key.Press {
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
	if s.kb.page == "" {
		return
	}
	first := s.kb.page == key.NamePageUp || s.kb.page == key.NameHome
	s.kb.page = ""
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

// FocusNewest is ChannelView.FocusNewest for a thread's replies.
func (v *ThreadView) FocusNewest(leave func()) bool {
	v.s.kb.leave = leave
	v.h.Invalidate()
	return v.s.focusNewest()
}
