package core

// Slack lists, as the web client loads and edits them (recorded in the real
// client and its bundle, shapes only):
//
//   - files.info → list_metadata {schema: columns, views, description_blocks,
//     subtask_schema, todo_mode}, list_limits.
//   - lists.records.list {list_id, include_subtasks, archived=false,
//     exclude_deprecated_values} → records [{id, fields: [{key, column_id,
//     text|rich_text|select|checkbox|date|user|number|…}], position,
//     parent_record_id}], response_metadata.next_cursor.
//   - lists.cells.update {list_id, cells: [{row_id, column_id, <value>}]}.
//   - lists.records.create {list_id, fields: [{column_id, <value>}]},
//     lists.records.delete {list_id, id}.
//   - RTM list_record_created / _updated / _removed, list_cells_updated →
//     Event{Type: "canvas"} (canvas.go).
//
// The public slackLists.* methods reject session tokens
// (not_allowed_token_type); these are the client's own.
//
// Scraping policy: list contents live only in memory.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// ListColumn is one field of a list's schema.
type ListColumn struct {
	ID, Key, Name string
	// Type: text | select | multi_select | checkbox | date | user | number |
	// rating | link | email | phone | channel | attachment | timestamp |
	// todo_completed | todo_assignee | todo_due_date | vote | canvas | …
	Type    string
	Primary bool
	Format  string // select: single_select | multi_select; number: …
	Choices []ListChoice
	// Max is the rating scale (stars); Precision is a number's decimals.
	Max, Precision int
	Emoji          string // rating emoji
	Raw            json.RawMessage
}

// ListChoice is a select option.
type ListChoice struct{ Value, Label, Color string }

// Choice finds a select option by value.
func (c *ListColumn) Choice(v string) (ListChoice, bool) {
	for _, ch := range c.Choices {
		if ch.Value == v {
			return ch, true
		}
	}
	return ListChoice{}, false
}

// ListViewColumn is a column's placement in one view.
type ListViewColumn struct {
	Key, ID  string
	Visible  bool
	Width    int
	Position string
	Wrap     bool
}

// ListSort is one sort key of a view.
type ListSort struct {
	Key  string // column key
	Desc bool
}

// ListFilter is one filter of a view.
type ListFilter struct {
	Key      string // column key
	Operator string // is | is_not | contains | is_empty | is_not_empty | …
	Values   []string
}

// ListView is a saved view (Slack: "All items", tables, boards).
type ListView struct {
	ID, Name string
	Type     string // table | board | record | calendar | …
	Columns  []ListViewColumn
	GroupBy  string // board: column key the columns come from
	Sort     []ListSort
	Filters  []ListFilter
	AllItems bool
	// ShowCompleted shows checked to-do rows (todo lists).
	ShowCompleted bool
}

// ListValue is one cell's value. Only the fields of the column's type are set.
type ListValue struct {
	Text     string
	RichText json.RawMessage // rich_text blocks (text columns)
	Doc      []richtext.Block
	Select   []string // option values
	Checkbox *bool
	Number   []float64
	Date     []string // YYYY-MM-DD
	User     []string
	Channel  []string
	Rating   []int
	Link     []ListLink
	Email    []string
	Phone    []string
	Files    []string // attachment file ids
	Time     []int64  // timestamp columns (unix seconds)
	Raw      json.RawMessage
}

// ListLink is a link cell entry.
type ListLink struct {
	URL, Text string
}

// Empty reports whether the value is blank.
func (v ListValue) Empty() bool {
	return v.Text == "" && len(v.Select) == 0 && v.Checkbox == nil && len(v.Number) == 0 && len(v.Date) == 0 &&
		len(v.User) == 0 && len(v.Channel) == 0 && len(v.Rating) == 0 && len(v.Link) == 0 && len(v.Email) == 0 &&
		len(v.Phone) == 0 && len(v.Files) == 0 && len(v.Time) == 0
}

// Checked is the checkbox value (false when unset).
func (v ListValue) Checked() bool { return v.Checkbox != nil && *v.Checkbox }

