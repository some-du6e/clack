package popups

// Taut wiring tests: popups' thunks and plugin menus/modals with real Taut
// plugins (built by the popups-taut workflow into TAUT_PLUGINS_DIR) over a
// synthetic workspace and a fake Slack API. No live Slack, no window.

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

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

const (
	tzSelf  = "USELF0001"
	tzTeam  = "T0266FRGM" // Hack Club, so Hack Club–only plugins run
	tzEnt   = "E09V59WQY1E"
	tzGen   = "C0GENERAL1"
	tzOther = "C0ELSEWHR1"
	tzMsgTS = "1700000002.000200"
)

// ---- fake Slack API ----

type tzAPI struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls map[string][]url.Values
}

func tzNewAPI(t *testing.T) *tzAPI {
	f := &tzAPI{calls: map[string][]url.Values{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		f.mu.Lock()
		f.calls[method] = append(f.calls[method], r.Form)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "conversations.join":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "channel": map[string]any{"id": r.Form.Get("channel"), "name": "elsewhere", "is_channel": true, "is_member": true}})
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *tzAPI) called(method string) []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.calls[method]...)
}

// ---- host ----

type tzHost struct {
	a  *core.App
	th *theme.Theme
	im *imgcache.Cache

	mu       sync.Mutex
	shown    []Overlay
	toasts   []string
	navs     []string
	overlays []taut.Overlay
	trees    map[uint64][]byte
	clip     []string
}

func (h *tzHost) Core() *core.App         { return h.a }
func (h *tzHost) Theme() *theme.Theme     { return h.th }
func (h *tzHost) Images() *imgcache.Cache { return h.im }
func (h *tzHost) Invalidate()             {}
func (h *tzHost) Navigate(r string) {
	h.mu.Lock()
	h.navs = append(h.navs, r)
	h.mu.Unlock()
}
func (h *tzHost) OpenThread(ch, ts string) {}
func (h *tzHost) CloseSecondary()          {}
func (h *tzHost) ShowOverlay(o Overlay) {
	h.mu.Lock()
	h.shown = append(h.shown, o)
	h.mu.Unlock()
}
func (h *tzHost) OpenEmojiPicker(image.Point, func(string)) {}
func (h *tzHost) OpenProfile(image.Point, string)           {}
func (h *tzHost) OpenMessageMenu(image.Point, core.Message) {}
func (h *tzHost) OpenLightbox(richtext.Image, core.Message) {}
func (h *tzHost) Toast(s string)                            { h.mu.Lock(); h.toasts = append(h.toasts, s); h.mu.Unlock() }
func (h *tzHost) shownCount() int                           { h.mu.Lock(); defer h.mu.Unlock(); return len(h.shown) }
func (h *tzHost) tree(root uint64) []byte                   { h.mu.Lock(); defer h.mu.Unlock(); return h.trees[root] }
func (h *tzHost) clipboard() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.clip...)
}
func (h *tzHost) pluginOverlays() []taut.Overlay {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]taut.Overlay(nil), h.overlays...)
}
func (h *tzHost) toastList() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.toasts...)
}

// ---- workspace ----

func tzSeed(s *store.Store) {
	s.Lock()
	s.TeamID, s.TeamName, s.TeamDomain = tzTeam, "Synthetic Club", "hackclub"
	s.Unlock()
	s.PutUsers([]slack.User{
		{ID: tzSelf, Name: "me", RealName: "Test Self", Profile: slack.Profile{DisplayName: "self", RealName: "Test Self"}},
		{ID: "UALICE", Name: "alice", RealName: "Alice Example", Profile: slack.Profile{DisplayName: "alice", RealName: "Alice Example"}},
		{ID: "UBOB", Name: "bob", RealName: "Bob Example", Profile: slack.Profile{DisplayName: "bob", RealName: "Bob Example"}},
	})
	s.PutConv(slack.Conversation{ID: tzGen, Name: "general", IsChannel: true, IsMember: true, IsGeneral: true, NumMembers: 3})
	s.PutExtConv(slack.Conversation{ID: tzOther, Name: "elsewhere", IsChannel: true, IsMember: false, NumMembers: 7})
	s.SetLatest(tzGen, []slack.Message{
		{Type: "message", TS: "1700000001.000100", User: "UALICE", Text: "first"},
		{Type: "message", TS: tzMsgTS, User: "UBOB", Text: "react to me", Reactions: []slack.Reaction{{Name: "tada", Count: 2, Users: []string{"UALICE", tzSelf}}}},
	}, false)
}

type tzEnv struct {
	h   *tzHost
	tt  *taut.Taut
	api *tzAPI
	dir string
}

