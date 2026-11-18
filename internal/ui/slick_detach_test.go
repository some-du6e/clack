package ui

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// A runtime the UI detached (sign-out, an account switch: its plugins
// stop on a goroutine of their own) can't write the UI any more: a patch
// it registers after the detach leaves every component, the next
// session's message menus included, on the native path; the sheets it set
// went with the detach; and its plugins' stop and Slick's own close don't
// take out a sheet the next session set under the same key (its Slick
// theme), nor add one.
func TestDetachedRuntimeLeavesPatchesAlone(t *testing.T) {
	e := newSVIn(t, "")
	e.installCode("PassSlick", svPassPlugin)
	e.until(5*time.Second, "MAMenu patched", func() bool { return slackc.Patched("MAMenu") })
	b := e.a.data
	s, tt := b.slick, b.taut
	tt.UIHooks().SetStyle("plugin:detach-test", ".old-session{}")
	if !slackcHasStyle("plugin:detach-test") {
		t.Fatal("the attached runtime's sheet isn't installed")
	}
	b.detachTaut()
	sClosed := false
	t.Cleanup(func() {
		if !sClosed {
			_ = s.Close()
		}
		_ = tt.Close()
	})
	if slackc.Patched("MAMenu") {
		t.Fatal("MAMenu still patched after the runtime was detached")
	}
	if slackcHasStyle("plugin:detach-test") {
		t.Error("the detached runtime's sheet stayed")
	}

	// the next session's Slick theme, under the key the old Slick's close
	// removes; the old runtime's style writes from here on, as styleTap
	// sees what the hooks hand slackc
	slackc.SetStyle("slick:theme", ".next-session{}")
	t.Cleanup(func() { slackc.RemoveStyle("slick:theme") })
	var mu sync.Mutex
	var late []string
	styleTap = func(key, css string) { mu.Lock(); late = append(late, key); mu.Unlock() }
	t.Cleanup(func() { styleTap = nil })

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
	tt.UIHooks().SetStyle("plugin:late", ".late{}")
	tt.UIHooks().RemoveStyle("slick:theme")
	_ = s.Close() // Slick's close removes slick:theme and slick:user
	sClosed = true
	e.frame()
	if !slackcHasStyle("slick:theme") {
		t.Error("the detached runtime took out the next session's Slick theme")
	}
	if slackcHasStyle("plugin:late") {
		t.Error("the detached runtime installed a sheet")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(late) > 0 {
		t.Errorf("the detached runtime wrote sheets %q", late)
	}
}

// An Attached that runs after its backend stopped (the runtime came up as
// the UI detached it) is ignored: the stopped runtime doesn't become
// slackc's again, and what it publishes stays out.
func TestAttachAfterDetachIgnored(t *testing.T) {
	e := newSVIn(t, "")
	b := e.a.data
	s, tt := b.slick, b.taut
	b.detachTaut() // no plugin on: its runtime never started
	t.Cleanup(func() { closeTaut(s, tt) })
	tt.UIHooks().Attached(tt.Bridge()) // it comes up now
	for i := 0; i < 3; i++ {
		e.frame()
	}
	tt.UIHooks().SetPatched([]string{"DetachTestComponent"})
	for i := 0; i < 3; i++ {
		e.frame()
	}
	if slackc.Patched("DetachTestComponent") {
		t.Error("a runtime attached after its detach published its patches")
	}
}

// An old runtime writing after a newer one attached does nothing: the
// newer session owns slackc's patched set and the UI's sheets.
func TestOldRuntimeAfterNewerAttached(t *testing.T) {
	e := newSVIn(t, "")
	ttA := e.a.data.taut
	ttA.UIHooks().Attached(ttA.Bridge())
	for i := 0; i < 3; i++ {
		e.frame()
	}
	ttB, done := svSecondTaut(t, e)
	defer done()
	ttB.UIHooks().Attached(ttB.Bridge())
	for i := 0; i < 3; i++ {
		e.frame()
	}
	var mu sync.Mutex
	var styles []string
	styleTap = func(key, css string) { mu.Lock(); styles = append(styles, key); mu.Unlock() }
	t.Cleanup(func() { styleTap = nil })
	ttA.UIHooks().SetPatched([]string{"OldOwnerComponent"})
	ttA.UIHooks().SetStyle("plugin:old-owner", ".a{}")
	ttB.UIHooks().SetPatched([]string{"NewOwnerComponent"})
	for i := 0; i < 3; i++ {
		e.frame()
	}
	if slackc.Patched("OldOwnerComponent") {
		t.Error("the old runtime's patches reached slackc after a newer one attached")
	}
	if !slackc.Patched("NewOwnerComponent") {
		t.Error("the newer runtime's patches didn't reach slackc")
	}
	mu.Lock()
	defer mu.Unlock()
	if slices.Contains(styles, "plugin:old-owner") {
		t.Error("the old runtime installed a sheet after a newer one attached")
	}
}
