package sentry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/sentry/sentrytest"
)

// panicOutput is a crash.log as the runtime writes it (after applog's
// header line) for a panic in a dev build: absolute paths under the home
// directory, a quoted message, a token in the value.
const panicOutput = `panic: render "the launch moved to friday, keep it quiet" for C0123ABCD45: xoxc-111-222-333 [recovered]
	panic: render failed

goroutine 42 [running]:
github.com/some-du6e/clack/internal/ui/messages.(*List).layout(0xc000123400, {0x1, 0x2})
	/home/kayla/src/clack/internal/ui/messages/list.go:812 +0x1f4
github.com/some-du6e/clack/internal/ui.(*App).frame(0xc0000a8000)
	/home/kayla/src/clack/internal/ui/app.go:1033 +0x88
gioui.org/app.(*Window).run(...)
	/home/kayla/go/pkg/mod/gioui.org@v0.8.0/app/window.go:120
created by github.com/some-du6e/clack/internal/ui.(*App).Run in goroutine 1
	/home/kayla/src/clack/internal/ui/app.go:700 +0x2a5

goroutine 1 [chan receive]:
main.main()
	/home/kayla/src/clack/cmd/clack/main.go:500 +0x99
`

func TestCrashEventFromPanic(t *testing.T) {
	setHome(t, "/home/kayla")
	ev := CrashEvent(Crash{
		Output: panicOutput,
		Header: "clack dev linux/amd64 pid 4242, started 2026-11-24T10:00:00Z",
		When:   time.Date(2026, 11, 24, 10, 5, 0, 0, time.UTC),
		Exit:   "applog: previous run ended without saying why (clack dev linux/amd64 pid 4242, started 2026-11-24T10:00:00Z): no exit line",
	})
	if ev.Level != "fatal" || ev.Timestamp != "2026-11-24T10:05:00Z" {
		t.Errorf("level %q timestamp %q", ev.Level, ev.Timestamp)
	}
	x := ev.Exception.Values[0]
	if x.Type != "panic" || x.Mechanism == nil || x.Mechanism.Handled {
		t.Errorf("exception %q %+v", x.Type, x.Mechanism)
	}
	if want := `render "…" for <id>: xoxc-REDACTED`; x.Value != want {
		t.Errorf("value %q, want %q", x.Value, want)
	}
	if x.Stacktrace == nil {
		t.Fatal("no stack")
	}
	fr := x.Stacktrace.Frames
	if len(fr) != 4 {
		t.Fatalf("%d frames, want the crashing goroutine's 4: %+v", len(fr), fr)
	}
	// oldest first: the goroutine's creator, the crashing frame last
	last, first := fr[3], fr[0]
	if last.Module != "github.com/some-du6e/clack/internal/ui/messages" || last.Function != "(*List).layout" || last.Lineno != 812 || !last.InApp {
		t.Errorf("crashing frame %+v", last)
	}
	if last.Filename != "~/src/clack/internal/ui/messages/list.go" {
		t.Errorf("crashing frame's file %q", last.Filename)
	}
	if first.Function != "(*App).Run" || first.Lineno != 700 {
		t.Errorf("creator frame %+v", first)
	}
	if g := fr[1]; g.Module != "gioui.org/app" || g.Function != "(*Window).run" || g.InApp || g.Lineno != 120 {
		t.Errorf("Gio's frame %+v", g)
	}
	crash, _ := ev.Contexts["crash"].(map[string]any)
	if exit, _ := crash["exit"].(string); crash["goroutines"] != "2" || !strings.Contains(exit, "ended without saying why") {
		t.Errorf("crash context %v", crash)
	}

	// what's sent: nothing of the message, the token, the home or the id
	fake := sentrytest.Start(t)
	c, err := New(fake.DSN(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Send(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	env := fake.Wait(t, 1, 5*time.Second)[0]
	if len(env.Items) != 1 || env.Items[0].Type() != "event" {
		t.Fatalf("items %v", env.Items)
	}
	raw := string(env.Raw)
	for _, bad := range []string{"launch moved", "friday", "keep it quiet", "xoxc-111", "kayla", "C0123ABCD45", "0xc000123400"} {
		if strings.Contains(raw, bad) {
			t.Errorf("the crash report carries %q", bad)
		}
	}
	var got map[string]any
	if err := json.Unmarshal(env.Items[0].Payload, &got); err != nil {
		t.Fatal(err)
	}
	if got["level"] != "fatal" || got["platform"] != "go" {
		t.Errorf("sent %v", got)
	}
}

func TestCrashEventFatalError(t *testing.T) {
	ev := CrashEvent(Crash{Output: "fatal error: concurrent map writes\n\ngoroutine 23 [running]:\ninternal/runtime/maps.fatal({0x1, 0x2})\n\t/usr/local/go/src/runtime/panic.go:1058 +0x18\nmain.main.func1()\n\t/tmp/x.go:9 +0x2b\n"})
	x := ev.Exception.Values[0]
	if x.Type != "fatal error" || x.Value != "concurrent map writes" {
		t.Errorf("exception %q %q", x.Type, x.Value)
	}
	if x.Stacktrace == nil {
		t.Fatal("no stack")
	}
	if fr := x.Stacktrace.Frames; len(fr) != 2 || fr[1].Module != "internal/runtime/maps" || fr[1].Function != "fatal" || fr[0].Module != "main" || !fr[0].InApp {
		t.Errorf("frames %+v", fr)
	}
}

// A signal: the [signal …] line goes in the context.
func TestCrashEventSignal(t *testing.T) {
	ev := CrashEvent(Crash{Output: "panic: runtime error: invalid memory address or nil pointer dereference\n[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x47c8b2]\n\ngoroutine 1 [running]:\nmain.main()\n\t/tmp/x.go:5 +0x12\n"})
	if x := ev.Exception.Values[0]; x.Value != "runtime error: invalid memory address or nil pointer dereference" {
		t.Errorf("value %q", x.Value)
	}
	crash, _ := ev.Contexts["crash"].(map[string]any)
	if s, _ := crash["signal"].(string); !strings.HasPrefix(s, "[signal SIGSEGV") {
		t.Errorf("signal %q", s)
	}
}

// Windows: a native crash only Windows recorded (a fail-fast before any
// of Clack's code could write) is a native exception with no stack.
func TestCrashEventNative(t *testing.T) {
	c := Crash{Native: "applog: Windows recorded the previous run's end (pid 99, 2026-11-24T10:05:00-06:00): exception 0xc0000374 (STATUS_HEAP_CORRUPTION) in ntdll.dll at offset 0xfc21b (Clack.exe; Application Error, event 1000)"}
	if c.Empty() {
		t.Fatal("a native record is something to send")
	}
	x := CrashEvent(c).Exception.Values[0]
	if x.Type != "native exception" || !strings.HasPrefix(x.Value, "exception 0xc0000374 (STATUS_HEAP_CORRUPTION) in ntdll.dll") || x.Stacktrace != nil {
		t.Errorf("exception %q %q %v", x.Type, x.Value, x.Stacktrace)
	}
	// the native capture's own record (crash_windows.go), in crash.log
	x = CrashEvent(Crash{Output: "2026/11/24 10:05:00.123 native exception 0xc0000005 (STATUS_ACCESS_VIOLATION, reading 0x0) at 0x7ffd1234 = ntdll.dll+0x1234 on thread 7; seen by the vectored handler\n"}).Exception.Values[0]
	if x.Type != "native exception" || !strings.HasPrefix(x.Value, "exception 0xc0000005 (STATUS_ACCESS_VIOLATION") {
		t.Errorf("native capture: %q %q", x.Type, x.Value)
	}
	if !(Crash{Exit: "applog: previous run ended without saying why"}).Empty() {
		t.Error("a run that ended without a crash record is something to send")
	}
}
