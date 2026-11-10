package canvasview

// Canvas tables, drawn the way Slack's canvas draws them (Quip's
// TableContainer in quip-collab-editor and the canvas-controller CSS),
// not as Block Kit tables:
//
//   - .table-element is table-layout:fixed, border-collapse:collapse; a
//     column is as wide as the table section stores it, else
//     containerWidth/numColumns (TableContainer's column width), so a
//     table without stored widths fills the column evenly.
//   - .table-cell: a 1px --dt_color-otl-sec border on every side, top
//     aligned; its .document-content has padding 8px 16px and min-height
//     46px, with 4px before its first section and after its last.
//   - the header row (.table-cell.header) is --dt_color-surf-pry with its
//     paragraphs at weight 700.
//   - .table-scroller scrolls sideways (overflow: auto hidden) when the
//     columns are wider than the page; its 16px padding is cancelled by
//     -16px margins, so the table starts at the text's edge, and a 20px
//     gradient from the background (.table-overflow-gradient-overlay)
//     marks a side that can scroll.

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// tableScroll is a table's sideways scroll.
type tableScroll struct {
	x   int
	scr gesture.Scroll
}

const (
	cellPadX, cellPadY = 16, 8
	cellEdge           = 4  // first section's margin-top, last's margin-bottom
	cellMinH           = 46 // .document-content min-height (border-box)
	tableGutter        = 16 // the scroller's padding, cancelled by its margins
	tableFade          = 20 // the overflow gradients
)

// tableCells builds each cell's blocks; the header row's paragraphs are
// bold (.table-cell.header .section[data-section-style="0"]{font-weight:700}).
func (v *View) tableCells(d *core.CanvasDoc, b *canvas.Block) [][]cell {
	rows := make([][]cell, len(b.Rows))
	for r, row := range b.Rows {
		rows[r] = make([]cell, len(row))
		for c, cl := range row {
			bs := cl.Blocks
			if b.Header && r == 0 {
				bs = append([]canvas.Block(nil), bs...)
				for i := range bs {
					if bs[i].Kind == canvas.Paragraph {
						bs[i].Spans = withStyle(bs[i].Spans, richtext.Bold)
					}
				}
			}
			kids := v.makeBlocks(d, bs, nil)
			for _, k := range kids {
				k.inCell = true
			}
			rows[r][c] = cell{kids: kids}
		}
	}
	return rows
}

// colWidths are the table's column widths in px for a column w wide.
func colWidths(gtx layout.Context, b *canvas.Block, n, w int) []int {
	ws := make([]int, n)
	if len(b.ColWidths) == n {
		for i, cw := range b.ColWidths {
			ws[i] = max(gtx.Dp(1), gtx.Dp(unit.Dp(cw)))
		}
		return ws
	}
	w -= gtx.Dp(1)      // the collapsed right border
	for i := range ws { // containerWidth / numColumns
		ws[i] = w*(i+1)/n - w*i/n
	}
	return ws
}

