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

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/chart"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ActionAnchor is a richview action's button for a menu (window
// coordinates): btn when the view gave it in window coordinates, else
// the pointer, which is on the button (the view's owner doesn't map its
// coordinates: Block Kit surfaces, views, popups' own richviews).
func ActionAnchor(btn image.Rectangle, window bool) image.Rectangle {
	if window || theme.WindowPointer == nil {
		return btn
	}
	if p, ok := theme.WindowPointer(); ok {
		return image.Rectangle{Min: p, Max: p}
	}
	return btn
}

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

// dtFull is the fullscreen view (DataTableFullscreenModal): the filter
// field over the table.
type dtFull struct {
	h     Host
	m     *Modal
	full  *richview.TableControl
	v     *richview.View
	ed    widget.Editor
	clear widget.Clickable // the field's Clear, while it has text
	focus bool             // the field takes the focus next frame
	// fired reads the element host's sent actions, fired0 its count at open
	fired  func() uint64
	fired0 uint64
}

// dtFullHandler is the fullscreen view's richview handler: its column
// headers sort the view's own state, and the empty state's Clear filter
// clears the field too.
type dtFullHandler struct {
	topicHandler
	f *dtFull
}

func (d dtFullHandler) DataTableAction(act string, c *richview.TableControl, btn image.Rectangle) {
	switch act {
	case "sort":
		d.h.ShowOverlay(NewDataTableSortMenu(d.h, ActionAnchor(btn, c.Window), c))
	case "clearfilter":
		d.f.clearFilter()
	}
}

// clearFilter is useDataTableState's clearFilter: the filter goes and the
// field takes the focus.
func (f *dtFull) clearFilter() {
	f.ed.SetText("")
	f.full.SetFilter("")
	f.focus = true
}

// OpenDataTableFull is View {caption}: every row under the filter, the
// inline table's sort and filter to start from (focus: the filter takes
// the keyboard focus, Search table).
func OpenDataTableFull(h Host, c *richview.TableControl, focus bool) {
	openDataTableFull(h, c, focus)
}

func openDataTableFull(h Host, c *richview.TableControl, focus bool) *dtFull {
	f := newDTFull(h, c, focus)
	h.ShowOverlay(f.m)
	return f
}

// newDataTableFull is OpenDataTableFull's modal and its table's richview,
// which draws the action cells' buttons through the inline table's
// element host (they send block_actions, as DataTableFullscreenModal's
// rows pass the table's onAction).
func newDataTableFull(h Host, c *richview.TableControl, focus bool) (*Modal, *richview.View) {
	f := newDTFull(h, c, focus)
	return f.m, f.v
}

func newDTFull(h Host, c *richview.TableControl, focus bool) *dtFull {
	t := *c.Table
	t.Full = true
	f := &dtFull{h: h, full: richview.NewTableControl(&t, c), focus: focus}
	f.v = richview.New(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.DataTableBlock, BlockID: t.BlockID, DataTable: &t}}}, richview.Options{NoHoverCards: true, Elements: c.Elements})
	f.v.SetTableControl(&t, f.full)
	f.ed.SingleLine = true
	f.ed.SetText(f.full.FilterText())
	// DataTableFullscreenModal's onAction runs the row's action and closes
	// the modal: the view closes once the message's elements send one
	if fc, ok := c.Elements.(interface{ ActionsFired() uint64 }); ok {
		f.fired, f.fired0 = fc.ActionsFired, fc.ActionsFired()
	}
	f.m = NewModal(h, ModalOpts{Title: t.Caption, Width: 960, Body: f.layout, FillBody: true,
		// the field, then its Clear while it shows
		FocusStops: func() []event.Tag {
			if f.ed.Len() > 0 {
				return []event.Tag{&f.ed, &f.clear}
			}
			return []event.Tag{&f.ed}
		}})
	return f
}

func (f *dtFull) layout(gtx layout.Context) layout.Dimensions {
	if f.fired != nil && f.fired() != f.fired0 {
		f.m.Dismiss()
		return layout.Dimensions{}
	}
	if RichStyle == nil {
		return layout.Dimensions{}
	}
	st := RichStyle(f.h)
	if st == nil {
		return layout.Dimensions{}
	}
	if f.clear.Clicked(gtx) {
		f.clearFilter()
	}
	if f.focus { // until it holds (the modal focuses itself on open)
		if gtx.Focused(&f.ed) {
			f.focus = false
		} else {
			gtx.Execute(key.FocusCmd{Tag: &f.ed})
		}
	}
	for {
		ev, ok := f.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.ChangeEvent); ok {
			f.full.SetFilter(f.ed.Text())
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// .dataTableBlockFullscreenFilter: full width, 8px under it
			return layout.Inset{Bottom: 8}.Layout(gtx, f.filterField)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return f.v.Layout(gtx, st, dtFullHandler{topicHandler{f.h}, f})
		}))
}

// filterField is DataTableBlockFilterInput (boot-shared 1EwK): the small
// input and, while it has text, its Clear button over the input's right
// end (.dataTableBlockFilterClearButton: 13px content-ter, 8px in,
// centred, underlined on hover and focus).
func (f *dtFull) filterField(gtx layout.Context) layout.Dimensions {
	th := f.h.Theme()
	d := Field(gtx, th, &f.ed, nsBlockKit.T("Filter table…"), "search", 32, 15)
	if f.ed.Len() == 0 {
		return d
	}
	cl := colorsOf(th)
	label := nsBlockKit.T("Clear")
	cw := textW(gtx, th, 13, theme.Regular, label)
	fh := d.Size.Y
	at(gtx, image.Pt(d.Size.X-dp(gtx, 8)-cw, 0), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &f.clear, nil, func(gtx layout.Context) layout.Dimensions {
			td := vcenter(gtx, fh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Fg2, label)
			})
			if f.clear.Hovered() || gtx.Focused(&f.clear) { // a text underline, not a border
				col := cl.Fg2
				lh := dp(gtx, 18) // 13px at 1.38463
				y := (fh-lh)/2 + lh - dp(gtx, 2)
				fill(gtx, image.Rect(0, y, td.Size.X, y+max(1, dp(gtx, 1))), col)
			}
			return layout.Dimensions{Size: image.Pt(td.Size.X, fh)}
		})
	})
	return d
}
