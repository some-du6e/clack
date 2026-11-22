package ui

// Other people's reactions in a channel draw as they come (Karim,
// 2026-11-19: "i think reactions are not syncing", "in a channel for
// another"). Slack's reaction_added and reaction_removed handlers
// (boot-render REACTIONS-SHARED-RTM, reactionRemoved) dispatch
// addReaction / removeReaction (boot-shared cBAo, yzJf) on the reactions
// store under the message's reaction key, whether or not the message is
// loaded, and every surface draws that store: the channel's row, the
// thread pane's root and replies. Here other people react over the
// socket to a plain message, a thread root, a reply in the open thread
// pane, a file message and a message the viewer reacted to too, in the
// channel on screen and in one not open; each change must draw within two
// frames of the socket bringing it (frames drawn as a window draws them:
// when the app asks), and a history refresh, a re-open and a socket
// reconnect must neither lose a reaction nor bring back a removed one.
// Natively, under a plugin patching the components a message and its
// reactions draw through, and under one patching the messages slice.
// Synthetic data only.

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// reactPassComponents patches what a message and its reactions draw
// through and passes it through.
const reactPassComponents = `(() => {
const TautPlugin = globalThis.TautPlugin
class ReactPassComponents extends TautPlugin {
  static id = 'ReactPassComponents'
  static pluginName = 'React pass components'
  static description = 'Patches the message and reaction components, changing nothing'
  static category = 'other'
  static authors = ['test']
  static defaultConfig = { enabled: false }
  start() {
    for (const name of ['ReactionBar', 'Reaction', 'MessageWrapper', 'Blocks', 'MessageText']) {
      this.api.patchComponent(name, (Original) => (props) => React.createElement(Original, props))
    }
  }
  stop() {}
}
return ReactPassComponents
})()`

// reactPassSlice patches the messages slice and passes it through.
const reactPassSlice = `(() => {
const TautPlugin = globalThis.TautPlugin
class ReactPassSlice extends TautPlugin {
  static id = 'ReactPassSlice'
  static pluginName = 'React pass slice'
  static description = 'Patches the messages slice, changing nothing'
  static category = 'other'
  static authors = ['test']
  static defaultConfig = { enabled: false }
  start() { this.api.redux.patchSlice('messages', (ch, bucket) => bucket) }
  stop() {}
}
return ReactPassSlice
})()`

// reactSliceText patches the messages slice by marking each message's
// text, as Message Logger's edit marks do: every row draws the plugins'
// view of its message, not core's.
const reactSliceText = `(() => {
const TautPlugin = globalThis.TautPlugin
class ReactSliceText extends TautPlugin {
  static id = 'ReactSliceText'
  static pluginName = 'React slice text'
  static description = 'Marks every message text, patching the messages slice'
  static category = 'other'
  static authors = ['test']
  static defaultConfig = { enabled: false }
  start() { this.api.messages.patchMessageText((s) => (typeof s === 'string' && s ? s + ' (logged)' : s)) }
  stop() {}
}
return ReactSliceText
})()`

// reactLogged is the mark reactSliceText adds to a message's text.
const reactLogged = " (logged)"

// pillKey is a reaction pill: its message, its reaction, its surface.
type pillKey struct {
	ts, name string
	pane     bool
}

// pillState is what a pill drew.
type pillState struct {
	count int
	self  bool
}

// pillWant is a reaction as a surface must draw it (count 0: no pill).
type pillWant struct {
	ts, name string
	pane     bool
	count    int
	self     bool
}

func (w pillWant) String() string {
	where := "channel"
	if w.pane {
		where = "pane"
	}
	if w.count == 0 {
		return fmt.Sprintf("%s %s:%s gone", where, w.ts, w.name)
	}
	return fmt.Sprintf("%s %s:%s ×%d self=%v", where, w.ts, w.name, w.count, w.self)
}

func TestChannelReactionsLive(t *testing.T) {
	if os.Getenv("CLACK_LATENCY_TESTS") == "" {
		t.Skip("CLACK_LATENCY_TESTS not set: flake-check's reactions-live job runs this")
	}
	for _, mode := range []string{"native", "components", "slice", "slice-text"} {
		t.Run(mode, func(t *testing.T) { channelReactionsLive(t, mode) })
	}
}

