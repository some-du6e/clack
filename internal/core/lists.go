package core

// Slack Lists (docs/LISTS.md, internal/lists). Reading: files.info for the
// list file (title, list_metadata: schema, views, description; list_limits)
// and lists.records.list for the items, as the web client's fetchRecords
// thunk does. Live: lists.open when a view appears, then the gateway's
// list_record_created / _updated / _removed, list_records_archived,
// list_cells_updated and list_contents_merged events, applied to the cached
// list. Editing: lists.cells.update, lists.records.create/delete,
// lists.create; comments are a thread in the list's companion channel.
// Kept in its own file; realtime events arrive through SubscribeRaw, so
// events.go is untouched.
//
// Scraping policy: list contents live only in memory.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/lists"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// ListInfo is what files.info says about a list file.
type ListInfo struct {
	ID, Title   string
	Creator     string // user id
	Created     int64
	Updated     int64 // edit_timestamp, else updated
	Editable    bool
	Access      string // read | write | owner
	Editors     []string
	Permalink   string
	Channels    []string
	IsPublic    bool
	LastEditor  string
	Description []richtext.Block // description_blocks
}

// ListDoc is one loaded list: its file, schema, views and items.
type ListDoc struct {
	Info        ListInfo
	Meta        lists.Meta
	Limits      lists.Limits
	Records     []lists.Record // not archived, top-level and subtasks
	PinnedViews []string       // lists.open's pinned_view_ids
	At          time.Time
}

// Clone copies the doc deeply enough that the UI can keep it while core
// applies live events to its own copy.
func (d *ListDoc) Clone() *ListDoc {
	if d == nil {
		return nil
	}
	c := *d
	c.Records = make([]lists.Record, len(d.Records))
	for i, r := range d.Records {
		r.Fields = append([]lists.Field(nil), r.Fields...)
		c.Records[i] = r
	}
	c.Meta.Schema = append([]lists.Column(nil), d.Meta.Schema...)
	c.Meta.Views = append([]lists.View(nil), d.Meta.Views...)
	return &c
}

// Record finds an item by id.
func (d *ListDoc) Record(id string) *lists.Record {
	for i := range d.Records {
		if d.Records[i].ID == id {
			return &d.Records[i]
		}
	}
	return nil
}

// IsListFile reports whether a file is a Slack list.
func IsListFile(filetype, mode, prettyType string) bool {
	return filetype == "list" || mode == "list" || strings.EqualFold(prettyType, "list")
}

// ListCommentsChannel is the companion channel that holds a list's item
// comment threads: the list's file id with C for F (Slack's 7/Dp).
func ListCommentsChannel(listID string) string {
	if strings.HasPrefix(listID, "F") {
		return "C" + listID[1:]
	}
	return listID
}

type listFileJSON struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Title         string          `json:"title"`
	Filetype      string          `json:"filetype"`
	Mode          string          `json:"mode"`
	PrettyType    string          `json:"pretty_type"`
	User          string          `json:"user"`
	Created       int64           `json:"created"`
	Updated       int64           `json:"updated"`
	EditTimestamp int64           `json:"edit_timestamp"`
	Editable      bool            `json:"editable"`
	Access        string          `json:"access"`
	Editors       []string        `json:"editors"`
	LastEditor    string          `json:"last_editor"`
	Permalink     string          `json:"permalink"`
	Channels      []string        `json:"channels"`
	Groups        []string        `json:"groups"`
	IMs           []string        `json:"ims"`
	IsPublic      bool            `json:"is_public"`
	ListMetadata  json.RawMessage `json:"list_metadata"`
	ListLimits    lists.Limits    `json:"list_limits"`
}

var errNotList = errors.New("core: not a list")

