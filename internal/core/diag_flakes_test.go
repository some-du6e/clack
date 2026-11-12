package core

// DIAG flakes-1111: temporary diagnostics for TestListEditRoundTrip's
// "list_cells_updated never applied" (CI 2026-11-11). Removed with the fix.

import (
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

type rawLog struct {
	mu  sync.Mutex
	evs []string
}

func diagRawTap(t *testing.T, a *App) *rawLog {
	l := &rawLog{}
	ch, stop := a.SubscribeRaw()
	t.Cleanup(stop)
	t0 := time.Now()
	go func() {
		for ev := range ch {
			l.mu.Lock()
			l.evs = append(l.evs, time.Since(t0).Round(time.Millisecond).String()+" "+ev.Type+" "+string(ev.Raw[:min(len(ev.Raw), 160)]))
			l.mu.Unlock()
		}
	}()
	return l
}

func diagListFlake(t *testing.T, a *App, fs *fakeslack.Server, l *rawLog) {
	t.Logf("DIAG ListOpen=%v connected=%v", a.ListOpen(fakeslack.ListKitchen), a.RTM.Connected())
	if r := a.CachedList(fakeslack.ListKitchen).Record("Rec0ITEM005"); r != nil {
		b, _ := json.Marshal(r.Field("Col0STATUS1"))
		t.Logf("DIAG cached status %s", b)
	} else {
		t.Logf("DIAG cached record missing")
	}
	fl, _ := fs.List(fakeslack.ListKitchen)
	for i := range fl.Records {
		if fl.Records[i].ID == "Rec0ITEM005" {
			b, _ := json.Marshal(fl.Records[i].Field("Col0STATUS1"))
			t.Logf("DIAG fake status %s", b)
		}
	}
	l.mu.Lock()
	for _, e := range l.evs {
		t.Logf("DIAG raw %s", e)
	}
	l.mu.Unlock()
	buf := make([]byte, 4<<20)
	buf = buf[:runtime.Stack(buf, true)]
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, "internal/core.") || strings.Contains(g, "internal/slack.") || strings.Contains(g, "internal/fakeslack.") {
			t.Logf("DIAG goroutine:\n%s", g)
		}
	}
}
