package composer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
)

// Synthetic state only: a made-up workspace and a fake Slack API that
// records calls. No real Slack, no real message contents.

const (
	tSelf  = "USELF"
	tTeam  = "T0266FRGM"   // the Hack Club team id, so hackClubOnly plugins run
	tEnt   = "E09V59WQY1E" // and its grid
	tChGen = "C0GENERAL"
)

type fakeSlack struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls []fakeCall
	reply map[string]any // method → answer, overriding the defaults
}

type fakeCall struct {
	Method string
	Form   url.Values
}

func newFakeSlack(t testing.TB) *fakeSlack {
	f := &fakeSlack{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(8 << 20)
		method := strings.TrimPrefix(r.URL.Path, "/api/")
		f.mu.Lock()
		f.calls = append(f.calls, fakeCall{method, r.Form})
		n := len(f.calls)
		f.mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/upload/") {
			_, _ = w.Write([]byte("OK - 4"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		rep := map[string]any{"ok": true}
		switch method {
		case "chat.postMessage":
			ts := "1700000100." + strings.Repeat("0", 5) + string(rune('0'+n%10))
			rep = map[string]any{"ok": true, "channel": r.Form.Get("channel"), "ts": ts,
				"message": map[string]any{"type": "message", "ts": ts, "user": tSelf, "text": r.Form.Get("text")}}
		case "files.getUploadURLExternal":
			rep = map[string]any{"ok": true, "upload_url": f.srv.URL + "/upload/F0TEST", "file_id": "F0TEST"}
		case "files.completeUploadExternal":
			rep = map[string]any{"ok": true, "files": []map[string]any{{"id": "F0TEST", "title": "x"}}}
		case "drafts.create", "drafts.update":
			rep = map[string]any{"ok": true, "draft": map[string]any{"id": "Dr0TEST", "client_draft_id": "x"}}
		}
		f.mu.Lock()
		if r, ok := f.reply[method].(map[string]any); ok {
			rep = r
		}
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(rep)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSlack) called(method string) []url.Values {
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

// syntheticApp is a core.App over a seeded store and the fake API.
func syntheticApp(t testing.TB) (*core.App, *fakeSlack) {
	t.Helper()
	f := newFakeSlack(t)
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test", UserID: tSelf, Team: tTeam, EnterpriseID: tEnt,
		TeamURL: f.srv.URL + "/", EnterpriseURL: f.srv.URL + "/"})
	c.EdgeURL = f.srv.URL + "/edge/" // never the real edgeapi.slack.com
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName, s.TeamDomain = tTeam, "Synthetic Club", "hackclub"
	s.Unlock()
	s.PutUsers([]slack.User{
		{ID: tSelf, Name: "me", RealName: "Test Self", Profile: slack.Profile{DisplayName: "self", RealName: "Test Self"}},
		{ID: "UALICE", Name: "alice", RealName: "Alice Example", Profile: slack.Profile{DisplayName: "alice", RealName: "Alice Example"}},
	})
	s.PutConv(slack.Conversation{ID: tChGen, Name: "general", IsChannel: true, IsMember: true, IsGeneral: true, NumMembers: 12})
	app := core.New(c, s)
	if u, ok := s.User(tSelf); ok {
		app.S.Self = *u
	}
	app.ServeOffline()
	return app, f
}

// noPluginTaut is a real plugin system with nothing installed (no JS engine
// starts).
func noPluginTaut(t testing.TB, app *core.App) *taut.Taut {
	t.Helper()
	dir := t.TempDir()
	tt, err := taut.New(taut.Options{App: app, Dir: dir, CredsPath: dir + "/creds.json", CacheDir: t.TempDir(), NoAutoStart: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tt.Close() })
	return tt
}

// stubTyping counts typing natives instead of using a socket.
func stubTyping(t testing.TB) *int {
	n := new(int)
	old := typingNative
	typingNative = func(coreAPI, string, string) error { *n++; return nil }
	t.Cleanup(func() { typingNative = old })
	return n
}

func withTaut(t testing.TB, tt *taut.Taut) {
	old := currentTaut()
	SetTaut(tt)
	t.Cleanup(func() { SetTaut(old) })
}

// TestRoutingNoPluginsNoAllocs: with no plugin, the routed call sites are
// direct calls; deciding costs atomic loads and zero allocations.
func TestRoutingNoPluginsNoAllocs(t *testing.T) {
	app, _ := syntheticApp(t)
	n := stubTyping(t)
	api := coreAPI{app}
	blocks := json.RawMessage(`[{"type":"rich_text","elements":[]}]`)
	for _, tc := range []struct {
		name string
		tt   *taut.Taut
	}{{"nil taut", nil}, {"no plugins", noPluginTaut(t, app)}} {
		t.Run(tc.name, func(t *testing.T) {
			withTaut(t, tc.tt)
			*n = 0
			ctx := context.Background()
			names := []string{thPrepareAndSend, thSendMessage, thDraftCreate, thDraftUpdate, thDraftDelete,
				thPendingFile, thUploadFiles, thACChannels, thACMembers}
			patched := ""
			allocs := testing.AllocsPerRun(1000, func() {
				syncHooks()
				sendTyping(api, tChGen, "")
				_, _ = transformBlocks(ctx, "hi", blocks)
				for _, name := range names {
					if currentTaut().ThunkPatched(name) {
						patched = name
					}
				}
			})
			if patched != "" {
				t.Fatalf("%s patched with no plugins", patched)
			}
			if allocs != 0 {
				t.Errorf("allocs per call = %v, want 0", allocs)
			}
			if *n < 1000 {
				t.Errorf("typing native ran %d times, want every call", *n)
			}
			if SendChecks != nil || LinkTransforms != nil || InlineMarkup != nil {
				t.Error("hooks installed with no plugins")
			}
		})
	}
}

func BenchmarkThunkGuard(b *testing.B) {
	app, _ := syntheticApp(b)
	for _, tc := range []struct {
		name string
		tt   *taut.Taut
	}{{"nil", nil}, {"noPlugins", noPluginTaut(b, app)}} {
		b.Run(tc.name, func(b *testing.B) {
			withTaut(b, tc.tt)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if currentTaut().ThunkPatched(thTyping) {
					b.Fatal("patched")
				}
			}
		})
	}
}

// BenchmarkSendTyping compares the routed typing call with the direct
// native: with no plugins they must cost the same.
func BenchmarkSendTyping(b *testing.B) {
	app, _ := syntheticApp(b)
	stubTyping(b)
	api := coreAPI{app}
	b.Run("direct", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = typingNative(api, tChGen, "")
		}
	})
	b.Run("routedNoPlugins", func(b *testing.B) {
		withTaut(b, noPluginTaut(b, app))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sendTyping(api, tChGen, "")
		}
	})
}

