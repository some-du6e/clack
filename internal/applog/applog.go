// Package applog keeps Clack's log on disk, like Slack desktop's logs
// folder: everything written with the standard log package also goes,
// redacted, to a rotating file in the OS's log directory, so a bug seen
// in a Clack started from the Dock or the Start menu (no terminal, so no
// stderr) still leaves its evidence.
//
//	macOS    ~/Library/Logs/Clack/clack.log
//	Linux    $XDG_STATE_HOME/clack/clack.log (~/.local/state/clack/clack.log)
//	Windows  %LOCALAPPDATA%\Clack\logs\clack.log
//
// CLACK_LOG_DIR overrides the directory. The file rotates at maxSize into
// clack.log.1 and clack.log.2 (three files in all).
//
// The UI never waits for the disk: Write copies the line into a buffered
// channel and returns; one goroutine opens the file (off the UI goroutine
// and the macOS main thread) and writes. When the buffer is full the line
// is dropped and counted ("N lines dropped" is written when there's room
// again). Every line goes through Redact on its way to the file.
package applog

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

const (
	// FileName is the live log file in Dir.
	FileName = "clack.log"
	// maxSize is when the live file is rotated.
	maxSize = 5 << 20
	// keepFiles is how many files stay: clack.log, .1, .2.
	keepFiles = 3
	// queueLen is how many lines wait for the disk before new ones drop.
	queueLen = 2048
	// ringLen is how many lines stay in memory (Tail without a file).
	ringLen = 256
)

// Dir is the directory Clack's log file is kept in.
func Dir() (string, error) {
	if d := os.Getenv("CLACK_LOG_DIR"); d != "" {
		return d, nil
	}
	home, _ := os.UserHomeDir()
	return dirFor(runtime.GOOS, os.Getenv, home)
}

var errNoDir = errors.New("no home directory to keep logs in")

// dirFor is Dir for goos, with the environment and home directory given
// (tests check every OS's path on any OS).
func dirFor(goos string, getenv func(string) string, home string) (string, error) {
	switch goos {
	case "darwin", "ios":
		if home == "" {
			return "", errNoDir
		}
		return filepath.Join(home, "Library", "Logs", "Clack"), nil
	case "windows":
		if d := getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "Clack", "logs"), nil
		}
		if home == "" {
			return "", errNoDir
		}
		return filepath.Join(home, "AppData", "Local", "Clack", "logs"), nil
	default:
		// XDG: a relative $XDG_STATE_HOME is invalid and is ignored
		if d := getenv("XDG_STATE_HOME"); d != "" && (strings.HasPrefix(d, "/") || filepath.IsAbs(d)) {
			return filepath.Join(d, "clack"), nil
		}
		if home == "" {
			return "", errNoDir
		}
		return filepath.Join(home, ".local", "state", "clack"), nil
	}
}

// Logger is the on-disk log. Its methods are safe from any goroutine; a
// nil Logger does nothing.
type Logger struct {
	dir string // "" when there is no log directory
	w   *Async
}

// Start sends the standard logger's output to stderr (through
// slack.RedactWriter, as before) and to the log file. The file is opened
// on the writer's goroutine; if it can't be, one warning goes to stderr
// and Clack logs to stderr only.
func Start(stderr io.Writer) *Logger {
	dir, err := Dir()
	l := start(dir, err, stderr)
	log.SetOutput(l.Output(stderr))
	return l
}

func start(dir string, dirErr error, stderr io.Writer) *Logger {
	l := &Logger{dir: dir}
	warn := func(err error) {
		where := dir
		if where == "" {
			where = "(no log directory)"
		}
		fmt.Fprintf(stderr, "clack: can't write the log file in %s (%v); logging to stderr only\n", where, err)
	}
	open := func() (io.WriteCloser, error) {
		if dirErr != nil {
			return nil, dirErr
		}
		return openRotating(filepath.Join(dir, FileName), maxSize, keepFiles)
	}
	l.w = NewAsync(open, warn, queueLen)
	return l
}

// Output is the writer for log.SetOutput: the file first (never blocks),
// then stderr through slack.RedactWriter. A failing stderr (none when
// started from the Dock) doesn't stop the file.
func (l *Logger) Output(stderr io.Writer) io.Writer {
	return tee{file: l.w, stderr: slack.RedactWriter(stderr)}
}

type tee struct {
	file   io.Writer
	stderr io.Writer
}

func (t tee) Write(p []byte) (int, error) {
	if t.file != nil {
		_, _ = t.file.Write(p)
	}
	_, _ = t.stderr.Write(p)
	return len(p), nil
}

// Dir is the log directory ("" when there is none).
func (l *Logger) Dir() string {
	if l == nil {
		return ""
	}
	return l.dir
}

// Path is the live log file ("" when there is none).
func (l *Logger) Path() string {
	if l == nil || l.dir == "" {
		return ""
	}
	return filepath.Join(l.dir, FileName)
}

// Writing reports whether the file is open (false before the writer has
// opened it, and for good after it couldn't).
func (l *Logger) Writing() bool { return l != nil && l.w.Opened() }

// Close writes what's queued (waiting at most d) and closes the file.
func (l *Logger) Close(d time.Duration) {
	if l != nil {
		l.w.Close(d)
	}
}

