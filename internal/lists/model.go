// Package lists is Slack Lists' data model (docs/LISTS.md): a list's
// schema (columns), its saved views, and its records (items), parsed from
// what files.info and lists.records.list return, plus the web client's own
// rules for filtering, sorting, searching and grouping records, and the
// typed cell values lists.cells.update takes.
//
// The rules are ported from Slack's web client (modern.vendor modules
// AhXZ, BFYq, mjHr, xWHc, build a84662d2d135); docs/LISTS.md cites each.
//
// Scraping policy: list contents live only in memory.
package lists

import (
	"encoding/json"
	"sort"
	"strings"
)

// Column types (Slack's tuyK enum).
const (
	Text           = "text"
	RichText       = "rich_text"
	Message        = "message"
	Number         = "number"
	Select         = "select"
	MultiSelect    = "multi_select"
	Date           = "date"
	User           = "user"
	Attachment     = "attachment"
	Checkbox       = "checkbox"
	Email          = "email"
	Phone          = "phone"
	Channel        = "channel"
	Currency       = "currency"
	Rating         = "rating"
	CreatedBy      = "created_by"
	LastEditedBy   = "last_edited_by"
	CreatedTime    = "created_time"
	LastEditedTime = "last_edited_time"
	Vote           = "vote"
	Assignee       = "assignee"
	DueDate        = "due_date"
	Completed      = "completed"
	Canvas         = "canvas"
	TodoAssignee   = "todo_assignee"
	TodoDueDate    = "todo_due_date"
	TodoCompleted  = "todo_completed"
	Reference      = "reference"
	Link           = "link"
)

// View types (Slack's LIzA enum). Clack draws Table and Board; Record and
// CanvasInline are internal views Slack never lists as tabs.
const (
	ViewTable        = "table"
	ViewBoard        = "board"
	ViewTimeline     = "timeline"
	ViewList         = "list"
	ViewGallery      = "gallery"
	ViewCalendar     = "calendar"
	ViewMap          = "map"
	ViewRecord       = "record"
	ViewCanvasInline = "canvas_inline"
)

// Choice is a select option.
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Color string `json:"color"` // gray brown orange yellow green blue indigo purple pink red cyan
}

// Options are a column's options (only the type's own are set).
type Options struct {
	Format                string          `json:"format,omitempty"` // select: single_select|multi_select; people/channel/canvas: single_entity|multi_entity; date: single|range; number: standard…
	Choices               []Choice        `json:"choices,omitempty"`
	Precision             *int            `json:"precision,omitempty"`
	DateFormat            string          `json:"date_format,omitempty"`
	Max                   int             `json:"max,omitempty"`   // rating
	Emoji                 string          `json:"emoji,omitempty"` // rating, vote (":star:")
	EmojiTeamID           string          `json:"emoji_team_id,omitempty"`
	EmojiURL              string          `json:"emoji_url,omitempty"`
	ShowMemberName        *bool           `json:"show_member_name,omitempty"`
	NotifyUsers           *bool           `json:"notify_users,omitempty"`
	MarkAsDoneWhenChecked bool            `json:"mark_as_done_when_checked,omitempty"`
	DefaultValue          *string         `json:"default_value,omitempty"`
	DefaultValueTyped     json.RawMessage `json:"default_value_typed,omitempty"`
	CurrencyFormat        string          `json:"currency_format,omitempty"`
	Currency              string          `json:"currency,omitempty"`
}

// Column is one field of a list's schema.
type Column struct {
	ID        string          `json:"id"`
	Key       string          `json:"key"`
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	IsPrimary bool            `json:"is_primary_column"`
	Options   Options         `json:"options"`
	Raw       json.RawMessage `json:"-"`
}

// Multi reports whether the column holds several values (multi_select,
// multi_entity people/channels/canvases).
func (c *Column) Multi() bool {
	switch c.Type {
	case MultiSelect:
		return true
	case Select:
		return c.Options.Format == "multi_select"
	case User, Assignee, TodoAssignee, Channel, Canvas:
		return c.Options.Format == "multi_entity"
	case Attachment:
		return true
	}
	return false
}

// Choice finds a select option by value.
func (c *Column) Choice(v string) (Choice, bool) {
	for _, ch := range c.Options.Choices {
		if ch.Value == v {
			return ch, true
		}
	}
	return Choice{}, false
}

