package slick

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/jsrt"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
)

// The harness: a core.App over a synthetic store (made-up people,
// channels and messages; no real Slack), a fake Slack Web API that records
// calls, Taut with UI hooks that capture what plugins ask the UI for, and
// Slick on top. Nothing reaches the network: fetches outside the fake API
// answer from h.fetches or 404.

const (
	selfID  = "USELF00001"
	aliceID = "UALICE0001"
	bobID   = "UBOB000001"
	teamID  = "T0266FRGM"
	entID   = "E09V59WQY1E"
	chGen   = "C0GENERAL1"
	chAnn   = "C0ANNOUNC1"
	chPriv  = "G0PRIVATE1"
	dmAlice = "D0ALICE001"
	sbID    = "USLACKBOT"
)

type fakeAPI struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls []apiCall
	reply map[string]any
}

type apiCall struct {
	Method string
	Form   url.Values
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{reply: map[string]any{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(8 << 20)
		method := strings.TrimPrefix(r.URL.Path, "/api/")
		f.mu.Lock()
		f.calls = append(f.calls, apiCall{method, r.Form})
		rep, ok := f.reply[method]
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			rep = map[string]any{"ok": true}
		}
		_ = json.NewEncoder(w).Encode(rep)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) called(method string) []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []url.Values
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c.Form)
		}
	}
	return out
}

func (f *fakeAPI) set(method string, reply any) {
	f.mu.Lock()
	f.reply[method] = reply
	f.mu.Unlock()
}

type testUI struct {
	mu       sync.Mutex
	trees    map[uint64][]byte
	patched  []string
	styles   map[string]string
	overlays []taut.Overlay
	tabs     []taut.PreferencesTab
	toasts   []string
}

func (u *testUI) hooks() taut.UIHooks {
	return taut.UIHooks{
		Deliver: func(root uint64, tree []byte) {
			u.mu.Lock()
			u.trees[root] = append([]byte(nil), tree...)
			u.mu.Unlock()
		},
		SetPatched:      func(names []string) { u.mu.Lock(); u.patched = names; u.mu.Unlock() },
		SetStyle:        func(k, css string) { u.mu.Lock(); u.styles[k] = css; u.mu.Unlock() },
		RemoveStyle:     func(k string) { u.mu.Lock(); delete(u.styles, k); u.mu.Unlock() },
		Overlays:        func(o []taut.Overlay) { u.mu.Lock(); u.overlays = o; u.mu.Unlock() },
		PreferencesTabs: func(tabs []taut.PreferencesTab) { u.mu.Lock(); u.tabs = tabs; u.mu.Unlock() },
		Toast: func(text, icon string, d time.Duration) {
			u.mu.Lock()
			u.toasts = append(u.toasts, text)
			u.mu.Unlock()
		},
		Input: func(op string, w taut.InputWhere, arg json.RawMessage) any {
			if op == "getContents" {
				return []map[string]string{{"insert": "\n"}}
			}
			return nil
		},
	}
}

func (u *testUI) style(substr string) string {
	u.mu.Lock()
	defer u.mu.Unlock()
	for k, v := range u.styles {
		if strings.Contains(k, substr) {
			return v
		}
	}
	return ""
}

type harness struct {
	t   *testing.T
	tt  *taut.Taut
	s   *Slick
	app *core.App
	api *fakeAPI
	ui  *testUI
	dir string

	fetchMu sync.Mutex
	fetches map[string]string // URL prefix → JSON body
	fetched []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	api := newFakeAPI(t)
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test%2Fcookie", UserID: selfID, Team: teamID, EnterpriseID: entID,
		TeamURL: api.srv.URL + "/", EnterpriseURL: api.srv.URL + "/"})
	st := store.New("")
	seed(st)
	app := core.New(c, st)
	if u, ok := st.User(selfID); ok {
		app.S.Self = *u
	}
	app.ServeOffline()
	h := &harness{t: t, app: app, api: api, dir: t.TempDir(), fetches: map[string]string{},
		ui: &testUI{trees: map[uint64][]byte{}, styles: map[string]string{}}}
	tt, err := taut.New(taut.Options{
		App: app, Dir: filepath.Join(h.dir, "taut"), CredsPath: filepath.Join(h.dir, "creds.json"), UI: h.ui.hooks(), NoAutoStart: true,
		CacheDir: filepath.Join(os.TempDir(), "clack-slick-test-cache"), Fetch: h.fetch,
		Log: func(level, msg string) {
			if level == "error" || level == "warn" {
				t.Logf("js %s: %s", level, msg)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.tt = tt
	s, err := New(Options{Taut: tt, Dir: filepath.Join(h.dir, "slick"), NoAutoStart: true, NoUpdates: true})
	if err != nil {
		t.Fatal(err)
	}
	h.s = s
	t.Cleanup(func() {
		_ = s.Close()
		_ = tt.Close()
	})
	return h
}

func (h *harness) fetch(ctx context.Context, req *http.Request) (*http.Response, error) {
	u := req.URL.String()
	if strings.HasPrefix(u, h.api.srv.URL) {
		return http.DefaultClient.Do(req.WithContext(ctx))
	}
	h.fetchMu.Lock()
	defer h.fetchMu.Unlock()
	h.fetched = append(h.fetched, u)
	best := ""
	for prefix := range h.fetches {
		if strings.HasPrefix(u, prefix) && len(prefix) > len(best) {
			best = prefix
		}
	}
	if best != "" {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(h.fetches[best])), Request: req}, nil
	}
	return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(nil)), Request: req}, nil
}

