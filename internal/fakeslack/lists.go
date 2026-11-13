package fakeslack

// Slack Lists (docs/LISTS.md): fixture lists served the way Slack serves
// them. files.info describes each list file (list_metadata: schema, views,
// description; list_limits); lists.records.list answers its items;
// lists.open, lists.cells.update, lists.records.create/delete,
// lists.create and lists.templates behave like Slack's, pushing the same
// realtime events (list_cells_updated, list_record_created,
// list_record_removed) over the fake socket. Item comments live in the
// list's companion channel (C + the file id's tail). Every value here is
// made up; nothing comes from a real workspace.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/lists"
	"github.com/some-du6e/clack/internal/slack"
)

// Fixture list ids.
const (
	ListKitchen = "F0LIST00001" // every column type, four views (table, board, filtered table, grouped table)
	ListTodos   = "F0LIST00002" // a to-do list, C0FAKE00001's list tab
	ListTabChan = "C0FAKE00001" // the channel whose tab bar has ListTodos
)

// FakeList is one list the fake holds.
type FakeList struct {
	ID, Title, Channel string
	Creator            string
	Created, Updated   int64
	Meta               lists.Meta
	Records            []lists.Record
	Opened             int // lists.open calls
}

type listStore struct {
	mu       sync.Mutex
	byID     map[string]*FakeList
	order    []string
	next     int
	cells    []url.Values               // lists.cells.update calls
	creates  []url.Values               // lists.records.create calls
	lcreates []url.Values               // lists.create calls
	dls      []string                   // list downloads (files-pri paths and queries)
	comments map[string][]slack.Message // "<channel>/<thread ts>" -> replies, root first
}

var listStores sync.Map // *Server -> *listStore

func (s *Server) listStore() *listStore {
	if v, ok := listStores.Load(s); ok {
		return v.(*listStore)
	}
	ls := &listStore{byID: map[string]*FakeList{}, comments: map[string][]slack.Message{}}
	now := time.Now().Unix()
	if s.Size.NoFixtureDocs {
		v, _ := listStores.LoadOrStore(s, ls)
		return v.(*listStore)
	}
	k := kitchenList(now)
	ls.byID[k.ID] = k
	t := todoList(now)
	ls.byID[t.ID] = t
	ls.order = []string{k.ID, t.ID}
	root := slack.Message{Type: "message", Subtype: "list_record_comment", User: "U0FAKE00001", TS: "1762500000.000300",
		ThreadTS: "1762500000.000300", Text: ""}
	ch := ListCommentsChannelOf(ListKitchen)
	ls.comments[ch+"/"+root.TS] = []slack.Message{root,
		{Type: "message", User: "U0FAKE00002", TS: "1762500100.000100", ThreadTS: root.TS, Text: "Can we get the screenshots in by Friday?"},
		{Type: "message", User: SelfID, TS: "1762500200.000100", ThreadTS: root.TS, Text: "Yes, I'll add them to the canvas :tada:"},
	}
	v, _ := listStores.LoadOrStore(s, ls)
	return v.(*listStore)
}

// ListCommentsChannelOf is a list's comments channel (C + the id's tail).
func ListCommentsChannelOf(id string) string { return "C" + strings.TrimPrefix(id, "F") }

func sp(s string) *string { return &s }
func bp(b bool) *bool     { return &b }
func ip(i int) *int       { return &i }

func richPlain(s string) json.RawMessage {
	b, _ := json.Marshal(lists.TextBlocks(s))
	return b
}

func textField(key, colID, s string) lists.Field {
	return lists.Field{Key: key, ColumnID: colID, Text: sp(s), RichText: richPlain(s)}
}

