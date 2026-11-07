package views

// Taut plugin support in the views, against synthetic state (no live
// Slack): a fake Web API, a seeded store, core offline, and a small plugin
// that patches members, channels, message text and the clearDrafts thunk.

import (
	"context"
	"encoding/json"
	"image"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

const (
	tSelf  = "USELF"
	tAlice = "UALICE"
	tGen   = "C0GENERAL"
	tMsgTS = "1700000001.000100"
)

type fakeSlack struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls map[string][]string // method → draft_id / channel params seen
}

func newFakeSlack(t *testing.T) *fakeSlack {
	f := &fakeSlack{calls: map[string][]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(8 << 20)
		m := strings.TrimPrefix(r.URL.Path, "/api/")
		f.mu.Lock()
		f.calls[m] = append(f.calls[m], r.Form.Get("draft_id")+r.Form.Get("channel"))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSlack) seen(method string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls[method]...)
}

// tautTestHost is a views Host with a Taut plugin system.
type tautTestHost struct {
	app *core.App
	th  *theme.Theme
	tt  *taut.Taut
}

func (h *tautTestHost) Core() *core.App                           { return h.app }
func (h *tautTestHost) Theme() *theme.Theme                       { return h.th }
func (h *tautTestHost) Images() *imgcache.Cache                   { return nil }
func (h *tautTestHost) Invalidate()                               {}
func (h *tautTestHost) Navigate(string)                           {}
func (h *tautTestHost) OpenThread(string, string)                 {}
func (h *tautTestHost) CloseSecondary()                           {}
func (h *tautTestHost) OpenProfile(image.Point, string)           {}
func (h *tautTestHost) OpenMessageMenu(image.Point, core.Message) {}
func (h *tautTestHost) OpenLightbox(richtext.Image, core.Message) {}
func (h *tautTestHost) Toast(string)                              {}
func (h *tautTestHost) Taut() *taut.Taut                          { return h.tt }

func newTautViews(t *testing.T) (*tautTestHost, *fakeSlack) {
	t.Helper()
	api := newFakeSlack(t)
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test", UserID: tSelf, Team: "T0266FRGM", EnterpriseID: "E09V59WQY1E",
		TeamURL: api.srv.URL + "/", EnterpriseURL: api.srv.URL + "/"})
	s := store.New("")
	s.PutUsers([]slack.User{
		{ID: tSelf, Name: "me", RealName: "Test Self", Profile: slack.Profile{DisplayName: "self", RealName: "Test Self"}},
		{ID: tAlice, Name: "alice", RealName: "Alice Example", Profile: slack.Profile{DisplayName: "alice", RealName: "Alice Example"}},
	})
	s.PutConv(slack.Conversation{ID: tGen, Name: "general", IsChannel: true, IsMember: true, NumMembers: 3})
	s.SetLatest(tGen, []slack.Message{{Type: "message", TS: tMsgTS, User: tAlice, Text: "the job fair is today"}}, false)
	app := core.New(c, s)
	if u, ok := s.User(tSelf); ok {
		app.S.Self = *u
	}
	app.ServeOffline()
	dir := t.TempDir()
	tt, err := taut.New(taut.Options{App: app, Dir: dir, CredsPath: dir + "/creds.json", NoAutoStart: true, CacheDir: t.TempDir(),
		Log: func(level, msg string) {
			if level == "error" || level == "warn" {
				t.Logf("js %s: %s", level, msg)
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tt.Close() })
	return &tautTestHost{app: app, th: theme.New(false), tt: tt}, api
}

// viewsPlugin is a synthetic Taut plugin (Taut's user-plugin IIFE shape).
const viewsPlugin = `(() => {
const TautPlugin = globalThis.TautPlugin, opt = globalThis.TautOpt;
class ViewsProbe extends TautPlugin {
  static id = 'ViewsProbe'
  static pluginName = 'Views probe'
  static description = 'patches what the views read'
  static authors = ['test']
  static category = 'other'
  static defaultConfig = { enabled: false }
  start() {
    const api = this.api
    api.redux.patchSlice('members', (id, m) => id === '` + tAlice + `' ? api.members.modifyMemberObject(m, {name: 'Ally'}) : m)
    api.redux.patchSlice('channels', (id, c) => id === '` + tGen + `' ? {...c, name: 'secret-room'} : c)
    api.messages.patchMessageText((t) => t.replace(/job/g, '***'))
    api.redux.patchThunk('clearDrafts', (original) => (args, ...rest) => original({...args, ids: [...args.ids, 'Dx2']}, ...rest))
  }
}
return ViewsProbe
})()`

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestViewsUnpatchedZeroCost(t *testing.T) {
	h, _ := newTautViews(t)
	b := newBase(h)
	b.frame()
	if got := b.userName(tAlice); got != "alice" {
		t.Fatalf("userName = %q", got)
	}
	if got := b.convTitle(tGen, nil); got != "#general" {
		t.Fatalf("convTitle = %q", got)
	}
	u, _ := h.app.User(tAlice)
	m := &core.Message{Channel: tGen, TS: tMsgTS, Text: "x"}
	if a := testing.AllocsPerRun(200, func() {
		_ = b.viewUser(u)
		_ = b.viewChannelName(tGen, "general")
		if b.viewMsg(m) != m {
			t.Fatal("unpatched viewMsg must return its argument")
		}
		_ = b.viewsPatched()
	}); a != 0 {
		t.Fatalf("unpatched patched-view lookups allocate %.1f times", a)
	}
	// a Host without plugins at all (nil *taut.Taut) behaves the same
	h.tt = nil
	b.frame()
	if got := b.userName(tAlice); got != "alice" || b.viewsPatched() {
		t.Fatalf("nil taut: %q", got)
	}
}

func TestViewsReadPatchedViewsAndDispatch(t *testing.T) {
	h, api := newTautViews(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id, err := h.tt.Import(ctx, []byte(viewsPlugin), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	b := newBase(h)
	eventually(t, "member patch", func() bool { b.frame(); return b.userName(tAlice) == "Ally" })
	eventually(t, "channel patch", func() bool { b.frame(); return b.convTitle(tGen, nil) == "#secret-room" })
	m := &core.Message{Channel: tGen, TS: tMsgTS, User: tAlice, Text: "the job fair is today"}
	eventually(t, "message patch", func() bool { b.frame(); return strings.Contains(b.preview(m), "***") })
	if n, _ := b.authorOf(m); n != "Ally" {
		t.Errorf("authorOf = %q, want the patched member name", n)
	}
	if !b.viewsPatched() {
		t.Error("viewsPatched should be true with slice patches")
	}

	// clearDrafts runs the plugin's wrapper, which adds an id: native
	// (core.DeleteDraft) sees both.
	if err := clearDraftsThunk(ctx, h.tt, h.app, []string{"Dx1"}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(api.seen("drafts.delete"), ",")
	if got != "Dx1,Dx2" {
		t.Errorf("drafts.delete calls = %q, want Dx1,Dx2", got)
	}
	// unpatched thunks are just their native
	if err := deleteDraftThunk(ctx, h.tt, h.app, "Dx3", tGen); err != nil {
		t.Fatal(err)
	}
	if got := api.seen("drafts.delete"); len(got) != 3 || got[2] != "Dx3" {
		t.Errorf("deleteDraftApi native: %q", got)
	}

	// disabling the plugin restores the plain views
	if err := h.tt.SetEnabled(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, "unpatched", func() bool { b.frame(); return !b.viewsPatched() && b.userName(tAlice) == "alice" })
}
