package ui

import "gioui.org/io/key"

// Keyboard mode is Slack's FocusEventManager.hasKeyboardFocus (client-boot,
// accessibilityPersistent.hasKeyboardFocus): Tab, F6 or an arrow key pressed
// (isKeyboardNavEvent) turns it on, a mouse press turns it off. Slack's
// focus transitions that aren't forced run only in it, and an edit that
// ends (stopEditingMessage) gives the message list the focus at its
// message in it.

// keyboardNavKeys are isKeyboardNavEvent's keys.
var keyboardNavKeys = []key.Name{key.NameTab, key.NameF6, key.NameUpArrow, key.NameDownArrow, key.NameLeftArrow, key.NameRightArrow}

// noteKeyboardNav takes a key press, whichever widget took it (konamiKeys
// reads them all: Gio patch 28), for keyboard mode.
func (a *App) noteKeyboardNav(e key.Event) {
	for _, n := range keyboardNavKeys {
		if e.Name == n {
			a.kbMode = true
			return
		}
	}
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

// threadListFocus is the navigation's focus for a thread opened in
// keyboard mode: TRANSITION_FOCUS_NAVIGATE_LISTENER transitions to the
// Thread view's focus key, THREAD_MESSAGE_LIST (getFocusKeyForView, kIpW;
// d7RN's default for a Thread view), not forced, so only in keyboard mode.
// The thread list's onFocusTransition has no message to go to (no
// lastFocusTs, no replyTs from the navigation) and focuses the list
// (BaseKeyboardNavigableList.focus: the first message in view, or at its
// next update while it has none). The reply box's mount focus
// (setFocusInput) comes in the same commit when the box's row is drawn
// with the first messages, before the list's update, which wins then;
// later, the box's comes after and wins (focusInputOnMount).
func (a *App) threadListFocus(channel, ts string) {
	if a.kbMode {
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
