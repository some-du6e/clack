//go:build windows

package applog

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

// CrashRecordFor is the line about Windows' record of the previous run's
// end, when prev (StartRun's line) says it ended without saying why and
// the Application event log has an Application Error event for its pid;
// "" otherwise. It runs wevtutil (a second or so): call it off the UI
// goroutine.
func CrashRecordFor(prev string) string {
	pid, started, ok := endedUnsaid(prev)
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "wevtutil", "qe", "Application",
		"/q:*[System[Provider[@Name='Application Error'] and (EventID=1000)]]", "/f:xml", "/rd:true", "/c:40")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return crashRecordIn(out, pid, started)
}
