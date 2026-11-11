package views

// Bulk manage files: the sidebar's edit mode, ported from Slack's
// UnifiedFilesSidebarView (client-unified-files-sidebar-view cNNe, CDN
// build 4d0c78085232). The gear's "Bulk manage files" sets it
// (setIsFileEditMode); every file row then draws a checkbox, loses its ×
// and toggles its pick on a click (setEditModeSelectedFileId), and
// FileOrganizeModeMenu sits under the list:
//
//   - .fileOrganizeModeMenu: a 1px otl-ter top border, padding 16px 16px
//     24px; the header (8px under it) "{count} selected", then " | " and
//     "Clear selection" once something is picked, and the small "close"
//     IconButton ("Done") in theme-content-inv-pry;
//   - the actions (12px apart, full width outline buttons, the 16px icon
//     8px before the text): with picks "Move to…" (arrow-right; its
//     BulkFileSectionMoveMenu opens above) and "Remove from {section}" /
//     "Remove from all sections" (trash), without "Create New Section"
//     (plus);
//   - bulkMoveFilesToCollection moves each file (no per-file toast), then
//     BulkFileActionUndoToast "Moved {n} file(s)." with Undo, and clears
//     the picks; bulkRemoveFilesFromCollection takes each picked file out
//     of its section, toasts "Removed {n} file(s) from their sections."
//     with Undo, and leaves edit mode.

