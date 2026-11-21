package ui

// Message latency under the plugin set TestPerfPluginScroll measures
// (perf workflow, jobs: plugins): TestMessageLatency's passes (others'
// messages and own sends, quiet and under a busy workspace's socket, over
// the fake's realtime socket with frames drawn as the window draws them)
// with the Library's plugins installed the way its Install button does,
// every Taut plugin it turns on by default and Slick's Message Logger
// (PERF_PLUGINS), plus a pass with 400 ms of work queued on the plugin
// runtime before each own send. Reports the distributions; budgets are
// TestMessageLatency's and TestTautMessageLatency's to enforce. The
// workflow runs this file on the base tree too (with msg_latency_test.go),
// so the job's report has the before and the after side by side.
//
//	PLUGIN_BENCH_LIBRARY  the `plugins` release's files (index.json + bundles)
//	PERF_PLUGINS          as TestPerfPluginScroll's
//	MSG_LATENCY_OUT       where the passes' JSON lines go
//
// Synthetic data only; nothing reaches Slack.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

func TestPerfMessageLatency(t *testing.T) {
	dir := os.Getenv("PLUGIN_BENCH_LIBRARY")
	if dir == "" {
		t.Skip("PLUGIN_BENCH_LIBRARY not set: the perf workflow (jobs: plugins) downloads the Library and runs this")
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
	want := os.Getenv("PERF_PLUGINS")
	if want == "" {
		want = "taut:@default,slick:MessageLogger"
	}
	var set []pluginlib.Entry
	for _, w := range strings.Split(want, ",") {
		sys, id, _ := strings.Cut(strings.TrimSpace(w), ":")
		for _, en := range ix.For(sys) {
			if id == "@default" && en.DefaultEnabled || en.ID == id {
				set = append(set, en)
			}
		}
	}
	e := newLatEnv(t, latEnvOpts{hackClub: true, rtt: latRTT})
	e.until("Taut and Slick", 60*time.Second, func() bool { return e.a.Taut() != nil && e.a.Slick() != nil })
	be := &benchEnv{perfEnv: e, t: t, fs: e.fs, tt: e.a.Taut(), trees: map[uint64][]byte{}, styles: map[string]string{}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	lib := e.a.pluginLib()
	if lib == nil {
		t.Fatal("no plugin library")
	}
	if _, err := lib.Index(ctx, true); err != nil {
		t.Fatal(err)
	}
	page := newLibraryPage(e.a, nil)
	var running, failed []string
	for _, en := range set {
		name := en.System + ":" + en.ID
		if err := perfInstall(ctx, be, page, en); err != nil {
			t.Logf("%s: %v", name, err)
			failed = append(failed, name)
			continue
		}
		running = append(running, name)
	}
	e.settle(1500 * time.Millisecond)
	t.Logf("running %d plugins (%s), failed %v; patched: %s", len(running), strings.Join(running, " "), failed, strings.Join(slackc.PatchedNames(), " "))
	rt, err := be.tt.JS()
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
	for _, ps := range passes {
		ps.pluginSend = true
		res[ps.name] = p.measure(t, ps, false)
	}
	latWrite(t, "TestPerfMessageLatency", res)
}
