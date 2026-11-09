package popups

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/refocus"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/shortcut"
)

// Keyboard and focus, as Slack's modals do it (BaseModal/c-sk-modal over
// react-modal, and the older DeprecatedBaseDialog; app.slack.com's
// modern.vendor and gantry-v2-shared-boot-async chunks):
//
//   - On open the autoFocus button takes the focus, with its ring
//     (BaseButtonInternal adds c-button--focus-visible for autoFocus when
//     visibleAutoFocus, the default): Delete in "Delete message"
//     (focusOnPrimaryButton) and "Delete draft?", "Leave Channel",
//     ConfirmationModal's submit unless shouldAutoFocusCancel, "Edit
//     Message" in "Notify N people?". Without one, react-modal focuses the
//     dialog itself.
//   - Enter and Space press the focused button; nothing else submits.
//     (DeprecatedBaseDialog binds Enter to go, but its onEnter skips any
//     target whose contentEditable is set, which every element's is.)
//   - Escape closes the topmost modal (react-modal's onRequestClose, or
//     the old dialog's Mousetrap escape).
//   - Tab and Shift+Tab move through the dialog's tabbable elements in
//     document order and wrap at the ends (react-modal scopeTab): the
//     body, the footer buttons, then the close button, which c-sk-modal
//     renders last and the old dialogs render first, in the header. A
//     focus reached by the keyboard shows the ring; a mouse press hides
//     it again (KeyboardFocusPropsProvider's hasKeyboardFocus).
//   - On close the focus goes back to what had it when the modal opened
//     (useRestoreFocus), if the focus is then nowhere or still in the
//     modal.

// autoFocus is the button that takes the focus on open, or nil.
func (m *Modal) autoFocus() *ModalButton {
	if b := m.o.AutoFocus; b != nil {
		if b.enabled() {
			return b
		}
		return nil
	}
	if m.o.EnterSubmits {
		for i := len(m.o.Buttons) - 1; i >= 0; i-- {
			if b := m.o.Buttons[i]; b.enabled() {
				return b
			}
		}
	}
	return nil
}

// stops is the tab order: [close], body, left footer buttons, the right
// group, [close].
func (m *Modal) stops() []event.Tag {
	var out []event.Tag
	closeStop := !m.o.NoClose
	if closeStop && m.o.CloseFirst {
		out = append(out, &m.close)
	}
	if m.o.FocusStops != nil {
		for _, t := range m.o.FocusStops() {
			if t != nil {
				out = append(out, t)
			}
		}
	}
	for _, left := range []bool{true, false} {
		for _, b := range m.o.Buttons {
			if b.Left == left && b.enabled() {
				out = append(out, &b.click)
			}
		}
	}
	if closeStop && !m.o.CloseFirst {
		out = append(out, &m.close)
	}
	return out
}

// keys runs the modal's focus and keys for this frame; true when it closed.
func (m *Modal) keys(gtx layout.Context) bool {
	m.frames++
	if !m.focus {
		m.focus = true
		if !m.overModal() {
			m.prev = refocus.Last(gtx.Now)
		}
		if b := m.autoFocus(); b != nil {
			m.focusTo(gtx, &b.click, !m.o.QuietFocus)
		} else {
			gtx.Execute(key.FocusCmd{Tag: m})
		}
	}
	top := isTop(m)
	if top && !m.wasTop && m.frames > 1 && gtx.Focused(nil) {
		// back on top (a menu, picker or modal over it closed) with the
		// focus gone: give it to the stop that had it
		if m.last != nil && m.hasStop(m.last) {
			gtx.Execute(key.FocusCmd{Tag: m.last})
		} else {
			gtx.Execute(key.FocusCmd{Tag: m})
		}
	}
	m.wasTop = top
	if !top {
		return false
	}
	filters := []event.Filter{
		key.Filter{Name: key.NameEscape},
		key.Filter{Name: key.NameTab, Optional: key.ModShift},
	}
	if m.o.OnCmdEnter != nil {
		mod := key.ModSuper // Mousetrap's "command" is the meta key
		if shortcut.GOOS == "darwin" {
			mod = key.ModCommand
		}
		filters = append(filters, key.Filter{Name: key.NameReturn, Required: mod}, key.Filter{Name: key.NameEnter, Required: mod})
	}
	for {
		e, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.NameEscape:
			m.Dismiss()
			return true
		case key.NameTab:
			m.tab(gtx, ke.Modifiers.Contain(key.ModShift))
		case key.NameReturn, key.NameEnter:
			m.o.OnCmdEnter()
			if m.done {
				return true
			}
		}
	}
	return false
}

func (m *Modal) hasStop(t event.Tag) bool {
	for _, s := range m.stops() {
		if s == t {
			return true
		}
	}
	return false
}

// tab moves the focus to the next (back: previous) stop, wrapping.
func (m *Modal) tab(gtx layout.Context, back bool) {
	stops := m.stops()
	if len(stops) == 0 {
		gtx.Execute(key.FocusCmd{Tag: m})
		return
	}
	cur := -1
	for i, t := range stops {
		if gtx.Focused(t) {
			cur = i
			break
		}
	}
	next := 0
	switch {
	case cur < 0 && back:
		next = len(stops) - 1
	case cur < 0:
		next = 0
	case back:
		next = (cur - 1 + len(stops)) % len(stops)
	default:
		next = (cur + 1) % len(stops)
	}
	m.focusTo(gtx, stops[next], true)
}

func (m *Modal) focusTo(gtx layout.Context, t event.Tag, ring bool) {
	gtx.Execute(key.FocusCmd{Tag: t})
	m.last, m.ring = t, ring
	m.h.Invalidate()
}

// ringOn reports whether the stop t shows the focus ring this frame.
func (m *Modal) ringOn(gtx layout.Context, t event.Tag) bool {
	if !m.ring || !gtx.Focused(t) {
		return false
	}
	m.last = t
	return true
}

// pointerSeen hides the ring after a mouse press anywhere in the window
// (pass-through: the press still reaches the button or field).
func (m *Modal) pointerSeen(gtx layout.Context) {
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &m.ptr, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Press {
			m.ring = false
		}
	}
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect(image.Rectangle{Max: gtx.Constraints.Max}).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &m.ptr)
}

// overModal reports whether another modal is open under m (a Block Kit
// confirm over its view, a nested dialog): that one takes the focus back
// itself when m closes.
func (m *Modal) overModal() bool {
	for _, o := range stackPrev {
		if o == m {
			continue
		}
		if om, ok := o.(*Modal); ok && !om.done && om.h == m.h {
			return true
		}
	}
	return false
}

// giveBackFocus asks the widget focused before the modal opened to take
// the focus back (Dismiss).
func (m *Modal) giveBackFocus() {
	if m.prev == nil {
		return
	}
	from := append([]event.Tag{m}, m.stops()...)
	refocus.Restore(m.prev, from...)
	m.prev = nil
	m.h.Invalidate()
}

// FocusedButton is the label of the footer button holding the key focus
// last frame ("" for none), and whether it shows the ring (tests).
func (m *Modal) FocusedButton(gtx layout.Context) (label string, ring bool) {
	for _, b := range m.o.Buttons {
		if gtx.Focused(&b.click) {
			return b.Label, m.ring
		}
	}
	if gtx.Focused(&m.close) {
		return "Close", m.ring
	}
	return "", false
}
