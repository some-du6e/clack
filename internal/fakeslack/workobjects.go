package fakeslack

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Work object fixtures: Size.WorkObjects adds #work-objects, messages
// whose links an app unfurled as work objects, in the shape Slack's
// client gets them (an attachment's work_object_entity: the entity's
// work_object_type "external", work_object_entity_type — the app's
// entity_type, "slack#/entities/task" … — and entity_id W:<app>:<the
// app's external_ref id>, with the compact, expanded and full layouts
// Slack's server builds from the app's entity_payload), and the methods
// and events the cards and their flexpane use:
//
//   - workObjects.getEntity answers ok, then entity_details_present
//     brings the live details (data_freshness "live")
//   - workObjects.getComments answers ok, then entity_comments_present
//     brings the comments; workObjects.postComment answers ok and
//     entity_comment_action_acknowledged brings the posted comment
//   - entities.refs.list lists the messages that shared an entity
//   - chat.refreshUnfurl answers ok and re-sends the message with the
//     layouts' updated_at moved to now (message_changed)
//
// Every name, task and picture is made up; images are this server's /img.

const (
	WorkObjectsChannelID = "C0WORKOBJ01"
	woBaseTS             = 1762700000

	// WOTrackerAppID is the made-up issue tracker app, WODocsAppID the
	// made-up docs app.
	WOTrackerAppID = "A0FAKEWOT1"
	WODocsAppID    = "A0FAKEWOD1"
	// WOTaskID is the task the first message unfurls (comments, an
	// updated_at, primary actions); WODocA and WODocB the two docs the
	// second message shares (compact cards side by side when rearranged).
	WOTaskID = "W:" + WOTrackerAppID + ":TASK-142"
	WODocA   = "W:" + WODocsAppID + ":doc-roadmap"
	WODocB   = "W:" + WODocsAppID + ":doc-notes"
)

// woTS is the fixture message i's ts.
func woTS(i int) string { return strconv.Itoa(woBaseTS+i*60) + ".000300" }

// WorkObjectTS are the #work-objects messages' ts: the task, the two
// docs.
var WorkObjectTS = []string{woTS(0), woTS(1)}

type woState struct {
	mu        sync.Mutex
	posted    []url.Values // workObjects.postComment calls
	refreshed []url.Values // chat.refreshUnfurl calls
	fetched   []url.Values // workObjects.getEntity calls
	comments  map[string][]map[string]any
}

var woStores sync.Map

func (s *Server) wo() *woState {
	if v, ok := woStores.Load(s); ok {
		return v.(*woState)
	}
	v, _ := woStores.LoadOrStore(s, &woState{comments: map[string][]map[string]any{}})
	return v.(*woState)
}

// WorkObjectCalls returns the workObjects.getEntity, workObjects.postComment
// and chat.refreshUnfurl calls so far.
func (s *Server) WorkObjectCalls() (fetched, posted, refreshed []url.Values) {
	st := s.wo()
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]url.Values(nil), st.fetched...), append([]url.Values(nil), st.posted...), append([]url.Values(nil), st.refreshed...)
}

// woImg is a fixture image on the server's /img.
func (s *Server) woImg(w, h int, seed string) string {
	return s.URL + "img/" + strconv.Itoa(w) + "x" + strconv.Itoa(h) + "/" + seed + ".png"
}

// woText is a plain text object.
func woText(t string) map[string]any { return map[string]any{"type": "plain_text", "text": t} }

// woRich is a one-paragraph rich_text block.
func woRich(t string) map[string]any {
	return map[string]any{"type": "rich_text", "elements": []any{map[string]any{"type": "rich_text_section",
		"elements": []any{map[string]any{"type": "text", "text": t}}}}}
}