// ShowsMemberName reports whether a people column shows names next to
// avatars (the default) or avatars only.
func (c *Column) ShowsMemberName() bool {
	return c.Options.ShowMemberName == nil || *c.Options.ShowMemberName
}

// ReadOnly reports whether Slack computes the column (nobody edits it).
func (c *Column) ReadOnly() bool {
	switch c.Type {
	case CreatedBy, LastEditedBy, CreatedTime, LastEditedTime:
		return true
	}
	return false
}

// ViewColumn is a column's placement in a view.
type ViewColumn struct {
	ID       string `json:"id"`
	Key      string `json:"key"`
	Visible  bool   `json:"visible"`
	Position string `json:"position"`
	Width    int    `json:"width,omitempty"`
	Wrap     bool   `json:"should_wrap_text,omitempty"`
}

// Sort is one sort key of a view.
type Sort struct {
	Key       string `json:"key"`
	ColumnID  string `json:"column_id,omitempty"`
	Ascending bool   `json:"ascending"`
}

// Filter is one filter of a view. Values are option values, user ids,
// dates (YYYY-MM-DD or a dynamic date such as "this_week"), numbers or
// text, by the column's type.
type Filter struct {
	Key         string            `json:"key"`
	ColumnID    string            `json:"column_id,omitempty"`
	Operator    string            `json:"operator"`
	Values      []string          `json:"-"`
	RawValues   json.RawMessage   `json:"values,omitempty"`
	TypedValues []json.RawMessage `json:"typed_values,omitempty"`
}

// Grouping is a view's group-by (boards: the lanes).
type Grouping struct {
	GroupBy         string `json:"group_by"`
	GroupByColumnID string `json:"group_by_column_id,omitempty"`
}

// View is a saved view of a list (Slack's view tabs: "All items" and
// whatever people made).
type View struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Type          string       `json:"type"`
	Position      string       `json:"position"`
	Columns       []ViewColumn `json:"columns"`
	Sorts         []Sort       `json:"sorts"`
	Filters       []Filter     `json:"filters"`
	Grouping      *Grouping    `json:"grouping,omitempty"`
	IsAllItems    bool         `json:"is_all_items_view"`
	IsLocked      bool         `json:"is_locked"`
	ShowCompleted *bool        `json:"show_completed_items,omitempty"`
	StickLeft     bool         `json:"stick_column_left"`
	CreatedBy     string       `json:"created_by,omitempty"`
	DateCreated   int64        `json:"date_created,omitempty"`
}

// GroupBy is the column key the view groups by ("" when none).
func (v *View) GroupBy() string {
	if v == nil || v.Grouping == nil {
		return ""
	}
	return v.Grouping.GroupBy
}

// Tab reports whether Slack shows the view as a tab (tables and boards;
// the record and canvas-inline views are internal).
func (v *View) Tab() bool { return v.Type != ViewRecord && v.Type != ViewCanvasInline }

// Limits are files.info's list_limits.
type Limits struct {
	RowCount       int  `json:"row_count"`
	RowLimit       int  `json:"row_count_limit"`
	ColumnCount    int  `json:"column_count"`
	ColumnLimit    int  `json:"column_count_limit"`
	ViewCount      int  `json:"view_count"`
	ViewLimit      int  `json:"view_count_limit"`
	ArchivedRows   int  `json:"archived_row_count"`
	MaxAttachments int  `json:"max_attachments_per_cell"`
	OverRows       bool `json:"over_row_maximum"`
	OverColumns    bool `json:"over_column_maximum"`
	OverViews      bool `json:"over_view_maximum"`
}

// Meta is files.info's list_metadata.
type Meta struct {
	Schema            []Column        `json:"schema"`
	SubtaskSchema     []Column        `json:"subtask_schema"`
	Views             []View          `json:"views"`
	Icon              string          `json:"icon"`
	Description       string          `json:"description"`
	DescriptionBlocks json.RawMessage `json:"description_blocks"`
	DefaultView       string          `json:"default_view"`
	TodoMode          bool            `json:"todo_mode"`
	IsTrial           bool            `json:"is_trial"`
	CreationSource    *struct {
		Type        string `json:"type"`
		ReferenceID string `json:"reference_id"`
	} `json:"creation_source,omitempty"`
}

// Column finds a column by key or id.
func (m *Meta) Column(keyOrID string) *Column {
	for i := range m.Schema {
		if m.Schema[i].Key == keyOrID || m.Schema[i].ID == keyOrID {
			return &m.Schema[i]
		}
	}
	return nil
}