// ListRecord is one row (item) of a list.
type ListRecord struct {
	ID        string
	Fields    map[string]ListValue // by column key
	Created   int64
	Updated   int64
	CreatedBy string
	UpdatedBy string
	Position  string
	Parent    string // subtasks: the parent record id
}

// List is a loaded list.
type List struct {
	Meta        CanvasMeta
	Title       string
	Description []richtext.Block
	Columns     []ListColumn
	Views       []ListView
	Records     []ListRecord
	TodoMode    bool
	DefaultView string
}

// Column finds a column by key or id.
func (l *List) Column(keyOrID string) *ListColumn {
	for i := range l.Columns {
		if l.Columns[i].Key == keyOrID || l.Columns[i].ID == keyOrID {
			return &l.Columns[i]
		}
	}
	return nil
}

// Primary returns the primary (name) column.
func (l *List) Primary() *ListColumn {
	for i := range l.Columns {
		if l.Columns[i].Primary {
			return &l.Columns[i]
		}
	}
	if len(l.Columns) > 0 {
		return &l.Columns[0]
	}
	return nil
}

type listColumnRaw struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Key       string `json:"key"`
	Type      string `json:"type"`
	IsPrimary bool   `json:"is_primary_column"`
	Options   struct {
		Format  string `json:"format"`
		Choices []struct {
			Value string `json:"value"`
			Label string `json:"label"`
			Color string `json:"color"`
		} `json:"choices"`
		Max       int    `json:"max"`
		Precision int    `json:"precision"`
		Emoji     string `json:"emoji"`
	} `json:"options"`
}

type listViewRaw struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Position string `json:"position"`
	Columns  []struct {
		Visible  bool   `json:"visible"`
		Key      string `json:"key"`
		ID       string `json:"id"`
		Position string `json:"position"`
		Width    int    `json:"width"`
		Wrap     bool   `json:"should_wrap_text"`
	} `json:"columns"`
	IsAllItems    bool            `json:"is_all_items_view"`
	ShowCompleted *bool           `json:"show_completed_items"`
	Grouping      json.RawMessage `json:"grouping"`
	GroupBy       string          `json:"group_by"`
	Sort          json.RawMessage `json:"sort"`
	SortBy        json.RawMessage `json:"sort_by"`
	Filters       json.RawMessage `json:"filters"`
}

type listMetaRaw struct {
	Schema            []json.RawMessage `json:"schema"`
	Views             []listViewRaw     `json:"views"`
	DescriptionBlocks json.RawMessage   `json:"description_blocks"`
	TodoMode          bool              `json:"todo_mode"`
	DefaultView       string            `json:"default_view"`
}

func parseListMeta(r *canvasFileRaw, l *List, rich func(json.RawMessage) []richtext.Block) {
	var m listMetaRaw
	if len(r.ListMetadata) == 0 || json.Unmarshal(r.ListMetadata, &m) != nil {
		return
	}
	l.TodoMode, l.DefaultView = m.TodoMode, m.DefaultView
	for _, raw := range m.Schema {
		var c listColumnRaw
		if json.Unmarshal(raw, &c) != nil {
			continue
		}
		col := ListColumn{ID: c.ID, Key: c.Key, Name: c.Name, Type: c.Type, Primary: c.IsPrimary,
			Format: c.Options.Format, Max: c.Options.Max, Precision: c.Options.Precision, Emoji: c.Options.Emoji, Raw: raw}
		for _, ch := range c.Options.Choices {
			col.Choices = append(col.Choices, ListChoice{Value: ch.Value, Label: ch.Label, Color: ch.Color})
		}
		l.Columns = append(l.Columns, col)
	}
	sort.SliceStable(m.Views, func(i, j int) bool { return m.Views[i].Position < m.Views[j].Position })
	for _, v := range m.Views {
		view := ListView{ID: v.ID, Name: v.Name, Type: v.Type, AllItems: v.IsAllItems, ShowCompleted: true, GroupBy: v.GroupBy}
		if v.ShowCompleted != nil {
			view.ShowCompleted = *v.ShowCompleted
		}
		for _, c := range v.Columns {
			view.Columns = append(view.Columns, ListViewColumn{Key: c.Key, ID: c.ID, Visible: c.Visible, Width: c.Width,
				Position: c.Position, Wrap: c.Wrap})
		}
		sort.SliceStable(view.Columns, func(i, j int) bool { return view.Columns[i].Position < view.Columns[j].Position })
		if len(v.Grouping) > 0 && v.Grouping[0] == '{' {
			var g struct {
				GroupBy  string `json:"group_by"`
				ColumnID string `json:"column_id"`
				Key      string `json:"key"`
			}
			_ = json.Unmarshal(v.Grouping, &g)
			view.GroupBy = firstNonEmpty(g.GroupBy, g.Key, g.ColumnID, view.GroupBy)
		}
		view.Sort = parseListSort(firstRaw(v.Sort, v.SortBy))
		view.Filters = parseListFilters(v.Filters)
		l.Views = append(l.Views, view)
	}
	if rich != nil {
		l.Description = rich(m.DescriptionBlocks)
	}
}

