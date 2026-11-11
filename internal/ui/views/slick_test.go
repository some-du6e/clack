package views

// Slick plugins through the native views (the ui-slick workflow, -run
// TestSlickView): each test installs a real plugin from Slick's own build
// (SLICK_PLUGINS_DIR; skipped when unset), unmodified, over Taut's
// runtime, draws the real Activity, DMs, Directories and Huddles views
// over a synthetic workspace (made-up people and messages, a fake Web
// API, no network) and checks what a user would see.

import (
	"context"
	"encoding/json"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/slick"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	sSelf    = "USELF00001"
	sAlice   = "UALICE0001" // the person BetterBlock blocks
	sBob     = "UBOB000001"
	sGen     = "C0GENERAL1"
	sDMAlice = "D0ALICE001"
	sDMBob   = "D0BOB00001"
	sRoom    = "R0HUDDLE01"

	tsAliceMention = "1700000001.000100"
	tsBobMention   = "1700000002.000200"
	tsMine         = "1700000003.000300"
	tsRoot         = "1700000004.000400"
	tsBobReply     = "1700000005.000500"
	tsAliceReply   = "1700000006.000600"
	tsAliceDM      = "1700000010.000100"
	tsBobDM        = "1700000011.000100"
)

// ---- the synthetic workspace and fake Web API ----

type slickAPI struct {
	srv   *httptest.Server
	mu    sync.Mutex
	reply map[string]any
	calls []string
}