// WorkObjectTask is the tracker task's entity at updatedAt (unix
// seconds), as the attachment and entity_details_present carry it.
func (s *Server) WorkObjectTask(updatedAt int64) map[string]any {
	icon := map[string]any{"type": "image", "image_url": s.woImg(72, 72, "wo-tracker-product"), "alt_text": "Tracker"}
	appIcons := map[string]any{"image_36": map[string]any{"image_url": s.woImg(36, 36, "wo-tracker-app"), "alt_text": "Example Tracker"},
		"image_72": map[string]any{"image_url": s.woImg(72, 72, "wo-tracker-app"), "alt_text": "Example Tracker"}}
	assignee := s.users[2%len(s.users)].ID
	fields := []any{
		map[string]any{"field_type": "tag", "label": "Status", "field_name": "status", "tag": []any{map[string]any{"text": "In progress", "color": "lagoon"}}},
		map[string]any{"field_type": "user", "label": "Assignee", "field_name": "assignee", "user": []any{map[string]any{"user_type": "slack", "user_id": assignee}}},
		map[string]any{"field_type": "date", "label": "Due date", "field_name": "due_date", "date": []any{"2026-11-20"}},
		map[string]any{"field_type": "tag", "label": "Priority", "field_name": "priority", "tag": []any{map[string]any{"text": "High", "color": "flamingo"}}},
	}
	full := append(append([]any{}, fields...),
		map[string]any{"field_type": "rich_text", "label": "Description", "field_name": "description", "long": true,
			"rich_text": woRich("Cards render like Slack's ExternalWorkObjectEntity: the header, the fields and the footer's refresh line.")},
		map[string]any{"field_type": "text", "label": "Project", "field_name": "project", "text": "Clack desktop"})
	actions := map[string]any{
		"primary_actions": []any{map[string]any{"type": "button", "action_id": "wo_task_start", "block_id": "wo_task", "text": woText("Start work")}},
		"overflow_actions": map[string]any{"app_actions": []any{map[string]any{"type": "button", "action_id": "wo_task_assign",
			"block_id": "wo_task", "text": woText("Assign to me")}}},
		"block_payload": map[string]any{"service_id": "B0FAKEWOT1", "service_team_id": TeamID, "container": map[string]any{"type": "message_attachment"}},
	}
	title := "Draw work object cards like Slack"
	layouts := map[string]any{
		"compact": map[string]any{"title": woText(title), "subtitle": woText("TASK-142 · Clack desktop"), "product_icon": icon,
			"actions": actions, "updated_at": updatedAt},
		"expanded": map[string]any{"title": woText(title), "subtitle": woText("TASK-142 · Clack desktop"), "product_icon": icon,
			"header_title": woText("Tracker"), "fields": map[string]any{"elements": fields}, "actions": actions, "updated_at": updatedAt},
		"full": map[string]any{"title": woText(title), "subtitle": map[string]any{"type": "plain_text", "text": "Example Tracker",
			"url": "https://tracker.example.com/TASK-142"}, "header_title": woText("Task"), "header_subtitle": woText("Tracker"),
			"product_icon": icon, "fields": map[string]any{"elements": full},
			"actions": map[string]any{"primary_actions": []any{map[string]any{"type": "open-in-app"}}}, "updated_at": updatedAt},
	}
	return map[string]any{
		"work_object_type": "external", "work_object_entity_type": "slack#/entities/task", "entity_id": WOTaskID,
		"app_id": WOTrackerAppID, "app_name": "Example Tracker", "product_name": "Tracker", "display_type": "Task",
		"external_url": "https://tracker.example.com/TASK-142", "app_icons": appIcons, "is_refreshable": true,
		"comments": map[string]any{"count": 2, "latest_timestamp": woBaseTS - 600}, "ts": strconv.Itoa(woBaseTS - 300),
		"layouts": layouts,
	}
}

// woDoc is a docs app document entity.
func (s *Server) woDoc(id, title, sub string) map[string]any {
	icon := map[string]any{"type": "image", "image_url": s.woImg(72, 72, "wo-docs-product"), "alt_text": "Docs"}
	appIcons := map[string]any{"image_72": map[string]any{"image_url": s.woImg(72, 72, "wo-docs-app"), "alt_text": "Example Docs"}}
	fields := []any{map[string]any{"field_type": "user", "label": "Owner", "field_name": "owner",
		"user": []any{map[string]any{"user_type": "external", "text": "Robin Example",
			"image": map[string]any{"image_url": s.woImg(48, 48, "wo-robin"), "alt_text": "Robin Example"}}}},
		map[string]any{"field_type": "timestamp", "label": "Last edited", "field_name": "last_edited", "timestamp": []any{woBaseTS - 86400}}}
	_, obj, _ := strings.Cut(strings.TrimPrefix(id, "W:"), ":")
	return map[string]any{
		"work_object_type": "external", "work_object_entity_type": "slack#/entities/content_item", "entity_id": id,
		"app_id": WODocsAppID, "app_name": "Example Docs", "product_name": "Docs", "display_type": "Document",
		"external_url": "https://docs.example.com/d/" + obj, "app_icons": appIcons,
		"layouts": map[string]any{
			"compact":  map[string]any{"title": woText(title), "subtitle": woText(sub), "product_icon": icon},
			"expanded": map[string]any{"title": woText(title), "subtitle": woText(sub), "product_icon": icon, "header_title": woText("Docs"), "fields": map[string]any{"elements": fields}},
			"full":     map[string]any{"title": woText(title), "subtitle": woText("Example Docs"), "header_title": woText("Document"), "fields": map[string]any{"elements": fields}},
		},
	}
}

