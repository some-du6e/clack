package fakeslack

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// The Huddles page's API (Slack's huddles-page chunk, Igs2):
// huddles.history with transformFiltersForApi's filters and a cursor,
// huddles.get for a live huddle's card, huddles.listBackgrounds for the
// cards' pictures and users.interactions.list / .set for the hero
// banner's dismissal. The fixtures are synthetic; tests set them.

// HuddleFixture is one huddle object as Slack's API returns it
// (huddles.history items, huddles.get's huddle).
type HuddleFixture map[string]any

type huddlePageState struct {
	history      []HuddleFixture
	info         map[string]HuddleFixture // huddles.get answers by id
	backgrounds  []map[string]any
	interactions map[string]int // component-type-step → count
	historyCalls []url.Values
	getCalls     []string
	bgCalls      int
	interCalls   []url.Values
	historyErr   string // huddles.history answers {ok:false, error}
}

// SetHuddleHistory sets what huddles.history pages through (newest first).
func (s *Server) SetHuddleHistory(hs []HuddleFixture) {
	s.hud.mu.Lock()
	s.hud.page.history = append([]HuddleFixture(nil), hs...)
	s.hud.mu.Unlock()
}

// SetHuddleHistoryError makes huddles.history fail with code ("" clears).
func (s *Server) SetHuddleHistoryError(code string) {
	s.hud.mu.Lock()
	s.hud.page.historyErr = code
	s.hud.mu.Unlock()
}

// SetHuddleInfo sets huddles.get's answer for a huddle id.
func (s *Server) SetHuddleInfo(id string, h HuddleFixture) {
	s.hud.mu.Lock()
	if s.hud.page.info == nil {
		s.hud.page.info = map[string]HuddleFixture{}
	}
	s.hud.page.info[id] = h
	s.hud.mu.Unlock()
}

// SetHuddleBackgrounds sets huddles.listBackgrounds' pictures.
func (s *Server) SetHuddleBackgrounds(bs []map[string]any) {
	s.hud.mu.Lock()
	s.hud.page.backgrounds = append([]map[string]any(nil), bs...)
	s.hud.mu.Unlock()
}

// HuddleHistoryCalls returns the huddles.history forms so far.
func (s *Server) HuddleHistoryCalls() []url.Values {
	s.hud.mu.Lock()
	defer s.hud.mu.Unlock()
	return append([]url.Values(nil), s.hud.page.historyCalls...)
}

// HuddleGetCalls returns the huddle ids huddles.get was asked for.
func (s *Server) HuddleGetCalls() []string {
	s.hud.mu.Lock()
	defer s.hud.mu.Unlock()
	return append([]string(nil), s.hud.page.getCalls...)
}

// HuddleBackgroundCalls counts huddles.listBackgrounds calls.
func (s *Server) HuddleBackgroundCalls() int {
	s.hud.mu.Lock()
	defer s.hud.mu.Unlock()
	return s.hud.page.bgCalls
}

// InteractionCalls returns the users.interactions.* forms so far (the
// method in "method").
func (s *Server) InteractionCalls() []url.Values {
	s.hud.mu.Lock()
	defer s.hud.mu.Unlock()
	return append([]url.Values(nil), s.hud.page.interCalls...)
}

// SetInteraction records an interaction as if users.interactions.set had
// run count times (a banner dismissed on another device).
func (s *Server) SetInteraction(component, typ string, count int) {
	s.hud.mu.Lock()
	if s.hud.page.interactions == nil {
		s.hud.page.interactions = map[string]int{}
	}
	s.hud.page.interactions[component+"-"+typ+"-DEFAULT"] = count
	s.hud.mu.Unlock()
}

func fixStrings(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func fixInt(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	}
	return 0
}

