package core

// List records outside the list (branch lists-records, CDN build
// fa938a086569, 2026-11-13; docs/EXPERIMENTS.md "lists-records"):
//
//   - Saved list records: Later's ListRecord rows (boot-render nckZ) and the
//     record menus' RemindMeAboutThisMenuItem (boot-shared Jf4A, behind
//     lists_in_later: client-boot Hm0j v). Its thunk addReminderToRecord
//     picks one of three saved-api calls (mheF): saveListRecord (saved.add
//     {item_type: list_record, item_id: the list, ts: the record,
//     date_due, todo_state}), updateSavedListRecord (saved.update {…,
//     date_due, mark}) and removeSavedListRecord (saved.delete {…}).
//     Core keeps a saved record's metadata next to the messages' (saved
//     set key listRecordSavedKey(list), record id).
//   - The record menus' Archive item (tAP0: archiveRecords, lists.records
//     .archive {list_id, ids}) and Duplicate item (Yja+ duplicateRecord:
//     lists.records.create {…, duplicated_record_id}).
//   - search_file_channel_messages (boot-shared lH2I mapStateToProps): a
//     file channel the client knows the file of reads as the file
//     (renderFileChannelSlug: a canvas or list_record element with the
//     file id).

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/lists"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/store"
)

// The experiments read here (TestExperimentReadsListed).
const (
	ExpListsInLater              = "lists_in_later"
	ExpSearchFileChannelMessages = "search_file_channel_messages"
)

func init() {
	store.FileChannel = func(s *store.Store, ch string) (richtext.FileChannel, bool) {
		if v, ok := appsByStore.Load(s); ok {
			return v.(*App).fileChannelSlug(ch)
		}
		return richtext.FileChannel{}, false
	}
	experimentReads[ExpListsInLater] = on
	experimentReads[ExpSearchFileChannelMessages] = on
	defaultExperimentGroups[ExpSearchFileChannelMessages] = "on" // the account's group (experiments.getByUser, 2026-11-13)
}

// ListsInLater is Hm0j v isListsInLaterExperimentEnabled: "on" ===
// getGroupForUser("lists_in_later").
func (a *App) ListsInLater() bool { return a.experiment(ExpListsInLater) == "on" }

// SearchFileChannelMessages is lH2I's I: "on" ===
// getGroupForUser("search_file_channel_messages").
func (a *App) SearchFileChannelMessages() bool {
	return a.experiment(ExpSearchFileChannelMessages) == "on"
}

// listRecordSavedKey is the saved set's "channel" for list listID's
// records (the ts is the record id).
func listRecordSavedKey(listID string) string { return "list_record:" + listID }

// ListRecordSaved is YeRq mc getSavedItemByRecordId for a record of
// listID: its saved metadata, ok false when it isn't in Later.
func (a *App) ListRecordSaved(listID, recID string) (SavedMeta, bool) {
	return a.SavedMeta(listRecordSavedKey(listID), recID)
}

func listRecordSavedParams(listID, recID string) url.Values {
	return url.Values{"item_type": {"list_record"}, "item_id": {listID}, "ts": {recID}}
}

// emitListRecordSaved tells views a record's Later state changed (the
// record menus' description, Later's rows).
func (a *App) emitListRecordSaved(listID, recID, name string) {
	a.emit(Event{Type: "saved", Name: name, ID: listID, TS: recID})
}

