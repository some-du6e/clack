package composer

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// wrapPaneSend wraps the channel composer's prepareAndSendMessage
// (MessagePaneInput), as SlimMessageBox and bChannel do; __wrapMode
// decides what the wrapper does with a send: "pass" calls the composer
// back with the opts it got, "hold" throws (the message stays), and
// "sendThenThrow" calls back, then throws.
const wrapPaneSend = `(() => {
const TautPlugin = globalThis.TautPlugin
class WrapPaneSend extends TautPlugin {
  static id = 'WrapPaneSend'
  static pluginName = 'Wrap pane send (test)'
  static description = 'test: a send wrapper around the channel composer'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    const R = globalThis.React
    globalThis.__wrapMode = 'pass'
    globalThis.__setWrapMode = (m) => { globalThis.__wrapMode = m }
    this.api.patchComponent('MessagePaneInput', (Original) => (props) =>
      R.createElement(Original, { ...props, prepareAndSendMessage: (o) => {
        const m = globalThis.__wrapMode
        if (m === 'hold') throw new Error('held back')
        props.prepareAndSendMessage(o)
        if (m === 'sendThenThrow') throw new Error('after sending')
      } }))
  }
  stop() {}
}
return WrapPaneSend
})()`

// paneSend is the channel composer with wrapPaneSend's wrapper, drawn by
// frame, and the plugins' runtime.
type paneSend struct {
	e     *slickEnv
	c     *Composer
	frame func()
	rt    *jsrt.Runtime
}

func newPaneSend(t *testing.T) *paneSend {
	t.Helper()
	e := startTautCode(t, []byte(wrapPaneSend), nil, nil)
	e.patched("MessagePaneInput")
	clearDraft(tChGen, "")
	c := New(e.h, tChGen, "")
	t.Cleanup(func() { c.Close(); clearDraft(tChGen, "") })
	ops, r := new(op.Ops), new(input.Router)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Min: image.Pt(600, 0), Max: image.Pt(600, 400)}, Now: time.Now(), Source: r.Source()}
		slackc.Frame(gtx)
		c.LayoutIn(gtx, tChGen)
		r.Frame(ops)
	}
	e.until(10*time.Second, "the plugin's prepareAndSendMessage", frame, func() bool { return c.sendJS.ID != 0 })
	rt, err := e.tt.JS()
	if err != nil || rt == nil {
		t.Fatalf("no JS runtime: %v", err)
	}
	t.Cleanup(func() { _ = rt.Do(func(*jsrt.Ctx) error { return nil }) })
	return &paneSend{e: e, c: c, frame: frame, rt: rt}
}

func (p *paneSend) mode(t *testing.T, m string) {
	t.Helper()
	if _, err := p.rt.Call(context.Background(), "__setWrapMode", m); err != nil {
		t.Fatal(err)
	}
}

// submit puts text in the composer and presses Enter.
func (p *paneSend) submit(text string) {
	d := NewDoc()
	d.Insert(0, text, Attr{})
	p.c.ed.SetDoc(d)
	p.c.submit()
}

func (p *paneSend) posted(text string) bool {
	for _, f := range p.e.slack.called("chat.postMessage") {
		if strings.Contains(f.Get("text"), text) || strings.Contains(f.Get("blocks"), text) {
			return true
		}
	}
	return false
}

func (p *paneSend) posts(text string) int {
	n := 0
	for _, f := range p.e.slack.called("chat.postMessage") {
		if strings.Contains(f.Get("text"), text) {
			n++
		}
	}
	return n
}

// Each message a wrapped send decides on waits in a slot of its own until
// the plugin's call comes back with it: two sends behind a JS backlog both
// go out, each with its own text. One slot for all gave the second send's
// call the first message and lost the first (SlimMessageBox's thread
// replies).
func TestSendViaJSKeepsEachMessage(t *testing.T) {
	p := newPaneSend(t)
	p.submit("warm")
	p.e.until(10*time.Second, "the warm send", p.frame, func() bool { return p.posted("warm") })
	jsBacklog(p.rt, 400*time.Millisecond)
	p.submit("the first of two")
	p.submit("the second of two")
	p.e.until(15*time.Second, "both messages posted", p.frame, func() bool {
		return p.posted("the first of two") && p.posted("the second of two")
	})
	if n := p.posts("the first of two"); n != 1 {
		t.Errorf("the first message went out %d times", n)
	}
}

