package richtext

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/some-du6e/clack/internal/chart"
)

// DataTable is a data_table block as Slack's DataTableBlock reads it
// (boot-shared Phw4, its rows cNHK and x+0S, state A1Xw, sort and filter
// UCSE, TSV /R+4; build 9f450b8ad3db). Its keys are decoded on their own
// (bkDataTable), with JavaScript's semantics, so a caption, page_size or
// cell typed unlike another block's same key never fails the message.
type DataTable struct {
	BlockID string
	// Caption is the header's title as React draws it ("" none).
	Caption string
	// Label is the caption when it is truthy ("" otherwise): caption ||
	// "table" names the group ("Compact view of {caption}") and the View
	// button ("View {caption}").
	Label string
	// PageSize is page_size (5): the rows a page shows.
	PageSize float64
	// RowHeader is row_header_column_index (0): the column each body row's
	// header cell is in (role rowheader).
	RowHeader int
	// Salesforce: is_salesforce. The footer always shows, with "Synced
	// from Salesforce".
	Salesforce bool
	// Header is the first row; Cols its length (--column-count).
	Header []DTCell
	Cols   int
	// Body is every other row.
	Body [][]DTCell
	// HasActions: a cell is an action_cell.
	HasActions bool
	// Full draws it as the fullscreen view's table (DataTableFullscreen
	// Virtualized): every row, cells wrapped, no header bar or footer.
	Full bool
	// Err: the render throws (rows not an array of arrays, a header cell
	// that is a string or a number, a text that isn't a string): the
	// surface's errorFallback draws instead.
	Err bool
}

// DTCellKind is a data_table cell's type as renderCellContent reads it.
type DTCellKind uint8

const (
	DTNone   DTCellKind = iota // null, or a type renderCellContent doesn't draw
	DTText                     // raw_text, cell_text: BaseText
	DTNumber                   // raw_number: its text
	DTRich                     // rich_text: the block, inline
	DTAction                   // action_cell: its element
)

// DTCell is a data_table cell.
type DTCell struct {
	Kind DTCellKind
	// Present: the cell is truthy, so its row draws a cell wrapper.
	Present bool
	// Text is a text or number cell's text as drawn.
	Text string
	// Spans are a rich_text cell's content (its sections joined by
	// breaks), and Blocks the same as blocks (the expanded view's).
	Spans  []Span
	Blocks []Block
	// Button is an action_cell's element.
	Button *Button
	// HeaderText: the header row's cell draws (it has a string text).
	HeaderText bool
	// Number and NumberOK are a raw_number cell's value: the numeric sort.
	Number   float64
	NumberOK bool
	// Sort is getCellSortValue: the text the sort and the filter compare.
	Sort string
	// TSV is convertTableBlockToTsv's cell, Plain convertTableCellToPlain
	// Text's (notifications).
	TSV, Plain string
}

// bkDataTable is a data_table block's keys, raw.
type bkDataTable struct {
	Rows            json.RawMessage `json:"rows"`
	PageSize        json.RawMessage `json:"page_size"`
	PageCamel       json.RawMessage `json:"pageSize"`
	RowHeader       json.RawMessage `json:"row_header_column_index"`
	RowHeaderCamel  json.RawMessage `json:"rowHeaderColumnIndex"`
	Salesforce      json.RawMessage `json:"is_salesforce"`
	SalesforceCamel json.RawMessage `json:"isSalesforce"`
	Caption         json.RawMessage `json:"caption"`
	BlockID         json.RawMessage `json:"block_id"`
}

func firstRaw(a, b json.RawMessage) json.RawMessage {
	if len(a) > 0 {
		return a
	}
	return b
}

// dataTableBlock builds a data_table block.
func (c *builder) dataTableBlock(b *bk) Block {
	v := b.table
	if v == nil {
		v = &bkDataTable{}
	}
	dt := &DataTable{BlockID: b.BlockID, PageSize: 5}
	blk := Block{Kind: DataTableBlock, BlockID: b.BlockID, DataTable: dt}
	// pageSize: a = 5 is the default for a missing key; the schema takes
	// only numbers, so anything else is the default here too
	if p, ok := rawValue(firstRaw(v.PageSize, v.PageCamel)); ok {
		if f, ok := p.(float64); ok && !math.IsNaN(f) {
			dt.PageSize = f
		}
	}
	if p, ok := rawValue(firstRaw(v.RowHeader, v.RowHeaderCamel)); ok {
		if f, ok := p.(float64); ok && f >= 0 && f == math.Trunc(f) && f < 1<<20 {
			dt.RowHeader = int(f)
		}
	}
	if s, ok := rawValue(firstRaw(v.Salesforce, v.SalesforceCamel)); ok {
		dt.Salesforce = jsTruthy(s)
	}
	capv, hasCap := rawValue(v.Caption)
	capText, err := chart.ReactText(capv, hasCap)
	if err != nil {
		dt.Err = true
	}
	dt.Caption = capText
	if hasCap && jsTruthy(capv) {
		dt.Label = capText
	}
	// rows: [P, ...ee] = t, then t.some(e => e.some(...)): rows and every
	// row must be arrays
	var rows []json.RawMessage
	if json.Unmarshal(v.Rows, &rows) != nil || len(v.Rows) == 0 || v.Rows[0] != '[' {
		dt.Err = true
		return blk
	}
	for ri, raw := range rows {
		var cells []json.RawMessage
		if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &cells) != nil {
			dt.Err = true
			return blk
		}
		row := make([]DTCell, len(cells))
		for ci, cr := range cells {
			cell, ok := c.dataTableCell(cr, b.BlockID, ri == 0)
			if !ok {
				dt.Err = true
				return blk
			}
			row[ci] = cell
			if cell.Kind == DTAction {
				dt.HasActions = true
			}
		}
		if ri == 0 {
			dt.Header, dt.Cols = row, len(row)
			continue
		}
		dt.Body = append(dt.Body, row)
	}
	return blk
}

