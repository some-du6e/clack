package views

// Later's list records: Slack's ListRecord row (boot-render nckZ, CDN
// build fa938a086569). Inside the saved row (m.$ with isLaterTab), the
// context line reads "List item" (translated only under lists_in_later,
// Hm0j v: `I ? $.t("List item") : "List item"`) with the reminder chip,
// then the list's FileEntity (UZv6, forceIconThumbnail): the list icon,
// the list's title, and as its detail the item's name
// (fields.fallback.name, "Untitled item"; 15px, two lines:
// .p-saved_item--list_record__name). Slack draws nothing until the list
// file is loaded (`return j ? … : null`); Clack keeps the row's place with
// its context line while it loads. A click opens the item
// (openListRecordDetails, itemDetailsInPrimary off on the To-dos page);
// a deleted list or item opens Slack's remove modal (Clack: the item's
// pane, which says it was deleted).

import (
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
)

// laterRecordLabel is the row's "List item".
func laterRecordLabel(c *core.App) string {
	if c != nil && c.ListsInLater() {
		return nsLater.T("List item")
	}
	return "List item"
}

// listRecordBody is the ListRecord row's content.
func (s *savedBase) listRecordBody(gtx C, it *core.SavedItem) D {
	c := s.h.Core()
	info, ok := c.SavedListRecordInfo(it.ItemID, it.TS)
	if !ok {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(300 * time.Millisecond)}) // until the list arrives
	}
	name := info.Name
	if name == "" {
		name = nsLater.T("Untitled item")
	}
	icon := func(gtx C) D {
		sz := gtx.Dp(laterAvatar)
		rrect(gtx, image.Rectangle{Max: image.Pt(sz, sz)}, gtx.Dp(8), s.p.ReadFill)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(sz, sz))
		layout.Center.Layout(g, func(gtx C) D { return s.icon(gtx, "lists", 20, s.p.Text) })
		return D{Size: image.Pt(sz, sz)}
	}
	return col(gtx,
		layout.Rigid(func(gtx C) D { return s.recordContext(gtx, it) }),
		layout.Rigid(func(gtx C) D {
			return row(gtx, layout.Start,
				layout.Rigid(icon),
				hgap(8), // .c-base_entity__avatar margin-right 8
				layout.Flexed(1, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					if !ok {
						return col(gtx,
							layout.Rigid(func(gtx C) D { return s.skeletonBar(gtx, 140, 22) }),
							layout.Rigid(func(gtx C) D { return s.skeletonBar(gtx, 220, 22) }),
						)
					}
					title := info.ListTitle
					if info.Deleted && title == "" {
						title = i18n.T("files", "Deleted list")
					}
					return col(gtx,
						layout.Rigid(func(gtx C) D { return s.para(gtx, 15, 22, wBlack, s.p.Text, 1, title) }),
						layout.Rigid(func(gtx C) D { return s.para(gtx, 15, 22, wReg, s.p.Text, 2, name) }),
					)
				}),
			)
		}),
	)
}

// recordContext is the row's context line: "List item" and the chip.
func (s *savedBase) recordContext(gtx C, it *core.SavedItem) D {
	gtx.Constraints.Min.Y = gtx.Dp(laterCtxH)
	return layout.W.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.Y = 0
		return row(gtx, layout.Middle,
			layout.Flexed(1, func(gtx C) D {
				return s.para(gtx, 13, 18, wReg, s.p.Muted, 1, laterRecordLabel(s.h.Core())) // .p-saved_item__link: 13px, weight initial
			}),
			layout.Rigid(func(gtx C) D { return s.savedChip(gtx, it) }),
		)
	})
}

// skeletonBar is a placeholder line h tall: a 12px rounded bar w wide,
// centred in it.
func (s *savedBase) skeletonBar(gtx C, w unit.Dp, h unit.Dp) D {
	lh := gtx.Dp(h)
	bw := min(gtx.Dp(w), gtx.Constraints.Max.X)
	rrect(gtx, image.Rect(0, lh/2-gtx.Dp(6), bw, lh/2+gtx.Dp(6)), gtx.Dp(4), s.p.ReadFill)
	return D{Size: image.Pt(gtx.Constraints.Max.X, lh)}
}

// openListRecord is the row's click: the item in the list's pane (a
// deleted one's pane says so; Slack opens its remove modal, which Clack
// doesn't have).
func (s *savedBase) openListRecord(it *core.SavedItem) {
	if o, ok := s.h.(interface{ OpenListItem(listID, recID string) }); ok {
		o.OpenListItem(it.ItemID, it.TS)
	}
}
