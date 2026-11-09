package ui

// The plugin bench (plugin-bench workflow, docs/PLUGIN-BENCH.md): every
// plugin the Library offers in both Taut and Slick runs, as the Library
// publishes it (same sha256), in a fresh whole client signed in to
// internal/fakeslack (Size.PluginBench: a 500-message #plugin-bench built
// for these plugins). For each variant it records whether it loads,
// console errors, what its scenario's checks see (the same checks for
// both variants), install time, frame and CPU cost scrolling the long
// channel, JS heap growth, and re-renders while idle; screenshots the
// scene; and writes one JSON per plugin that ./internal/pluginbench/report
// turns into internal/pluginbench/bench.json (the Library installs the
// winner) and the docs table.
//
//	PLUGIN_BENCH_LIBRARY  the `plugins` release's files (index.json + bundles)
//	PLUGIN_BENCH_OUT      where the per-plugin JSON goes
//	PLUGIN_BENCH_SHOTS    where the screenshots go (needs a GPU: xvfb-run)
//	PLUGIN_BENCH_ONLY     comma-separated plugin names (default: all)
//	PLUGIN_BENCH_SHARD    "i/n": this job's share of the plugins
//
// Synthetic data only; nothing reaches Slack.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/pluginbench"
	"github.com/some-du6e/yoloslackgoclient/internal/pluginlib"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// benchSize is the workspace: small directory, the bench channels, the
// identity fixtures' bots.
var benchSize = fakeslack.Size{Emoji: 300, Users: 400, Channels: 12, DMs: 4, Usergroups: 4, History: 60, Unread: 0, PluginBench: true}

var benchJSCache string // a warm jsrt compilation cache dir ("" until the first run)

// benchEnv is one variant's client.
type benchEnv struct {
	*perfEnv
	t    *testing.T
	fs   *fakeslack.Server
	c    fakeslack.BenchCase
	tt   *taut.Taut
	lib  *libraryPage
	sys  string
	id   string
	logs *benchLog

	mu         sync.Mutex
	trees      map[uint64][]byte
	deliveries int
	same       int // deliveries identical to the root's previous tree
	perRoot    map[uint64]int
	calls      benchCalls
	styles     map[string]string
	doc        []byte
	nextRoot   uint64
}

// benchLog keeps the plugin runtime's console lines (taut js error/warn,
// slick: …) while a variant runs.
type benchLog struct {
	mu    sync.Mutex
	lines []string
	buf   []byte
}

var benchConsole = regexp.MustCompile(`taut js (error|warn): |taut: [^\n]*(Error|error|failed)|slick: `)

func (l *benchLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		line := string(l.buf[:i])
		l.buf = l.buf[i+1:]
		if m := benchConsole.FindStringIndex(line); m != nil {
			l.lines = append(l.lines, line[m[0]:])
		}
	}
	return len(p), nil
}

func (l *benchLog) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.lines
	l.lines = nil
	return out
}

var benchDigits = regexp.MustCompile(`\d+`)

// benchStart starts the client against fs as a Hack Club account (the
// enterprise id), so Hack Club–only plugins run.
func benchStart(t *testing.T, fs *fakeslack.Server) *benchEnv {
	home := t.TempDir()
	t.Setenv("CLACK_HOME", home)
	// the compiled JS engine from the first variant's run (wazero's disk
	// cache), so later variants don't spend their start compiling it
	jsCache := filepath.Join(home, "taut", "cache")
	if benchJSCache != "" {
		copyTree(t, benchJSCache, jsCache)
	} else if d, err := os.MkdirTemp("", "clack-bench-jscache-"); err == nil {
		t.Cleanup(func() {
			if _, err := os.Stat(jsCache); err == nil && benchJSCache == "" {
				copyTree(t, jsCache, d)
				benchJSCache = d
			}
		})
	}
	pe := &perfEnv{tb: t, fs: fs, home: home, size: image.Pt(1440, 900), now: time.Now(), start: time.Now()}
	images := imgcache.New(fs.Fetch, "", nil)
	pe.a = New(Options{ConfigDir: home, Started: pe.start, NoRealtime: true}, images)
	cache := filepath.Join(home, "cache")
	creds := fs.Creds()
	creds.EnterpriseID = "E09V59WQY1E" // taut.HackClubIDs
	svc := core.New(slack.New(creds), store.New(cache))
	pe.a.Attach(svc, cache)
	images.SetInvalidate(func() {})
	pe.a.th.Images = images
	if installHooks != nil {
		installHooks(pe.a)
	}
	e := &benchEnv{perfEnv: pe, t: t, fs: fs, c: fs.Bench(), trees: map[uint64][]byte{}, styles: map[string]string{}, nextRoot: 1 << 40}
	deliverTap = e.record
	styleTap = func(k, css string) {
		e.mu.Lock()
		e.styles[k] = css
		e.mu.Unlock()
	}
	documentTap = func(b []byte) {
		e.mu.Lock()
		e.doc = append([]byte(nil), b...)
		e.mu.Unlock()
	}
	t.Cleanup(func() { deliverTap, styleTap, documentTap = nil, nil, nil })
	pe.a.data.start(pe.a)
	t.Cleanup(pe.shutdown)
	t.Cleanup(func() {
		if pe.a.data != nil {
			pe.a.data.stopTaut()
		}
	})
	return e
}

func (e *benchEnv) record(root uint64, tree []byte) {
	e.mu.Lock()
	if e.calls.gone(root) {
		// the commit unmounting a root slackc swept (rows the scroll pass
		// left behind): cleanup, not a re-render
		delete(e.trees, root)
		e.mu.Unlock()
		return
	}
	if bytes.Equal(e.trees[root], tree) {
		e.same++
	}
	e.trees[root] = append([]byte(nil), tree...)
	e.deliveries++
	if e.perRoot == nil {
		e.perRoot = map[uint64]int{}
	}
	e.perRoot[root]++
	e.mu.Unlock()
}

func (e *benchEnv) delivered() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.deliveries
}

