package popups

// Slick acceptance through popups' native views: each TestSlickView<Plugin>
// installs the real built plugin (SLICK_PLUGINS_DIR, built by the ui-slick
// workflow with Slick's own build; skipped when unset), opens the real popup
// (profile card, full profile, menus, search, GIF picker) over a synthetic
// workspace with a fake Slack API, lays it out frame by frame, and checks
// what the user would see: the plugin's rows in the host trees those
// popups lay out, and the native rows the menus and lists end up drawing.
// No live Slack, no network, no window.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/slick"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	slSelf  = "USELF00001"
	slAlice = "UALICE0001"
	slBob   = "UBOB000001"
	slApp   = "UAPPBOT001" // an app's bot user
	slAppID = "A0APP00001"
	slTeam  = "T0266FRGM"
	slEnt   = "E09V59WQY1E"
	slGen   = "C0GENERAL1"
	slDMA   = "D0ALICE001"
	slDMB   = "D0BOB00001"
	slMsgTS = "1700000002.000200"
)

// ---- fake Slack API ----

type slAPI struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls map[string][]url.Values
	reply map[string]string
}

func slNewAPI(t *testing.T) *slAPI {
	f := &slAPI{calls: map[string][]url.Values{}, reply: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		f.mu.Lock()
		f.calls[method] = append(f.calls[method], r.Form)
		body, ok := f.reply[method]
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			body = `{"ok":true}`
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *slAPI) set(method, body string) { f.mu.Lock(); f.reply[method] = body; f.mu.Unlock() }
func (f *slAPI) called(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls[method])
}

// ---- recording bridge: which root renders which Slack component ----

type slRec struct {
	mu    sync.Mutex
	names map[uint64]string
	trees map[uint64][]byte
}

type slBridge struct {
	*taut.Bridge
	rec *slRec
}

func (b slBridge) Render(root uint64, name string, props []byte) {
	b.rec.mu.Lock()
	b.rec.names[root] = name
	b.rec.mu.Unlock()
	b.Bridge.Render(root, name, props)
}

// rendered reports whether a boundary named name was mounted.
func (r *slRec) rendered(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range r.names {
		if n == name {
			return true
		}
	}
	return false
}

// trees returns the host trees delivered for roots rendering name.
func (r *slRec) treesOf(name string) [][]*slackc.Node {
	r.mu.Lock()
	var raws [][]byte
	for root, n := range r.names {
		if n == name && r.trees[root] != nil {
			raws = append(raws, r.trees[root])
		}
	}
	r.mu.Unlock()
	var out [][]*slackc.Node
	for _, raw := range raws {
		if nodes, err := slackc.ParseTree(raw); err == nil {
			out = append(out, nodes)
		}
	}
	return out
}

// dump is what the trees of name show: text, ".class", "@Component" and
// [attr=value] for aria-label, title, data-qa and role, element props
// included.
func (r *slRec) dump(name string) string {
	var b strings.Builder
	for _, nodes := range r.treesOf(name) {
		slDumpNodes(&b, nodes)
		b.WriteString("\n")
	}
	return b.String()
}

func slDumpNodes(b *strings.Builder, nodes []*slackc.Node) {
	for _, n := range nodes {
		switch {
		case n.IsText():
			b.WriteString(n.V)
			b.WriteByte(' ')
		case n.IsComponent():
			fmt.Fprintf(b, "@%s ", n.N)
		default:
			for _, c := range n.Class {
				fmt.Fprintf(b, ".%s ", c)
			}
		}
		for _, a := range []string{"aria-label", "title", "data-qa", "role"} {
			if v, ok := n.AttrString(a); ok && v != "" {
				fmt.Fprintf(b, "[%s=%s] ", a, v)
			}
		}
		for _, v := range n.P {
			slDumpValue(b, v)
		}
		slDumpNodes(b, n.C)
	}
}

func slDumpValue(b *strings.Builder, v any) {
	switch v := v.(type) {
	case slackc.Element:
		slDumpNodes(b, v.Nodes)
	case slackc.Props:
		for _, x := range v {
			slDumpValue(b, x)
		}
	case map[string]any:
		for _, x := range v {
			slDumpValue(b, x)
		}
	case []any:
		for _, x := range v {
			slDumpValue(b, x)
		}
	}
}