// The wrapped send's JS call is the user's (taut's Bridge.Call): it runs
// ahead of the JS work queued before it, as a send's checks do.
func TestSendViaJSOnTheUserLane(t *testing.T) {
	p := newPaneSend(t)
	p.submit("warm")
	p.e.until(10*time.Second, "the warm send", p.frame, func() bool { return p.posted("warm") })
	const backlog = time.Second
	jsBacklog(p.rt, backlog)
	t0 := time.Now()
	p.submit("ahead of the backlog")
	p.e.until(15*time.Second, "the wrapped send", p.frame, func() bool { return p.posted("ahead of the backlog") })
	d := time.Since(t0)
	t.Logf("the wrapped send went out %v after Enter, %v of JS work queued before it", d.Round(time.Millisecond), backlog)
	if d > backlog/2 {
		t.Errorf("the wrapped send went out %v after Enter: it waited for the %v of JS work queued before it", d.Round(time.Millisecond), backlog)
	}
}

// A send the wrapper holds back puts the message back with its GIFs; when
// the user typed a new message meanwhile, neither goes back, so the GIFs
// don't go out with the new message.
func TestSendViaJSHeldBackPutsTheMessageBack(t *testing.T) {
	p := newPaneSend(t)
	p.mode(t, "hold")
	p.c.att.addGif(testGif("a").Selected(true))
	p.submit("held back")
	if n := p.c.att.gifCount(); n != 0 {
		t.Fatalf("%d GIFs in the composer after Enter, want 0", n)
	}
	p.e.until(10*time.Second, "the held-back message back in the composer", p.frame, func() bool {
		return strings.Contains(p.c.ed.Doc().Text(), "held back") && p.c.att.gifCount() == 1
	})

	// the user types a new message before the wrapper's answer comes
	p.c.ed.SetDoc(NewDoc())
	p.c.att.addGif(testGif("b").Selected(true))
	jsBacklog(p.rt, 400*time.Millisecond)
	p.submit("held back again")
	p.c.ed.InsertText("a new message")
	for dl := time.Now().Add(3 * time.Second); time.Now().Before(dl); time.Sleep(20 * time.Millisecond) {
		p.frame()
	}
	if text := p.c.ed.Doc().Text(); !strings.Contains(text, "a new message") || strings.Contains(text, "held back again") {
		t.Errorf("the composer reads %q, want the new message alone", text)
	}
	if n := p.c.att.gifCount(); n != 0 {
		t.Errorf("%d GIFs went back into the new message, want 0", n)
	}
}

// An error the wrapper throws after it passed the message on leaves the
// message sent: nothing goes back into the composer.
func TestSendViaJSErrorAfterSendKeepsItSent(t *testing.T) {
	p := newPaneSend(t)
	p.mode(t, "sendThenThrow")
	p.submit("sent, then the plugin threw")
	p.e.until(10*time.Second, "the message posted", p.frame, func() bool { return p.posted("sent, then the plugin threw") })
	for i := 0; i < 20; i++ {
		p.frame()
		time.Sleep(10 * time.Millisecond)
	}
	if text := strings.TrimSpace(p.c.ed.Doc().Text()); text != "" {
		t.Errorf("the composer reads %q after the message went out", text)
	}
	if n := p.posts("sent, then the plugin threw"); n != 1 {
		t.Errorf("the message went out %d times", n)
	}
}

// An edit's send transforms are the user's, as a send's are
// (taut.UserAction): they run ahead of the JS work queued before them,
// and the edit is saved without waiting for it.
func TestEditOnTheUserLane(t *testing.T) {
	app, fs := syntheticApp(t)
	stubTyping(t)
	tt := pluginTaut(t, app, `start() { this.off = this.api.onMessageSendBlocks((b) => b) }
  stop() { this.off?.() }`)
	waitFor(t, 10*time.Second, "the plugin's send transform", func() bool { return tt.HasSendTransforms() })
	rt, err := tt.JS()
	if err != nil || rt == nil {
		t.Fatalf("no JS runtime: %v", err)
	}
	t.Cleanup(func() { _ = rt.Do(func(*jsrt.Ctx) error { return nil }) })
	h := &slickHost{app: app, th: theme.New(false)}
	edit := func(ts, text string) time.Duration {
		c := NewEdit(h, core.Message{Channel: tChGen, TS: ts, Text: "before"}, nil)
		defer c.Close()
		d := NewDoc()
		d.Insert(0, text, Attr{})
		c.ed.SetDoc(d)
		n := len(fs.called("chat.update"))
		t0 := time.Now()
		c.submit()
		for dl := time.Now().Add(10 * time.Second); len(fs.called("chat.update")) == n; time.Sleep(2 * time.Millisecond) {
			if time.Now().After(dl) {
				t.Fatalf("the edit %q was never saved", text)
			}
		}
		return time.Since(t0)
	}
	edit("1700000000.000100", "warm")
	const backlog = time.Second
	jsBacklog(rt, backlog)
	d := edit("1700000000.000200", "saved ahead of the backlog")
	t.Logf("the edit was saved %v after Enter, %v of JS work queued before it", d.Round(time.Millisecond), backlog)
	if d > backlog/2 {
		t.Errorf("the edit was saved %v after Enter: its transforms waited for the %v of JS work queued before it", d.Round(time.Millisecond), backlog)
	}
}

