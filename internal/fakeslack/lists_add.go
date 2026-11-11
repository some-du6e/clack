package fakeslack

// Adding to lists (internal/core/lists_add.go, docs/LISTS.md):
// lists.addToList appends an item holding the message in the list's
// message column (made first when the list has none, as Slack warns:
// "Continuing will automatically create one"), lists.records.open counts
// opens, and conversations.addTab adds tabs; ListTabChan's
// conversations.info lists them after its fixture tabs. Every call is
// recorded for tests. Nothing here comes from a real workspace.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/lists"
)

type listAdds struct {
	mu    sync.Mutex
	adds  []url.Values                // lists.addToList
	opens []url.Values                // lists.records.open
	tabs  []url.Values                // conversations.addTab
	added map[string][]map[string]any // channel -> tabs conversations.addTab added
	full  map[string]bool             // channels whose tab bar is full (too_many_tabs)
}

var listAddStores sync.Map // *Server -> *listAdds

func (s *Server) listAdds() *listAdds {
	if v, ok := listAddStores.Load(s); ok {
		return v.(*listAdds)
	}
	v, _ := listAddStores.LoadOrStore(s, &listAdds{added: map[string][]map[string]any{}, full: map[string]bool{}})
	return v.(*listAdds)
}

// AddToListCalls returns lists.addToList's forms, oldest first.
func (s *Server) AddToListCalls() []url.Values {
	la := s.listAdds()
	la.mu.Lock()
	defer la.mu.Unlock()
	return append([]url.Values(nil), la.adds...)
}

// OpenRecordCalls returns lists.records.open's forms, oldest first.
func (s *Server) OpenRecordCalls() []url.Values {
	la := s.listAdds()
	la.mu.Lock()
	defer la.mu.Unlock()
	return append([]url.Values(nil), la.opens...)
}

// AddTabCalls returns conversations.addTab's forms, oldest first.
func (s *Server) AddTabCalls() []url.Values {
	la := s.listAdds()
	la.mu.Lock()
	defer la.mu.Unlock()
	return append([]url.Values(nil), la.tabs...)
}

// SetTabsFull makes conversations.addTab answer too_many_tabs for ch.
func (s *Server) SetTabsFull(ch string, full bool) {
	la := s.listAdds()
	la.mu.Lock()
	la.full[ch] = full
	la.mu.Unlock()
}

// addedTabs are the tabs conversations.addTab added to ch.
func (s *Server) addedTabs(ch string) []any {
	la := s.listAdds()
	la.mu.Lock()
	defer la.mu.Unlock()
	var out []any
	for _, t := range la.added[ch] {
		out = append(out, t)
	}
	return out
}

// histText is the text of message ts in ch, from the history the fake
// serves (made on first use, as historyPage does).
func (s *Server) histText(ch, ts string) (string, bool) {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	msgs, okc := s.hist[ch]
	if !okc {
		var gallery bool
		if msgs, gallery = s.galleryHistoryOf(ch); !gallery {
			msgs = s.history(ch)
		}
		s.hist[ch] = msgs
	}
	for _, m := range msgs {
		if m.TS == ts {
			return m.Text, true
		}
	}
	return "", false
}

