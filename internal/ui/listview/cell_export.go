package listview

import (
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/lists"
)

// DrawCell draws one cell's value in a w×h box (vertically centred), as
// the grid does: for the list cards other views draw (a canvas's list
// link card). primary draws its text bold; f may be an empty field.
func DrawCell(gtx layout.Context, hst Host, c *lists.Column, f *lists.Field, w, h int, primary bool) {
	if f == nil {
		f = &lists.Field{Key: c.Key}
	}
	drawCell(gtx, hst, c, f, w, h, cellOpts{primary: primary})
}
