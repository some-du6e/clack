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

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// botHosts is a Grid workspace's three ways to a bot: the edge cache
// (users/info, which knows BEDGE…), the enterprise host's bots.info (org
// apps, BORG…) and the team host's (the workspace's own bots, BWS…).
// BNET… drops the request (a 502); BGONE… nobody knows.
type botHosts struct {
	mu   sync.Mutex
	reqs []string // "edge B1,B2", "ent B1,B2 app_ids=…", "team …"
}

func (h *botHosts) serve(t *testing.T) *httptest.Server {
	bot := func(id string) map[string]any {
		return map[string]any{"id": id, "name": "bot " + id, "app_id": "A" + id[1:], "icons": map[string]string{"image_72": "https://img.example/" + id + ".png"}}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/edge/") {
			b, _ := io.ReadAll(r.Body)
			var body struct {
				Updated map[string]int `json:"updated_ids"`
			}
			_ = json.Unmarshal(b, &body)
			var ids []string
			res := []map[string]any{}
			for id, v := range body.Updated {
				if v != 0 {
					t.Errorf("edge users/info updated_ids[%s] = %d, want 0", id, v)
				}
				ids = append(ids, id)
				if strings.HasPrefix(id, "BEDGE") {
					res = append(res, bot(id))
				}
			}
			sort.Strings(ids)
			h.add("edge " + strings.Join(ids, ","))
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "results": res, "failed_ids": []string{}})
			return
		}
		_ = r.ParseForm()
		host := "team"
		if strings.HasPrefix(r.URL.Path, "/ent/") {
			host = "ent"
		}
		if !strings.HasSuffix(r.URL.Path, "/bots.info") {
			_, _ = w.Write([]byte(`{"ok":false,"error":"not_in_test"}`))
			return
		}
		ids := strings.Split(r.PostForm.Get("bots"), ",")
		req := host + " " + strings.Join(ids, ",")
		if r.PostForm.Has("app_ids") {
			req += " app_ids=" + r.PostForm.Get("app_ids")
		}
		h.add(req)
		var found []map[string]any
		for _, id := range ids {
			switch {
			case strings.HasPrefix(id, "BNET"):
				w.WriteHeader(http.StatusBadGateway)
				return
			case host == "ent" && strings.HasPrefix(id, "BORG"), host == "team" && strings.HasPrefix(id, "BWS"):
				found = append(found, bot(id))
			}
		}
		if len(found) == 0 {
			_, _ = w.Write([]byte(`{"ok":false,"error":"bots_not_found"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "bots": found})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (h *botHosts) add(r string) {
	h.mu.Lock()
	h.reqs = append(h.reqs, r)
	h.mu.Unlock()
}

func (h *botHosts) take() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.reqs
	h.reqs = nil
	return out
}

// Karim's log: bots.info answered bots_not_found for bots the enterprise
// host doesn't know, and a connection reset left a bot unnamed for the
// rest of the session. Slack's fetchBotsById asks the edge cache's
// users/info first and bots.info only for the rest, with the messages'
// app ids; Clack asks the team host once more for what the enterprise
// host doesn't know, and forgets ids whose request failed on the way so
// the next draw asks again.
func TestResolveBotsLikeFetchBotsByID(t *testing.T) {
	h := &botHosts{}
	srv := h.serve(t)
	c := slack.New(slack.Creds{Token: "xoxc-team", EnterpriseToken: "xoxc-org", UserID: "USELF", Team: "T1", EnterpriseID: "E1",
		TeamURL: srv.URL + "/team/", EnterpriseURL: srv.URL + "/ent/"})
	c.EdgeURL = srv.URL + "/edge/"
	a := New(c, store.New(""))
	t.Cleanup(a.Shutdown)
	ctx := context.Background()

	ids := []string{"BEDGE01", "BORG01", "BWS01", "BGONE01"}
	for _, id := range ids {
		app := ""
		if id == "BWS01" {
			app = "AWS01" // its message said which app it came from
		}
		a.S.NoteMissingBotApp(id, app)
	}
	_, bots := a.S.TakeMissingNames()
	got := a.resolve(ctx, nil, nil, bots)
	sort.Strings(got)
	if want := []string{"BEDGE01", "BORG01", "BWS01"}; !slices.Equal(got, want) {
		t.Errorf("resolved %v, want %v", got, want)
	}
	want := []string{
		"edge BEDGE01,BGONE01,BORG01,BWS01",        // the edge cache first, every id
		"ent BORG01,BWS01,BGONE01 app_ids=,AWS01,", // bots.info for the rest, app_ids lined up
		"team BWS01,BGONE01 app_ids=AWS01,",        // what the org host left out, once on the team host
	}
	if reqs := h.take(); !slices.Equal(reqs, want) {
		t.Errorf("requests\n%s\nwant\n%s", strings.Join(reqs, "\n"), strings.Join(want, "\n"))
	}
	for _, id := range got {
		if b, ok := a.S.Bot(id); !ok || b.Name != "bot "+id || b.Icon == "" {
			t.Errorf("%s stored as %+v %v", id, b, ok)
		}
	}

	// one bot the org host answers bots_not_found for (Karim's
	// "bots.info: slack bots.info: bots_not_found")
	a.S.NoteMissingBot("BWS02")
	_, bots = a.S.TakeMissingNames()
	if got := a.resolve(ctx, nil, nil, bots); !slices.Equal(got, []string{"BWS02"}) {
		t.Errorf("a workspace bot the org host doesn't know: resolved %v", got)
	}
	if reqs := h.take(); !slices.Equal(reqs, []string{"edge BWS02", "ent BWS02", "team BWS02"}) {
		t.Errorf("requests %v", reqs)
	}

	// a request that failed on the way (Karim's connection reset): asked
	// again at the next draw; one nobody knows is not
	a.S.NoteMissingBot("BNET01")
	_, bots = a.S.TakeMissingNames()
	if got := a.resolve(ctx, nil, nil, bots); len(got) != 0 {
		t.Errorf("resolved %v through a failed request", got)
	}
	if reqs := h.take(); !slices.Equal(reqs, []string{"edge BNET01", "ent BNET01"}) {
		t.Errorf("requests %v (no team host retry after a failed request)", reqs)
	}
	for _, id := range []string{"BNET01", "BGONE01", "BEDGE01"} {
		a.S.NoteMissingBot(id) // the next draw
	}
	if _, bots := a.S.TakeMissingNames(); !slices.Equal(bots, []string{"BNET01"}) {
		t.Errorf("asked again: %v, want only the bot whose request failed", bots)
	}
}
