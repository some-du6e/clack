package ui

import (
	"context"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/ui/inputidle"
)

// The viewer's activity, told to the realtime socket as Slack's client
// and desktop app tell it (core/tickle.go; docs/NOTIFY-RULES.md §10):
//
//   - the window gaining the focus (handleWindowFocusEvent: setFocused),
//     a press of the mouse or of a key in any of Clack's windows
//     (handleWindowMouseDownEvent, handleWindowKeyDownEvent: Gio's router
//     tells of each, patch 33's press hook), and
//   - the desktop app's poll (systemIdleStatusEpic): at once and every
//     10 s, while the system had input in the last 20 s
//     (getSystemIdleTime() <= 20) and the screen isn't locked
//     (systemLockedStatus), maybeTickleMS with reason "polling".
//
// core's Tickle throttles them all to one tickle per 19 s.

// presenceConfig is the desktop poll's: systemIdleStatusEpic's interval
// and ACTIVE bound, the system's idle time (powerMonitor.getSystemIdleTime)
// and the screen's lock. Tests swap it.
type presenceConfig struct {
	every  time.Duration
	active int
	idle   func() int
	locked func() bool
}

var presenceCfg atomic.Pointer[presenceConfig]

func init() {
	presenceCfg.Store(&presenceConfig{every: 10 * time.Second, active: systemIdleSeconds, idle: presenceIdleSeconds, locked: desktop.ScreenLocked})
}

// presenceIdleSeconds is getSystemIdleTime: the system's, on Windows and
// macOS; on Linux, where Clack reads none (Electron reads X11's
// screensaver), how long Clack's own windows have had no input.
func presenceIdleSeconds() int {
	if runtime.GOOS == "linux" {
		return int(inputidle.Since() / time.Second)
	}
	return desktop.SystemIdleTime()
}

// presencePoll is systemIdleStatusEpic and tickleMessageServerEpic for c,
// until ctx ends.
func presencePoll(ctx context.Context, c *core.App) {
	cfg := presenceCfg.Load() // as the session starts
	tick := func() {
		if cfg.idle() <= cfg.active && !cfg.locked() {
			c.Tickle("polling")
		}
	}
	tick()
	t := time.NewTicker(cfg.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}

// presencePressed is the press hook: a mouse press or a key press in any
// window (any goroutine: it reads the session's core through tickler).
func (a *App) presencePressed(mouse bool) {
	reason := "keydown"
	if mouse {
		reason = "mousedown"
	}
	a.tickle(reason)
}

// tickle is maybeTickleMS for the session's core, if there is one. Any
// goroutine.
func (a *App) tickle(reason string) {
	if c := a.tickler.Load(); c != nil {
		c.Tickle(reason)
	}
}