func (h *harness) answer(prefix, body string) {
	h.fetchMu.Lock()
	h.fetches[prefix] = body
	h.fetchMu.Unlock()
}

func (h *harness) wasFetched(prefix string) bool {
	h.fetchMu.Lock()
	defer h.fetchMu.Unlock()
	for _, u := range h.fetched {
		if strings.HasPrefix(u, prefix) {
			return true
		}
	}
	return false
}

// seed fills the store with a small synthetic workspace.
func seed(s *store.Store) {
	s.Lock()
	s.TeamID, s.TeamName, s.TeamDomain = teamID, "Synthetic Club", "hackclub"
	s.Unlock()
	s.PutUsers([]slack.User{
		{ID: selfID, Name: "me", RealName: "Test Self", TZ: "America/New_York", Profile: slack.Profile{DisplayName: "self", RealName: "Test Self", Pronouns: "they/them"}},
		{ID: aliceID, Name: "alice", RealName: "Alice Example", TZ: "Europe/London", Profile: slack.Profile{DisplayName: "alice", RealName: "Alice Example", Pronouns: "she/her"}},
		{ID: bobID, Name: "bob", RealName: "Bob Example", Profile: slack.Profile{DisplayName: "bob", RealName: "Bob Example"}},
		{ID: sbID, Name: "slackbot", RealName: "Slackbot", IsBot: true, Profile: slack.Profile{DisplayName: "Slackbot"}},
	})
	posting, _ := json.Marshal(map[string]any{"posting_restricted_to": map[string]any{"type": []string{"admin"}, "user": []string{aliceID}}})
	s.PutConv(slack.Conversation{ID: chGen, Name: "general", IsChannel: true, IsMember: true, IsGeneral: true, NumMembers: 1234})
	s.PutConv(slack.Conversation{ID: chAnn, Name: "announcements", IsChannel: true, IsMember: true, NumMembers: 99, Properties: posting})
	s.PutConv(slack.Conversation{ID: chPriv, Name: "secret-club", IsChannel: false, IsGroup: true, IsPrivate: true, IsMember: true, NumMembers: 3})
	s.PutConv(slack.Conversation{ID: dmAlice, IsIM: true, User: aliceID, IsMember: true})
	s.PutConv(slack.Conversation{ID: "D0SLACKBO1", IsIM: true, User: sbID, IsMember: true})
	s.SetLatest(chGen, []slack.Message{
		{Type: "message", TS: "1700000001.000100", User: aliceID, Text: "the job fair is today, damn"},
		{Type: "message", TS: "1700000002.000200", User: bobID, Text: "see https://example.com/?utm_source=x", Reactions: []slack.Reaction{{Name: "tada", Count: 2, Users: []string{aliceID, selfID}}}},
		{Type: "message", TS: "1700000003.000300", User: selfID, Text: "hello there"},
		{Type: "message", TS: "1700000004.000400", BotID: "B0BOT00001", Username: "Relay", Text: "relayed hi", Subtype: "bot_message"},
	}, false)
	s.SetLatest(dmAlice, []slack.Message{{Type: "message", TS: "1700000010.000100", User: aliceID, Text: "hi"}}, false)
	s.PutBot("B0BOT00001", store.BotInfo{Name: "Helper Bot"})
}

func (h *harness) ctx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	h.t.Cleanup(cancel)
	return ctx
}

func (h *harness) rt() *jsrt.Runtime {
	rt, err := h.s.js()
	if err != nil {
		h.t.Fatal(err)
	}
	return rt
}

var jsN int
var jsMu sync.Mutex

// js evaluates an async function body with S = __slick, T = __taut, TA =
// the base TautAPI, and returns its JSON result.
func (h *harness) js(body string) json.RawMessage {
	h.t.Helper()
	rt := h.rt()
	jsMu.Lock()
	jsN++
	name := fmt.Sprintf("__slickTest%d", jsN)
	jsMu.Unlock()
	src := fmt.Sprintf("globalThis.%s = async () => { const S = globalThis.__slick; const T = globalThis.__taut; const TA = globalThis.TautAPI; %s }", name, body)
	if _, err := rt.Eval(name+".js", src); err != nil {
		h.t.Fatalf("js: %v\n%s", err, body)
	}
	out, err := rt.Call(h.ctx(), name)
	if err != nil {
		h.t.Fatalf("js: %v\n%s", err, body)
	}
	return out
}

