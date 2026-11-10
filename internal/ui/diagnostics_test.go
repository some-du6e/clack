package ui

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/composer"
)

func TestDiagnosticsText(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	in := diagInfo{
		version:  "v0.9.0-alpha.19",
		started:  now.Add(-90 * time.Minute),
		logDir:   filepath.Join(home, "Library", "Logs", "Clack"),
		realtime: true,
		patched:  []string{"ClientPrimaryView", "RenderedViewContents"},
		route:    Route("search?q=quarterly numbers").logString(),
		plugins:  "slick: chat-tabs (running); taut: none",
		log:      []string{"2026/11/08 11:59:00 ui: first line", "2026/11/08 11:59:59 rtm: dial ?token=xoxc-LEAKED: refused"},
	}
	got := in.text(now)
	for _, want := range []string{
		"Clack diagnostics\n", "version: v0.9.0-alpha.19\n", "os: ", "running for: 1h30m0s\n",
		"log file: ~" + string(filepath.Separator) + filepath.Join("Library", "Logs", "Clack", "clack.log") + "\n",
		"live updates: on\n", "page: search?…\n",
		"plugins: slick: chat-tabs (running); taut: none\n",
		"patched components: ClientPrimaryView, RenderedViewContents\n",
		"last 2 log lines:\n2026/11/08 11:59:00 ui: first line\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	for _, leak := range []string{"LEAKED", "quarterly", home + string(filepath.Separator)} {
		if strings.Contains(got, leak) {
			t.Errorf("%q in the diagnostics:\n%s", leak, got)
		}
	}
	empty := diagInfo{}.text(now)
	if !strings.Contains(empty, "version: dev\n") || !strings.Contains(empty, "plugins: none\n") || !strings.Contains(empty, "patched components: none\n") {
		t.Errorf("no plugins, no log:\n%s", empty)
	}
	if pluginStates(nil, nil) != "slick: none; taut: none" || runningPlugins(nil, nil) != "slick: none; taut: none" {
		t.Errorf("no plugin systems: %q / %q", pluginStates(nil, nil), runningPlugins(nil, nil))
	}
}

// lockedBuf is a log output read while the log goroutine writes.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// The missed-frame line: written at the first stand-in, then at most once
// per focusLogEvery, with the counts, the route (no search query) and the
// patch state.
func TestMissedFrameLogLine(t *testing.T) {
	var out lockedBuf
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&out)
	log.SetFlags(0)
	defer func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) }()

	lines := func(want int) []string {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			var ls []string
			for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
				if strings.HasPrefix(l, "ui: missed frames") {
					ls = append(ls, l)
				}
			}
			if len(ls) >= want || time.Now().After(deadline) {
				return ls
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	composer.TakeFocusStats() // what earlier tests' frames counted
	a := &App{}
	t0 := time.Unix(1_800_000_000, 0)
	frame := func(at time.Time) { a.logMissedFrames(layout.Context{Now: at}) }

	frame(t0) // nothing missed: no line
	a.missedFrameLog.standIn("c/C1/", true)
	a.missedFrameLog.standIn("c/C1/", false)
	frame(t0.Add(time.Millisecond))
	ls := lines(1)
	if len(ls) != 1 {
		t.Fatalf("first stand-ins: %d lines\n%s", len(ls), out.String())
	}
	for _, want := range []string{"body missing from 2 frame(s)", "kept its focus in 1)", "route c/C1/", "patched ClientPrimaryView=false RenderedViewContents=false", "plugins running: slick: none; taut: none"} {
		if !strings.Contains(ls[0], want) {
			t.Errorf("no %q in %q", want, ls[0])
		}
	}
	a.missedFrameLog.standIn("search?q=private words", false)
	frame(t0.Add(2 * time.Second)) // rate-limited
	a.missedFrameLog.standIn("search?q=private words", false)
	frame(t0.Add(5 * time.Second))
	time.Sleep(50 * time.Millisecond)
	if ls := lines(1); len(ls) != 1 {
		t.Fatalf("a second line within %v: %q", focusLogEvery, ls)
	}
	frame(t0.Add(11 * time.Second))
	ls = lines(2)
	if len(ls) != 2 || !strings.Contains(ls[1], "body missing from 2 frame(s)") || !strings.Contains(ls[1], "route search?…") || strings.Contains(out.String(), "private") {
		t.Fatalf("after %v: %q", focusLogEvery, ls)
	}
	frame(t0.Add(30 * time.Second)) // nothing new: no line
	time.Sleep(50 * time.Millisecond)
	if ls := lines(2); len(ls) != 2 {
		t.Errorf("a line with nothing missed: %q", ls)
	}
}
