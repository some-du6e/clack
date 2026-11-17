package ui

import "gioui.org/gpu/headless"

// The plugin perf passes' GPU counts (perf_plugins_test.go's
// perfGPUStats): Gio's frame stats, Clack patch 36. The base tree the
// perf workflow runs perf_plugins_test.go on has no such file.
func init() {
	perfGPUStats = func(w *headless.Window) perfGPUFrame {
		s := w.ClackFrameStats()
		return perfGPUFrame{Draws: int64(s.Draws), APICalls: s.APICalls, Skipped: s.Skipped, Crossings: s.Crossings, CoverPx: s.CoverPx, BlitPx: s.BlitPx, PathsBuilt: int64(s.PathsBuilt)}
	}
}
