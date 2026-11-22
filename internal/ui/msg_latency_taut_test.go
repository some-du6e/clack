package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// TestTautMessageLatency is TestMessageLatency with plugins like Karim's
// (ui-taut builds them from Taut's and Slick's sources): Taut's Show Real
// User and OwlMode and Slick's Message Logger, so MessageWrapper,
// MessageText, Blocks and the sender are patched on every row and
// OwlMode's send check and send patch gate every send, plus a send check
// of the test's own (the path every send takes with any plugin's
// addSendCheck). The quiet own pass lets the plugin runtime finish its
// work before each Enter (the patched composer's re-render after the
// typing), and its sends are held to Slack's next frame like ones without
// plugins: the checks and the send's patches run in one JS call queued at
// Enter ahead of the composer's re-render (taut's CheckThenDispatch), as
// Slack runs them in one task. The "JS busy"
// pass queues 400 ms of work on the plugin runtime, in 10 ms tasks, before
// each own send instead: what a channel's patched rows leave it when
// messages and profile changes come in (the perf job's plugin passes
// measured seconds of it); its sends wait for the task running when they
// come, one frame more, never the backlog. A patched row draws its native
// at once and the plugin's tree when it lands, so others' rows are timed
// as without plugins.
func TestTautMessageLatency(t *testing.T) {
	tdir, sdir := os.Getenv("TAUT_PLUGINS_DIR"), os.Getenv("SLICK_PLUGINS_DIR")
	if tdir == "" || sdir == "" {
		t.Skip("TAUT_PLUGINS_DIR and SLICK_PLUGINS_DIR not set: the ui-taut workflow builds both and runs this")
	}
	e := newLatEnv(t, latEnvOpts{hackClub: true, rtt: latRTT})
	e.until("Taut and Slick", 60*time.Second, func() bool { return e.a.Taut() != nil && e.a.Slick() != nil })
	tt, sl := e.a.Taut(), e.a.Slick()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, name := range []string{"ShowRealUser", "OwlMode"} {
		code, err := os.ReadFile(filepath.Join(tdir, name+".js"))
		if err != nil {
			t.Fatal(err)
		}
		id, err := tt.Import(ctx, code, "")
		if err == nil {
			err = tt.SetEnabled(ctx, id, true)
		}
		if err != nil { // OwlMode is Hack Club's: the env has Hack Club's enterprise id
			t.Fatalf("%s didn't start: %v", name, err)
		}
	}
	check, err := tt.Import(ctx, []byte(`(() => {
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
	if err := tt.SetEnabled(ctx, check, true); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"MessageLogger.js", "MessageLogger.main.js"} {
		code, err := os.ReadFile(filepath.Join(sdir, f))
		if err != nil {
			if f == "MessageLogger.js" {
				t.Fatal(err)
			}
			continue
		}
		if _, err := sl.Import(ctx, code); err != nil {
			t.Fatalf("import %s: %v", f, err)
		}
	}
	if err := sl.SetEnabled(ctx, "MessageLogger", true); err != nil {
		t.Fatal(err)
	}
	e.until("the plugins' patches", 30*time.Second, func() bool {
		return slackc.Patched("MessageWrapper") && slackc.Patched("MessageText") && tt.HasSendChecks()
	})
	t.Logf("patched: %s", strings.Join(slackc.PatchedNames(), " "))
	rt, err := tt.JS()
	if err != nil || rt == nil {
		t.Fatalf("no JS runtime: %v", err)
	}
	p := newLatProbe(t, e, e.fs.ChannelIDs()[0])
	res := map[string]map[string]latDist{}
	passes := append(latPasses(), latPass{name: "quiet, own sends, JS busy", own: 6, busyJS: func(d time.Duration) {
		const each = 10 * time.Millisecond
		for i := 0; i < int(d/each); i++ {
			rt.Post(func(*jsrt.Ctx) { time.Sleep(each) })
		}
	}})
	idle := func() { _ = rt.Do(func(*jsrt.Ctx) error { return nil }) }
	// what the JS thread ran after each own send's Enter (the quiet
	// passes): which task held a pending copy past its frame
	var tasksMu sync.Mutex
	var tasks []jsrt.TaskRun
	jsrt.SetTaskHook(func(r jsrt.TaskRun) {
		tasksMu.Lock()
		tasks = append(tasks, r)
		tasksMu.Unlock()
	})
	defer jsrt.SetTaskHook(nil)
	for _, ps := range passes {
		ps.pluginSend = true
		if ps.busyJS == nil && ps.own > 0 && !ps.busy {
			ps.drainJS = idle
		}
		res[ps.name] = p.measure(t, ps, true)
		if ps.name != "quiet, own sends" {
			continue
		}
		p.mu.Lock()
		tasksMu.Lock()
		for _, w := range p.waits {
			if w.kind != "own" || !strings.HasPrefix(w.text, ps.name+" own ") {
				continue
			}
			var line []string
			for _, r := range tasks {
				if at := r.Start.Sub(w.t0); at >= -5*time.Millisecond && at < 60*time.Millisecond {
					lane := ""
					if r.Hold {
						lane = " hold"
					} else if r.Urgent {
						lane = " user"
					}
					line = append(line, fmt.Sprintf("+%.1f %s%s %.1fms", float64(at)/1e6, r.Name, lane, float64(r.Took)/1e6))
				}
			}
			t.Logf("JS after own send (pending copy in %v, %d frames): %s", w.lat.Round(100*time.Microsecond), w.frames, strings.Join(line, "; "))
		}
		tasksMu.Unlock()
		p.mu.Unlock()
	}
	latWrite(t, "TestTautMessageLatency", res)
}