// Tail returns up to n of the newest log lines, redacted: from the files
// (this run and earlier ones) when Clack writes one, otherwise this run's
// lines kept in memory. It waits up to a second for queued lines; call it
// off the UI goroutine.
func (l *Logger) Tail(n int) []string {
	if l == nil || n <= 0 {
		return nil
	}
	l.w.Flush(time.Second)
	if l.w.Opened() && l.dir != "" {
		p := filepath.Join(l.dir, FileName)
		lines := tailFile(p, n)
		if len(lines) < n {
			lines = append(tailFile(p+".1", n-len(lines)), lines...)
		}
		if len(lines) > 0 {
			return lines
		}
	}
	return l.w.Recent(n)
}

// tailFile is the last n lines of path (it reads at most its last 1 MiB).
func tailFile(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	const most = 1 << 20
	off := st.Size() - most
	if off < 0 {
		off = 0
	}
	b, err := io.ReadAll(io.NewSectionReader(f, off, st.Size()-off))
	if err != nil {
		return nil
	}
	if off > 0 { // the first line is cut
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// ---- the async writer ----

// Async is an io.Writer that never blocks: lines go through a buffered
// channel to one goroutine that writes them, redacted, to the sink open
// returns. A full buffer drops the line.
type Async struct {
	ch      chan item
	dropped atomic.Int64
	opened  atomic.Bool
	off     atomic.Bool // the sink couldn't open: drop everything
	done    chan struct{}
	close   sync.Once

	mu   sync.Mutex
	ring []string // the newest ringLen redacted lines
	next int
}

type item struct {
	line []byte
	ack  chan struct{} // Flush/Close: closed once everything before it is written
	quit bool
}

// NewAsync starts the writer goroutine; it calls open first, then writes
// lines as they come. If open fails, warn is called once and every line
// is dropped. queue is the buffer length in lines.
func NewAsync(open func() (io.WriteCloser, error), warn func(error), queue int) *Async {
	a := &Async{ch: make(chan item, queue), done: make(chan struct{})}
	go a.run(open, warn)
	return a
}

// Write queues a copy of p (one log record). It never blocks.
func (a *Async) Write(p []byte) (int, error) {
	if a.off.Load() {
		a.keep(p)
		return len(p), nil
	}
	b := make([]byte, len(p))
	copy(b, p)
	select {
	case a.ch <- item{line: b}:
	default:
		a.dropped.Add(1)
	}
	return len(p), nil
}

// keep puts a redacted line in the memory ring (no file to tail).
func (a *Async) keep(p []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.keepLocked(p)
}

func (a *Async) keepLocked(p []byte) {
	s := strings.TrimRight(Redact(string(p)), "\n")
	for _, l := range strings.Split(s, "\n") {
		if len(a.ring) < ringLen {
			a.ring = append(a.ring, l)
			continue
		}
		a.ring[a.next] = l
		a.next = (a.next + 1) % ringLen
	}
}

// Recent is up to n of the newest lines kept in memory.
func (a *Async) Recent(n int) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	all := append(append([]string(nil), a.ring[a.next:]...), a.ring[:a.next]...)
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all
}

// Dropped is how many lines were dropped so far for a full buffer.
func (a *Async) Dropped() int64 { return a.dropped.Load() }

// Opened reports whether the sink is open.
func (a *Async) Opened() bool { return a.opened.Load() }

// Flush waits, at most d, until every line queued before it is written.
func (a *Async) Flush(d time.Duration) bool { return a.marker(item{ack: make(chan struct{})}, d) }

// Close writes what's queued (waiting at most d), then closes the sink.
// Later writes are dropped.
func (a *Async) Close(d time.Duration) {
	a.close.Do(func() {
		a.marker(item{ack: make(chan struct{}), quit: true}, d)
	})
}

func (a *Async) marker(it item, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case a.ch <- it:
	case <-a.done:
		return true
	case <-t.C:
		return false
	}
	select {
	case <-it.ack:
		return true
	case <-a.done:
		return true
	case <-t.C:
		return false
	}
}

func (a *Async) run(open func() (io.WriteCloser, error), warn func(error)) {
	defer close(a.done)
	w, err := open()
	if err != nil {
		// What was queued before the open failed stays in memory, in order:
		// writers wait on mu to keep theirs until this is done.
		a.mu.Lock()
		a.off.Store(true)
		var acks []chan struct{}
	drain:
		for {
			select {
			case it := <-a.ch:
				if it.line != nil {
					a.keepLocked(it.line)
				}
				if it.ack != nil {
					acks = append(acks, it.ack)
				}
			default:
				break drain
			}
		}
		a.mu.Unlock()
		// Warn before releasing Flush/Close: a Close that has returned
		// has said why there's no file (Clack may exit right after it).
		if warn != nil {
			warn(err)
		}
		for _, c := range acks {
			close(c)
		}
		return
	}
	a.opened.Store(true)
	failing := false
	for it := range a.ch {
		if it.line != nil {
			if n := a.dropped.Swap(0); n > 0 {
				_, _ = fmt.Fprintf(w, "%s applog: %d log lines dropped (the disk was too slow)\n", time.Now().Format("2006/01/02 15:04:05"), n)
			}
			s := Redact(string(it.line))
			if _, err := io.WriteString(w, s); err != nil {
				if !failing && warn != nil {
					warn(err) // once per run of failures (a full disk)
				}
				failing = true
			} else {
				failing = false
			}
		}
		if it.quit { // closed before Close returns (Windows can't remove an open file)
			a.off.Store(true)
			a.opened.Store(false)
			_ = w.Close()
			close(it.ack)
			return
		}
		if it.ack != nil {
			close(it.ack)
		}
	}
}