// dataTableCell reads one cell (header: the first row's, which the header
// row reads with `"text" in e`, a TypeError for a string or a number).
// False: the render throws.
func (c *builder) dataTableCell(raw json.RawMessage, blockID string, header bool) (DTCell, bool) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return DTCell{}, true
	}
	var cell DTCell
	cell.Present = jsTruthy(v)
	m, isObj := v.(map[string]any)
	if header && cell.Present && !isObj {
		if _, arr := v.([]any); !arr {
			return cell, false // "text" in e on a primitive
		}
	}
	if !isObj {
		return cell, true
	}
	text, hasText := m["text"]
	if header {
		_, cell.HeaderText = text.(string)
	}
	typ, _ := m["type"].(string)
	switch typ {
	case "raw_text", "cell_text":
		cell.Kind = DTText
		// BaseText: nothing for a falsy text; e.includes("\n") throws for
		// any other non-string
		if s, ok := text.(string); ok {
			cell.Text = s
		} else if jsTruthy(text) && !header {
			return cell, false
		}
	case "raw_number":
		cell.Kind = DTNumber
		// renderCellText(e.text): the text as a React child
		t, err := chart.ReactText(text, hasText)
		if err != nil && !header {
			return cell, false
		}
		cell.Text = t
		if f, ok := m["value"].(float64); ok {
			cell.Number, cell.NumberOK = f, true
		}
	case "rich_text":
		cell.Kind = DTRich
		var rb bk
		if json.Unmarshal(raw, &rb) == nil {
			cell.Blocks = c.richText(rb.Elements)
			cell.Spans = flattenSpans(cell.Blocks)
		}
	case "action_cell":
		cell.Kind = DTAction
		var cb struct {
			Element json.RawMessage `json:"element"`
		}
		if json.Unmarshal(raw, &cb) == nil && len(cb.Element) > 0 {
			var el bk
			if json.Unmarshal(cb.Element, &el) == nil && el.Type != "" {
				if btn, ok := c.element(&el, blockID); ok {
					cell.Button = &btn
				}
			}
		}
	}
	// getCellSortValue, convertTableBlockToTsv, convertTableCellToPlainText
	switch cell.Kind {
	case DTText, DTNumber:
		cell.Sort = jsJoinString(text, hasText)
		cell.TSV = cell.Sort
		if jsTruthy(text) {
			cell.Plain = cell.Sort
		}
	case DTRich:
		cell.Sort = PlainText(Doc{Blocks: cell.Blocks})
		cell.TSV, cell.Plain = cell.Sort, cell.Sort
	case DTAction:
		if cell.Button != nil {
			cell.TSV, cell.Plain = cell.Button.Text, cell.Button.Text
		}
	}
	return cell, true
}

// jsTruthy is a JSON value's truthiness.
func jsTruthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case float64:
		return t != 0 && !math.IsNaN(t)
	case string:
		return t != ""
	}
	return true
}

// jsJoinString is a value as Array.prototype.join writes it: null and
// undefined as "", anything else its String().
func jsJoinString(v any, present bool) string {
	if !present || v == nil {
		return ""
	}
	return chart.JSString(v)
}

// TextRows are the header row and the body rows as convertTableCellTo
// PlainText reads each cell (a line break as a space).
func (t *DataTable) TextRows() [][]string {
	out := make([][]string, 0, len(t.Body)+1)
	add := func(cs []DTCell) {
		row := make([]string, len(cs))
		for i := range cs {
			row[i] = strings.ReplaceAll(cs[i].Plain, "\n", " ")
		}
		out = append(out, row)
	}
	if t.Header != nil {
		add(t.Header)
	}
	for _, r := range t.Body {
		add(r)
	}
	return out
}

// DataTableTSV is convertTableBlockToTsv over every row: the cells
// tab-joined, the rows newline-joined, nothing quoted.
func (t *DataTable) TSV() string {
	var b strings.Builder
	write := func(row []DTCell) {
		for i, c := range row {
			if i > 0 {
				b.WriteByte('\t')
			}
			b.WriteString(c.TSV)
		}
	}
	write(t.Header)
	for _, r := range t.Body {
		b.WriteByte('\n')
		write(r)
	}
	return b.String()
}