// slFind is the first "@" node named name in nodes (children and element
// props, depth first).
func slFind(nodes []*slackc.Node, name string) *slackc.Node {
	for _, n := range nodes {
		if n.IsComponent() && n.N == name {
			return n
		}
		if f := slFind(n.C, name); f != nil {
			return f
		}
		for _, v := range n.P {
			if e, ok := v.(slackc.Element); ok {
				if f := slFind(e.Nodes, name); f != nil {
					return f
				}
			}
		}
	}
	return nil
}

// ---- workspace ----

func slSeed(s *store.Store) {
	s.Lock()
	s.TeamID, s.TeamName, s.TeamDomain = slTeam, "Synthetic Club", "hackclub"
	s.Unlock()
	s.PutUsers([]slack.User{
		{ID: slSelf, Name: "me", RealName: "Test Self", TZ: "America/New_York", Profile: slack.Profile{DisplayName: "self", RealName: "Test Self"}},
		{ID: slAlice, Name: "alice", RealName: "Alice Example", TZ: "Europe/London", Profile: slack.Profile{DisplayName: "alice", RealName: "Alice Example", Pronouns: "she/her"}},
		{ID: slBob, Name: "bob", RealName: "Bob Example", TZ: "America/Chicago", Profile: slack.Profile{DisplayName: "bob", RealName: "Bob Example"}},
		{ID: slApp, Name: "helperbot", RealName: "Helper Bot", IsBot: true, Profile: slack.Profile{DisplayName: "Helper Bot", RealName: "Helper Bot", BotID: "B0HELPER01", APIAppID: slAppID}},
	})
	s.PutConv(slack.Conversation{ID: slGen, Name: "general", IsChannel: true, IsMember: true, IsGeneral: true, NumMembers: 4})
	s.PutConv(slack.Conversation{ID: slDMA, IsIM: true, User: slAlice, IsMember: true})
	s.PutConv(slack.Conversation{ID: slDMB, IsIM: true, User: slBob, IsMember: true})
	s.SetLatest(slGen, []slack.Message{
		{Type: "message", TS: "1700000001.000100", User: slAlice, Text: "first"},
		{Type: "message", TS: slMsgTS, User: slBob, Text: "a synthetic message"},
	}, false)
}

type slEnv struct {
	h   *tzHost
	tt  *taut.Taut
	s   *slick.Slick
	api *slAPI
	rec *slRec
	dir string
}

func slPluginsDir(t *testing.T) string {
	dir := os.Getenv("SLICK_PLUGINS_DIR")
	if dir == "" {
		t.Skip("SLICK_PLUGINS_DIR not set: the ui-slick workflow builds Slick's plugins and runs this")
	}
	return dir
}

