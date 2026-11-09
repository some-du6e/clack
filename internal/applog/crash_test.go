package applog

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

// A panic on a goroutine nothing recovers kills the process with its
// trace on fd 2 only; with CaptureCrashes the runtime also writes it to
// crash.log, and the next start copies it, redacted, into clack.log.
func TestCrashReachesTheLog(t *testing.T) {
	if dir := os.Getenv("CLACK_CRASH_HELPER_DIR"); dir != "" {
		l := start(dir, nil, io.Discard)
		if err := l.captureCrashes("helper run"); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			var m map[string]int
			m["boom"] = 1 // assignment to entry in nil map: an unrecovered runtime panic
		}()
		<-done
		t.Fatal("still alive")
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashReachesTheLog$", "-test.count=1")
	cmd.Env = append(os.Environ(), "CLACK_CRASH_HELPER_DIR="+dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the helper didn't crash:\n%s", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, CrashFileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{crashHeaderPrefix + " helper run", "panic: assignment to entry in nil map", "goroutine ", "TestCrashReachesTheLog"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("crash.log lacks %q:\n%s", want, raw)
		}
	}

	// the next run copies it into the log and starts crash.log afresh
	// (this process's crash output then holds crash.log open: Windows can't
	// remove an open file, so hand it back before TempDir's cleanup)
	t.Cleanup(func() { debug.SetCrashOutput(nil, debug.CrashOptions{}) })
	l := start(dir, nil, io.Discard)
	if err := l.captureCrashes("next run"); err != nil {
		t.Fatal(err)
	}
	l.Close(2 * time.Second)
	logged, _ := os.ReadFile(filepath.Join(dir, FileName))
	for _, want := range []string{"the previous run crashed (helper run)", "panic: assignment to entry in nil map", "TestCrashReachesTheLog"} {
		if !strings.Contains(string(logged), want) {
			t.Errorf("clack.log lacks %q:\n%s", want, logged)
		}
	}
	fresh, _ := os.ReadFile(filepath.Join(dir, CrashFileName))
	if string(fresh) != crashHeaderPrefix+" next run\n" {
		t.Errorf("crash.log after the import: %q", fresh)
	}
}

// The import is redacted like every log line, and a clean previous run
// (just the header) adds nothing.
func TestPreviousCrashRedactedAndCleanRunSilent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, CrashFileName)
	tok := "xoxc-" + strings.Repeat("1234567890", 5)
	os.WriteFile(p, []byte(crashHeaderPrefix+" old\npanic: Get \"https://x.slack.com/api/a?token="+tok+"\"\n\ngoroutine 1 [running]:\n"), 0o600)
	rec := previousCrash(p)
	if !strings.Contains(rec, "the previous run crashed (old)") || !strings.Contains(rec, "goroutine 1 [running]") {
		t.Fatalf("record %q", rec)
	}
	if strings.Contains(Redact(rec), tok) {
		t.Errorf("the token survived redaction: %q", Redact(rec))
	}
	os.WriteFile(p, []byte(crashHeaderPrefix+" clean\n"), 0o600)
	if rec := previousCrash(p); rec != "" {
		t.Errorf("a clean run imported %q", rec)
	}
	os.Remove(p)
	if rec := previousCrash(p); rec != "" {
		t.Errorf("no file imported %q", rec)
	}
}