func (a *App) listInfo(f *listFileJSON) (ListInfo, lists.Meta, error) {
	meta, err := lists.ParseMeta(f.ListMetadata)
	if err != nil {
		return ListInfo{}, meta, err
	}
	title := f.Title
	if title == "" {
		title = f.Name
	}
	in := ListInfo{ID: f.ID, Title: title, Creator: f.User, Created: f.Created, Updated: f.EditTimestamp,
		Editable: f.Editable, Access: f.Access, Editors: f.Editors, Permalink: f.Permalink, IsPublic: f.IsPublic,
		LastEditor: f.LastEditor}
	if in.Updated == 0 {
		in.Updated = f.Updated
	}
	in.Channels = append(append(append(in.Channels, f.Channels...), f.Groups...), f.IMs...)
	if len(meta.DescriptionBlocks) > 0 && string(meta.DescriptionBlocks) != "null" {
		in.Description = richtext.FromMessage(a.S, &slack.Message{Blocks: meta.DescriptionBlocks}).Blocks
	} else if meta.Description != "" {
		in.Description = richtext.FromMrkdwn(a.S, meta.Description)
	}
	return in, meta, nil
}

func (a *App) listFile(ctx context.Context, id string) (*listFileJSON, error) {
	var r struct {
		File listFileJSON `json:"file"`
	}
	if err := a.C.Call(ctx, "files.info", url.Values{"file": {id}, "count": {"0"}}, &r); err != nil {
		return nil, err
	}
	if !IsListFile(r.File.Filetype, r.File.Mode, r.File.PrettyType) {
		return nil, errNotList
	}
	return &r.File, nil
}