func newSlickAPI(t *testing.T) *slickAPI {
	f := &slickAPI{reply: map[string]any{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(8 << 20)
		m := strings.TrimPrefix(r.URL.Path, "/api/")
		f.mu.Lock()
		f.calls = append(f.calls, m)
		rep, ok := f.reply[m]
		f.mu.Unlock()
		if !ok {
			rep = map[string]any{"ok": true}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rep)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func sUser(id, name, real string) slack.User {
	return slack.User{ID: id, Name: name, RealName: real, Profile: slack.Profile{DisplayName: name, RealName: real, Title: "Synthetic tester"}}
}

var sUsers = []slack.User{
	sUser(sSelf, "self", "Test Self"),
	sUser(sAlice, "alice", "Alice Example"),
	sUser(sBob, "bob", "Bob Example"),
}

func sMsg(ts, user, text string) slack.Message {
	return slack.Message{Type: "message", TS: ts, User: user, Text: text}
}

func seedSlick(s *store.Store) {
	s.Lock()
	s.TeamID, s.TeamName, s.TeamDomain = "T0266FRGM", "Synthetic Club", "synthetic"
	s.Unlock()
	s.PutUsers(sUsers)
	s.PutConv(slack.Conversation{ID: sGen, Name: "general", IsChannel: true, IsMember: true, NumMembers: 3})
	s.PutConv(slack.Conversation{ID: sDMAlice, IsIM: true, User: sAlice, IsMember: true})
	s.PutConv(slack.Conversation{ID: sDMBob, IsIM: true, User: sBob, IsMember: true})
	root := sMsg(tsRoot, sSelf, "a synthetic thread")
	root.ReplyCount, root.ReplyUsers, root.LatestReply = 2, []string{sBob, sAlice}, tsAliceReply
	root.ThreadTS = tsRoot
	s.SetLatest(sGen, []slack.Message{
		sMsg(tsAliceMention, sAlice, "<@"+sSelf+"> made-up mention one"),
		sMsg(tsBobMention, sBob, "<@"+sSelf+"> made-up mention two"),
		sMsg(tsMine, sSelf, "my synthetic message"),
		root,
	}, false)
	br, ar := sMsg(tsBobReply, sBob, "bob's synthetic reply"), sMsg(tsAliceReply, sAlice, "alice's synthetic reply")
	br.ThreadTS, ar.ThreadTS = tsRoot, tsRoot
	s.SetThread(sGen, tsRoot, []slack.Message{root, br, ar})
	s.SetLatest(sDMAlice, []slack.Message{sMsg(tsAliceDM, sAlice, "synthetic hi from alice")}, false)
	s.SetLatest(sDMBob, []slack.Message{sMsg(tsBobDM, sBob, "synthetic hi from bob")}, false)
}

func feedItem(key, typ, ch, ts, author string) map[string]any {
	return map[string]any{"is_unread": true, "feed_ts": ts, "key": key,
		"item": map[string]any{"type": typ, "message": map[string]any{"ts": ts, "channel": ch, "author_user_id": author}}}
}

func (f *slickAPI) seedReplies() {
	thread := map[string]any{"is_unread": true, "feed_ts": tsAliceReply, "key": "k-thread",
		"item": map[string]any{"type": "thread_v2", "bundle_info": map[string]any{"unread_count": 2,
			"payload": map[string]any{"thread_entry": map[string]any{"channel_id": sGen, "latest_ts": tsAliceReply,
				"thread_ts": tsRoot, "unread_msg_count": 2}}}}}
	react := map[string]any{"is_unread": true, "feed_ts": "1700000020.000000", "key": "k-react",
		"item": map[string]any{"type": "message_reaction", "reaction": map[string]any{"user": sAlice, "name": "tada"},
			"message": map[string]any{"ts": tsMine, "channel": sGen, "author_user_id": sSelf}}}
	f.reply["activity.feed"] = map[string]any{"ok": true, "items": []any{
		feedItem("k-alice", "at_user", sGen, tsAliceMention, sAlice),
		feedItem("k-bob", "at_user", sGen, tsBobMention, sBob),
		thread, react,
		feedItem("k-dm-alice", "dm", sDMAlice, tsAliceDM, sAlice),
		feedItem("k-dm-bob", "dm", sDMBob, tsBobDM, sBob),
	}}
	f.reply["client.dms"] = map[string]any{"ok": true, "ims": []any{
		map[string]any{"id": sDMAlice, "message": sMsg(tsAliceDM, sAlice, "synthetic hi from alice"),
			"channel": map[string]any{"id": sDMAlice, "is_im": true, "user": sAlice}},
		map[string]any{"id": sDMBob, "message": sMsg(tsBobDM, sBob, "synthetic hi from bob"),
			"channel": map[string]any{"id": sDMBob, "is_im": true, "user": sBob}},
	}}
	f.reply["search.modules.people"] = map[string]any{"ok": true, "items": sUsers,
		"pagination": map[string]any{"total_count": len(sUsers), "page": 1, "page_count": 1}}
}

// slickHost is a views Host with Taut + Slick, recording toasts.
type slickHost struct {
	app *core.App
	th  *theme.Theme
	tt  *taut.Taut
	mu  sync.Mutex
	ts  []string
}

func (h *slickHost) Core() *core.App                           { return h.app }
func (h *slickHost) Theme() *theme.Theme                       { return h.th }
func (h *slickHost) Images() *imgcache.Cache                   { return nil }
func (h *slickHost) Invalidate()                               {}
func (h *slickHost) Navigate(string)                           {}
func (h *slickHost) OpenThread(string, string)                 {}
func (h *slickHost) CloseSecondary()                           {}
func (h *slickHost) OpenProfile(image.Point, string)           {}
func (h *slickHost) OpenMessageMenu(image.Point, core.Message) {}
func (h *slickHost) OpenLightbox(richtext.Image, core.Message) {}
func (h *slickHost) Taut() *taut.Taut                          { return h.tt }
func (h *slickHost) Toast(s string) {
	h.mu.Lock()
	h.ts = append(h.ts, s)
	h.mu.Unlock()
}

func (h *slickHost) toasts() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.ts...)
}

// slickUI routes Taut's UI hooks to slackc like the ui root, recording
// the trees and JS→Go calls.
type slickUI struct {
	mu     sync.Mutex
	trees  map[uint64][]byte
	callGo int
}

func (u *slickUI) hooks() taut.UIHooks {
	return taut.UIHooks{
		Attached: func(b *taut.Bridge) { slackc.SetBridge(b) },
		Deliver: func(root uint64, tree []byte) {
			u.mu.Lock()
			u.trees[root] = append([]byte(nil), tree...)
			u.mu.Unlock()
			_ = slackc.Deliver(root, tree)
		},
		SetPatched: slackc.SetPatched,
		CallGo: func(id int64, args []byte) ([]byte, error) {
			u.mu.Lock()
			u.callGo++
			u.mu.Unlock()
			return slackc.CallGo(id, args)
		},
		SetStyle:    slackc.SetStyle,
		RemoveStyle: slackc.RemoveStyle,
	}
}

func (u *slickUI) goCalls() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.callGo
}

