package fakeslack

// List records outside the list (core lists_records.go): saved list
// records (saved.add / saved.update / saved.delete with item_type
// list_record: item_id the list, ts the record), which saved.list lists
// with Size.ListRecordsInLater, and lists.records.archive {list_id, ids}
// (list_records_archived). The saved items have the shape saved.list
// gives a real list record (2026-11-13, keys only): item_id, item_type,
// ts, date_created, date_due, date_completed, date_updated, is_archived,
// state, todo_state.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"
)

// The fixture's saved list records (Size.ListRecordsInLater).
const (
	SavedRecordTodo  = "Rec0ITEM001" // a to-do with a reminder tomorrow ("Write the launch announcement")
	SavedRecordSaved = "Rec0ITEM003" // saved, no reminder ("Record the demo video")
)

type recordSaves struct {
	mu     sync.Mutex
	items  map[string]map[string]any // list/record
	writes []url.Values
	seeded bool
}

var recordSavesOf sync.Map // *Server -> *recordSaves

func (s *Server) recordSaves() *recordSaves {
	v, _ := recordSavesOf.LoadOrStore(s, &recordSaves{items: map[string]map[string]any{}})
	rs := v.(*recordSaves)
	rs.mu.Lock()
	if !rs.seeded {
		rs.seeded = true
		if s.Size.ListRecordsInLater && !s.Size.NoFixtureDocs {
			now := time.Now()
			y, m, d := now.AddDate(0, 0, 1).Date()
			due := time.Date(y, m, d, 9, 0, 0, 0, now.Location()).Unix()
			rs.items[ListKitchen+"/"+SavedRecordTodo] = savedRecord(ListKitchen, SavedRecordTodo, "to_do", now.Add(-2*time.Hour).Unix(), due)
			rs.items[ListKitchen+"/"+SavedRecordSaved] = savedRecord(ListKitchen, SavedRecordSaved, "saved", now.Add(-3*time.Hour).Unix(), 0)
		}
	}
	rs.mu.Unlock()
	return rs
}

func savedRecord(list, rec, todo string, created, due int64) map[string]any {
	return map[string]any{"item_id": list, "item_type": "list_record", "ts": rec, "date_created": created,
		"date_due": due, "date_completed": int64(0), "date_updated": created, "is_archived": false,
		"state": "in_progress", "todo_state": todo}
}

// ListRecordSaveWrites are the saved.* calls made for list records, in
// order (tests).
func (s *Server) ListRecordSaveWrites() []url.Values {
	rs := s.recordSaves()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]url.Values(nil), rs.writes...)
}

// SavedListRecord is a list record's saved item as the fake holds it
// (nil when it isn't saved).
func (s *Server) SavedListRecord(list, rec string) map[string]any {
	rs := s.recordSaves()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if it := rs.items[list+"/"+rec]; it != nil {
		out := map[string]any{}
		for k, v := range it {
			out[k] = v
		}
		return out
	}
	return nil
}

func (s *Server) handleListRecords(method string, f url.Values) (any, bool) {
	switch method {
	case "saved.add", "saved.update", "saved.delete":
		if f.Get("item_type") != "list_record" {
			return nil, false
		}
		rs := s.recordSaves()
		k := f.Get("item_id") + "/" + f.Get("ts")
		rs.mu.Lock()
		rs.writes = append(rs.writes, f)
		it := rs.items[k]
		now := time.Now().Unix()
		switch method {
		case "saved.add":
			if it != nil {
				rs.mu.Unlock()
				return map[string]any{"ok": false, "error": "already_saved"}, true
			}
			due, _ := strconv.ParseInt(f.Get("date_due"), 10, 64)
			todo := "saved"
			if due > 0 || f.Get("todo_state") == "to_do" {
				todo = "to_do"
			}
			if due == 0 && todo == "to_do" {
				due = now
			}
			it = savedRecord(f.Get("item_id"), f.Get("ts"), todo, now, due)
			rs.items[k] = it
		case "saved.update":
			if it == nil {
				rs.mu.Unlock()
				return map[string]any{"ok": false, "error": "not_found"}, true
			}
			if d := f.Get("date_due"); d != "" {
				n, _ := strconv.ParseInt(d, 10, 64)
				it["date_due"] = n
				if it["todo_state"] == "saved" && n > 0 {
					it["todo_state"] = "to_do"
				}
			}
			switch f.Get("mark") {
			case "uncompleted":
				it["date_completed"], it["todo_state"] = int64(0), "to_do"
			case "unarchived":
				it["is_archived"] = false
			}
			it["date_updated"] = now
		case "saved.delete":
			if it == nil {
				rs.mu.Unlock()
				return map[string]any{"ok": false, "error": "not_found"}, true
			}
			delete(rs.items, k)
		}
		ev := map[string]string{"saved.add": "saved_added", "saved.update": "saved_updated", "saved.delete": "saved_deleted"}[method]
		var copied map[string]any
		if it != nil {
			b, _ := json.Marshal(it)
			_ = json.Unmarshal(b, &copied)
		} else {
			copied = map[string]any{"item_id": f.Get("item_id"), "item_type": "list_record", "ts": f.Get("ts")}
		}
		rs.mu.Unlock()
		go s.Push(map[string]any{"type": ev, "saved": copied, "event_ts": fmt.Sprintf("%d.000800", now)})
		return ok(), true
	case "saved.list":
		if !s.Size.ListRecordsInLater {
			return nil, false
		}
		rs := s.recordSaves()
		rs.mu.Lock()
		keys := make([]string, 0, len(rs.items))
		for k := range rs.items {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		todos := f.Get("filter") == "todo" || f.Get("filter") == "todo_with_completed"
		items := []map[string]any{}
		for _, k := range keys {
			it := rs.items[k]
			isTodo := it["todo_state"] == "to_do" || it["todo_state"] == "completed"
			if isTodo == todos {
				items = append(items, it)
			}
		}
		rs.mu.Unlock()
		return ok("saved_items", items, "counts", map[string]any{"todo_count": len(items)}), true
	case "lists.records.archive":
		var ids []string
		_ = json.Unmarshal([]byte(f.Get("ids")), &ids)
		ls := s.listStore()
		ls.mu.Lock()
		l, okl := ls.byID[f.Get("list_id")]
		if !okl {
			ls.mu.Unlock()
			return map[string]any{"ok": false, "error": "list_not_found"}, true
		}
		gone := map[string]bool{}
		for _, id := range ids {
			gone[id] = true
		}
		kept := l.Records[:0]
		for _, r := range l.Records {
			if !gone[r.ID] {
				kept = append(kept, r)
			}
		}
		l.Records = kept
		ls.archives = append(ls.archives, f)
		ls.mu.Unlock()
		go s.Push(map[string]any{"type": "list_records_archived", "list_id": l.ID, "ids": ids,
			"event_ts": fmt.Sprintf("%d.000900", time.Now().Unix())})
		return ok(), true
	}
	return nil, false
}

// ListArchives are the lists.records.archive calls (tests).
func (s *Server) ListArchives() []url.Values {
	ls := s.listStore()
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return append([]url.Values(nil), ls.archives...)
}