func channelReactionsLive(t *testing.T, mode string) {
	sz := fakeslack.Small
	sz.Authors = 20
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	chs, ids := fs.ChannelIDs(), fs.UserIDs()
	ch, away := chs[1], chs[2]
	author, replier := ids[2], ids[3]
	r1, r2 := ids[4], ids[5] // the people who react
	plain, _ := fs.PostQuiet(ch, author, "a plain message to react to")
	root, _ := fs.PostQuiet(ch, author, "a thread root to react to")
	reply := fs.PostReplyQuiet(ch, root, replier, "a reply to react to")
	file := fs.PostFileQuiet(ch, author, "the outage report")
	mine, _ := fs.PostQuiet(ch, author, "a message the viewer reacted to")
	fs.ReactQuiet(ch, mine, "eyes", fakeslack.SelfID)
	elsewhere, _ := fs.PostQuiet(away, author, "a message in a channel not open")

	var e *perfEnv
	switch mode {
	case "native":
		e = startEnv(t, fs, t.TempDir())
	default:
		e = benchStart(t, fs).perfEnv
	}
	e.waitReady()
	c := e.a.data.core
	c.SetRealtime(true)
	e.until("the socket", 20*time.Second, func() bool { return c.RTM != nil && c.RTM.Connected() })
	if mode != "native" {
		e.until("Taut", 60*time.Second, func() bool { return e.a.Taut() != nil })
		tt := e.a.Taut()
		code, id := reactPassComponents, "ReactPassComponents"
		switch mode {
		case "slice":
			code, id = reactPassSlice, "ReactPassSlice"
		case "slice-text":
			code, id = reactSliceText, "ReactSliceText"
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := tt.Import(ctx, []byte(code), ""); err != nil {
			t.Fatal(err)
		}
		if err := tt.SetEnabled(ctx, id, true); err != nil {
			t.Fatal(err)
		}
		e.until("the plugin's patches", 30*time.Second, func() bool {
			if mode == "slice" || mode == "slice-text" {
				return tt.SlicePatched("messages")
			}
			return slackc.Patched("Reaction") && slackc.Patched("ReactionBar") && slackc.Patched("MessageWrapper")
		})
	}

	// what each frame draws: the pills (messages.ReactionDrawnHook), and
	// whether the app asked for a frame since the last one
	var pmu sync.Mutex
	pills := map[pillKey]pillState{}
	messages.ReactionDrawnHook = func(_, ts, name string, count int, self, thread bool) {
		pmu.Lock()
		pills[pillKey{ts, name, thread}] = pillState{count, self}
		pmu.Unlock()
	}
	t.Cleanup(func() { messages.ReactionDrawnHook = nil })
	// slice-text: the bodies a frame draws, the plugins' marked texts
	var bodies []string
	if mode == "slice-text" {
		richview.DrawnHook = func(d richtext.Doc) {
			pmu.Lock()
			bodies = append(bodies, richtext.PlainText(d))
			pmu.Unlock()
		}
		t.Cleanup(func() { richview.DrawnHook = nil })
	}
	// the messages' texts as posted (slice-text: drawn marked)
	texts := map[string]string{plain: "a plain message to react to", root: "a thread root to react to", reply: "a reply to react to",
		file: "the outage report", mine: "a message the viewer reacted to", elsewhere: "a message in a channel not open"}
	var frameBodies []string
	var asked atomic.Bool
	prevHook := invalidateHook
	invalidateHook = func(*App) { asked.Store(true) }
	t.Cleanup(func() { invalidateHook = prevHook })

	var frames int64
	var lastFrame time.Time
	const vsync = time.Second / 60
	// frame draws one frame and returns its pills
	frame := func() map[pillKey]pillState {
		if w := time.Until(lastFrame.Add(vsync)); w > 0 {
			time.Sleep(w)
		}
		lastFrame = time.Now()
		pmu.Lock()
		clear(pills)
		bodies = bodies[:0]
		pmu.Unlock()
		e.frame()
		frames++
		pmu.Lock()
		defer pmu.Unlock()
		frameBodies = append(frameBodies[:0], bodies...)
		out := make(map[pillKey]pillState, len(pills))
		for k, v := range pills {
			out[k] = v
		}
		return out
	}
	// drew reports whether the surface drew the row of ts in the last frame
	drew := func(ts string, pane bool) bool {
		if pane {
			tv := e.threadView(ch, root)
			if tv == nil {
				return false
			}
			_, _, ok := tv.RowText(ts)
			return ok
		}
		cv := e.a.convos[ch+"/"]
		if cv == nil {
			return false
		}
		l, ok := cv.list.(channelList)
		if !ok {
			return false
		}
		for _, r := range l.VisibleRows() {
			if r.Key == ts {
				return true
			}
		}
		return false
	}
	// drewText reports whether the last frame drew a body with s
	drewText := func(s string) bool {
		for _, b := range frameBodies {
			if strings.Contains(b, s) {
				return true
			}
		}
		return false
	}
	// viewsDrawn (slice-text) runs frames until the rows of tss draw the
	// plugins' views of their messages (taut converts a newly shown row's
	// view a moment after it first draws core's copy), so each change is
	// measured on a row already drawing the view
	viewsDrawn := func(what string, tss ...string) {
		t.Helper()
		if mode != "slice-text" {
			return
		}
		for dl := time.Now().Add(15 * time.Second); time.Now().Before(dl); time.Sleep(5 * time.Millisecond) {
			frame()
			ok := true
			for _, ts := range tss {
				ok = ok && drewText(texts[ts]+reactLogged)
			}
			if ok {
				return
			}
		}
		// which row, drawing what, and what taut had for it: the pane's
		// rows have flaked here on loaded runners (2 of 4, 4 of 6 runs)
		var got []string
		tv := e.threadView(ch, root)
		for _, ts := range tss {
			drawn := "not drawn"
			switch {
			case drewText(texts[ts] + reactLogged):
				drawn = "marked"
			case drewText(texts[ts]):
				drawn = "unmarked"
			}
			pane := "no pane row"
			if tv != nil {
				if txt, _, ok := tv.RowText(ts); ok {
					pane = fmt.Sprintf("pane row %q", txt)
				}
			}
			view := "no taut"
			if tt := e.a.Taut(); tt != nil {
				m, _, ready, ok := tt.MessageViewState(ch, ts, false, 0)
				view = fmt.Sprintf("view ok %v ready %v %q", ok, ready, m.Text)
			}
			got = append(got, fmt.Sprintf("%s: %s, %s, %s", ts, drawn, pane, view))
		}
		t.Errorf("%s: the rows never drew the plugins' views (%s)", what, strings.Join(got, "; "))
	}
	holds := func(ps map[pillKey]pillState, ws []pillWant) bool {
		for _, w := range ws {
			if !drew(w.ts, w.pane) {
				return false
			}
			got, ok := ps[pillKey{w.ts, w.name, w.pane}]
			if w.count == 0 {
				if ok {
					return false
				}
				continue
			}
			if !ok || got.count != w.count || got.self != w.self {
				return false
			}
			if mode == "slice-text" && !drewText(texts[w.ts]+reactLogged) {
				return false // the pill, but on a row not drawing the plugins' view
			}
		}
		return true
	}
	describe := func(ps map[pillKey]pillState) string {
		var out []string
		for k, v := range ps {
			out = append(out, fmt.Sprintf("%s:%s(pane %v)×%d self=%v", k.ts, k.name, k.pane, v.count, v.self))
		}
		sort.Strings(out)
		return strings.Join(out, " ")
	}

	// step sends one reaction over the socket and runs frames as the
	// window asks for them until every want draws; it fails a change
	// drawn more than two frames after the socket brought it
	var lat []string
	late := 0
	step := func(what string, act func() bool, ws ...pillWant) {
		t.Helper()
		pushedN, pushed := frames, time.Now()
		if !act() {
			t.Fatalf("%s: the fake lost the message", what)
		}
		var last map[pillKey]pillState
		for dl := time.Now().Add(5 * time.Second); time.Now().Before(dl); {
			now := time.Now()
			wake, ok := e.r.WakeupTime()
			if !asked.Swap(false) && !(ok && !wake.After(now)) {
				time.Sleep(time.Millisecond)
				continue
			}
			last = frame()
			if holds(last, ws) {
				n := frames - pushedN
				lat = append(lat, fmt.Sprintf("%s %dms/%df", what, time.Since(pushed).Milliseconds(), n))
				if n > 2 {
					late++
					t.Errorf("%s, %s: drawn %d frames (%v) after the socket brought it, want at most two", mode, what, n, time.Since(pushed).Round(time.Millisecond))
				}
				return
			}
		}
		t.Errorf("%s, %s: never drawn as %v; the last frame drew %s", mode, what, ws, describe(last))
	}
	// settled checks ws hold after frames drawn for a while (no budget)
	settled := func(what string, ws ...pillWant) {
		t.Helper()
		var last map[pillKey]pillState
		for dl := time.Now().Add(15 * time.Second); time.Now().Before(dl); time.Sleep(5 * time.Millisecond) {
			last = frame()
			if holds(last, ws) {
				return
			}
		}
		t.Errorf("%s, %s: want %v; the last frame drew %s", mode, what, ws, describe(last))
	}
	react := func(ts, name, user string, add bool) func() bool {
		return func() bool { return fs.ReactLive(ch, ts, name, user, add) }
	}
	quiet := func() {
		e.settle(500 * time.Millisecond)
		e.blur()
		asked.Store(false)
	}
	reopen := func(target string) {
		e.unblur()
		e.openChannel(target)
		e.until(target+"'s history", 20*time.Second, func() bool { return e.loadedMessages(target) > 0 })
		e.toLatest()
	}

	// the channel on screen
	reopen(ch)
	settled("the channel", pillWant{ts: mine, name: "eyes", count: 1, self: true})
	viewsDrawn("the channel", plain, root, file, mine)
	quiet()
	step("someone's reaction on a plain message", react(plain, "tada", r1, true), pillWant{ts: plain, name: "tada", count: 1})
	step("another person's same reaction", react(plain, "tada", r2, true), pillWant{ts: plain, name: "tada", count: 2})
	step("one of them takes it back", react(plain, "tada", r1, false), pillWant{ts: plain, name: "tada", count: 1})
	step("the other too", react(plain, "tada", r2, false), pillWant{ts: plain, name: "tada"})
	step("a reaction on a thread root", react(root, "tada", r1, true), pillWant{ts: root, name: "tada", count: 1})
	step("a reaction on a file message", react(file, "tada", r1, true), pillWant{ts: file, name: "tada", count: 1})
	step("someone's on the viewer's reaction", react(mine, "eyes", r1, true), pillWant{ts: mine, name: "eyes", count: 2, self: true})
	step("they take it back", react(mine, "eyes", r1, false), pillWant{ts: mine, name: "eyes", count: 1, self: true})

	// the thread open beside it: a reply's, and the root's in both
	e.unblur()
	e.a.OpenThread(ch, root)
	settled("the thread pane", pillWant{ts: root, name: "tada", pane: true, count: 1})
	viewsDrawn("the thread pane", root, reply)
	quiet()
	step("a reaction on a reply in the open pane", react(reply, "tada", r1, true), pillWant{ts: reply, name: "tada", pane: true, count: 1})
	step("a reaction on the root, both surfaces", react(root, "heart", r2, true),
		pillWant{ts: root, name: "heart", count: 1}, pillWant{ts: root, name: "heart", pane: true, count: 1})
	t.Logf("%s: %d changes, %d late: %s", mode, len(lat), late, strings.Join(lat, "; "))

	// a channel not open when the reaction comes, and the open one left
	e.unblur()
	e.a.CloseSecondary()
	if !fs.ReactLive(away, elsewhere, "tada", r1, true) {
		t.Fatal("the fake lost the other channel's message")
	}
	e.settle(300 * time.Millisecond)
	reopen(away)
	awayDrew := func() bool {
		cv := e.a.convos[away+"/"]
		if cv == nil {
			return false
		}
		_, ok := frame()[pillKey{elsewhere, "tada", false}]
		return ok
	}
	if !e.untilSoft(15*time.Second, awayDrew) {
		t.Errorf("%s: a reaction made while its channel wasn't open never drew once it opened", mode)
	}
	if !fs.ReactLive(ch, plain, "smile", r1, true) {
		t.Fatal("the fake lost the message")
	}
	e.settle(300 * time.Millisecond)
	reopen(ch)
	final := []pillWant{
		{ts: plain, name: "smile", count: 1}, {ts: plain, name: "tada"},
		{ts: root, name: "tada", count: 1}, {ts: root, name: "heart", count: 1},
		{ts: file, name: "tada", count: 1}, {ts: mine, name: "eyes", count: 1, self: true},
	}
	settled("a reaction made while the channel was left, on re-opening it", final...)

	// what the server holds since: a refresh, a re-open and a reconnect
	// keep every reaction and bring back none that was removed
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := c.Refresh(ctx, ch); err != nil {
		t.Fatal(err)
	}
	e.ownSendList(ch).Refresh()
	e.settle(time.Second)
	settled("after a history refresh", final...)
	reopen(away)
	reopen(ch)
	settled("after a re-open", final...)
	ws := fs.CallCount("client.getWebSocketURL")
	c.RTM.Reconnect("test: the machine woke")
	e.until("the socket back", 20*time.Second, func() bool {
		return fs.CallCount("client.getWebSocketURL") > ws && c.RTM.Connected()
	})
	e.settle(2 * time.Second)
	settled("after a socket reconnect", final...)
}