func tzPluginsDir(t *testing.T) string {
	dir := os.Getenv("TAUT_PLUGINS_DIR")
	if dir == "" {
		t.Skip("TAUT_PLUGINS_DIR not set: the popups-taut workflow builds Taut's plugins and runs this")
	}
	return dir
}

func tzNewEnv(t *testing.T) *tzEnv {
	t.Helper()
	api := tzNewAPI(t)
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test", UserID: tzSelf, Team: tzTeam, EnterpriseID: tzEnt,
		TeamURL: api.srv.URL + "/", EnterpriseURL: api.srv.URL + "/"})
	s := store.New("")
	tzSeed(s)
	a := core.New(c, s)
	if u, ok := s.User(tzSelf); ok {
		a.S.Self = *u
	}
	a.ServeOffline()
	h := &tzHost{a: a, th: theme.New(false), trees: map[uint64][]byte{}}
	dir := t.TempDir()
	tt, err := taut.New(taut.Options{
		App: a, Dir: dir, CredsPath: filepath.Join(dir, "creds.json"), NoAutoStart: true, CacheDir: t.TempDir(),
		Fetch: func(ctx context.Context, req *http.Request) (*http.Response, error) {
			if strings.HasPrefix(req.URL.String(), api.srv.URL) {
				return http.DefaultClient.Do(req.WithContext(ctx))
			}
			return &http.Response{StatusCode: 404, Header: http.Header{}, Body: http.NoBody, Request: req}, nil
		},
		ClipboardWrite: func(s string) error { h.mu.Lock(); h.clip = append(h.clip, s); h.mu.Unlock(); return nil },
		Log: func(level, msg string) {
			if level == "error" || level == "warn" {
				t.Logf("js %s: %s", level, msg)
			}
		},
		UI: taut.UIHooks{
			Attached:   func(b *taut.Bridge) { slackc.SetBridge(b) },
			SetPatched: slackc.SetPatched,
			CallGo:     slackc.CallGo,
			Deliver: func(root uint64, tree []byte) {
				h.mu.Lock()
				h.trees[root] = append([]byte(nil), tree...)
				h.mu.Unlock()
			},
			Overlays: func(o []taut.Overlay) { h.mu.Lock(); h.overlays = o; h.mu.Unlock() },
			Toast:    func(text, icon string, d time.Duration) { h.Toast(text) },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		SetTaut(nil)
		slackc.SetBridge(nil)
		_ = tt.Close()
	})
	SetTaut(tt)
	return &tzEnv{h: h, tt: tt, api: api, dir: tzPluginsDir(t)}
}

