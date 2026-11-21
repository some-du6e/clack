package core

import (
	"context"
	"encoding/json"
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

// gatedUsersEdge is an edge users/info returning every id it's asked for,
// each request answered only once the test sends a token on gate (all of
// them once gate is closed). requests are the ids of each request, in the
// order they arrived.
func gatedUsersEdge(t *testing.T, a *App, gate <-chan struct{}) (requests func() [][]string) {
	var mu sync.Mutex
	var asked [][]string
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
			res = append(res, slack.User{ID: id, Name: strings.ToLower(id), Profile: slack.Profile{DisplayName: "Name " + id}})
		}
		sort.Strings(ids)
		mu.Lock()
		asked = append(asked, ids)
		mu.Unlock()
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "results": res})
	}))
	t.Cleanup(srv.Close)
	a.C.EdgeURL = srv.URL + "/edge/"
	return func() [][]string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(asked)
	}
}

// One users/info request at a time for a member, as Slack's one members
// UnknownFetchManager per team: a lookup that misses a member Boot's
// EnsureUsers has out waits for that answer and names the member from
// it, and EnsureUsers for a member the fetcher has out waits for the
// fetcher's answer. During boot both asked for the DM rows' people.
func TestUsersAskedOnceAtATime(t *testing.T) {
	a, _ := testApp(t)
	gate := make(chan struct{}, 4)
	requests := gatedUsersEdge(t, a, gate)
	t.Cleanup(func() { close(gate) }) // before the server's Close: nothing stays held
	evs, stop := a.Subscribe()
	defer stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.memberLoop(ctx)

	// EnsureUsers has UK1 out: the fetcher waits for it
	ensured := make(chan struct{})
	go func() {
		a.EnsureUsers(ctx, []string{"UK0000001"})
		close(ensured)
	}()
	waitFor(t, "EnsureUsers' request", 3*time.Second, func() bool { return len(requests()) == 1 })
	if _, ok := a.User("UK0000001"); ok {
		t.Fatal("found before its answer")
	}
	time.Sleep(memberDebounce + 100*time.Millisecond) // the fetcher's tick, and then some
	if got := requests(); len(got) != 1 {
		t.Fatalf("users/info asked again for a member EnsureUsers has out: %v", got)
	}
	gate <- struct{}{}
	ev := next(t, evs, "names")
	if !slices.Contains(ev.Users, "UK0000001") {
		t.Fatalf("the lookup's names %v don't name the member EnsureUsers got", ev.Users)
	}
	select {
	case <-ensured:
	case <-time.After(3 * time.Second):
		t.Fatal("EnsureUsers didn't return")
	}

	// the fetcher has UK2 out: EnsureUsers waits for its answer
	a.User("UK0000002")
	waitFor(t, "the fetcher's request", 3*time.Second, func() bool { return len(requests()) == 2 })
	ensured = make(chan struct{})
	go func() {
		a.EnsureUsers(ctx, []string{"UK0000002"})
		close(ensured)
	}()
	select {
	case <-ensured:
		t.Fatal("EnsureUsers returned before the answer it needs")
	case <-time.After(300 * time.Millisecond):
	}
	if got := requests(); len(got) != 2 || strings.Join(got[1], ",") != "UK0000002" {
		t.Fatalf("users/info asked again for a member the fetcher has out: %v", got)
	}
	gate <- struct{}{}
	select {
	case <-ensured:
	case <-time.After(3 * time.Second):
		t.Fatal("EnsureUsers didn't return once the fetcher's answer was in")
	}
	if _, ok := a.S.RawUser("UK0000002"); !ok {
		t.Fatal("EnsureUsers returned without the member")
	}
	if got := requests(); len(got) != 2 {
		t.Fatalf("requests %v", got)
	}
}
