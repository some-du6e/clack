// SPDX-License-Identifier: Unlicense OR MIT

//go:build darwin && !ios

package app

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/app/cocoatest"
)

// Clack patch (CLACK_PATCHES.md §32) on a real macOS desktop
// (CLACK_MACOS_WINDOW_TESTS, ci's macos job). Karim's freezes after his
// Mac woke: the AppKit main thread waited in -[CAMetalLayer nextDrawable]
// (allowsNextDrawableTimeout NO) for seconds, and minutes while the
// screens slept, because a window drew whenever the app asked, occluded
// or not, screens awake or not.

// logCapture collects what the standard logger prints while a test runs
// (Gio's §32 lines; the test's output still gets them).
type logCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *logCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	os.Stderr.Write(p)
	return c.buf.Write(p)
}

func (c *logCapture) count(s string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Count(c.buf.String(), s)
}

func captureLog(t *testing.T) *logCapture {
	c := new(logCapture)
	log.SetOutput(c)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return c
}

// waitLogged waits for the n-th line with s.
func (c *logCapture) waitLogged(t *testing.T, s string, n int) {
	t.Helper()
	macWaitUntil(t, fmt.Sprintf("log line %d with %q", n, s), func() bool { return c.count(s) >= n })
}

// settle waits until the window has drawn nothing for quiet, within
// limit; false when frames kept coming.
func (h *macTestWindow) settle(quiet, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	last, at := h.frames.Load(), time.Now()
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		if f := h.frames.Load(); f != last {
			last, at = f, time.Now()
		} else if time.Since(at) >= quiet {
			return true
		}
	}
	return false
}

// pausedFrames: the frames the window draws in 1.5 s while the app
// invalidates it every 150 ms, and whether the app still got its
// wake-ups.
func (h *macTestWindow) pausedFrames() (frames int64, woken bool) {
	f0, w0 := h.frames.Load(), h.wakeups.Load()
	for range 10 {
		h.w.Invalidate()
		time.Sleep(150 * time.Millisecond)
	}
	return h.frames.Load() - f0, h.wakeups.Load() > w0
}

// waitFrames waits for n more frames than from and reports how long they
// took.
func (h *macTestWindow) waitFrames(t *testing.T, what string, from, n int64) time.Duration {
	t.Helper()
	start := time.Now()
	macWaitUntil(t, what, func() bool { return h.frames.Load() >= from+n })
	return time.Since(start)
}