// wait draws frames until cond holds or d passes.
func (e *benchEnv) wait(d time.Duration, cond func() bool) bool {
	dl := time.Now().Add(d)
	for {
		e.frame()
		if cond() {
			return true
		}
		if time.Now().After(dl) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// treeWith returns a delivered tree containing every sub ("" if none).
func (e *benchEnv) treeWith(subs ...string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, tr := range e.trees {
		s := string(tr)
		ok := true
		for _, sub := range subs {
			if !strings.Contains(s, sub) {
				ok = false
				break
			}
		}
		if ok {
			return s
		}
	}
	return ""
}

func (e *benchEnv) waitTree(d time.Duration, subs ...string) bool {
	return e.wait(d, func() bool { return e.treeWith(subs...) != "" })
}

// rootsWith counts delivered roots whose tree contains sub.
func (e *benchEnv) rootsWith(sub string) map[uint64]bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[uint64]bool{}
	for r, tr := range e.trees {
		if strings.Contains(string(tr), sub) {
			out[r] = true
		}
	}
	return out
}

// render renders a Slack component through the plugin bridge at a fresh
// root and returns its tree once delivered.
func (e *benchEnv) render(name string, props any) []*slackc.Node {
	nodes, _ := slackc.ParseTree(e.renderRaw(name, props))
	return nodes
}

// renderRaw is render's tree as delivered.
func (e *benchEnv) renderRaw(name string, props any) []byte {
	e.mu.Lock()
	e.nextRoot++
	root := e.nextRoot
	e.mu.Unlock()
	b, _ := json.Marshal(props)
	e.tt.Bridge().Render(root, name, b)
	var tree []byte
	e.wait(5*time.Second, func() bool {
		e.mu.Lock()
		tree = e.trees[root]
		e.mu.Unlock()
		return tree != nil
	})
	e.tt.Bridge().Unmount(root)
	return tree
}

func findComp(nodes []*slackc.Node, name string) *slackc.Node {
	for _, n := range nodes {
		if n.IsComponent() && n.N == name {
			return n
		}
		if f := findComp(n.C, name); f != nil {
			return f
		}
	}
	return nil
}

func nodesText(nodes []*slackc.Node) string {
	var b strings.Builder
	var walk func([]*slackc.Node)
	walk = func(ns []*slackc.Node) {
		for _, n := range ns {
			if n.IsText() {
				b.WriteString(n.V)
			}
			walk(n.C)
		}
	}
	walk(nodes)
	return b.String()
}

func (e *benchEnv) openBench() {
	e.openChannel(fakeslack.BenchChannelID)
	e.wait(15*time.Second, func() bool { return e.loadedMessages(fakeslack.BenchChannelID) > 0 })
	e.settle(400 * time.Millisecond)
}

// toBottom scrolls the open channel to its newest message.
func (e *benchEnv) toBottom() {
	p := e.listPoint()
	for i := 0; i < 80; i++ {
		e.scroll(p, 4000)
		e.frame()
	}
	e.settle(300 * time.Millisecond)
}

// loadAll scrolls up until the whole channel is loaded.
func (e *benchEnv) loadAll() {
	p := e.listPoint()
	last, same := -1, 0
	for i := 0; i < 60 && same < 4; i++ {
		for k := 0; k < 20; k++ {
			e.scroll(p, -3000)
			e.frame()
		}
		e.settle(150 * time.Millisecond)
		n := e.loadedMessages(fakeslack.BenchChannelID)
		if n >= fakeslack.BenchMessages {
			break
		}
		if n == last {
			same++
		} else {
			same = 0
		}
		last = n
	}
}

// scrollPass measures one pass over the channel: from the bottom up and
// back down, a wheel step a frame.
type scrollStats struct {
	median, p95 float64 // µs
	cpu         float64 // ms
	allocs      float64 // per frame
}

func (e *benchEnv) scrollPass() scrollStats {
	e.toBottom()
	p := e.listPoint()
	runtime.GC()
	var ms0, ms1 runtime.MemStats
	runtime.ReadMemStats(&ms0)
	c0 := procCPU()
	var ds []float64
	step := func(dy float32) {
		e.scroll(p, dy)
		t0 := time.Now()
		e.frame()
		ds = append(ds, float64(time.Since(t0).Microseconds()))
	}
	for i := 0; i < 150; i++ {
		step(-260)
	}
	for i := 0; i < 150; i++ {
		step(260)
	}
	// the JS renders of rows that scrolled in finish after the frames
	e.settle(400 * time.Millisecond)
	cpu := procCPU() - c0
	runtime.ReadMemStats(&ms1)
	sort.Float64s(ds)
	return scrollStats{median: ds[len(ds)/2], p95: ds[len(ds)*95/100], cpu: float64(cpu.Microseconds()) / 1000,
		allocs: float64(ms1.Mallocs-ms0.Mallocs) / float64(len(ds))}
}

// idle draws frames with no input for d and counts deliveries and frames
// that asked for another; then measures process CPU with no frames.
func (e *benchEnv) idle(d time.Duration) (deliveries, wakeups int, cpuPerSec float64) {
	e.settle(time.Second)
	e.mu.Lock()
	before := maps.Clone(e.perRoot)
	same0 := e.same
	e.mu.Unlock()
	c0 := e.calls.snapshot()
	e.js(`globalThis.__bench && (globalThis.__bench.inv = {}, globalThis.__bench.refresh = 0, globalThis.__bench.act = {}); 0`)
	n0 := e.delivered()
	for dl := time.Now().Add(d); time.Now().Before(dl); {
		e.frame()
		if _, ok := e.r.WakeupTime(); ok {
			wakeups++
		}
		time.Sleep(16 * time.Millisecond)
	}
	deliveries = e.delivered() - n0
	if deliveries > 0 {
		e.mu.Lock()
		again := map[uint64][]byte{}
		for r, n := range e.perRoot {
			if n > before[r] {
				again[r] = e.trees[r]
			}
		}
		e.t.Logf("idle: %d deliveries (%d identical to the root's last) to %d roots: %s; Go→JS meanwhile: %s",
			deliveries, e.same-same0, len(again), idleShapes(again), e.calls.since(c0))
		e.mu.Unlock()
		e.t.Logf("idle JS: %s", e.js(`JSON.stringify(globalThis.__bench ?? null)`))
	}
	cpu0 := procCPU()
	time.Sleep(2 * time.Second)
	cpuPerSec = float64((procCPU() - cpu0).Microseconds()) / 1000 / 2
	return
}

func (e *benchEnv) jsHeap() float64 {
	rt, err := e.tt.JS()
	if err != nil || rt == nil {
		return 0
	}
	return float64(rt.Stats().Memory) / 1024
}

// shot renders the last frame (GPU; skipped without one).
func (e *benchEnv) shot(dir, name string) *image.RGBA {
	if dir == "" {
		return nil
	}
	e.settle(500 * time.Millisecond)
	e.frame()
	img := pixeltest.Render(e.t, e.size.X, e.size.Y, &e.ops)
	if f, err := os.Create(filepath.Join(dir, name+".png")); err == nil {
		_ = png.Encode(f, img)
		f.Close()
	}
	return img
}

// ---- scenarios ----

type benchCheck struct {
	name string
	run  func(e *benchEnv) (bool, string)
}

type benchScenario struct {
	opts   map[string]map[string]any // system → options set before enabling
	before func(e *benchEnv)         // storage seeding (after the runtime is up, before install)
	checks []benchCheck
	scene  func(e *benchEnv) // the screenshot's view (default: #plugin-bench at the bottom)
	notes  string
}

var allDays = []any{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

const slackbotNotice = "Your workspace has been using `/poll` to kick off certain actions with Old Poll. Recently, New Poll was installed to Fake Club with the same command. " +
	"Now, when people enter `/poll`, it'll run the action set up by New Poll. You can learn more about <https://slack.com/help/articles/201259356-Slash-commands-in-Slack|managing app installation settings> in the Help Center."

func richBlocksOf(text string) json.RawMessage {
	b, _ := json.Marshal([]map[string]any{{"type": "rich_text", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": text}}}}}})
	return b
}

// menuItems opens a menu as the App does and returns its rows once they
// stop changing (plugins add theirs asynchronously).
func (e *benchEnv) menuItems(m *popups.Menu, want func([]popups.MenuItem) bool) []popups.MenuItem {
	e.a.ShowOverlay(m)
	defer func() {
		for e.a.dismissTop() {
		}
	}()
	e.wait(8*time.Second, func() bool { return want(m.Items()) })
	e.settle(200 * time.Millisecond)
	return m.Items()
}

func hasLabel(items []popups.MenuItem, re *regexp.Regexp) (string, bool) {
	for _, it := range items {
		if re.MatchString(it.Label) {
			return it.Label, true
		}
		if l, ok := hasLabel(it.Sub, re); ok {
			return l, true
		}
	}
	return "", false
}

func labels(items []popups.MenuItem) string {
	var out []string
	for _, it := range items {
		if it.Label != "" {
			out = append(out, it.Label)
		}
	}
	return strings.Join(out, ", ")
}

// typingSilenced dispatches currentUserStartedTyping and reports whether
// the native (the typing event Slack sends) was skipped.
func (e *benchEnv) typingSilenced() bool {
	type args struct {
		ChannelID string `json:"channelId"`
	}
	calls := 0
	native := func(ctx context.Context, a args) (any, error) { calls++; return nil, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = taut.Dispatch(ctx, e.tt, "currentUserStartedTyping", args{fakeslack.BenchChannelID}, native)
	return calls == 0
}

// notificationShown sends a new message's notification the way Clack
// does (backend.go: showNotification with the Slack message, then
// notificationText) and reports whether the desktop notification would
// show text: a plugin may drop it or blank the message.
func (e *benchEnv) notificationShown(ch, user, text string) bool {
	ts := fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
	msg := map[string]any{"type": "message", "channel": ch, "ts": ts, "text": text, "user": user}
	args := map[string]any{"title": "Bench", "message": msg, "body": text, "channelId": ch, "ts": ts}
	shown := false
	native := func(ctx context.Context, a map[string]any) (any, error) {
		if _, t, ok := notificationText(a, msg, text); ok && t != "" {
			shown = true
		}
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = taut.Dispatch(ctx, e.tt, "showNotification", args, native)
	return shown
}

var fnRef = regexp.MustCompile(`"onClick":\{"\$fn":(\d+)\}`)

// clickComposerButtons clicks every message-box button plugins added
// (Taut's composer.addButton row): how a user turns on a plugin whose
// switch lives there.
func (e *benchEnv) clickComposerButtons() int {
	tree := e.renderRaw("TautComposerButtons", map[string]any{"placement": "end", "context": map[string]any{"kind": "channel", "channelId": fakeslack.BenchChannelID}})
	n := 0
	for _, m := range fnRef.FindAllSubmatch(tree, -1) {
		id, _ := strconv.ParseInt(string(m[1]), 10, 64)
		e.tt.Bridge().Call(id, []byte("[]"), nil)
		n++
	}
	return n
}

func (e *benchEnv) userName(id string) (display, real string) {
	if u, ok := e.a.data.core.S.User(id); ok {
		return u.Profile.DisplayName, u.RealName
	}
	return "", ""
}

func (e *benchEnv) threadWithPronouns() string {
	for _, ts := range e.c.Threads {
		if e.c.Pronouns[e.c.ThreadBy[ts]] != "" {
			return ts
		}
	}
	return e.c.Threads[0]
}

var benchScenarios = map[string]benchScenario{
	"userpronouns": {
		checks: []benchCheck{
			{"pronouns next to the sender in channel rows", func(e *benchEnv) (bool, string) {
				var found []string
				for _, p := range e.c.Pronouns {
					if e.treeWith(">"+p+"<") != "" || e.treeWith(`"v":"`+p+`"`) != "" {
						found = append(found, p)
					}
				}
				if len(found) == 0 {
					return false, "no pronouns in any host tree"
				}
				return true, fmt.Sprintf("%d of %d people's pronouns shown", len(found), len(e.c.Pronouns))
			}},
			{"pronouns in the thread pane header", func(e *benchEnv) (bool, string) {
				ts := e.threadWithPronouns()
				want := e.c.Pronouns[e.c.ThreadBy[ts]]
				before := e.rootsWith(`"v":"` + want + `"`)
				e.a.OpenThread(fakeslack.BenchChannelID, ts)
				ok := e.wait(8*time.Second, func() bool {
					for r := range e.rootsWith(`"v":"` + want + `"`) {
						if !before[r] {
							return true
						}
					}
					return false
				})
				if !ok {
					return false, fmt.Sprintf("no new tree with %s after opening the thread (pane %v, ThreadSenderAndTimestampGeneric patched %v, %d trees)",
						want, e.a.secondary.kind == secThread, slackc.Patched("ThreadSenderAndTimestampGeneric"), len(e.trees))
				}
				return true, ""
			}},
		},
		scene: func(e *benchEnv) {
			e.a.OpenThread(fakeslack.BenchChannelID, e.threadWithPronouns())
			e.settle(800 * time.Millisecond)
		},
	},
	"whoreacted": {
		opts: map[string]map[string]any{pluginlib.Taut: {"maxAvatars": 4}, pluginlib.Slick: {"maxAvatars": 4}},
		checks: []benchCheck{
			{"reactors' avatars inside the reaction pills", func(e *benchEnv) (bool, string) {
				if !e.waitTree(8*time.Second, "/img/24x24/u") {
					return false, "no tree with a reactor's 24px avatar"
				}
				return true, ""
			}},
			{"+N past the avatar limit (7 reactors, 4 shown)", func(e *benchEnv) (bool, string) {
				if !e.waitTree(5*time.Second, `"v":"+3"`) && !e.waitTree(time.Second, "/img/24x24/u", `"v":"+"`, `"v":"3"`) {
					return false, "no +3"
				}
				return true, ""
			}},
		},
	},
	"copyreacted": {
		checks: []benchCheck{
			{"a Copy who reacted button in the reaction bar", func(e *benchEnv) (bool, string) {
				if !e.waitTree(8*time.Second, "Copy who reacted") {
					return false, "no tree with the button"
				}
				return true, ""
			}},
		},
	},
	"censorship": {
		checks: []benchCheck{
			{`"job" masked in the message (default terms)`, func(e *benchEnv) (bool, string) {
				ts := e.c.Censored[len(e.c.Censored)-1]
				var text string
				ok := e.wait(8*time.Second, func() bool {
					m, ok := e.tt.MessageView(fakeslack.BenchChannelID, ts)
					text = m.Text
					return ok && !strings.Contains(text, "job") && strings.Contains(text, "fair")
				})
				if !ok {
					return false, fmt.Sprintf("message view: %q", text)
				}
				return true, text[strings.Index(text, "the "):]
			}},
		},
	},
	"clearurls": {
		checks: []benchCheck{
			{"tracking parameters removed from a sent link", func(e *benchEnv) (bool, string) {
				in := "see https://example.com/page?utm_source=news&id=7"
				var got string
				ok := e.wait(15*time.Second, func() bool {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					out := e.tt.TransformSendBlocks(ctx, richBlocksOf(in))
					link := e.tt.TransformLink(ctx, "https://example.com/page?utm_source=news&id=7", "paste")
					got = string(out) + " | paste: " + link
					return !strings.Contains(string(out), "utm_source") && strings.Contains(string(out), "id=7") ||
						link == "https://example.com/page?id=7"
				})
				if !ok {
					return false, "unchanged: " + got
				}
				return true, ""
			}},
		},
		notes: "The rules come from ClearURLs/Rules on GitHub at start, so this check needs the network.",
	},
	"clicktoload": {
		checks: []benchCheck{
			{"hooks media embeds (AttachmentMedia or the document's frames)", func(e *benchEnv) (bool, string) {
				if slackc.Patched("AttachmentMedia") {
					return true, "patches AttachmentMedia"
				}
				if e.tt.Bridge().WantsEvent("click") || len(e.styles) > 0 {
					return true, "watches the document"
				}
				return false, "no hook"
			}},
		},
		notes: "Clack draws link unfurls natively and never loads third-party frames, so there is no embed for either variant to hold; the check only sees that the hook is in place.",
	},
	"customnamerecording": {
		checks: []benchCheck{
			{"patches the profile editor's audio button", func(e *benchEnv) (bool, string) {
				return slackc.Patched("EditAudioButton"), ""
			}},
		},
		notes: "Its button lives in Edit profile, behind a file picker; the bench checks the patch only.",
	},
	"experiments": {
		checks: []benchCheck{
			{"an Experiments tab in Preferences", func(e *benchEnv) (bool, string) {
				ok := e.wait(5*time.Second, func() bool {
					for _, tab := range e.a.tautTabs {
						if strings.Contains(strings.ToLower(tab.Label), "experiment") {
							return true
						}
					}
					return false
				})
				var ls []string
				for _, tab := range e.a.tautTabs {
					ls = append(ls, tab.Label)
				}
				return ok, strings.Join(ls, ", ")
			}},
		},
	},
	"haikuwarning": {
		opts:  map[string]map[string]any{pluginlib.Taut: {"anyChannel": true}},
		notes: "Taut's warns only in channels Orpheus is in unless \"Warn even if Orpheus doesn't respond\" is on; the bench turns that on, as Orpheus isn't in the fake workspace.",
		checks: []benchCheck{
			{"an ordinary message goes out", func(e *benchEnv) (bool, string) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				text := "just a normal message"
				return e.tt.SendCheck(ctx, taut.ComposerContext{Kind: "channel", ChannelID: fakeslack.BenchChannelID, Text: text, Blocks: richBlocksOf(text)}), ""
			}},
			{"a haiku is held for a confirmation", func(e *benchEnv) (bool, string) {
				text := "an old silent pond a frog jumps into the pond splash silence again"
				e.settle(3 * time.Second) // Taut's syllable dictionary loads from GitHub at start
				done := make(chan bool, 1)
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
					defer cancel()
					done <- e.tt.SendCheck(ctx, taut.ComposerContext{Kind: "channel", ChannelID: fakeslack.BenchChannelID, Text: text, Blocks: richBlocksOf(text)})
				}()
				deadline := time.After(3 * time.Second)
				for {
					select {
					case ok := <-done:
						return false, fmt.Sprintf("send check answered %v without asking", ok)
					case <-deadline:
						for e.a.dismissTop() {
						}
						return true, ""
					default:
						e.frame()
						time.Sleep(15 * time.Millisecond)
					}
				}
			}},
		},
	},
	"humancount": {
		checks: []benchCheck{
			{"the header's member count leaves apps out", func(e *benchEnv) (bool, string) {
				var got any
				ok := e.wait(8*time.Second, func() bool {
					n := findComp(e.render("BaseAvatarStack", map[string]any{"channelId": fakeslack.BenchChannelID, "memberCount": 1240}), "BaseAvatarStack")
					if n == nil {
						return false
					}
					got = n.P["memberCount"]
					return got != nil && got != float64(1240)
				})
				return ok, fmt.Sprintf("memberCount %v (all members: 1240)", got)
			}},
		},
	},
	"lastseen": {
		checks: []benchCheck{
			{"Last seen on a profile", func(e *benchEnv) (bool, string) {
				id := e.c.Authors[0]
				e.a.OpenUserProfile(id)
				ok := e.waitTree(10*time.Second, "Last seen")
				return ok, ""
			}},
		},
		scene: func(e *benchEnv) { e.a.OpenUserProfile(e.c.Authors[0]); e.settle(800 * time.Millisecond) },
	},
	"nicknames": {
		opts: map[string]map[string]any{pluginlib.Slick: {"names": map[string]any{"__AUTHOR__": "Bench Nick"}}},
		before: func(e *benchEnv) {
			e.tt.DataWrite("plugin:Nicknames:storage", "nicknames", `{"`+e.c.Authors[0]+`":"Bench Nick"}`)
		},
		checks: []benchCheck{
			{"a nickname replaces the member's name", func(e *benchEnv) (bool, string) {
				var m taut.MemberFields
				ok := e.wait(8*time.Second, func() bool {
					var ok bool
					m, ok = e.tt.MemberView(e.c.Authors[0])
					return ok && (m.DisplayName == "Bench Nick" || m.RealName == "Bench Nick")
				})
				return ok, fmt.Sprintf("display %q real %q", m.DisplayName, m.RealName)
			}},
			{"Set nickname… in the profile ⋮ menu", func(e *benchEnv) (bool, string) {
				re := regexp.MustCompile(`(?i)nickname`)
				items := e.menuItems(popups.NewProfileOverflowMenu(e.a, image.Pt(900, 200), e.c.Authors[1]), func(it []popups.MenuItem) bool { _, ok := hasLabel(it, re); return ok })
				l, ok := hasLabel(items, re)
				if !ok {
					return false, labels(items)
				}
				return true, l
			}},
		},
	},
	"officehours": {
		opts: map[string]map[string]any{
			pluginlib.Taut:  {"invisible": []any{map[string]any{"days": allDays, "start": "00:00", "end": "23:59"}}},
			pluginlib.Slick: {"schedule": []any{map[string]any{"days": []any{0, 1, 2, 3, 4, 5, 6}, "start": "00:00", "end": "23:59", "status": "invisible"}}},
		},
		checks: []benchCheck{
			{"inside an Away window, you're set away", func(e *benchEnv) (bool, string) {
				ok := e.wait(12*time.Second, func() bool { return e.fs.CallCount("users.setPresence") > 0 })
				return ok, fmt.Sprintf("users.setPresence calls: %d", e.fs.CallCount("users.setPresence"))
			}},
		},
	},
	"oneko": {
		checks: []benchCheck{
			{"the cat is in the document", func(e *benchEnv) (bool, string) {
				ok := e.wait(5*time.Second, func() bool {
					e.mu.Lock()
					defer e.mu.Unlock()
					return bytes.Contains(e.doc, []byte("oneko"))
				})
				return ok, ""
			}},
			{"it follows the pointer (mousemove forwarded)", func(e *benchEnv) (bool, string) {
				return e.tt.Bridge().WantsEvent("mousemove"), ""
			}},
		},
	},
	"quickjoin": {
		checks: []benchCheck{
			{"Join channel from a channel you're not in", func(e *benchEnv) (bool, string) {
				re := regexp.MustCompile(`(?i)^join`)
				e.openChannel(fakeslack.PreviewChannelID)
				e.wait(10*time.Second, func() bool { c, ok := e.a.data.core.Conv(fakeslack.PreviewChannelID); return ok && c.Name != "" })
				e.settle(300 * time.Millisecond)
				items := e.menuItems(popups.NewChannelMenu(e.a, image.Pt(300, 200), fakeslack.PreviewChannelID), func(it []popups.MenuItem) bool { _, ok := hasLabel(it, re); return ok })
				if l, ok := hasLabel(items, re); ok {
					return true, "channel menu: " + l
				}
				if e.tt.ThunkPatched("joinChannel") || e.tt.ThunkPatched("joinCodeChannel") {
					return true, "joins from channel links (patches joinChannel)"
				}
				return false, "menu: " + labels(items)
			}},
		},
		notes: "Taut's adds Join channel to the channel menu; Slick's joins from a link to the channel (double-click, hover card). The check passes on either.",
	},
	"restrictedchannelwarning": {
		checks: []benchCheck{
			{"admins see the restricted box with Post anyway", func(e *benchEnv) (bool, string) {
				e.openChannel(fakeslack.BenchAdminsID)
				ok := e.waitTree(10*time.Second, "Post anyway")
				return ok, ""
			}},
		},
		scene: func(e *benchEnv) { e.openChannel(fakeslack.BenchAdminsID); e.settle(800 * time.Millisecond) },
	},
	"showrealuser": {
		checks: []benchCheck{
			{"a relay bot's message credits the person", func(e *benchEnv) (bool, string) {
				var ts, who string
				for k, v := range e.c.Relayed {
					if k > ts {
						ts, who = k, v
					}
				}
				display, real := e.userName(who)
				var m core.Message
				ok := e.wait(10*time.Second, func() bool {
					m, _ = e.tt.MessageView(fakeslack.BenchChannelID, ts)
					return m.User == who || (display != "" && e.treeWith(display) != "") || (real != "" && e.treeWith(real) != "")
				})
				return ok, fmt.Sprintf("message user %q (want %s)", m.User, who)
			}},
		},
	},
	"showsendingbot": {
		checks: []benchCheck{
			{"the sending app's badge on a person's message", func(e *benchEnv) (bool, string) {
				// the badge is the app's avatar (botId/botProfile) in the
				// plugin's badge element, beside the person's own avatar
				var found string
				ok := e.wait(10*time.Second, func() bool {
					for _, cls := range []string{"taut-sending-bot", "slick-sending-bot"} {
						if e.treeWith(cls, `"`+fakeslack.BenchSenderBotID+`"`) != "" {
							found = cls
							return true
						}
					}
					return false
				})
				return ok, found
			}},
		},
	},
	"shutupslackbot": {
		checks: []benchCheck{
			{"the slash-command notice is marked read", func(e *benchEnv) (bool, string) {
				ts := fmt.Sprintf("%d.000100", time.Now().Unix())
				raw, _ := json.Marshal(map[string]any{"type": "message", "channel": fakeslack.BenchSlackbotDMID, "user": fakeslack.SlackbotID, "text": slackbotNotice, "ts": ts})
				e.a.data.core.Inject(slack.Event{Type: "message", Channel: json.RawMessage(`"` + fakeslack.BenchSlackbotDMID + `"`), Raw: raw})
				ok := e.wait(8*time.Second, func() bool { return e.fs.Marked(fakeslack.BenchSlackbotDMID) == ts })
				return ok, "marked: " + e.fs.Marked(fakeslack.BenchSlackbotDMID)
			}},
			{"its notification is silenced, others aren't", func(e *benchEnv) (bool, string) {
				if e.notificationShown(fakeslack.BenchSlackbotDMID, fakeslack.SlackbotID, slackbotNotice) {
					return false, "the notice's notification was shown"
				}
				if !e.notificationShown(fakeslack.BenchSlackbotDMID, fakeslack.SlackbotID, "your reminder: lunch") {
					return false, "an ordinary notification was dropped"
				}
				return true, ""
			}},
		},
	},
	"silenttyping": {
		checks: []benchCheck{
			{"a typing switch in the message box", func(e *benchEnv) (bool, string) {
				if e.tt.ComposerButtons() {
					return true, "composer button"
				}
				if slackc.Patched("TextyButtonOverflow") {
					return true, "in the composer's overflow"
				}
				return false, ""
			}},
			{"once on, typing isn't sent", func(e *benchEnv) (bool, string) {
				if e.typingSilenced() {
					return true, "on by default"
				}
				n := e.clickComposerButtons()
				ok := e.wait(3*time.Second, e.typingSilenced)
				return ok, fmt.Sprintf("after clicking %d message box button(s)", n)
			}},
		},
	},
	"slimmessagebox": {
		checks: []benchCheck{
			{"restyles the message box (TextyButtons, InputContainer)", func(e *benchEnv) (bool, string) {
				ok := slackc.Patched("TextyButtons") || slackc.Patched("InputContainer") || slackc.Patched("MessageInput")
				return ok, strings.Join(slices.DeleteFunc(slackc.PatchedNames(), func(s string) bool {
					return !strings.Contains(s, "Texty") && !strings.Contains(s, "Input")
				}), ", ")
			}},
		},
	},
	"streamermode": {
		opts: map[string]map[string]any{pluginlib.Taut: {"alwaysOn": true}, pluginlib.Slick: {"activation": "always"}},
		checks: []benchCheck{
			{"private things are blurred (a blur stylesheet)", func(e *benchEnv) (bool, string) {
				ok := e.wait(5*time.Second, func() bool {
					e.mu.Lock()
					defer e.mu.Unlock()
					for _, css := range e.styles {
						if strings.Contains(css, "blur(") {
							return true
						}
					}
					return false
				})
				return ok, ""
			}},
			{"notifications are silenced", func(e *benchEnv) (bool, string) {
				ok := e.wait(5*time.Second, func() bool { return !e.notificationShown(fakeslack.BenchSlackbotDMID, e.c.Authors[0], "secret dm") })
				return ok, ""
			}},
		},
	},
	"bettertypingindicator": {
		checks: []benchCheck{
			{"names of everyone typing", func(e *benchEnv) (bool, string) {
				typists := e.c.Authors[1:4]
				inject := func() {
					for _, u := range typists {
						raw, _ := json.Marshal(map[string]any{"type": "user_typing", "channel": fakeslack.BenchChannelID, "user": u})
						e.a.data.core.Inject(slack.Event{Type: "user_typing", Channel: json.RawMessage(`"` + fakeslack.BenchChannelID + `"`), Raw: raw})
					}
				}
				inject()
				found := 0
				ok := e.wait(8*time.Second, func() bool {
					found = 0
					for _, u := range typists {
						d, r := e.userName(u)
						if (d != "" && e.treeWith(d) != "") || (r != "" && e.treeWith(r) != "") {
							found++
						}
					}
					if found < len(typists) {
						inject()
					}
					return found == len(typists)
				})
				return ok, fmt.Sprintf("%d of %d typists named", found, len(typists))
			}},
		},
	},
	"accountswitcher": {
		checks: []benchCheck{
			{"Switch account in the profile menu", func(e *benchEnv) (bool, string) {
				re := regexp.MustCompile(`(?i)switch account|add another account`)
				items := e.menuItems(popups.NewUserMenu(e.a, image.Pt(40, 760)), func(it []popups.MenuItem) bool { _, ok := hasLabel(it, re); return ok })
				l, ok := hasLabel(items, re)
				if !ok {
					return false, labels(items)
				}
				return true, l
			}},
		},
	},
	"adminbackend": {
		checks: []benchCheck{
			{"admin tools in a member's profile ⋮ menu", func(e *benchEnv) (bool, string) {
				re := regexp.MustCompile(`(?i)identity|telescreen|fire engine|attend|slack admin`)
				items := e.menuItems(popups.NewProfileOverflowMenu(e.a, image.Pt(900, 200), e.c.Authors[1]), func(it []popups.MenuItem) bool { _, ok := hasLabel(it, re); return ok })
				l, ok := hasLabel(items, re)
				if !ok {
					return false, labels(items)
				}
				return true, l
			}},
		},
	},
}

// ---- the bench ----

func TestPluginBench(t *testing.T) {
	dir := os.Getenv("PLUGIN_BENCH_LIBRARY")
	if dir == "" {
		t.Skip("PLUGIN_BENCH_LIBRARY not set: the plugin-bench workflow downloads the Library and runs this")
	}
	out := os.Getenv("PLUGIN_BENCH_OUT")
	shots := os.Getenv("PLUGIN_BENCH_SHOTS")
	for _, d := range []string{out, shots} {
		if d != "" {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	srv := httptest.NewServer(http.StripPrefix("/rel/", http.FileServer(http.Dir(dir))))
	t.Cleanup(srv.Close)
	t.Setenv("CLACK_PLUGIN_LIBRARY", srv.URL+"/rel/index.json")
	b, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ix pluginlib.Index
	if err := json.Unmarshal(b, &ix); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, sys := range []string{pluginlib.Taut, pluginlib.Slick} {
		if s, ok := ix.Sources[sys]; ok {
			sources = append(sources, sys+" "+shortSHA(s.Commit))
		}
	}
	var pairs []libGroup
	for _, g := range libraryGroups(libraryEntries(&ix)) {
		seen := map[string]bool{}
		for _, e := range g.Entries {
			seen[e.System] = true
		}
		if seen[pluginlib.Taut] && seen[pluginlib.Slick] {
			pairs = append(pairs, g)
		}
	}
	if only := os.Getenv("PLUGIN_BENCH_ONLY"); only != "" {
		want := map[string]bool{}
		for _, n := range strings.Split(only, ",") {
			want[libNameKey(n)] = true
		}
		pairs = slices.DeleteFunc(pairs, func(g libGroup) bool { return !want[libNameKey(g.first().Name)] })
	}
	if sh := os.Getenv("PLUGIN_BENCH_SHARD"); sh != "" {
		var i, n int
		if _, err := fmt.Sscanf(sh, "%d/%d", &i, &n); err != nil || n <= 0 || i < 0 || i >= n {
			t.Fatalf("bad PLUGIN_BENCH_SHARD %q", sh)
		}
		var mine []libGroup
		for k, g := range pairs {
			if k%n == i {
				mine = append(mine, g)
			}
		}
		pairs = mine
	}
	t.Logf("%d plugins in both systems to bench", len(pairs))
	for _, g := range pairs {
		name := g.first().Name
		t.Run(libNameKey(name), func(t *testing.T) {
			sc, ok := benchScenarios[libNameKey(name)]
			if !ok {
				t.Logf("no scenario for %s: load, cost and idle only", name)
			}
			p := pluginbench.Plugin{Name: name, Notes: sc.notes}
			var imgs []*image.RGBA
			for _, e := range g.Entries {
				if e.System != pluginlib.Taut && e.System != pluginlib.Slick {
					continue
				}
				v, img := benchVariant(t, &ix, e, sc, shots)
				p.Variants = append(p.Variants, v)
				imgs = append(imgs, img)
			}
			if len(imgs) == 2 && imgs[0] != nil && imgs[1] != nil {
				d := pixelDiff(imgs[0], imgs[1])
				p.Variants[0].PixelDiff, p.Variants[1].PixelDiff = d, d
			}
			pluginbench.Decide(&p)
			t.Logf("%s: winner %s (%s)", name, p.Winner, p.Reason)
			for _, v := range p.Variants {
				t.Logf("  %s %s: loaded=%v checks %d/%d errors %d init %.1fms frame %.0f/%.0fµs cpu %.0f/%.0fms jsheap %.0fKB idle %d trees %d wakeups (%d) cpu %.1f/%.1fms/s",
					v.System, v.ID, v.Loaded, v.Passed(), len(v.Checks), v.ErrorCount, v.InitMS, v.FrameUS, v.BaseFrameUS, v.ScrollCPUMS, v.BaseCPUMS,
					v.JSHeapKB, v.IdleDeliveries, v.IdleWakeups, v.BaseWakeups, v.IdleCPUMS, v.BaseIdleCPUMS)
				for _, c := range v.Checks {
					t.Logf("    [%v] %s %s", c.Pass, c.Name, c.Detail)
				}
				for _, l := range v.Errors {
					t.Logf("    console: %s", l)
				}
			}
			if out != "" {
				b, _ := json.MarshalIndent(struct {
					pluginbench.Plugin
					Sources []string `json:"sources"`
				}{p, sources}, "", "  ")
				if err := os.WriteFile(filepath.Join(out, libNameKey(name)+".json"), b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// benchVariant runs one variant in a fresh client.
func benchVariant(t *testing.T, ix *pluginlib.Index, entry pluginlib.Entry, sc benchScenario, shots string) (pluginbench.Variant, *image.RGBA) {
	v := pluginbench.Variant{System: entry.System, ID: entry.ID, SHA256: entry.SHA256, Size: entry.Size}
	if entry.Main != nil {
		v.Size += entry.Main.Size
	}
	logs := &benchLog{}
	prev := log.Writer()
	log.SetOutput(io.MultiWriter(prev, logs))
	defer log.SetOutput(prev)

	fs, err := fakeslack.Start(benchSize)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	e := benchStart(t, fs)
	defer func() {
		if e.a.data != nil {
			e.a.data.stopTaut()
		}
		e.shutdown()
	}()
	e.waitReady()
	e.until("Taut and Slick", 60*time.Second, func() bool { return e.a.Taut() != nil && e.a.Slick() != nil })
	e.tt = e.a.Taut()
	sl := e.a.Slick()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// the runtimes up (a no-op plugin in each), so Load is the plugin's own
	if _, err := e.tt.Import(ctx, synthTaut("BenchWarm", "Bench warm-up", "no-op", "other", false), ""); err != nil {
		t.Fatal(err)
	}
	if err := e.tt.SetEnabled(ctx, "BenchWarm", true); err != nil {
		t.Fatal(err)
	}
	if _, err := sl.Import(ctx, synthSlick("BenchWarmSlick", "Bench warm-up", "no-op", "app")); err != nil {
		t.Fatal(err)
	}
	if err := sl.SetEnabled(ctx, "BenchWarmSlick", true); err != nil {
		t.Fatal(err)
	}

	e.settle(300 * time.Millisecond)
	slackc.SetBridge(countingBridge{e.tt.Bridge(), &e.calls}) // the runtime is up: Attached has run
	e.js(jsProbe)
	e.openBench()
	e.loadAll()
	t.Logf("%s %s: %d messages loaded", entry.System, entry.ID, e.loadedMessages(fakeslack.BenchChannelID))
	base := e.scrollPass()
	v.BaseFrameUS, v.BaseCPUMS, v.BaseAllocs = base.median, base.cpu, base.allocs
	_, v.BaseWakeups, v.BaseIdleCPUMS = e.idle(2 * time.Second)
	heap0, goHeap0 := e.jsHeap(), liveHeapMB()
	patched0 := slackc.PatchedNames()
	_ = logs.take() // the client's own start

	if sc.before != nil {
		sc.before(e)
	}
	lib := e.a.pluginLib()
	if lib == nil {
		t.Fatal("no plugin library")
	}
	if _, err := lib.Index(ctx, true); err != nil {
		t.Fatal(err)
	}
	code, main, err := lib.Download(ctx, entry)
	if err != nil {
		t.Fatalf("download %s: %v", entry.File.File, err)
	}
	page := newLibraryPage(e.a, nil)
	h, ok := page.host(entry.System)
	if !ok {
		t.Fatalf("no %s host", entry.System)
	}
	t0 := time.Now()
	if err := h.install(ctx, entry, code, main, false); err != nil {
		v.StartError = "install: " + err.Error()
	}
	if v.StartError == "" {
		for k, val := range sc.opts[entry.System] {
			if m, ok := val.(map[string]any); ok { // "__AUTHOR__" keys name the first bench author
				r := map[string]any{}
				for kk, vv := range m {
					r[strings.ReplaceAll(kk, "__AUTHOR__", e.c.Authors[0])] = vv
				}
				val = r
			}
			var err error
			if entry.System == pluginlib.Taut {
				err = e.tt.SetOption(ctx, entry.ID, k, val)
			} else {
				err = sl.SetOption(ctx, entry.ID, k, val)
			}
			if err != nil {
				t.Logf("option %s: %v", k, err)
			}
		}
		if entry.System == pluginlib.Taut {
			err = e.tt.SetEnabled(ctx, entry.ID, true)
		} else {
			err = sl.SetEnabled(ctx, entry.ID, true)
		}
		if err != nil {
			v.StartError = err.Error()
		}
	}
	running := func() (bool, string) {
		if entry.System == pluginlib.Taut {
			for _, c := range e.tt.Settings().Categories {
				for _, p := range c.Plugins {
					if p.ID == entry.ID {
						return p.Running, p.Error
					}
				}
			}
			return false, "not installed"
		}
		for _, p := range sl.Settings().Plugins {
			if p.ID == entry.ID {
				return p.Running, p.StartError
			}
		}
		return false, "not installed"
	}
	dl := time.Now().Add(10 * time.Second)
	for v.StartError == "" {
		r, why := running()
		if r {
			v.Loaded = true
			break
		}
		if why != "" || time.Now().After(dl) {
			v.StartError = orStr(why, "didn't start in 10 s")
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	v.InitMS = float64(time.Since(t0).Microseconds()) / 1000
	e.settle(800 * time.Millisecond)
	for _, n := range slackc.PatchedNames() {
		if !slices.Contains(patched0, n) {
			v.Patched = append(v.Patched, n)
		}
	}

	if v.Loaded {
		for _, c := range sc.checks {
			for e.a.dismissTop() {
			}
			e.openBench()
			pass, detail := c.run(e)
			v.Checks = append(v.Checks, pluginbench.Check{Name: c.name, Pass: pass, Detail: detail})
		}
		for e.a.dismissTop() {
		}
		e.a.CloseSecondary()
		e.openBench()
		st := e.scrollPass()
		v.FrameUS, v.FrameP95US, v.ScrollCPUMS, v.AllocsFrame = st.median, st.p95, st.cpu, st.allocs
		const idleFor = 3 * time.Second
		v.IdleDeliveries, v.IdleWakeups, v.IdleCPUMS = e.idle(idleFor)
		v.IdleSeconds = idleFor.Seconds()
		v.JSHeapKB = math.Max(0, e.jsHeap()-heap0)
		v.GoHeapKB = math.Max(0, (liveHeapMB()-goHeap0)*1024)
	} else {
		for _, c := range sc.checks {
			v.Checks = append(v.Checks, pluginbench.Check{Name: c.name, Detail: "didn't load"})
		}
	}
	e.mu.Lock()
	v.Roots = 0
	for r := range e.trees {
		if r < 1<<40 {
			v.Roots++
		}
	}
	e.mu.Unlock()

	// console lines, numbers folded so repeats count once
	seen := map[string]bool{}
	for _, l := range logs.take() {
		v.ErrorCount++
		k := benchDigits.ReplaceAllString(l, "#")
		if !seen[k] && len(v.Errors) < 8 {
			seen[k] = true
			if len(l) > 300 {
				l = l[:300] + "…"
			}
			v.Errors = append(v.Errors, l)
		}
	}

	var img *image.RGBA
	if shots != "" && v.Loaded {
		for e.a.dismissTop() {
		}
		e.a.CloseSecondary()
		e.openBench()
		e.toBottom()
		if sc.scene != nil {
			sc.scene(e)
		}
		v.Shot = "bench-" + libNameKey(entry.Name) + "-" + entry.System + ".png"
		img = e.shot(shots, strings.TrimSuffix(v.Shot, ".png"))
	}
	return v, img
}

// pixelDiff is the share of pixels whose channels differ by more than a
// little (antialiasing noise stays out).
func pixelDiff(a, b *image.RGBA) float64 {
	r := a.Bounds().Intersect(b.Bounds())
	if r.Empty() {
		return 1
	}
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i, j := a.PixOffset(x, y), b.PixOffset(x, y)
			for c := 0; c < 3; c++ {
				d := int(a.Pix[i+c]) - int(b.Pix[j+c])
				if d > 24 || d < -24 {
					n++
					break
				}
			}
		}
	}
	return float64(n) / float64(r.Dx()*r.Dy())
}

var compName = regexp.MustCompile(`"n":"([A-Za-z0-9_]+)"`)

// idleShapes names the components in the delivered trees (diagnostics
// for a variant that keeps re-rendering).
func idleShapes(trees map[uint64][]byte) string {
	count := map[string]int{}
	for _, tr := range trees {
		if m := compName.FindSubmatch(tr); m != nil {
			count[string(m[1])]++
		}
	}
	var out []string
	for k, n := range count {
		out = append(out, fmt.Sprintf("%s×%d", k, n))
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

// benchCalls counts what the UI asks of the plugin runtime (diagnostics:
// whether an idle re-render starts in Go or in JS).
type benchCalls struct {
	render, renderIn, renderFn, rects, dispatch, unmount atomic.Int64

	mu       sync.Mutex
	unmapped map[uint64]bool // roots unmounted (ids are never reused)
}

func (c *benchCalls) gone(root uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.unmapped[root]
}

type benchCallCounts [6]int64

func (c *benchCalls) snapshot() benchCallCounts {
	return benchCallCounts{c.render.Load(), c.renderIn.Load(), c.renderFn.Load(), c.rects.Load(), c.dispatch.Load(), c.unmount.Load()}
}

func (c *benchCalls) since(b benchCallCounts) string {
	n := c.snapshot()
	return fmt.Sprintf("Render %d RenderIn %d RenderFn %d SetRects %d Dispatch %d Unmount %d",
		n[0]-b[0], n[1]-b[1], n[2]-b[2], n[3]-b[3], n[4]-b[4], n[5]-b[5])
}

// countingBridge is the plugin bridge, counted.
type countingBridge struct {
	*taut.Bridge
	c *benchCalls
}

func (b countingBridge) Render(root uint64, name string, props []byte) {
	b.c.render.Add(1)
	b.Bridge.Render(root, name, props)
}

func (b countingBridge) RenderIn(root, parent uint64, parentK int64, name string, props []byte) {
	b.c.renderIn.Add(1)
	b.Bridge.RenderIn(root, parent, parentK, name, props)
}

func (b countingBridge) RenderFn(root uint64, fn int64, args []byte) {
	b.c.renderFn.Add(1)
	b.Bridge.RenderFn(root, fn, args)
}

func (b countingBridge) SetRects(r map[int64][4]float32) {
	b.c.rects.Add(1)
	b.Bridge.SetRects(r)
}

func (b countingBridge) Dispatch(target int64, ev []byte) {
	b.c.dispatch.Add(1)
	b.Bridge.Dispatch(target, ev)
}

func (b countingBridge) Unmount(root uint64) {
	b.c.unmount.Add(1)
	b.c.mu.Lock()
	if b.c.unmapped == nil {
		b.c.unmapped = map[uint64]bool{}
	}
	b.c.unmapped[root] = true
	b.c.mu.Unlock()
	b.Bridge.Unmount(root)
}

// jsProbe counts, in JS, the mirror's invalidations by slice, redux
// refreshes and store dispatches (diagnostics for idle re-renders).
const jsProbe = `(() => {
  const T = globalThis.__taut
  if (!T || globalThis.__bench) return
  const b = (globalThis.__bench = { inv: {}, refresh: 0, act: {} })
  const inv = T.invalidate
  T.invalidate = (changes) => {
    for (const c of changes || []) {
      const k = c.slice + (c.keys && c.keys.length ? '[' + c.keys.length + ']' : '')
      b.inv[k] = (b.inv[k] || 0) + 1
    }
    return inv(changes)
  }
  const rf = T.redux.refresh
  T.redux.refresh = (...a) => { b.refresh++; return rf(...a) }
  T.refresh = T.redux.refresh
  const st = T.redux.getStore()
  const d = st.dispatch
  st.dispatch = (a) => {
    const k = typeof a === 'function' ? 'thunk' : (a && a.type) || '?'
    b.act[k] = (b.act[k] || 0) + 1
    return d(a)
  }
})()`

func (e *benchEnv) js(src string) string {
	rt, err := e.tt.JS()
	if err != nil || rt == nil {
		return ""
	}
	v, err := rt.Eval("bench-probe.js", src)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprint(v)
}