func kitchenList(now int64) *FakeList {
	col := func(id, key, name, typ string, o lists.Options) lists.Column {
		return lists.Column{ID: id, Key: key, Name: name, Type: typ, Options: o}
	}
	status := []lists.Choice{{Value: "OptTODO0001", Label: "Not started", Color: "gray"},
		{Value: "OptPROG0001", Label: "In progress", Color: "blue"},
		{Value: "OptBLOK0001", Label: "Blocked", Color: "red"},
		{Value: "OptDONE0001", Label: "Done", Color: "green"}}
	tags := []lists.Choice{{Value: "OptTAGDES01", Label: "Design", Color: "purple"},
		{Value: "OptTAGENG01", Label: "Engineering", Color: "indigo"},
		{Value: "OptTAGMKT01", Label: "Marketing", Color: "orange"},
		{Value: "OptTAGDOC01", Label: "Docs", Color: "yellow"}}
	schema := []lists.Column{
		{ID: "Col00000001", Key: "name", Name: "Item", Type: lists.Text, IsPrimary: true, Options: lists.Options{Format: "text"}},
		col("Col0STATUS1", "Col0STATUS1", "Status", lists.Select, lists.Options{Format: "single_select", Choices: status}),
		col("Col0OWNER01", "Col0OWNER01", "Owner", lists.User, lists.Options{Format: "single_entity", ShowMemberName: bp(true)}),
		col("Col0DUE0001", "Col0DUE0001", "Due date", lists.Date, lists.Options{Format: "single", DateFormat: "default"}),
		col("Col0TAGS001", "Col0TAGS001", "Tags", lists.Select, lists.Options{Format: "multi_select", Choices: tags}),
		col("Col0DONE001", "Col0DONE001", "Shipped", lists.Checkbox, lists.Options{}),
		col("Col0EST0001", "Col0EST0001", "Estimate", lists.Number, lists.Options{Format: "standard", Precision: ip(1)}),
		col("Col0PRIO001", "Col0PRIO001", "Priority", lists.Rating, lists.Options{Max: 5, Emoji: ":star:"}),
		col("Col0VOTE001", "Col0VOTE001", "Votes", lists.Vote, lists.Options{Emoji: ":+1:"}),
		col("Col0LINK001", "Col0LINK001", "Link", lists.Link, lists.Options{}),
		col("Col0MAIL001", "Col0MAIL001", "Contact", lists.Email, lists.Options{}),
		col("Col0PHON001", "Col0PHON001", "Phone", lists.Phone, lists.Options{}),
		col("Col0CHAN001", "Col0CHAN001", "Channel", lists.Channel, lists.Options{Format: "single_entity"}),
		col("Col0TEAM001", "Col0TEAM001", "Helpers", lists.User, lists.Options{Format: "multi_entity", ShowMemberName: bp(false)}),
		col("Col0FILE001", "Col0FILE001", "Files", lists.Attachment, lists.Options{}),
		col("Col0CANV001", "Col0CANV001", "Spec", lists.Canvas, lists.Options{Format: "single_entity"}),
		col("Col0MSG0001", "Col0MSG0001", "Thread", lists.Message, lists.Options{Format: "standard"}),
		col("Col0CRBY001", "Col0CRBY001", "Created by", lists.CreatedBy, lists.Options{}),
		col("Col0CRAT001", "Col0CRAT001", "Created", lists.CreatedTime, lists.Options{}),
		col("Col0EDBY001", "Col0EDBY001", "Last edited by", lists.LastEditedBy, lists.Options{}),
		col("Col0EDAT001", "Col0EDAT001", "Last edited", lists.LastEditedTime, lists.Options{}),
	}
	var vcols []lists.ViewColumn
	for i, c := range schema {
		vc := lists.ViewColumn{ID: c.ID, Key: c.Key, Visible: true, Position: fmt.Sprintf("%04d", i)}
		if c.IsPrimary {
			vc.Width = 280
		}
		vcols = append(vcols, vc)
	}
	board := make([]lists.ViewColumn, 0, len(vcols))
	for _, vc := range vcols {
		switch vc.Key {
		case "name", "Col0OWNER01", "Col0DUE0001", "Col0TAGS001", "Col0PRIO001":
		default:
			vc.Visible = false
		}
		board = append(board, vc)
	}
	mine := json.RawMessage(`["viewer"]`)
	notDone := json.RawMessage(`["OptDONE0001"]`)
	views := []lists.View{
		{ID: "View0ALL001", Name: "All items", Type: lists.ViewTable, Position: "0001", Columns: vcols, IsAllItems: true,
			Sorts: []lists.Sort{}, Filters: []lists.Filter{}, Grouping: &lists.Grouping{}},
		{ID: "View0BRD001", Name: "Board", Type: lists.ViewBoard, Position: "0002", Columns: board,
			Sorts: []lists.Sort{}, Filters: []lists.Filter{}, Grouping: &lists.Grouping{GroupBy: "Col0STATUS1", GroupByColumnID: "Col0STATUS1"}},
		{ID: "View0MINE01", Name: "My open items", Type: lists.ViewTable, Position: "0003", Columns: vcols,
			Sorts:   []lists.Sort{{Key: "Col0DUE0001", ColumnID: "Col0DUE0001", Ascending: true}},
			Filters: []lists.Filter{{Key: "Col0OWNER01", ColumnID: "Col0OWNER01", Operator: lists.OpContains, RawValues: mine}, {Key: "Col0STATUS1", ColumnID: "Col0STATUS1", Operator: lists.OpDoesNotContain, RawValues: notDone}}},
		// a table grouped by status (the Lists keyboard tests: group edges)
		{ID: "View0GRP001", Name: "By status", Type: lists.ViewTable, Position: "0004", Columns: vcols,
			Sorts: []lists.Sort{}, Filters: []lists.Filter{}, Grouping: &lists.Grouping{GroupBy: "Col0STATUS1", GroupByColumnID: "Col0STATUS1"}},
		{ID: "View0REC001", Name: "", Type: lists.ViewRecord, Position: "0009", Columns: vcols},
	}
	desc, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "block_id": "desc1", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{
			map[string]any{"type": "text", "text": "Everything we need for the "},
			map[string]any{"type": "text", "text": "launch", "style": map[string]bool{"bold": true}},
			map[string]any{"type": "text", "text": ", in one place."}}}}}})
	l := &FakeList{ID: ListKitchen, Title: "Launch tracker", Creator: "U0FAKE00001", Created: now - 86400*7, Updated: now - 1800,
		Meta: lists.Meta{Schema: schema, Views: views, Icon: ":rocket:", Description: "Everything we need for the launch, in one place.",
			DescriptionBlocks: desc, DefaultView: "View0ALL001"}}
	day := func(d int) string { return time.Unix(now, 0).AddDate(0, 0, d).Format("2006-01-02") }
	ts := func(back int64) float64 { return float64(now - back) }
	rich := json.RawMessage(`[{"type":"rich_text","block_id":"r1","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Polish the "},{"type":"text","text":"onboarding","style":{"bold":true}},{"type":"text","text":" flow with "},{"type":"user","user_id":"U0FAKE00002"}]}]}]`)
	rec := func(id string, pos string, created int64, fs ...lists.Field) lists.Record {
		r := lists.Record{ID: id, ListID: ListKitchen, DateCreated: now - created, CreatedBy: "U0FAKE00001", UpdatedBy: SelfID,
			UpdatedTimestamp: fmt.Sprintf("%d.000100", now-created/2), Position: pos, ViewPositions: map[string]string{}}
		r.Fields = append(r.Fields, fs...)
		r.Fields = append(r.Fields,
			lists.Field{Key: "Col0CRBY001", ColumnID: "Col0CRBY001", User: []string{"U0FAKE00001"}},
			lists.Field{Key: "Col0CRAT001", ColumnID: "Col0CRAT001", Timestamp: []float64{ts(created)}},
			lists.Field{Key: "Col0EDBY001", ColumnID: "Col0EDBY001", User: []string{SelfID}},
			lists.Field{Key: "Col0EDAT001", ColumnID: "Col0EDAT001", Timestamp: []float64{ts(created / 2)}})
		return r
	}
	f := func(key string) lists.Field { return lists.Field{Key: key, ColumnID: key} }
	sel := func(key string, v ...string) lists.Field { x := f(key); x.Select = v; return x }
	usr := func(key string, v ...string) lists.Field { x := f(key); x.User = v; return x }
	date := func(key, d string) lists.Field { x := f(key); x.Date = []string{d}; return x }
	num := func(key string, n float64) lists.Field { x := f(key); x.Number = []float64{n}; return x }
	chk := func(key string, b bool) lists.Field { x := f(key); x.Checkbox = bp(b); return x }
	rate := func(key string, n float64) lists.Field { x := f(key); x.Rating = []float64{n}; return x }
	link := func(key, u, name string) lists.Field {
		x := f(key)
		x.Link = []lists.LinkValue{{OriginalURL: u, DisplayAsURL: name == "", DisplayName: name}}
		return x
	}
	mail := func(key, v string) lists.Field { x := f(key); x.Email = []string{v}; return x }
	phone := func(key, v string) lists.Field { x := f(key); x.Phone = []string{v}; return x }
	chans := func(key string, v ...string) lists.Field { x := f(key); x.Channel = v; return x }
	att := func(key string, v ...string) lists.Field { x := f(key); x.Attachment = v; return x }
	msg := func(key string) lists.Field {
		x := f(key)
		x.Message = []lists.MessageValue{{Value: "https://fake.slack.com/archives/C0FAKE00000/p1762516900000100", ChannelID: "C0FAKE00000", TS: "1762516900.000100"}}
		return x
	}
	first := rec("Rec0ITEM001", "0001", 86400*6,
		textField("name", "Col00000001", "Write the launch announcement"),
		sel("Col0STATUS1", "OptPROG0001"), usr("Col0OWNER01", SelfID), date("Col0DUE0001", day(2)),
		sel("Col0TAGS001", "OptTAGMKT01", "OptTAGDOC01"), chk("Col0DONE001", false), num("Col0EST0001", 3.5),
		rate("Col0PRIO001", 4), usr("Col0VOTE001", "U0FAKE00001", "U0FAKE00002", SelfID),
		link("Col0LINK001", "https://example.com/launch", "Launch brief"), mail("Col0MAIL001", "press@example.com"),
		phone("Col0PHON001", "+1 555 0100"), chans("Col0CHAN001", "C0FAKE00000"), usr("Col0TEAM001", "U0FAKE00002", "U0FAKE00003"),
		att("Col0FILE001", CanvasFile), att("Col0CANV001", CanvasShared), msg("Col0MSG0001"))
	first.ThreadTS = "1762500000.000300"
	richRec := rec("Rec0ITEM002", "0002", 86400*5, lists.Field{Key: "name", ColumnID: "Col00000001", Text: sp("Polish the onboarding flow with @Ada"), RichText: rich},
		sel("Col0STATUS1", "OptBLOK0001"), usr("Col0OWNER01", "U0FAKE00002"), date("Col0DUE0001", day(-1)),
		sel("Col0TAGS001", "OptTAGDES01"), chk("Col0DONE001", false), num("Col0EST0001", 8), rate("Col0PRIO001", 5),
		usr("Col0VOTE001", "U0FAKE00003"), link("Col0LINK001", "https://example.com/onboarding", ""),
		usr("Col0TEAM001", SelfID), att("Col0FILE001", CanvasImage))
	l.Records = []lists.Record{first, richRec,
		rec("Rec0ITEM003", "0003", 86400*4, textField("name", "Col00000001", "Record the demo video"),
			sel("Col0STATUS1", "OptTODO0001"), usr("Col0OWNER01", SelfID), date("Col0DUE0001", day(6)),
			sel("Col0TAGS001", "OptTAGMKT01"), chk("Col0DONE001", false), num("Col0EST0001", 2), rate("Col0PRIO001", 3)),
		rec("Rec0ITEM004", "0004", 86400*3, textField("name", "Col00000001", "Ship the beta"),
			sel("Col0STATUS1", "OptDONE0001"), usr("Col0OWNER01", "U0FAKE00003"), date("Col0DUE0001", day(-4)),
			sel("Col0TAGS001", "OptTAGENG01"), chk("Col0DONE001", true), num("Col0EST0001", 13), rate("Col0PRIO001", 5),
			usr("Col0VOTE001", "U0FAKE00001", "U0FAKE00002"), chans("Col0CHAN001", "C0FAKE00002")),
		rec("Rec0ITEM005", "0005", 86400*2, textField("name", "Col00000001", "Update the docs site"),
			sel("Col0STATUS1", "OptPROG0001"), usr("Col0OWNER01", "U0FAKE00001"),
			sel("Col0TAGS001", "OptTAGDOC01", "OptTAGENG01"), chk("Col0DONE001", false), num("Col0EST0001", 5)),
		rec("Rec0ITEM006", "0006", 86400, textField("name", "Col00000001", "")),
	}
	return l
}

