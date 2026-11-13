package demo

import (
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Saved for later and To-dos (saved.list / add / update / delete): the
// viewer starts with a few saved messages and a short to-do list from
// their first week, one of them overdue and one done; what they save,
// complete or archive in the demo changes it, and every change is echoed
// as saved_added / saved_updated / saved_deleted like Slack's server.

type savedItem struct {
	ch, ts    string
	todo      string // "saved", "to_do", "completed"
	created   int64
	due       int64
	completed int64
	archived  bool
}

func (it *savedItem) json() map[string]any {
	state := "in_progress"
	if it.todo == "completed" {
		state = "completed"
	}
	if it.archived {
		state = "archived"
	}
	return map[string]any{"item_id": it.ch, "item_type": "message", "ts": it.ts, "state": state, "todo_state": it.todo,
		"date_created": it.created, "date_due": it.due, "date_completed": it.completed, "is_archived": it.archived,
		"date_updated": max(it.created, it.completed)}
}

// seedSavedLocked saves a few messages from the backstory: the newest
// message (top level) of the named people in these channels.
func (w *World) seedSavedLocked() {
	now := w.now()
	type seed struct {
		ch, by, todo string
		dueIn        time.Duration // 0: no due date
		done         bool
		hoursAgo     int
	}
	seeds := []seed{
		{ch: ITHelpID, by: "theo", todo: "saved", hoursAgo: 50},                           // the pinned how-to
		{ch: GeneralID, by: "gordon", todo: "to_do", dueIn: 26 * time.Hour, hoursAgo: 30}, // the Quillies form
		{ch: PartyID, by: "marisol", todo: "to_do", dueIn: -20 * time.Hour, hoursAgo: 48}, // overdue
		{ch: WareID, by: "ruth", todo: "to_do", dueIn: 3 * 24 * time.Hour, hoursAgo: 70},  // the training module
		{ch: SalesID, by: "priya", todo: "completed", done: true, hoursAgo: 60},           // done
		{ch: ConnectID, by: "ines", todo: "saved", hoursAgo: 3},                           // Ines's contact note
		{ch: CodeID, by: "inkwell", todo: "to_do", dueIn: 5 * time.Hour, hoursAgo: 20},    // review the PR
	}
	for _, s := range seeds {
		p, ok := w.person(s.by)
		if !ok {
			continue
		}
		var ts string
		for _, m := range w.hist[s.ch] { // newest first
			if m.User == p.ID && m.Subtype == "" {
				ts = m.TS
				break
			}
		}
		if ts == "" {
			continue
		}
		it := &savedItem{ch: s.ch, ts: ts, todo: s.todo, created: now.Add(-time.Duration(s.hoursAgo) * time.Hour).Unix()}
		if s.dueIn != 0 {
			it.due = now.Add(s.dueIn).Unix()
		}
		if s.done {
			it.completed = now.Add(-time.Duration(s.hoursAgo/2) * time.Hour).Unix()
		}
		w.saved[s.ch+"/"+ts] = it
	}
}

// savedList answers saved.list: saved_with_archived (the Saved page)
// lists bookmarks, todo_with_completed (To-dos) the to-dos with the
// completed ones; saved, todo, completed and archived narrow further.
func (w *World) savedList(f url.Values) any {
	filter := f.Get("filter")
	w.mu.Lock()
	var all []*savedItem
	for _, it := range w.saved {
		keep := false
		switch filter {
		case "", "saved_with_archived":
			keep = it.todo == "saved"
		case "saved":
			keep = it.todo == "saved" && !it.archived
		case "todo_with_completed":
			keep = it.todo == "to_do" || it.todo == "completed"
		case "todo":
			keep = it.todo == "to_do" && !it.archived
		case "completed":
			keep = it.todo == "completed"
		case "archived":
			keep = it.archived
		default:
			keep = true
		}
		if keep {
			all = append(all, it)
		}
	}
	counts := map[string]int{}
	now := w.now().Unix()
	for _, it := range w.saved {
		switch {
		case it.archived:
			counts["archived"]++
		case it.todo == "completed":
			counts["completed"]++
		default:
			counts["uncompleted"]++
			if it.due > 0 && it.due < now {
				counts["overdue"]++
			}
		}
	}
	w.mu.Unlock()
	sort.Slice(all, func(i, j int) bool {
		if all[i].created != all[j].created {
			return all[i].created > all[j].created
		}
		return all[i].ts > all[j].ts
	})
	items := []any{}
	for _, it := range all {
		items = append(items, it.json())
	}
	return okMap("saved_items", items, "counts", map[string]any{"uncompleted_count": counts["uncompleted"],
		"uncompleted_overdue_count": counts["overdue"], "archived_count": counts["archived"], "completed_count": counts["completed"],
		"total_count": len(w.saved)}, "response_metadata", map[string]any{"next_cursor": ""})
}

// savedGet answers saved.get {items}: the reminder rows' saved items
// (items that aren't saved are left out, as Slack leaves them).
func (w *World) savedGet(f url.Values) any {
	var keys []struct {
		ItemID   string `json:"item_id"`
		ItemType string `json:"item_type"`
		TS       string `json:"ts"`
	}
	_ = json.Unmarshal([]byte(f.Get("items")), &keys)
	w.mu.Lock()
	defer w.mu.Unlock()
	items := []any{}
	for _, k := range keys {
		if it, ok := w.saved[k.ItemID+"/"+k.TS]; ok && (k.ItemType == "" || k.ItemType == "message") {
			items = append(items, it.json())
		}
	}
	return okMap("saved_items", items)
}

// savedWrite answers saved.add / delete / update and pushes the event.
func (w *World) savedWrite(method string, f url.Values) any {
	key := f.Get("item_id") + "/" + f.Get("ts")
	if f.Get("item_type") != "" && f.Get("item_type") != "message" {
		key = f.Get("item_type") + ":" + f.Get("item_id")
	}
	w.mu.Lock()
	it := w.saved[key]
	now := w.now().Unix()
	evType := ""
	switch method {
	case "saved.add":
		if it != nil {
			w.mu.Unlock()
			return failMap("already_saved")
		}
		it = &savedItem{ch: f.Get("item_id"), ts: f.Get("ts"), todo: orStr(f.Get("todo_state"), "saved"), created: now}
		if d, _ := strconv.ParseInt(f.Get("date_due"), 10, 64); d > 0 {
			it.due = d
			if it.todo == "saved" {
				it.todo = "to_do"
			}
		}
		w.saved[key] = it
		evType = "saved_added"
	case "saved.delete":
		if it == nil {
			w.mu.Unlock()
			return failMap("not_found")
		}
		delete(w.saved, key)
		evType = "saved_deleted"
	case "saved.update":
		if it == nil {
			w.mu.Unlock()
			return failMap("not_found")
		}
		if t := f.Get("todo_state"); t != "" {
			it.todo = t
		}
		switch f.Get("mark") {
		case "completed":
			it.todo, it.completed = "completed", now
		case "uncompleted":
			it.todo, it.completed = "to_do", 0
		case "archived":
			it.archived = true
		case "unarchived":
			it.archived = false
		}
		if d := f.Get("date_due"); d != "" {
			it.due, _ = strconv.ParseInt(d, 10, 64)
		}
		evType = "saved_updated"
	default:
		w.mu.Unlock()
		return okMap()
	}
	item := it.json()
	w.mu.Unlock()
	if !strings.Contains(key, ":") {
		w.fs.Push(map[string]any{"type": evType, "saved": item, "event_ts": microTS(w.now().UnixMicro())})
	}
	return okMap()
}

// seedActivityExtrasLocked adds the Activity items the history alone
// doesn't imply: Marisol assigning the viewer a row in the restock list,
// a reminder for the overdue to-do, and Wendell adding the viewer to the
// Slack Connect channel.
func (w *World) seedActivityExtrasLocked() {
	add := func(feedTS string, unread bool, item map[string]any) {
		w.actSeq++
		e := &actEntry{Key: "demo-x" + strconv.Itoa(w.actSeq), FeedTS: feedTS, Type: item["type"].(string), Unread: unread, Item: item}
		w.activity = append(w.activity, e)
	}
	id := func(h string) string { p, _ := w.person(h); return p.ID }
	now := w.now()
	ts := func(ago time.Duration) string { return microTS(now.Add(-ago).UnixMicro()) }
	add(ts(70*time.Hour), false, map[string]any{"type": "list_record_assigned", "linked_item_id": RestockListID,
		"list_payload": map[string]any{"list_id": RestockListID, "row_id": "Rec0DEMO007", "assigned_by": id("marisol")}})
	for _, it := range w.saved {
		if it.todo == "to_do" && it.due > 0 && it.due < now.Unix() {
			add(microTS(it.due*1e6), true, map[string]any{"type": "saved_reminder",
				"saved_reminder_payload": map[string]any{"item_type": "message", "item_id": it.ch, "item_id_2": it.ts}})
		}
	}
	for _, m := range w.hist[ConnectID] {
		if m.User == id("wendell") {
			add(microTS(tsMicros(m.TS)-90e6), false, map[string]any{"type": "internal_channel_invite",
				"message": map[string]any{"ts": m.TS, "channel": ConnectID, "author_user_id": m.User, "user": m.User}})
			break
		}
	}
	sort.SliceStable(w.activity, func(i, j int) bool { return tsBefore(w.activity[j].FeedTS, w.activity[i].FeedTS) })
}
