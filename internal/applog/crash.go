package applog

// Crash capture. A Go panic nothing recovers, and the runtime's fatal
// errors (concurrent map writes, out of memory, a deadlock), print their
// message and every goroutine's stack straight to fd 2, not through the
// log package, so a Clack started from the Dock (stderr goes nowhere)
// died without a trace in its log. CaptureCrashes hands the runtime a
// second crash output (runtime/debug.SetCrashOutput), crash.log next to
// clack.log, opened at start with a one-line header. That covers every
// goroutine, the main thread included, with no recover wrapper to forget.
//
// The runtime writes that file raw. On the next start CaptureCrashes
// copies what came after the header into clack.log as one redacted record
// ("the previous run crashed") and starts crash.log afresh, so the trace
// ends up in the log a bug report attaches, redacted like every other line.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
)

// CrashFileName is the runtime's crash output in Dir.
const CrashFileName = "crash.log"

// maxCrashImport bounds how much of an old crash.log is copied into the
// log (a crash with thousands of goroutines writes megabytes).
const maxCrashImport = 256 << 10

// CaptureCrashes sends the runtime's crash output to Dir/crash.log as
// well as stderr, after copying the previous run's crash, if there was
// one, into the log. header names this run ("clack v0.1.0 pid 123").
// It works on its own goroutine (no disk access on the caller's thread);
// a crash in the first moments of a run may miss the file.
func (l *Logger) CaptureCrashes(header string) {
	if l == nil || l.dir == "" {
		return
	}
	done := l.crash.start()
	go func() {
		defer close(done) // PreviousCrash's wait
		if err := l.captureCrashes(header); err != nil {
			// in clack.log too: a start with no capture says why
			log.Printf("applog: no crash capture this run: %v", err)
		}
	}()
}

func (l *Logger) captureCrashes(header string) error {
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(l.dir, CrashFileName)
	// this run's alone while it runs (another Clack's launch that hands its
	// link over and exits mustn't start the file afresh under the running
	// one's crash output)
	// held for a moment by another Clack's launch handing its link over
	// (it opens and exits): try again for up to 2 s
	f, err := openCrashFile(path)
	for i := 0; errors.Is(err, errCrashFileHeld) && i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		f, err = openCrashFile(path)
	}
	if err != nil {
		return err
	}
	defer f.Close() // the runtime and the native capture keep their own duplicates
	if rec, c := crashRecord(f); rec != "" {
		_, _ = l.w.Write([]byte(rec))
		l.w.Flush(2 * time.Second) // in clack.log before crash.log starts afresh
		if c != nil {
			l.crash.set(c) // for the opt-in crash report (PreviousCrash)
		}
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "%s %s\n", crashHeaderPrefix, header); err != nil {
		return err
	}
	// Windows: a fault outside Go code leaves its record too, and with no
	// stderr fd 2 is this file (crash_windows.go); the runtime's crash
	// output would then write each Go crash twice
	if armNative(f, func() { l.w.Flush(time.Second) }) {
		return nil
	}
	return debug.SetCrashOutput(f, debug.CrashOptions{})
}

// errCrashFileHeld: another running Clack's crash output is crash.log.
var errCrashFileHeld = errors.New("another Clack running from this log folder holds " + CrashFileName)

// releaseCrashCapture hands crash.log back (tests: Windows can't remove
// an open file).
func releaseCrashCapture() {
	_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
	releaseNative()
}

const crashHeaderPrefix = "clack crash output:"

// previousCrash is the log record for what the runtime wrote to path in
// the previous run (after the header line), or "" when it wrote nothing.
func previousCrash(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	return previousCrashFrom(f)
}

// crashMarks are what a crash's report holds: the runtime's (a panic, a
// fatal error, a signal, a Windows exception Go caught, its goroutine
// dump) or the native capture's (crash_windows.go writeNative).
var crashMarks = [][]byte{
	[]byte("panic: "), []byte("fatal error: "), []byte("runtime: "), []byte("goroutine "),
	[]byte("[signal "), []byte("Exception 0x"), []byte("native exception 0x"),
}

