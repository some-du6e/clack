package ui

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// A runtime the UI detached (sign-out, an account switch: its plugins
// stop on a goroutine of their own) can't put its patches back into
// slackc: a plugin it starts after the detach leaves every component, the
// next session's message menus included, on the native path.
func TestDetachedRuntimeLeavesPatchesAlone(t *testing.T) {
	e := newSVIn(t, "")
	e.installCode("PassSlick", svPassPlugin)
	e.until(5*time.Second, "MAMenu patched", func() bool { return slackc.Patched("MAMenu") })
	b := e.a.data
	s, tt := b.slick, b.taut
	b.detachTaut()
	t.Cleanup(func() { closeTaut(s, tt) })
	if slackc.Patched("MAMenu") {
		t.Fatal("MAMenu still patched after the runtime was detached")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := s.SetEnabled(ctx, "PassSlick", false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnabled(ctx, "PassSlick", true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		e.frame()
		if slackc.Patched("MAMenu") {
			t.Fatal("the detached runtime's patch reached slackc")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
