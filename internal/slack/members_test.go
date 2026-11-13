package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// membersEdge is an edge users/info that answers ids starting with "U"
// (ids in pending are pending the first time they're asked; ids in failing
// make their chunk's request fail) and records what was asked.
type membersEdge struct {
	srv      *httptest.Server
	mu       sync.Mutex
	asked    [][]string
	bodies   []map[string]json.RawMessage
	pending  map[string]int // id -> how many more answers list it pending
	failing  map[string]bool
	inFlight atomic.Int32
	peak     atomic.Int32
}

func newMembersEdge(t *testing.T) (*membersEdge, *Client) {
	e := &membersEdge{pending: map[string]int{}, failing: map[string]bool{}}
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := e.inFlight.Add(1)
		defer e.inFlight.Add(-1)
		for {
			p := e.peak.Load()
			if n <= p || e.peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond) // overlap concurrent chunks
		b, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		_ = json.Unmarshal(b, &body)
		var up map[string]int
		_ = json.Unmarshal(body["updated_ids"], &up)
		ids := make([]string, 0, len(up))
		for id := range up {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		e.mu.Lock()
		e.asked = append(e.asked, ids)
		e.bodies = append(e.bodies, body)
		var res []User
		var pending, failed []string
		fail := false
		for _, id := range ids {
			switch {
			case e.failing[id]:
				fail = true
			case e.pending[id] > 0:
				e.pending[id]--
				pending = append(pending, id)
			case strings.HasPrefix(id, "U"):
				res = append(res, User{ID: id, Name: strings.ToLower(id)})
			default:
				failed = append(failed, id)
			}
		}
		e.mu.Unlock()
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "results": res, "pending_ids": pending, "failed_ids": failed})
	}))
	t.Cleanup(e.srv.Close)
	c := New(Creds{Token: "xoxc-test", UserID: "USELF", Team: "T1"})
	c.EdgeURL = e.srv.URL + "/edge/"
	return e, c
}

func memberIDs(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%07d", prefix, i)
	}
	return out
}

// Slack's fetchMembersById: chunks of 100 ids, two requests at a time,
// each with check_interaction, include_profile_only_users and the ids
// as updated_ids at 0.
func TestMembersInfoChunks(t *testing.T) {
	e, c := newMembersEdge(t)
	ids := memberIDs("U", 250)
	r := c.MembersInfo(context.Background(), ids)
	if r.Err != nil || len(r.Users) != 250 || len(r.Missing) != 0 || len(r.Failed) != 0 {
		t.Fatalf("users %d missing %d failed %d err %v", len(r.Users), len(r.Missing), len(r.Failed), r.Err)
	}
	if len(e.asked) != 3 {
		t.Fatalf("%d requests, want 3", len(e.asked))
	}
	for _, a := range e.asked {
		if len(a) > MembersPerQuery {
			t.Errorf("a request asked for %d ids", len(a))
		}
	}
	if p := e.peak.Load(); p > MembersInFlight {
		t.Errorf("%d requests in flight at once, want at most %d", p, MembersInFlight)
	}
	b := e.bodies[0]
	if string(b["check_interaction"]) != "true" || string(b["include_profile_only_users"]) != "true" {
		t.Errorf("body %v", b)
	}
	var up map[string]int
	_ = json.Unmarshal(b["updated_ids"], &up)
	for id, v := range up {
		if v != 0 {
			t.Errorf("updated_ids[%s] = %d", id, v)
		}
	}
}

// pending_ids are asked again (up to three more rounds); ids users/info
// leaves out are missing; a chunk whose request fails is failed.
func TestMembersInfoPendingMissingFailed(t *testing.T) {
	e, c := newMembersEdge(t)
	e.pending["U0000001"] = 2 // answered on the third ask
	e.pending["U0000002"] = 9 // never: missing after four asks
	e.failing["U0000150"] = true
	ids := append(memberIDs("U", 3), "B0000001")
	ids = append(ids, memberIDs("X", 96)...) // fills the first chunk
	ids = append(ids, "U0000150")            // the second, alone
	t0 := time.Now()
	r := c.MembersInfo(context.Background(), ids)
	got := map[string]bool{}
	for _, u := range r.Users {
		got[u.ID] = true
	}
	if !got["U0000000"] || !got["U0000001"] || got["U0000002"] {
		t.Errorf("users %v", got)
	}
	missing := strings.Join(r.Missing, ",")
	if !strings.Contains(missing, "U0000002") || !strings.Contains(missing, "B0000001") || strings.Contains(missing, "U0000001") {
		t.Errorf("missing %v", r.Missing)
	}
	if len(r.Failed) != 1 || r.Failed[0] != "U0000150" || r.Err == nil {
		t.Errorf("failed %v err %v", r.Failed, r.Err)
	}
	// rounds 500, 1000, 1500 ms apart
	if d := time.Since(t0); d < 2900*time.Millisecond {
		t.Errorf("pending ids asked again after %s, want the three waits (3 s)", d)
	}
	asks := 0
	for _, a := range e.asked {
		for _, id := range a {
			if id == "U0000002" {
				asks++
			}
		}
	}
	if asks != 4 {
		t.Errorf("a pending id was asked %d times, want 4", asks)
	}
}
