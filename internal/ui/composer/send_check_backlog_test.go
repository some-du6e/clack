package composer

import (
	"context"
	"testing"
	"time"

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
  start() { this.off = this.api.composer.addSendCheck(() => true) }
  stop() { this.off?.() }
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
	waitFor(t, 10*time.Second, "the plugin's send check", func() bool { return tt.HasSendChecks() })
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
