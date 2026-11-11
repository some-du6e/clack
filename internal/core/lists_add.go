package core

// Adding to lists, as Slack's web client does it (docs/LISTS.md):
//
//   - A message's "Add to list…" (modern.vendor VzJM addToList):
//     lists.addToList {list_id, channel_id, message_ts, reason:
//     "add-to-list"} → {record, list}; then the pref
//     lists_add_to_list_default_list remembers the list and the new item
//     opens (RoRD openRecord: lists.records.open {list_id, id, reason:
//     "open-record"}).
//   - "Create New List" there (addToNewList): lists.create from the
//     add_to_new_list template, then lists.addToList into it.
//   - A channel's "+" > List (client-boot-shared /wV5 useCreateListTab,
//     kXBb maybeOpenShareFileAsTabConfirmationModal):
//     conversations.addTab {channel_id, type: "list", data: {file_id,
//     access_level}, label, reason: "add-channel-tab"}; a new list is
//     lists.create {template_id: "blank", todo_mode, context_channel_id,
//     reason: "create-list-from-channel-tabs"} first.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/some-du6e/clack/internal/lists"
)

// PrefAddToListDefault is the list "Add to list…" preselects
// (getDefaultListForAddToList).
const PrefAddToListDefault = "lists_add_to_list_default_list"

// AddToList adds message ts in channel to a list (lists.addToList) and
// returns the new item. The cached list, if any, gets the item at once.
func (a *App) AddToList(ctx context.Context, listID, channel, ts string) (lists.Record, error) {
	v := url.Values{"list_id": {listID}, "channel_id": {channel}, "message_ts": {ts}, "reason": {"add-to-list"}}
	var r struct {
		Record json.RawMessage `json:"record"`
	}
	if err := a.C.Call(ctx, "lists.addToList", v, &r); err != nil {
		return lists.Record{}, err
	}
	rec, err := lists.ParseRecord(r.Record)
	if err != nil {
		return rec, err
	}
	st := a.lists()
	st.mu.Lock()
	if d := st.docs[listID]; d != nil && d.Record(rec.ID) == nil {
		d.Records = append(d.Records, rec)
	}
	st.mu.Unlock()
	a.emit(Event{Type: "list", ID: listID, Name: ListChanged})
	return rec, nil
}

// OpenListRecord tells Slack an item was opened (lists.records.open,
// openRecord's first call).
func (a *App) OpenListRecord(ctx context.Context, listID, recID string) error {
	return a.C.Call(ctx, "lists.records.open", url.Values{"list_id": {listID}, "id": {recID}, "reason": {"open-record"}}, nil)
}

// CreateListOpts are lists.create's arguments beyond the name.
type CreateListOpts struct {
	Name       string // "" is Slack's "Untitled list"
	TemplateID string // lists.templates' ids, or Slack's built-in ones: "blank", "add_to_new_list"
	Reason     string
	TodoMode   bool
	// ContextChannelID is the channel a list made from its tab bar
	// belongs to.
	ContextChannelID string
}

// CreateListWith makes a list with Slack's full lists.create arguments
// and returns its file id.
func (a *App) CreateListWith(ctx context.Context, o CreateListOpts) (string, error) {
	name := o.Name
	if strings.TrimSpace(name) == "" {
		name = "Untitled list"
	}
	v := url.Values{"name": {name}, "include_copied_list_records": {"true"}}
	if o.TemplateID != "" {
		v.Set("template_id", o.TemplateID)
	}
	if o.Reason != "" {
		v.Set("reason", o.Reason)
	}
	if o.TodoMode {
		v.Set("todo_mode", "true")
	}
	if o.ContextChannelID != "" {
		v.Set("context_channel_id", o.ContextChannelID)
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

// ListHeader reads a list's file and schema (files.info only): the add
// dialogs check its access and whether it has a message column.
func (a *App) ListHeader(ctx context.Context, id string) (ListInfo, lists.Meta, error) {
	f, err := a.listFile(ctx, id)
	if err != nil {
		return ListInfo{}, lists.Meta{}, err
	}
	return a.listInfo(f)
}

// IsFileGone reports whether err says the file was deleted or isn't there
// (or isn't a list).
func IsFileGone(err error) bool {
	return errors.Is(err, errNotList) || isCode(err, "file_not_found", "file_deleted")
}

// ErrTooManyTabs is conversations.addTab's too_many_tabs.
var ErrTooManyTabs = errors.New("too_many_tabs")

// AddListTab adds list id to channel's tab bar (conversations.addTab).
// viewID is the tab's default view (lists_channel_tab_default_refine_view_id;
// "" for none); access is the share level the channel gets ("read" or
// "write"; "" when the list is already shared there or was made for the
// tab); label is the tab's name ("" for the list's own title).
func (a *App) AddListTab(ctx context.Context, channel, id, viewID, access, label string) error {
	data := map[string]string{"file_id": id}
	if viewID != "" {
		data["lists_channel_tab_default_refine_view_id"] = viewID
	}
	if access != "" {
		data["access_level"] = access
	}
	b, _ := json.Marshal(data)
	v := url.Values{"channel_id": {channel}, "type": {"list"}, "data": {string(b)}, "reason": {"add-channel-tab"}}
	if label != "" {
		v.Set("label", label)
	}
	err := a.C.Call(ctx, "conversations.addTab", v, nil)
	if isCode(err, "too_many_tabs") {
		return ErrTooManyTabs
	}
	return err
}

// ListSharedWith reports whether the list is already shared to channel,
// so its tab is added without Slack's "Share this list?" step (kXBb asks
// AEKY for the channel's existing share and skips the modal when there
// is one). info is ListHeader's.
func ListSharedWith(info ListInfo, channel string) bool {
	for _, c := range info.Channels {
		if c == channel {
			return true
		}
	}
	return false
}

// ListResult is one row of the add dialogs' list explorer.
type ListResult struct {
	File
	LastRead int64 // unix seconds the viewer last opened it; 0: never
}

// SearchLists is the explorer of Slack's list dialogs (i/CI
// useListExplorer: the unified files browser on lists), one page of
// search.modules.files with type:lists, most recently viewed first, or
// by relevance for a query.
func (a *App) SearchLists(ctx context.Context, query string, page int) ([]ListResult, int, error) {
	p := filesParams(FilesOpts{Kind: "lists", Query: query, Page: page}, a.S.SelfID())
	var res struct {
		Pagination searchPagination `json:"pagination"`
		Items      []browseFileRaw  `json:"items"`
	}
	if err := a.C.Call(ctx, "search.modules.files", p, &res); err != nil {
		return nil, 0, err
	}
	files := a.filePage(ctx, res.Items)
	out := make([]ListResult, 0, len(files))
	for i, f := range files {
		if !f.IsList || res.Items[i].IsDeleted || f.Mode == "tombstone" {
			continue
		}
		r := ListResult{File: f}
		var ms float64
		if json.Unmarshal(res.Items[i].LastRead, &ms) == nil && ms > 0 {
			r.LastRead = int64(ms / 1000)
		}
		out = append(out, r)
	}
	return out, max(1, res.Pagination.PageCount), nil
}
