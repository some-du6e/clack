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
	leave    func() // back to the composer
}

// focusNewest selects the newest message and takes keyboard focus.
func (s *listState) focusNewest() bool {
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