func firstRaw(rs ...json.RawMessage) json.RawMessage {
	for _, r := range rs {
		if len(r) > 0 && string(r) != "null" {
			return r
		}
	}
	return nil
}

func parseListSort(raw json.RawMessage) []ListSort {
	if len(raw) == 0 {
		return nil
	}
	type one struct {
		Key       string `json:"key"`
		ColumnKey string `json:"column_key"`
		ColumnID  string `json:"column_id"`
		Direction string `json:"direction"`
		Order     string `json:"order"`
		Desc      bool   `json:"desc"`
	}
	var list []one
	if raw[0] == '{' {
		var o one
		if json.Unmarshal(raw, &o) == nil {
			list = []one{o}
		}
	} else {
		_ = json.Unmarshal(raw, &list)
	}
	var out []ListSort
	for _, o := range list {
		k := firstNonEmpty(o.Key, o.ColumnKey, o.ColumnID)
		if k == "" {
			continue
		}
		d := strings.ToLower(firstNonEmpty(o.Direction, o.Order))
		out = append(out, ListSort{Key: k, Desc: o.Desc || d == "desc" || d == "descending"})
	}
	return out
}

func parseListFilters(raw json.RawMessage) []ListFilter {
	if len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var list []struct {
		Key       string            `json:"key"`
		ColumnKey string            `json:"column_key"`
		ColumnID  string            `json:"column_id"`
		Operator  string            `json:"operator"`
		Op        string            `json:"op"`
		Values    []json.RawMessage `json:"values"`
		Value     json.RawMessage   `json:"value"`
	}
	_ = json.Unmarshal(raw, &list)
	var out []ListFilter
	for _, f := range list {
		lf := ListFilter{Key: firstNonEmpty(f.Key, f.ColumnKey, f.ColumnID), Operator: firstNonEmpty(f.Operator, f.Op)}
		vals := f.Values
		if len(f.Value) > 0 {
			vals = append(vals, f.Value)
		}
		for _, v := range vals {
			if s := looseString(v); s != "" {
				lf.Values = append(lf.Values, s)
			}
		}
		if lf.Key != "" {
			out = append(out, lf)
		}
	}
	return out
}

type listFieldRaw struct {
	Key        string            `json:"key"`
	ColumnID   string            `json:"column_id"`
	Text       string            `json:"text"`
	RichText   json.RawMessage   `json:"rich_text"`
	Select     []string          `json:"select"`
	Checkbox   *bool             `json:"checkbox"`
	Number     []json.RawMessage `json:"number"`
	Date       []string          `json:"date"`
	User       []string          `json:"user"`
	Channel    []string          `json:"channel"`
	Rating     []json.RawMessage `json:"rating"`
	Email      []string          `json:"email"`
	Phone      []string          `json:"phone"`
	Attachment []json.RawMessage `json:"attachment"`
	Timestamp  []json.RawMessage `json:"timestamp"`
	Link       []struct {
		OriginalURL string `json:"original_url"`
		URL         string `json:"url"`
		DisplayName string `json:"display_name"`
		Title       string `json:"title"`
	} `json:"link"`
}