// listRecords reads every item (lists.records.list, following the cursor).
func (a *App) listRecords(ctx context.Context, id string, archived bool) ([]lists.Record, error) {
	var out []lists.Record
	cursor := ""
	for page := 0; page < 50; page++ {
		v := url.Values{"list_id": {id}, "include_subtasks": {"true"}, "archived": {fmt.Sprint(archived)},
			"include_suggested": {"false"}, "exclude_deprecated_values": {"true"}, "limit": {"1000"}}
		if cursor != "" {
			v.Set("cursor", cursor)
		}
		var r struct {
			Records []json.RawMessage `json:"records"`
			Meta    struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := a.C.Call(ctx, "lists.records.list", v, &r); err != nil {
			return nil, err
		}
		for _, raw := range r.Records {
			if rec, err := lists.ParseRecord(raw); err == nil {
				out = append(out, rec)
			}
		}
		if cursor = r.Meta.NextCursor; cursor == "" {
			break
		}
	}
	return out, nil
}

// listState is per-App list bookkeeping (kept off App's struct).
type listState struct {
	mu       sync.Mutex
	docs     map[string]*ListDoc // last loaded (and live-updated) lists
	open     map[string]int      // views showing each list
	pending  map[string]bool     // a refetch is scheduled
	watching bool
	node     string // this client's HLC node (lists.records.create's updated_timestamp)
	hlcMS    int64
	hlcCount int
}

var listStates sync.Map // *App -> *listState

func (a *App) lists() *listState {
	if v, ok := listStates.Load(a); ok {
		return v.(*listState)
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	v, _ := listStates.LoadOrStore(a, &listState{docs: map[string]*ListDoc{}, open: map[string]int{}, pending: map[string]bool{},
		node: hex.EncodeToString(b)})
	return v.(*listState)
}

// List loads a list: files.info and lists.records.list together. The
// result is cached, and kept current by realtime events while the list is
// open (OpenList).
func (a *App) List(ctx context.Context, id string) (*ListDoc, error) {
	var (
		f    *listFileJSON
		recs []lists.Record
		ferr error
		rerr error
		wg   sync.WaitGroup
	)
	wg.Add(2)
	go func() { defer wg.Done(); f, ferr = a.listFile(ctx, id) }()
	go func() { defer wg.Done(); recs, rerr = a.listRecords(ctx, id, false) }()
	wg.Wait()
	if ferr != nil {
		return nil, ferr
	}
	if rerr != nil {
		return nil, rerr
	}
	info, meta, err := a.listInfo(f)
	if err != nil {
		return nil, err
	}
	d := &ListDoc{Info: info, Meta: meta, Limits: f.ListLimits, At: time.Now()}
	for _, r := range recs {
		if !r.Archived {
			d.Records = append(d.Records, r)
		}
	}
	users := []string{info.Creator}
	users = append(users, info.Editors...)
	for _, r := range d.Records {
		users = append(users, r.CreatedBy, r.UpdatedBy)
		for _, fl := range r.Fields {
			users = append(users, fl.User...)
		}
	}
	a.EnsureUsers(ctx, users)
	st := a.lists()
	st.mu.Lock()
	if old := st.docs[id]; old != nil {
		d.PinnedViews = old.PinnedViews
	}
	st.docs[id] = d
	st.mu.Unlock()
	return d.Clone(), nil
}

// CachedList is the last loaded state of a list (nil if never loaded).
func (a *App) CachedList(id string) *ListDoc {
	st := a.lists()
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.docs[id].Clone()
}

// OpenList tells Slack the viewer has the list open (lists.open, which
// also returns the pinned views) and starts applying the list's realtime
// events. Call CloseList when the view goes away.
func (a *App) OpenList(ctx context.Context, id string) error {
	st := a.lists()
	st.mu.Lock()
	st.open[id]++
	first := st.open[id] == 1
	watch := !st.watching
	st.watching = true
	st.mu.Unlock()
	if watch {
		go a.watchLists()
	}
	if !first {
		return nil
	}
	var r struct {
		PinnedViewIDs []string `json:"pinned_view_ids"`
	}
	if err := a.C.Call(ctx, "lists.open", url.Values{"list_id": {id}}, &r); err != nil {
		return err
	}
	st.mu.Lock()
	if d := st.docs[id]; d != nil {
		d.PinnedViews = r.PinnedViewIDs
	}
	st.mu.Unlock()
	return nil
}

// CloseList undoes OpenList.
func (a *App) CloseList(id string) {
	st := a.lists()
	st.mu.Lock()
	if st.open[id] > 0 {
		st.open[id]--
	}
	if st.open[id] == 0 {
		delete(st.open, id)
	}
	st.mu.Unlock()
}

// ListOpen reports whether a view has the list open.
func (a *App) ListOpen(id string) bool {
	st := a.lists()
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.open[id] > 0
}

// ListEvent kinds (Event.Name of Type "list").
const (
	ListChanged  = "changed"  // records changed in place (the cached doc is current)
	ListReloaded = "reloaded" // refetched (schema or many records changed)
)

// listEventID is the list a realtime event is about ("" if none).
func listEventID(ev slack.Event) (string, bool) {
	switch ev.Type {
	case "list_record_created", "list_record_updated", "list_record_removed", "list_records_archived",
		"list_cells_updated", "list_contents_merged":
		var e struct {
			ListID string `json:"list_id"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		return e.ListID, true
	case "file_change", "file_shared", "file_deleted":
		var e struct {
			FileID string `json:"file_id"`
			File   struct {
				ID string `json:"id"`
			} `json:"file"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		if e.FileID != "" {
			return e.FileID, false
		}
		return e.File.ID, false
	}
	return "", false
}

func (a *App) watchLists() {
	evs, cancel := a.SubscribeRaw()
	defer cancel()
	for ev := range evs {
		id, listEv := listEventID(ev)
		if id == "" || !a.ListOpen(id) {
			continue
		}
		if !listEv { // file_change: the title, schema or views moved
			a.listRefetch(id)
			continue
		}
		if a.applyListEvent(id, ev) {
			a.emit(Event{Type: "list", ID: id, Name: ListChanged})
		} else {
			a.listRefetch(id)
		}
	}
}

// applyListEvent applies one list_* event to the cached list; false asks
// for a refetch (Slack's list_contents_merged does fetchRecords too).
func (a *App) applyListEvent(id string, ev slack.Event) bool {
	var e struct {
		ID      string            `json:"id"`
		IDs     []string          `json:"ids"`
		Restore bool              `json:"restore"`
		Deleted bool              `json:"deleted"`
		Record  json.RawMessage   `json:"record"`
		Cells   []json.RawMessage `json:"cells"`
		Column  json.RawMessage   `json:"column"`
	}
	if json.Unmarshal(ev.Raw, &e) != nil {
		return false
	}
	st := a.lists()
	st.mu.Lock()
	defer st.mu.Unlock()
	d := st.docs[id]
	if d == nil {
		return false
	}
	remove := func(rid string) {
		for i := range d.Records {
			if d.Records[i].ID == rid {
				d.Records = append(d.Records[:i], d.Records[i+1:]...)
				return
			}
		}
	}
	switch ev.Type {
	case "list_record_removed":
		remove(e.ID)
	case "list_records_archived":
		if e.Restore {
			return false // the restored items' contents aren't in the event
		}
		for _, rid := range e.IDs {
			remove(rid)
		}
	case "list_record_updated", "list_record_created":
		if e.Deleted {
			remove(e.ID)
			break
		}
		if len(e.Record) == 0 {
			return true
		}
		rec, err := lists.ParseRecord(e.Record)
		if err != nil || rec.ID == "" {
			return false
		}
		if rec.Archived {
			remove(rec.ID)
			break
		}
		if r := d.Record(rec.ID); r != nil {
			*r = rec
		} else {
			d.Records = append(d.Records, rec)
		}
	case "list_cells_updated":
		if len(e.Column) > 0 && string(e.Column) != "null" {
			var col lists.Column
			if json.Unmarshal(e.Column, &col) == nil {
				col.Raw = e.Column
				for i := range d.Meta.Schema {
					if d.Meta.Schema[i].Key == col.Key {
						d.Meta.Schema[i] = col
					}
				}
			}
		}
		for _, raw := range e.Cells {
			var c struct {
				RowID string `json:"row_id"`
			}
			var f lists.Field
			if json.Unmarshal(raw, &c) != nil || json.Unmarshal(raw, &f) != nil {
				continue
			}
			f.Raw = raw
			if f.Key == "" {
				if col := d.Meta.Column(f.ColumnID); col != nil {
					f.Key = col.Key
				}
			}
			if r := d.Record(c.RowID); r != nil && f.Key != "" {
				r.SetField(f)
			}
		}
	case "list_contents_merged":
		return false
	}
	return true
}

// listRefetch reloads an open list, coalescing bursts into one fetch per
// 400 ms, then tells the views.
func (a *App) listRefetch(id string) {
	st := a.lists()
	st.mu.Lock()
	if st.pending[id] {
		st.mu.Unlock()
		return
	}
	st.pending[id] = true
	st.mu.Unlock()
	time.AfterFunc(400*time.Millisecond, func() {
		st.mu.Lock()
		delete(st.pending, id)
		st.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if _, err := a.List(ctx, id); err == nil {
			a.emit(Event{Type: "list", ID: id, Name: ListReloaded})
		}
	})
}

// ListInfo reads just the list's header (files.info), for polling when
// realtime is off.
func (a *App) ListInfo(ctx context.Context, id string) (ListInfo, error) {
	f, err := a.listFile(ctx, id)
	if err != nil {
		return ListInfo{}, err
	}
	in, _, err := a.listInfo(f)
	return in, err
}

// ---- editing ----

// guard refuses an edit the fidelity guard rejects for the cached cell.
func (a *App) guardListEdit(id string, e lists.Edit) error {
	if e.Column == nil {
		return errors.New("core: list edit without a column")
	}
	var f *lists.Field
	st := a.lists()
	st.mu.Lock()
	if d := st.docs[id]; d != nil && e.RowID != "" {
		if r := d.Record(e.RowID); r != nil {
			if ff := r.Field(e.Column.Key); ff != nil {
				cp := *ff
				f = &cp
			}
		}
	}
	st.mu.Unlock()
	if err := lists.Editable(e.Column, f); err != nil {
		return fmt.Errorf("this field can't be edited in Clack yet (%w)", err)
	}
	return nil
}

// UpdateListCells writes cells (lists.cells.update, Slack's bulk update
// path: {list_id, cells: [{row_id, column_id, <typed value>}]}). Every
// edit passes the fidelity guard first; the cached list shows the new
// values at once and the server's echo confirms them.
func (a *App) UpdateListCells(ctx context.Context, id string, edits ...lists.Edit) error {
	if len(edits) == 0 {
		return nil
	}
	var cells []map[string]any
	for _, e := range edits {
		if err := a.guardListEdit(id, e); err != nil {
			return err
		}
		c, err := e.Cell()
		if err != nil {
			return err
		}
		cells = append(cells, c)
	}
	b, _ := json.Marshal(cells)
	if err := a.C.Call(ctx, "lists.cells.update", url.Values{"list_id": {id}, "cells": {string(b)}}, nil); err != nil {
		return err
	}
	st := a.lists()
	st.mu.Lock()
	if d := st.docs[id]; d != nil {
		for _, e := range edits {
			if r := d.Record(e.RowID); r != nil {
				var old lists.Field
				if f := r.Field(e.Column.Key); f != nil {
					old = *f
				}
				r.SetField(e.Apply(old))
			}
		}
	}
	st.mu.Unlock()
	a.emit(Event{Type: "list", ID: id, Name: ListChanged})
	return nil
}

// hlc is Slack's hybrid logical clock stamp for lists.records.create
// (HGm0: "<ms>-<counter, 4 hex>-<node, 16>").
func (a *App) hlc() string {
	st := a.lists()
	st.mu.Lock()
	defer st.mu.Unlock()
	ms := time.Now().UnixMilli()
	if ms <= st.hlcMS {
		st.hlcCount++
		ms = st.hlcMS
	} else {
		st.hlcMS, st.hlcCount = ms, 0
	}
	node := ("0000000000000000" + st.node)
	return fmt.Sprintf("%d-%04X-%s", ms, st.hlcCount, node[len(node)-16:])
}

// CreateListRecord adds an item (lists.records.create {list_id,
// initial_fields, position, parent_record_id, updated_timestamp}).
func (a *App) CreateListRecord(ctx context.Context, id string, position, parent string, fields ...lists.Edit) (lists.Record, error) {
	var init []map[string]any
	for _, e := range fields {
		e.RowID = ""
		if err := lists.Editable(e.Column, nil); err != nil {
			return lists.Record{}, fmt.Errorf("this field can't be edited in Clack yet (%w)", err)
		}
		m, err := e.InitialField()
		if err != nil {
			return lists.Record{}, err
		}
		init = append(init, m)
	}
	v := url.Values{"list_id": {id}, "position": {position}, "updated_timestamp": {a.hlc()}}
	if len(init) > 0 {
		b, _ := json.Marshal(init)
		v.Set("initial_fields", string(b))
	}
	if parent != "" {
		v.Set("parent_record_id", parent)
	}
	var r struct {
		Record json.RawMessage `json:"record"`
	}
	if err := a.C.Call(ctx, "lists.records.create", v, &r); err != nil {
		return lists.Record{}, err
	}
	rec, err := lists.ParseRecord(r.Record)
	if err != nil {
		return rec, err
	}
	st := a.lists()
	st.mu.Lock()
	if d := st.docs[id]; d != nil && d.Record(rec.ID) == nil {
		d.Records = append(d.Records, rec)
	}
	st.mu.Unlock()
	a.emit(Event{Type: "list", ID: id, Name: ListChanged})
	return rec, nil
}

// DeleteListRecord deletes an item (lists.records.delete {list_id, id}).
func (a *App) DeleteListRecord(ctx context.Context, id, recID string) error {
	if err := a.C.Call(ctx, "lists.records.delete", url.Values{"list_id": {id}, "id": {recID}}, nil); err != nil {
		return err
	}
	st := a.lists()
	st.mu.Lock()
	if d := st.docs[id]; d != nil {
		for i := range d.Records {
			if d.Records[i].ID == recID {
				d.Records = append(d.Records[:i], d.Records[i+1:]...)
				break
			}
		}
	}
	st.mu.Unlock()
	a.emit(Event{Type: "list", ID: id, Name: ListChanged})
	return nil
}

// ListRecordThread is an item's comments thread: the channel and root ts.
// Items that never had a comment have no thread_ts yet; Slack's web client
// then reads conversations.replies with ts "<list id>-<record id>", which
// answers with the item's root message (subtype list_record_comment).
func (a *App) ListRecordThread(ctx context.Context, listID, recID string) (channel, ts string, err error) {
	channel = ListCommentsChannel(listID)
	if d := a.CachedList(listID); d != nil {
		if r := d.Record(recID); r != nil && r.ThreadTS != "" {
			return channel, r.ThreadTS, nil
		}
	}
	var r struct {
		Messages []struct {
			TS       string `json:"ts"`
			ThreadTS string `json:"thread_ts"`
			Subtype  string `json:"subtype"`
		} `json:"messages"`
	}
	if err := a.C.Call(ctx, "conversations.replies", url.Values{"channel": {channel}, "ts": {listID + "-" + recID}, "limit": {"1"}}, &r); err != nil {
		return "", "", err
	}
	for _, m := range r.Messages {
		if m.ThreadTS == "" || m.ThreadTS == m.TS {
			st := a.lists()
			st.mu.Lock()
			if d := st.docs[listID]; d != nil {
				if rec := d.Record(recID); rec != nil {
					rec.ThreadTS = m.TS
				}
			}
			st.mu.Unlock()
			return channel, m.TS, nil
		}
	}
	return "", "", errors.New("core: the item has no comment thread")
}

// CommentOnListRecord posts a comment in an item's thread.
func (a *App) CommentOnListRecord(ctx context.Context, listID, recID, text string) (Message, error) {
	ch, ts, err := a.ListRecordThread(ctx, listID, recID)
	if err != nil {
		return Message{}, err
	}
	return a.Send(ctx, SendOpts{Channel: ch, ThreadTS: ts, Text: text})
}

// ListTemplate is one of lists.templates' templates.
type ListTemplate struct {
	ID, Name, Description, Icon string
	Starter                     bool
}

// ListTemplates reads Slack's list templates (lists.templates).
func (a *App) ListTemplates(ctx context.Context) ([]ListTemplate, error) {
	type tpl struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Icon        string `json:"icon"`
	}
	var r struct {
		Templates        []tpl `json:"templates"`
		StarterTemplates []tpl `json:"starter_templates"`
	}
	if err := a.C.Call(ctx, "lists.templates", url.Values{}, &r); err != nil {
		return nil, err
	}
	var out []ListTemplate
	for _, t := range r.StarterTemplates {
		out = append(out, ListTemplate{ID: t.ID, Name: t.Name, Description: t.Description, Icon: t.Icon, Starter: true})
	}
	for _, t := range r.Templates {
		out = append(out, ListTemplate{ID: t.ID, Name: t.Name, Description: t.Description, Icon: t.Icon})
	}
	return out, nil
}

// CreateList makes a new list (lists.create {name, template_id?,
// include_copied_list_records}) and returns its file id. An empty name is
// Slack's "Untitled list".
func (a *App) CreateList(ctx context.Context, name, templateID string) (string, error) {
	if strings.TrimSpace(name) == "" {
		name = "Untitled list"
	}
	v := url.Values{"name": {name}, "include_copied_list_records": {"true"}, "include_canvas_inline_view": {"false"}}
	if templateID != "" {
		v.Set("template_id", templateID)
	}
	var r struct {
		List struct {
			ID string `json:"id"`
		} `json:"list"`
	}
	if err := a.C.Call(ctx, "lists.create", v, &r); err != nil {
		return "", err
	}
	if r.List.ID == "" {
		return "", errors.New("core: lists.create returned no list")
	}
	return r.List.ID, nil
}
