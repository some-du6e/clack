package applog

// Why a run ended. Karim's α.108 froze after he opened it again from the
// notification area (2026-11-18), and nothing in clack.log said whether
// the process ended, or how: a Clack ended by Task Manager, by Windows
// closing the session, or by an installer's Restart Manager leaves no
// line, and the start after it couldn't say what came before.
//
// Every way the process ends calls Exit with the reason, which logs one
// "exit: <reason>" line and waits for it to reach the file. The primary
// instance (the one holding the instance socket) also keeps a run record,
// lastrun.txt next to clack.log: a line naming the run when it starts,
// and a line with the reason when it ends. The next start reads it
// (PreviousRun) and logs the previous run's last reason, or that the
// previous run ended without saying why: it was killed, the computer lost
// power, or it froze and was ended.

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

// RunFileName is the run record in Dir.
const RunFileName = "lastrun.txt"

const (
	runStartedPrefix = "run: "
	runEndedPrefix   = "ended "
)

// exitState is the Logger's run record and its one exit.
type exitState struct {
	mu     sync.Mutex
	run    string // the run record's path, once StartRun made it ("" for a second launch, a CLI)
	exited bool
}

// StartRun is called by the primary instance once it holds the instance
// socket. It returns the line about the previous run ("" on a first
// start) and starts this run's record, header naming it ("clack v0.1.0
// windows/amd64 pid 123, started …"). Exit later adds the reason. Only
// one process per log directory should call it: a second launch that
// hands over must not overwrite the running instance's record.
func (l *Logger) StartRun(header string) string {
	if l == nil || l.dir == "" {
		return ""
	}
	path := filepath.Join(l.dir, RunFileName)
	prev := previousRun(path)
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return prev
	}
	if err := os.WriteFile(path, []byte(runStartedPrefix+header+"\n"), 0o600); err != nil {
		return prev
	}
	l.exit.mu.Lock()
	l.exit.run = path
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
// reason to the run record, then writes what's queued for the log file
// (waiting at most d) and closes it. Only the first call does anything:
// the process is ending, and the first reason is the one that started
// it (a tray Quit, then the window's close). Any goroutine.
func (l *Logger) Exit(reason string, d time.Duration) {
	if l == nil {
		log.Printf("exit: %s", reason)
		return
	}
	l.exit.mu.Lock()
	if l.exit.exited {
		l.exit.mu.Unlock()
		return
	}
	l.exit.exited = true
	run := l.exit.run
	l.exit.mu.Unlock()
	log.Printf("exit: %s", reason)
	if run != "" {
		if f, err := os.OpenFile(run, os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			_, _ = fmt.Fprintf(f, "%s%s: %s\n", runEndedPrefix, time.Now().Format(time.RFC3339), Redact(reason))
			_ = f.Close()
		}
	}
	l.w.Close(d)
}
