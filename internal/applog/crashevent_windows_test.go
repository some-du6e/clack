//go:build windows

package applog

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The real query: a child (this test binary again) ends in a fail-fast,
// as a heap corruption ends a process (RaiseFailFastException: no handler
// of Go's runs, nothing of Clack's can write), and Windows' Application
// Error event for it is what CrashRecordFor finds, by the child's pid and
// start; a start an hour off (another process with the pid) finds
// nothing. It leaves a crash event in the runner's log, so it runs only
// where CLACK_WINDOWS_WER_TESTS is set (ci's windows-applog job).
func TestCrashRecordForRealEvent(t *testing.T) {
	if os.Getenv("CLACK_CRASH_CHILD") == "1" {
		fmt.Printf("child pid %d, started %s\n", os.Getpid(), time.Now().Format(time.RFC3339))
		os.Stdout.Sync()
		k32 := syscall.NewLazyDLL("kernel32.dll")
		k32.NewProc("RaiseFailFastException").Call(0, 0, 0)
		os.Exit(3) // not reached
	}
	if os.Getenv("CLACK_WINDOWS_WER_TESTS") == "" {
		t.Skip("crashes a child on purpose (an event in the Application log): set CLACK_WINDOWS_WER_TESTS=1")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashRecordForRealEvent$", "-test.count=1")
	cmd.Env = append(os.Environ(), "CLACK_CRASH_CHILD=1")
	out, err := cmd.Output()
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	t.Logf("the child: %q, exit code 0x%08x (%v)", out, uint32(code), err)
	var pid int
	var started string
	for _, l := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(l), "child pid "); ok {
			p, s, _ := strings.Cut(rest, ", started ")
			pid, _ = strconv.Atoi(p)
			started = s
		}
	}
	if pid == 0 || started == "" {
		t.Fatal("the child didn't say its pid and start")
	}
	if code == 3 || code == 0 {
		t.Fatal("the child lived past its fail-fast")
	}
	prev := fmt.Sprintf("applog: previous run ended without saying why (pid %d, started %s)", pid, started)

	// Windows Error Reporting writes the event after the process ended,
	// on its own time
	var line string
	deadline := time.Now().Add(90 * time.Second)
	for line == "" && time.Now().Before(deadline) {
		if line = CrashRecordFor(prev); line == "" {
			time.Sleep(2 * time.Second)
		}
	}
	t.Logf("CrashRecordFor: %s", line)
	if line == "" {
		t.Fatalf("no Application Error event for pid %d in 90 s", pid)
	}
	for _, want := range []string{fmt.Sprintf("pid %d,", pid), "exception 0x", "Application Error, event 1000"} {
		if !strings.Contains(line, want) {
			t.Errorf("the record %q has no %q", line, want)
		}
	}

	at, err := time.Parse(time.RFC3339, started)
	if err != nil {
		t.Fatal(err)
	}
	other := fmt.Sprintf("applog: previous run ended without saying why (pid %d, started %s)", pid, at.Add(time.Hour).Format(time.RFC3339))
	if l := CrashRecordFor(other); l != "" {
		t.Errorf("a run with the pid an hour later took the child's event: %s", l)
	}
}
