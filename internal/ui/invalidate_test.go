package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Gio's Window.Invalidate deadlocks when called on the macOS main thread (it
// holds invMu across runOnMain, which runs inline there and re-locks it). The
// UI package must reach the window only through App.Invalidate, which hands
// the call to invalidator's goroutine. This scan keeps new direct calls out.
func TestOnlyInvalidatorTouchesWindowInvalidate(t *testing.T) {
	direct := regexp.MustCompile(`\b(w|win|a\.win|window|winLive\.Load\(\))\.Invalidate\b`)
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		inInvalidator := false
		for i, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "func ") {
				inInvalidator = strings.HasPrefix(line, "func (a *App) invalidator()")
			}
			if direct.MatchString(line) && !inInvalidator {
				t.Errorf("%s:%d calls the Gio window's Invalidate directly; use a.Invalidate (deadlocks on the macOS main thread):\n\t%s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// Invalidate must never block, whatever goroutine calls it and however many
// requests pile up before the window exists.
func TestInvalidateNeverBlocks(t *testing.T) {
	a := &App{inval: make(chan struct{}, 1)}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			a.Invalidate()
		}
		(&App{}).Invalidate() // zero App (headless tests): nil channel, still returns
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Invalidate blocked")
	}
}
