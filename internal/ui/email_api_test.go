package ui

import (
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// The email tests' reads of what the change adds (a red run replaces
// this file with stubs; docs: red-evidence runs).

// emailFileOf reports whether f is an email file (richtext.File.Email).
func emailFileOf(f *richtext.File) bool { return f != nil && f.Email != nil }

// emailPaneFileID is the email the secondary view's FileDetails shows ("":
// none).
func emailPaneFileID(a *App) string {
	if p, ok := a.secondary.view.(*emailPane); ok && a.secondary.kind == secEmail {
		return p.file().ID
	}
	return ""
}

// emailPaneFull reports whether the FileDetails has the whole email
// (files.info's).
func emailPaneFull(a *App) bool {
	p, ok := a.secondary.view.(*emailPane)
	return ok && p.full != nil && p.view != nil
}

// emailPaneView is the FileDetails' laid-out view.
func emailPaneView(a *App) *richview.View {
	if p, ok := a.secondary.view.(*emailPane); ok {
		return p.view
	}
	return nil
}

// openEmailPane is the header's click on email f of message ch/ts.
func openEmailPane(a *App, f *richtext.File, ch, ts string) { a.OpenEmailFile(f, ch, ts) }