// historyMatch applies huddles.history's filters to one huddle the way
// the page's filters read: users (any of them was there; with
// users_only, nobody else but you), channels, the date range, missed
// huddles only (you're in missed_participants) and the Has filters.
func historyMatch(h HuddleFixture, f url.Values) bool {
	hist := fixStrings(h["participant_history"])
	if us := splitList(f.Get("users")); len(us) > 0 {
		hit := false
		for _, u := range us {
			if slices.Contains(hist, u) {
				hit = true
			}
		}
		if !hit {
			return false
		}
		if f.Get("users_only") == "true" {
			for _, p := range hist {
				if p != SelfID && !slices.Contains(us, p) {
					return false
				}
			}
		}
	}
	if cs := splitList(f.Get("channels")); len(cs) > 0 {
		chans := fixStrings(h["channels"])
		if len(chans) == 0 || !slices.Contains(cs, chans[0]) {
			return false
		}
	}
	start := fixInt(h["date_start"])
	if v, err := strconv.ParseInt(f.Get("start_date_ts"), 10, 64); err == nil && start < v {
		return false
	}
	if v, err := strconv.ParseInt(f.Get("end_date_ts"), 10, 64); err == nil && start > v {
		return false
	}
	if f.Get("missed_huddles_only") == "true" {
		m, _ := h["missed_participants"].(map[string]any)
		if _, ok := m[SelfID]; !ok {
			return false
		}
	}
	if f.Get("has_huddle_notes") == "true" && h["transcript_file_id"] == nil {
		return false
	}
	if f.Get("has_thread_replies") == "true" && h["thread_root_ts"] == nil {
		return false
	}
	if f.Get("has_artifacts") == "true" {
		a, _ := h["artifacts"].(map[string]any)
		if len(fixStrings(a["file_ids"])) == 0 && a["links"] == nil {
			return false
		}
	}
	return true
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func (s *Server) handleHuddlesPage(method string, f url.Values) (any, bool) {
	h := &s.hud
	switch method {
	case "huddles.history":
		h.mu.Lock()
		defer h.mu.Unlock()
		h.page.historyCalls = append(h.page.historyCalls, f)
		if h.page.historyErr != "" {
			return map[string]any{"ok": false, "error": h.page.historyErr}, true
		}
		var match []HuddleFixture
		for _, x := range h.page.history {
			if historyMatch(x, f) {
				match = append(match, x)
			}
		}
		limit, _ := strconv.Atoi(f.Get("limit"))
		if limit <= 0 {
			limit = 30
		}
		off, _ := strconv.Atoi(strings.TrimPrefix(f.Get("cursor"), "off:"))
		off = min(max(off, 0), len(match))
		end := min(off+limit, len(match))
		next := ""
		if end < len(match) {
			next = "off:" + strconv.Itoa(end)
		}
		page := append([]HuddleFixture{}, match[off:end]...)
		return ok("huddles", page, "response_metadata", map[string]any{"next_cursor": next}), true
	case "huddles.get":
		id := f.Get("huddle_id")
		h.mu.Lock()
		defer h.mu.Unlock()
		h.page.getCalls = append(h.page.getCalls, id)
		if x, found := h.page.info[id]; found {
			return ok("huddle", x), true
		}
		for _, x := range h.page.history {
			if x["id"] == id {
				return ok("huddle", x), true
			}
		}
		return map[string]any{"ok": false, "error": "huddle_not_found"}, true
	case "huddles.listBackgrounds":
		h.mu.Lock()
		defer h.mu.Unlock()
		h.page.bgCalls++
		bs := h.page.backgrounds
		if bs == nil {
			bs = []map[string]any{}
		}
		return ok("backgrounds", bs), true
	case "users.interactions.list":
		h.mu.Lock()
		defer h.mu.Unlock()
		rec := url.Values{"method": {method}}
		for k, v := range f {
			rec[k] = v
		}
		h.page.interCalls = append(h.page.interCalls, rec)
		comps, types := splitList(f.Get("components")), splitList(f.Get("types"))
		out := []map[string]any{}
		for i := range comps {
			if i >= len(types) {
				break
			}
			if n := h.page.interactions[comps[i]+"-"+types[i]+"-DEFAULT"]; n > 0 {
				out = append(out, map[string]any{"component": comps[i], "type": types[i], "step": "DEFAULT", "count": n})
			}
		}
		return ok("interactions", out), true
	case "users.interactions.set":
		h.mu.Lock()
		defer h.mu.Unlock()
		rec := url.Values{"method": {method}}
		for k, v := range f {
			rec[k] = v
		}
		h.page.interCalls = append(h.page.interCalls, rec)
		if f.Get("component") == "" || f.Get("type") == "" {
			return map[string]any{"ok": false, "error": "invalid_arguments"}, true
		}
		if h.page.interactions == nil {
			h.page.interactions = map[string]int{}
		}
		h.page.interactions[f.Get("component")+"-"+f.Get("type")+"-DEFAULT"]++
		return map[string]any{"ok": true}, true
	}
	return nil, false
}
