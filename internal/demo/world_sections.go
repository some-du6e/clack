package demo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Sidebar sections: users.channelSections.list from ws.Sections, and the
// section menus' writes (create, rename, delete, move channels) kept so
// the list reads them back, as fakeslack's sections.go does for its own.

type liveSection struct {
	ID, Name, Emoji, Type string
	Channels              []string
}

func (w *World) seedSections() {
	for _, s := range w.ws.Sections {
		w.sections = append(w.sections, &liveSection{ID: s.ID, Name: s.Name, Emoji: s.Emoji, Type: s.Type,
			Channels: append([]string(nil), s.Channels...)})
	}
	has := map[string]bool{}
	for _, s := range w.sections {
		has[s.Type] = true
	}
	// Slack always has the two default sections; a Workspace without
	// them still gets a sidebar
	if !has["channels"] {
		w.sections = append(w.sections, &liveSection{ID: "L0DEMOCHAN", Type: "channels"})
	}
	if !has["direct_messages"] {
		w.sections = append(w.sections, &liveSection{ID: "L0DEMODMS0", Type: "direct_messages"})
	}
}

func (w *World) sectionsList() any {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]map[string]any, 0, len(w.sections))
	for i, s := range w.sections {
		next := ""
		if i+1 < len(w.sections) {
			next = w.sections[i+1].ID
		}
		m := map[string]any{"channel_section_id": s.ID, "name": s.Name, "type": s.Type, "emoji": s.Emoji,
			"next_channel_section_id": next, "last_updated": w.t0.Unix(), "is_redacted": false}
		if s.Type == "standard" || s.Type == "stars" {
			ids := append([]string{}, s.Channels...)
			m["channel_ids_page"] = map[string]any{"channel_ids": ids, "count": len(ids)}
		}
		out = append(out, m)
	}
	return okMap("channel_sections", out, "last_updated", w.t0.Unix())
}

func (w *World) sectionCall(method string, f url.Values) (any, []any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	find := func(id string) int {
		for i, s := range w.sections {
			if s.ID == id {
				return i
			}
		}
		return -1
	}
	upsert := func(s *liveSection) []any {
		return []any{map[string]any{"type": "channel_sections_upserted", "channel_section_id": s.ID, "name": s.Name, "emoji": s.Emoji,
			"channel_section_type": s.Type}}
	}
	switch method {
	case "users.channelSections.create":
		name := strings.TrimSpace(f.Get("name"))
		if name == "" {
			return failMap("invalid_name"), nil
		}
		for _, s := range w.sections {
			if strings.EqualFold(s.Name, name) {
				return failMap("already_exists"), nil
			}
		}
		w.secSeq++
		s := &liveSection{ID: fmt.Sprintf("L0DEMONEW%d", w.secSeq), Name: name, Emoji: f.Get("emoji"), Type: "standard"}
		at := 0
		if n := find(f.Get("next_channel_section_id")); n >= 0 {
			at = n
		}
		w.sections = append(w.sections[:at], append([]*liveSection{s}, w.sections[at:]...)...)
		return okMap("channel_section_id", s.ID), upsert(s)
	case "users.channelSections.set":
		i := find(f.Get("channel_section_id"))
		if i < 0 {
			return failMap("section_not_found"), nil
		}
		s := w.sections[i]
		if _, has := f["name"]; has {
			s.Name, s.Emoji = f.Get("name"), f.Get("emoji")
		}
		return okMap(), upsert(s)
	case "users.channelSections.delete":
		i := find(f.Get("channel_section_id"))
		if i < 0 {
			return failMap("section_not_found"), nil
		}
		if t := w.sections[i].Type; t != "standard" && t != "stars" {
			return failMap("invalid_section_type"), nil
		}
		id := w.sections[i].ID
		w.sections = append(w.sections[:i], w.sections[i+1:]...)
		return okMap(), []any{map[string]any{"type": "channel_sections_deleted", "channel_section_id": id}}
	case "users.channelSections.channels.bulkUpdate":
		var ins, rem []struct {
			Section  string   `json:"channel_section_id"`
			Channels []string `json:"channel_ids"`
		}
		if json.Unmarshal([]byte(orJSON(f.Get("insert"), "[]")), &ins) != nil || json.Unmarshal([]byte(orJSON(f.Get("remove"), "[]")), &rem) != nil {
			return failMap("invalid_arguments"), nil
		}
		for _, r := range rem {
			if i := find(r.Section); i >= 0 {
				drop := map[string]bool{}
				for _, c := range r.Channels {
					drop[c] = true
				}
				var keep []string
				for _, c := range w.sections[i].Channels {
					if !drop[c] {
						keep = append(keep, c)
					}
				}
				w.sections[i].Channels = keep
			}
		}
		for _, in := range ins {
			if i := find(in.Section); i >= 0 {
				// a channel lives in one custom section: moving it takes it out
				// of the others
				for _, s := range w.sections {
					if s.Type == "standard" {
						var keep []string
						for _, c := range s.Channels {
							if !hasStr(in.Channels, c) {
								keep = append(keep, c)
							}
						}
						s.Channels = keep
					}
				}
				for _, c := range in.Channels {
					if !hasStr(w.sections[i].Channels, c) {
						w.sections[i].Channels = append([]string{c}, w.sections[i].Channels...)
					}
				}
			}
		}
		return okMap(), []any{map[string]any{"type": "channel_sections_channels_upserted"}}
	}
	return nil, nil
}

func orJSON(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func hasStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func okMap(kv ...any) map[string]any {
	m := map[string]any{"ok": true}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func failMap(code string) map[string]any { return map[string]any{"ok": false, "error": code} }