// tree returns a delivered tree that has the native ("@") component name,
// decoded, and its JSON.
func (u *slickUI) tree(name string) ([]any, string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, tr := range u.trees {
		var nodes []any
		if json.Unmarshal(tr, &nodes) != nil {
			continue
		}
		if findAt(nodes, name, nil) != nil {
			return nodes, string(tr)
		}
	}
	return nil, ""
}

// findAt finds the native node name, returning the path of nodes from the
// root down to it.
func findAt(nodes []any, name string, path []map[string]any) []map[string]any {
	for _, x := range nodes {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		p := append(append([]map[string]any(nil), path...), m)
		if m["t"] == "@" && m["n"] == name {
			return p
		}
		if kids, ok := m["c"].([]any); ok {
			if f := findAt(kids, name, p); f != nil {
				return f
			}
		}
	}
	return nil
}

type slickEnv struct {
	t    *testing.T
	h    *slickHost
	api  *slickAPI
	ui   *slickUI
	s    *slick.Slick
	app  *core.App
	ops  op.Ops
	size image.Point
}

func pluginsDir(t *testing.T) string {
	dir := os.Getenv("SLICK_PLUGINS_DIR")
	if dir == "" {
		t.Skip("SLICK_PLUGINS_DIR not set (the ui-slick workflow builds Slick's plugins)")
	}
	return dir
}