// crashMarked reports whether b holds a crash's report.
func crashMarked(b []byte) bool {
	for _, m := range crashMarks {
		if bytes.Contains(b, m) {
			return true
		}
	}
	return false
}

// firstLine is b's first line, at most 120 bytes of it.
func firstLine(b []byte) string {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		b = b[:i]
	}
	if len(b) > 120 {
		b = b[:120]
	}
	return string(bytes.TrimSpace(b))
}

// previousCrashFrom is previousCrash for the open file f (read from its
// current offset, the start).
func previousCrashFrom(f *os.File) string {
	rec, _ := crashRecord(f)
	return rec
}

// crashRecord is previousCrashFrom's record and, when the record is a
// crash's, the crash itself.
func crashRecord(f *os.File) (string, *Crash) {
	b, err := io.ReadAll(io.LimitReader(f, maxCrashImport+1))
	if err != nil {
		return "", nil
	}
	header := ""
	if bytes.HasPrefix(b, []byte(crashHeaderPrefix)) {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			header = string(bytes.TrimSpace(b[len(crashHeaderPrefix):i]))
			b = b[i+1:]
		} else {
			b = nil
		}
	}
	b = bytes.TrimRight(b, "\n")
	if len(bytes.TrimSpace(b)) == 0 {
		return "", nil
	}
	if !crashMarked(b) {
		// Windows with no stderr: crash.log is fd 2 for the whole run, so
		// what a DLL writes to stderr lands here too; only the runtime's
		// own report, or the native capture's, is a crash
		return fmt.Sprintf("%s applog: the previous run's crash.log holds no crash, only other output (%d bytes, its first line %q)\n",
			time.Now().Format("2006/01/02 15:04:05"), len(b), firstLine(b)), nil
	}
	cut := ""
	if len(b) > maxCrashImport {
		b, cut = b[:maxCrashImport], "\n[crash output cut at 256 KiB]"
	}
	c := &Crash{Header: header, Output: string(b)}
	when := ""
	if st, err := f.Stat(); err == nil {
		c.When = st.ModTime()
		when = " at " + st.ModTime().Format("2006/01/02 15:04:05")
	}
	if header != "" {
		header = " (" + header + ")"
	}
	return fmt.Sprintf("%s applog: the previous run crashed%s%s; its crash output:\n%s%s\n",
		time.Now().Format("2006/01/02 15:04:05"), header, when, b, cut), c
}

// Crash is the previous run's crash as this start's capture found it in
// crash.log, raw (at most 256 KiB of it): what the opt-in crash report
// sends, scrubbed (internal/sentry), at the next start.
type Crash struct {
	Header string    // the crashed run's ("clack v0.1.0 windows/amd64 pid 123, started …")
	Output string    // what the runtime, or the native capture, wrote after the header
	When   time.Time // crash.log's last write (zero: unknown)
}

// crashState is CaptureCrashes' result for PreviousCrash.
type crashState struct {
	mu   sync.Mutex
	done chan struct{} // closed once the capture read crash.log (nil: no capture)
	prev *Crash
}

func (s *crashState) start() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done == nil {
		s.done = make(chan struct{})
	}
	return s.done
}

func (s *crashState) set(c *Crash) {
	s.mu.Lock()
	s.prev = c
	s.mu.Unlock()
}

// PreviousCrash waits (at most d) for this start's crash capture to read
// crash.log (CaptureCrashes), and returns the previous run's crash; ok is
// false when it hadn't crashed, or no capture ran (a second launch, no
// log folder).
func (l *Logger) PreviousCrash(d time.Duration) (Crash, bool) {
	if l == nil {
		return Crash{}, false
	}
	l.crash.mu.Lock()
	done := l.crash.done
	l.crash.mu.Unlock()
	if done == nil {
		return Crash{}, false
	}
	select {
	case <-done:
	case <-time.After(d):
		return Crash{}, false
	}
	l.crash.mu.Lock()
	defer l.crash.mu.Unlock()
	if l.crash.prev == nil {
		return Crash{}, false
	}
	return *l.crash.prev, true
}
