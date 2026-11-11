package views

// Paging through the Files browser's results, as Slack's unified files
// view does (client-browse-unified-files-view useUnifiedFilesExplorer,
// CDN build 4d0c78085232): the browser shows one page of results at a
// time (useBrowser's results[page]) and, when pageCount > 1, a
// "pagination" row under them (explorer rows 31Gm: margin 8px above and
// below) with Pagination (boot-shared ZIeL):
//
//   - centred, the items 4px apart with 8px around them, caption text
//     (13px/1.25, 400);
//   - the back and forward buttons are circle IconButtons (Slack's
//     caret-left / caret-right chevrons) in the medium 20px box,
//     aria-disabled at the ends;
//   - up to five page numbers (getCurrentPageRange: the page in the
//     middle where it can be), each min 20px wide, 20px tall, 6px side
//     padding, radius 4; the current page in base-inv-hgl-2 with
//     content-inv-pry text, the others content-sec, content-pry on
//     base-sec under the pointer.
//
// handlePageSelect loads the page (and moves the focus to its first
// result: Clack scrolls the list back to the top).

import (
	"image"
	"strconv"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// pageRange is getCurrentPageRange with rangeSize 5.
func pageRange(page, total int) []int {
	const size = 5
	if page <= 0 || total <= 0 {
		return nil
	}
	start := max(min(page-size/2, total-(size-1)), 1)
	end := min(start+size, total+1)
	out := make([]int, 0, size)
	for p := start; p < end; p++ {
		out = append(out, p)
	}
	return out
}

// selectPage is handlePageSelect.
func (f *Files) selectPage(p int) {
	if p < 1 || p > f.pages || p == f.page {
		return
	}
	f.wantPage, f.reload = p, true
	f.list.Position = layout.Position{}
}

// pagesUpdate runs the pagination row's clicks.
func (f *Files) pagesUpdate(gtx C) {
	if f.prevPage.Clicked(gtx) && f.page > 1 {
		f.selectPage(f.page - 1)
	}
	if f.nextPage.Clicked(gtx) && f.page < f.pages {
		f.selectPage(f.page + 1)
	}
	for p, c := range f.pageClicks {
		if c.Clicked(gtx) {
			f.selectPage(p)
		}
	}
}

// pagination is the "pagination" row.
func (f *Files) pagination(gtx C) D {
	if f.pages <= 1 {
		return D{}
	}
	if f.pageClicks == nil {
		f.pageClicks = map[int]*widget.Clickable{}
	}
	return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.N.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = 0 // N keeps the full width as the minimum: the row would stretch and pack left
			return layout.UniformInset(8).Layout(gtx, func(gtx C) D {
				// Slack's caret-left / caret-right are chevrons: Clack's
				// chevron-left / chevron-right (its caret-* are the filled ones)
				ch := []layout.FlexChild{layout.Rigid(func(gtx C) D { return f.pageArrow(gtx, &f.prevPage, "chevron-left", f.page > 1) })}
				for _, p := range pageRange(f.page, f.pages) {
					p := p
					c := f.pageClicks[p]
					if c == nil {
						c = new(widget.Clickable)
						f.pageClicks[p] = c
					}
					ch = append(ch, hgap(4), layout.Rigid(func(gtx C) D { return f.pageNumber(gtx, c, p, p == f.page) }))
				}
				ch = append(ch, hgap(4), layout.Rigid(func(gtx C) D { return f.pageArrow(gtx, &f.nextPage, "chevron-right", f.page < f.pages) }))
				return row(gtx, layout.Middle, ch...)
			})
		})
	})
}

// pageArrow is the back / forward IconButton: a 20px circle, the 18px
// caret (smedium's icon size), content-sec; the hover fill unless
// aria-disabled.
func (f *Files) pageArrow(gtx C, c *widget.Clickable, icon string, on bool) D {
	return c.Layout(gtx, func(gtx C) D {
		s := gtx.Dp(20)
		if on {
			pointerCursor(gtx)
			if c.Hovered() {
				rrect(gtx, image.Rect(0, 0, s, s), s/2, f.p.Hover)
			}
		}
		fg := f.p.Sec
		if !on {
			fg = theme.WithAlpha(fg, fg.A/2)
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(s, s))
		layout.Center.Layout(g, func(gtx C) D { return f.icon(gtx, icon, 18, fg) })
		return D{Size: image.Pt(s, s)}
	})
}

// pageNumber is one page button.
func (f *Files) pageNumber(gtx C, c *widget.Clickable, p int, cur bool) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		label := strconv.Itoa(p)
		tw := f.th.Measure(gtx, 13, wReg, label)
		w := max(gtx.Dp(20), tw+2*gtx.Dp(6))
		h := gtx.Dp(20)
		r := image.Rect(0, 0, w, h)
		fg := f.p.Sec
		switch {
		case cur:
			rrect(gtx, r, gtx.Dp(4), f.p.Green)
			fg = rgb(0xffffff)
		case c.Hovered():
			rrect(gtx, r, gtx.Dp(4), f.p.Surface)
			fg = f.p.Text
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		layout.Center.Layout(g, func(gtx C) D { return f.label(gtx, 13, wReg, fg, label) })
		return D{Size: image.Pt(w, h)}
	})
}
