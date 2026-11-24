//go:build windows

package applog

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Karim's α.113 Clack vanished twice (2026-11-21) with crash.log holding
// only its header: a fault outside Go code never reached it. A helper
// process with no stderr, as a windowsgui Clack started from the Start
// menu has, captures crashes and then faults:
//
//   - "go-thread": RtlMoveMemory onto address 8, called from a goroutine
//     (an access violation inside ntdll on a Go thread). The runtime's
//     report ("Exception 0xc0000005 …", the goroutines) goes to fd 2, which
//     is crash.log now.
//   - "foreign-thread": a thread Windows starts at address 0x10, which Go
//     doesn't own. The unhandled-exception filter's record names the
//     exception, where it happened and the thread.
//
// Each record is in crash.log, and the next start copies it into
// clack.log.
func TestNativeFaultReachesTheLog(t *testing.T) {
	if mode := os.Getenv("CLACK_NATIVE_CRASH_HELPER"); mode != "" {
		nativeCrashHelper(t, mode, os.Getenv("CLACK_NATIVE_CRASH_DIR"))
		return
	}
	for _, c := range []struct {
		mode string
		want [][]string // each: one of these is in crash.log
	}{
		{"go-thread", [][]string{
			{"Exception 0xc0000005", "native exception 0xc0000005 (access violation, writing 0x8)"},
		}},
		{"foreign-thread", [][]string{
			{"native exception 0xc0000005 (access violation"},
			{"at 0x10 (in no module) on thread"},
			{"which started at 0x10 (in no module)"},
			{"seen by the unhandled-exception filter"},
		}},
	} {
		t.Run(c.mode, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeFaultReachesTheLog$", "-test.count=1", "-test.v")
			cmd.Env = append(os.Environ(), "CLACK_NATIVE_CRASH_HELPER="+c.mode, "CLACK_NATIVE_CRASH_DIR="+dir)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("the helper didn't crash:\n%s", out)
			}
			raw, rerr := os.ReadFile(filepath.Join(dir, CrashFileName))
			if rerr != nil {
				t.Fatal(rerr)
			}
			t.Logf("the helper ended with %v; its crash.log:\n%s", err, raw)
			if !strings.HasPrefix(string(raw), crashHeaderPrefix+" helper run, "+c.mode+"\n") {
				t.Errorf("crash.log doesn't start with the helper's header")
			}
			for _, alts := range c.want {
				if !containsAny(string(raw), alts) {
					t.Errorf("crash.log has none of %q", alts)
				}
			}

			// the next start copies it into the log
			t.Cleanup(releaseCrashCapture)
			l := start(dir, nil, io.Discard)
			closeForCleanup(t, l)
			if err := l.captureCrashes("next run"); err != nil {
				t.Fatal(err)
			}
			l.Close(30 * time.Second)
			logged, _ := os.ReadFile(filepath.Join(dir, FileName))
			if !strings.Contains(string(logged), "the previous run crashed (helper run, "+c.mode+")") {
				t.Errorf("clack.log lacks the import:\n%s", logged)
			}
			for _, alts := range c.want {
				if !containsAny(string(logged), alts) {
					t.Errorf("clack.log has none of %q", alts)
				}
			}
		})
	}
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func nativeCrashHelper(t *testing.T, mode, dir string) {
	// a windowsgui process started from the shell has no stderr
	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, 0); err != nil {
		t.Fatal(err)
	}
	// as Go's runtime starts a process (captureCrashes' werOn then lets
	// Windows Error Reporting record the crash; Go's
	// WER_FAULT_REPORTING_NO_UI keeps it from showing a dialog)
	windows.SetErrorMode(0x0001 | semNoGPFaultErrorBox) // SEM_FAILCRITICALERRORS
	l := start(dir, nil, io.Discard)
	if err := l.captureCrashes("helper run, " + mode); err != nil {
		t.Fatal(err)
	}
	k32 := windows.NewLazySystemDLL("kernel32.dll")
	switch mode {
	case "go-thread":
		done := make(chan struct{})
		go func() {
			defer close(done)
			var src [8]byte
			k32.NewProc("RtlMoveMemory").Call(8, uintptr(unsafe.Pointer(&src[0])), uintptr(len(src)))
		}()
		<-done
	case "foreign-thread":
		h, _, err := k32.NewProc("CreateThread").Call(0, 0, 0x10, 0, 0, 0)
		if h == 0 {
			t.Fatalf("CreateThread: %v", err)
		}
		windows.WaitForSingleObject(windows.Handle(h), windows.INFINITE)
	}
	time.Sleep(10 * time.Second)
	t.Fatal("still alive")
}
