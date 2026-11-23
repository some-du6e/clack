package popups

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// timeLinkLayoutT lays tl out with tip's text (time_link_still_test.go).
func timeLinkLayoutT(tl *TimeLink, gtx layout.Context, th *theme.Theme, size image.Point, baseline int, tip func() string) {
	tl.Layout(gtx, th, size, baseline, th.C.Fg, tip)
}
