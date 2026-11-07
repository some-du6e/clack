package messages

import (
	"context"
	"encoding/json"
	"image"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// A synthetic workspace (made-up users and messages, no real Slack) under a
// real core.App and, for the plugin tests, a real taut over jsrt.

const (
	tSelf  = "USELF"
	tGen   = "C0GENERAL"
	tBot   = "USLACKBOT"
	tBotDM = "D0SLACKBOT"
	tsFair = "1700000001.000100"
	tsTada = "1700000002.000200"
	tsMine = "1700000003.000300"
	tEmpty = "C0NOTCACHED" // no cached history: opening it calls conversations.history
)

type fakeSlack struct {
	srv   *httptest.Server
	mu    sync.Mutex
	fail  map[string]string // method -> Slack error code to answer with
	calls []struct {
		method string
		form   url.Values
	}
}

func (f *fakeSlack) called(method string) []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []url.Values
	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c.form)
		}
	}
	return out
}

func newTestApp(t testing.TB) (*core.App, *fakeSlack) {
	f := &fakeSlack{fail: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(8 << 20)
		f.mu.Lock()
		f.calls = append(f.calls, struct {
			method string
			form   url.Values
		}{strings.TrimPrefix(r.URL.Path, "/api/"), r.Form})
		code := f.fail[strings.TrimPrefix(r.URL.Path, "/api/")]
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if code != "" {
			_, _ = w.Write([]byte(`{"ok":false,"error":"` + code + `"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(f.srv.Close)
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test", UserID: tSelf, Team: "T0266FRGM", EnterpriseID: "E09V59WQY1E",
		TeamURL: f.srv.URL + "/", EnterpriseURL: f.srv.URL + "/"})
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName, s.TeamDomain = "T0266FRGM", "Synthetic Club", "hackclub"
	s.Unlock()
	s.PutUsers([]slack.User{
		{ID: tSelf, Name: "me", RealName: "Test Self", Profile: slack.Profile{DisplayName: "self", Pronouns: "they/them", Image48: "https://avatars.invalid/self-48.png"}},
		{ID: "UALICE", Name: "alice", RealName: "Alice Example", Profile: slack.Profile{DisplayName: "alice", Pronouns: "she/her", Image48: "https://avatars.invalid/alice-48.png"}},
		{ID: "UBOB", Name: "bob", RealName: "Bob Example", Profile: slack.Profile{DisplayName: "bob"}},
		{ID: tBot, Name: "slackbot", RealName: "Slackbot", IsBot: true, Profile: slack.Profile{DisplayName: "Slackbot"}},
	})
	s.PutConv(slack.Conversation{ID: tGen, Name: "general", IsChannel: true, IsMember: true, IsGeneral: true, NumMembers: 3})
	s.PutConv(slack.Conversation{ID: tBotDM, IsIM: true, User: tBot, IsMember: true})
	s.PutConv(slack.Conversation{ID: tEmpty, Name: "not-cached", IsChannel: true, IsMember: true, NumMembers: 2})
	s.SetLatest(tGen, []slack.Message{
		{Type: "message", TS: tsFair, User: "UALICE", Text: "the job fair is today", ReplyCount: 1, ThreadTS: tsFair, LatestReply: "1700000004.000400", ReplyUsers: []string{"UBOB"}},
		{Type: "message", TS: tsTada, User: "UBOB", Text: "synthetic tada", Reactions: []slack.Reaction{{Name: "tada", Count: 2, Users: []string{"UALICE", tSelf}}}},
		{Type: "message", TS: tsMine, User: tSelf, Text: "hello there"},
	}, false)
	s.SetThread(tGen, tsFair, []slack.Message{
		{Type: "message", TS: tsFair, User: "UALICE", Text: "the job fair is today", ThreadTS: tsFair, ReplyCount: 1},
		{Type: "message", TS: "1700000004.000400", User: "UBOB", Text: "synthetic reply", ThreadTS: tsFair},
	})
	s.SetLatest(tBotDM, []slack.Message{{Type: "message", TS: "1700000100.000100", User: tBot, Text: "welcome"}}, false)
	app := core.New(c, s)
	if u, ok := s.User(tSelf); ok {
		app.S.Self = *u
	}
	app.ServeOffline()
	return app, f
}

// coreHost is a Host over a real core and theme.
type coreHost struct {
	nilHost
	app *core.App
	th  *theme.Theme
}

func (h coreHost) Core() *core.App     { return h.app }
func (h coreHost) Theme() *theme.Theme { return h.th }

func frameFn(v interface {
	Layout(gtx layout.Context) layout.Dimensions
}) func() {
	ops := new(op.Ops)
	return func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(900, 700)), Now: time.Now()}
		v.Layout(gtx)
	}
}

// until runs frames until cond holds.
func until(t *testing.T, d time.Duration, what string, frame func(), cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		frame()
		if cond() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func newTaut(t testing.TB, app *core.App, hooks taut.UIHooks) *taut.Taut {
	dir := t.TempDir()
	tt, err := taut.New(taut.Options{App: app, Dir: dir, CredsPath: filepath.Join(dir, "creds.json"), UI: hooks, NoAutoStart: true,
		CacheDir: filepath.Join(dir, "cache"), Log: func(level, msg string) {
			if level == "error" {
				t.Logf("js %s: %s", level, msg)
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tt.Close() })
	return tt
}

func install(t *testing.T, tt *taut.Taut, code []byte) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id, err := tt.Import(ctx, code, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatalf("enable %s: %v", id, err)
	}
	time.Sleep(150 * time.Millisecond)
	return id
}

// ---- zero cost ----

// listFrame lays out a loaded channel list (a dozen frames to settle).
func listFrame(t testing.TB) func() {
	app, _ := newTestApp(t)
	h := coreHost{app: app, th: theme.New(false)}
	v := NewChannelView(h, tGen)
	v.Preview = true
	f := frameFn(v)
	for i := 0; i < 200 && !(v.s.snap.loaded && len(v.s.items) > 0); i++ {
		f()
		time.Sleep(5 * time.Millisecond)
	}
	for i := 0; i < 10; i++ {
		f()
	}
	return f
}

// With a plugin system attached but nothing patched, a frame costs the
// same as without one: no JS engine, no extra allocations.
func TestTautZeroCost(t *testing.T) {
	SetTaut(nil)
	f := listFrame(t)
	base := testing.AllocsPerRun(100, f)

	app, _ := newTestApp(t)
	tt := newTaut(t, app, taut.UIHooks{})
	SetTaut(tt)
	defer SetTaut(nil)
	for i := 0; i < 5; i++ {
		f()
	}
	with := testing.AllocsPerRun(100, f)
	t.Logf("list frame: %v allocs without taut, %v with taut and no plugins", base, with)
	if with > base {
		t.Errorf("an idle plugin system costs %v allocs/frame (none: %v)", with, base)
	}
	if tt.Started() {
		t.Error("the JS engine started without an enabled plugin")
	}
}

func BenchmarkListFrame(b *testing.B) {
	for _, mode := range []string{"no-taut", "taut-idle"} {
		b.Run(mode, func(b *testing.B) {
			SetTaut(nil)
			f := listFrame(b)
			if mode == "taut-idle" {
				app, _ := newTestApp(b)
				SetTaut(newTaut(b, app, taut.UIHooks{}))
				defer SetTaut(nil)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				f()
			}
		})
	}
}

// ---- thunk shapes ----

func pluginJS(src string) []byte {
	return []byte("(() => {\nconst TautPlugin = globalThis.TautPlugin, opt = globalThis.TautOpt;\n" + src + "\n})()")
}

// recorder checks Slack's argument shapes and rewrites them observably,
// so the test sees through core's API calls that the list dispatched the
// thunk with that shape and that natives honour the plugin's args.
const recorderPlugin = `
class ClackShapes extends TautPlugin {
  static id = 'ClackShapes'
  static pluginName = 'Clack thunk shapes'
  static description = 'test: checks the list dispatches Slack-shaped thunks'
  static authors = ['clack']
  static category = 'other'
  static defaultConfig = { enabled: false }
  start() {
    const r = this.api.redux
    const keys = (a) => Object.keys(a || {}).sort().join(',')
    r.patchThunk('markLastRead', (orig) => (args, ...rest) =>
      keys(args) === 'id,ts' && args.id === '` + tGen + `' ? orig({ ...args, ts: '` + tsTada + `' }, ...rest) : orig(args, ...rest))
    r.patchThunk('changeReactionFromUserAction', (orig) => (args, ...rest) =>
      args?.key === 'message-` + tsTada + `-` + tGen + `' && args.name === 'tada' ? orig({ ...args, name: 'eyes' }, ...rest) : orig(args, ...rest))
    r.patchThunk('submitAddOrRemoveReaction', (orig) => (args, ...rest) =>
      keys(args) === 'add,name,reactionKey' ? orig({ ...args, name: args.name + '_ok' }, ...rest) : orig(args, ...rest))
    r.patchThunk('commitMessageEdit', (orig) => (args, ...rest) =>
      keys(args) === 'blocks,channelId,text,ts' ? orig({ ...args, text: args.text + ' (shape ok)' }, ...rest) : orig(args, ...rest))
    r.patchThunk('chatDeleteFetcher', (orig) => (args, ...rest) =>
      keys(args) === 'channel,ts' ? orig({ ...args, ts: '` + tsMine + `' }, ...rest) : orig(args, ...rest))
  }
}
return ClackShapes`

func TestTautThunkShapes(t *testing.T) {
	app, api := newTestApp(t)
	tt := newTaut(t, app, taut.UIHooks{})
	SetTaut(tt)
	defer SetTaut(nil)
	install(t, tt, pluginJS(recorderPlugin))
	until(t, 3*time.Second, "thunks patched", func() {}, func() bool {
		return tt.ThunkPatched("markLastRead") && tt.ThunkPatched("submitAddOrRemoveReaction")
	})
	h := coreHost{app: app, th: theme.New(false)}
	v := NewChannelView(h, tGen)
	f := frameFn(v)
	// at the bottom the list marks read: markLastRead {id, ts}, the plugin
	// moves ts, core marks that
	until(t, 5*time.Second, "conversations.mark", f, func() bool {
		for _, form := range api.called("conversations.mark") {
			if form.Get("channel") == tGen && form.Get("ts") == tsTada {
				return true
			}
		}
		return false
	})
	// a reaction click: changeReactionFromUserAction {key, name} renamed by
	// the plugin, then submitAddOrRemoveReaction {reactionKey, name, add}
	r := v.s.rows[tsTada]
	if r == nil {
		t.Fatal("no row for the tada message")
	}
	v.s.react(h, r, "tada", false, tSelf)
	until(t, 5*time.Second, "reactions call", f, func() bool {
		return len(api.called("reactions.add")) > 0 || len(api.called("reactions.remove")) > 0
	})
	var got string
	for _, m := range []string{"reactions.add", "reactions.remove"} {
		for _, form := range api.called(m) {
			got += m + ":" + form.Get("name") + " "
		}
	}
	if !strings.Contains(got, "eyes_ok") {
		t.Errorf("reaction calls %q, want the plugin-rewritten name eyes_ok", got)
	}
	// edits (composer.SaveEdit) and deletes (popups' Delete hook)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	blocks := json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"edited"}]}]}]`)
	if err := commitEdit(ctx, app, tGen, tsMine, "edited", blocks); err != nil {
		t.Fatal(err)
	}
	if ups := api.called("chat.update"); len(ups) != 1 || ups[0].Get("text") != "edited (shape ok)" || ups[0].Get("ts") != tsMine {
		t.Errorf("chat.update %v, want the commitMessageEdit-shaped edit", ups)
	}
	if err := deleteMessage(ctx, app, tGen, "1700000099.000000"); err != nil {
		t.Fatal(err)
	}
	if ds := api.called("chat.delete"); len(ds) != 1 || ds[0].Get("ts") != tsMine {
		t.Errorf("chat.delete %v, want the chatDeleteFetcher-shaped delete", ds)
	}
}

// ---- the published plugins (taut-plugins artifact) ----

func pluginFile(t *testing.T, name string) []byte {
	dir := os.Getenv("TAUT_PLUGINS_DIR")
	if dir == "" {
		t.Skip("TAUT_PLUGINS_DIR not set (the taut workflow builds Taut's plugins)")
	}
	b, err := os.ReadFile(filepath.Join(dir, name+".js"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// pluginUI wires taut to slackc like the ui root and records the trees.
type pluginUI struct {
	mu    sync.Mutex
	trees map[uint64][]byte
}

func (u *pluginUI) hooks() taut.UIHooks {
	return taut.UIHooks{
		Attached: func(b *taut.Bridge) { slackc.SetBridge(b) },
		Deliver: func(root uint64, tree []byte) {
			u.mu.Lock()
			u.trees[root] = append([]byte(nil), tree...)
			u.mu.Unlock()
			_ = slackc.Deliver(root, tree)
		},
		SetPatched:  slackc.SetPatched,
		CallGo:      slackc.CallGo,
		SetStyle:    slackc.SetStyle,
		RemoveStyle: slackc.RemoveStyle,
		ThreadPane:  ThreadPane,
	}
}

func (u *pluginUI) anyTree(sub string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, tr := range u.trees {
		if strings.Contains(string(tr), sub) {
			return true
		}
	}
	return false
}

func pluginEnv(t *testing.T) (*core.App, *fakeSlack, *taut.Taut, *pluginUI, coreHost) {
	app, api := newTestApp(t)
	u := &pluginUI{trees: map[uint64][]byte{}}
	tt := newTaut(t, app, u.hooks())
	slackc.SetBridge(tt.Bridge())
	SetTaut(tt)
	t.Cleanup(func() { SetTaut(nil); slackc.SetBridge(nil); slackc.SetPatched(nil) })
	return app, api, tt, u, coreHost{app: app, th: theme.New(false)}
}

func TestPluginCensorship(t *testing.T) {
	code := pluginFile(t, "Censorship")
	_, _, tt, _, h := pluginEnv(t)
	install(t, tt, code)
	v := NewChannelView(h, tGen)
	v.Preview = true
	f := frameFn(v)
	until(t, 5*time.Second, "the censored row", f, func() bool {
		r := v.s.rows[tsFair]
		return r != nil && r.m.Text == "the *** fair is today" && strings.Contains(r.rv.TextRange(0, r.rv.PlainLen()), "***")
	})
	if r := v.s.rows[tsFair]; r.src.Text != "the job fair is today" {
		t.Errorf("the source message changed: %q", r.src.Text)
	}
}

func TestPluginShutUpSlackbot(t *testing.T) {
	code := pluginFile(t, "ShutUpSlackbot")
	app, api, tt, _, h := pluginEnv(t)
	install(t, tt, code)
	v := NewChannelView(h, tBotDM)
	v.Preview = true // only the plugin marks read here
	f := frameFn(v)
	for i := 0; i < 20; i++ {
		f()
	}
	notice := "Your workspace has been using `/poll` to kick off certain actions with Old Poll. Recently, New Poll was installed to Synthetic Club with the same command. " +
		"Now, when people enter `/poll`, it'll run the action set up by New Poll. You can learn more about <https://slack.com/help/articles/201259356-Slash-commands-in-Slack|managing app installation settings> in the Help Center."
	raw, _ := json.Marshal(map[string]any{"type": "message", "channel": tBotDM, "user": tBot, "text": notice, "ts": "1700000500.000100"})
	app.Inject(slack.Event{Type: "message", Channel: json.RawMessage(`"` + tBotDM + `"`), Raw: raw})
	until(t, 5*time.Second, "the notice in the list and marked read", f, func() bool {
		if v.s.rows["1700000500.000100"] == nil {
			return false
		}
		for _, form := range api.called("conversations.mark") {
			if form.Get("channel") == tBotDM && form.Get("ts") == "1700000500.000100" {
				return true
			}
		}
		return false
	})
}

func TestPluginBringBackSlackbot(t *testing.T) {
	code := pluginFile(t, "BringBackSlackbot")
	_, _, tt, _, h := pluginEnv(t)
	install(t, tt, code)
	v := NewChannelView(h, tGen)
	v.Preview = true
	f := frameFn(v)
	until(t, 5*time.Second, "the forced experiment, list still drawing", f, func() bool {
		raw, ok := tt.View("experiments", "reskin_custom_responses")
		return ok && strings.Contains(string(raw), `"off"`) && len(v.s.rows) >= 3
	})
}

func TestPluginUserPronouns(t *testing.T) {
	code := pluginFile(t, "UserPronouns")
	_, _, tt, u, h := pluginEnv(t)
	install(t, tt, code)
	until(t, 3*time.Second, "components patched", func() {}, func() bool {
		return slackc.Patched("BaseBroadcastPreamble") && slackc.Patched("ThreadSenderAndTimestampGeneric")
	})
	// channel rows: Alice's header gets her pronouns
	v := NewChannelView(h, tGen)
	v.Preview = true
	until(t, 8*time.Second, "pronouns in the channel header", frameFn(v), func() bool { return u.anyTree("she/her") })
	// the thread pane: ThreadSenderAndTimestampGeneric, rendered with $pane
	tv := NewThreadView(h, tGen, tsFair)
	ft := frameFn(tv)
	until(t, 8*time.Second, "the thread loaded", ft, func() bool { return len(tv.s.snap.msgs) > 0 })
	if got := ThreadPane("loaded", tGen, tsFair, ""); len(got.([]string)) == 0 {
		t.Errorf("usePane().loaded() = %v", got)
	}
	r := tv.s.rows[tsFair]
	if r == nil {
		t.Fatal("no thread root row")
	}
	if p := r.threadSenderProps(); p["$pane"] == nil {
		t.Errorf("thread component props lack $pane: %v", p)
	}
}

func TestPluginWhoReacted(t *testing.T) {
	code := pluginFile(t, "WhoReacted")
	_, _, tt, u, h := pluginEnv(t)
	install(t, tt, code)
	until(t, 3*time.Second, "Reaction patched", func() {}, func() bool { return slackc.Patched("Reaction") })
	v := NewChannelView(h, tGen)
	v.Preview = true
	// the tada pill: WhoReacted lists the reactors' avatars in it
	f := frameFn(v)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) && !(u.anyTree("taut-reactors__avatar") && u.anyTree("alice-48.png")) {
		f()
		time.Sleep(15 * time.Millisecond)
	}
	if !u.anyTree("taut-reactors__avatar") || !u.anyTree("alice-48.png") {
		u.mu.Lock()
		for root, tr := range u.trees {
			s := string(tr)
			if len(s) > 4000 {
				s = s[:4000] + "…"
			}
			t.Logf("tree %d: %s", root, s)
		}
		u.mu.Unlock()
		t.Fatalf("no reactor avatars (img.taut-reactors__avatar with Alice's picture) in the reaction pill; patched Reaction=%v UnstyledButton=%v",
			slackc.Patched("Reaction"), slackc.Patched("UnstyledButton"))
	}
}
