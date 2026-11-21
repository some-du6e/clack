package carettest

import (
	"image/color"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// editorPlaceholder is the placeholder Lists' inputs draw beside their
// editor (theme.EditorPlaceholder; a plain Label before Gio patch 54).
func editorPlaceholder(th *theme.Theme, gtx layout.Context, size unit.Sp, col color.NRGBA, txt string) layout.Dimensions {
	return th.EditorPlaceholder(gtx, size, col, txt)
}

// setDeviceScale records the window's unzoomed scale, as Clack's window
// does every frame (widget.SetDeviceScale).
func setDeviceScale(src input.Source, pxPerDp float32) { widget.SetDeviceScale(src, pxPerDp) }