// woAttachment wraps an entity as the attachment Slack sends for an app
// unfurl with a work object.
func woAttachment(id int, link string, entity map[string]any) slack.Attachment {
	raw, _ := json.Marshal(entity)
	return slack.Attachment{ID: id, FromURL: link, OriginalURL: link, AppUnfurlURL: link, IsAppUnfurl: true,
		WorkObjectEntity: raw, AppID: fmt.Sprint(entity["app_id"]), Fallback: fmt.Sprint(entity["display_type"])}
}

func (s *Server) addWorkObjectsChannel() {
	s.convs = append(s.convs, slack.Conversation{ID: WorkObjectsChannelID, Name: "work-objects", IsChannel: true, IsMember: true,
		NumMembers: 6, ContextTeamID: TeamID, Purpose: &slack.Purpose{Value: "Work object fixtures."}})
	task := s.WorkObjectTask(woBaseTS - 300)
	const taskURL = "https://tracker.example.com/TASK-142"
	docA := s.woDoc(WODocA, "Clack roadmap", "Docs · Edited yesterday")
	docB := s.woDoc(WODocB, "Meeting notes", "Docs · Edited Monday")
	msgs := []slack.Message{
		{Type: "message", TS: woTS(0), User: s.users[1%len(s.users)].ID, Text: "Picking this up: <" + taskURL + ">",
			Attachments: []slack.Attachment{woAttachment(1, taskURL, task)}, ClientMsgID: "00000000-workobj-0"},
		{Type: "message", TS: woTS(1), User: s.users[3%len(s.users)].ID,
			Text: "Both docs: <https://docs.example.com/d/doc-roadmap> <https://docs.example.com/d/doc-notes>",
			Attachments: []slack.Attachment{woAttachment(1, "https://docs.example.com/d/doc-roadmap", docA),
				woAttachment(2, "https://docs.example.com/d/doc-notes", docB)}, ClientMsgID: "00000000-workobj-1"},
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 { // history is newest first
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	s.hist[WorkObjectsChannelID] = msgs
	st := s.wo()
	st.mu.Lock()
	st.comments[WOTaskID] = []map[string]any{
		{"id": "c1", "sender": map[string]any{"user_type": "slack", "user_id": s.users[1%len(s.users)].ID}, "timestamp": woBaseTS - 1200,
			"comment": map[string]any{"blocks": woRich("Can we match the hover bar too?")}, "can_reply": true},
		{"id": "c2", "sender": map[string]any{"user_type": "external", "text": "Robin Example",
			"image": map[string]any{"image_url": s.woImg(48, 48, "wo-robin")}}, "timestamp": woBaseTS - 600,
			"comment": map[string]any{"value": "Yes, and the refresh line."}},
	}
	st.mu.Unlock()
}

// woEntityByID finds a fixture entity.
func (s *Server) woEntityByID(id string) map[string]any {
	switch id {
	case WOTaskID:
		return s.WorkObjectTask(woBaseTS - 300)
	case WODocA:
		return s.woDoc(WODocA, "Clack roadmap", "Docs · Edited yesterday")
	case WODocB:
		return s.woDoc(WODocB, "Meeting notes", "Docs · Edited Monday")
	}
	return nil
}

// handleWorkObjects answers the work object methods.
func (s *Server) handleWorkObjects(method string, f url.Values) (any, bool) {
	st := s.wo()
	switch method {
	case "workObjects.getEntity":
		st.mu.Lock()
		st.fetched = append(st.fetched, f)
		st.mu.Unlock()
		id := f.Get("entity_id")
		e := s.woEntityByID(id)
		if e == nil {
			return map[string]any{"ok": false, "error": "entity_not_found"}, true
		}
		go func() {
			time.Sleep(50 * time.Millisecond)
			s.Push(map[string]any{"type": "entity_details_present", "entity_id": id, "entity_url": e["external_url"],
				"entity_details": e, "data_freshness": "live", "event_ts": fmt.Sprintf("%d.000300", time.Now().Unix())})
		}()
		return ok(), true
	case "workObjects.getComments":
		id := f.Get("entity_id")
		st.mu.Lock()
		list := append([]map[string]any(nil), st.comments[id]...)
		st.mu.Unlock()
		go func() {
			time.Sleep(50 * time.Millisecond)
			s.Push(map[string]any{"type": "entity_comments_present", "entity_id": id, "comments": list, "can_post_comment": true})
		}()
		return ok(), true
	case "workObjects.postComment":
		id := f.Get("entity_id")
		var cm struct {
			Blocks json.RawMessage `json:"blocks"`
		}
		_ = json.Unmarshal([]byte(f.Get("comment")), &cm)
		st.mu.Lock()
		st.posted = append(st.posted, f)
		n := len(st.comments[id]) + 1
		c := map[string]any{"id": "c" + strconv.Itoa(n), "sender": map[string]any{"user_type": "slack", "user_id": SelfID},
			"timestamp": time.Now().Unix(), "comment": map[string]any{"blocks": cm.Blocks}, "can_edit": true, "can_delete": true}
		st.comments[id] = append(st.comments[id], c)
		st.mu.Unlock()
		go func() {
			time.Sleep(50 * time.Millisecond)
			s.Push(map[string]any{"type": "entity_comment_action_acknowledged", "comment_action": "post", "entity_id": id, "comment": c})
		}()
		return ok(), true
	case "search.modules.workObjects":
		return s.searchWorkObjects(f), true
	case "entities.refs.list":
		if f.Get("entity_id") != WOTaskID {
			return ok("refs", []any{}), true
		}
		return ok("refs", []any{map[string]any{"channel_id": WorkObjectsChannelID, "timestamp": woTS(0)}}), true
	case "chat.refreshUnfurl":
		st.mu.Lock()
		st.refreshed = append(st.refreshed, f)
		st.mu.Unlock()
		ch, ts := f.Get("channel"), f.Get("ts")
		if ch != WorkObjectsChannelID || ts != woTS(0) {
			return map[string]any{"ok": false, "error": "cant_refresh"}, true
		}
		go func() {
			time.Sleep(100 * time.Millisecond)
			task := s.WorkObjectTask(time.Now().Unix())
			s.histMu.Lock()
			var m slack.Message
			for i := range s.hist[ch] {
				if s.hist[ch][i].TS == ts {
					s.hist[ch][i].Attachments = []slack.Attachment{woAttachment(1, "https://tracker.example.com/TASK-142", task)}
					m = s.hist[ch][i]
				}
			}
			s.histMu.Unlock()
			if m.TS != "" {
				s.Push(map[string]any{"type": "message", "subtype": "message_changed", "channel": ch, "message": m, "ts": ts, "hidden": true})
			}
		}()
		return ok(), true
	}
	return nil, false
}

// searchWorkObjects answers search.modules.workObjects: the fixture
// entities whose title has every query word, as work_object_unfurl items.
func (s *Server) searchWorkObjects(f url.Values) map[string]any {
	q := strings.ToLower(strings.TrimSpace(f.Get("query")))
	var items []any
	for _, id := range []string{WOTaskID, WODocA, WODocB} {
		e := s.woEntityByID(id)
		raw, _ := json.Marshal(e)
		hay := strings.ToLower(string(raw))
		match := q != ""
		for _, w := range strings.Fields(q) {
			if !strings.Contains(hay, w) {
				match = false
			}
		}
		if match {
			items = append(items, map[string]any{"work_object_unfurl": e, "iid": "wo-" + id})
		}
	}
	if items == nil {
		items = []any{}
	}
	return ok("items", items, "query", f.Get("query"))
}