// layoutTable draws b's table w wide and returns its height.
func (v *View) layoutTable(gtx layout.Context, b *blk, w int) int {
	th := v.h.Theme()
	c := &th.C
	n := 0
	for _, r := range b.cells {
		n = max(n, len(r))
	}
	if n == 0 {
		return 0
	}
	ws := colWidths(gtx, b.b, n, w)
	total := gtx.Dp(1)
	for _, cw := range ws {
		total += cw
	}
	// sideways scroll when the columns overflow the page
	s := &b.tscr
	maxX := max(0, total-w)
	if maxX > 0 {
		d := s.scr.Update(gtx.Metric, gtx.Source, gtx.Now, gesture.Horizontal,
			pointer.ScrollRange{Min: -s.x, Max: maxX - s.x}, pointer.ScrollRange{})
		s.x += d
	}
	s.x = min(max(s.x, 0), maxX)

	// rows: lay each cell out (recorded), the row as tall as its tallest
	edge := gtx.Dp(1)
	type laid struct {
		call op.CallOp
		h    int
	}
	var heights []int
	var calls [][]laid
	for _, row := range b.cells {
		rh := gtx.Dp(cellMinH)
		cs := make([]laid, len(row))
		for ci := 0; ci < len(row) && ci < n; ci++ {
			inner := max(1, ws[ci]-edge-2*gtx.Dp(cellPadX))
			m := op.Record(gtx.Ops)
			kh := v.layoutKids(gtx, row[ci].kids, gtx.Dp(cellPadX), inner)
			call := m.Stop()
			ch := 2*gtx.Dp(cellPadY) + 2*gtx.Dp(cellEdge) + kh
			cs[ci] = laid{call, ch}
			rh = max(rh, ch)
		}
		heights = append(heights, rh+edge) // with its top border (collapsed)
		calls = append(calls, cs)
	}
	height := edge
	for _, h := range heights {
		height += h
	}

	clipR := image.Rect(-gtx.Dp(tableGutter), 0, w+gtx.Dp(tableGutter), height+gtx.Dp(4)) // the scroller's 4px bottom border
	defer clip.Rect(clipR).Push(gtx.Ops).Pop()
	if maxX > 0 {
		s.scr.Add(gtx.Ops)
	}
	func() {
		defer op.Offset(image.Pt(-s.x, 0)).Push(gtx.Ops).Pop()
		y := 0
		for r, cs := range calls {
			x := 0
			for ci := 0; ci < n; ci++ {
				cr := image.Rect(x, y, x+ws[ci]+edge, y+heights[r]+edge)
				if b.b.Header && r == 0 {
					theme.Fill(gtx, cr, c.BgInset) // --dt_color-surf-pry
				}
				border(gtx, cr, edge, c.BorderStrong) // --dt_color-otl-sec
				if ci < len(cs) {
					func() {
						defer op.Offset(image.Pt(x+edge, y+edge+gtx.Dp(cellPadY)+gtx.Dp(cellEdge))).Push(gtx.Ops).Pop()
						cs[ci].call.Add(gtx.Ops)
					}()
				}
				x += ws[ci]
			}
			y += heights[r]
		}
	}()
	if s.x > 0 {
		fade(gtx, image.Rect(-gtx.Dp(tableGutter), 0, -gtx.Dp(tableGutter)+gtx.Dp(tableFade), height), c.Bg, true)
	}
	if s.x < maxX {
		fade(gtx, image.Rect(w+gtx.Dp(tableGutter)-gtx.Dp(tableFade), 0, w+gtx.Dp(tableGutter), height), c.Bg, false)
	}
	return height + gtx.Dp(4)
}

// border strokes r's edges w px wide inside r.
func border(gtx layout.Context, r image.Rectangle, w int, col color.NRGBA) {
	theme.Fill(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+w), col)
	theme.Fill(gtx, image.Rect(r.Min.X, r.Max.Y-w, r.Max.X, r.Max.Y), col)
	theme.Fill(gtx, image.Rect(r.Min.X, r.Min.Y, r.Min.X+w, r.Max.Y), col)
	theme.Fill(gtx, image.Rect(r.Max.X-w, r.Min.Y, r.Max.X, r.Max.Y), col)
}

// fade is the overflow gradient: bg at the outer edge to clear inwards
// (fromLeft: the left edge).
func fade(gtx layout.Context, r image.Rectangle, bg color.NRGBA, fromLeft bool) {
	clear := bg
	clear.A = 0
	a, b := bg, clear
	p0, p1 := f32.Pt(float32(r.Min.X), 0), f32.Pt(float32(r.Max.X), 0)
	if !fromLeft {
		a, b = clear, bg
	}
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	paint.LinearGradientOp{Stop1: p0, Color1: a, Stop2: p1, Color2: b}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}