// SaveListRecord is addSavedListRecord: saved.add {item_type:
// list_record, item_id, ts, date_due, todo_state}. todo_state is Qq
// ({isLaterTodo, hasReminder}): to_do for a later_todos viewer adding
// without a reminder, else not sent; date_due only with a reminder.
func (a *App) SaveListRecord(ctx context.Context, listID, recID string, due int64) error {
	p := listRecordSavedParams(listID, recID)
	todo := a.experiment("later_todos") == "on" // 6owI tj
	if due > 0 {
		p.Set("date_due", strconv.FormatInt(due, 10))
	} else if todo {
		p.Set("todo_state", "to_do")
	}
	if err := a.C.Call(ctx, "saved.add", p, nil); err != nil && !isCode(err, "already_saved") {
		return err
	}
	now := time.Now().Unix()
	m := SavedMeta{TodoState: "saved", DateCreated: now, DateDue: due}
	if due > 0 || todo {
		m.TodoState = "to_do"
		if due == 0 {
			m.DateDue = now // bs: a to-do without a reminder is due when added
		}
	}
	a.setSavedMeta(listRecordSavedKey(listID), recID, m)
	a.emitListRecordSaved(listID, recID, "add")
	return nil
}

// UpdateSavedListRecord is updateSavedListRecord: saved.update
// {item_type: list_record, item_id, ts, date_due, mark}; mark is
// "unarchived" or "uncompleted" (addReminderToRecord's, for an archived
// or completed item) or "".
func (a *App) UpdateSavedListRecord(ctx context.Context, listID, recID string, due int64, mark string) error {
	p := listRecordSavedParams(listID, recID)
	p.Set("date_due", strconv.FormatInt(due, 10))
	if mark != "" {
		p.Set("mark", mark)
	}
	if err := a.C.Call(ctx, "saved.update", p, nil); err != nil {
		return err
	}
	a.patchSavedMeta(listRecordSavedKey(listID), recID, func(m *SavedMeta) {
		m.DateDue = due
		switch mark {
		case "unarchived":
			m.Archived = false
		case "uncompleted":
			m.DateCompleted = 0
			if m.TodoState == "completed" {
				m.TodoState = "to_do"
			}
		}
	})
	a.emitListRecordSaved(listID, recID, "update")
	return nil
}

// UnsaveListRecord is removeSavedListRecord: saved.delete {item_type:
// list_record, item_id, ts}.
func (a *App) UnsaveListRecord(ctx context.Context, listID, recID string) error {
	if err := a.C.Call(ctx, "saved.delete", listRecordSavedParams(listID, recID), nil); err != nil {
		return err
	}
	a.markSaved(listRecordSavedKey(listID), recID, false)
	a.emitListRecordSaved(listID, recID, "delete")
	return nil
}

// RemindListRecord is addReminderToRecord (Jf4A A) for a picked time
// (due unix seconds; "Custom…" opens the reminder modal instead):
//
//   - a saved record and a time: updateSavedListRecord with mark
//     "unarchived" for an archived one, "uncompleted" for a completed
//     one;
//   - a saved record and no time: removeSavedListRecord;
//   - else addSavedListRecord.
func (a *App) RemindListRecord(ctx context.Context, listID, recID string, due int64) error {
	m, saved := a.ListRecordSaved(listID, recID)
	switch {
	case saved && due > 0:
		mark := ""
		if m.Archived {
			mark = "unarchived"
		}
		if m.DateCompleted > 0 {
			mark = "uncompleted"
		}
		return a.UpdateSavedListRecord(ctx, listID, recID, due, mark)
	case saved:
		return a.UnsaveListRecord(ctx, listID, recID)
	}
	return a.SaveListRecord(ctx, listID, recID, due)
}

// ClearListRecordReminder is ListRecordRemindMenu's onClear: a to-do
// keeps its place with its creation time as the due date
// (updateSavedListRecord {dateDue: dateCreated}); anything else leaves
// Later (removeSavedListRecord's fetcher, saved.delete).
func (a *App) ClearListRecordReminder(ctx context.Context, listID, recID string) error {
	m, saved := a.ListRecordSaved(listID, recID)
	if saved && m.TodoState == "to_do" {
		return a.UpdateSavedListRecord(ctx, listID, recID, m.DateCreated, "")
	}
	return a.UnsaveListRecord(ctx, listID, recID)
}

