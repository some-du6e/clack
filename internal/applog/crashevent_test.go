package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fileTime is t as ProcessCreationTime carries it (hex FILETIME).
func fileTime(t time.Time) string {
	return fmt.Sprintf("0x%x", uint64(t.UnixNano()/100)+116444736000000000)
}

// A run that ended without saying why: the next start finds its
// Application Error event (named data, Windows 10 and later, or in the
// template's order) and logs the exception and module Windows recorded,
// but only Clack's own process: its executable's name, its pid, and its
// creation time the run record's start. Not another program's event, nor
// one of a later process given the same pid, and one event that doesn't
// decode hides no other.
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
	named := func(app string, pid int, created time.Time, code string) string {
		when := created.Add(3 * time.Minute)
		return fmt.Sprintf(`<Event xmlns="http://schemas.microsoft.com/win/2004/08/events/event"><System><Provider Name="Application Error"/><EventID>1000</EventID><TimeCreated SystemTime="%s"/></System><EventData>`+
			`<Data Name="AppName">%s</Data><Data Name="AppVersion">0.1.0.0</Data><Data Name="AppTimeStamp">0</Data><Data Name="ModuleName">ntdll.dll</Data><Data Name="ModuleVersion">10.0.26100.1</Data><Data Name="ModuleTimeStamp">0</Data>`+
			`<Data Name="ExceptionCode">%s</Data><Data Name="FaultingOffset">00000000000ff1a9</Data><Data Name="ProcessId">0x%x</Data><Data Name="ProcessCreationTime">%s</Data><Data Name="AppPath">C:\%s</Data><Data Name="ModulePath">C:\Windows\ntdll.dll</Data></EventData></Event>`,
			when.Format(time.RFC3339Nano), app, code, pid, fileTime(created), app)
	}
	unnamed := strings.NewReplacer(` Name="AppName"`, "", ` Name="AppVersion"`, "", ` Name="AppTimeStamp"`, "", ` Name="ModuleName"`, "", ` Name="ModuleVersion"`, "",
		` Name="ModuleTimeStamp"`, "", ` Name="ExceptionCode"`, "", ` Name="FaultingOffset"`, "", ` Name="ProcessId"`, "", ` Name="ProcessCreationTime"`, "",
		` Name="AppPath"`, "", ` Name="ModulePath"`, "")
	created := started.Add(-300 * time.Millisecond)
	for _, c := range []struct {
		name, xml, want string
	}{
		{"its event", named("clack.exe", 4242, created, "c0000005") + named("clack.exe", 9736, created, "c0000374"), "exception 0xc0000374 (STATUS_HEAP_CORRUPTION) in ntdll.dll at offset 0xff1a9"},
		{"its event, data in order", unnamed.Replace(named("Clack.exe", 9736, created, "c0000409")), "exception 0xc0000409 (STATUS_STACK_BUFFER_OVERRUN, a fail-fast) in ntdll.dll"},
		{"a bad event before its own", "<Event><EventData><Data>&bogus;</Data></EventData></Event>" + named("clack.exe", 9736, created, "c0000374"), "STATUS_HEAP_CORRUPTION"},
		{"another program, its pid", named("notepad.exe", 9736, created, "c0000374"), ""},
		{"its pid, a later process (reused)", named("clack.exe", 9736, started.Add(2*time.Hour), "c0000374"), ""},
		{"another pid's only", named("clack.exe", 4242, created, "c0000374"), ""},
		{"no events", "", ""},
		{"not XML", "<Event><<<", ""},
	} {
		got := crashRecordIn([]byte(c.xml), "clack.exe", pid, started)
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if got, ok := fileTimeHex(fileTime(started)); !ok || !got.Equal(started) {
		t.Errorf("FILETIME round trip: %v %v", got, ok)
	}
}