func todoList(now int64) *FakeList {
	schema := []lists.Column{
		{ID: "Col0TDNAME1", Key: "name", Name: "Task", Type: lists.Text, IsPrimary: true, Options: lists.Options{Format: "text"}},
		{ID: "Col0TDDONE1", Key: "todo_completed", Name: "Completed", Type: lists.TodoCompleted},
		{ID: "Col0TDWHO01", Key: "todo_assignee", Name: "Assignee", Type: lists.TodoAssignee, Options: lists.Options{Format: "single_entity", ShowMemberName: bp(true)}},
		{ID: "Col0TDDUE01", Key: "todo_due_date", Name: "Due date", Type: lists.TodoDueDate, Options: lists.Options{Format: "single", DateFormat: "default"}},
	}
	var vcols []lists.ViewColumn
	for i, c := range schema {
		vcols = append(vcols, lists.ViewColumn{ID: c.ID, Key: c.Key, Visible: true, Position: fmt.Sprintf("%04d", i)})
	}
	l := &FakeList{ID: ListTodos, Title: "Team to-dos", Channel: ListTabChan, Creator: SelfID, Created: now - 86400*2, Updated: now - 600,
		Meta: lists.Meta{Schema: schema, TodoMode: true, DefaultView: "View0TDALL1", Icon: ":white_check_mark:",
			Views: []lists.View{{ID: "View0TDALL1", Name: "All items", Type: lists.ViewTable, Position: "0001", Columns: vcols, IsAllItems: true,
				ShowCompleted: bp(true), Sorts: []lists.Sort{}, Filters: []lists.Filter{}}}}}
	day := func(d int) string { return time.Unix(now, 0).AddDate(0, 0, d).Format("2006-01-02") }
	add := func(id, pos, name string, done bool, who string, due string) {
		r := lists.Record{ID: id, ListID: ListTodos, DateCreated: now - 3600, CreatedBy: SelfID, Position: pos, ViewPositions: map[string]string{}}
		r.Fields = []lists.Field{textField("name", "Col0TDNAME1", name),
			{Key: "todo_completed", ColumnID: "Col0TDDONE1", Checkbox: bp(done)},
			{Key: "todo_assignee", ColumnID: "Col0TDWHO01", User: []string{who}}}
		if due != "" {
			r.Fields = append(r.Fields, lists.Field{Key: "todo_due_date", ColumnID: "Col0TDDUE01", Date: []string{due}})
		}
		l.Records = append(l.Records, r)
	}
	add("Rec0TODO001", "0001", "Book the venue", true, "U0FAKE00001", day(-2))
	add("Rec0TODO002", "0002", "Order stickers", false, SelfID, day(1))
	add("Rec0TODO003", "0003", "Send the invites", false, "U0FAKE00002", day(3))
	return l
}

