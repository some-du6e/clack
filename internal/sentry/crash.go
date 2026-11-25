package sentry

import (
	"bufio"
	"strconv"
	"strings"
	"time"
)

// Crash is what Clack recorded of a run that crashed, for its crash
// report at the next start: the runtime's crash output from crash.log
// (applog.Logger.PreviousCrash: a Go panic or fatal error, or on Windows
// the native capture's record), the run it names, and how that run ended
// (its run record's line, applog.Logger.StartRun) and what Windows
// recorded of it (applog.CrashRecordFor).
type Crash struct {
	Output string    // crash.log after its header, raw
	Header string    // the crashed run's "clack v0.1.0 windows/amd64 pid 123, started …"
	When   time.Time // when crash.log was last written (zero: unknown)
	Exit   string    // the previous run's exit line ("" none)
	Native string    // Windows' Application Error record ("" none)
}

// Empty reports nothing to send: no crash output and no native record (a
// clean end, or one that ended without a crash: a kill, power lost).
func (c Crash) Empty() bool {
	return strings.TrimSpace(c.Output) == "" && strings.TrimSpace(c.Native) == ""
}

// inAppPrefix is Clack's own module: its frames are the app's.
const inAppPrefix = "github.com/some-du6e/clack/"

// maxFrames caps a stack (Sentry's own limit is about 250).
const maxFrames = 120

// CrashEvent is c as a Sentry event: a fatal exception whose type is
// what crashed ("panic", "fatal error", "native exception"), whose value
// is the crash's message with what it quotes left out (scrubCrashText),
// and whose stack is the crashing goroutine's frames (function, file,
// line; no arguments, no addresses), with the run, its exit line and
// Windows' record as the "crash" context. Every string is scrubbed again
// when it is sent (Client.Send).
func CrashEvent(c Crash) *Event {
	p := parseCrash(c.Output)
	if p.typ == "" && c.Native != "" {
		p.typ, p.value = "native exception", nativeValue(c.Native)
	}
	if p.typ == "" {
		p.typ = "crash"
	}
	x := Exception{Type: p.typ, Value: scrubCrashText(p.value), Mechanism: &Mechanism{Type: "crash", Handled: false}}
	if len(p.frames) > 0 {
		// Sentry lists the oldest call first, Go the innermost
		fr := make([]Frame, len(p.frames))
		for i, f := range p.frames {
			f.Filename = Scrub(f.Filename) // a dev build's paths are absolute
			fr[len(p.frames)-1-i] = f
		}
		x.Stacktrace = &Stacktrace{Frames: fr}
	}
	ev := &Event{Level: "fatal", Logger: "clack.crash", Exception: &Exceptions{Values: []Exception{x}}}
	if !c.When.IsZero() {
		ev.Timestamp = c.When.UTC().Format(time.RFC3339Nano)
	}
	crash := map[string]any{"type": "crash"}
	if c.Header != "" {
		crash["run"] = scrubCrashText(c.Header)
	}
	if c.Exit != "" {
		crash["exit"] = scrubCrashText(c.Exit)
	}
	if c.Native != "" {
		crash["windows"] = scrubCrashText(c.Native)
	}
	if p.signal != "" {
		crash["signal"] = scrubCrashText(p.signal)
	}
	if p.goroutines > 0 {
		crash["goroutines"] = strconv.Itoa(p.goroutines)
	}
	ev.Contexts = map[string]any{"crash": crash}
	ev.Tags = map[string]string{"kind": "crash", "crash": p.typ}
	return ev
}

// nativeValue is the part of Windows' record that says what happened:
// from "exception 0x…" on ("applog: Windows recorded the previous run's
// end (pid …): exception 0xc0000374 (…) in ntdll.dll at offset 0x…").
func nativeValue(s string) string {
	if i := strings.Index(s, "exception 0x"); i >= 0 {
		return s[i:]
	}
	return s
}

// parsed is what parseCrash read of a crash's output.
type parsed struct {
	typ, value string
	signal     string  // the runtime's [signal …] line
	frames     []Frame // the crashing goroutine's, innermost first
	goroutines int     // goroutine blocks in the output
}

