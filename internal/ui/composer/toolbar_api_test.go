package composer

import (
	"image"

	"gioui.org/layout"
	"gioui.org/unit"
)

// tbMoreRow is the open overflow row's rectangle in the chrome's
// coordinates, as the row last drew (its buttons and Taut's).
func tbMoreRow(c *Composer) image.Rectangle {
	px := c.ui.pxPerDpOr1()
	gtx := layout.Context{Metric: unit.Metric{PxPerDp: px, PxPerSp: px}}
	return crMoreRowRectW(gtx, c.ui.rect[crFootMore], c.ui.more.w)
}

// tbPlugRect is where Taut's composer.addButton buttons were last laid out
// in the toolbar (the chrome's coordinates; empty when they weren't).
func tbPlugRect(c *Composer) image.Rectangle { return c.ui.plugRect }