// BenchmarkSyncHooks is the per-frame hook check.
func BenchmarkSyncHooks(b *testing.B) {
	app, _ := syntheticApp(b)
	withTaut(b, noPluginTaut(b, app))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		syncHooks()
	}
}

func TestSyncHooksLeavesForeignHooks(t *testing.T) {
	withTaut(t, nil)
	mine := func(u string) string { return u + "#x" }
	LinkTransforms = mine
	t.Cleanup(func() { LinkTransforms = nil })
	syncHooks()
	if LinkTransforms == nil || LinkTransforms("a") != "a#x" {
		t.Fatal("syncHooks cleared a hook it didn't install")
	}
}

func TestDraftIDsSlackShaped(t *testing.T) {
	re := regexp.MustCompile(`^([A-Z0-9]+)-(\d+\.\d+)$`) // OwlMode's DRAFT_ID_RE
	c := &Composer{ch: tChGen, ts: "1700000000.000100"}
	c.initIDs()
	if m := re.FindStringSubmatch(c.draftID()); m == nil || m[1] != tChGen || m[2] != c.ts {
		t.Errorf("thread draftId %q", c.draftID())
	}
	c = &Composer{ch: tChGen}
	c.initIDs()
	if c.draftID() != tChGen {
		t.Errorf("channel draftId %q", c.draftID())
	}
	c = &Composer{edit: &core.Message{Channel: tChGen, TS: "1700000000.000100"}}
	c.initIDs()
	if !strings.HasPrefix(c.draftID(), "edit-") {
		t.Errorf("edit draftId %q", c.draftID())
	}
}

// TestAutocompleteRowsRoundTrip: a plugin's answer (filtered, reordered,
// one row added) maps back to full composer rows.
func TestAutocompleteRowsRoundTrip(t *testing.T) {
	items := []acItem{
		{Kind: acUser, ID: "UALICE", Name: "alice", Secondary: "Alice Example", Avatar: "https://a/x.png", Atom: &Atom{Kind: AtomUser, ID: "UALICE", Text: "@alice"}},
		{Kind: acUser, ID: "here", Name: "here", Atom: &Atom{Kind: AtomBroadcast, ID: "here", Text: "@here"}},
		{Kind: acUser, ID: "channel", Name: "channel", Atom: &Atom{Kind: AtomBroadcast, ID: "channel", Text: "@channel"}},
	}
	rows := toRows(items)
	if rows[1].ID != "BKhere" || rows[1].Item["is_broadcast_keyword"] != true {
		t.Fatalf("broadcast row %+v", rows[1])
	}
	b, _ := json.Marshal(rows)
	var back []acRow
	_ = json.Unmarshal(b, &back)
	// NativeAtChannel-style: drop @channel, keep the rest, plus a new user.
	plugin := []acRow{back[1], back[0], {ID: "UBOB", Item: map[string]any{"id": "UBOB", "name": "bob", "real_name": "Bob Example"}}}
	known := map[string]acItem{}
	for i := range items {
		known[rowKey(&items[i])] = items[i]
	}
	out := fromRows(acUser, plugin, known)
	if len(out) != 3 || out[0].ID != "here" || out[1].Avatar != "https://a/x.png" || out[2].Atom == nil || out[2].Atom.Text != "@bob" {
		t.Fatalf("mapped %+v", out)
	}
	ch := fromRows(acChannel, []acRow{{ID: "G0PRIV", Item: map[string]any{"name": "secret", "is_private": true}}}, nil)
	if len(ch) != 1 || !ch[0].Private || ch[0].Atom.Text != "#secret" {
		t.Fatalf("channel %+v", ch)
	}
}