func (e *tzEnv) ctx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// load imports and enables a built plugin, unmodified.
func (e *tzEnv) load(t *testing.T, name string) {
	t.Helper()
	code, err := os.ReadFile(filepath.Join(e.dir, name+".js"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := e.tt.Import(e.ctx(t), code, "")
	if err != nil {
		t.Fatalf("import %s: %v", name, err)
	}
	if err := e.tt.SetEnabled(e.ctx(t), id, true); err != nil {
		t.Fatalf("enable %s: %v", name, err)
	}
}

// until pumps popups' and slackc's UI queues until cond holds.
func tzUntil(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		Pump()
		slackc.Pump()
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func tzItem(items []MenuItem, key string) (MenuItem, bool) {
	for _, it := range items {
		if it.Key == key {
			return it, true
		}
		if it2, ok := tzItem(it.Sub, key); ok {
			return it2, true
		}
	}
	return MenuItem{}, false
}

// tzFind is the first "@" node named n in a host tree (depth first).
func tzFind(nodes []any, n string) map[string]any {
	for _, x := range nodes {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		if m["t"] == "@" && m["n"] == n {
			return m
		}
		if c, ok := m["c"].([]any); ok {
			if r := tzFind(c, n); r != nil {
				return r
			}
		}
		if p, ok := m["p"].(map[string]any); ok {
			for _, v := range p {
				if el, ok := v.(map[string]any); ok {
					if ns, ok := el["$el"].([]any); ok {
						if r := tzFind(ns, n); r != nil {
							return r
						}
					}
				}
			}
		}
	}
	return nil
}

func tzTree(t *testing.T, h *tzHost, root uint64) []any {
	var nodes []any
	if err := json.Unmarshal(h.tree(root), &nodes); err != nil {
		t.Fatalf("tree %d: %v", root, err)
	}
	return nodes
}

// ---- no plugins: the native path does nothing extra ----

func TestTautNoPluginsIsFree(t *testing.T) {
	SetTaut(nil)
	h := &tzHost{th: theme.New(false)}
	m := NewChannelMenu(h, image.Pt(10, 10), tzGen)
	if m == nil || len(m.items) == 0 {
		t.Fatal("no channel menu")
	}
	if n := h.shownCount(); n != 0 {
		t.Errorf("channel menu started %d async jobs without plugins", n)
	}
	mm := NewMemberMenu(h, image.Pt(10, 10), "UALICE")
	if mm == nil || len(mm.items) == 0 {
		t.Fatal("no member menu")
	}
	if n := h.shownCount(); n != 0 {
		t.Errorf("member menu started %d async jobs without plugins", n)
	}
	msgs := []core.Message{{Channel: tzGen, TS: tzMsgTS, Text: "x"}}
	if got := tautSearchInline(context.Background(), h, "x", msgs); len(got) != 1 || got[0].Text != "x" {
		t.Errorf("searchInline without plugins changed results: %+v", got)
	}
	if tautFor("getContextMenuTemplate") != nil {
		t.Error("tautFor without a runtime")
	}
}

func TestTautTemplateRoundTrip(t *testing.T) {
	h := &tzHost{th: theme.New(false)}
	ran := ""
	base := []MenuItem{
		{Label: "Copy link", Icon: "link", Do: func() { ran = "copy" }},
		Sep(),
		{Label: "Leave channel", Danger: true, Do: func() { ran = "leave" }},
	}
	tpl := tautTemplate(base)
	b, err := json.Marshal(tpl)
	if err != nil {
		t.Fatalf("template isn't JSON-safe: %v", err)
	}
	// a plugin appends an item, the way QuickJoin does
	var list []any
	_ = json.Unmarshal(b, &list)
	list = append(list, map[string]any{"key": "plugin__sep", "type": "separator"}, map[string]any{"key": "plugin__join", "label": "Join channel", "click": map[string]any{"$fn": 1<<51 | 7}})
	raw, _ := json.Marshal(list)
	items, ok := tautMenuItems(h, raw, base)
	if !ok || len(items) != 5 {
		t.Fatalf("items %+v", items)
	}
	items[0].Do()
	if ran != "copy" {
		t.Error("native item lost its action")
	}
	if it, ok := tzItem(items, "plugin__join"); !ok || it.Label != "Join channel" || it.Do == nil {
		t.Errorf("plugin item %+v", it)
	}
}

// ---- plugins ----

func TestTautPlugins(t *testing.T) {
	tzPluginsDir(t)
	t.Run("QuickJoin", func(t *testing.T) {
		e := tzNewEnv(t)
		e.load(t, "QuickJoin")
		tzUntil(t, 10*time.Second, "QuickJoin patches getContextMenuTemplate", func() bool { return e.tt.ThunkPatched("getContextMenuTemplate") })
		m := NewChannelMenu(e.h, image.Pt(10, 10), tzOther)
		var join MenuItem
		tzUntil(t, 10*time.Second, "Join channel in the channel menu", func() bool {
			var ok bool
			join, ok = tzItem(m.items, "taut-quick-join__join")
			return ok
		})
		if join.Label != "Join channel" || join.Do == nil {
			t.Fatalf("join item %+v", join)
		}
		if _, ok := tzItem(m.items, "copy"); !ok && len(m.items) < 3 {
			t.Errorf("native items gone: %+v", m.items)
		}
		join.Do()
		tzUntil(t, 10*time.Second, "conversations.join", func() bool { return len(e.api.called("conversations.join")) > 0 })
		// a channel you're in gets no join item
		mine := NewChannelMenu(e.h, image.Pt(10, 10), tzGen)
		before := len(mine.items)
		time.Sleep(300 * time.Millisecond)
		Pump()
		if _, ok := tzItem(mine.items, "taut-quick-join__join"); ok {
			t.Errorf("member channel got Join (%d → %d items)", before, len(mine.items))
		}
	})
	t.Run("Nicknames", func(t *testing.T) {
		e := tzNewEnv(t)
		e.load(t, "Nicknames")
		tzUntil(t, 10*time.Second, "Nicknames patches getMemberContextMenuTemplate", func() bool { return e.tt.ThunkPatched("getMemberContextMenuTemplate") })
		m := NewMemberMenu(e.h, image.Pt(10, 10), "UALICE")
		var set MenuItem
		tzUntil(t, 10*time.Second, "Set nickname… in the member menu", func() bool {
			var ok bool
			set, ok = tzItem(m.items, "taut-nicknames__set")
			return ok
		})
		if !strings.HasPrefix(set.Label, "Set nickname") || set.Do == nil {
			t.Fatalf("nickname item %+v", set)
		}
		if _, ok := tzItem(m.items, "copy_display_name"); !ok {
			t.Errorf("native items gone: %+v", m.items)
		}
		// Nicknames also patches MenuFromTemplate, which renders the menu
		// from its props: the thunk's item survives that render (live, the
		// native template came back and "Set nickname…" vanished)
		m.applyTemplate(m.tplProps())
		if _, ok := tzItem(m.items, "taut-nicknames__set"); !ok {
			t.Errorf("a MenuFromTemplate render dropped the thunk's item: %+v", m.items)
		}
		set.Do()
		tzUntil(t, 10*time.Second, "the nickname modal (api.modal.openModal)", func() bool {
			for _, o := range e.h.pluginOverlays() {
				if o.Kind == "modal" && o.Root != 0 {
					return true
				}
			}
			return false
		})
		// the modal renders through slackc as Slack's ConfirmationModal
		o := e.h.pluginOverlays()[0]
		tzUntil(t, 10*time.Second, "the modal's host tree", func() bool { return len(e.h.tree(o.Root)) > 0 })
		if n := tzFind(tzTree(t, e.h, o.Root), "ConfirmationModal"); n == nil {
			t.Errorf("modal tree has no ConfirmationModal: %s", e.h.tree(o.Root))
		}
	})
	t.Run("OwlMode", func(t *testing.T) {
		e := tzNewEnv(t)
		e.load(t, "OwlMode")
		tzUntil(t, 10*time.Second, "OwlMode patches submitAddOrRemoveReaction", func() bool { return e.tt.ThunkPatched("submitAddOrRemoveReaction") })
		// not an owl thread: the plugin passes it on to native (core.React)
		React(e.h, tzGen, tzMsgTS, ":eyes:", true)
		tzUntil(t, 10*time.Second, "reactions.add", func() bool {
			for _, v := range e.api.called("reactions.add") {
				if v.Get("name") == "eyes" && v.Get("timestamp") == tzMsgTS {
					return true
				}
			}
			return false
		})
		if ts := e.h.toastList(); len(ts) > 0 {
			t.Errorf("toasts: %v", ts)
		}
		// the picker's boundary carries the message OwlMode reads
		p := NewReactionPicker(e.h, image.Pt(10, 10), tzGen, tzMsgTS, "", nil)
		pp := p.popoverProps()
		if pp["channelId"] != tzGen || pp["ts"] != tzMsgTS {
			t.Errorf("EmojiPickerPopover props %v", pp)
		}
	})
	t.Run("CopyReacted", func(t *testing.T) {
		e := tzNewEnv(t)
		e.load(t, "CopyReacted")
		tzUntil(t, 10*time.Second, "CopyReacted patches ReactionAddButton", func() bool { return slackc.Patched("ReactionAddButton") })
		props, _ := json.Marshal(map[string]any{
			"reactionKey": reactionKey(tzGen, tzMsgTS),
			"reactions":   []any{map[string]any{"name": "tada", "count": 2, "users": []string{"UALICE", tzSelf}}},
			"children":    map[string]any{"$slot": 1, "n": "ReactionAddButton", "p": map[string]any{}},
		})
		const bar, menu = 101, 102
		e.tt.Bridge().Render(bar, "ReactionBar", props)
		var trig map[string]any
		tzUntil(t, 10*time.Second, "CopyReacted's Menu (MenuTrigger) in the reaction bar", func() bool {
			if len(e.h.tree(bar)) == 0 {
				return false
			}
			trig = tzFind(tzTree(t, e.h, bar), "MenuTrigger")
			return trig != nil
		})
		tp, _ := trig["p"].(map[string]any)
		rm, _ := tp["renderMenu"].(map[string]any)
		fn, _ := rm["$fn"].(float64)
		if fn == 0 {
			t.Fatalf("MenuTrigger has no renderMenu: %v", tp)
		}
		e.tt.Bridge().RenderFn(menu, int64(fn), []byte(`[{"isOpen":true,"position":"bottom"}]`))
		var mft map[string]any
		tzUntil(t, 10*time.Second, "MenuFromTemplate from renderMenu", func() bool {
			if len(e.h.tree(menu)) == 0 {
				return false
			}
			mft = tzFind(tzTree(t, e.h, menu), "MenuFromTemplate")
			return mft != nil
		})
		mp, _ := json.Marshal(mft["p"])
		p, err := slackc.DecodeProps(mp)
		if err != nil {
			t.Fatal(err)
		}
		// popups draws it: the template becomes Menu rows with JS clicks
		items := menuFromTemplate(e.h.th, p.List("template"), nil)
		every, ok := tzItem(items, "taut-copy-reacted__everyone")
		if !ok || !strings.HasPrefix(every.Label, "Everyone") || every.Do == nil {
			t.Fatalf("menu rows %+v", items)
		}
		every.Do()
		tzUntil(t, 10*time.Second, "the reactors on the clipboard", func() bool {
			for _, s := range e.h.clipboard() {
				if strings.Contains(s, "UALICE") || strings.Contains(s, "alice") {
					return true
				}
			}
			return false
		})
	})
}
