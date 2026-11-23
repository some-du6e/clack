package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A run that ended without saying why: the next start finds its pid's
// Application Error event (named data, Windows 10 and later, or in the
// template's order) and logs the exception and module Windows recorded,
// not another process's event or one from before the run started.
func TestCrashRecordFor(t *testing.T) {
	started := time.Date(2026, 11, 21, 17, 30, 0, 0, time.UTC)
	dir := t.TempDir()
	rec := filepath.Join(dir, RunFileName)
	header := fmt.Sprintf("clack v0.1.0 windows/amd64 pid %d, started %s", 9736, started.Format(time.RFC3339))
	if err := os.WriteFile(rec, []byte(runStartedPrefix+header+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := previousRun(rec)
	pid, at, ok := endedUnsaid(prev)
	if !ok || pid != 9736 || !at.Equal(started) {
		t.Fatalf("from %q: pid %d, started %v, %v", prev, pid, at, ok)
	}
	if _, _, ok := endedUnsaid("applog: previous run (" + header + ") ended quit from the tray"); ok {
		t.Error("a run that said why it ended read as one that didn't")
	}
	named := func(pid int, when time.Time, code string) string {
		return fmt.Sprintf(`<Event xmlns="http://schemas.microsoft.com/win/2004/08/events/event"><System><Provider Name="Application Error"/><EventID>1000</EventID><TimeCreated SystemTime="%s"/></System><EventData>`+
			`<Data Name="AppName">clack.exe</Data><Data Name="AppVersion">0.1.0.0</Data><Data Name="AppTimeStamp">0</Data><Data Name="ModuleName">ntdll.dll</Data><Data Name="ModuleVersion">10.0.26100.1</Data><Data Name="ModuleTimeStamp">0</Data>`+
			`<Data Name="ExceptionCode">%s</Data><Data Name="FaultingOffset">00000000000ff1a9</Data><Data Name="ProcessId">0x%x</Data><Data Name="ProcessCreationTime">0x1</Data><Data Name="AppPath">C:\clack.exe</Data><Data Name="ModulePath">C:\Windows\ntdll.dll</Data></EventData></Event>`,
			when.Format(time.RFC3339Nano), code, pid)
	}
	unnamed := strings.NewReplacer(` Name="AppName"`, "", ` Name="AppVersion"`, "", ` Name="AppTimeStamp"`, "", ` Name="ModuleName"`, "", ` Name="ModuleVersion"`, "",
		` Name="ModuleTimeStamp"`, "", ` Name="ExceptionCode"`, "", ` Name="FaultingOffset"`, "", ` Name="ProcessId"`, "", ` Name="ProcessCreationTime"`, "",
		` Name="AppPath"`, "", ` Name="ModulePath"`, "")
	later := started.Add(3 * time.Minute)
	for _, c := range []struct {
		name, xml, want string
	}{
		{"its event", named(4242, later, "c0000005") + named(9736, later, "c0000374"), "exception 0xc0000374 (STATUS_HEAP_CORRUPTION) in ntdll.dll at offset 0xff1a9"},
		{"its event, data in order", unnamed.Replace(named(9736, later, "c0000409")), "exception 0xc0000409 (STATUS_STACK_BUFFER_OVERRUN, a fail-fast) in ntdll.dll"},
		{"another pid's only", named(4242, later, "c0000374"), ""},
		{"its pid, before the run started (an earlier process's)", named(9736, started.Add(-time.Hour), "c0000374"), ""},
		{"no events", "", ""},
		{"not XML", "<Event><<<", ""},
	} {
		got := crashRecordIn([]byte(c.xml), pid, started)
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
