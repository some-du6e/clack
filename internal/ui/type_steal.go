package ui

import (
	"time"
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
// enableShortcuts (zQkO: isCmdKey with e.key b, i or v), Mod held and
// Shift allowed. Not Alt: Windows sends AltGr as Ctrl+Alt, and AltGr+V
// types "@" on Czech, Hungarian and Croatian layouts (Slack's e.key is
// "@" then, not "v"), as Option changes a Mac's key.
var stealShortcuts = []key.Name{"B", "I", "V"}

// typeSteal reads the typed text and shortcuts no widget has the focus
// for (App.frame, after the shortcuts) and hands them to the primary
// view's channel input, the frame's together (messages.StealText).
func (a *App) typeSteal(gtx layout.Context) {
	gtx.Source.SetUnfocusedText(&a.stealTag)
	fs := []event.Filter{key.FocusFilter{Target: &a.stealTag}}
	if gtx.Focused(nil) {
		for _, n := range stealShortcuts {
			fs = append(fs, key.Filter{Name: n, Required: key.ModShortcut, Optional: key.ModShift})
		}
	}
	st := &a.stealText
	for {
		ev, ok := gtx.Event(fs...)
		if !ok {
			break
		}
		switch e := ev.(type) {
		case key.EditEvent:
			st.Edit(e, stealsText)
		case key.CompositionEvent:
			st.Composition(e)
		case key.Event:
			if e.State == key.Press {
				st.Key(e)
			}
		}
	}
	text, keys := st.Take()
	if text == "" && len(keys) == 0 {
		return
	}
	if c := a.stealTarget(); c != nil {
		if t := a.stealInto(gtx, c, text, keys); t != nil {
			gtx.Execute(key.FocusCmd{Tag: t})
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
// has: the primary view's channel, while its message input can (canSteal).
func (a *App) stealTarget() *convo {
	ch := a.router.Current().Channel()
	if ch == "" {
		return nil
	}
	if c := a.convos[ch+"/"]; a.canSteal(c) {
		return c
	}
	return nil
}

// canSteal reports whether c's input can take a key: MessagePaneInput
// listens only while it is mounted and no overlay is open (and
// isMenuVisible), ThreadsFlexpane's onKeyDown not while an overlay is
// open. Mounted is drawn in the last frame (or this one, for a list
// laid out after it): not behind another of the channel's tabs (Canvas,
// List, Files, Pins, Workflows, a folder; a DM's), whose views Slack
// draws without the input, nor a preview's or archived channel's footer
// in its place, an edit composer, or a thread slice short of its end.
func (a *App) canSteal(c *convo) bool {
	if a.login != nil || a.intro != nil || a.overlayOpen() || slackc.ModalOpen() {
		return false
	}
	if c == nil || c.comp == nil || (c.edit != nil && !c.edit.Done()) || a.footerFor(c) != footNone {
		return false
	}
	if hc, ok := c.list.(interface{ HidesComposer() bool }); ok && hc.HidesComposer() {
		return false
	}
	d, ok := c.comp.(interface{ DrawnAt(time.Time) bool })
	return ok && (d.DrawnAt(a.prevNow) || d.DrawnAt(a.frameNow))
}

// stealInto hands keys typeSteal or a message list took to c's input
// (canSteal's): text is typed at its caret, the shortcuts do what they do
// there once it has the focus. It returns the input's focus tag for the
// caller to focus at once (nil: c's input can't take them).
func (a *App) stealInto(gtx layout.Context, c *convo, text string, keys []key.Event) event.Tag {
	if !a.canSteal(c) {
		return nil
	}
	t, ok := c.comp.(interface {
		TypeText(string)
		StealKeys(time.Time, []key.Event)
		FocusTag() any
	})
	if !ok {
		return nil
	}
	if text != "" {
		t.TypeText(text)
	}
	if len(keys) > 0 {
		t.StealKeys(gtx.Now, keys)
	}
	a.Invalidate()
	return t.FocusTag()
}

// wireSteal gives c's message list its keys' way to c's input (kbnav.go
// SetSteal).
func (a *App) wireSteal(c *convo) {
	if l, ok := c.list.(interface {
		SetSteal(func(layout.Context, string, []key.Event) event.Tag)
	}); ok {
		l.SetSteal(func(gtx layout.Context, text string, keys []key.Event) event.Tag {
			return a.stealInto(gtx, c, text, keys)
		})
	}
}
