package views

import (
	"image"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// bodyMenu is this frame's right-click on m's body (content_menu.go).
func bodyMenu(b *base, gtx C, ph popups.Host, at image.Point, m core.Message) (popups.Overlay, bool) {
	return b.contentMenu(gtx, ph, at, m)
}
