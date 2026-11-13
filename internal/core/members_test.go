package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// usersEdge is an edge users/info answering ids that start with "UK"
// (the rest aren't returned), recording each request's ids.
type usersEdge struct {
	mu    sync.Mutex
	asked [][]string
	at    []time.Time
}

func (e *usersEdge) serve(t *testing.T, a *App) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body struct {
			Updated map[string]int `json:"updated_ids"`
		}
		_ = json.Unmarshal(b, &body)
		var ids []string
		var res []slack.User
		for id := range body.Updated {
			ids = append(ids, id)
			if strings.HasPrefix(id, "UK") {
				res = append(res, slack.User{ID: id, Name: strings.ToLower(id), Profile: slack.Profile{DisplayName: "Name " + id}})
			}
		}
		sort.Strings(ids)
		e.mu.Lock()
		e.asked = append(e.asked, ids)
		e.at = append(e.at, time.Now())
		e.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "results": res})
	}))
	t.Cleanup(srv.Close)
	a.C.EdgeURL = srv.URL + "/edge/"
}

func (e *usersEdge) requests() [][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.asked)
}

func waitFor(t *testing.T, what string, d time.Duration, cond func() bool) {
	t.Helper()
	for dl := time.Now().Add(d); time.Now().Before(dl); time.Sleep(10 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// The UnknownFetchManager: lookups that miss in one burst go in one
// users/info request; what it returns is stored and "names" names them;
// ids it didn't return are non-existent and not asked again; one asked
// while a request is in flight waits for it, at least 200 ms after it
// began; a member the cache evicted is asked again at its next lookup.
func TestUnknownMembersFetched(t *testing.T) {
	a, _ := testApp(t)
	e := &usersEdge{}
	e.serve(t, a)
	evs, stop := a.Subscribe()
	defer stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.memberLoop(ctx)

	for i := range 3 {
		if _, ok := a.User(fmt.Sprintf("UK%07d", i)); ok {
			t.Fatal("an unknown member was found")
		}
	}
	a.S.UserName("UNOPE001") // the resolver's lookups too
	ev := next(t, evs, "names")
	got := e.requests()
	if len(got) != 1 || strings.Join(got[0], ",") != "UK0000000,UK0000001,UK0000002,UNOPE001" {
		t.Fatalf("requests %v", got)
	}
	if !slices.Contains(ev.Users, "UK0000001") {
		t.Errorf("names %v", ev.Users)
	}
	if u, ok := a.User("UK0000001"); !ok || u.Name != "Name UK0000001" {
		t.Fatalf("UK0000001 = %+v %v", u, ok)
	}
	if !a.S.Nonexistent("UNOPE001") {
		t.Fatal("an id users/info didn't return isn't non-existent")
	}
	a.User("UNOPE001")
	time.Sleep(300 * time.Millisecond)
	if n := len(e.requests()); n != 1 {
		t.Fatalf("a non-existent member was asked again (%d requests)", n)
	}

	// evicted: asked again at the next lookup
	a.S.SetMembersSizeForDebug(1, 1)
	gone := a.EvictMembers()
	a.S.SetMembersSizeForDebug(-1, -1)
	if !slices.Contains(gone, "UK0000002") {
		t.Fatalf("evicted %v", gone)
	}
	if _, ok := a.User("UK0000002"); ok {
		t.Fatal("an evicted member was found")
	}
	waitFor(t, "the evicted member back", 3*time.Second, func() bool { _, ok := a.S.RawUser("UK0000002"); return ok })
	reqs := e.requests()
	if last := reqs[len(reqs)-1]; strings.Join(last, ",") != "UK0000002" {
		t.Fatalf("asked %v", last)
	}

	// debounce: a second burst right after a request waits until 200 ms
	// after it began
	e.mu.Lock()
	t0 := e.at[len(e.at)-1]
	e.mu.Unlock()
	a.User("UK0000009")
	waitFor(t, "the debounced request", 3*time.Second, func() bool { return len(e.requests()) == len(reqs)+1 })
	e.mu.Lock()
	t1 := e.at[len(e.at)-1]
	e.mu.Unlock()
	// (the server sees a request a little after the loop began it)
	if d := t1.Sub(t0); d < memberDebounce-30*time.Millisecond {
		t.Errorf("requests %s apart, want about %s", d, memberDebounce)
	}
}

// The members pass emits members_evicted with what went; the viewer stays.
func TestMembersPassEvent(t *testing.T) {
	a, _ := testApp(t)
	var us []slack.User
	for i := range 800 {
		us = append(us, slack.User{ID: fmt.Sprintf("UM%07d", i)})
	}
	a.S.PutUsers(us)
	evs, stop := a.Subscribe()
	defer stop()
	gone := a.EvictMembers()
	if len(gone) != 804-500 {
		t.Fatalf("evicted %d, want %d", len(gone), 804-500)
	}
	ev := next(t, evs, "members_evicted")
	if len(ev.Users) != len(gone) {
		t.Errorf("event names %d", len(ev.Users))
	}
	if _, ok := a.S.RawUser("USELF"); !ok {
		t.Error("the viewer was evicted")
	}
}