type listRecordRaw struct {
	ID             string         `json:"id"`
	DateCreated    int64          `json:"date_created"`
	CreatedBy      string         `json:"created_by"`
	UpdatedBy      string         `json:"updated_by"`
	Updated        string         `json:"updated_timestamp"`
	Position       string         `json:"position"`
	ParentRecordID string         `json:"parent_record_id"`
	Fields         []listFieldRaw `json:"fields"`
}

func (a *App) listValue(f listFieldRaw) ListValue {
	v := ListValue{Text: f.Text, RichText: f.RichText, Select: f.Select, Checkbox: f.Checkbox, Date: f.Date,
		User: f.User, Channel: f.Channel, Email: f.Email, Phone: f.Phone}
	if len(f.RichText) > 0 && string(f.RichText) != "null" {
		v.Doc = a.richBlocks(f.RichText)
		if v.Text == "" {
			v.Text = strings.TrimSpace(richtext.PlainText(richtext.Doc{Blocks: v.Doc}))
		}
	}
	for _, n := range f.Number {
		if x, err := strconv.ParseFloat(strings.Trim(string(n), `"`), 64); err == nil {
			v.Number = append(v.Number, x)
		}
	}
	for _, n := range f.Rating {
		if x, err := strconv.Atoi(strings.Trim(string(n), `"`)); err == nil {
			v.Rating = append(v.Rating, x)
		}
	}
	for _, n := range f.Timestamp {
		if x, err := strconv.ParseInt(strings.Trim(string(n), `"`), 10, 64); err == nil {
			v.Time = append(v.Time, x)
		}
	}
	for _, at := range f.Attachment {
		if s := looseString(at); s != "" {
			v.Files = append(v.Files, s)
		} else {
			var o struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(at, &o) == nil && o.ID != "" {
				v.Files = append(v.Files, o.ID)
			}
		}
	}
	for _, l := range f.Link {
		u := firstNonEmpty(l.OriginalURL, l.URL)
		v.Link = append(v.Link, ListLink{URL: u, Text: firstNonEmpty(l.DisplayName, l.Title, u)})
	}
	return v
}

// richBlocks renders rich_text blocks (list text cells, descriptions).
func (a *App) richBlocks(raw json.RawMessage) []richtext.Block {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return richtext.FromMessage(a.S, &slack.Message{Blocks: raw}).Blocks
}