// ArchiveListRecords is archiveRecords (tAP0's p.p): lists.records.archive
// {list_id, ids}. The list_records_archived event takes them out of the
// list.
func (a *App) ArchiveListRecords(ctx context.Context, listID string, ids ...string) error {
	b, _ := json.Marshal(ids)
	if err := a.C.Call(ctx, "lists.records.archive", url.Values{"list_id": {listID}, "ids": {string(b)}}, nil); err != nil {
		return err
	}
	st := a.lists()
	st.mu.Lock()
	if d := st.docs[listID]; d != nil {
		gone := map[string]bool{}
		for _, id := range ids {
			gone[id] = true
		}
		kept := d.Records[:0]
		for _, r := range d.Records {
			if !gone[r.ID] {
				kept = append(kept, r)
			}
		}
		d.Records = kept
	}
	st.mu.Unlock()
	a.emit(Event{Type: "list", ID: listID, Name: ListChanged})
	return nil
}

// DuplicateListRecord is Duplicate item (Yja+ eR → createRecord
// {duplicatedRecordId, position}): lists.records.create {list_id,
// duplicated_record_id, position, updated_timestamp}; the copy sits
// after the record (position "" leaves it to the server).
func (a *App) DuplicateListRecord(ctx context.Context, listID, recID, position string) (lists.Record, error) {
	v := url.Values{"list_id": {listID}, "position": {position}, "updated_timestamp": {a.hlc()}, "duplicated_record_id": {recID}}
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
	if d := st.docs[listID]; d != nil && d.Record(rec.ID) == nil {
		d.Records = append(d.Records, rec)
	}
	st.mu.Unlock()
	a.emit(Event{Type: "list", ID: listID, Name: ListChanged})
	return rec, nil
}

// ListRecordLink is useGetPermalinkForListRecord (x+OS R4): the list's
// permalink with ?record_id=.
func ListRecordLink(permalink, recID string) string {
	if permalink == "" || recID == "" {
		return ""
	}
	return permalink + "?record_id=" + recID
}

// ---- Later's ListRecord rows ----

// SavedListRecord is what Later's ListRecord row (nckZ) draws: the list
// file (j: the row draws nothing until it is loaded), its title, and the
// record's name (fields.fallback.name: the primary field's text;
// "Untitled item" when empty). Deleted is a tombstoned list or record (P:
// isTombstoned || isDeleted: a click opens the remove modal).
type SavedListRecord struct {
	ListTitle string
	Name      string
	Deleted   bool
}

type savedRecordState struct {
	mu   sync.Mutex
	got  map[string]SavedListRecord // list/record
	busy map[string]bool
}

var savedRecordStates sync.Map // *App -> *savedRecordState

func (a *App) savedRecords() *savedRecordState {
	if v, ok := savedRecordStates.Load(a); ok {
		return v.(*savedRecordState)
	}
	v, _ := savedRecordStates.LoadOrStore(a, &savedRecordState{got: map[string]SavedListRecord{}, busy: map[string]bool{}})
	return v.(*savedRecordState)
}

// SavedListRecordInfo is a saved record's row data; ok false while the
// list loads (the call starts the load, and a "list_record_info" event
// with the list and record ids follows).
func (a *App) SavedListRecordInfo(listID, recID string) (SavedListRecord, bool) {
	k := listID + "/" + recID
	if d := a.CachedList(listID); d != nil {
		return savedListRecordOf(d, recID), true
	}
	st := a.savedRecords()
	st.mu.Lock()
	r, ok := st.got[k]
	if ok || st.busy[listID] || a.C == nil {
		st.mu.Unlock()
		return r, ok
	}
	st.busy[listID] = true
	st.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(a.bg, 45*time.Second)
		defer cancel()
		d, err := a.List(ctx, listID)
		st.mu.Lock()
		delete(st.busy, listID)
		switch {
		case err == nil && d != nil:
			st.got[k] = savedListRecordOf(d, recID)
		case IsFileGone(err):
			st.got[k] = SavedListRecord{Deleted: true}
		}
		st.mu.Unlock()
		a.emit(Event{Type: "list_record_info", ID: listID, TS: recID})
	}()
	return SavedListRecord{}, false
}

