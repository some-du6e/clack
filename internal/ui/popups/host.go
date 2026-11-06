// Package popups draws everything that floats above yoloslack's shell:
// menus, popovers, modals, tooltips and toasts, plus the feature popups
// built on them (emoji picker, quick switcher / search, profile card,
// lightbox, and the message/channel/team/user menus with their actions).
//
// Every popup is an Overlay. The ui root stacks overlays, draws them on top
// of the shell with full-window constraints and routes Escape to Dismiss.
// Overlays also close themselves on Escape and outside clicks.
//
// Scraping policy: nothing here writes message contents to disk. The only
// file this package persists is the emoji frecency list (emoji names).
package popups

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Overlay is a popup drawn above the shell. It is an alias of an unnamed
// interface so that any package's Host can declare ShowOverlay with either
// popups.Overlay or the same literal interface.
type Overlay = interface {
	Layout(gtx layout.Context) layout.Dimensions
	Dismissed() bool
}

// Host is what popups needs from the ui root (docs/NATIVE.md names, so one
// *ui.App satisfies every package's Host).
type Host interface {
	Core() *core.App
	Theme() *theme.Theme
	Images() *imgcache.Cache
	Invalidate()
	Navigate(route string)
	OpenThread(channel, ts string)
	CloseSecondary()
	ShowOverlay(o Overlay)
	OpenEmojiPicker(anchor image.Point, onPick func(name string))
	OpenProfile(anchor image.Point, userID string)
	OpenMessageMenu(anchor image.Point, m core.Message)
	OpenLightbox(img richtext.Image, m core.Message)
	Toast(text string)
}

// dismisser is implemented by every overlay here; the root calls Dismiss on
// Escape.
type dismisser interface{ Dismiss() }

// base carries the dismissed flag shared by all overlays.
type base struct {
	done    bool
	onClose func()
}

func (b *base) Dismissed() bool { return b.done }

// Dismiss closes the overlay (idempotent).
func (b *base) Dismiss() {
	if b.done {
		return
	}
	b.done = true
	if b.onClose != nil {
		b.onClose()
	}
}
