package ui

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// The presence tickles' test seams (presence_activity.go, core/tickle.go).
// The red commit replaces this file with stubs: the test then runs on the
// code before them. Set them before the session starts: its poll takes
// its settings then.

// presenceSeam swaps the desktop poll's settings for the test's.
func presenceSeam(t *testing.T, change func(*presenceConfig)) {
	old := presenceCfg.Load()
	cfg := *old
	change(&cfg)
	presenceCfg.Store(&cfg)
	t.Cleanup(func() { presenceCfg.Store(old) })
}

// presenceTimers shortens core's 19 s throttle and the poll's 10 s.
func presenceTimers(t *testing.T, throttle, poll time.Duration) {
	t.Cleanup(core.SetTickleEvery(throttle))
	presenceSeam(t, func(c *presenceConfig) { c.every = poll })
}

// systemIdle makes the system's idle time secs, and returns its setter.
func systemIdle(t *testing.T, secs int) func(int) {
	v := new(atomic.Int64)
	v.Store(int64(secs))
	presenceSeam(t, func(c *presenceConfig) { c.idle = func() int { return int(v.Load()) } })
	return func(s int) { v.Store(int64(s)) }
}

// screenLocked makes the screen locked or not, and returns its setter.
func screenLocked(t *testing.T, on bool) func(bool) {
	v := new(atomic.Bool)
	v.Store(on)
	presenceSeam(t, func(c *presenceConfig) { c.locked = v.Load })
	return v.Store
}