// List loads a list: schema and views (files.info) and every record
// (lists.records.list, paged). Memory only.
func (a *App) List(ctx context.Context, listID string) (List, error) {
	r, err := a.canvasInfo(ctx, listID)
	if err != nil {
		return List{}, err
	}
	l := List{Meta: r.meta(a.S.SelfID()), Title: firstNonEmpty(r.Title, r.Name)}
	l.Meta.IsList = true
	if l.Meta.Deleted {
		return l, ErrCanvasDeleted
	}
	parseListMeta(r, &l, a.richBlocks)
	cursor := ""
	for page := 0; page < 50; page++ {
		p := url.Values{"list_id": {listID}, "include_subtasks": {"true"}, "archived": {"false"},
			"include_suggested": {"false"}, "exclude_deprecated_values": {"true"}}
		if cursor != "" {
			p.Set("cursor", cursor)
		}
		var res struct {
			Records []listRecordRaw `json:"records"`
			Meta    struct {
				Next string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := a.C.Call(ctx, "lists.records.list", p, &res); err != nil {
			return l, err
		}
		for _, rr := range res.Records {
			rec := ListRecord{ID: rr.ID, Created: rr.DateCreated, CreatedBy: rr.CreatedBy, UpdatedBy: rr.UpdatedBy,
				Position: rr.Position, Parent: rr.ParentRecordID, Fields: map[string]ListValue{}}
			rec.Updated, _ = strconv.ParseInt(rr.Updated, 10, 64)
			for _, f := range rr.Fields {
				key := f.Key
				if key == "" {
					key = f.ColumnID
				}
				rec.Fields[key] = a.listValue(f)
			}
			l.Records = append(l.Records, rec)
		}
		cursor = res.Meta.Next
		if cursor == "" {
			break
		}
	}
	sort.SliceStable(l.Records, func(i, j int) bool { return l.Records[i].Position < l.Records[j].Position })
	return l, nil
}

// ListCell is one cell to write: the column and its new value. Set exactly
// the field matching the column's type.
type ListCell struct {
	RowID, ColumnID string
	Value           ListValue
}

// cellJSON is the API shape of one cell value for column type typ.
func cellJSON(typ string, v ListValue) map[string]any {
	m := map[string]any{}
	switch typ {
	case "text":
		m["rich_text"] = textRichText(v.Text)
	case "select", "multi_select":
		m["select"] = nonNil(v.Select)
	case "checkbox", "todo_completed":
		m["checkbox"] = v.Checked()
	case "date", "todo_due_date":
		m["date"] = nonNil(v.Date)
	case "user", "todo_assignee":
		m["user"] = nonNil(v.User)
	case "channel":
		m["channel"] = nonNil(v.Channel)
	case "number":
		m["number"] = v.Number
		if v.Number == nil {
			m["number"] = []float64{}
		}
	case "rating":
		m["rating"] = v.Rating
		if v.Rating == nil {
			m["rating"] = []int{}
		}
	case "email":
		m["email"] = nonNil(v.Email)
	case "phone":
		m["phone"] = nonNil(v.Phone)
	case "link":
		var ls []map[string]string
		for _, l := range v.Link {
			ls = append(ls, map[string]string{"original_url": l.URL, "display_as_url": "false", "display_name": l.Text})
		}
		if ls == nil {
			ls = []map[string]string{}
		}
		m["link"] = ls
	default:
		m["rich_text"] = textRichText(v.Text)
	}
	return m
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// textRichText wraps plain text as one rich_text block.
func textRichText(s string) []map[string]any {
	return []map[string]any{{"type": "rich_text", "elements": []map[string]any{{
		"type": "rich_text_section", "elements": []map[string]any{{"type": "text", "text": s}}}}}}
}

// UpdateListCells writes cells (lists.cells.update).
func (a *App) UpdateListCells(ctx context.Context, l *List, cells []ListCell) error {
	if len(cells) == 0 {
		return nil
	}
	var out []map[string]any
	for _, c := range cells {
		col := l.Column(c.ColumnID)
		if col == nil {
			return errors.New("core: unknown list column")
		}
		m := cellJSON(col.Type, c.Value)
		m["row_id"], m["column_id"] = c.RowID, col.ID
		out = append(out, m)
	}
	b, _ := json.Marshal(out)
	return a.C.Call(ctx, "lists.cells.update", url.Values{"list_id": {l.Meta.ID}, "cells": {string(b)}}, nil)
}

// CreateListRecord adds an item (lists.records.create) with the given
// initial values (column id/key → value) and returns its id.
func (a *App) CreateListRecord(ctx context.Context, l *List, values map[string]ListValue) (string, error) {
	var fields []map[string]any
	for k, v := range values {
		col := l.Column(k)
		if col == nil {
			continue
		}
		m := cellJSON(col.Type, v)
		m["column_id"] = col.ID
		fields = append(fields, m)
	}
	if fields == nil {
		fields = []map[string]any{}
	}
	b, _ := json.Marshal(fields)
	var res struct {
		Record struct {
			ID string `json:"id"`
		} `json:"record"`
		ID string `json:"id"`
	}
	if err := a.C.Call(ctx, "lists.records.create", url.Values{"list_id": {l.Meta.ID}, "fields": {string(b)}}, &res); err != nil {
		return "", err
	}
	return firstNonEmpty(res.Record.ID, res.ID), nil
}

// DeleteListRecord removes an item (lists.records.delete).
func (a *App) DeleteListRecord(ctx context.Context, l *List, id string) error {
	return a.C.Call(ctx, "lists.records.delete", url.Values{"list_id": {l.Meta.ID}, "id": {id}}, nil)
}
