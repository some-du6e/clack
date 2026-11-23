package ui

import "gioui.org/app/winclip"

// watchClipBusy says when a window's own copy or paste (Gio's: a text
// field's Ctrl+V, a Copy link) didn't happen because another app kept the
// Windows clipboard open (winclip.OnBusy; only Windows reports it). Before,
// it was dropped silently, and Copy link still said the link was copied.
func (a *App) watchClipBusy() {
	winclip.OnBusy = func(write bool) {
		msg := "The clipboard is busy. Try pasting again."
		if write {
			msg = "Couldn't copy: the clipboard is busy. Try again."
		}
		a.Post(func() { a.Toast(msg) })
	}
}