// An occluded window draws no frames, an animating one included (the
// display link stops), while the app still gets its wake-ups; it draws
// again as soon as it's visible, without being asked, as Chromium's
// occluded WebContents is shown again the moment its window is.
func TestOccludedWindowPausesMac(t *testing.T) {
	if !macWindowTestsEnabled() {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	h := newMacTestWindowWith(t, func() bool { return false })
	defer h.destroy(t)
	defer cocoatest.OnMain(cocoatest.Uncover)
	h.makeKey(t)
	h.animate.Store(true)
	h.w.Invalidate()
	h.waitFrames(t, "an animating window's frames", h.frames.Load(), 20)

	cover := func(what string) {
		t.Helper()
		cocoatest.OnMain(cocoatest.Cover)
		start := time.Now()
		macWaitUntil(t, what+": AppKit reporting the window occluded", func() bool { return !h.state().OcclusionVisible })
		t.Logf("%s: occluded after %v", what, time.Since(start).Round(time.Millisecond))
	}
	uncover := func(what string) time.Time {
		t.Helper()
		cocoatest.OnMain(cocoatest.Uncover)
		macWaitUntil(t, what+": AppKit reporting the window visible", func() bool { return h.state().OcclusionVisible })
		return time.Now()
	}

	cover("animating")
	if !h.settle(300*time.Millisecond, 3*time.Second) {
		t.Error("occluded and animating: the window kept drawing for 3 s")
	}
	if n, woken := h.pausedFrames(); n != 0 || !woken {
		t.Errorf("occluded and animating: %d frames in 1.5 s with Invalidate every 150 ms (want none); app woken %v (want true)", n, woken)
	}
	f0 := h.frames.Load()
	at := uncover("animating")
	d := h.waitFrames(t, "an animating window's frames once it's visible", f0, 5)
	t.Logf("animating: 5 frames %v after AppKit reported the window visible (%v after the cover went)", d, time.Since(at)-d)

	// not animating: an Invalidate while occluded draws nothing, and the
	// window draws once it's visible again, without another Invalidate
	h.animate.Store(false)
	if !h.settle(500*time.Millisecond, 5*time.Second) {
		t.Fatal("the window kept drawing after its animation stopped")
	}
	cover("idle")
	h.settle(300*time.Millisecond, 3*time.Second)
	f0 = h.frames.Load()
	h.w.Invalidate()
	time.Sleep(time.Second)
	if n := h.frames.Load() - f0; n != 0 {
		t.Errorf("occluded: %d frames after an Invalidate, want none until it's visible", n)
	}
	uncover("idle")
	d = h.waitFrames(t, "a frame once the window is visible again", f0, 1)
	t.Logf("idle: a frame %v after AppKit reported the window visible", d)
	if d > 2*time.Second {
		t.Errorf("the window drew %v after it was visible again, want it at once", d)
	}
}

// pingMain is how long the main thread took to run an empty block from
// the main queue, up to limit (ok false: it didn't within limit).
func pingMain(limit time.Duration) (time.Duration, bool) {
	start := time.Now()
	done := make(chan struct{})
	go cocoatest.OnMain(func() { close(done) })
	select {
	case <-done:
		return time.Since(start), true
	case <-time.After(limit):
		return limit, false
	}
}

// With the compositor keeping every drawable of the window's CAMetalLayer
// (what a sleeping display does; a waking one hands them back late), a
// frame waits at most the layer's second for one, is skipped and tried
// again, the main thread free in between; the window isn't destroyed; it
// presents again once drawables come back. Each run of skipped frames gets
// a line in the log, and so does its end. cocoatest.HoldDrawables has
// nextDrawable answer as CAMetalLayer documents for a pool with none free
// (a test can't make the compositor keep them on a desktop that shows
// them); the layer's timeout setting is the real one.
func TestNoDrawableSkipsFrameMac(t *testing.T) {
	if !macWindowTestsEnabled() {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	logs := captureLog(t)
	h := newMacTestWindowWith(t, func() bool { return false })
	defer h.destroy(t)
	var timeout, metal bool
	cocoatest.OnMain(func() { timeout, metal = cocoatest.DrawableTimeout(h.view) })
	t.Logf("the layer's allowsNextDrawableTimeout %v", timeout)
	if !metal {
		t.Fatal("the window's layer isn't a CAMetalLayer")
	}
	if !timeout {
		t.Error("allowsNextDrawableTimeout is NO: nextDrawable waits on the main thread for as long as the compositor keeps the drawables")
	}
	cocoatest.OnMain(cocoatest.HoldDrawables)
	// from this goroutine: without the timeout the main thread waits in
	// nextDrawable until the drawables come back
	released := false
	release := func() {
		if !released {
			released = true
			cocoatest.ReleaseDrawables()
		}
	}
	defer release()

	s0, p0, f0 := metalSkipped.Load(), metalPresented.Load(), h.frames.Load()
	h.w.Invalidate()
	var worst time.Duration
	for end := time.Now().Add(4 * time.Second); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
		d, ok := pingMain(2500 * time.Millisecond)
		if !ok {
			t.Errorf("with no drawable to be had, the main thread ran nothing for over %v", d)
			break
		}
		worst = max(worst, d)
	}
	t.Logf("the main thread's longest wait for a block: %v", worst.Round(time.Millisecond))
	skipped, frames, presented := metalSkipped.Load()-s0, h.frames.Load()-f0, metalPresented.Load()-p0
	t.Logf("in 4 s with no drawables: %d frames, %d skipped, %d presented; nextDrawable called %d times", frames, skipped, presented, cocoatest.HeldCalls())
	if skipped < 2 {
		t.Errorf("%d frames skipped for want of a drawable in 4 s, want at least 2 (one try a second)", skipped)
	}
	if frames < 2 {
		t.Errorf("%d frames in 4 s: the frame without a drawable wasn't tried again", frames)
	}
	if presented != 0 {
		t.Errorf("%d frames presented with every drawable held", presented)
	}
	select {
	case <-h.done:
		t.Fatal("the window was destroyed: a missing drawable failed its GPU context")
	default:
	}

	release()
	d := time.Now()
	macWaitUntil(t, "a frame presented once the drawables came back", func() bool { return metalPresented.Load() > p0 })
	t.Logf("presented %v after the drawables came back", time.Since(d).Round(time.Millisecond))
	logs.waitLogged(t, "gio: no Metal drawable", 1)
	logs.waitLogged(t, "gio: Metal drawables again", 1)
	if c := logs.count("gio: no Metal drawable"); c != 1 {
		t.Errorf("%d lines for one run of skipped frames, want 1", c)
	}
}

// The screens' sleep (NSWorkspaceScreensDidSleepNotification) stops every
// window's frames, an animating one's included, until they wake
// (ScreensDidWake), as Chromium's WindowOcclusionCheckerMac counts every
// window occluded while the displays sleep; the computer's own wake
// doesn't end it (the screens may still be off), input does (the screens
// are on, whatever the notifications said). Each gets one line in the log,
// and so do the computer's sleep and wake.
func TestScreensSleepWakeMac(t *testing.T) {
	if !macWindowTestsEnabled() {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	logs := captureLog(t)
	h := newMacTestWindowWith(t, func() bool { return false })
	defer h.destroy(t)
	post := func(n cocoatest.WorkspaceNote) { cocoatest.OnMain(func() { cocoatest.PostWorkspace(n) }) }
	defer post(cocoatest.ScreensDidWake) // never leave the screens asleep for the next test
	h.makeKey(t)
	h.animate.Store(true)
	h.w.Invalidate()
	h.waitFrames(t, "an animating window's frames", h.frames.Load(), 20)

	post(cocoatest.WillSleep)
	logs.waitLogged(t, "gio: macOS is going to sleep", 1)
	post(cocoatest.ScreensDidSleep)
	logs.waitLogged(t, "gio: the screens went to sleep", 1)
	if !h.settle(300*time.Millisecond, 3*time.Second) {
		t.Error("screens asleep: the animating window kept drawing for 3 s")
	}
	if n, woken := h.pausedFrames(); n != 0 || !woken {
		t.Errorf("screens asleep: %d frames in 1.5 s with Invalidate every 150 ms (want none); app woken %v (want true)", n, woken)
	}
	post(cocoatest.DidWake)
	logs.waitLogged(t, "gio: macOS woke from sleep", 1)
	if n, _ := h.pausedFrames(); n != 0 {
		t.Errorf("the computer awake, its screens still asleep: %d frames, want none", n)
	}
	f0 := h.frames.Load()
	post(cocoatest.ScreensDidWake)
	logs.waitLogged(t, "gio: the screens woke up", 1)
	d := h.waitFrames(t, "frames once the screens woke", f0, 5)
	t.Logf("5 frames %v after ScreensDidWake", d)

	// a ScreensDidWake that never comes: a click means the screens are on
	post(cocoatest.ScreensDidSleep)
	logs.waitLogged(t, "gio: the screens went to sleep", 2)
	if !h.settle(300*time.Millisecond, 3*time.Second) {
		t.Error("screens asleep again: the window kept drawing for 3 s")
	}
	f0 = h.frames.Load()
	h.click(500, 300, 1)
	d = h.waitFrames(t, "frames after a click while the screens were asleep", f0, 5)
	t.Logf("5 frames %v after the click", d)
	logs.waitLogged(t, "gio: input while the screens were asleep", 1)
	post(cocoatest.ScreensDidWake) // late: already awake
	time.Sleep(300 * time.Millisecond)
	if c := logs.count("gio: the screens woke up"); c != 1 {
		t.Errorf("%d lines for the screens' wake, want 1 (a late ScreensDidWake after input is no news)", c)
	}
	for _, s := range []string{"gio: macOS is going to sleep", "gio: macOS woke from sleep"} {
		if c := logs.count(s); c != 1 {
			t.Errorf("%d lines %q, want 1", c, s)
		}
	}
}