func (h *harness) jsInto(body string, v any) {
	h.t.Helper()
	if err := json.Unmarshal(h.js(body), v); err != nil {
		h.t.Fatalf("js result: %v", err)
	}
}

func eventually(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// render mounts a Slack component through Taut's bridge and returns its tree.
func (h *harness) render(root uint64, name string, props any) []any {
	h.t.Helper()
	b, _ := json.Marshal(props)
	h.ui.mu.Lock()
	delete(h.ui.trees, root)
	h.ui.mu.Unlock()
	h.tt.Bridge().Render(root, name, b)
	var tree []byte
	eventually(h.t, 5*time.Second, "a tree for "+name, func() bool {
		h.ui.mu.Lock()
		defer h.ui.mu.Unlock()
		tree = h.ui.trees[root]
		return tree != nil
	})
	var nodes []any
	if err := json.Unmarshal(tree, &nodes); err != nil {
		h.t.Fatalf("tree: %v: %s", err, tree)
	}
	return nodes
}

func findNative(nodes []any, n string) map[string]any {
	for _, x := range nodes {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		if m["t"] == "@" && m["n"] == n {
			return m
		}
		if kids, ok := m["c"].([]any); ok {
			if f := findNative(kids, n); f != nil {
				return f
			}
		}
	}
	return nil
}

func treeText(nodes []any) string {
	var b strings.Builder
	var walk func([]any)
	walk = func(ns []any) {
		for _, x := range ns {
			if m, ok := x.(map[string]any); ok {
				if m["t"] == "#" {
					fmt.Fprint(&b, m["v"])
				}
				if kids, ok := m["c"].([]any); ok {
					walk(kids)
				}
				if p, ok := m["p"].(map[string]any); ok {
					for _, v := range p {
						if el, ok := v.(map[string]any); ok {
							if sub, ok := el["$el"].([]any); ok {
								walk(sub)
							}
						}
					}
				}
			}
		}
	}
	walk(nodes)
	return b.String()
}

// state is what Taut's and Slick's runtimes have registered.
type state struct {
	Running        []string `json:"running"`
	Mains          []string `json:"mains"`
	Components     []string `json:"components"`
	Thunks         []string `json:"thunks"`
	RTM            []string `json:"rtm"`
	SendChecks     int      `json:"sendChecks"`
	SendTransforms int      `json:"sendTransforms"`
	Styles         []string `json:"styles"`
	Slices         []string `json:"slices"`
	StatePatches   int      `json:"statePatches"`
	Tabs           []string `json:"tabs"`
	Overlays       []string `json:"overlays"`
	Resize         int      `json:"resize"`
	Delta          int      `json:"deltaTransforms"`
	Documents      int      `json:"documentListeners"`
	MainListeners  int      `json:"mainListeners"`
	Protocols      []string `json:"protocols"`
	NetRules       int      `json:"netRules"`
	NotifyFilters  int      `json:"notificationFilters"`
	Switches       []string `json:"switches"`
}

func (h *harness) state() state {
	var st state
	h.jsInto(`return { ...T.introspect ? globalThis.__taut.introspect() : {}, ...S.introspect() }`, &st)
	return st
}

// registrations lists what a plugin added, as comparable strings.
func (st state) registrations() []string {
	var out []string
	add := func(kind string, xs []string) {
		for _, x := range xs {
			out = append(out, kind+":"+x)
		}
	}
	add("component", st.Components)
	add("thunk", st.Thunks)
	add("rtm", st.RTM)
	add("style", st.Styles)
	add("slice", st.Slices)
	add("tab", st.Tabs)
	add("overlay", st.Overlays)
	add("protocol", st.Protocols)
	n := func(kind string, k int) {
		for i := 0; i < k; i++ {
			out = append(out, fmt.Sprintf("%s#%d", kind, i))
		}
	}
	n("sendCheck", st.SendChecks)
	n("sendTransform", st.SendTransforms)
	n("statePatch", st.StatePatches)
	n("resize", st.Resize)
	n("delta", st.Delta)
	n("document", st.Documents)
	n("mainListener", st.MainListeners)
	n("netRule", st.NetRules)
	n("notifyFilter", st.NotifyFilters)
	return out
}

func has(list []string, x string) bool {
	for _, y := range list {
		if y == x {
			return true
		}
	}
	return false
}

func hasPrefix(list []string, p string) bool {
	for _, y := range list {
		if strings.HasPrefix(y, p) {
			return true
		}
	}
	return false
}

// importFile installs a built plugin file the way a user does.
func (h *harness) importFile(path string) string {
	h.t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		h.t.Fatal(err)
	}
	name, err := h.s.Import(h.ctx(), b)
	if err != nil {
		h.t.Fatalf("import %s: %v", filepath.Base(path), err)
	}
	return name
}

func (h *harness) plugin(id string) PluginInfo {
	for _, p := range h.s.Settings().Plugins {
		if p.ID == id {
			return p
		}
	}
	h.t.Fatalf("%s isn't installed", id)
	return PluginInfo{}
}