func (s *Server) handleListsAdd(method string, f url.Values) (any, bool) {
	switch method {
	case "lists.addToList":
		la := s.listAdds()
		la.mu.Lock()
		la.adds = append(la.adds, f)
		la.mu.Unlock()
		ch, ts := f.Get("channel_id"), f.Get("message_ts")
		text, found := s.histText(ch, ts)
		if !found {
			return map[string]any{"ok": false, "error": "message_not_found"}, true
		}
		ls := s.listStore()
		ls.mu.Lock()
		l, okl := ls.byID[f.Get("list_id")]
		if !okl {
			ls.mu.Unlock()
			return map[string]any{"ok": false, "error": "list_not_found"}, true
		}
		var msgCol *lists.Column
		for i := range l.Meta.Schema {
			if l.Meta.Schema[i].Type == lists.Message {
				msgCol = &l.Meta.Schema[i]
				break
			}
		}
		if msgCol == nil {
			l.Meta.Schema = append(l.Meta.Schema, lists.Column{ID: "Col0NEWMSG01", Key: "Col0NEWMSG01", Name: "Message", Type: lists.Message})
			msgCol = &l.Meta.Schema[len(l.Meta.Schema)-1]
			for i := range l.Meta.Views {
				l.Meta.Views[i].Columns = append(l.Meta.Views[i].Columns, lists.ViewColumn{ID: msgCol.ID, Key: msgCol.Key, Visible: true,
					Position: fmt.Sprintf("%04d", len(l.Meta.Views[i].Columns))})
			}
		}
		ls.next++
		now := time.Now().Unix()
		r := lists.Record{ID: fmt.Sprintf("Rec0ADD%05d", ls.next), ListID: l.ID, DateCreated: now, CreatedBy: SelfID,
			UpdatedBy: SelfID, Position: fmt.Sprintf("%04d", 7000+ls.next), ViewPositions: map[string]string{},
			UpdatedTimestamp: fmt.Sprintf("%d.000100", now)}
		if p := l.Meta.Primary(); p != nil {
			name := strings.TrimSpace(text)
			blocks, _ := json.Marshal(lists.TextBlocks(name))
			r.SetField(lists.Field{Key: p.Key, ColumnID: p.ID, Text: &name, RichText: blocks})
		}
		link := "https://fake.slack.com/archives/" + ch + "/p" + strings.ReplaceAll(ts, ".", "")
		r.SetField(lists.Field{Key: msgCol.Key, ColumnID: msgCol.ID, Message: []lists.MessageValue{{Value: link, ChannelID: ch, TS: ts}}})
		l.Records = append(l.Records, r)
		l.Updated = now
		file := s.listFile(l)
		ls.mu.Unlock()
		go s.Push(map[string]any{"type": "list_record_created", "list_id": r.ListID, "record": r,
			"event_ts": fmt.Sprintf("%d.000610", now)})
		return ok("record", r, "list", file), true
	case "lists.records.open":
		la := s.listAdds()
		la.mu.Lock()
		la.opens = append(la.opens, f)
		la.mu.Unlock()
		ls := s.listStore()
		ls.mu.Lock()
		defer ls.mu.Unlock()
		l, okl := ls.byID[f.Get("list_id")]
		if !okl {
			return map[string]any{"ok": false, "error": "list_not_found"}, true
		}
		for _, r := range l.Records {
			if r.ID == f.Get("id") {
				return ok(), true
			}
		}
		return map[string]any{"ok": false, "error": "record_not_found"}, true
	case "conversations.addTab":
		la := s.listAdds()
		la.mu.Lock()
		defer la.mu.Unlock()
		la.tabs = append(la.tabs, f)
		ch := f.Get("channel_id")
		if la.full[ch] {
			return map[string]any{"ok": false, "error": "too_many_tabs"}, true
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(f.Get("data")), &data); err != nil || ch == "" || f.Get("type") == "" {
			return map[string]any{"ok": false, "error": "invalid_arguments"}, true
		}
		id, _ := data["file_id"].(string)
		for _, t := range la.added[ch] {
			if d, _ := t["data"].(map[string]any); d != nil && d["file_id"] == id {
				return map[string]any{"ok": false, "error": "file_already_added"}, true
			}
		}
		if acc, _ := data["access_level"].(string); acc != "" { // the tab shares the list with the channel
			ls := s.listStore()
			ls.mu.Lock()
			if l := ls.byID[id]; l != nil && l.Channel == "" {
				l.Channel = ch
			}
			ls.mu.Unlock()
		}
		tab := map[string]any{"id": fmt.Sprintf("Ct0ADD%03d", len(la.tabs)), "type": f.Get("type"), "label": f.Get("label"),
			"data": map[string]any{"file_id": id}}
		la.added[ch] = append(la.added[ch], tab)
		var tabz []any
		for _, t := range la.added[ch] {
			tabz = append(tabz, t)
		}
		return ok("tabz", tabz), true
	}
	return nil, false
}