// parseCrash reads Go's crash output: its first "panic: " or "fatal
// error: " line (or the native capture's "native exception 0x…", or Go's
// "Exception 0x…" for a Windows exception it caught), then the first
// goroutine's frames, each a function line and a "\tfile:line +0x…"
// line, "created by …" included.
func parseCrash(out string) parsed {
	var p parsed
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	var lines []string
	for sc.Scan() {
		lines = append(lines, strings.TrimRight(sc.Text(), "\r"))
	}
	first := -1 // the first goroutine block's header line
	for i, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case p.typ == "" && strings.HasPrefix(t, "panic: "):
			p.typ, p.value = "panic", strings.TrimSuffix(strings.TrimPrefix(t, "panic: "), " [recovered]")
		case p.typ == "" && strings.HasPrefix(t, "fatal error: "):
			p.typ, p.value = "fatal error", strings.TrimPrefix(t, "fatal error: ")
		case p.typ == "" && strings.Contains(t, "native exception 0x"):
			p.typ, p.value = "native exception", t[strings.Index(t, "native exception 0x")+len("native "):]
		case p.typ == "" && strings.HasPrefix(t, "Exception 0x"):
			p.typ, p.value = "exception", t
		case p.signal == "" && strings.HasPrefix(t, "[signal "):
			p.signal = t
		case strings.HasPrefix(l, "goroutine ") && strings.HasSuffix(t, ":"):
			p.goroutines++
			if first < 0 {
				first = i
			}
		}
	}
	start := first + 1
	if first < 0 {
		// a Windows exception Go caught prints its frames with no
		// goroutine header: from the function line over the first file
		// line
		start = -1
		for i := 1; i < len(lines); i++ {
			if strings.HasPrefix(lines[i], "\t") {
				start = i - 1
				break
			}
		}
	}
	if start >= 0 {
		p.frames = framesFrom(lines[start:])
	}
	return p
}

// framesFrom reads frames until the block ends (an empty line or the
// next goroutine).
func framesFrom(lines []string) []Frame {
	var out []Frame
	for i := 0; i < len(lines) && len(out) < maxFrames; i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "goroutine ") {
			break
		}
		if strings.HasPrefix(l, "\t") || strings.HasPrefix(l, "...") {
			continue // a file line without its function, "...additional frames elided..."
		}
		fn := funcName(l)
		if fn == "" {
			continue
		}
		f := splitFunc(fn)
		if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "\t") {
			f.Filename, f.Lineno = fileLine(strings.TrimSpace(lines[i+1]))
			i++
		}
		out = append(out, f)
	}
	return out
}

// funcName is a frame's function line without its arguments: "main.(*T).run(0xc000010000, 0x5)"
// is "main.(*T).run", "created by main.main in goroutine 1" is "main.main".
func funcName(l string) string {
	l = strings.TrimSpace(l)
	if s, ok := strings.CutPrefix(l, "created by "); ok {
		if i := strings.Index(s, " in goroutine "); i >= 0 {
			s = s[:i]
		}
		return s
	}
	if strings.HasSuffix(l, ")") {
		// the arguments are the last parenthesized group (a method's
		// receiver, (*T), comes before it)
		depth := 0
		for i := len(l) - 1; i >= 0; i-- {
			switch l[i] {
			case ')':
				depth++
			case '(':
				depth--
				if depth == 0 {
					return l[:i]
				}
			}
		}
	}
	if strings.ContainsAny(l, " :=") {
		return "" // not a frame: a runtime line ("PC=0x…", "rax 0x…")
	}
	return l
}

// splitFunc is a frame for fn: its package path as the module, the rest
// as the function ("github.com/x/y/z.(*T).M" is module
// "github.com/x/y/z", function "(*T).M").
func splitFunc(fn string) Frame {
	f := Frame{Function: fn}
	slash := strings.LastIndexByte(fn, '/')
	if dot := strings.IndexByte(fn[slash+1:], '.'); dot >= 0 {
		f.Module, f.Function = fn[:slash+1+dot], fn[slash+1+dot+1:]
	}
	f.InApp = strings.HasPrefix(f.Module, inAppPrefix) || f.Module == "main"
	return f
}

// fileLine reads "file.go:123 +0x45" (or "file.go:123").
func fileLine(s string) (string, int) {
	if i := strings.LastIndex(s, " +0x"); i >= 0 {
		s = s[:i]
	}
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return s, 0
	}
	n, err := strconv.Atoi(s[i+1:])
	if err != nil {
		return s, 0
	}
	return s[:i], n
}
