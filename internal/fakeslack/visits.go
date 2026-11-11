package fakeslack

// visits.list and visits.record: the server side of Slack's recent
// places (desktop_search_recents_read / _write), in the shapes Slack's
// client reads (visits.list {checksum, visits: [{entity_id, entity_type,
// date_last_visited_ms, thread_ts}]}; visits.record {visits, client_now_ms}
// answering {errors, checksum}). Only synthetic ids are stored.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"sync"
)

// Visit is one visits.list row.
type Visit struct {
	EntityID   string `json:"entity_id"`
	EntityType string `json:"entity_type"`
	LastMS     int64  `json:"date_last_visited_ms"`
	ThreadTS   string `json:"thread_ts,omitempty"`
}

type visitStore struct {
	mu     sync.Mutex
	visits []Visit
	forms  map[string][]url.Values
	gen    int
}

var visitStores sync.Map // *Server -> *visitStore

func (s *Server) vstore() *visitStore {
	v, _ := visitStores.LoadOrStore(s, &visitStore{forms: map[string][]url.Values{}})
	return v.(*visitStore)
}

// SetVisits replaces the stored visits.
func (s *Server) SetVisits(vs ...Visit) {
	st := s.vstore()
	st.mu.Lock()
	st.visits = append([]Visit(nil), vs...)
	st.gen++
	st.mu.Unlock()
}

// Visits returns the stored visits, newest first.
func (s *Server) Visits() []Visit {
	st := s.vstore()
	st.mu.Lock()
	defer st.mu.Unlock()
	out := append([]Visit(nil), st.visits...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastMS > out[j].LastMS })
	return out
}

// VisitForms returns the forms of every call to visits.list or
// visits.record, in order.
func (s *Server) VisitForms(method string) []url.Values {
	st := s.vstore()
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]url.Values(nil), st.forms[method]...)
}

func (st *visitStore) checksumLocked() string { return fmt.Sprintf("v%d-%d", st.gen, len(st.visits)) }

func (s *Server) handleVisits(method string, f url.Values) (any, bool) {
	if method != "visits.list" && method != "visits.record" {
		return nil, false
	}
	st := s.vstore()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.forms[method] = append(st.forms[method], f)
	switch method {
	case "visits.list":
		lim, _ := strconv.Atoi(f.Get("limit"))
		if lim <= 0 {
			lim = 40
		}
		vs := append([]Visit(nil), st.visits...)
		sort.SliceStable(vs, func(i, j int) bool { return vs[i].LastMS > vs[j].LastMS })
		if len(vs) > lim {
			vs = vs[:lim]
		}
		return ok("checksum", st.checksumLocked(), "visits", vs), true
	default: // visits.record
		var in []struct {
			EntityID   string `json:"entity_id"`
			EntityType string `json:"entity_type"`
			ThreadTS   string `json:"thread_ts"`
			At         int64  `json:"client_event_time_ms"`
		}
		if json.Unmarshal([]byte(f.Get("visits")), &in) != nil || len(in) == 0 {
			return map[string]any{"ok": false, "error": "invalid_arguments"}, true
		}
		for _, v := range in {
			kept := st.visits[:0:0]
			for _, old := range st.visits {
				if old.EntityID != v.EntityID || old.ThreadTS != v.ThreadTS {
					kept = append(kept, old)
				}
			}
			st.visits = append(kept, Visit{EntityID: v.EntityID, EntityType: v.EntityType, LastMS: v.At, ThreadTS: v.ThreadTS})
		}
		st.gen++
		return ok("errors", []any{}, "checksum", st.checksumLocked()), true
	}
}
