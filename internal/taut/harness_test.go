package taut

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// The harness: a core.App over a synthetic store (no real Slack, no real
// message contents: everything here is made up), a fake Slack API that
// records calls, and UI hooks that capture what plugins ask the UI for.

const (
	selfID   = "USELF"
	teamID   = "T0266FRGM"   // Hack Club, so hackClubOnly plugins run
	entID    = "E09V59WQY1E" // Hack Club's grid
	chGen    = "C0GENERAL"
	chAnn    = "C0ANNOUNCE"
	chOther  = "C0NOTMEMBER"
	dmAlice  = "D0ALICE"
	slackbot = "USLACKBOT"
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
	mu        sync.Mutex
	trees     map[uint64][]byte
	treeCh    chan uint64
	patched   []string
	styles    map[string]string
	overlays  []Overlay
	tabs      []PreferencesTab
	toasts    []string
	callGo    func(id int64, args []byte) ([]byte, error)
	inputs    map[string]string
	drafts    []json.RawMessage
	switched  []slack.Creds
	panes     []string
	attached  *Bridge
	prefsOpen []string
}

func (u *testUI) hooks() UIHooks {
	return UIHooks{
		Attached: func(b *Bridge) { u.mu.Lock(); u.attached = b; u.mu.Unlock() },
		Deliver: func(root uint64, tree []byte) {
			u.mu.Lock()
			u.trees[root] = append([]byte(nil), tree...)
			u.mu.Unlock()
			select {
			case u.treeCh <- root:
			default:
			}
		},
		SetPatched: func(names []string) { u.mu.Lock(); u.patched = names; u.mu.Unlock() },
		CallGo: func(id int64, args []byte) ([]byte, error) {
			if u.callGo != nil {
				return u.callGo(id, args)
			}
			return nil, nil
		},
		SetStyle:        func(k, css string) { u.mu.Lock(); u.styles[k] = css; u.mu.Unlock() },
		RemoveStyle:     func(k string) { u.mu.Lock(); delete(u.styles, k); u.mu.Unlock() },
		Overlays:        func(o []Overlay) { u.mu.Lock(); u.overlays = o; u.mu.Unlock() },
		PreferencesTabs: func(tabs []PreferencesTab) { u.mu.Lock(); u.tabs = tabs; u.mu.Unlock() },
		Toast: func(text, icon string, d time.Duration) {
			u.mu.Lock()
			u.toasts = append(u.toasts, text)
			u.mu.Unlock()
		},
		OpenPreferences: func(tab string) { u.mu.Lock(); u.prefsOpen = append(u.prefsOpen, tab); u.mu.Unlock() },
		ThreadPane: func(op, ch, ts, arg string) any {
			u.mu.Lock()
			u.panes = append(u.panes, op+":"+ch+"/"+ts+":"+arg)
			u.mu.Unlock()
			if op == "jumpToReply" {
				return true
			}
			if op == "loaded" {
				return []string{ts}
			}
			return nil
		},
		Input: func(op string, w InputWhere, arg json.RawMessage) any {
			u.mu.Lock()
			defer u.mu.Unlock()
			k := w.ViewContext + "|" + w.ChannelID
			switch op {
			case "isEmpty":
				return u.inputs[k] == ""
			case "setContents":
				u.inputs[k] = string(arg)
			case "getContents":
				return []map[string]string{{"insert": u.inputs[k] + "\n"}}
			}
			return nil
		},
		SetLocalDraft: func(d json.RawMessage) { u.mu.Lock(); u.drafts = append(u.drafts, d); u.mu.Unlock() },
		SwitchAccount: func(c slack.Creds) { u.mu.Lock(); u.switched = append(u.switched, c); u.mu.Unlock() },
	}
}

type harness struct {
	t   *testing.T
	tt  *Taut
	app *core.App
	api *fakeAPI
	ui  *testUI
	dir string
	// fetches answers fetch() by URL prefix; anything else is a 404, so no
	// plugin reaches the internet from tests.
	fetchMu sync.Mutex
	fetches map[string]string
	nativeT []string // native thunk calls the test recorded
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	api := newFakeAPI(t)
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test%2Fcookie", UserID: selfID, Team: teamID, EnterpriseID: entID,
		TeamURL: api.srv.URL + "/", EnterpriseURL: api.srv.URL + "/"})
	s := store.New("")
	seed(s)
	app := core.New(c, s)
	app.S.Self = *mustUser(s, selfID)
	app.ServeOffline()
	h := &harness{t: t, app: app, api: api, dir: t.TempDir(), fetches: map[string]string{},
		ui: &testUI{trees: map[uint64][]byte{}, treeCh: make(chan uint64, 64), styles: map[string]string{}, inputs: map[string]string{}}}
	tt, err := New(Options{
		App: app, Dir: h.dir, CredsPath: h.dir + "/creds.json", UI: h.ui.hooks(), NoAutoStart: true,
		CacheDir: t.TempDir(), Fetch: h.fetch, Log: func(level, msg string) {
			if level == "error" || level == "warn" {
				t.Logf("js %s: %s", level, msg)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.tt = tt
	t.Cleanup(func() { _ = tt.Close() })
	return h
}

func (h *harness) fetch(ctx context.Context, req *http.Request) (*http.Response, error) {
	u := req.URL.String()
	if strings.HasPrefix(u, h.api.srv.URL) {
		return http.DefaultClient.Do(req.WithContext(ctx))
	}
	h.fetchMu.Lock()
	defer h.fetchMu.Unlock()
	for prefix, body := range h.fetches {
		if strings.HasPrefix(u, prefix) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		}
	}
	return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(nil)), Request: req}, nil
}

func mustUser(s *store.Store, id string) *slack.User {
	u, _ := s.User(id)
	return u
}

