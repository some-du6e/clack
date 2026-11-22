package applog

import (
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startLogged is Start for a test: the standard logger writes to l's file
// (Exit's line goes through it), put back when the test ends.
func startLogged(t *testing.T, dir string) *Logger {
	t.Helper()
	l := start(dir, nil, io.Discard)
	log.SetOutput(l.Output(io.Discard))
	t.Cleanup(func() { log.SetOutput(os.Stderr); l.Close(time.Second) })
	return l
}

// startOpen is start for a test, closed when the test ends (Exit leaves
// the log open; Windows can't remove a temporary directory with an open
// file in it).
func startOpen(t *testing.T, dir string) *Logger {
	t.Helper()
	l := start(dir, nil, io.Discard)
	t.Cleanup(func() { l.Close(time.Second) })
	return l
}

// A run that says why it ended leaves "exit: <why>" in clack.log and the
// reason in its run record, which the next start logs; a run ended from
// outside (killed: no Exit) is "previous run ended without saying why" at
// the next start. A second launch that hands over keeps its exit line out
// of the running instance's record, and only a run's first Exit counts.
func TestExitReasonNextStart(t *testing.T) {
	dir := t.TempDir()
	logged := func() string {
		b, _ := os.ReadFile(filepath.Join(dir, FileName))
		return string(b)
	}

	// a first start: nothing before it
	one := startLogged(t, dir)
	if prev := startRun(one, "inst", "clack v1 pid 1, started then"); prev != "" {
		t.Errorf("first start: %q", prev)
	}
	// a second launch handing over: its exit line, not the record's
	hand := startOpen(t, dir)
	hand.Exit("handed this launch to the Clack already running", 2*time.Second)
	one.Exit("Quit in the tray menu", 2*time.Second)
	one.Exit("the window closed", 2*time.Second) // the close the Quit caused
	if s := logged(); !strings.Contains(s, "exit: Quit in the tray menu\n") || strings.Contains(s, "exit: the window closed") {
		t.Errorf("clack.log after the first run:\n%s", s)
	}

	// the next start names the reason
	two := startLogged(t, dir)
	prev := startRun(two, "inst", "clack v2 pid 2, started now")
	if !strings.HasPrefix(prev, "applog: previous run (clack v1 pid 1, started then) ended ") || !strings.HasSuffix(prev, ": Quit in the tray menu") {
		t.Errorf("after a Quit: %q", prev)
	}

	// two is killed: no Exit. The start after it says so.
	three := startLogged(t, dir)
	prev = startRun(three, "inst", "clack v3 pid 3, started later")
	if !strings.HasPrefix(prev, "applog: previous run ended without saying why (clack v2 pid 2, started now)") {
		t.Errorf("after a kill: %q", prev)
	}
	rec, _ := os.ReadFile(runFile(dir, "inst"))
	if string(rec) != runStartedPrefix+"clack v3 pid 3, started later\n" {
		t.Errorf("the run record of a run going on: %q", rec)
	}
}

// The kill is a real one: a process that started its run record and is
// killed (Task Manager's End task, TerminateProcess on Windows) leaves
// the record as it was, and the next start logs that it ended without
// saying why.
func TestExitReasonAfterKill(t *testing.T) {
	if dir := os.Getenv("CLACK_EXIT_HELPER_DIR"); dir != "" {
		l := start(dir, nil, io.Discard)
		startRun(l, "inst", "helper run")
		os.Stdout.WriteString("running\n")
		time.Sleep(time.Minute) // until killed
		return
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestExitReasonAfterKill$", "-test.count=1")
	cmd.Env = append(os.Environ(), "CLACK_EXIT_HELPER_DIR="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	if n, _ := io.ReadAtLeast(out, buf, len("running\n")); !strings.Contains(string(buf[:n]), "running") {
		t.Fatalf("the helper said %q", buf[:n])
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	next := startLogged(t, dir)
	if prev := startRun(next, "inst", "next run"); !strings.HasPrefix(prev, "applog: previous run ended without saying why (helper run)") {
		t.Errorf("after the kill: %q", prev)
	}
}

// After a signal's or a session end's early Exit, the shutdown it starts
// still reaches clack.log, and the later Exit (exitWith's) flushes it
// without a second exit line. Review of karim-1118: the early Exit closed
// the file, so the window's close, the plugins' stop and a fallback after
// a hang were never logged.
func TestExitKeepsLogging(t *testing.T) {
	dir := t.TempDir()
	l := startLogged(t, dir)
	startRun(l, "inst", "clack v1 pid 1, started then")
	l.Exit("signal: terminated", 2*time.Second)
	log.Print("ui: the window closed after the signal")
	l.Exit("the window closed", 2*time.Second)
	b, _ := os.ReadFile(filepath.Join(dir, FileName))
	s := string(b)
	if !strings.Contains(s, "exit: signal: terminated\n") || !strings.Contains(s, "ui: the window closed after the signal\n") {
		t.Errorf("clack.log doesn't have the exit line and the shutdown after it:\n%s", s)
	}
	if strings.Contains(s, "exit: the window closed") {
		t.Errorf("a second exit line:\n%s", s)
	}
}

// Two instances (separate CLACK_HOME directories) sharing a log directory
// keep their own run records: the second's start doesn't call the first,
// still running, killed, and the first's end doesn't land in the second's
// record. Review of karim-1118: one record per log directory, while
// single-instance is per config directory.
func TestRunRecordPerInstance(t *testing.T) {
	dir := t.TempDir()
	a := startLogged(t, dir)
	if prev := startRun(a, "aaaa", "clack A pid 1, started then"); prev != "" {
		t.Errorf("the first instance's start: %q", prev)
	}
	b := startOpen(t, dir)
	if prev := startRun(b, "bbbb", "clack B pid 2, started now"); prev != "" {
		t.Errorf("the second instance's start: %q, want nothing about the first, which is still running", prev)
	}
	a.Exit("Quit in the menu", 2*time.Second)
	ra, _ := os.ReadFile(runFile(dir, "aaaa"))
	rb, _ := os.ReadFile(runFile(dir, "bbbb"))
	if !strings.Contains(string(ra), ": Quit in the menu") {
		t.Errorf("A's record %q has no reason", ra)
	}
	if string(rb) != runStartedPrefix+"clack B pid 2, started now\n" {
		t.Errorf("B's record %q, want B's start alone", rb)
	}
}

// A run that let go of the instance socket before its end was recorded (a
// quick relaunch took the record over) doesn't write its reason into the
// new run's record. Review of karim-1118 (inst.Close before the exit).
func TestExitLeavesATakenRecord(t *testing.T) {
	dir := t.TempDir()
	old := startLogged(t, dir)
	startRun(old, "inst", "clack v1 pid 1, started then")
	next := startOpen(t, dir)
	startRun(next, "inst", "clack v1 pid 2, started now") // the relaunch
	old.Exit("Quit in the menu", 2*time.Second)
	if rec, _ := os.ReadFile(runFile(dir, "inst")); string(rec) != runStartedPrefix+"clack v1 pid 2, started now\n" {
		t.Errorf("the new run's record: %q", rec)
	}
}
