package ui

import (
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"
	"weak"

	"gioui.org/text"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

// A test's session is let go once the test is over: its App, its core App,
// the fake server it talked to and its shaper are collected once the next
// session has taken the process's place (ui.New points process-wide hooks
// at the newest App: one App per process). perfEnv's shutdown ends a
// session as a sign-out does and drops what the process-wide registries
// keep of it. Before ci-speed-3 nothing of a session was let go (core's
// maps keyed by *App, fakeslack's by *Server, the message hubs, the
// caches keyed by shaper, and goroutines ranging over channels nobody
// closed), and ui.test held 9 GB live by its end. A failure lists the
// goroutines the two sessions left running, and what holds the session:
// the roots a heap dump shows reaching it (retainReport).
func TestFinishedSessionIsCollected(t *testing.T) {
	before := goroutineStacks()
	var (
		app weak.Pointer[App]
		ca  weak.Pointer[core.App]
		srv weak.Pointer[fakeslack.Server]
		sh  weak.Pointer[text.Shaper]
	)
	session := func(t *testing.T, keep bool) {
		fs, err := fakeslack.Start(fakeslack.Small)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { fs.Close() })
		e := startEnv(t, fs, t.TempDir())
		e.waitReady()
		ch := fs.ChannelIDs()[1]
		e.openChannel(ch)
		e.until("history", 10*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
		e.settle(300 * time.Millisecond)
		if keep {
			app, ca, srv, sh = weak.Make(e.a), weak.Make(e.a.data.core), weak.Make(fs), weak.Make(e.a.th.Shaper)
		}
	}
	t.Run("session", func(t *testing.T) { session(t, true) })
	t.Run("the next session", func(t *testing.T) { session(t, false) })
	held := func() []string {
		var h []string
		for _, p := range []struct {
			what string
			live bool
		}{
			{"the App", app.Value() != nil},
			{"its core App", ca.Value() != nil},
			{"the fake server", srv.Value() != nil},
			{"its shaper", sh.Value() != nil},
		} {
			if p.live {
				h = append(h, p.what)
			}
		}
		return h
	}
	for dl := time.Now().Add(10 * time.Second); len(held()) > 0 && time.Now().Before(dl); {
		runtime.GC()
		time.Sleep(50 * time.Millisecond)
	}
	if h := held(); len(h) > 0 {
		t.Errorf("the first session still reachable 10 s after the next one ended: %s; goroutines the two sessions left:\n%s",
			strings.Join(h, ", "), goroutinesAdded(before, 80))
		t.Log(retainReport(t, sessionTargets(app, ca, srv, sh)))
	}
}

// sessionTargets are the addresses of a session's objects still live, for
// retainReport: no pointer to them stays on this goroutine's stack.
func sessionTargets(app weak.Pointer[App], ca weak.Pointer[core.App], srv weak.Pointer[fakeslack.Server], sh weak.Pointer[text.Shaper]) []retainTarget {
	var ts []retainTarget
	add := func(what string, p unsafe.Pointer) {
		if p != nil {
			ts = append(ts, retainTarget{what, uintptr(p)})
		}
	}
	add("the App", unsafe.Pointer(app.Value()))
	add("its core App", unsafe.Pointer(ca.Value()))
	add("the fake server", unsafe.Pointer(srv.Value()))
	add("its shaper", unsafe.Pointer(sh.Value()))
	return ts
}