// seed fills the store with a small synthetic workspace.
func seed(s *store.Store) {
	s.Lock()
	s.TeamID, s.TeamName, s.TeamDomain = teamID, "Synthetic Club", "hackclub"
	s.Unlock()
	s.PutUsers([]slack.User{
		{ID: selfID, Name: "me", RealName: "Test Self", TZ: "America/New_York", Profile: slack.Profile{DisplayName: "self", RealName: "Test Self", Pronouns: "they/them"}},
		{ID: "UALICE", Name: "alice", RealName: "Alice Example", Profile: slack.Profile{DisplayName: "alice", RealName: "Alice Example", Pronouns: "she/her"}},
		{ID: "UBOB", Name: "bob", RealName: "Bob Example", Profile: slack.Profile{DisplayName: "bob", RealName: "Bob Example"}},
		{ID: slackbot, Name: "slackbot", RealName: "Slackbot", IsBot: true, Profile: slack.Profile{DisplayName: "Slackbot"}},
	})
	posting, _ := json.Marshal(map[string]any{"posting_restricted_to": map[string]any{"type": []string{"admin"}, "user": []string{"UALICE"}}, "threads_restricted_to": map[string]any{"type": []string{"ra"}, "user": []string{}}})
	s.PutConv(slack.Conversation{ID: chGen, Name: "general", IsChannel: true, IsMember: true, IsGeneral: true, NumMembers: 1234})
	s.PutConv(slack.Conversation{ID: chAnn, Name: "announcements", IsChannel: true, IsMember: true, NumMembers: 99, Properties: posting})
	s.PutConv(slack.Conversation{ID: dmAlice, IsIM: true, User: "UALICE", IsMember: true})
	s.PutConv(slack.Conversation{ID: "D0SLACKBOT", IsIM: true, User: slackbot, IsMember: true})
	s.PutExtConv(slack.Conversation{ID: chOther, Name: "elsewhere", IsChannel: true, IsMember: false, NumMembers: 7})
	s.SetLatest(chGen, []slack.Message{
		{Type: "message", TS: "1700000001.000100", User: "UALICE", Text: "the job fair is today"},
		{Type: "message", TS: "1700000002.000200", User: "UBOB", Text: "see https://example.com/?utm_source=x", Reactions: []slack.Reaction{{Name: "tada", Count: 2, Users: []string{"UALICE", selfID}}}},
		{Type: "message", TS: "1700000003.000300", User: selfID, Text: "hello there"},
	}, false)
	s.SetLatest(dmAlice, []slack.Message{{Type: "message", TS: "1700000010.000100", User: "UALICE", Text: "hi"}}, false)
	s.PutBot("B0BOT", store.BotInfo{Name: "Helper Bot"})
}

// ---- helpers ----

func (h *harness) ctx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	h.t.Cleanup(cancel)
	return ctx
}

func (h *harness) rt() *runtime {
	r, err := h.tt.ensureRuntime()
	if err != nil {
		h.t.Fatal(err)
	}
	return r
}

// js evaluates an async JS function body with `T` = the runtime internals,
// `api` = the base TautAPI, and returns its JSON result.
func (h *harness) js(body string) json.RawMessage {
	h.t.Helper()
	r := h.rt()
	name := fmt.Sprintf("__test%d", time.Now().UnixNano())
	src := fmt.Sprintf("globalThis.%s = async () => { const T = globalThis.__taut; const api = globalThis.TautAPI; %s }", name, body)
	if _, err := r.rt.Eval(name+".js", src); err != nil {
		h.t.Fatalf("js: %v\n%s", err, body)
	}
	out, err := r.rt.Call(h.ctx(), name)
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

// eventually polls cond until it holds or d passes.
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

// render mounts a Slack component through the bridge and returns its first tree.
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

// findNative finds the first "@" node named n in a host tree.
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

// treeText is all text in a host tree.
func treeText(nodes []any) string {
	var b strings.Builder
	var walk func([]any)
	walk = func(ns []any) {
		for _, x := range ns {
			switch m := x.(type) {
			case string:
				b.WriteString(m)
			case map[string]any:
				if m["t"] == "#" {
					fmt.Fprint(&b, m["v"])
				}
				if kids, ok := m["c"].([]any); ok {
					walk(kids)
				}
			}
		}
	}
	walk(nodes)
	return b.String()
}

type introspection struct {
	Running        []string          `json:"running"`
	Components     []string          `json:"components"`
	Thunks         []string          `json:"thunks"`
	RTM            []string          `json:"rtm"`
	Buttons        []string          `json:"buttons"`
	SendChecks     int               `json:"sendChecks"`
	LinkTransforms int               `json:"linkTransforms"`
	InlineMarkup   bool              `json:"inlineMarkup"`
	SendTransforms int               `json:"sendTransforms"`
	Actionable     int               `json:"actionable"`
	Styles         []string          `json:"styles"`
	Slices         []string          `json:"slices"`
	StatePatches   int               `json:"statePatches"`
	Tabs           []string          `json:"tabs"`
	Overlays       []string          `json:"overlays"`
	Experiments    map[string]any    `json:"experiments"`
	Resize         int               `json:"resize"`
	Peers          int               `json:"peers"`
	Extra          map[string]string `json:"-"`
}

func (h *harness) introspect() introspection {
	var in introspection
	out, err := h.rt().rt.Call(h.ctx(), "__taut.introspect")
	if err != nil {
		h.t.Fatal(err)
	}
	if err := json.Unmarshal(out, &in); err != nil {
		h.t.Fatal(err)
	}
	return in
}

func has(list []string, x string) bool {
	for _, y := range list {
		if y == x {
			return true
		}
	}
	return false
}
