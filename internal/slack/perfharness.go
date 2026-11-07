//go:build perfharness

package slack

// The perf workflow builds with -tags perfharness so the client accepts
// internal/fakeslack on 127.0.0.1 (see harness.go).
func init() { harnessLoopback = true }
