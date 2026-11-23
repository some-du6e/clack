package ui

import (
	"unicode"
	"unicode/utf8"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Typing with no input focused goes into the conversation's message
// input, as Slack's does. MessagePaneInput listens for keydown on the
// document while it is mounted and no overlay is open
// (startListeningToKeydown, gantry boot-shared nI5B), and its
// handleKeyDown focuses the input for a key shouldInputStealKeyEvent
// (gqm0) passes whose target isn't in the thread pane, the Activity list
// or the channel sidebar's list; the browser then types the key's
// character into it. The key passes when nothing took it
// (defaultPrevented), no menu is up (isMenuVisible), its target is no
// input (isInputElement), and it is printable (isPrintableKeyEvent: no
// Ctrl or Meta, one character) and not Space, or it is Mod+B, Mod+I or
// Mod+V (enableShortcuts: zQkO's isCmdKey with b, i or v).
//
// Gio routes typed text to the focused handler only, so the App names a
// handler for the text nothing has the focus for (Gio patch 64) and hands
// it to the channel's composer; Mod+B, Mod+I and Mod+V come as keys. A
// message list with the keyboard focus hands its keys on itself
// (messages kbnav.go: the channel's to its input, as handleKeyDown does
// for a list item's key; the thread's to its reply box, ThreadsFlexpane's
// onKeyDown). Keys in the sidebar and Activity's list stay theirs.

// stealShortcuts are the shortcuts shouldInputStealKeyEvent passes with
// enableShortcuts (zQkO), Mod held.
var stealShortcuts = []key.Name{"B", "I", "V"}

// typeSteal reads the typed text and shortcuts no widget has the focus
// for (App.frame, after the shortcuts) and hands them to the primary
// view's channel input.
func (a *App) typeSteal(gtx layout.Context) {
	gtx.Source.SetUnfocusedText(&a.stealTag)
	fs := []event.Filter{key.FocusFilter{Target: &a.stealTag}}
	if gtx.Focused(nil) {
		for _, n := range stealShortcuts {
			fs = append(fs, key.Filter{Name: n, Required: key.ModShortcut, Optional: key.ModShift | key.ModAlt})
		}
	}
	for {
		ev, ok := gtx.Event(fs...)
		if !ok {
			return
		}
		var text string
		var k key.Event
		switch e := ev.(type) {
		case key.EditEvent:
			if !stealsText(e.Text) {
				continue
			}
			text = e.Text
		case key.Event:
			if e.State != key.Press {
				continue
			}
			k = e
		default:
			continue
		}
		if c := a.stealTarget(); c != nil {
			if t := a.stealInto(c, text, k); t != nil {
				gtx.Execute(key.FocusCmd{Tag: t})
			}
		}
	}
}

// stealsText is shouldInputStealKeyEvent's printable key for typed text:
// one character, not Space (keyCode 32).
func stealsText(s string) bool {
	r, n := utf8.DecodeRuneInString(s)
	return n > 0 && n == len(s) && r != ' ' && unicode.IsPrint(r)
}

// stealTarget is the conversation whose input takes a key nothing else
// has: the primary view's channel while its message input is mounted
// (not a preview's or archived channel's footer, nor an edit composer in
// its place) and no overlay or menu is up (MessagePaneInput stops
// listening while an overlay is open; isMenuVisible).
func (a *App) stealTarget() *convo {
	if a.login != nil || a.intro != nil || a.overlayOpen() || slackc.ModalOpen() {
		return nil
	}
	ch := a.router.Current().Channel()
	if ch == "" {
		return nil
	}
	c := a.convos[ch+"/"]
	if c == nil || c.comp == nil || c.edit != nil || a.footerFor(c) != footNone {
		return nil
	}
	if hc, ok := c.list.(interface{ HidesComposer() bool }); ok && hc.HidesComposer() {
		return nil
	}
	return c
}

// stealInto hands a key typeSteal or a message list took to c's input:
// text is typed at its caret, a shortcut (k) does what it does there. It
// returns the input's focus tag for the caller to focus at once, so the
// keys after it go there too (nil: c has no input to take it).
func (a *App) stealInto(c *convo, text string, k key.Event) event.Tag {
	if c == nil || c.comp == nil || (c.edit != nil && !c.edit.Done()) {
		return nil
	}
	t, ok := c.comp.(interface {
		TypeText(string)
		StealKey(key.Event)
		FocusTag() any
	})
	if !ok {
		return nil
	}
	if text != "" {
		t.TypeText(text)
	} else {
		t.StealKey(k)
	}
	a.Invalidate()
	return t.FocusTag()
}

// wireSteal gives c's message list its keys' way to c's input (kbnav.go
// SetSteal).
func (a *App) wireSteal(c *convo) {
	if l, ok := c.list.(interface {
		SetSteal(func(string, key.Event) event.Tag)
	}); ok {
		l.SetSteal(func(text string, k key.Event) event.Tag { return a.stealInto(c, text, k) })
	}
}
