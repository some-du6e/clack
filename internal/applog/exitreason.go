package applog

// Why a run ended. Karim's α.108 froze after he opened it again from the
// notification area (2026-11-18), and nothing in clack.log said whether
// the process ended, or how: a Clack ended by Task Manager, by Windows
// closing the session, or by an installer's Restart Manager leaves no
// line, and the start after it couldn't say what came before.
//
// Every way the process ends calls Exit with the reason, which logs one
// "exit: <reason>" line and waits for it to reach the file; the log stays
// open, so the shutdown the reason starts is logged too. The primary
// instance (the one holding the instance socket) also keeps a run record
// next to clack.log, one per instance (lastrun-<instance>.txt: separate
// CLACK_HOME directories are separate instances sharing a log directory):
// a line naming the run when it starts, and a line with the reason when
// it ends. The next start reads it and logs the previous run's last
// reason, or that the previous run ended without saying why: it was
// killed, the computer lost power, or it froze and was ended.

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RunFileName is the run record in Dir of an instance with no key.
const RunFileName = "lastrun.txt"

// RunFile is the name of instance's run record in Dir.
func RunFile(instance string) string {
	if instance == "" {
		return RunFileName
	}
	return "lastrun-" + instance + ".txt"
}

const (
	runStartedPrefix = "run: "
	runEndedPrefix   = "ended "
)

// exitState is the Logger's run record and its one exit.
type exitState struct {
	mu     sync.Mutex
	run    string // the run record's path, once StartRun made it ("" for a second launch, a CLI)
	header string // its first line
	exited bool
	done   chan struct{} // closed once the first Exit has logged and recorded its reason
}

// StartRun is called by the primary instance once it holds the instance
// socket (instance: its key, desktop.InstanceKey). It returns the line
// about the previous run of that instance ("" on a first start) and
// starts this run's record, header naming it ("clack v0.1.0
// windows/amd64 pid 123, started …"). Exit later adds the reason. A
// second launch that hands over must not call it: the record is the
// running instance's.
func (l *Logger) StartRun(instance, header string) string {
	if l == nil || l.dir == "" {
		return ""
	}
	path := filepath.Join(l.dir, RunFile(instance))
	prev := previousRun(path)
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return prev
	}
	first := runStartedPrefix + header
	if err := os.WriteFile(path, []byte(first+"\n"), 0o600); err != nil {
		return prev
	}
	l.exit.mu.Lock()
	l.exit.run, l.exit.header = path, first
	l.exit.mu.Unlock()
	return prev
}

// previousRun is the line StartRun logs for the record at path ("" when
// there is none).
func previousRun(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	var header, ended string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, runStartedPrefix) && header == "":
			header = strings.TrimPrefix(line, runStartedPrefix)
		case strings.HasPrefix(line, runEndedPrefix):
			ended = strings.TrimPrefix(line, runEndedPrefix)
		}
	}
	if header == "" {
		return ""
	}
	if ended == "" {
		return fmt.Sprintf("applog: previous run ended without saying why (%s): no exit line, so it was ended from outside (Task Manager, a kill), lost power, or crashed (a crash's output would be just above)", header)
	}
	return fmt.Sprintf("applog: previous run (%s) ended %s", header, ended)
}

// Exit logs "exit: <reason>" and, for the primary instance, adds the
// reason to the run record, then waits (at most d) for what's queued for
// the log file to reach it. The log stays open: what the ending does next
// (a window closing, plugins stopping, a fallback after a hang) is logged
// too. Only the first call logs and records: the process is ending, and
// the first reason is the one that started it (a tray Quit, then the
// window's close). A later call waits (at most d) for the first to finish,
// then flushes again, so the exit that follows it loses nothing. Any
// goroutine.
func (l *Logger) Exit(reason string, d time.Duration) { l.exitLine(reason, d, true) }

// ExitQuiet is Exit with the exit line in the log file only, not on
// stderr: for the CLI's own ends (-version, clack login, a launch handed
// to the running Clack), whose terminal shows the command's output.
func (l *Logger) ExitQuiet(reason string, d time.Duration) { l.exitLine(reason, d, false) }

func (l *Logger) exitLine(reason string, d time.Duration, stderr bool) {
	if l == nil {
		if stderr {
			log.Printf("exit: %s", reason)
		}
		return
	}
	l.exit.mu.Lock()
	if l.exit.done == nil {
		l.exit.done = make(chan struct{})
	}
	done := l.exit.done
	if l.exit.exited {
		l.exit.mu.Unlock()
		select {
		case <-done:
		case <-time.After(d):
		}
		l.Flush(d)
		return
	}
	l.exit.exited = true
	run, header := l.exit.run, l.exit.header
	l.exit.mu.Unlock()
	defer close(done)
	if stderr || l.w == nil {
		log.Printf("exit: %s", reason)
	} else {
		log.New(l.w, log.Prefix(), log.Flags()).Printf("exit: %s", reason) // the file alone, as the standard logger formats it
	}
	if run != "" && recordIs(run, header) {
		if f, err := os.OpenFile(run, os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			_, _ = fmt.Fprintf(f, "%s%s: %s\n", runEndedPrefix, time.Now().Format(time.RFC3339), Redact(reason))
			_ = f.Close()
		}
	}
	l.Flush(d)
}

// recordIs reports whether the run record at path is still the one that
// starts with header: a Clack started after this one let go of the
// instance socket has made it its own, and this run's reason isn't
// that run's.
func recordIs(path, header string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	return sc.Scan() && strings.TrimSpace(sc.Text()) == header
}

// Flush waits (at most d) for what's queued for the log file to reach it.
func (l *Logger) Flush(d time.Duration) {
	if l == nil || l.w == nil {
		return
	}
	l.w.Flush(d)
}