func slNewEnv(t *testing.T) *slEnv {
	t.Helper()
	dir := slPluginsDir(t)
	api := slNewAPI(t)
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test", UserID: slSelf, Team: slTeam, EnterpriseID: slEnt,
		TeamURL: api.srv.URL + "/", EnterpriseURL: api.srv.URL + "/"})
	c.EdgeURL = api.srv.URL + "/edge/"
	st := store.New("")
	slSeed(st)
	a := core.New(c, st)
	if u, ok := st.User(slSelf); ok {
		a.S.Self = *u
	}
	a.ServeOffline()
	noNet := func(context.Context, string) ([]byte, error) { return nil, errors.New("no network in tests") }
	h := &tzHost{a: a, th: theme.New(false), trees: map[uint64][]byte{}, im: imgcache.New(noNet, "", nil)}
	rec := &slRec{names: map[uint64]string{}, trees: map[uint64][]byte{}}
	tdir := t.TempDir()
	tt, err := taut.New(taut.Options{
		App: a, Dir: tdir, CredsPath: filepath.Join(tdir, "creds.json"), NoAutoStart: true, CacheDir: t.TempDir(),
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
			Attached:   func(b *taut.Bridge) { slackc.SetBridge(slBridge{b, rec}) },
			SetPatched: slackc.SetPatched,
			CallGo:     slackc.CallGo,
			Deliver: func(root uint64, tree []byte) {
				rec.mu.Lock()
				rec.trees[root] = append([]byte(nil), tree...)
				rec.mu.Unlock()
				_ = slackc.Deliver(root, tree)
			},
			SetStyle:    slackc.SetStyle,
			RemoveStyle: slackc.RemoveStyle,
			Overlays:    func(o []taut.Overlay) { h.mu.Lock(); h.overlays = o; h.mu.Unlock() },
			Toast:       func(text, icon string, d time.Duration) { h.Toast(text) },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := slick.New(slick.Options{Taut: tt, Dir: filepath.Join(tdir, "slick"), NoAutoStart: true, NoUpdates: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		SetTaut(nil)
		slackc.Reset()
		slackc.SetBridge(nil)
		_ = s.Close()
		_ = tt.Close()
	})
	SetTaut(tt)
	return &slEnv{h: h, tt: tt, s: s, api: api, rec: rec, dir: dir}
}

func (e *slEnv) ctx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// load installs a built Slick plugin (and its main half) as a user does,
// applies settings, enables it and checks it started.
func (e *slEnv) load(t *testing.T, id string, settings map[string]any) {
	t.Helper()
	for _, f := range []string{id + ".js", id + ".main.js"} {
		code, err := os.ReadFile(filepath.Join(e.dir, f))
		if err != nil {
			if f == id+".js" {
				t.Fatal(err)
			}
			continue
		}
		if _, err := e.s.Import(e.ctx(t), code); err != nil {
			t.Fatalf("import %s: %v", f, err)
		}
	}
	for k, v := range settings {
		if err := e.s.SetOption(e.ctx(t), id, k, v); err != nil {
			t.Fatalf("%s: set %s: %v", id, k, err)
		}
	}
	if err := e.s.SetEnabled(e.ctx(t), id, true); err != nil {
		t.Fatalf("enable %s: %v", id, err)
	}
	for _, p := range e.s.Settings().Plugins {
		if p.ID == id && (!p.Running || p.StartError != "") {
			t.Fatalf("%s didn't start: running=%v error=%q", id, p.Running, p.StartError)
		}
	}
}

func (e *slEnv) user(t *testing.T, id string) core.User {
	t.Helper()
	u, ok := lookupUser(e.h, id)
	if !ok {
		t.Fatalf("no user %s", id)
	}
	return u
}

// slFrame lays w out like a window frame (1200×800 at 1 px/dp).
func slFrame(w interface {
	Layout(gtx layout.Context) layout.Dimensions
}) func() {
	ops := new(op.Ops)
	return func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1200, 800)), Now: time.Now()}
		slackc.Frame(gtx)
		w.Layout(gtx)
	}
}

