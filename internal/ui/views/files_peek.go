package views

// The rail's Files peek: boot-shared j33M UnifiedFilesPeekView (web client
// build f132f0e78468), a PeekView with the header "Files"
// (quip-files-browser) and the "Starred" UnreadsToggleButton as its header
// button (.unifiedFilesPeekTriggerButtonGroup):
//
//   - UnifiedFilesPeekContent runs its own browser (UNIFIED_FILES_PEEK:
//     the default file types, sorted by LAST_ENGAGED, Recently viewed),
//     separate from the Files tab's filters and search; with Starred on it
//     lists the starred files instead (files.favorites.list).
//   - The list (.unifiedFilesPeekTrigger) is the Files tab's file rows
//     with useListStyling (p-explorer_result--list): square, full width,
//     a line between rows, hover sk_foreground_min_solid, the detail
//     "Last updated …", the star and ⋯ actions.
//   - While the browser loads: 13 file skeleton rows. With nothing:
//     "📁 No files yet" / "Recently viewed canvases and lists will show up
//     here.", or with Starred "⭐ No starred files yet" / "Click the star on
//     any canvas or list to add it here for later".
//   - A row opens the file (handleFileContainerClick, source
//     SLACK_DOCS_TAB_PEEK), then closes the peek.
//
// Not ported: AgentCanvasPeekSection ("Created by agents": Clack has no
// agent canvas ids), the 3p custom sections' starred copy.

import (
	"context"
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var nsQuipFiles = i18n.NS("quip-files-browser")

// filesPeek is the peek's browser state.
type filesPeek struct {
	peeking   bool
	data      async[core.FilePage]
	list      widget.List
	starred   toggle
	close     func()
	lastStar  bool
	starAsked bool
}

// PeekTitle is the card's header.
func (f *Files) PeekTitle() string { return nsQuipFiles.T("Files") }

// PeekOpened is UnifiedFilesPeekContent's mount: the browser loads its
// first page.
func (f *Files) PeekOpened(closePeek func()) {
	pk := &f.pk
	pk.close = closePeek
	pk.list.Position = layout.Position{}
	pk.list.Axis = layout.Vertical
	a := f.h.Core()
	if a == nil {
		return
	}
	pk.data.load(f.h, true, func(ctx context.Context) (core.FilePage, error) {
		return a.Files(ctx, core.FilesOpts{Page: 1})
	})
	if pk.starred.Value {
		f.loadStarred()
	}
}

// LayoutPeekButtons is the Starred toggle.
func (f *Files) LayoutPeekButtons(gtx C) D {
	f.frame()
	return f.peekToggle(gtx, &f.pk.starred, nsQuipFiles.T("Starred"))
}

// peekKeys are the rows the peek lists (ok false while loading).
func (f *Files) peekKeys() ([]fileItem, bool) {
	pk := &f.pk
	if pk.starred.Value {
		st, ok, _ := f.starred.get()
		if !ok || f.starred.busy() && len(st) == 0 {
			return nil, false
		}
		return f.cleanTitles(st), true
	}
	pg, ok, _ := pk.data.get()
	if !ok {
		return nil, false
	}
	return f.cleanTitles(pg.Files), true
}

// LayoutPeek draws the list on the card.
func (f *Files) LayoutPeek(gtx C) D {
	pk := &f.pk
	pk.peeking = true
	defer func() { pk.peeking = false }()
	f.frame()
	f.drainDone()
	if pk.starred.Update(gtx) || pk.starred.Value != pk.lastStar {
		pk.lastStar = pk.starred.Value
		pk.list.Position = layout.Position{}
		if pk.starred.Value && !f.starred.busy() {
			f.loadStarred()
		}
	}
	size := gtx.Constraints.Max
	gtx.Constraints = layout.Exact(size)
	pp := peekPalOf(f.th)
	fill(gtx, image.Rectangle{Max: size}, pp.bg)
	items, ok := f.peekKeys()
	for i := range items {
		c := f.clicksOf(items[i].ID)
		if c.row.Clicked(gtx) {
			it := items[i]
			openFile(f.h, it) // then maybeClosePeek
			if pk.close != nil {
				pk.close()
			}
		}
		if c.star.Clicked(gtx) {
			f.toggleStar(items[i].File)
		}
		if c.menu.Clicked(gtx) {
			f.openMenu(items[i], f.buttonRect(&c.menu, c.menuSize))
		}
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	switch {
	case !ok: // 13 FileListItemSkeleton rows
		return f.scrollList(gtx, &pk.list, 13, func(gtx C, i int) D { return f.peekSkeletonRow(gtx, i) })
	case len(items) == 0:
		if pk.starred.Value {
			return f.peekEmpty(gtx, "star", nsQuipFiles.T("No starred files yet"), nsQuipFiles.T("Click the star on any canvas or list to add it here for later"))
		}
		return f.peekEmpty(gtx, "file_cabinet", nsQuipFiles.T("No files yet"), nsQuipFiles.T("Recently viewed canvases and lists will show up here."))
	}
	return f.scrollList(gtx, &pk.list, len(items), func(gtx C, i int) D {
		return f.peekRow(gtx, &items[i], i == 0)
	})
}

// peekRow is a file row with useListStyling on the card: padding 12px
// 16px, the 1px otl-ter line above all but the first, square.
func (f *Files) peekRow(gtx C, it *fileItem, first bool) D {
	c := f.clicksOf(it.ID)
	pp := peekPalOf(f.th)
	return c.row.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		w := gtx.Constraints.Max.X
		bw := max(1, gtx.Dp(1))
		h := gtx.Dp(12+36+12) + bw
		if c.row.Hovered() { // .unifiedFilesPeekTrigger .c-pillow_file--clickable:hover
			fill(gtx, image.Rect(0, 0, w, h), f.p.Hover)
		}
		if !first {
			fill(gtx, image.Rect(0, 0, w, bw), flatten(pp.otlTer, pp.bg))
		}
		st := op.Offset(image.Pt(0, bw+gtx.Dp(12))).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, gtx.Dp(36)))
		layout.Inset{Left: 16, Right: 16}.Layout(g, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return f.fileTile(gtx, it, 36) }),
				hgap(12),
				layout.Flexed(1, func(gtx C) D {
					return col(gtx,
						layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wBold, pp.fg, it.title()) }),
						layout.Rigid(func(gtx C) D { return f.label(gtx, 13, wReg, f.p.Meta, f.fileMeta(it)) }),
					)
				}),
				layout.Rigid(func(gtx C) D {
					if f.h.Core().FileSectionsOn() || !it.isCanvas() && !it.isList() { // showStarredStatus: canvases and lists
						return D{}
					}
					return f.starButton(gtx, &c.star, f.h.Core().FileStarred(it.ID))
				}),
				layout.Rigid(func(gtx C) D {
					d := f.iconButton(gtx, &c.menu, "kebab", 28, 18, f.p.Meta)
					c.menuSize = d.Size
					return d
				}),
			)
		})
		st.Pop()
		return D{Size: image.Pt(w, h)}
	})
}

