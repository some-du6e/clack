package ui

import (
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/composer"
)

// Keyboard mode is Slack's FocusEventManager.hasKeyboardFocus (client-boot,
// accessibilityPersistent.hasKeyboardFocus). onDocumentKeyDown keeps the
// last keydown (relatedEvent); onDocumentFocus, as the focus moves to an
// element that isn't an input (isInputElement: INPUT, SELECT, TEXTAREA,
// contenteditable; it returns early for those), turns keyboard mode on
// for a Tab, F6 or arrow keydown (isKeyboardNavEvent), off for anything
// else (a mousedown, another key), and keeps it with no event since. So a
// caret arrow in a text field, which moves no focus, changes nothing.
// Slack's focus transitions that aren't forced run only in keyboard mode,
// and an edit that ends (stopEditingMessage) gives the message list the
// focus at its message in it.
//
// Gio moves the focus between handlers only: an arrow in a list (the
// message lists, Activity's, a vkList) moves the selection inside one
// handler, where Slack's roving focus moves between row elements, so a
// nav key with a non-input focused counts as a focus move there. A press
// in the window turns keyboard mode off at once (notePress).

// kbRelated is relatedEvent: what came last before the focus moved.
type kbRelated uint8

const (
	kbRelNone  kbRelated = iota // nothing since the focus last moved to a non-input
	kbRelNav                    // a Tab, F6 or arrow keydown
	kbRelKey                    // another keydown
	kbRelPress                  // a pointer press
)

// keyboardNavKeys are isKeyboardNavEvent's keys.
var keyboardNavKeys = []key.Name{key.NameTab, key.NameF6, key.NameUpArrow, key.NameDownArrow, key.NameLeftArrow, key.NameRightArrow}

// noteKeyboardNav takes a key press, whichever widget took it (konamiKeys
// reads them all: Gio patch 28): onDocumentKeyDown's relatedEvent.
func (a *App) noteKeyboardNav(e key.Event) {
	a.kbRelated = kbRelKey
	for _, n := range keyboardNavKeys {
		if e.Name == n {
			a.kbRelated = kbRelNav
			return
		}
	}
}

// notePress is a pointer press in the window (readRootPointer): keyboard
// mode ends, and a thread's list waiting for the focus (threadListFocus)
// doesn't get it: the press put the focus somewhere of its own.
func (a *App) notePress() {
	a.kbMode = false
	a.kbRelated = kbRelPress
	a.threadFocus.list = ""
}

// keyboardModeFrame is onDocumentFocus for the focus the last frame left
// (App.frame, before this frame's keys are noted). A move elsewhere also
// drops a thread list's pending focus (threadListFocus), unless it went
// into that thread (its reply box mounting with the focus, the list
// itself).
func (a *App) keyboardModeFrame(gtx layout.Context) {
	f := gtx.Source.FocusTag()
	moved := f != a.kbFocus
	a.kbFocus = f
	if moved && f != nil && a.threadFocus.list != "" && !a.focusInThread(gtx, a.threadFocus.list) {
		a.threadFocus.list = ""
	}
	if f == nil || isTextInput(f) {
		return // no focus event, or an input's: onDocumentFocus returns early
	}
	switch {
	case moved:
		a.kbMode = a.kbRelated == kbRelNav || (a.kbMode && a.kbRelated == kbRelNone)
	case a.kbRelated == kbRelNav:
		a.kbMode = true // a nav key in a focused list: its roving focus moved
	default:
		return
	}
	a.kbRelated = kbRelNone
}

// isTextInput is isInputElement for a focus tag: an editor's.
func isTextInput(t event.Tag) bool {
	switch t.(type) {
	case *composer.Editor, *widget.Editor:
		return true
	}
	return false
}

// focusInThread reports whether the key focus is in the thread k
// (ch+"/"+ts) as drawn in either view container: its reply box, an edit
// composer in its place, its message list.
func (a *App) focusInThread(gtx layout.Context, k string) bool {
	in := false
	check := func() {
		c := a.convos[k]
		if c == nil {
			return
		}
		for _, cv := range []composerView{c.comp, c.edit} {
			if ft, ok := cv.(interface{ FocusTag() any }); ok && gtx.Focused(ft.FocusTag()) {
				in = true
			}
		}
		if l, ok := c.list.(interface{ ListKeyFocus(layout.Context) bool }); ok && l.ListKeyFocus(gtx) {
			in = true
		}
	}
	check()
	if !in && a.splitOpen() {
		a.inPane(paneSplit, check)
	}
	return in
}

// KeyboardMode reports keyboard mode (messages.KeyboardModer).
func (a *App) KeyboardMode() bool { return a.kbMode }

// FocusInput gives the conversation's input the focus: the channel's
// message input (thread "") or the thread's reply box
// (messages.InputFocuser: an edit started from it ends).
func (a *App) FocusInput(channel, thread string) {
	if thread != "" {
		a.FocusThreadComposer(channel, thread)
		return
	}
	a.focusChannelComposer(channel)
}

// PreventMoveFocus runs open with its navigation keeping the focus where
// it is (Slack's preventMoveFocus): Activity's option+up/down, whose item
// opens beside the inbox while the focus stays in its list
// (VIEW_ACTIVITY_LIST, views.Activity.openStep).
func (a *App) PreventMoveFocus(open func()) {
	was := a.noMoveFocus
	a.noMoveFocus = true
	defer func() { a.noMoveFocus = was }()
	open()
}

// threadListFocus is the navigation's focus for a thread opened in
// keyboard mode: TRANSITION_FOCUS_NAVIGATE_LISTENER transitions to the
// Thread view's focus key, THREAD_MESSAGE_LIST (getFocusKeyForView, kIpW;
// d7RN's default for a Thread view), not forced, so only in keyboard mode,
// and not for an open with preventMoveFocus. The thread list's
// onFocusTransition has no message to go to (no lastFocusTs, no replyTs
// from the navigation) and focuses the list (BaseKeyboardNavigableList.
// focus: the first message in view, or at its next update while it has
// none). The reply box's mount focus (setFocusInput) comes in the same
// commit when the box's row is drawn with the first messages, before the
// list's update, which wins then; later, the box's comes after and wins
// (focusInputOnMount). A press, or the focus moving out of the thread,
// before the list draws drops it (keyboardModeFrame).
func (a *App) threadListFocus(channel, ts string) {
	if a.kbMode && !a.noMoveFocus {
		a.threadFocus.list = channel + "/" + ts
	}
}

// threadListFocusNow gives c's thread list the keyboard focus when
// threadListFocus asked for it and the list has messages to draw this
// frame: before the list's layout, after the box's mount focus
// (layoutConvo), so the list's focus comes last in a frame that draws
// both.
func (a *App) threadListFocusNow(c *convo) {
	if c.ts == "" || a.threadFocus.list != c.ch+"/"+c.ts {
		return
	}
	l, ok := c.list.(interface {
		HasMessages() bool
		FocusFirstInView(leave func()) bool
	})
	if !ok || !l.HasMessages() {
		return
	}
	a.threadFocus.list = ""
	l.FocusFirstInView(func() {
		if c.comp != nil {
			c.comp.Focus()
		}
	})
}
