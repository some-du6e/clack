package ui

import (
	"os"
	"path/filepath"
	"time"
)

// TestPerfPluginScroll's recorder passes (PERF_PLUGIN_RECORDER): the
// recorder as the watchdog runs it, and its window written to a file as a
// slow frame writes it. Only in trees with flightrec.go: perf.yml runs
// HEAD's perf_plugins_test.go on the base tree too, and there this file
// isn't, so perfRecorder stays nil.
func init() {
	perfRecorder = func() (func(), func() (int, time.Duration), error) {
		dir, err := os.MkdirTemp("", "perf-flightrec")
		if err != nil {
			return nil, nil, err
		}
		rec, err := startFlightRecorder(dir, traceGap)
		if err != nil {
			_ = os.RemoveAll(dir)
			return nil, nil, err
		}
		dump := func() (int, time.Duration) {
			t0 := time.Now()
			n, err := rec.write(filepath.Join(dir, "pass.trace"))
			if err != nil {
				return 0, 0
			}
			return int(n), time.Since(t0)
		}
		stop := func() {
			rec.stop()
			_ = os.RemoveAll(dir)
		}
		return stop, dump, nil
	}
}
