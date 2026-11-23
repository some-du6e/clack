package slack

import (
	"context"
	"sync"
	"testing"
	"time"
)

// The watchdog's test seams (rtm.go). The red commit replaces this file
// with stubs: the tests then run on the code before the watchdog.

func watchTimers(t *testing.T, frames, ticks, every time.Duration) {
	t.Cleanup(SetStallTimers(frames, ticks, every))
}

// stallPongCheck makes the pong check's goroutine stop at its next tick
// until release.
func stallPongCheck(t *testing.T) (release func()) {
	block := make(chan struct{})
	h := func(ctx context.Context) {
		select {
		case <-block:
		case <-ctx.Done():
		}
	}
	rtmPingHook.Store(&h)
	var once sync.Once
	release = func() {
		once.Do(func() {
			rtmPingHook.Store(nil)
			close(block)
		})
	}
	t.Cleanup(release)
	return release
}