// Primary is the primary (name) column.
func (m *Meta) Primary() *Column {
	for i := range m.Schema {
		if m.Schema[i].IsPrimary {
			return &m.Schema[i]
		}
	}
	if len(m.Schema) > 0 {
		return &m.Schema[0]
	}
	return nil
}

// TabViews are the views Slack shows as tabs, in their order (position).
func (m *Meta) TabViews() []*View {
	var out []*View
	for i := range m.Views {
		if m.Views[i].Tab() {
			out = append(out, &m.Views[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}

// View finds a view by id.
func (m *Meta) View(id string) *View {
	for i := range m.Views {
		if m.Views[i].ID == id {
			return &m.Views[i]
		}
	}
	return nil
}

// DefaultTab is the view a list opens on: default_view, else the
// "All items" view, else the first tab.
func (m *Meta) DefaultTab() *View {
	if v := m.View(m.DefaultView); v != nil && v.Tab() {
		return v
	}
	tabs := m.TabViews()
	for _, v := range tabs {
		if v.IsAllItems {
			return v
		}
	}
	if len(tabs) > 0 {
		return tabs[0]
	}
	return nil
}

// VisibleColumns are the view's visible columns in order. A view without
// a column list shows the schema's order (Slack: listSchema keys).
func (m *Meta) VisibleColumns(v *View) []*Column {
	var out []*Column
	if v != nil && len(v.Columns) > 0 {
		vc := append([]ViewColumn(nil), v.Columns...)
		sort.SliceStable(vc, func(i, j int) bool { return vc[i].Position < vc[j].Position })
		for _, c := range vc {
			if !c.Visible {
				continue
			}
			if col := m.Column(c.Key); col != nil {
				out = append(out, col)
			} else if col := m.Column(c.ID); col != nil {
				out = append(out, col)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	for i := range m.Schema {
		out = append(out, &m.Schema[i])
	}
	return out
}

// ColumnWidth is the width a table view gives a column: the view's own,
// else Slack's per-type default (use_get_column_width).
func ColumnWidth(v *View, c *Column) int {
	if v != nil {
		for _, vc := range v.Columns {
			if (vc.Key == c.Key || vc.ID == c.ID) && vc.Width > 0 {
				return vc.Width
			}
		}
	}
	if c.IsPrimary {
		return 384
	}
	switch c.Type {
	case Rating:
		return 32*c.Options.Max + 32
	case Date, TodoDueDate:
		return 200
	case Checkbox:
		return 128
	case TodoCompleted:
		return 32
	case Select, MultiSelect:
		return 128
	case Message:
		return 160
	case User, LastEditedBy, CreatedBy, TodoAssignee:
		return 230
	case Channel:
		return 128
	case Vote:
		return 96
	case CreatedTime, LastEditedTime:
		return 160
	}
	return 250
}

// Wraps reports whether the view wraps the column's text.
func Wraps(v *View, c *Column) bool {
	if v == nil {
		return false
	}
	for _, vc := range v.Columns {
		if vc.Key == c.Key || vc.ID == c.ID {
			return vc.Wrap
		}
	}
	return false
}

func (f *Filter) decode() {
	f.Values = nil
	if len(f.RawValues) == 0 {
		return
	}
	var raw []json.RawMessage
	if json.Unmarshal(f.RawValues, &raw) != nil {
		return
	}
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) == nil {
			f.Values = append(f.Values, s)
			continue
		}
		f.Values = append(f.Values, strings.Trim(string(r), `"`))
	}
}

// ParseMeta decodes files.info's list_metadata.
func ParseMeta(raw json.RawMessage) (Meta, error) {
	var m Meta
	if len(raw) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, err
	}
	var cols struct {
		Schema  []json.RawMessage `json:"schema"`
		Subtask []json.RawMessage `json:"subtask_schema"`
	}
	_ = json.Unmarshal(raw, &cols)
	for i := range m.Schema {
		if i < len(cols.Schema) {
			m.Schema[i].Raw = cols.Schema[i]
		}
	}
	for i := range m.SubtaskSchema {
		if i < len(cols.Subtask) {
			m.SubtaskSchema[i].Raw = cols.Subtask[i]
		}
	}
	for i := range m.Views {
		for j := range m.Views[i].Filters {
			m.Views[i].Filters[j].decode()
		}
	}
	return m, nil
}
