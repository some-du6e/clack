package composer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/slick"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Slick plugins through the composer's native views. Each test installs
// the real built plugin (Slick's own build, SLICK_PLUGINS_DIR; skipped when
// unset) unmodified over Taut, drives a real Composer (typing, frames,
// attachments) and checks what the user would see or what goes out to the
// fake Slack API. Synthetic workspace only.

const tChRandom = "C0RANDOM" // a channel that isn't #general

type slickEnv struct {
	t     *testing.T
	app   *core.App
	slack *fakeSlack
	tt    *taut.Taut
	s     *slick.Slick
	h     *slickHost

	mu    sync.Mutex
	trees map[uint64][]byte

	why func() string // what to log when until times out
}

// slickHost is the composer's Host over the synthetic core and a real
// theme; the shell's popups do nothing.
type slickHost struct {
	app *core.App
	th  *theme.Theme
}

func (h *slickHost) Core() *core.App                           { return h.app }
func (h *slickHost) Theme() *theme.Theme                       { return h.th }
func (h *slickHost) Images() *imgcache.Cache                   { return nil }
func (h *slickHost) Invalidate()                               {}
func (h *slickHost) Navigate(string)                           {}
func (h *slickHost) OpenThread(string, string)                 {}
func (h *slickHost) CloseSecondary()                           {}
func (h *slickHost) ShowOverlay(popups.Overlay)                {}
func (h *slickHost) OpenEmojiPicker(image.Point, func(string)) {}
func (h *slickHost) OpenProfile(image.Point, string)           {}
func (h *slickHost) OpenMessageMenu(image.Point, core.Message) {}
func (h *slickHost) OpenLightbox(richtext.Image, core.Message) {}
func (h *slickHost) Toast(string)                              {}

// startSlick installs Slick plugin name with opts set before it starts.
// before runs first (fake API answers).
func startSlick(t *testing.T, name string, opts map[string]any, before func(e *slickEnv)) *slickEnv {
	t.Helper()
	dir := os.Getenv("SLICK_PLUGINS_DIR")
	if dir == "" {
		t.Skip("SLICK_PLUGINS_DIR not set: the ui-slick workflow builds Slick's plugins and runs this")
	}
	code, err := os.ReadFile(filepath.Join(dir, name+".js"))
	if err != nil {
		t.Fatal(err)
	}
	app, fs := syntheticApp(t, func(st *store.Store) {
		st.PutUsers([]slack.User{
			{ID: "U0BOB00001", Name: "bob", RealName: "Bob Example", Profile: slack.Profile{DisplayName: "bob", RealName: "Bob Example"}},
			{ID: "U0BOBBY001", Name: "bobby", RealName: "Bobby Sample", Profile: slack.Profile{DisplayName: "bobby", RealName: "Bobby Sample"}},
		})
		st.PutConv(slack.Conversation{ID: tChRandom, Name: "random", IsChannel: true, IsMember: true, NumMembers: 40})
		// DMs with both, so the composer knows them (its snapshot's people)
		st.PutConv(slack.Conversation{ID: "D0BOB00001", IsIM: true, User: "U0BOB00001", IsMember: true})
		st.PutConv(slack.Conversation{ID: "D0BOBBY001", IsIM: true, User: "U0BOBBY001", IsMember: true})
	})
	e := &slickEnv{t: t, app: app, slack: fs, trees: map[uint64][]byte{}, h: &slickHost{app: app, th: theme.New(false)}}
	if before != nil {
		before(e)
	}
	tdir := t.TempDir()
	tt, err := taut.New(taut.Options{
		App: app, Dir: filepath.Join(tdir, "taut"), CredsPath: filepath.Join(tdir, "creds.json"), CacheDir: t.TempDir(), NoAutoStart: true,
		UI: taut.UIHooks{
			Attached: func(b *taut.Bridge) { slackc.SetBridge(b) },
			Deliver: func(root uint64, tree []byte) {
				e.mu.Lock()
				e.trees[root] = append([]byte(nil), tree...)
				e.mu.Unlock()
				_ = slackc.Deliver(root, tree)
			},
			SetPatched:  slackc.SetPatched,
			CallGo:      slackc.CallGo,
			SetStyle:    slackc.SetStyle,
			RemoveStyle: slackc.RemoveStyle,
		},
		Fetch: func(ctx context.Context, req *http.Request) (*http.Response, error) {
			if strings.HasPrefix(req.URL.String(), fs.srv.URL) {
				return http.DefaultClient.Do(req.WithContext(ctx))
			}
			return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(nil)), Request: req}, nil
		},
		Log: func(level, msg string) {
			if level == "error" || level == "warn" {
				t.Logf("js %s: %s", level, msg)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tt.Close() })
	s, err := slick.New(slick.Options{Taut: tt, Dir: filepath.Join(tdir, "slick"), NoAutoStart: true, NoUpdates: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	e.tt, e.s = tt, s
	// The composer's prelude registers before the runtime starts, as in
	// the app (SetTaut runs next to taut.New).
	SetTaut(tt)
	slackc.Init(slackc.Env{Theme: e.h.th, Core: app, Invalidate: func() {}})
	slackc.SetBridge(tt.Bridge())
	t.Cleanup(func() {
		slackc.Init(slackc.Env{})
		SetTaut(nil)
		syncHooks()
		slackc.SetBridge(nil)
		slackc.Reset()
		slackc.SetPatched(nil)
	})
	ctx := e.ctx()
	if _, err := s.Import(ctx, code); err != nil {
		t.Fatalf("import %s: %v", name, err)
	}
	id := name // the built file is named by the plugin's id
	for k, v := range opts {
		if err := s.SetOption(ctx, id, k, v); err != nil {
			t.Fatalf("set %s.%s: %v", id, k, err)
		}
	}
	if err := s.SetEnabled(ctx, id, true); err != nil {
		t.Fatalf("enable %s: %v", id, err)
	}
	for _, p := range s.Settings().Plugins {
		if p.ID == id && (!p.Running || p.StartError != "") {
			t.Fatalf("%s didn't start: running=%v error=%q", id, p.Running, p.StartError)
		}
	}
	return e
}

func (e *slickEnv) ctx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	e.t.Cleanup(cancel)
	return ctx
}

// composer is a real channel composer over the env, with a frame func
// that lays it out like the ui root (slackc.Frame, then Layout).
func (e *slickEnv) composer(ch string) (*Composer, func()) {
	clearDraft(ch, "") // drafts are per channel and shared by the tests
	c := New(e.h, ch, "")
	e.t.Cleanup(func() {
		c.Close()
		clearDraft(ch, "")
	})
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(900, 600)}, Now: time.Now()}
		slackc.Frame(gtx)
		c.Layout(gtx)
	}
	return c, frame
}

