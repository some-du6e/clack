package ui

import (
	"os"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// TestGiantWallOpenSpans times an open of #giant-wall, the shape of the
// channel Karim asked about (#i-love-slack-squared, 2026-11-24: Slack's
// first page there, 28 messages, was 50 MB of JSON), in three spans on the
// client: the history answer read whole to decoded (slack.Client.Call's
// json.Unmarshal), decoded to converted (core.History: the store, then the
// core messages with their Docs), and converted to the first frame that
// paints the pane's rows. It measures and bounds nothing; with
// CLACK_GIANT_OUT it writes a CPU profile of the open. Skipped unless
// CLACK_GIANT_OPEN_SPANS is set (flake-check's giant-open job).
func TestGiantWallOpenSpans(t *testing.T) {
	if os.Getenv("CLACK_GIANT_OPEN_SPANS") == "" {
		t.Skip("CLACK_GIANT_OPEN_SPANS not set (flake-check's giant-open job runs this)")
	}
	fs := giantServer(t, 100, false)
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	ch := fakeslack.GiantWallID

	var mu sync.Mutex
	var size, converted int
	var navigated, received, decoded, convertedAt, painted time.Time
	calls := func(method string, n int, r, d time.Time) {
		if method != "conversations.history" || n < 1<<20 {
			return // not #giant-wall's page (the only one past a MB)
		}
		mu.Lock()
		if received.IsZero() {
			size, received, decoded = n, r, d
		}
		mu.Unlock()
	}
	slack.CallTimes.Store(&calls)
	defer slack.CallTimes.Store(nil)
	conv := func(c string, n int, at time.Time) {
		mu.Lock()
		if c == ch && convertedAt.IsZero() && !decoded.IsZero() {
			converted, convertedAt = n, at
		}
		mu.Unlock()
	}
	core.HistoryConverted.Store(&conv)
	defer core.HistoryConverted.Store(nil)

	if out := os.Getenv("CLACK_GIANT_OUT"); out != "" {
		dir := filepath.Join(out, t.Name())
		if err := os.MkdirAll(dir, 0o755); err == nil {
			if f, err := os.Create(filepath.Join(dir, "open.cpu.pprof")); err == nil {
				if pprof.StartCPUProfile(f) == nil {
					defer func() { pprof.StopCPUProfile(); f.Close() }()
				}
			}
		}
	}
	list := func() *channelList {
		if c := e.a.convos[ch+"/"]; c != nil {
			if l, ok := c.list.(channelList); ok {
				return &l
			}
		}
		return nil
	}
	navigated = time.Now()
	e.a.Navigate("c/" + ch)
	rows := 0
	for dl := time.Now().Add(90 * time.Second); time.Now().Before(dl); {
		e.frame()
		if l := list(); l != nil {
			if msgs, loaded, _ := l.LayoutSig(); loaded && msgs > 0 && len(l.VisibleRows()) > 0 {
				painted, rows = time.Now(), len(l.VisibleRows())
				break
			}
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if painted.IsZero() || received.IsZero() || convertedAt.IsZero() {
		t.Fatalf("the open: painted %v, the answer read %v, converted %v (after %v)", !painted.IsZero(), !received.IsZero(), !convertedAt.IsZero(), time.Since(navigated).Round(time.Millisecond))
	}
	ms := func(d time.Duration) string { return d.Round(time.Millisecond).String() }
	t.Logf("#giant-wall open: a %.1f MB answer, %d messages converted, %d rows on screen", float64(size)/(1<<20), converted, rows)
	t.Logf("spans: navigate to the answer read %s (the fake's encoding and the transfer); read to decoded %s; decoded to converted %s; converted to the first painted frame %s; the client's three %s",
		ms(received.Sub(navigated)), ms(decoded.Sub(received)), ms(convertedAt.Sub(decoded)), ms(painted.Sub(convertedAt)), ms(painted.Sub(received)))
}

// TestGiantWallReopenSpans times #giant-wall's reopens: in the session,
// after another channel, and in a warm start on the same cache (quit,
// start again with the channel the route). Each is navigate (or start) to
// the first frame that paints its rows, with the history reads it made of
// #giant-wall (how many, how many bytes): whether Clack draws what it holds
// at once and what it asks Slack for again. Measures, bounds nothing.
// Skipped unless CLACK_GIANT_OPEN_SPANS is set (flake-check's giant-open).
func TestGiantWallReopenSpans(t *testing.T) {
	if os.Getenv("CLACK_GIANT_OPEN_SPANS") == "" {
		t.Skip("CLACK_GIANT_OPEN_SPANS not set (flake-check's giant-open job runs this)")
	}
	fs := giantServer(t, 100, false)
	home := t.TempDir()
	ch := fakeslack.GiantWallID

	var mu sync.Mutex
	var reads, bytes int
	calls := func(method string, n int, r, d time.Time) {
		if method == "conversations.history" && n >= 1<<20 { // #giant-wall's (the only one past a MB)
			mu.Lock()
			reads, bytes = reads+1, bytes+n
			mu.Unlock()
		}
	}
	slack.CallTimes.Store(&calls)
	defer slack.CallTimes.Store(nil)
	took := func() (int, int) {
		mu.Lock()
		defer mu.Unlock()
		r, b := reads, bytes
		reads, bytes = 0, 0
		return r, b
	}
	painted := func(e *perfEnv, what string, t0 time.Time) time.Duration {
		t.Helper()
		for dl := time.Now().Add(90 * time.Second); time.Now().Before(dl); {
			e.frame()
			if c := e.a.convos[ch+"/"]; c != nil {
				if l, ok := c.list.(channelList); ok {
					if msgs, loaded, _ := l.LayoutSig(); loaded && msgs > 0 && len(l.VisibleRows()) > 0 {
						return time.Since(t0)
					}
				}
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("%s: no rows of #giant-wall painted in 90 s", what)
		return 0
	}
	ms := func(d time.Duration) string { return d.Round(time.Millisecond).String() }

	// session 1: the first open, another channel, the reopen
	e := startEnv(t, fs, home)
	e.waitReady()
	t0 := time.Now()
	e.a.Navigate("c/" + ch)
	first := painted(e, "the first open", t0)
	e.settle(2 * time.Second) // its page read, stored, the list settled
	r1, b1 := took()
	e.a.Navigate("c/" + fs.ChannelIDs()[1])
	e.settle(time.Second)
	took()
	t0 = time.Now()
	e.a.Navigate("c/" + ch)
	again := painted(e, "the reopen", t0)
	e.settle(2 * time.Second) // what it reads after drawing
	r2, b2 := took()
	<-e.a.data.stop()
	e.shutdown()

	// session 2: a warm start at #giant-wall
	setUIRoute(t, home, "c/"+ch)
	t0 = time.Now()
	e2 := startEnv(t, fs, home)
	warm := painted(e2, "the warm start", t0)
	e2.settle(3 * time.Second)
	r3, b3 := took()

	t.Logf("#giant-wall's first open %s, %d history reads (%.1f MB)", ms(first), r1, float64(b1)/(1<<20))
	t.Logf("its reopen after another channel %s, %d history reads (%.1f MB) by 2 s after", ms(again), r2, float64(b2)/(1<<20))
	t.Logf("a warm start at it %s to its rows, %d history reads (%.1f MB) by 3 s after", ms(warm), r3, float64(b3)/(1<<20))
}