// What the plugin runtime was handed before a send (a socket event for
// rtm.on, a store update) reaches the plugins before the send's checks
// run, however much rendering is queued: the send's call runs ahead of the
// renders, never ahead of the runtime's earlier events (runtime.post's
// in-order posts). A send check reading a message the socket just brought
// saw the store as it was before it.
func TestRuntimePostsStayAheadOfASend(t *testing.T) {
	c, fs, _ := composerEnv(t, nil)
	tt, _ := laneTaut(t, c.api.a, `start() {
    this.last = 'nothing'
    globalThis.__laneSeen = []
    globalThis.__laneSeenNow = () => globalThis.__laneSeen
    globalThis.__laneLastNow = () => this.last
    this.offs = [
      this.api.rtm.on('message', (ev) => { this.last = ev.text }),
      this.api.composer.addSendCheck(() => { globalThis.__laneSeen.push('check saw ' + this.last); return true }),
    ]
  }
  stop() { for (const off of this.offs ?? []) off() }`)
	waitFor(t, 10*time.Second, "the plugin's send check", func() bool { return tt.HasSendChecks() })
	syncHooks()
	rt, err := tt.JS()
	if err != nil || rt == nil {
		t.Fatalf("no JS runtime: %v", err)
	}
	seen := func() []string {
		t.Helper()
		raw, err := rt.Call(context.Background(), "__laneSeenNow")
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		_ = json.Unmarshal(raw, &out)
		return out
	}
	send := func(text string) {
		t.Helper()
		n := len(fs.called("chat.postMessage"))
		d := NewDoc()
		d.Insert(0, text, Attr{})
		c.ed.SetDoc(d)
		c.submit()
		waitFor(t, 15*time.Second, text+" posted", func() bool { return len(fs.called("chat.postMessage")) > n })
	}
	n := 0
	socket := func(text string) {
		n++
		raw, _ := json.Marshal(map[string]any{"type": "message", "channel": tChGen, "user": "U0FAKEOTHER", "text": text, "ts": fmt.Sprintf("1800000000.%06d", n)})
		chRaw, _ := json.Marshal(tChGen)
		c.api.a.Inject(slack.Event{Type: "message", Channel: chRaw, Raw: raw})
	}
	// the plugin hears the socket (its rtm.on reached Go)
	waitFor(t, 10*time.Second, "the plugin's rtm.on hearing a message", func() bool {
		ping := fmt.Sprintf("ping %d", n+1)
		socket(ping)
		raw, err := rt.Call(context.Background(), "__laneLastNow")
		var last string
		_ = json.Unmarshal(raw, &last)
		return err == nil && last == ping
	})
	send("warm")
	before := len(seen())
	// the JS thread busy (a render): the event is handed to the plugin
	// runtime, through Taut's raw reader, and waits behind it, then Enter
	release := make(chan struct{})
	rt.Post(func(*jsrt.Ctx) { <-release })
	jsBacklog(rt, 200*time.Millisecond)
	// core's raw tap reaches the runtime through the mirror's goroutine:
	// Enter comes once the event is queued
	queued := make(chan struct{}, 64)
	jsrt.SetQueueHook(func(q jsrt.QueuedTask) {
		if strings.HasPrefix(q.Name, "Post rtmEvent ") {
			queued <- struct{}{}
		}
	})
	t.Cleanup(func() { jsrt.SetQueueHook(nil) })
	socket("the message right before Enter")
	select {
	case <-queued:
	case <-time.After(10 * time.Second):
		t.Fatal("the socket's message never reached the runtime")
	}
	jsrt.SetQueueHook(nil)
	n0 := len(fs.called("chat.postMessage"))
	d := NewDoc()
	d.Insert(0, "ok", Attr{})
	c.ed.SetDoc(d)
	c.submit() // its checks' call is queued before submit returns
	close(release)
	waitFor(t, 15*time.Second, "ok posted", func() bool { return len(fs.called("chat.postMessage")) > n0 })
	got := strings.Join(seen()[before:], ", ")
	if want := "check saw the message right before Enter"; got != want {
		t.Errorf("the send check saw %q, want %q: the send went ahead of the event the runtime had before it", got, want)
	}
}
