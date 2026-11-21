package composer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/taut"
)

// A message's send checks (composer.addSendCheck: OwlMode, on by default
// in the Library) run ahead of the JS work queued before them, as a
// browser runs input ahead of its queued tasks (jsrt.UserBlocking): the
// pending copy waits for the checks, not for the renders, invalidations
// and events the plugin runtime still has queued. With plugins patching
// every row on a busy workspace that backlog was seconds (the perf job's
// plugin passes), and every own message waited through it before it
// showed (Karim, α.102: "my messages are being delayed").
func TestSendCheckAheadOfJSBacklog(t *testing.T) {
	app, _ := syntheticApp(t)
	tt := sendCheckTaut(t, app)
	rt, err := tt.JS()
	if err != nil || rt == nil {
		t.Fatalf("no JS runtime: %v", err)
	}
	check := func() (time.Duration, bool) {
		t0 := time.Now()
		done := make(chan bool, 1)
		tautSendCheck(SendCheck{ChannelID: tChGen, Text: "hello"}, func(ok bool) { done <- ok })
		select {
		case ok := <-done:
			return time.Since(t0), ok
		case <-time.After(20 * time.Second):
			t.Fatal("the send check never answered")
			return 0, false
		}
	}
	if _, ok := check(); !ok { // warm: the check's first run
		t.Fatal("the send check held the message back")
	}
	// 400 ms of JS work queued, in 10 ms tasks: what a channel's patched
	// rows leave the runtime when messages and profile changes arrive
	const tasks, each = 40, 10 * time.Millisecond
	for i := 0; i < tasks; i++ {
		rt.Post(func(*jsrt.Ctx) { time.Sleep(each) })
	}
	d, ok := check()
	if !ok {
		t.Fatal("the send check held the message back")
	}
	t.Logf("the send check answered %v after the send, %v of JS work queued before it", d.Round(time.Millisecond), tasks*each)
	if d > 120*time.Millisecond {
		t.Errorf("the send check answered %v after the send: it waited for the %v of JS work queued before it", d.Round(time.Millisecond), tasks*each)
	}
	_ = rt.Do(func(*jsrt.Ctx) error { return nil }) // the backlog, before the runtime closes
}

// A message without files goes out from its send checks' answer, not from
// the composer's next frame: the pending copy showed a frame later with
// any plugin's send check than without one (Slack's prepareAndSendMessage
// sends when its checks resolve). Here nothing lays the composer out after
// Enter, so a send waiting for a frame never goes.
func TestSendCheckedMessageGoesOutWithoutAFrame(t *testing.T) {
	c, fs, _ := composerEnv(t, nil)
	sendCheckTaut(t, c.api.a)
	syncHooks() // what the composer's Layout does at its start
	if SendChecks == nil {
		t.Fatal("the plugin's send check isn't installed")
	}
	d := NewDoc()
	d.Insert(0, "checked and sent", Attr{})
	c.ed.SetDoc(d)
	c.submit()
	text, _ := lastPost(t, fs)
	if !strings.Contains(text, "checked and sent") {
		t.Errorf("posted %q", text)
	}
}

// A plugin that wraps the send (OwlMode patches prepareAndSendMessage)
// and transforms its blocks: the native JS calls back from the patched
// thunk runs its own JS work (the transform) ahead of the queued JS too,
// as the thunk's dispatch does. JS calls a native back with a context of
// its own, and the send waited for the whole backlog there.
func TestSendThunkWorkAheadOfJSBacklog(t *testing.T) {
	c, fs, _ := composerEnv(t, nil)
	tt := pluginTaut(t, c.api.a, `start() {
    this.offs = [this.api.redux.patchThunk('prepareAndSendMessage', (orig) => (...a) => orig(...a)), this.api.onMessageSendBlocks((b) => b)]
  }
  stop() { for (const off of this.offs ?? []) off() }`)
	waitFor(t, 10*time.Second, "the patched send thunk and the transform", func() bool {
		return tt.ThunkPatched("prepareAndSendMessage") && tt.HasSendTransforms()
	})
	rt, err := tt.JS()
	if err != nil || rt == nil {
		t.Fatalf("no JS runtime: %v", err)
	}
	send := func(text string) time.Duration {
		n := len(fs.called("chat.postMessage"))
		d := NewDoc()
		d.Insert(0, text, Attr{})
		c.ed.SetDoc(d)
		t0 := time.Now()
		c.submit()
		for dl := time.Now().Add(10 * time.Second); len(fs.called("chat.postMessage")) == n; time.Sleep(2 * time.Millisecond) {
			if time.Now().After(dl) {
				t.Fatalf("%q never went out", text)
			}
		}
		return time.Since(t0)
	}
	send("warm") // the thunk's and the transform's first runs
	const tasks, each = 40, 10 * time.Millisecond
	for i := 0; i < tasks; i++ {
		rt.Post(func(*jsrt.Ctx) { time.Sleep(each) })
	}
	d := send("through the wrapped send")
	t.Logf("the wrapped send went out %v after Enter, %v of JS work queued before it", d.Round(time.Millisecond), tasks*each)
	if d > 200*time.Millisecond {
		t.Errorf("the wrapped send went out %v after Enter: its native's JS work waited for the %v queued before it", d.Round(time.Millisecond), tasks*each)
	}
	_ = rt.Do(func(*jsrt.Ctx) error { return nil })
}

// sendCheckTaut is a plugin system over app with one plugin installed and
// running whose send check lets every message through, made the
// composer's (withTaut).
func sendCheckTaut(t *testing.T, app *core.App) *taut.Taut {
	t.Helper()
	tt := pluginTaut(t, app, `start() { this.off = this.api.composer.addSendCheck(() => true) }
  stop() { this.off?.() }`)
	waitFor(t, 10*time.Second, "the plugin's send check", func() bool { return tt.HasSendChecks() })
	return tt
}

// pluginTaut is a plugin system over app, made the composer's (withTaut),
// with one synthetic plugin installed and running: body is its class
// body's methods (start, stop).
func pluginTaut(t *testing.T, app *core.App, body string) *taut.Taut {
	t.Helper()
	dir := t.TempDir()
	tt, err := taut.New(taut.Options{App: app, Dir: dir, CredsPath: dir + "/creds.json", CacheDir: t.TempDir(), NoAutoStart: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tt.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	id, err := tt.Import(ctx, []byte(`(() => {
const TautPlugin = globalThis.TautPlugin
class LatencySendCheck extends TautPlugin {
  static id = 'LatencySendCheck'
  static pluginName = 'Latency send check'
  static description = 'A send check that lets every message through'
  static category = 'messageBox'
  static authors = ['test']
  static defaultConfig = { enabled: false }
  `+body+`
}
return LatencySendCheck
})()`), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	withTaut(t, tt)
	t.Cleanup(func() {
		SetTaut(nil)
		syncHooks()
	})
	return tt
}