import (
	"context"
	"image"
	"image/color"
	"sort"
	"strconv"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// startOrganize is Bulk manage files (setIsFileEditMode true).
func (f *Files) startOrganize() {
	f.organize = true
	if f.picked == nil {
		f.picked = map[string]bool{}
	}
}

// stopOrganize is the bar's Done: edit mode ends and the picks go.
func (f *Files) stopOrganize() {
	f.organize = false
	f.picked = map[string]bool{}
}

// pickedIDs are the picked files in a stable order.
func (f *Files) pickedIDs() []string {
	var ids []string
	for id, on := range f.picked {
		if on {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// organizeUpdate runs the bar's clicks.
func (f *Files) organizeUpdate(gtx C) {
	if !f.organize {
		return
	}
	if f.orgDone.Clicked(gtx) {
		f.stopOrganize()
	}
	if f.orgClear.Clicked(gtx) {
		f.picked = map[string]bool{}
	}
	if f.orgCreate.Clicked(gtx) {
		f.sectionModal("")
	}
	if f.orgRemove.Clicked(gtx) {
		f.bulkRemove()
	}
	if f.orgMove.Clicked(gtx) {
		f.openMenuAt(f.bulkMoveItems(), f.buttonRect(&f.orgMove, f.orgMoveSize), "top", 0)
	}
}

// bulkMoveItems is BulkFileSectionMoveMenu: "Move to…", the collections,
// then a divider and "Create section" (not on a free plan).
func (f *Files) bulkMoveItems() []popups.MenuItem {
	a := f.h.Core()
	colls, _ := a.FileCollections()
	items := []popups.MenuItem{{Header: "Move to…"}}
	for _, c := range colls {
		c := c
		items = append(items, popups.MenuItem{Key: c.ID, Label: c.Name,
			LabelText: func(gtx C, col color.NRGBA) D { return f.collectionName(gtx, c, false, col) },
			Do:        func() { f.bulkMove(c) }})
	}
	if f.canCreateSection() {
		items = append(items, popups.Sep(), popups.MenuItem{Key: "create-section", Label: "Create section", Icon: "plus",
			Do: func() { f.sectionModalThen("", func(c core.FileCollection) { f.bulkMove(c) }) }})
	}
	return items
}

// bulkMove is bulkMoveFilesToCollection.
func (f *Files) bulkMove(c core.FileCollection) {
	a := f.h.Core()
	ids := f.pickedIDs()
	if len(ids) == 0 {
		return
	}
	from := map[string]string{}
	for _, id := range ids {
		if prev, ok := a.FileCollectionOf(id); ok {
			from[id] = prev.ID
		}
	}
	f.write(func(ctx context.Context, a *core.App) error {
		var first error
		for _, id := range ids {
			if _, err := a.MoveFileToCollection(ctx, id, c.ID); err != nil && first == nil {
				first = err
			}
		}
		return first
	}, func(error) {})
	undo := func() {
		f.write(func(ctx context.Context, a *core.App) error {
			for _, id := range ids {
				if prev := from[id]; prev != "" {
					a.MoveFileToCollection(ctx, id, prev)
				} else {
					a.RemoveFileFromCollection(ctx, id, c.ID)
				}
			}
			return nil
		}, func(error) {})
	}
	f.bulkToast("Moved "+plural(len(ids), "file")+".", undo)
	f.picked = map[string]bool{}
}

// bulkRemove is bulkRemoveFilesFromCollection.
func (f *Files) bulkRemove() {
	a := f.h.Core()
	from := map[string]string{}
	var ids []string
	for _, id := range f.pickedIDs() {
		if c, ok := a.FileCollectionOf(id); ok {
			from[id] = c.ID
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		f.write(func(ctx context.Context, a *core.App) error {
			for _, id := range ids {
				a.RemoveFileFromCollection(ctx, id, from[id])
			}
			return nil
		}, func(error) {})
		undo := func() {
			f.write(func(ctx context.Context, a *core.App) error {
				for _, id := range ids {
					a.MoveFileToCollection(ctx, id, from[id])
				}
				return nil
			}, func(error) {})
		}
		f.bulkToast("Removed "+plural(len(ids), "file")+" from their sections.", undo)
	}
	f.stopOrganize()
}

func (f *Files) bulkToast(text string, undo func()) {
	if tu, ok := f.h.(interface{ ToastUndo(string, func()) }); ok {
		tu.ToastUndo(text, undo)
		return
	}
	f.h.Toast(text)
}

// removeLabel is the remove button's label: "Remove from {sectionName}"
// when every picked file in a section is in the same one, else "Remove
// from all sections".
func (f *Files) removeLabel() string {
	a := f.h.Core()
	seen := map[string]string{}
	for _, id := range f.pickedIDs() {
		if c, ok := a.FileCollectionOf(id); ok {
			seen[c.ID] = c.Name
		}
	}
	if len(seen) == 1 {
		for _, name := range seen {
			return "Remove from " + name
		}
	}
	return "Remove from all sections"
}

// organizeBar is FileOrganizeModeMenu.
func (f *Files) organizeBar(gtx C, sc sbColors) D {
	n := len(f.pickedIDs())
	w := gtx.Constraints.Max.X
	m := op.Record(gtx.Ops)
	d := layout.Inset{Top: 16, Left: 16, Right: 16, Bottom: 24}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return col(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
					return row(gtx, layout.Middle,
						layout.Flexed(1, func(gtx C) D {
							return row(gtx, layout.Middle,
								layout.Rigid(func(gtx C) D { return f.label(gtx, 13, wReg, sc.pry, strconv.Itoa(n)+" selected") }),
								layout.Rigid(func(gtx C) D {
									if n == 0 {
										return D{}
									}
									return f.label(gtx, 13, wReg, sc.pry, " | ")
								}),
								layout.Rigid(func(gtx C) D {
									if n == 0 {
										return D{}
									}
									return f.orgClear.Layout(gtx, func(gtx C) D {
										pointerCursor(gtx)
										return f.label(gtx, 13, wReg, sc.pry, "Clear selection")
									})
								}),
							)
						}),
						hgap(16),
						layout.Rigid(func(gtx C) D {
							// the small "close" IconButton (16px) in theme-content-inv-pry
							return f.sidebarIconButton(gtx, &f.orgDone, &f.orgDoneTip, "close", "", 16, sc.pry, sc) // aria-label "Done", no tooltip
						}),
					)
				})
			}),
			layout.Rigid(func(gtx C) D {
				if n == 0 {
					return f.orgButton(gtx, &f.orgCreate, &f.orgCreateTip, "plus", "Create New Section", sc, nil)
				}
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						return f.orgButton(gtx, &f.orgMove, &f.orgMoveTip, "arrow-right", "Move to…", sc, &f.orgMoveSize)
					}),
					layout.Rigid(func(gtx C) D { return D{Size: image.Pt(0, gtx.Dp(12))} }),
					layout.Rigid(func(gtx C) D {
						l := f.removeLabel()
						return f.orgButton(gtx, &f.orgRemove, &f.orgRemoveTip, "trash", l, sc, nil)
					}),
				)
			}),
		)
	})
	call := m.Stop()
	fill(gtx, image.Rect(0, 0, w, max(1, gtx.Dp(1))), sc.rule)
	call.Add(gtx.Ops)
	return D{Size: image.Pt(w, d.Size.Y)}
}

// orgButton is one of the bar's outline buttons (fileOrganizeModeMenu
// Action: width 100%, 36px, radius 8, the 16px icon 8px before the 15px
// Bold label, centred), with its tooltip above.
func (f *Files) orgButton(gtx C, c *widget.Clickable, tip *popups.TipState, icon, label string, sc sbColors, size *image.Point) D {
	d := c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		w, h := gtx.Constraints.Max.X, gtx.Dp(36)
		r := image.Rect(0, 0, w, h)
		rad := gtx.Dp(8)
		if c.Hovered() {
			rrect(gtx, r, rad, sc.hover)
		}
		border(gtx, r, rad, max(1, gtx.Dp(1)), sc.text)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		layout.Center.Layout(g, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return f.icon(gtx, icon, 16, sc.pry) }),
				hgap(8),
				layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wBold, sc.pry, label) }),
			)
		})
		return D{Size: image.Pt(w, h)}
	})
	if size != nil {
		*size = d.Size
	}
	popups.Tooltip(gtx, f.th, tip, c.Hovered(), d.Size, popups.Above, label, "")
	return d
}
