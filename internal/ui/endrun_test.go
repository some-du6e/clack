package ui

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
)

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
