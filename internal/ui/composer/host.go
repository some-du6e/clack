// Package composer is the message composer: a rich WYSIWYG editor with
// Slack's formatting toolbar, autocomplete for @ # : /, attachments,
// drafts, typing indicators and edit mode.
package composer

import (
	"image"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Host is what the composer needs from the app shell. The method names
// match the shared Host convention in docs/NATIVE.md, so *ui.App satisfies
// it without an adapter.
type Host interface {
	Core() *core.App
	Theme() *theme.Theme
	Images() *imgcache.Cache
	Invalidate()
	Navigate(route string)
	OpenEmojiPicker(anchor image.Point, onPick func(name string))
	Toast(text string)
}
