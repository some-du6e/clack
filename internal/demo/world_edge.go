package demo

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// edge is fakeslack's SetEdgeHook: the edge API (users and channels
// lookups, the member lists, counts) from the demo's directory.
func (w *World) edge(method string, body map[string]json.RawMessage) (any, bool) {
	str := func(k string) string {
		var s string
		_ = json.Unmarshal(body[k], &s)
		return s
	}
	ids := func() []string {
		var up map[string]any
		_ = json.Unmarshal(body["updated_ids"], &up)
		var out []string
		for id := range up {
			out = append(out, id)
		}
		if len(out) == 0 {
			_ = json.Unmarshal(body["ids"], &out)
		}
		sort.Strings(out)
		return out
	}
	switch method {
	case "edge/huddles/list":
		return w.activeHuddles(), true
	case "edge/users/info":
		res, failed := []slack.User{}, []string{}
		w.mu.Lock()
		for _, id := range ids() {
			if u, ok := w.userLocked(id); ok {
				res = append(res, u)
			} else {
				failed = append(failed, id)
			}
		}
		w.mu.Unlock()
		return okMap("results", res, "failed_ids", failed), true
	case "edge/users/list":
		var chans []string
		_ = json.Unmarshal(body["channels"], &chans)
		filter := str("filter")
		w.mu.Lock()
		var member map[string]bool
		for _, ch := range chans {
			if c, ok := w.convLocked(ch); ok {
				if member == nil {
					member = map[string]bool{}
				}
				for _, id := range w.membersLocked(c) {
					member[id] = true
				}
			}
		}
		w.mu.Unlock()
		res := []slack.User{}
		for _, u := range w.matchUsers("", true) {
			if member != nil && !member[u.ID] {
				continue
			}
			isApp := u.IsBot
			switch {
			case filter == "apps" && !isApp:
				continue
			case strings.Contains(filter, "NOT bots") && isApp:
				continue
			}
			res = append(res, u)
		}
		var presentFirst bool
		_ = json.Unmarshal(body["present_first"], &presentFirst)
		if presentFirst {
			sort.SliceStable(res, func(i, j int) bool { return res[i].Presence == "active" && res[j].Presence != "active" })
		}
		return okMap("results", res, "next_marker", ""), true
	case "edge/users/search":
		q := strings.ToLower(strings.TrimSpace(str("query")))
		res := w.matchUsers(q, true)
		if res == nil {
			res = []slack.User{}
		}
		return okMap("results", res), true
	case "edge/users/counts":
		ch := str("channel")
		people, bots := 0, 0
		w.mu.Lock()
		var ids []string
		if c, ok := w.convLocked(ch); ok {
			ids = w.membersLocked(c)
		} else {
			for id := range w.users {
				ids = append(ids, id)
			}
		}
		for _, id := range ids {
			if u, ok := w.users[id]; ok && u.IsBot {
				bots++
			} else {
				people++
			}
		}
		w.mu.Unlock()
		return okMap("counts", map[string]int{"everyone": people + bots, "people": people, "members": people, "guests": 0,
			"bots": bots, "apps": bots}), true
	case "edge/channels/search":
		q := strings.ToLower(strings.TrimSpace(str("query")))
		res := []map[string]any{}
		w.mu.Lock()
		for _, c := range w.convs {
			if c.IsIM || c.IsMPIM || (q != "" && !strings.Contains(c.Name, q)) {
				continue
			}
			res = append(res, map[string]any{"id": c.ID, "name": c.Name, "is_channel": c.IsChannel, "is_private": c.IsPrivate,
				"is_archived": c.IsArchived, "is_member": c.IsMember, "is_general": c.IsGeneral, "is_mpim": false,
				"num_members": w.memberCountLocked(c), "member_count": w.memberCountLocked(c), "created": c.Created,
				"creator": c.Creator, "topic": c.Topic, "purpose": c.Purpose, "context_team_id": TeamID})
		}
		w.mu.Unlock()
		return okMap("results", res), true
	case "edge/channels/info":
		res, failed := []slack.Conversation{}, []string{}
		w.mu.Lock()
		for _, id := range ids() {
			if c, ok := w.convLocked(id); ok {
				res = append(res, w.convOutLocked(*c))
			} else {
				failed = append(failed, id)
			}
		}
		w.mu.Unlock()
		return okMap("results", res, "failed_ids", failed), true
	}
	return nil, false
}
