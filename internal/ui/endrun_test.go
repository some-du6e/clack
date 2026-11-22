package ui

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unsafe"
	"weak"
)

// testApps are the Apps startEnv made, weakly, each with its test's name:
// the end-of-run report counts those still live and asks a heap dump
// what holds the oldest.
var testApps struct {
	sync.Mutex
	l []testApp
}

type testApp struct {
	test string
	p    weak.Pointer[App]
}

func init() {
	startEnvHook = func(tb testing.TB, a *App) {
		testApps.Lock()
		testApps.l = append(testApps.l, testApp{tb.Name(), weak.Make(a)})
		testApps.Unlock()
	}
}

// liveTestApps is the tracked Apps still live, oldest first, as addresses
// (no pointer to them stays on the caller's stack), and how many were
// made.
func liveTestApps() (live []retainTarget, made int) {
	testApps.Lock()
	defer testApps.Unlock()
	for _, ta := range testApps.l {
		if p := unsafe.Pointer(ta.p.Value()); p != nil {
			live = append(live, retainTarget{"the App of " + ta.test, uintptr(p)})
		}
	}
	return live, len(testApps.l)
}

// endOfRunReport writes, after all of the package's tests, what the test
// binary still holds of them: its live heap once collected and the
// goroutines still running, by stack (CLACK_TEST_HEAP_REPORT: a file;
// CLACK_TEST_HEAP_PROFILE: a heap profile too). Unset, it does nothing.
// ci-speed-3 measured finished tests' sessions being let go with it.
func endOfRunReport() {
	path := os.Getenv("CLACK_TEST_HEAP_REPORT")
	if path == "" {
		return
	}
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	var b bytes.Buffer
	fmt.Fprintf(&b, "end of the tests: live heap %d MiB (heap in use %d MiB, from the OS %d MiB), %d goroutines\n",
		ms.HeapAlloc>>20, ms.HeapInuse>>20, ms.Sys>>20, runtime.NumGoroutine())
	live, made := liveTestApps()
	fmt.Fprintf(&b, "startEnv's Apps: %d made, %d still live (the newest is the process's: one App per process)\n", made, len(live))
	// by test (its subtests together), and the first App of each
	var order []string
	byTest := map[string][]retainTarget{}
	for _, l := range live[:max(len(live)-1, 0)] { // the newest is the process's
		top, _, _ := strings.Cut(strings.TrimPrefix(l.what, "the App of "), "/")
		if byTest[top] == nil {
			order = append(order, top)
		}
		byTest[top] = append(byTest[top], l)
	}
	for _, top := range order {
		fmt.Fprintf(&b, "  live: %s ×%d\n", top, len(byTest[top]))
	}
	// what holds the first App of the first n tests (CLACK_RETAIN_DUMP=n,
	// 3 if not a number; a dump is as big as the heap, and reading it back
	// takes about as much again)
	if v := os.Getenv("CLACK_RETAIN_DUMP"); v != "" && len(order) > 0 {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			n = 3
		}
		var targets []retainTarget
		for _, top := range order[:min(len(order), n)] {
			targets = append(targets, byTest[top][0])
		}
		if ms.HeapAlloc > 3<<30 {
			fmt.Fprintf(&b, "(no heap dump: %d MiB live)\n", ms.HeapAlloc>>20)
		} else if dir, err := os.MkdirTemp("", "retain"); err == nil {
			b.WriteString(heapRetainers(dir, targets, 3, 14))
			_ = os.RemoveAll(dir)
		}
	}
	_ = pprof.Lookup("goroutine").WriteTo(&b, 1)
	_ = os.WriteFile(path, b.Bytes(), 0o644)
	if hp := os.Getenv("CLACK_TEST_HEAP_PROFILE"); hp != "" {
		if f, err := os.Create(hp); err == nil {
			_ = pprof.WriteHeapProfile(f)
			f.Close()
		}
	}
}

// goroutinesMentioning is the goroutine profile's stacks (by count) that
// name sub, at most max lines.
func goroutinesMentioning(sub string, max int) string {
	var b bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&b, 1)
	var out []string
	for _, st := range strings.Split(b.String(), "\n\n") {
		if strings.Contains(st, sub) {
			out = append(out, strings.Split(st, "\n")...)
		}
		if len(out) >= max {
			break
		}
	}
	if len(out) > max {
		out = out[:max]
	}
	return strings.Join(out, "\n")
}

// goroutineStacks is the goroutine profile's stacks, each with how many
// goroutines are on it.
func goroutineStacks() map[string]int {
	var b bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&b, 1)
	// past the "goroutine profile: total N" line: it shares the first
	// stack's chunk, which was skipped (the biggest group: blockkit's
	// goroutines, every session's, went unlisted)
	_, prof, _ := strings.Cut(b.String(), "\n")
	out := map[string]int{}
	for _, st := range strings.Split(prof, "\n\n") {
		head, rest, ok := strings.Cut(st, "\n")
		if !ok || !strings.Contains(head, " @ ") {
			continue
		}
		var n int
		fmt.Sscanf(strings.TrimSpace(head), "%d", &n)
		// the stack's lines without the addresses, which the counts' line has
		out[rest] += n
	}
	return out
}

// goroutinesAdded is the stacks with more goroutines now than in before,
// each with how many more, at most max lines.
func goroutinesAdded(before map[string]int, max int) string {
	var out []string
	for st, n := range goroutineStacks() {
		if d := n - before[st]; d > 0 && !strings.Contains(st, "goroutineStacks") {
			out = append(out, fmt.Sprintf("%d more:", d))
			out = append(out, strings.Split(st, "\n")...)
		}
		if len(out) >= max {
			break
		}
	}
	if len(out) > max {
		out = out[:max]
	}
	if len(out) == 0 {
		return "\tnone"
	}
	return strings.Join(out, "\n")
}