func savedListRecordOf(d *ListDoc, recID string) SavedListRecord {
	out := SavedListRecord{ListTitle: d.Info.Title}
	rec := d.Record(recID)
	if rec == nil {
		out.Deleted = true
		return out
	}
	if p := d.Meta.Primary(); p != nil {
		out.Name = strings.TrimSpace(rec.Field(p.Key).PlainText())
	}
	return out
}

// ---- file channels as their files (search_file_channel_messages) ----

// FileChannelFile is lH2I's fileIdForChannel and fileChannelType for
// conversation ch: under search_file_channel_messages, a file channel
// (is_file) whose file the client already holds (tgNF Lp: no fetch; for
// Clack the sidebar's file store) reads as "canvas" or "list" with the
// file id (I3+E l: C → F).
func (a *App) FileChannelFile(ch string) (kind, fileID string, ok bool) {
	if !a.SearchFileChannelMessages() {
		return "", "", false
	}
	c, found := a.S.Conv(ch)
	if !found || !IsFileChannel(c) {
		return "", "", false
	}
	fileID = FileOfChannel(ch)
	switch f := a.sidebarFileOf(fileID); f.typ {
	case "canvas", "list":
		return f.typ, fileID, true
	}
	if a.CachedList(fileID) != nil { // a list the client has open
		return "list", fileID, true
	}
	if r, known := a.S.KnownCanvasRef(fileID); known && r.Status == "normal" && (r.Kind == "canvas" || r.Kind == "list") {
		return r.Kind, fileID, true // files.info answered for it (a slug, a tab)
	}
	return "", "", false
}

// FileChannelTitle is the file's title as the client knows it (its
// files.info), "" when it doesn't.
func (a *App) FileChannelTitle(fileID string) string {
	if t := a.sidebarFileOf(fileID).title; t != "" {
		return t
	}
	if d := a.CachedList(fileID); d != nil {
		return d.Info.Title
	}
	if r, ok := a.S.KnownCanvasRef(fileID); ok {
		return r.Title
	}
	return ""
}

// fileChannelSlug is the store's hook for richtext (richtext
// FileChannelResolver).
func (a *App) fileChannelSlug(ch string) (richtext.FileChannel, bool) {
	kind, id, ok := a.FileChannelFile(ch)
	if !ok {
		return richtext.FileChannel{}, false
	}
	return richtext.FileChannel{Kind: kind, FileID: id}, true
}

// resolveListRecords fills list slugs' record names (richtext
// ListRecordResolver) from each list (the cached copy, else lists.open
// and lists.records.list), and returns the list ids it filled. A record
// the list doesn't have is "deleted" (getRecordVariant: !record).
func (a *App) resolveListRecords(ctx context.Context, recs [][2]string) []string {
	byList := map[string][]string{}
	for _, r := range recs {
		byList[r[0]] = append(byList[r[0]], r[1])
	}
	var done []string
	for id, ids := range byList {
		d := a.CachedList(id)
		var err error
		if d == nil {
			d, err = a.List(ctx, id)
		}
		if err != nil || d == nil {
			if IsFileGone(err) {
				for _, r := range ids {
					a.S.PutListRecordName(id, r, nil, "deleted")
				}
				done = append(done, id)
				continue
			}
			for _, r := range ids {
				a.S.ForgetListRecord(id, r)
			}
			continue
		}
		p := d.Meta.Primary()
		for _, r := range ids {
			rec := d.Record(r)
			if rec == nil {
				a.S.PutListRecordName(id, r, nil, "deleted")
				continue
			}
			var raw json.RawMessage
			if p != nil {
				if f := rec.Field(p.Key); f != nil {
					raw = f.RichText
					if len(raw) == 0 && f.Text != nil {
						raw, _ = json.Marshal(lists.TextBlocks(*f.Text))
					}
				}
			}
			a.S.PutListRecordName(id, r, raw, "base")
		}
		done = append(done, id)
	}
	return done
}
