package popups

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/richview"
)

var nsAttachmentMedia = i18n.NS("attachment_media")

// NewImageActionsMenu is AttachmentImageActions' More actions (boot-shared
// UvHM, build 9f450b8ad3db): the image's menu (4Wp/, left-bottom,
// offsetX -4) at btn. Copy link puts href (the image's link,
// externalSourceUrl || url) on the clipboard; with an expandable key,
// "Hide image" collapses it (setExpanded false: the image goes until
// /expand, or its caret where it has one, shows it again).
func NewImageActionsMenu(h Host, btn image.Rectangle, href, key string) *Menu {
	items := []MenuItem{{Label: nsAttachmentMedia.T("Copy link"), DoCtx: func(gtx layout.Context) { copyText(gtx, href) }}}
	if key != "" {
		items = append(items, MenuItem{Label: nsAttachmentMedia.T("Hide image"), Do: func() { richview.SetExpandable(key, false) }})
	}
	return NewMenuOpts(h, items, MenuOpts{Anchor: btn, Popover: "left-bottom", OffsetX: -4})
}
