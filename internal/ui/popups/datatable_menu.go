package popups

// A data_table block's header (boot-shared Phw4 DataTableBlock, CDN build
// 9f450b8ad3db):
//
//   - More actions (renderMenu, position bottom-left): Copy table (copy),
//     Download table (download); Share table needs share_and_copy_blocks,
//     which is off on the accounts Clack follows;
//   - a column header's menu (DataTableBlockHeaderCell, bottom-left, width
//     auto): the "Sort" heading, Ascending (arrow-up) and Descending
//     (arrow-down) as radio rows, and with the column sorted a separator
//     and Clear Sort (danger);
//   - View {caption}: DataTableFullscreenModal, the caption as its title,
//     the filter, then every row (DataTableFullscreenVirtualized) with the
//     inline table's sort and filter to start from; its action cells'
//     buttons are the message's live elements (the inline table's element
//     host), so they send block_actions.
//
// Copy table puts convertTableBlockToTsv's text on the clipboard (Slack
// adds a slack/block-kit flavor, the block for pasting into its composer,
// which Clack's composer doesn't read). Download table saves the same as
// <filenameSlugFromLabel(caption, "data-table")>.tsv after the IP check
// (DataTableBlock_checkIP, a Grid policy Clack skips); with no caption
// Slack's slug call throws and nothing downloads, as here. The fullscreen
// view is Clack's modal, not Slack Kit's fullscreen one.

import (
	"context"
	"errors"
	"image"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/chart"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// DataTableAction runs a data_table's header button or column header.
func DataTableAction(h Host, act string, c *richview.TableControl, btn image.Rectangle) {
	if c == nil || c.Table == nil {
		return
	}
	switch act {
	case "more":
		h.ShowOverlay(NewDataTableMenu(h, btn, c))
	case "sort":
		h.ShowOverlay(NewDataTableSortMenu(h, btn, c))
	case "view", "search":
		// Search table opens the header's filter field in Slack; Clack
		// filters in the fullscreen view
		OpenDataTableFull(h, c, act == "search")
	}
}

// NewDataTableMenu is the table's More actions menu.
func NewDataTableMenu(h Host, btn image.Rectangle, c *richview.TableControl) *Menu {
	t := c.Table
	items := []MenuItem{
		MenuItem{Label: "Copy table", Icon: "copy", DoCtx: func(gtx layout.Context) { copyText(gtx, t.TSV()) }}.In(nsBlockKit),
		MenuItem{Label: "Download table", Icon: "download", Do: func() { DownloadDataTable(h, t) }}.In(nsBlockKit),
	}
	return NewMenuOpts(h, items, MenuOpts{Anchor: btn, Popover: "bottom-left", TryOpposite: true})
}

// NewDataTableSortMenu is column c.Column's menu.
func NewDataTableSortMenu(h Host, btn image.Rectangle, c *richview.TableControl) *Menu {
	col := c.Column
	sc, desc, sorted := c.SortState()
	sorted = sorted && sc == col
	items := []MenuItem{
		{Header: nsBlockKit.T("Sort")},
		MenuItem{Label: "Ascending", Icon: "arrow-up", Radio: true, Checked: sorted && !desc, Do: func() { c.Sort(col, false) }}.In(nsBlockKit),
		MenuItem{Label: "Descending", Icon: "arrow-down", Radio: true, Checked: sorted && desc, Do: func() { c.Sort(col, true) }}.In(nsBlockKit),
	}
	if sorted {
		items = append(items, Sep(), MenuItem{Label: "Clear Sort", Danger: true, Do: c.ResetSort}.In(nsBlockKit))
	}
	return NewMenuOpts(h, items, MenuOpts{Anchor: btn, Popover: "bottom-left", TryOpposite: true, Width: 200})
}

// DownloadDataTable is Download table.
func DownloadDataTable(h Host, t *richtext.DataTable) {
	if t.Caption == "" {
		return // filenameSlugFromLabel(undefined) throws
	}
	tsv := t.TSV()
	name := chart.FilenameSlug(t.Caption, "data-table") + ".tsv"
	run(h, func(ctx context.Context) (string, error) {
		return lbSave(name, "", []byte(tsv))
	}, func(p string, err error) {
		if errors.Is(err, ErrDownloadCanceled) {
			return
		}
		if err != nil {
			h.Toast("Couldn't download: " + errText(err))
			return
		}
		h.Toast("Saved to " + p)
	})
}

// dtFullHandler is the fullscreen view's richview handler: its column
// headers sort the view's own state.
type dtFullHandler struct{ topicHandler }

func (d dtFullHandler) DataTableAction(act string, c *richview.TableControl, btn image.Rectangle) {
	if act == "sort" {
		d.h.ShowOverlay(NewDataTableSortMenu(d.h, btn, c))
	}
}

// OpenDataTableFull is View {caption}: every row under the filter, the
// inline table's sort and filter to start from (focus: the filter takes
// the keyboard focus, Search table).
func OpenDataTableFull(h Host, c *richview.TableControl, focus bool) {
	m, _ := newDataTableFull(h, c, focus)
	h.ShowOverlay(m)
}

// newDataTableFull is OpenDataTableFull's modal and its table's richview,
// which draws the action cells' buttons through the inline table's
// element host (they send block_actions, as DataTableFullscreenModal's
// rows pass the table's onAction).
func newDataTableFull(h Host, c *richview.TableControl, focus bool) (*Modal, *richview.View) {
	t := *c.Table
	t.Full = true
	full := richview.NewTableControl(&t, c)
	v := richview.New(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.DataTableBlock, BlockID: t.BlockID, DataTable: &t}}}, richview.Options{NoHoverCards: true, Elements: c.Elements})
	v.SetTableControl(&t, full)
	var ed widget.Editor
	ed.SingleLine = true
	ed.SetText(full.FilterText())
	focused := !focus
	title := t.Caption
	m := NewModal(h, ModalOpts{Title: title, Width: 960, Body: func(gtx layout.Context) layout.Dimensions {
		if RichStyle == nil {
			return layout.Dimensions{}
		}
		st := RichStyle(h)
		if st == nil {
			return layout.Dimensions{}
		}
		if !focused {
			gtx.Execute(key.FocusCmd{Tag: &ed})
			focused = true
		}
		for {
			ev, ok := ed.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.ChangeEvent); ok {
				full.SetFilter(ed.Text())
			}
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				// .dataTableBlockFullscreenFilter: full width, 8px under it
				return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return Field(gtx, h.Theme(), &ed, nsBlockKit.T("Filter table…"), "search", 32, 15)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.Layout(gtx, st, dtFullHandler{topicHandler{h}})
			}))
	}})
	return m, v
}