func (s *Server) listFile(l *FakeList) map[string]any {
	meta, _ := json.Marshal(l.Meta)
	f := map[string]any{"id": l.ID, "name": l.Title, "title": l.Title, "mimetype": "application/vnd.slack-list",
		"filetype": "list", "pretty_type": "List", "mode": "list", "user": l.Creator, "created": l.Created, "timestamp": l.Created,
		"updated": l.Updated, "edit_timestamp": l.Updated, "editable": true, "access": "owner",
		"editors": []string{l.Creator, SelfID}, "last_editor": SelfID,
		"permalink": s.permalinkHost() + "/lists/" + TeamID + "/" + l.ID,
		"is_public": true, "comments_count": 0, "size": 0,
		"list_metadata":         json.RawMessage(meta),
		"url_private_download":  s.URL + "files-pri/" + TeamID + "-" + l.ID + "/download/" + listSlug(l.Title),
		"list_csv_download_url": s.URL + "files-pri/" + TeamID + "-" + l.ID + "/csv/" + listSlug(l.Title) + ".csv",
		"list_limits": map[string]any{"row_count": len(l.Records), "row_count_limit": 10000, "column_count": len(l.Meta.Schema),
			"column_count_limit": 100, "view_count": len(l.Meta.Views), "view_count_limit": 25, "archived_row_count": 0,
			"max_attachments_per_cell": 10, "over_row_maximum": false, "over_column_maximum": false, "over_view_maximum": false},
		"channels": []string{}, "groups": []string{}, "ims": []string{}}
	if l.Channel != "" {
		f["channels"] = []string{l.Channel}
	}
	return f
}

// listSlug is a list's file name in its download URLs.
func listSlug(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "_"):
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

