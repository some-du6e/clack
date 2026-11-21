package ui

import (
	"runtime"
	"strings"
	"testing"
	"time"
	"weak"

	"gioui.org/text"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

// A test's session is let go once the test is over: its App, its core App,
// the fake server it talked to and its shaper are collected. perfEnv's
// shutdown ends the session as a sign-out does and drops what the
// process-wide registries keep of it. Before ci-speed-3 every one of them
// stayed reachable (core's maps keyed by *App, fakeslack's by *Server, the
// message hubs, the caches keyed by shaper...), and ui.test held 9 GB live
// by its end.
func TestFinishedSessionIsCollected(t *testing.T) {
	var (
		app weak.Pointer[App]
		ca  weak.Pointer[core.App]
		srv weak.Pointer[fakeslack.Server]
		sh  weak.Pointer[text.Shaper]
	)
	t.Run("session", func(t *testing.T) {
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
		app, ca, srv, sh = weak.Make(e.a), weak.Make(e.a.data.core), weak.Make(fs), weak.Make(e.a.th.Shaper)
	})
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
		t.Errorf("still reachable 10 s after their test: %s; goroutines naming clack's packages:\n%s",
			strings.Join(h, ", "), goroutinesMentioning("some-du6e/clack/internal", 80))
	}
}
