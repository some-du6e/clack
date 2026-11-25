package applog

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The crash a start's capture finds in crash.log is what PreviousCrash
// hands the opt-in crash report (internal/sentry), raw, with the crashed
// run's header and when it was written; a clean crash.log, or one with
// other output only, is no crash. Once read, crash.log starts afresh, so
// the start after has nothing to report again.
func TestPreviousCrashForTheReport(t *testing.T) {
	t.Cleanup(releaseCrashCapture)
	dir := t.TempDir()
	p := filepath.Join(dir, CrashFileName)
	out := "panic: boom\n\ngoroutine 1 [running]:\nmain.main()\n\t/x/main.go:3 +0x1"
	if err := os.WriteFile(p, []byte(crashHeaderPrefix+" clack v1 linux/amd64 pid 7, started 2026-11-24T10:00:00Z\n"+out+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := start(dir, nil, io.Discard)
	closeForCleanup(t, l)
	l.CaptureCrashes("this run")
	c, ok := l.PreviousCrash(10 * time.Second)
	if !ok {
		t.Fatal("PreviousCrash found no crash in crash.log")
	}
	if c.Header != "clack v1 linux/amd64 pid 7, started 2026-11-24T10:00:00Z" || c.Output != out || c.When.IsZero() {
		t.Errorf("crash %+v", c)
	}
	if b, _ := os.ReadFile(p); string(b) != crashHeaderPrefix+" this run\n" {
		t.Errorf("crash.log after the start: %q", b)
	}
	releaseCrashCapture()

	for name, content := range map[string]string{
		"clean":        crashHeaderPrefix + " clean\n",
		"other output": crashHeaderPrefix + " old\nlibpng warning: iCCP: known incorrect sRGB profile\n",
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, CrashFileName), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		l := start(dir, nil, io.Discard)
		closeForCleanup(t, l)
		l.CaptureCrashes("this run")
		if c, ok := l.PreviousCrash(10 * time.Second); ok {
			t.Errorf("%s crash.log: PreviousCrash %+v", name, c)
		}
		releaseCrashCapture()
	}

	// no capture (a second launch, no log folder): nothing, at once
	var none *Logger
	if _, ok := none.PreviousCrash(time.Hour); ok {
		t.Error("a nil Logger had a crash")
	}
	t0 := time.Now()
	if _, ok := (&Logger{}).PreviousCrash(time.Hour); ok || time.Since(t0) > time.Second {
		t.Errorf("no capture: ok %v after %v", ok, time.Since(t0))
	}
}