// serveListDownload answers a list's CSV or JSON download
// (url_private_download with csv / json for download) with a made-up
// export of its items; false when id isn't a list.
func (s *Server) serveListDownload(w http.ResponseWriter, r *http.Request, id, kind string) bool {
	ls := s.listStore()
	ls.mu.Lock()
	l, ok := ls.byID[id]
	if ok {
		ls.dls = append(ls.dls, r.URL.Path+"?"+r.URL.RawQuery)
	}
	var names []string
	if ok {
		for _, rec := range l.Records {
			for _, f := range rec.Fields {
				if f.Key == "name" && f.Text != nil {
					names = append(names, *f.Text)
				}
			}
		}
	}
	ls.mu.Unlock()
	if !ok {
		return false
	}
	if kind == "json" {
		w.Header().Set("Content-Type", "application/json")
		b, _ := json.Marshal(map[string]any{"name": l.Title, "items": names})
		w.Write(b)
		return true
	}
	w.Header().Set("Content-Type", "text/csv")
	var b strings.Builder
	b.WriteString("Name\n")
	for _, n := range names {
		b.WriteString(strings.ReplaceAll(n, ",", " ") + "\n")
	}
	w.Write([]byte(b.String()))
	return true
}

// ListDownloads returns the list downloads received (tests).
func (s *Server) ListDownloads() []string {
	ls := s.listStore()
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return append([]string(nil), ls.dls...)
}

// ListCreates returns the lists.create calls received (tests).
func (s *Server) ListCreates() []url.Values {
	ls := s.listStore()
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return append([]url.Values(nil), ls.lcreates...)
}

// List returns a copy of a fixture list's state (tests).
func (s *Server) List(id string) (FakeList, bool) {
	ls := s.listStore()
	ls.mu.Lock()
	defer ls.mu.Unlock()
	l, ok := ls.byID[id]
	if !ok {
		return FakeList{}, false
	}
	c := *l
	c.Records = append([]lists.Record(nil), l.Records...)
	for i := range c.Records { // SetField writes a record's cells in place
		c.Records[i].Fields = append([]lists.Field(nil), c.Records[i].Fields...)
	}
	return c, true
}

// ListCellUpdates returns the lists.cells.update calls received (tests).
func (s *Server) ListCellUpdates() []url.Values {
	ls := s.listStore()
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return append([]url.Values(nil), ls.cells...)
}

// ListRecordCreates returns the lists.records.create calls received (tests).
func (s *Server) ListRecordCreates() []url.Values {
	ls := s.listStore()
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return append([]url.Values(nil), ls.creates...)
}

// ListComments returns an item thread's messages, root first (tests).
func (s *Server) ListComments(listID, threadTS string) []slack.Message {
	ls := s.listStore()
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return append([]slack.Message(nil), ls.comments[ListCommentsChannelOf(listID)+"/"+threadTS]...)
}

// EditListAsOther changes a cell as another user would in Slack and pushes
// list_cells_updated to every socket (live-update tests, CI shots).
func (s *Server) EditListAsOther(listID, recID string, f lists.Field) {
	ls := s.listStore()
	ls.mu.Lock()
	l := ls.byID[listID]
	if l == nil {
		ls.mu.Unlock()
		return
	}
	for i := range l.Records {
		if l.Records[i].ID == recID {
			l.Records[i].SetField(f)
			l.Records[i].UpdatedBy = "U0FAKE00002"
		}
	}
	ls.mu.Unlock()
	cell := fieldJSON(f)
	cell["row_id"] = recID
	s.Push(map[string]any{"type": "list_cells_updated", "list_id": listID, "cells": []any{cell},
		"event_ts": fmt.Sprintf("%d.000300", time.Now().Unix())})
}

// AddListRecordAsOther adds an item as another user would and pushes
// list_record_created.
func (s *Server) AddListRecordAsOther(listID, name string) string {
	ls := s.listStore()
	ls.mu.Lock()
	l := ls.byID[listID]
	if l == nil {
		ls.mu.Unlock()
		return ""
	}
	ls.next++
	r := lists.Record{ID: fmt.Sprintf("Rec0OTHER%02d", ls.next), ListID: listID, DateCreated: time.Now().Unix(),
		CreatedBy: "U0FAKE00002", Position: fmt.Sprintf("%04d", 9000+ls.next), ViewPositions: map[string]string{}}
	if p := l.Meta.Primary(); p != nil {
		r.Fields = []lists.Field{textField(p.Key, p.ID, name)}
	}
	l.Records = append(l.Records, r)
	ls.mu.Unlock()
	s.Push(map[string]any{"type": "list_record_created", "list_id": listID, "record": r,
		"event_ts": fmt.Sprintf("%d.000400", time.Now().Unix())})
	return r.ID
}