// slUntil draws frames (and runs popups' and slackc's queues) until cond.
func slUntil(t *testing.T, d time.Duration, what string, frame func(), cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if frame != nil {
			frame()
		}
		Pump()
		slackc.Pump()
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func slPatched(t *testing.T, names ...string) {
	t.Helper()
	slUntil(t, 10*time.Second, "patches on "+strings.Join(names, ", "), nil, func() bool {
		for _, n := range names {
			if !slackc.Patched(n) {
				return false
			}
		}
		return true
	})
}

func slIndex(items []MenuItem, key string) int {
	for i, it := range items {
		if it.Key == key {
			return i
		}
	}
	return -1
}

// ---- plugins ----

// LastSeen: "Last seen …" on the profile card (inside Slack's LocalTime,
// in ProfileHoverCard's context) and in the full profile's presence row
// (Presence inside RimetoProfilePresence, its own text hidden).
func TestSlickViewLastSeen(t *testing.T) {
	e := slNewEnv(t)
	seen := time.Now().Add(-5 * time.Minute)
	e.api.set("search.messages", fmt.Sprintf(`{"ok":true,"messages":{"total":1,"matches":[{"ts":"%d.000100","channel":{"id":%q}}]}}`, seen.Unix(), slGen))
	e.load(t, "LastSeen", nil)
	slPatched(t, "ProfileHoverCard", "Presence", "RimetoProfilePresence", "LocalTime")

	card := NewProfileCard(e.h, image.Pt(200, 120), slAlice)
	slUntil(t, 15*time.Second, "Last seen on the profile card", slFrame(card), func() bool {
		d := e.rec.dump("ProfileHoverCard")
		return strings.Contains(d, "Last seen") && strings.Contains(d, ".slick-last-seen__card")
	})
	if e.api.called("search.messages") == 0 {
		t.Error("LastSeen never looked up the last message")
	}
	d := e.rec.dump("ProfileHoverCard")
	for _, want := range []string{"@MemberProfileHoverCard", "@MemberProfileBaseEntity", "@LocalTime"} {
		if !strings.Contains(d, want) {
			t.Errorf("card tree lacks %s: %s", want, d)
		}
	}

	v := NewProfileView(e.h, slAlice)
	slUntil(t, 15*time.Second, "Last seen in the full profile's presence row", slFrame(v), func() bool {
		d := e.rec.dump("RimetoProfilePresence")
		return strings.Contains(d, "Last seen") && strings.Contains(d, ".slick-last-seen")
	})
	// the plugin drew Presence without its text ("Away" would sit beside "Last seen")
	for _, nodes := range e.rec.treesOf("RimetoProfilePresence") {
		if n := slFind(nodes, "Presence"); n != nil && n.P.Has("showText") && n.P.Bool("showText") {
			t.Errorf("Presence still shows its text under LastSeen: %v", n.P)
		}
	}
}

// LocalTime: the card's base entity gets "<time> local time" and Slack's
// LocalTime line goes away; the full profile's LocalTimeRow draws the
// plugin's clock row.
func TestSlickViewLocalTime(t *testing.T) {
	e := slNewEnv(t)
	e.load(t, "LocalTime", nil)
	slPatched(t, "MemberProfileHoverCard", "MemberProfileBaseEntity", "LocalTimeRow", "LocalTime")

	card := NewProfileCard(e.h, image.Pt(200, 120), slAlice)
	slUntil(t, 15*time.Second, "the card's local time line", slFrame(card), func() bool {
		d := e.rec.dump("MemberProfileHoverCard")
		return strings.Contains(d, "local time") && strings.Contains(d, ".slick-local-time__card--hover")
	})
	for _, nodes := range e.rec.treesOf("MemberProfileHoverCard") {
		if n := slFind(nodes, "LocalTime"); n != nil {
			t.Errorf("Slack's LocalTime still renders in the card under the plugin")
		}
	}

	v := NewProfileView(e.h, slAlice)
	slUntil(t, 15*time.Second, "the full profile's local time row", slFrame(v), func() bool {
		d := e.rec.dump("LocalTimeRow")
		return strings.Contains(d, "local time") && strings.Contains(d, ".slick-local-time__card--profile")
	})
}

// Nicknames: "Set nickname…" in a member's ⋮ menu (after Copy display
// name) and an app's ⋮ menu (last), the nickname modal, and Edit profile
// going through openEditProfileModal with EditProfileFormPane mounted.
func TestSlickViewNicknames(t *testing.T) {
	e := slNewEnv(t)
	e.load(t, "Nicknames", nil)
	slPatched(t, "RimetoMemberProfileOverflowMenu", "MenuFromTemplate", "AppDetailsOverflow", "EditProfileFormPane")

	alice := e.user(t, slAlice)
	m := NewMenuOpts(e.h, pfMoreItems(e.h, alice, false, nil), pfMenuOpts(slAlice, MenuOpts{Anchor: image.Rect(600, 100, 628, 128), AlignRight: true}))
	slUntil(t, 15*time.Second, "Set nickname… in the member's ⋮ menu", slFrame(m), func() bool {
		return slIndex(m.items, "slick-nicknames__set") >= 0
	})
	i := slIndex(m.items, "slick-nicknames__set")
	set := m.items[i]
	if !strings.HasPrefix(set.Label, "Set nickname") {
		t.Fatalf("nickname row %+v", set)
	}
	if i == 0 || !strings.HasPrefix(m.items[i-1].Label, "Copy display name") {
		t.Errorf("Set nickname… isn't right after Copy display name: %+v", m.items)
	}
	if set.Do == nil {
		t.Fatal("Set nickname… has no action: " + slSlotFnGap)
	} else {
		set.Do()
		slUntil(t, 10*time.Second, "the nickname modal", nil, func() bool {
			for _, o := range e.h.pluginOverlays() {
				if o.Kind == "modal" && o.Root != 0 {
					return true
				}
			}
			return false
		})
	}

	app := e.user(t, slApp)
	if got := pfAppID(e.h, app); got != slAppID {
		t.Fatalf("app id %q", got)
	}
	am := NewMenuOpts(e.h, pfMoreItems(e.h, app, false, nil), pfAppMenuOpts(slAppID, slApp, MenuOpts{Anchor: image.Rect(600, 100, 628, 128)}))
	slUntil(t, 15*time.Second, "Set nickname… in the app's ⋮ menu", slFrame(am), func() bool {
		return slIndex(am.items, "slick-nicknames__set") == len(am.items)-1
	})

	// Edit profile dispatches openEditProfileModal through the plugin; the
	// native (Clack's stand-in toast) runs under it
	slUntil(t, 10*time.Second, "openEditProfileModal patched", nil, func() bool { return e.tt.ThunkPatched("openEditProfileModal") })
	pfEditProfile(e.h)
	slUntil(t, 10*time.Second, "the edit-profile toast via the thunk", nil, func() bool {
		for _, s := range e.h.toastList() {
			if s == pfEditToast {
				return true
			}
		}
		return false
	})
	v := NewProfileView(e.h, slSelf)
	v.editUntil = time.Now().Add(pfEditPaneFor)
	slUntil(t, 10*time.Second, "EditProfileFormPane mounted while editing", slFrame(v), func() bool {
		return e.rec.rendered("EditProfileFormPane")
	})
}

// MessageLogger: after a message is deleted, its ⋮ menu (Menu inside
// MessageActionsMenu) gets "Accept deletion".
func TestSlickViewMessageLogger(t *testing.T) {
	e := slNewEnv(t)
	e.load(t, "MessageLogger", nil)
	slPatched(t, "MessageActionsMenu", "Menu")
	slUntil(t, 10*time.Second, "MessageLogger listens for deletes", nil, func() bool {
		return e.tt.Started()
	})
	time.Sleep(300 * time.Millisecond) // its RTM listeners
	raw, _ := json.Marshal(map[string]any{
		"type": "message", "subtype": "message_deleted", "hidden": true, "channel": slGen,
		"ts": fmt.Sprintf("%d.000900", time.Now().Unix()), "deleted_ts": slMsgTS, "event_ts": fmt.Sprintf("%d.000900", time.Now().Unix()),
		"previous_message": map[string]any{"type": "message", "user": slBob, "text": "a synthetic message", "ts": slMsgTS},
	})
	e.h.a.Inject(slack.Event{Type: "message", Channel: json.RawMessage(`"` + slGen + `"`), Raw: raw})

	msg := core.Message{Channel: slGen, TS: slMsgTS, User: slBob, Text: "a synthetic message"}
	m := NewMessageMenu(e.h, image.Pt(900, 200), msg)
	slUntil(t, 15*time.Second, "Accept deletion in the message's ⋮ menu", slFrame(m), func() bool {
		d := e.rec.dump("MessageActionsMenu")
		return strings.Contains(d, "Accept deletion") && strings.Contains(d, "[data-qa=slick_ml_accept_delete]") && slIndex(m.items, "plugin-row") >= 0
	})
	if i := slIndex(m.items, "plugin-row"); i < len(m.items)-1 {
		t.Errorf("the plugin row isn't after the native rows: %d of %d", i, len(m.items))
	}
	if slIndex(m.items, "mark_unread") < 0 {
		t.Errorf("native rows gone: %+v", m.items)
	}
}

// AccountSwitcher: "Switch account" (with "Add another account") right
// before Sign out in the user menu.
func TestSlickViewAccountSwitcher(t *testing.T) {
	e := slNewEnv(t)
	e.load(t, "AccountSwitcher", nil)
	slPatched(t, "MenuFromTemplate")
	m := NewUserMenu(e.h, image.Pt(40, 760))
	slUntil(t, 15*time.Second, "Switch account in the user menu", slFrame(m), func() bool {
		return slIndex(m.items, "slick-account-switcher") >= 0
	})
	i := slIndex(m.items, "slick-account-switcher")
	sw := m.items[i]
	if sw.Label != "Switch account" || len(sw.Sub) == 0 {
		t.Fatalf("switch row %+v", sw)
	}
	if j := slIndex(m.items, "sign-out"); j != i+1 {
		t.Errorf("Switch account at %d, Sign out at %d", i, j)
	}
	add := slIndex(sw.Sub, "slick-account-switcher__add")
	if add < 0 || sw.Sub[add].Label != "Add another account" || sw.Sub[add].Do == nil {
		t.Errorf("submenu %+v", sw.Sub)
	}
}

// BetterBlock: a blocked person's card is covered by "You blocked this
// person", the ⋮ menu offers Block/Unblock, and search drops them.
func TestSlickViewBetterBlock(t *testing.T) {
	e := slNewEnv(t)
	e.load(t, "BetterBlock", map[string]any{"blockedUsers": map[string]any{slAlice: "Alice Example"}})
	slPatched(t, "ProfileHoverCard", "SearchAutocomplete", "SearchInputBox", "MenuFromTemplate")

	card := NewProfileCard(e.h, image.Pt(200, 120), slAlice)
	slUntil(t, 15*time.Second, "the blocked card", slFrame(card), func() bool {
		d := e.rec.dump("ProfileHoverCard")
		return strings.Contains(d, "You blocked this person") && strings.Contains(d, ".slick-betterblock-profile")
	})

	for _, c := range []struct{ id, want string }{{slBob, "Block "}, {slAlice, "Unblock "}} {
		u := e.user(t, c.id)
		m := NewMenuOpts(e.h, pfMoreItems(e.h, u, false, nil), pfMenuOpts(c.id, MenuOpts{Anchor: image.Rect(600, 100, 628, 128)}))
		slUntil(t, 15*time.Second, c.want+"in "+c.id+"'s ⋮ menu", slFrame(m), func() bool {
			i := slIndex(m.items, "slick-betterblock-toggle")
			return i >= 0 && strings.HasPrefix(m.items[i].Label, c.want)
		})
	}

	// message suggestions from search.inline (the popover's Recent
	// messages): one by Alice (blocked), one by Bob, one by an app (its
	// bot user in bot_profile)
	const aliceTS, bobTS, botTS = "1700000101.000100", "1700000102.000100", "1700000103.000100"
	ext := func(s string) string {
		return `"extracts":[{"text":"` + s + `","truncated_head":false,"truncated_tail":false}]`
	}
	e.api.set("search.inline", fmt.Sprintf(`{"ok":true,"query":"example","pagination":{"total_count":3,"page":1,"per_page":3,"page_count":1},"items":[`+
		`{"ts":%q,"user":%q,"channel_id":%q,"iid":"I1",%s},`+
		`{"ts":%q,"user":%q,"channel_id":%q,"iid":"I2",%s},`+
		`{"ts":%q,"bot_id":"B0HELPER01","channel_id":%q,"iid":"I3",%s,`+
		`"bot_profile":{"id":"B0HELPER01","name":"Helper Bot","app_id":%q,"user_id":%q}}]}`,
		aliceTS, slAlice, slGen, ext("example from alice"), bobTS, slBob, slGen, ext("example from bob"),
		botTS, slGen, ext("example from the bot"), slAppID, slApp))
	msgRow := func(rows []srRow, ts string) *srRow {
		for i := range rows {
			if rows[i].kind == srRMsg && rows[i].msg != nil && rows[i].msg.TS == ts {
				return &rows[i]
			}
		}
		return nil
	}

	p := NewSearchPopover(e.h, "example", image.Rectangle{})
	f := slFrame(p)
	slUntil(t, 15*time.Second, "the message suggestions among the search rows", f, func() bool {
		return msgRow(p.full, aliceTS) != nil && msgRow(p.full, bobTS) != nil && msgRow(p.full, botTS) != nil
	})
	// the author rides on each message suggestion, as Slack's do
	for _, c := range []struct{ ts, user, bot string }{{aliceTS, slAlice, ""}, {bobTS, slBob, ""}, {botTS, slApp, "B0HELPER01"}} {
		r := msgRow(p.full, c.ts)
		sg := p.slickSuggestion(r, r.key())
		if got := sg.StrOr("user", ""); got != c.user {
			t.Errorf("message %s suggestion: user %q, want %q", c.ts, got, c.user)
		}
		if got := sg.StrOr("bot_id", ""); got != c.bot {
			t.Errorf("message %s suggestion: bot_id %q, want %q", c.ts, got, c.bot)
		}
	}
	slUntil(t, 15*time.Second, "Alice's row among the search rows", f, func() bool {
		for i := range p.full {
			if p.full[i].kind == srRUser && p.full[i].id == slAlice {
				return true
			}
		}
		return false
	})
	slUntil(t, 15*time.Second, "Alice dropped from the search list", f, func() bool {
		if !e.rec.rendered("SearchAutocomplete") {
			return false
		}
		for i := range p.rows {
			if p.rows[i].kind == srRUser && p.rows[i].id == slAlice {
				return false
			}
		}
		return len(p.hide) > 0
	})
	for i := range p.full {
		r := &p.full[i]
		if r.kind == srRUser && r.id == slBob && p.hide[r.key()] {
			t.Errorf("Bob (not blocked) hidden too")
		}
	}
	// Alice's message drops out of the dropdown; Bob's and the app's stay
	slUntil(t, 15*time.Second, "Alice's message dropped from the search list", f, func() bool {
		return msgRow(p.rows, aliceTS) == nil
	})
	if r := msgRow(p.full, aliceTS); !p.hide[r.key()] {
		t.Error("Alice's message isn't among the rows BetterBlock dropped")
	}
	for _, ts := range []string{bobTS, botTS} {
		if msgRow(p.rows, ts) == nil {
			t.Errorf("message %s (not blocked) hidden too", ts)
		}
	}
	if p.sel >= 0 && p.sel < len(p.rows) && p.rows[p.sel].kind == srRUser && p.rows[p.sel].id == slAlice {
		t.Error("the selection is on the blocked row")
	}
}

// BetterGifs: Giphy items in Clack's GIF grid get the favourite star
// (GifListItem); the picker body is the GifPicker boundary.
func TestSlickViewBetterGifs(t *testing.T) {
	gifAnims.mu.Lock()
	prev := gifAnims.fetch
	gifAnims.fetch = func(context.Context, string) ([]byte, error) { return nil, errors.New("no network in tests") }
	gifAnims.mu.Unlock()
	t.Cleanup(func() { gifAnims.mu.Lock(); gifAnims.fetch = prev; gifAnims.mu.Unlock() })

	e := slNewEnv(t)
	e.load(t, "BetterGifs", nil)
	slPatched(t, "GifListItem")
	p := NewEmojiPickerAt(e.h, image.Pt(400, 700), nil)
	p.setGifMode(true)
	g := &p.gif
	g.started = true // synthetic results, no fetch
	g.items = []Gif{
		{ID: "synthGif01", Title: "synthetic party", URL: "https://media1.giphy.com/media/synthGif01/giphy.webp",
			Preview: "https://media1.giphy.com/media/synthGif01/200w.webp", Still: "https://media1.giphy.com/media/synthGif01/200w_s.gif", W: 480, H: 270},
		{ID: "synthGif02", Title: "synthetic wave", URL: "https://media2.giphy.com/media/synthGif02/giphy.webp",
			Preview: "https://media2.giphy.com/media/synthGif02/200w.webp", Still: "https://media2.giphy.com/media/synthGif02/200w_s.gif", W: 300, H: 300},
	}
	g.tiles = make([]widget.Clickable, len(g.items))
	slUntil(t, 15*time.Second, "the favourite star on the GIF tiles", slFrame(p), func() bool {
		d := e.rec.dump("GifListItem")
		return strings.Contains(d, ".slick-bg__native-card") && strings.Contains(d, "[aria-label=Favorite synthetic party]") &&
			strings.Contains(d, ".slick-bg__star") && strings.Contains(d, "@GifListItem")
	})
	if slackc.Patched("GifPicker") {
		slUntil(t, 10*time.Second, "BetterGifs' picker", slFrame(p), func() bool {
			return strings.Contains(e.rec.dump("GifPicker"), "[data-qa=slick_better_gifs]")
		})
	} else {
		t.Error("GifPicker isn't patched: BetterGifs' {filter: isGifPicker} must match the native's real source (internal/taut REAL_SOURCES)")
	}
	// The tab row with BetterGifs installed (it patches the GIF tab's body,
	// not the row): light and dark, on either tab, next to the plain rows
	// TestEmojiTabRow draws (icon-shots). Needs a GPU; skipped without.
	if os.Getenv("SLICK_SHOTS_DIR") != "" {
		th := e.h.th
		dark0 := th.Dark
		defer th.SetDark(dark0)
		for _, dark := range []bool{false, true} {
			th.SetDark(dark)
			for _, gif := range []bool{false, true} {
				p.gifMode = gif
				name := fmt.Sprintf("emoji-tabs-bettergifs-%s-%s", map[bool]string{false: "light", true: "dark"}[dark], map[bool]string{false: "emoji", true: "gifs"}[gif])
				epSaveShot(t, os.Getenv("SLICK_SHOTS_DIR"), name, epTabRowShot(t, th, p, 2))
			}
		}
	}
}

// ChatTabs: "Open in new Slick tab" in the channel menu (its getTemplate
// through ContextMenuTriggerWithKeyboardFocus inside ChannelContextMenu),
// and its wrapper around BaseSearch.
func TestSlickViewChatTabs(t *testing.T) {
	e := slNewEnv(t)
	e.load(t, "ChatTabs", nil)
	slPatched(t, "ChannelContextMenu", "ContextMenuTriggerWithKeyboardFocus", "BaseSearch")
	m := NewChannelMenu(e.h, image.Pt(900, 60), slGen)
	f := slFrame(m)
	slUntil(t, 10*time.Second, "the channel menu inside ChannelContextMenu > ContextMenuTriggerWithKeyboardFocus", f, func() bool {
		d := e.rec.dump("ChannelContextMenu")
		return strings.Contains(d, "@ContextMenuTriggerWithKeyboardFocus") && strings.Contains(d, "@MenuFromTemplate")
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && slIndex(m.items, "slick-chat-tabs-open") < 0 {
		f()
		Pump()
		slackc.Pump()
		time.Sleep(20 * time.Millisecond)
	}
	if i := slIndex(m.items, "slick-chat-tabs-open"); i >= 0 {
		if m.items[i].Label != "Open in new Slick tab" {
			t.Errorf("tab row %+v", m.items[i])
		}
	} else {
		t.Error("no Open in new Slick tab row: " + slSlotFnGap)
	}
	if len(m.items) < 3 {
		t.Errorf("native rows gone: %+v", m.items)
	}
	p := NewSearchPopover(e.h, "", image.Rectangle{})
	slUntil(t, 15*time.Second, "ChatTabs' search wrapper", slFrame(p), func() bool {
		d := e.rec.dump("BaseSearch")
		return strings.Contains(d, ".slick-chat-tabs-search") && strings.Contains(d, "@BaseSearch")
	})
}

// InstalledApps: its block in a person's full profile (Content > the
// profile flexpane > member_profile_pane), which can't load offline, and
// its wrapper around an app's AboutPage.
func TestSlickViewInstalledApps(t *testing.T) {
	e := slNewEnv(t)
	e.load(t, "InstalledApps", nil)
	slPatched(t, "Content", "AboutPage")
	v := NewProfileView(e.h, slAlice)
	slUntil(t, 15*time.Second, "the installed-apps block in the full profile", slFrame(v), func() bool {
		d := e.rec.dump("Content")
		return strings.Contains(d, ".slick-installed-apps") && strings.Contains(d, "Installed apps unavailable for this session.")
	})
	av := NewProfileView(e.h, slApp)
	slUntil(t, 15*time.Second, "the app's AboutPage wrapper", slFrame(av), func() bool {
		return strings.Contains(e.rec.dump("AboutPage"), ".slick-about-scroll-wrapper")
	})
	d := e.rec.dump("Content")
	if !strings.Contains(d, "@AppDetailsOverflow") {
		t.Errorf("the app profile's tree has no AppDetailsOverflow: %s", d)
	}
}

// slSlotFnGap: jsrt's nativeProps (internal/jsrt/js/dom.js) returns a
// props object with an own $slot as is ("already host-tree shaped"), so a
// slot component's function props are dropped by JSON: a click a plugin
// adds to a template inside a Within slot (Nicknames, BetterBlock), or a
// getTemplate it wraps on a slot (ChatTabs), never reaches Go. The check
// becomes strict once that test only applies below the top level.
const slSlotFnGap = "function props a plugin sets on a slot component must reach Go (internal/jsrt dom.js nativeProps)"

type slNoNet struct{}

func (slNoNet) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("no network in tests")
}
