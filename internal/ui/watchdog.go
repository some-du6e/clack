package ui

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/openurl"
)

// The frame watchdog: field diagnosis for freezes.
//
// Run marks when it starts handling a window event (a frame, the posted
// work drained before it, the GPU submit) and when it's done. If one of
// those takes longer than freezeAfter, a goroutine writes every
// goroutine's stack to <config dir>/freeze-reports/freeze-<time>.txt and
// the app keeps running; when the stuck work finishes, the report gets
// how long it took. The stacks hold function names, file lines and
// argument words only (no strings, so no message text); the whole report
// goes through slack.Redact anyway, so no token or cookie is written.
// README "Freezes" says where to find the file.

// freezeAfter is how long one frame or UI-goroutine task may run before a
// report is written.
const freezeAfter = 2 * time.Second

// freezeKeep is how many reports stay on disk (oldest removed first).
const freezeKeep = 10

type watchdog struct {
	dir     string // freeze-reports directory ("" = off)
	ui      string // ui.json, copied into the report
	started time.Time
	after   time.Duration // freezeAfter (shorter in tests)

	busy  atomic.Int64 // unix ns the current task began; 0 = idle
	epoch atomic.Uint64
	stage atomic.Pointer[string]
	page  atomic.Pointer[string] // the route's page at the last frame

	// written is called with each report's path (tests).
	written func(path string)
	quit    chan struct{}
}

func newWatchdog(configDir string, started time.Time) *watchdog {
	w := &watchdog{started: started, after: freezeAfter, quit: make(chan struct{})}
	if configDir != "" {
		w.dir = filepath.Join(configDir, "freeze-reports")
		w.ui = filepath.Join(configDir, "ui.json")
	}
	return w
}

var (
	stageEvent  = "window event"
	stagePosted = "posted UI work (App.Post)"
	stageLayout = "frame layout"
	stageSubmit = "frame submit (GPU, main thread)"
	stageShaper = "first frame"
)

// begin starts timing a task on the UI goroutine. Cheap: atomics only.
func (w *watchdog) begin(stage *string) {
	if w == nil {
		return
	}
	w.stage.Store(stage)
	w.epoch.Add(1)
	w.busy.Store(time.Now().UnixNano())
}

// next moves the running task to another stage (same task, same clock).
func (w *watchdog) next(stage *string) {
	if w != nil {
		w.stage.Store(stage)
	}
}

func (w *watchdog) end() {
	if w != nil {
		w.busy.Store(0)
	}
}

// notePage records the page shown (UI goroutine; stores only on change).
func (w *watchdog) notePage(p string) {
	if w == nil {
		return
	}
	if cur := w.page.Load(); cur == nil || *cur != p {
		w.page.Store(&p)
	}
}

// showFreezeReports is Help > Troubleshooting > Show Freeze Reports: the folder in Finder (or
// a toast when there are none). Any goroutine.
func (a *App) showFreezeReports() {
	if a.opts.ConfigDir == "" {
		return
	}
	dir := filepath.Join(a.opts.ConfigDir, "freeze-reports")
	if ents, err := os.ReadDir(dir); err != nil || len(ents) == 0 {
		a.Post(func() { a.Toast("No freeze reports: Clack hasn't frozen on this computer") })
		return
	}
	_ = openurl.OpenFolder(dir)
}

// run is the watchdog goroutine.
func (w *watchdog) run() {
	if w == nil || w.dir == "" {
		return
	}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	var (
		open      string // report of the task still running
		openEpoch uint64
		openSince time.Time
	)
	for {
		select {
		case <-tick.C:
		case <-w.quit:
			return
		}
		since := w.busy.Load()
		ep := w.epoch.Load()
		if open != "" && (since == 0 || ep != openEpoch) {
			d := time.Since(openSince).Round(100 * time.Millisecond)
			appendFile(open, fmt.Sprintf("\nThe window was responsive again after about %v.\n", d))
			log.Printf("ui: the stuck UI work finished after about %v", d)
			open = ""
		}
		if since == 0 || open != "" || ep == openEpoch {
			continue
		}
		start := time.Unix(0, since)
		if time.Since(start) < w.after {
			continue
		}
		path, err := w.report(start)
		openEpoch = ep // one report per stuck task
		if err != nil {
			log.Printf("ui: the window is blocked (%s) but the freeze report couldn't be written: %v", w.stageName(), err)
			continue
		}
		open, openSince = path, start
		log.Printf("ui: the window has been blocked for %v (%s); freeze report: %s", time.Since(start).Round(100*time.Millisecond), w.stageName(), path)
		if w.written != nil {
			w.written(path)
		}
	}
}

// stop ends run.
func (w *watchdog) stop() {
	if w != nil {
		close(w.quit)
	}
}

func (w *watchdog) stageName() string {
	if s := w.stage.Load(); s != nil {
		return *s
	}
	return "?"
}

// report writes one freeze report and returns its path.
func (w *watchdog) report(start time.Time) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Clack freeze report\n\n")
	fmt.Fprintf(&b, "version: %s\n", buildVersion)
	fmt.Fprintf(&b, "os: %s/%s, %s, %d CPUs\n", runtime.GOOS, runtime.GOARCH, runtime.Version(), runtime.NumCPU())
	fmt.Fprintf(&b, "written: %s (%v after start)\n", time.Now().Format(time.RFC3339), time.Since(w.started).Round(time.Second))
	fmt.Fprintf(&b, "blocked: %v so far, in %s\n", time.Since(start).Round(10*time.Millisecond), w.stageName())
	if p := w.page.Load(); p != nil {
		fmt.Fprintf(&b, "page: %s\n", *p)
	}
	if ui, err := os.ReadFile(w.ui); err == nil && len(ui) < 64<<10 {
		fmt.Fprintf(&b, "\nui.json:\n%s\n", strings.TrimSpace(string(ui)))
	}
	stacks := allStacks()
	fmt.Fprintf(&b, "\ngoroutines (%d):\n\n%s\n", runtime.NumGoroutine(), stacks)
	if err := os.MkdirAll(w.dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(w.dir, "freeze-"+time.Now().Format("20060102-150405.000")+".txt")
	if err := os.WriteFile(path, []byte(slack.Redact(b.String())), 0o600); err != nil {
		return "", err
	}
	w.prune()
	return path, nil
}

// allStacks is runtime.Stack for every goroutine (grows the buffer up to
// 16 MiB).
func allStacks() []byte {
	buf := make([]byte, 256<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) || len(buf) >= 16<<20 {
			return buf[:n]
		}
		buf = make([]byte, 2*len(buf))
	}
}

// prune keeps the newest freezeKeep reports.
func (w *watchdog) prune() {
	ents, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range ents {
		if n := e.Name(); !e.IsDir() && strings.HasPrefix(n, "freeze-") && strings.HasSuffix(n, ".txt") {
			names = append(names, n)
		}
	}
	sort.Strings(names) // timestamps sort by time
	for len(names) > freezeKeep {
		_ = os.Remove(filepath.Join(w.dir, names[0]))
		names = names[1:]
	}
}

func appendFile(path, s string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.WriteString(s)
	_ = f.Close()
}
