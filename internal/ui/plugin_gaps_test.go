package ui

// Host gaps the plugin bench found (docs/PLUGIN-BENCH.md), each checked
// with the Library's published bundle, unmodified, in the whole client
// over the bench workspace: TestPluginGap*. The plugin-bench workflow's
// gaps job runs them (PLUGIN_BENCH_LIBRARY set); synthetic data only.

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// gapEnv is the client with one published plugin installed and enabled.
type gapEnv struct {
	*benchEnv
	logs  *benchLog
	entry pluginlib.Entry
}

// gapStart starts the bench client and installs the Library's sys
// variant of the plugin listed as name, with opts set before enabling.
func gapStart(t *testing.T, sys, name string, opts map[string]any) *gapEnv {
	dir := os.Getenv("PLUGIN_BENCH_LIBRARY")
	if dir == "" {
		t.Skip("PLUGIN_BENCH_LIBRARY not set: the plugin-bench workflow downloads the Library and runs this")
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
	var entry pluginlib.Entry
	for _, e := range libraryEntries(&ix) {
		if e.System == sys && libNameKey(e.Name) == libNameKey(name) {
			entry = e
		}
	}
	if entry.ID == "" {
		t.Fatalf("no %s %s in the Library", sys, name)
	}
	logs := &benchLog{}
	prev := log.Writer()
	log.SetOutput(io.MultiWriter(prev, logs))
	t.Cleanup(func() { log.SetOutput(prev) })

	fs, err := fakeslack.Start(benchSize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	e := benchStart(t, fs)
	e.waitReady()
	e.until("Taut and Slick", 60*time.Second, func() bool { return e.a.Taut() != nil && e.a.Slick() != nil })
	e.tt = e.a.Taut()
	sl := e.a.Slick()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
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
	h, ok := newLibraryPage(e.a, nil).host(sys)
	if !ok {
		t.Fatalf("no %s host", sys)
	}
	if err := h.install(ctx, entry, code, main, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	for k, v := range opts {
		if sys == pluginlib.Taut {
			err = e.tt.SetOption(ctx, entry.ID, k, v)
		} else {
			err = sl.SetOption(ctx, entry.ID, k, v)
		}
		if err != nil {
			t.Fatalf("option %s: %v", k, err)
		}
	}
	if sys == pluginlib.Taut {
		err = e.tt.SetEnabled(ctx, entry.ID, true)
	} else {
		err = sl.SetEnabled(ctx, entry.ID, true)
	}
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	e.settle(800 * time.Millisecond)
	slackc.SetBridge(countingBridge{e.tt.Bridge(), &e.calls})
	e.openBench()
	return &gapEnv{benchEnv: e, logs: logs, entry: entry}
}

// TestPluginGapPronounsThread: both User Pronouns variants put the root
// sender's pronouns in the thread pane's header, as in channel rows. The
// pane passes ThreadSenderAndTimestampGeneric what Slack's does (msg,
// adjacent, visible, omitTimestamp, omitLinebreak, keepSenderAccessible,
// timestampLabel); the bench saw nothing because its fixture never served
// the thread (conversations.replies answered with no messages), so the
// pane drew no root.
func TestPluginGapPronounsThread(t *testing.T) {
	for _, sys := range []string{pluginlib.Taut, pluginlib.Slick} {
		t.Run(sys, func(t *testing.T) {
			e := gapStart(t, sys, "User Pronouns", nil)
			ts := e.threadWithPronouns()
			want := e.c.Pronouns[e.c.ThreadBy[ts]]
			before := e.rootsWith(`"v":"` + want + `"`)
			e.a.OpenThread(fakeslack.BenchChannelID, ts)
			ok := e.wait(10*time.Second, func() bool {
				for r := range e.rootsWith(`"v":"` + want + `"`) {
					if !before[r] {
						return true
					}
				}
				return false
			})
			if !ok {
				// the pane's tree needs the thread: conversations.replies
				// answered with the root (the bench fixture served none)
				t.Errorf("thread pane: %d trees, loaded %v", e.delivered(), messages.ThreadPane("loaded", fakeslack.BenchChannelID, ts, ""))
				t.Fatalf("no new tree with %s after opening the thread", want)
			}
		})
	}
}

// TestPluginGapStreamerModeRows: Taut's Streamer Mode patches
// MessageBackground on every row; scrolling the channel while promises
// settle from Go and Go collects garbage logs no errors. jsrt released
// the handles of a promise's settle functions twice (Free, then the GC
// cleanup), by then another value's, so componentElement's element or
// props turned into __clack (React #31) and its function into a
// non-function.
func TestPluginGapStreamerModeRows(t *testing.T) {
	e := gapStart(t, pluginlib.Taut, "Streamer Mode", map[string]any{"alwaysOn": true})
	if !slackc.Patched("MessageBackground") {
		t.Fatal("MessageBackground isn't patched")
	}
	_ = e.logs.take()
	p := e.listPoint()
	for i := 0; i < 120; i++ {
		e.scroll(p, -900)
		e.frame()
		if i%4 == 0 {
			// Go-settled promises (a plugin's API calls, thunks) free
			// their settle functions' handles, and the collector runs
			e.js(`for (let i = 0; i < 16; i++) __clack.taut_api('auth.test', {}).catch(() => {}); 0`)
			e.settle(20 * time.Millisecond)
			runtime.GC()
		}
		e.settle(15 * time.Millisecond)
	}
	e.settle(500 * time.Millisecond)
	if errs := e.logs.take(); len(errs) > 0 {
		for _, l := range errs[:min(len(errs), 5)] {
			t.Logf("console: %.400s", l)
		}
		t.Fatalf("%d console errors while scrolling the patched rows", len(errs))
	}
}

// TestPluginGapQuickJoinDoubleClick: Slick's Quick Join listens for clicks
// on the document and looks for a[data-channel-id]; Clack draws message
// links natively. A double click on a link to a channel you're not in
// joins it, and a single click still opens it once the plugin replays it.
func TestPluginGapQuickJoinDoubleClick(t *testing.T) {
	e := gapStart(t, pluginlib.Slick, "Quick Join", nil)
	ch := fakeslack.PreviewChannelID
	ts := e.postChannelLink(ch)
	if !slackc.WantsGlobal("click") {
		t.Fatal("Quick Join isn't listening for clicks on the document")
	}
	link := e.channelLink(ts, ch)
	if link.Empty() {
		t.Fatal("no channel link in the posted message")
	}
	// a single click is held, then replayed: the link opens the channel
	e.clickAt(link.Min.Add(link.Max).Div(2), time.Duration(time.Now().UnixNano()))
	if !e.wait(5*time.Second, func() bool { return e.a.router.Current().Channel() == ch }) {
		t.Fatalf("a single click on the link didn't open %s (route %q)", ch, e.a.router.Current())
	}
	if n := len(e.fs.JoinCalls()); n != 0 {
		t.Fatalf("a single click joined: %v", e.fs.JoinCalls())
	}
	// a double click joins and opens nothing
	if ok, why := e.doubleClickJoins(ts, ch); !ok {
		t.Fatal(why)
	}
	e.settle(600 * time.Millisecond)
	if got := e.a.router.Current().Channel(); got != fakeslack.BenchChannelID {
		t.Errorf("the double click also navigated: route %q", e.a.router.Current())
	}
}

// postChannelLink makes ch known to the client (public, not a member),
// posts a message linking to it at the bottom of #plugin-bench and
// returns its ts once the link is drawn.
func (e *benchEnv) postChannelLink(ch string) string {
	e.openChannel(ch)
	e.wait(10*time.Second, func() bool { c, ok := e.a.data.core.Conv(ch); return ok && c.Name != "" })
	e.openBench()
	ts := fmt.Sprintf("%d.000200", time.Now().Unix())
	raw, _ := json.Marshal(map[string]any{"type": "message", "channel": fakeslack.BenchChannelID, "user": e.c.Authors[0], "ts": ts,
		"text": "come to <#" + ch + ">", "blocks": []any{map[string]any{"type": "rich_text", "block_id": "qj", "elements": []any{
			map[string]any{"type": "rich_text_section", "elements": []any{
				map[string]any{"type": "text", "text": "come to "}, map[string]any{"type": "channel", "channel_id": ch}}}}}}})
	e.a.data.core.Inject(slack.Event{Type: "message", Channel: json.RawMessage(`"` + fakeslack.BenchChannelID + `"`), Raw: raw})
	e.toBottom()
	e.wait(10*time.Second, func() bool { return !e.channelLink(ts, ch).Empty() })
	return ts
}

// doubleClickJoins double-clicks message ts's link to ch in #plugin-bench
// and reports whether that joined ch.
func (e *benchEnv) doubleClickJoins(ts, ch string) (bool, string) {
	e.openBench()
	e.toBottom()
	var link image.Rectangle
	if !e.wait(5*time.Second, func() bool { link = e.channelLink(ts, ch); return !link.Empty() }) {
		return false, "no channel link to double-click"
	}
	p := link.Min.Add(link.Max).Div(2)
	t0 := time.Duration(time.Now().UnixNano())
	e.clickAt(p, t0)
	e.frame()
	e.clickAt(p, t0+80*time.Millisecond)
	joined := func() bool {
		return slices.ContainsFunc(e.fs.JoinCalls(), func(j fakeslack.JoinCall) bool { return j.Channel == ch })
	}
	if !e.wait(8*time.Second, joined) {
		return false, fmt.Sprintf("a double click on the link didn't join %s (joins %v)", ch, e.fs.JoinCalls())
	}
	return true, ""
}

// channelLink is the window box of message ts's link to channel ch.
func (e *benchEnv) channelLink(ts, ch string) image.Rectangle {
	c := e.a.convos[fakeslack.BenchChannelID+"/"]
	if c == nil {
		return image.Rectangle{}
	}
	l, ok := c.list.(channelList)
	if !ok {
		return image.Rectangle{}
	}
	for _, r := range l.LinkRects() {
		if r.TS == ts && r.Kind == richview.HitChannel && r.ID == ch {
			return r.R
		}
	}
	return image.Rectangle{}
}
