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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
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
	go func() {
		if err := l.captureCrashes(header); err != nil {
			fmt.Fprintf(os.Stderr, "clack: no crash log: %v\n", err)
		}
	}()
}

func (l *Logger) captureCrashes(header string) error {
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(l.dir, CrashFileName)
	if rec := previousCrash(path); rec != "" {
		_, _ = l.w.Write([]byte(rec))
		l.w.Flush(2 * time.Second) // in clack.log before crash.log starts afresh
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close() // SetCrashOutput keeps its own duplicate of the descriptor
	if _, err := fmt.Fprintf(f, "%s %s\n", crashHeaderPrefix, header); err != nil {
		return err
	}
	return debug.SetCrashOutput(f, debug.CrashOptions{})
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
	b, err := io.ReadAll(io.LimitReader(f, maxCrashImport+1))
	if err != nil {
		return ""
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
		return ""
	}
	cut := ""
	if len(b) > maxCrashImport {
		b, cut = b[:maxCrashImport], "\n[crash output cut at 256 KiB]"
	}
	when := ""
	if st, err := f.Stat(); err == nil {
		when = " at " + st.ModTime().Format("2006/01/02 15:04:05")
	}
	if header != "" {
		header = " (" + header + ")"
	}
	return fmt.Sprintf("%s applog: the previous run crashed%s%s; its crash output:\n%s%s\n",
		time.Now().Format("2006/01/02 15:04:05"), header, when, b, cut)
}
