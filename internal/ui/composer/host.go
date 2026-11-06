// Package composer is the message composer: a rich WYSIWYG editor with
// Slack's formatting toolbar, autocomplete for @ # : /, attachments,
// drafts, typing indicators and edit mode.
package composer

import "github.com/some-du6e/yoloslackgoclient/internal/ui/popups"

// Host is what the composer needs from the app shell. It is popups.Host
// (the shared names in docs/NATIVE.md), so *ui.App satisfies it without an
// adapter and the composer can open popups' menus, modals and tooltips.
type Host interface {
	popups.Host
}