// peekSkeletonRow is FileListItemSkeleton in the list's row box.
func (f *Files) peekSkeletonRow(gtx C, i int) D {
	pp := peekPalOf(f.th)
	w := gtx.Constraints.Max.X
	bw := max(1, gtx.Dp(1))
	h := gtx.Dp(12+36+12) + bw
	if i > 0 {
		fill(gtx, image.Rect(0, 0, w, bw), flatten(pp.otlTer, pp.bg))
	}
	x, y := gtx.Dp(16), bw+gtx.Dp(12)
	rrect(gtx, image.Rect(x, y, x+gtx.Dp(36), y+gtx.Dp(36)), gtx.Dp(8), pp.skel)
	tx := x + gtx.Dp(36+12)
	w1 := (w - tx - gtx.Dp(16)) * (45 + (i*17)%30) / 100
	w2 := (w - tx - gtx.Dp(16)) * (30 + (i*23)%25) / 100
	rrect(gtx, image.Rect(tx, y+gtx.Dp(4), tx+w1, y+gtx.Dp(16)), gtx.Dp(4), pp.skel)
	rrect(gtx, image.Rect(tx, y+gtx.Dp(23), tx+w2, y+gtx.Dp(33)), gtx.Dp(4), pp.skel)
	return D{Size: image.Pt(w, h)}
}

// peekEmpty is the isExtended empty state (gkKy): the LARGE emoji before
// the 15px bold title, the description (padding 0 4px) under it, centred.
func (f *Files) peekEmpty(gtx C, emoji, title, desc string) D {
	pp := peekPalOf(f.th)
	size := gtx.Constraints.Max
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(size.X-gtx.Dp(32), size.Y)}
	d := layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(g,
		layout.Rigid(func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					if u, _, ok := f.h.Core().Emoji(emoji); ok && u != "" {
						return f.emojiImg(gtx, u, 32)
					}
					return D{Size: image.Pt(gtx.Dp(32), gtx.Dp(32))}
				}),
				hgap(4),
				layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wBold, pp.fg, title) }),
			)
		}),
		vgap(4),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 4, Right: 4}.Layout(gtx, func(gtx C) D {
				return f.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: pp.sec, Align: text.Middle}, desc)
			})
		}),
	)
	call := m.Stop()
	st := op.Offset(image.Pt((size.X-d.Size.X)/2, max(0, (size.Y-d.Size.Y)/2))).Push(gtx.Ops)
	call.Add(gtx.Ops)
	st.Pop()
	return D{Size: size}
}

// PeekCount is the rows the card lists (-1 while it loads).
func (f *Files) PeekCount() int {
	items, ok := f.peekKeys()
	if !ok {
		return -1
	}
	return len(items)
}

// PeekStarred reports the Starred toggle.
func (f *Files) PeekStarred() bool { return f.pk.starred.Value }