// until runs frames until cond holds.
func (e *slickEnv) until(d time.Duration, what string, frame func(), cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if frame != nil {
			frame()
		}
		if cond() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	e.dumpTrees()
	if e.why != nil {
		e.t.Logf("state: %s", e.why())
	}
	e.t.Fatalf("timed out waiting for %s", what)
}

func (e *slickEnv) dumpTrees() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for root, tr := range e.trees {
		s := string(tr)
		if len(s) > 3000 {
			s = s[:3000] + "…"
		}
		e.t.Logf("tree %d: %s", root, s)
	}
}

// treeWith returns the text of the first delivered tree whose JSON
// contains sub ("" when none does).
func (e *slickEnv) treeWith(sub string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, tr := range e.trees {
		if !strings.Contains(string(tr), sub) {
			continue
		}
		var nodes []any
		if json.Unmarshal(tr, &nodes) != nil {
			continue
		}
		return nodesText(nodes), true
	}
	return "", false
}

// nodesText concatenates the text nodes of a host tree, including slots'.
func nodesText(nodes []any) string {
	var b strings.Builder
	var walk func([]any)
	walk = func(ns []any) {
		for _, x := range ns {
			m, ok := x.(map[string]any)
			if !ok {
				continue
			}
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
	walk(nodes)
	return b.String()
}

func (e *slickEnv) patched(names ...string) {
	e.t.Helper()
	e.until(5*time.Second, "patched "+strings.Join(names, ", "), nil, func() bool {
		for _, n := range names {
			if !slackc.Patched(n) {
				return false
			}
		}
		return true
	})
}

func rowNames(c *Composer) []string {
	items, _ := c.ac.Items()
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Name
	}
	return out
}

func hasName(names []string, n string) bool {
	for _, x := range names {
		if x == n {
			return true
		}
	}
	return false
}

// TestSlickViewCharacterCounter: typing shows the message's length (and
// Slack's limit) next to the send button, and it follows the text.
func TestSlickViewCharacterCounter(t *testing.T) {
	e := startSlick(t, "CharacterCounter", nil, nil)
	e.patched("WysiwygContainer", "SendButton", "TextyInput")
	c, frame := e.composer(tChGen)
	edType(c.ed, "hello")
	count := regexp.MustCompile(`(^|\D)5/40,?000`)
	e.until(8*time.Second, "the count 5/40,000 in the composer footer", frame, func() bool {
		txt, ok := e.treeWith("slick-char-counter")
		return ok && count.MatchString(txt)
	})
	e.why = func() string { return fmt.Sprintf("shown %b", c.ui.shown) }
	e.until(5*time.Second, "the send button drawn next to the count", frame, func() bool {
		return c.ui.shown&(1<<crSend) != 0
	})
	edType(c.ed, " world")
	count = regexp.MustCompile(`(^|\D)11/40,?000`)
	e.until(5*time.Second, "the count following the text (11)", frame, func() bool {
		txt, ok := e.treeWith("slick-char-counter")
		return ok && count.MatchString(txt)
	})
}

// TestSlickViewbChannel: where bChannel relays (its /bchannel command is
// installed and the viewer can't @channel), the @ rows offer every
// broadcast keyword, @everyone too outside #general.
func TestSlickViewbChannel(t *testing.T) {
	e := startSlick(t, "bChannel", nil, func(e *slickEnv) {
		e.slack.mu.Lock()
		e.slack.reply = map[string]any{"commands.list": map[string]any{"ok": true,
			"commands": map[string]any{"/bchannel": map[string]any{"name": "/bchannel", "desc": "Relay @channel", "type": "app"}}}}
		e.slack.mu.Unlock()
	})
	e.patched("TextyAutocomplete")
	// the slashCommand slice loads on first read
	rt, err := e.tt.JS()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval("composer_test_cmds.js", `globalThis.__composerTestCmds = () => Object.values(globalThis.__taut.getRawState()?.slashCommand?.commands || {}).map((c) => c && c.name)`); err != nil {
		t.Fatal(err)
	}
	e.until(5*time.Second, "the /bchannel command in the store", nil, func() bool {
		raw, err := rt.Call(e.ctx(), "__composerTestCmds")
		return err == nil && strings.Contains(string(raw), "/bchannel")
	})
	if got := rankBroadcasts("ev", "channel", false); len(got) != 0 {
		t.Fatalf("natively #random offers %v for @ev", got)
	}
	c, frame := e.composer(tChRandom)
	edType(c.ed, "hi @ev")
	e.until(8*time.Second, "@everyone in the @ rows", frame, func() bool {
		return c.ac.Open() && hasName(rowNames(c), "everyone")
	})
	if !c.ac.ov.includeAll {
		t.Error("includeAllBroadcastKeywords didn't reach the autocomplete")
	}
}

// TestSlickViewBetterBlock: a blocked person drops out of the @ rows;
// others with a matching name stay.
func TestSlickViewBetterBlock(t *testing.T) {
	e := startSlick(t, "BetterBlock", map[string]any{"blockedUsers": map[string]any{"U0BOB00001": "bob"}}, nil)
	e.patched("TextyAutocomplete")
	c, frame := e.composer(tChGen)
	e.why = func() string {
		items, sel := c.ac.Items()
		ids := make([]string, len(items))
		for i, it := range items {
			ids[i] = it.ID + "=" + it.Name
		}
		c.ac.mu.Lock()
		src := c.ac.memberSourcesLocked()
		c.ac.mu.Unlock()
		var native []string
		for _, it := range src.rank("bob", false) {
			native = append(native, it.ID+"="+it.Name)
		}
		_, haveBobby := c.ac.known["U0BOBBY001"]
		return fmt.Sprintf("open=%v active=%v trig=%+v rows=%v sel=%d disallowed=%v patched(autocompleteMembers)=%v native=%v known=%d bobbyKnown=%v",
			c.ac.Open(), c.ac.active, c.ac.trig, ids, sel, c.ac.ov.disallowed, currentTaut().ThunkPatched(thACMembers), native, len(c.ac.known), haveBobby)
	}
	edType(c.ed, "@bob")
	e.until(8*time.Second, "bobby in the @ rows and bob gone", frame, func() bool {
		names := rowNames(c)
		return c.ac.Open() && hasName(names, "bobby") && !hasName(names, "bob")
	})
	if !c.ac.ov.disallowed["U0BOB00001"] {
		t.Errorf("disallowedResults = %v, want U0BOB00001 in it", c.ac.ov.disallowed)
	}
	// still gone after more async rows land
	for i := 0; i < 20; i++ {
		frame()
		time.Sleep(10 * time.Millisecond)
	}
	if names := rowNames(c); hasName(names, "bob") {
		t.Errorf("bob came back: %v", names)
	}
}

// TestSlickViewAnonymiseFileNames: a file attached to the composer is
// uploaded under a random name (extension kept), and the chip shows the
// plugin's name toggle. Clicking the toggle switches the pending file
// between its original and anonymous name (updatePendingFile and
// maybePersistPendingFileData): the chip, MultiFileUploadItem's
// pendingFile and the button follow, and the file is shared under the
// name shown, while its bytes stay uploaded once, under the random name.
func TestSlickViewAnonymiseFileNames(t *testing.T) {
	e := startSlick(t, "AnonymiseFileNames", nil, nil)
	e.patched("MultiFileUploadItem")
	c, frame := e.composer(tChGen)
	const real = "vacation-at-home.png"
	c.att.Add(real, []byte("\x89PNG synthetic"))
	var name string
	e.until(8*time.Second, "the upload slot request", frame, func() bool {
		calls := e.slack.called("files.getUploadURLExternal")
		if len(calls) == 0 {
			return false
		}
		name = calls[0].Get("filename")
		return true
	})
	if name == "" || name == real || strings.Contains(name, "vacation") || !strings.HasSuffix(name, ".png") {
		t.Fatalf("uploaded as %q, want a random name ending .png", name)
	}
	if files := c.att.Files(); len(files) != 1 || files[0].Name != name {
		t.Errorf("pending file %+v, want it named %q", files, name)
	}
	e.until(5*time.Second, "the name toggle on the chip", frame, func() bool {
		_, ok := e.treeWith("slick-afn-toggle")
		return ok
	})
	e.until(8*time.Second, "the bytes uploaded", frame, func() bool {
		c.att.mu.Lock()
		defer c.att.mu.Unlock()
		return len(c.att.chips) == 1 && c.att.chips[0].prepared != nil
	})
	rt, err := e.tt.JS()
	if err != nil {
		t.Fatal(err)
	}
	chip := func() (title string) {
		c.att.mu.Lock()
		defer c.att.mu.Unlock()
		return c.att.chips[0].title
	}
	// toggle clicks the plugin's button and waits until the chip, its
	// props and the button all show want (anonymous: aria-pressed).
	toggle := func(want string, anonymous bool) {
		t.Helper()
		e.mu.Lock()
		var k float64
		for _, tr := range e.trees {
			var nodes []any
			if json.Unmarshal(tr, &nodes) == nil {
				if n := nodeWhere(nodes, isToggle); n != nil {
					k, _ = n["k"].(float64)
				}
			}
		}
		e.trees = map[uint64][]byte{} // what follows is the click's
		e.mu.Unlock()
		if k == 0 {
			t.Fatal("no toggle to click")
		}
		if _, err := rt.DispatchEventSync(e.ctx(), int(k), []byte(`{"type":"click","button":0}`)); err != nil {
			t.Fatal(err)
		}
		e.why = func() string {
			c.att.mu.Lock()
			defer c.att.mu.Unlock()
			return fmt.Sprintf("chip title %q, name %q", c.att.chips[0].title, c.att.chips[0].name)
		}
		e.until(8*time.Second, fmt.Sprintf("the chip named %q (anonymous %v)", want, anonymous), frame, func() bool {
			if chip() != want {
				return false
			}
			e.mu.Lock()
			defer e.mu.Unlock()
			for _, tr := range e.trees {
				var nodes []any
				if json.Unmarshal(tr, &nodes) != nil {
					continue
				}
				btn := nodeWhere(nodes, isToggle)
				item := nodeWhere(nodes, func(n map[string]any) bool { return n["t"] == "@" && n["n"] == "MultiFileUploadItem" })
				if btn == nil || item == nil {
					continue
				}
				bp, _ := btn["p"].(map[string]any)
				ip, _ := item["p"].(map[string]any)
				pf, _ := ip["pendingFile"].(map[string]any)
				file, _ := pf["file"].(map[string]any)
				// host props carry attribute values as strings
				pressed := fmt.Sprint(bp["aria-pressed"]) == fmt.Sprint(anonymous)
				busy := bp["disabled"] == true || bp["disabled"] == "true"
				if pressed && !busy && pf["title"] == want && file["name"] == name {
					return true
				}
			}
			return false
		})
	}
	toggle(real, false) // the original name
	if files := c.att.Files(); files[0].Title != real || files[0].Name != name {
		t.Errorf("after using the original name: %+v, want title %q and the uploaded name %q", files, real, name)
	}
	toggle(name, true) // anonymous again
	if files := c.att.Files(); files[0].Title != name {
		t.Errorf("after anonymising again: %+v, want title %q", files, name)
	}
	toggle(real, false)

	// the upload follows: the file is shared under the name the chip shows
	c.submit()
	e.until(8*time.Second, "the file shared", frame, func() bool { return len(e.slack.called("files.completeUploadExternal")) > 0 })
	var shared []struct{ ID, Title string }
	if err := json.Unmarshal([]byte(e.slack.called("files.completeUploadExternal")[0].Get("files")), &shared); err != nil {
		t.Fatal(err)
	}
	if len(shared) != 1 || shared[0].Title != real {
		t.Errorf("shared %+v, want one file titled %q", shared, real)
	}
	if got := e.slack.called("files.getUploadURLExternal"); len(got) != 1 {
		t.Errorf("bytes uploaded %d times, want once", len(got))
	} else if got[0].Get("filename") != name {
		t.Errorf("bytes uploaded as %q, want %q", got[0].Get("filename"), name)
	}
}

func isToggle(n map[string]any) bool {
	p, _ := n["p"].(map[string]any)
	cls, _ := p["className"].(string)
	return n["t"] == "button" && strings.Contains(cls, "slick-afn-toggle")
}

// nodeWhere is the first node in a host tree (children and element
// props included) that ok accepts.
func nodeWhere(nodes []any, ok func(map[string]any) bool) map[string]any {
	for _, x := range nodes {
		m, isMap := x.(map[string]any)
		if !isMap {
			continue
		}
		if ok(m) {
			return m
		}
		if kids, isList := m["c"].([]any); isList {
			if f := nodeWhere(kids, ok); f != nil {
				return f
			}
		}
		if p, isMap := m["p"].(map[string]any); isMap {
			for _, v := range p {
				if el, isEl := v.(map[string]any); isEl {
					if sub, isList := el["$el"].([]any); isList {
						if f := nodeWhere(sub, ok); f != nil {
							return f
						}
					}
				}
			}
		}
	}
	return nil
}

// TestSlickViewSilentTyping: typing in the composer, then clearing it,
// sends no typing event; with the plugin's switch off they go out again.
func TestSlickViewSilentTyping(t *testing.T) {
	var started, ended atomic.Int32
	oldS, oldE := typingNative, typingEndedNative
	typingNative = func(coreAPI, string, string) error { started.Add(1); return nil }
	typingEndedNative = func(coreAPI, string, string) error { ended.Add(1); return nil }
	t.Cleanup(func() { typingNative, typingEndedNative = oldS, oldE })

	e := startSlick(t, "SilentTyping", nil, nil)
	e.until(5*time.Second, "the typing thunks patched", nil, func() bool {
		return currentTaut().ThunkPatched(thTyping) && currentTaut().ThunkPatched(thTypingEnded)
	})
	typeAndClear := func(c *Composer, frame func()) {
		edType(c.ed, "hi there")
		for i := 0; i < 5; i++ {
			frame()
		}
		if !c.typingOn {
			t.Fatal("the composer didn't report typing")
		}
		c.ed.Replace(0, c.ed.Doc().Len(), "")
		for i := 0; i < 5; i++ {
			frame()
		}
		if c.typingOn {
			t.Fatal("clearing the composer didn't end typing")
		}
	}
	c, frame := e.composer(tChGen)
	typeAndClear(c, frame)
	time.Sleep(500 * time.Millisecond) // the dispatches run off the UI goroutine
	if n, m := started.Load(), ended.Load(); n != 0 || m != 0 {
		t.Fatalf("typing went out while silenced: %d started, %d ended", n, m)
	}

	// Control: with SilentTyping's switch off, the same composer path
	// reports typing and its end.
	if err := e.s.SetOption(e.ctx(), "SilentTyping", "active", false); err != nil {
		t.Fatal(err)
	}
	c2, frame2 := e.composer(tChGen)
	typeAndClear(c2, frame2)
	e.until(5*time.Second, "typing started and ended through the natives", frame2, func() bool {
		return started.Load() == 1 && ended.Load() == 1
	})
}