func newSlickEnv(t *testing.T) *slickEnv {
	t.Helper()
	pluginsDir(t)
	api := newSlickAPI(t)
	api.seedReplies()
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test", UserID: sSelf, Team: "T0266FRGM", EnterpriseID: "E09V59WQY1E",
		TeamURL: api.srv.URL + "/", EnterpriseURL: api.srv.URL + "/"})
	c.EdgeURL = api.srv.URL + "/edge/"
	st := store.New("")
	seedSlick(st)
	app := core.New(c, st)
	if u, ok := st.User(sSelf); ok {
		app.S.Self = *u
	}
	app.ServeOffline()
	ui := &slickUI{trees: map[uint64][]byte{}}
	tdir := t.TempDir()
	tt, err := taut.New(taut.Options{App: app, Dir: filepath.Join(tdir, "taut"), CredsPath: filepath.Join(tdir, "creds.json"), UI: ui.hooks(),
		NoAutoStart: true, CacheDir: filepath.Join(tdir, "cache"),
		Log: func(level, msg string) {
			if level == "error" || level == "warn" {
				t.Logf("js %s: %s", level, msg)
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := slick.New(slick.Options{Taut: tt, Dir: filepath.Join(tdir, "slick"), NoAutoStart: true, NoUpdates: true})
	if err != nil {
		t.Fatal(err)
	}
	slackc.SetBridge(tt.Bridge())
	t.Cleanup(func() {
		_ = s.Close()
		_ = tt.Close()
		slackc.Reset()
		slackc.SetBridge(nil)
		slackc.SetPatched(nil)
	})
	return &slickEnv{t: t, h: &slickHost{app: app, th: theme.New(false), tt: tt}, api: api, ui: ui, s: s, app: app, size: image.Pt(1200, 800)}
}

func (e *slickEnv) ctx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	e.t.Cleanup(cancel)
	return ctx
}

// install imports the built plugin as a user does, applies settings, and
// enables it.
func (e *slickEnv) install(id string, settings map[string]any) {
	e.t.Helper()
	code, err := os.ReadFile(filepath.Join(pluginsDir(e.t), id+".js"))
	if err != nil {
		e.t.Fatal(err)
	}
	name, err := e.s.Import(e.ctx(), code) // returns the plugin's name
	if err != nil {
		e.t.Fatalf("import %s: %v", id, err)
	}
	e.t.Logf("installed %s (%s)", id, name)
	for k, v := range settings {
		if err := e.s.SetOption(e.ctx(), id, k, v); err != nil {
			e.t.Fatalf("set %s: %v", k, err)
		}
	}
	if err := e.s.SetEnabled(e.ctx(), id, true); err != nil {
		e.t.Fatalf("enable %s: %v", id, err)
	}
	time.Sleep(150 * time.Millisecond)
}

func (e *slickEnv) gtx() layout.Context {
	e.ops.Reset()
	return layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(e.size), Now: time.Now()}
}

// frame lays out w once (and runs queued JS→Go calls first, as the ui
// root's slackc.Frame does).
func (e *slickEnv) frame(w func(C) D) {
	gtx := e.gtx()
	slackc.Pump()
	w(gtx)
}

// until runs frames of w until cond holds.
func (e *slickEnv) until(what string, w func(C) D, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		e.frame(w)
		if cond() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	e.t.Fatalf("timed out waiting for %s", what)
}

func (e *slickEnv) patched(names ...string) {
	e.t.Helper()
	e.until("patches on "+strings.Join(names, ", "), func(C) D { return D{} }, func() bool {
		for _, n := range names {
			if !slackc.Patched(n) {
				return false
			}
		}
		return true
	})
}

// ---- Activity ----

func actPage(v *Activity) core.ActivityPage {
	p, _, _ := v.data.get()
	return p
}

// actRowH draws one Activity row as the list does and returns its height
// (0: hidden).
func (e *slickEnv) actRowH(v *Activity, key string) int {
	p := actPage(v)
	for i := range p.Items {
		if actKey(&p.Items[i]) == key {
			gtx := e.gtx()
			gtx.Constraints = layout.Constraints{Max: image.Pt(800, 400)}
			gtx.Constraints.Min.X = 800
			return v.row(gtx, &p.Items[i], 1).Size.Y
		}
	}
	e.t.Fatalf("no activity item %s (have %d)", key, len(p.Items))
	return 0
}

func TestSlickViewBetterBlockActivity(t *testing.T) {
	e := newSlickEnv(t)
	e.install("BetterBlock", map[string]any{"blockedUsers": map[string]any{sAlice: "alice"}})
	e.patched("ActivityItem", "ActivityThread", "ActivityDM", "ActivityRowContentMultiLine", "ActivityRowContentDense")
	v := NewActivity(e.h)
	e.until("the feed", v.Layout, func() bool { return len(actPage(v).Items) == 6 })

	// rows settle once each patched boundary's first tree arrives
	visible := func(key string) bool { return e.actRowH(v, key) > 0 }
	e.until("alice's rows hidden, bob's shown", v.Layout, func() bool {
		return !visible("k-alice") && !visible("k-react") && !visible("k-dm-alice") && visible("k-bob") && visible("k-dm-bob")
	})

	// the thread row stays, previewing bob's reply instead of alice's newer one
	bob, _ := v.authorOf(&core.Message{Channel: sGen, User: sBob})
	e.until("the thread projected onto bob's reply", v.Layout, func() bool {
		if !visible("k-thread") {
			return false
		}
		r := v.rows["k-thread"]
		return r != nil && r.msg() != nil && r.msg().TS == tsBobReply && r.cur.name == bob
	})

	// Dense rows: the SingleLine/Dense boundaries hide the same rows
	v.dense = true
	v.dirty = true
	e.until("dense rows filtered", v.Layout, func() bool { return !visible("k-alice") && visible("k-bob") })

	// unblocking (a live setting) brings alice back
	if err := e.s.SetOption(e.ctx(), "BetterBlock", "blockedUsers", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	e.until("alice's rows back", v.Layout, func() bool { return visible("k-alice") && visible("k-dm-alice") })
}

// ---- DMs ----

func (e *slickEnv) dmItemH(d *DMs, ch string) int {
	i, ok := d.index[ch]
	if !ok {
		e.t.Fatalf("no DM %s", ch)
	}
	r := d.rowState(ch)
	r.v, r.dm = d, &d.items[i]
	gtx := e.gtx()
	gtx.Constraints = layout.Constraints{Min: image.Pt(360, 0), Max: image.Pt(360, 400)}
	return slackc.Component(gtx, "DmsListItem", r, (*dmsRow).itemProps, (*dmsRow).layoutItem).Size.Y
}

func dmShown(d *DMs, ch string) bool {
	for _, en := range d.visible() {
		if en.dm >= 0 && d.items[en.dm].Channel == ch {
			return true
		}
	}
	return false
}

func TestSlickViewBetterBlockDMs(t *testing.T) {
	e := newSlickEnv(t)
	e.install("BetterBlock", map[string]any{"blockedUsers": map[string]any{sAlice: "alice"}})
	e.patched("DmsList", "DmsListItem", "DmsChannel")
	d := NewDMs(e.h)
	e.until("the DMs", d.Layout, func() bool { return len(d.items) == 2 })
	e.until("alice's DM gone from the list", d.Layout, func() bool { return !dmShown(d, sDMAlice) && dmShown(d, sDMBob) })
	// her row itself (DmsListItem / DmsChannel) renders nothing either
	e.until("alice's row empty", d.Layout, func() bool { return e.dmItemH(d, sDMAlice) == 0 && e.dmItemH(d, sDMBob) > 0 })
	if err := e.s.SetOption(e.ctx(), "BetterBlock", "blockedUsers", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	e.until("alice's DM back", d.Layout, func() bool { return dmShown(d, sDMAlice) && e.dmItemH(d, sDMAlice) > 0 })
}

// ---- People ----

func peopleShown(d *Directories) []string {
	st, _, _ := d.people.get()
	us := st.list
	if d.pusers != nil && slackc.Patched("PeopleGrid") {
		us = d.pusers
	}
	ids := make([]string, len(us))
	for i, u := range us {
		ids[i] = u.ID
	}
	return ids
}

func has(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func TestSlickViewBetterBlockPeople(t *testing.T) {
	e := newSlickEnv(t)
	e.install("BetterBlock", map[string]any{"blockedUsers": map[string]any{sAlice: "alice"}})
	e.patched("PeopleGrid")
	d := NewDirectories(e.h)
	e.until("the people directory", d.Layout, func() bool { st, ok, _ := d.people.get(); return ok && len(st.list) == 3 })
	e.until("alice's card gone", d.Layout, func() bool {
		ids := peopleShown(d)
		return len(ids) == 2 && !has(ids, sAlice) && has(ids, sBob)
	})
	if st, _, _ := d.people.get(); len(st.list) != 3 {
		t.Errorf("the directory's own results changed: %d people", len(st.list))
	}
	if err := e.s.SetOption(e.ctx(), "BetterBlock", "blockedUsers", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	e.until("alice's card back", d.Layout, func() bool { return has(peopleShown(d), sAlice) })
}

// ---- Huddles ----

// liveHuddle draws the Huddles page with a live huddle in #general.
func (e *slickEnv) liveHuddle() *Huddles {
	e.t.Helper()
	v := NewHuddles(e.h)
	e.frame(v.Layout) // subscribes
	raw, _ := json.Marshal(map[string]any{"type": "sh_room_join", "user": sBob, "room": map[string]any{
		"id": sRoom, "name": "", "date_start": time.Now().Add(-90 * time.Second).Unix(),
		"participants": []string{sAlice, sBob}, "channels": []string{sGen}}})
	e.app.Inject(slack.Event{Type: "sh_room_join", Channel: json.RawMessage(strconv.Quote(sGen)), Raw: raw})
	e.until("the live huddle", v.Layout, func() bool { return len(v.liveRooms()) == 1 })
	return v
}

func TestSlickViewBetterHuddles(t *testing.T) {
	e := newSlickEnv(t)
	e.install("BetterHuddles", nil)
	e.patched("HuddleInviteWindowWithPreviewContent")
	e.until("startHuddle wrapped", func(C) D { return D{} }, func() bool { return e.h.tt.ThunkPatched("startHuddle") })
	v := e.liveHuddle()
	// the Join point gets BetterHuddles' own onHuddleJoin
	e.until("BetterHuddles' onHuddleJoin on the Join point", v.Layout, func() bool {
		f, ok := v.joinFn[sRoom]
		return ok && f.ID != 0
	})
	// joining runs it: it notes the preview's devices and calls Clack's,
	// which dispatches startHuddle through BetterHuddles' wrapper
	before := e.ui.goCalls()
	v.joinClicked(sRoom)
	e.until("BetterHuddles calling Clack's onHuddleJoin", v.Layout, func() bool { return e.ui.goCalls() > before })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(e.h.toasts()) == 0 {
		e.frame(v.Layout)
		time.Sleep(20 * time.Millisecond)
	}
	// BetterHuddles takes a join over only once its call window can
	// render; until then Slack (here Clack's native startHuddle) keeps it
	if ts := e.h.toasts(); len(ts) > 0 {
		t.Logf("startHuddle reached Clack's native: %q", ts[0])
	} else {
		t.Logf("startHuddle was taken over by BetterHuddles (no native toast)")
	}
}

// WhoReacted in the Threads, Unreads and Activity views: their reaction
// pills are Slack's Reaction with the pill button (UnstyledButton
// data-qa="reactji") in its tree, as in a channel, so the reactors' avatars
// the plugin adds inside the button are drawn (they weren't: the views drew
// a native pill with no boundary).
func TestSlickViewWhoReactedInViews(t *testing.T) {
	e := newSlickEnv(t)
	alice, bob := sUser(sAlice, "alice", "Alice Example"), sUser(sBob, "bob", "Bob Example")
	alice.Profile.Image24, alice.Profile.Image48 = "https://avatars.invalid/alice-24.png", "https://avatars.invalid/alice-48.png"
	bob.Profile.Image24, bob.Profile.Image48 = "https://avatars.invalid/bob-24.png", "https://avatars.invalid/bob-48.png"
	e.app.S.PutUsers([]slack.User{alice, bob})
	e.install("WhoReacted", nil)
	e.patched("Reaction", "UnstyledButton")
	v := NewActivity(e.h)
	e.frame(v.Layout) // the view's theme and palette
	m := &core.Message{Channel: sGen, TS: tsMine, User: sSelf, Text: "my synthetic message",
		Reactions: []core.Reaction{{Name: "tada", Count: 2, Users: []string{sAlice, sBob}}}}
	var d D
	pills := func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		d = v.actReactions(gtx, m)
		return d
	}
	has := func(sub string) bool {
		e.ui.mu.Lock()
		defer e.ui.mu.Unlock()
		for _, tr := range e.ui.trees {
			if strings.Contains(string(tr), sub) {
				return true
			}
		}
		return false
	}
	e.until("the reactors' avatars in the tada pill", pills, func() bool {
		return has("slick-wr__av") && has("alice-24.png") && has(`"data-qa":"reactji"`)
	})
	if d.Size.X <= 0 || d.Size.Y < 24 || d.Size.Y > 32 {
		t.Errorf("the pill row is %v, want one pill row", d.Size)
	}
}

// Censorship in the Activity feed: ActivityItem hands its native a masked
// copy of the message (msg, its text and blocks), and the row previews
// that copy, as Slack does (it previewed the store's message).
func TestSlickViewCensorshipActivity(t *testing.T) {
	e := newSlickEnv(t)
	e.install("Censorship", map[string]any{"terms": "mention"})
	e.patched("ActivityItem")
	v := NewActivity(e.h)
	e.until("the feed", v.Layout, func() bool { return len(actPage(v).Items) == 6 })
	var text string
	masked := func() bool {
		if e.actRowH(v, "k-bob") <= 0 {
			return false
		}
		r := v.rows["k-bob"]
		if r == nil || r.msg() == nil {
			return false
		}
		text = richtext.PlainText(r.msg().Doc) + " / " + r.msg().Text
		return !strings.Contains(text, "mention") && strings.Contains(text, "made-up") && strings.Contains(text, "*******")
	}
	deadline := time.Now().Add(10 * time.Second)
	for !masked() {
		if time.Now().After(deadline) {
			r := v.rows["k-bob"]
			e.ui.mu.Lock()
			inTree := false
			for _, tr := range e.ui.trees {
				inTree = inTree || strings.Contains(string(tr), "made-up *******")
			}
			e.ui.mu.Unlock()
			t.Fatalf("bob's mention previewed %q (row %v, override %v), masked copy in a delivered tree: %v", text, r != nil, r != nil && r.pm != nil, inTree)
		}
		e.frame(v.Layout)
		time.Sleep(15 * time.Millisecond)
	}
	t.Logf("previewed: %q", text)
}
