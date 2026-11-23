package messages

import (
	"image"

	"github.com/some-du6e/clack/internal/ui/richview"
)

// DataBlockRect is a visible message's charts (data_visualization) and
// tables (data_table) as the last frame laid them out, in window
// coordinates.
type DataBlockRect struct {
	TS     string
	Charts []richview.ChartRect
	Tables []richview.DataTableRect
	// View is the message body's richview (its table state: DataTablePage,
	// DataTableRows).
	View *richview.View
}

// DataBlockRects lists the visible messages' charts and tables.
func (v *ChannelView) DataBlockRects() []DataBlockRect { return v.s.dataBlockRects() }

// DataBlockRects is ChannelView.DataBlockRects for the thread.
func (v *ThreadView) DataBlockRects() []DataBlockRect { return v.s.dataBlockRects() }

func (s *listState) dataBlockRects() []DataBlockRect {
	var out []DataBlockRect
	s.eachBody(func(ts string, off image.Point, rv *richview.View) {
		cs, ts2 := rv.ChartRects(), rv.DataTableRects()
		if len(cs) == 0 && len(ts2) == 0 {
			return
		}
		d := DataBlockRect{TS: ts, View: rv}
		for _, c := range cs {
			c.R = c.R.Add(off)
			d.Charts = append(d.Charts, c)
		}
		for _, t := range ts2 {
			bs := make(map[string]image.Rectangle, len(t.Buttons))
			for k, r := range t.Buttons {
				bs[k] = r.Add(off)
			}
			t.Buttons = bs
			rows := make([]image.Rectangle, len(t.Rows))
			for i, r := range t.Rows {
				rows[i] = r.Add(off)
			}
			t.Rows = rows
			d.Tables = append(d.Tables, t)
		}
		out = append(out, d)
	})
	return out
}
