package carettest

import (
	"image/color"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// editorPlaceholder is the placeholder Lists' inputs draw beside their
// editor (theme.EditorPlaceholder; a plain Label before Gio patch 54).
func editorPlaceholder(th *theme.Theme, gtx layout.Context, size unit.Sp, col color.NRGBA, txt string) layout.Dimensions {
	return th.EditorPlaceholder(gtx, size, col, txt)
}