func fieldJSON(f lists.Field) map[string]any {
	b, _ := json.Marshal(f)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

// cellField reads one lists.cells.update / initial_fields entry into a
// field (the inverse of lists.Edit.Typed).
func cellField(col *lists.Column, raw map[string]json.RawMessage) (lists.Field, string) {
	f := lists.Field{Key: col.Key, ColumnID: col.ID}
	strs := func(k string) []string {
		var v []string
		_ = json.Unmarshal(raw[k], &v)
		return v
	}
	switch {
	case raw["rich_text"] != nil:
		f.RichText = raw["rich_text"]
		t, _ := lists.PlainRichText(f.RichText)
		f.Text = &t
	case raw["number"] != nil:
		_ = json.Unmarshal(raw["number"], &f.Number)
	case raw["select"] != nil:
		f.Select = strs("select")
		for _, v := range f.Select {
			if _, ok := col.Choice(v); !ok {
				return f, "invalid_select_option"
			}
		}
	case raw["date"] != nil:
		f.Date = strs("date")
	case raw["user"] != nil:
		f.User = strs("user")
	case raw["channel"] != nil:
		f.Channel = strs("channel")
	case raw["checkbox"] != nil:
		var b bool
		_ = json.Unmarshal(raw["checkbox"], &b)
		f.Checkbox = &b
	case raw["email"] != nil:
		f.Email = strs("email")
	case raw["phone"] != nil:
		f.Phone = strs("phone")
	case raw["rating"] != nil:
		_ = json.Unmarshal(raw["rating"], &f.Rating)
	case raw["link"] != nil:
		var ls []struct {
			OriginalURL  string `json:"original_url"`
			DisplayAsURL bool   `json:"display_as_url"`
			DisplayName  string `json:"display_name"`
		}
		if json.Unmarshal(raw["link"], &ls) != nil {
			return f, "invalid_arguments"
		}
		for _, l := range ls {
			if l.OriginalURL == "" {
				return f, "invalid_arguments" // a camelCase key would land here
			}
			f.Link = append(f.Link, lists.LinkValue{OriginalURL: l.OriginalURL, DisplayAsURL: l.DisplayAsURL, DisplayName: l.DisplayName})
		}
	default:
		return f, "invalid_arguments"
	}
	return f, ""
}

// handleLists answers the list methods; handled=false passes on.
func (s *Server) handleLists(method string, f url.Values) (any, bool) {
	if out, handled := s.handleListsAdd(method, f); handled { // lists_add.go
		return out, true
	}
	ls := s.listStore()
	switch method {
	case "files.info":
		ls.mu.Lock()
		defer ls.mu.Unlock()
		if l, okl := ls.byID[f.Get("file")]; okl {
			return ok("file", s.listFile(l), "comments", []any{}), true
		}
		if strings.HasPrefix(f.Get("file"), "F0LIST") { // a list id the fake never had: deleted
			return map[string]any{"ok": false, "error": "file_not_found"}, true
		}
		return nil, false
	case "conversations.info":
		if f.Get("channel") != ListTabChan {
			return nil, false
		}
		for _, c := range s.convs {
			if c.ID == ListTabChan {
				b, _ := json.Marshal(c)
				var m map[string]any
				_ = json.Unmarshal(b, &m)
				var tabs []any
				props := map[string]any{}
				cs := s.canvasStore() // the channel's canvas tabs come first, as canvas.go lists them
				cs.mu.Lock()
				for _, id := range cs.order {
					if cv := cs.byID[id]; cv.Channel == ListTabChan {
						tabs = append(tabs, map[string]any{"id": "Ct" + cv.ID, "type": "canvas", "label": "",
							"data": map[string]any{"file_id": cv.ID, "shared_ts": "1762500000.000100"}})
						if props["canvas"] == nil {
							props["canvas"] = map[string]any{"file_id": cv.ID, "is_empty": false}
						}
					}
				}
				cs.mu.Unlock()
				tabs = append(tabs,
					map[string]any{"id": "Ct" + ListTodos, "type": "list", "label": "", "data": map[string]any{"file_id": ListTodos}},
					map[string]any{"id": "Ctfiles", "type": "files", "label": ""})
				tabs = append(tabs, s.addedTabs(ListTabChan)...) // conversations.addTab (lists_add.go)
				props["tabs"] = tabs
				m["properties"] = props
				addCounts(m, c, f)
				return ok("channel", m), true
			}
		}
		return nil, false
	case "search.modules.files":
		q := f.Get("query")
		if !strings.Contains(q, "type:lists") {
			return nil, false
		}
		ls.mu.Lock()
		var items []map[string]any
		for _, id := range ls.order {
			items = append(items, s.listFile(ls.byID[id]))
		}
		ls.mu.Unlock()
		if strings.Contains(q, "type:quip") {
			cs := s.canvasStore()
			cs.mu.Lock()
			for _, id := range cs.order {
				items = append(items, s.canvasFile(cs.byID[id]))
			}
			cs.mu.Unlock()
		}
		return ok("items", items, "pagination", map[string]any{"total_count": len(items), "page": 1, "page_count": 1}), true
	case "lists.open":
		ls.mu.Lock()
		defer ls.mu.Unlock()
		l, okl := ls.byID[f.Get("list_id")]
		if !okl {
			return map[string]any{"ok": false, "error": "list_not_found"}, true
		}
		l.Opened++
		return ok("pinned_view_ids", []string{}), true
	case "lists.records.list":
		ls.mu.Lock()
		defer ls.mu.Unlock()
		l, okl := ls.byID[f.Get("list_id")]
		if !okl {
			return map[string]any{"ok": false, "error": "list_not_found"}, true
		}
		archived := f.Get("archived") == "true"
		var recs []lists.Record
		for _, r := range l.Records {
			if r.Archived == archived {
				recs = append(recs, r)
			}
		}
		return ok("records", recs, "response_metadata", map[string]any{"next_cursor": ""}), true
	case "lists.cells.update":
		var cells []map[string]json.RawMessage
		if err := json.Unmarshal([]byte(f.Get("cells")), &cells); err != nil || len(cells) == 0 {
			return map[string]any{"ok": false, "error": "invalid_arguments"}, true
		}
		ls.mu.Lock()
		ls.cells = append(ls.cells, f)
		l, okl := ls.byID[f.Get("list_id")]
		if !okl {
			ls.mu.Unlock()
			return map[string]any{"ok": false, "error": "list_not_found"}, true
		}
		var out []any
		for _, c := range cells {
			var row, colID string
			_ = json.Unmarshal(c["row_id"], &row)
			_ = json.Unmarshal(c["column_id"], &colID)
			col := l.Meta.Column(colID)
			if col == nil || row == "" {
				ls.mu.Unlock()
				return map[string]any{"ok": false, "error": "invalid_arguments"}, true
			}
			fld, errc := cellField(col, c)
			if errc != "" {
				ls.mu.Unlock()
				return map[string]any{"ok": false, "error": errc}, true
			}
			found := false
			for i := range l.Records {
				if l.Records[i].ID == row {
					l.Records[i].SetField(fld)
					l.Records[i].UpdatedBy = SelfID
					found = true
				}
			}
			if !found {
				ls.mu.Unlock()
				return map[string]any{"ok": false, "error": "record_not_found"}, true
			}
			m := fieldJSON(fld)
			m["row_id"] = row
			out = append(out, m)
		}
		l.Updated = time.Now().Unix()
		id := l.ID
		ls.mu.Unlock()
		go s.Push(map[string]any{"type": "list_cells_updated", "list_id": id, "cells": out,
			"event_ts": fmt.Sprintf("%d.000500", time.Now().Unix())})
		return ok("cells", out), true
	case "lists.records.create":
		ls.mu.Lock()
		ls.creates = append(ls.creates, f)
		l, okl := ls.byID[f.Get("list_id")]
		if !okl {
			ls.mu.Unlock()
			return map[string]any{"ok": false, "error": "list_not_found"}, true
		}
		var init []map[string]json.RawMessage
		if raw := f.Get("initial_fields"); raw != "" {
			if json.Unmarshal([]byte(raw), &init) != nil {
				ls.mu.Unlock()
				return map[string]any{"ok": false, "error": "invalid_arguments"}, true
			}
		}
		ls.next++
		now := time.Now().Unix()
		pos := f.Get("position")
		if pos == "" {
			pos = fmt.Sprintf("%04d", 5000+ls.next)
		}
		r := lists.Record{ID: fmt.Sprintf("Rec0NEW%04d", ls.next), ListID: l.ID, DateCreated: now, CreatedBy: SelfID,
			UpdatedBy: SelfID, Position: pos, ParentRecordID: f.Get("parent_record_id"), ViewPositions: map[string]string{},
			UpdatedTimestamp: fmt.Sprintf("%d.000100", now)}
		for _, c := range init {
			var colID string
			_ = json.Unmarshal(c["column_id"], &colID)
			col := l.Meta.Column(colID)
			if col == nil {
				ls.mu.Unlock()
				return map[string]any{"ok": false, "error": "invalid_arguments"}, true
			}
			fld, errc := cellField(col, c)
			if errc != "" {
				ls.mu.Unlock()
				return map[string]any{"ok": false, "error": errc}, true
			}
			r.SetField(fld)
		}
		l.Records = append(l.Records, r)
		ls.mu.Unlock()
		go s.Push(map[string]any{"type": "list_record_created", "list_id": r.ListID, "record": r,
			"event_ts": fmt.Sprintf("%d.000600", now)})
		return ok("record", r), true
	case "lists.records.delete":
		ls.mu.Lock()
		l, okl := ls.byID[f.Get("list_id")]
		if !okl {
			ls.mu.Unlock()
			return map[string]any{"ok": false, "error": "list_not_found"}, true
		}
		id := f.Get("id")
		found := false
		for i := range l.Records {
			if l.Records[i].ID == id {
				l.Records = append(l.Records[:i], l.Records[i+1:]...)
				found = true
				break
			}
		}
		ls.mu.Unlock()
		if !found {
			return map[string]any{"ok": false, "error": "record_not_found"}, true
		}
		go s.Push(map[string]any{"type": "list_record_removed", "list_id": l.ID, "id": id,
			"event_ts": fmt.Sprintf("%d.000700", time.Now().Unix())})
		return ok(), true
	case "lists.templates":
		// Slack's production template ids (PTs7.op), which the Files
		// view's Templates strip picks by
		tpls := []any{
			map[string]any{"id": "F07QP7D3TC5", "name": "Project tracker", "description": "Plan and track a project", "icon": ":rocket:"},
			map[string]any{"id": "F07QS06GTNX", "name": "Feedback tracker", "description": "Collect and triage feedback", "icon": ":speech_balloon:"},
			map[string]any{"id": "F07QRV73FNF", "name": "Quarterly plan", "description": "Plan the quarter's projects", "icon": ":calendar:"}}
		// the file-backed set (lists_file_backed_templates) also has the
		// contact tracker, which the sales tracker of the plain set replaces
		files := append(append([]any(nil), tpls...),
			map[string]any{"id": "F07QKDVNEK1", "name": "Contact tracker", "description": "Keep track of contacts", "icon": ":busts_in_silhouette:"})
		plain := append(append([]any(nil), tpls...),
			map[string]any{"id": "sales_tracker", "name": "Sales tracker", "description": "Track deals through the pipeline", "icon": ":chart_with_upwards_trend:"})
		starter := []any{map[string]any{"id": "Ttodo", "name": "To-do list", "description": "Keep track of what needs doing", "icon": ":white_check_mark:"}}
		starterFiles := []any{map[string]any{"id": "F08C8VB1DN3", "name": "To-do list", "description": "Keep track of what needs doing", "icon": ":white_check_mark:"}}
		return ok("starter_templates", starter, "templates", plain, "template_files", files, "starter_template_files", starterFiles), true
	case "lists.create":
		name := f.Get("name")
		if name == "" {
			return map[string]any{"ok": false, "error": "invalid_args"}, true
		}
		ls.mu.Lock()
		ls.next++
		now := time.Now().Unix()
		id := fmt.Sprintf("F0LISTNEW%02d", ls.next)
		l := &FakeList{ID: id, Title: name, Creator: SelfID, Created: now, Updated: now}
		ls.lcreates = append(ls.lcreates, f)
		if src, found := ls.byID[f.Get("copy_from_list_id")]; found { // More actions > Make a copy
			l.Meta = src.Meta
			l.Meta.Icon = f.Get("icon")
			for _, r := range src.Records {
				r.ID, r.ListID = r.ID+"C"+id[len(id)-2:], id
				l.Records = append(l.Records, r)
			}
			ls.byID[id] = l
			ls.order = append(ls.order, id)
			file := s.listFile(l)
			ls.mu.Unlock()
			return ok("list", file), true
		}
		switch tpl := f.Get("template_id"); {
		case tpl == "blank" && f.Get("todo_mode") == "true": // the channel tab bar's new list (lists_add.go)
			t := todoList(now)
			l.Meta.Schema, l.Meta.TodoMode = t.Meta.Schema, true
		case tpl == "", tpl == "blank":
			l.Meta.Schema = []lists.Column{{ID: "Col0NEWNAME", Key: "name", Name: "Name", Type: lists.Text, IsPrimary: true, Options: lists.Options{Format: "text"}}}
		case tpl == "add_to_new_list": // "Add to list…" > Create New List
			l.Meta.Schema = []lists.Column{{ID: "Col0NEWNAME", Key: "name", Name: "Name", Type: lists.Text, IsPrimary: true, Options: lists.Options{Format: "text"}},
				{ID: "Col0NEWMSG", Key: "Col0NEWMSG", Name: "Message", Type: lists.Message}}
		case tpl == "Ttodo", tpl == "F08C8VB1DN3": // the to-do starter, plain and file-backed
			t := todoList(now)
			l.Meta.Schema, l.Meta.TodoMode = t.Meta.Schema, true
		case tpl == "F07QP7D3TC5", tpl == "F07QS06GTNX", tpl == "F07QRV73FNF", tpl == "F07QKDVNEK1", tpl == "sales_tracker":
			k := kitchenList(now)
			l.Meta.Schema = k.Meta.Schema[:5]
		default:
			ls.mu.Unlock()
			return map[string]any{"ok": false, "error": "invalid_template"}, true
		}
		var vcols []lists.ViewColumn
		for i, c := range l.Meta.Schema {
			vcols = append(vcols, lists.ViewColumn{ID: c.ID, Key: c.Key, Visible: true, Position: fmt.Sprintf("%04d", i)})
		}
		l.Meta.Views = []lists.View{{ID: "View0NEW" + id[len(id)-2:], Name: "All items", Type: lists.ViewTable, Position: "0001",
			Columns: vcols, IsAllItems: true, Sorts: []lists.Sort{}, Filters: []lists.Filter{}}}
		ls.byID[id] = l
		ls.order = append(ls.order, id)
		file := s.listFile(l)
		ls.mu.Unlock()
		return ok("list", file), true
	case "conversations.replies":
		ch := f.Get("channel")
		if !strings.HasPrefix(ch, "C0LIST") {
			return nil, false
		}
		ts := f.Get("ts")
		ls.mu.Lock()
		defer ls.mu.Unlock()
		if lid, rid, okt := strings.Cut(ts, "-"); okt { // Slack's "<list>-<record>" lookup: make the root
			l := ls.byID[lid]
			if l == nil {
				return map[string]any{"ok": false, "error": "thread_not_found"}, true
			}
			for i := range l.Records {
				if l.Records[i].ID != rid {
					continue
				}
				if l.Records[i].ThreadTS == "" {
					ls.next++
					root := fmt.Sprintf("%d.%06d", time.Now().Unix(), 900+ls.next)
					l.Records[i].ThreadTS = root
					ls.comments[ch+"/"+root] = []slack.Message{{Type: "message", Subtype: "list_record_comment", User: SelfID, TS: root, ThreadTS: root}}
				}
				return ok("messages", ls.comments[ch+"/"+l.Records[i].ThreadTS][:1], "has_more", false), true
			}
			return map[string]any{"ok": false, "error": "thread_not_found"}, true
		}
		msgs, okc := ls.comments[ch+"/"+ts]
		if !okc {
			return map[string]any{"ok": false, "error": "thread_not_found"}, true
		}
		return ok("messages", msgs, "has_more", false), true
	case "chat.postMessage":
		ch := f.Get("channel")
		if !strings.HasPrefix(ch, "C0LIST") || f.Get("thread_ts") == "" {
			return nil, false
		}
		ls.mu.Lock()
		key := ch + "/" + f.Get("thread_ts")
		if _, okc := ls.comments[key]; !okc {
			ls.mu.Unlock()
			return map[string]any{"ok": false, "error": "thread_not_found"}, true
		}
		ts := fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
		m := slack.Message{Type: "message", User: SelfID, TS: ts, ThreadTS: f.Get("thread_ts"), Text: f.Get("text"), ClientMsgID: f.Get("client_msg_id")}
		ls.comments[key] = append(ls.comments[key], m)
		ls.mu.Unlock()
		return ok("ts", ts, "channel", ch, "message", m), true
	}
	return nil, false
}
