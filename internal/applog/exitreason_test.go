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
	if prev := one.StartRun("clack v1 pid 1, started then"); prev != "" {
		t.Errorf("first start: %q", prev)
	}
	// a second launch handing over: its exit line, not the record's
	hand := start(dir, nil, io.Discard)
	hand.Exit("handed this launch to the Clack already running", 2*time.Second)
	one.Exit("Quit in the tray menu", 2*time.Second)
	one.Exit("the window closed", 2*time.Second) // the close the Quit caused
	if s := logged(); !strings.Contains(s, "exit: Quit in the tray menu\n") || strings.Contains(s, "exit: the window closed") {
		t.Errorf("clack.log after the first run:\n%s", s)
	}

	// the next start names the reason
	two := startLogged(t, dir)
	prev := two.StartRun("clack v2 pid 2, started now")
	if !strings.HasPrefix(prev, "applog: previous run (clack v1 pid 1, started then) ended ") || !strings.HasSuffix(prev, ": Quit in the tray menu") {
		t.Errorf("after a Quit: %q", prev)
	}

	// two is killed: no Exit. The start after it says so.
	three := startLogged(t, dir)
	prev = three.StartRun("clack v3 pid 3, started later")
	if !strings.HasPrefix(prev, "applog: previous run ended without saying why (clack v2 pid 2, started now)") {
		t.Errorf("after a kill: %q", prev)
	}
	rec, _ := os.ReadFile(filepath.Join(dir, RunFileName))
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
		l.StartRun("helper run")
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
	if prev := next.StartRun("next run"); !strings.HasPrefix(prev, "applog: previous run ended without saying why (helper run)") {
		t.Errorf("after the kill: %q", prev)
	}
}
