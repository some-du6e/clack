package applog

// Windows' own record of how the previous run ended. A crash outside Go's
// sight, a fail-fast like a heap corruption (0xc0000374, Karim's quote
// paste on 2026-11-21) above all, ends the process before anything of
// Clack's can write: no crash.log, no exit line, and the next start's run
// record says only that the run "ended without saying why". Windows logs
// it anyway: Application Error, event 1000, in the Application event log,
// naming the exception, the faulting module and offset, and the process
// id. After such an end, the next start looks the previous run's pid up
// there (CrashRecordFor) and logs what Windows saw.

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// runHeader matches StartRun's header in its line: "(... pid 123, started
// <RFC3339>)" (RFC3339 has no ")").
var runHeader = regexp.MustCompile(`pid (\d+), started ([^)\s]+)\)`)

// endedUnsaid reports whether prev (StartRun's line) is a run that ended
// without saying why, and that run's pid and start.
func endedUnsaid(prev string) (pid int, started time.Time, ok bool) {
	if !strings.HasPrefix(prev, "applog: previous run ended without saying why (") {
		return 0, time.Time{}, false
	}
	m := runHeader.FindStringSubmatch(prev)
	if m == nil {
		return 0, time.Time{}, false
	}
	pid, err := strconv.Atoi(m[1])
	if err != nil || pid <= 0 {
		return 0, time.Time{}, false
	}
	started, err = time.Parse(time.RFC3339, m[2])
	if err != nil {
		return 0, time.Time{}, false
	}
	return pid, started, true
}

// appErrorEvent is one Application Error event (1000) as wevtutil prints it
// (/f:xml): its time and its data, named (Windows 10 and later) or in order.
type appErrorEvent struct {
	System struct {
		Time struct {
			SystemTime string `xml:"SystemTime,attr"`
		} `xml:"TimeCreated"`
	} `xml:"System"`
	Data []struct {
		Name  string `xml:"Name,attr"`
		Value string `xml:",chardata"`
	} `xml:"EventData>Data"`
}

// The data's order when unnamed (the event's template).
var appErrorFields = []string{"AppName", "AppVersion", "AppTimeStamp", "ModuleName", "ModuleVersion", "ModuleTimeStamp",
	"ExceptionCode", "FaultingOffset", "ProcessId", "ProcessCreationTime", "AppPath", "ModulePath"}

func (e appErrorEvent) field(name string) string {
	for i, d := range e.Data {
		if d.Name == name || d.Name == "" && i < len(appErrorFields) && appErrorFields[i] == name {
			return strings.TrimSpace(d.Value)
		}
	}
	return ""
}

// crashRecordIn finds pid's Application Error event at or after started in
// wevtutil's XML (events one after another, no root) and says what it
// records, "" when there is none.
func crashRecordIn(events []byte, pid int, started time.Time) string {
	d := xml.NewDecoder(strings.NewReader("<Events>" + string(events) + "</Events>"))
	var all struct {
		Events []appErrorEvent `xml:"Event"`
	}
	if err := d.Decode(&all); err != nil {
		return ""
	}
	for _, e := range all.Events {
		p, err := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(e.field("ProcessId")), "0x"), 16, 64)
		if err != nil || int(p) != pid {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, e.System.Time.SystemTime)
		if err != nil || at.Before(started.Add(-time.Second)) {
			continue
		}
		code := strings.ToLower(strings.TrimPrefix(strings.ToLower(e.field("ExceptionCode")), "0x"))
		name := ""
		if n, ok := exceptionNames[code]; ok {
			name = " (" + n + ")"
		}
		return fmt.Sprintf("applog: Windows recorded the previous run's end (pid %d, %s): exception 0x%s%s in %s at offset 0x%s (%s; Application Error, event 1000)",
			pid, at.Local().Format(time.RFC3339), code, name, e.field("ModuleName"), strings.TrimLeft(strings.ToLower(e.field("FaultingOffset")), "0x"), e.field("AppName"))
	}
	return ""
}

// exceptionNames names the codes a native crash usually ends with.
var exceptionNames = map[string]string{
	"c0000005": "STATUS_ACCESS_VIOLATION",
	"c0000374": "STATUS_HEAP_CORRUPTION",
	"c0000409": "STATUS_STACK_BUFFER_OVERRUN, a fail-fast",
	"c00000fd": "STATUS_STACK_OVERFLOW",
	"c0000602": "STATUS_FAIL_FAST_EXCEPTION",
	"80000003": "a breakpoint",
	"e06d7363": "a C++ exception",
}
