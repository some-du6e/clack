package core

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Boot benchmarks against internal/fakeslack (synthetic, Hack Club sized).
// Run by .github/workflows/perf.yml; -short uses a small workspace.

var (
	perfOnce sync.Once
	perfFake *fakeslack.Server
)

func perfServer(tb testing.TB) *fakeslack.Server {
	perfOnce.Do(func() {
		if os.Getenv("CLACK_BENCH_LOG") == "" {
			log.SetOutput(io.Discard) // keeps benchmark output parseable
		}
		sz := fakeslack.HackClub
		if testing.Short() {
			sz = fakeslack.Small
		}
		if u := os.Getenv("CLACK_FAKESLACK_URL"); u != "" {
			perfFake = fakeslack.Remote(u, sz)
			return
		}
		var err error
		if perfFake, err = fakeslack.Start(sz); err != nil {
			tb.Fatal(err)
		}
	})
	return perfFake
}

func bootOnce(tb testing.TB, fs *fakeslack.Server, cache string) *App {
	a := New(slack.New(fs.Creds()), store.New(cache))
	if err := a.Boot(context.Background()); err != nil {
		tb.Fatal(err)
	}
	return a
}

func heapMB() float64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.HeapAlloc) / (1 << 20)
}

// BenchmarkCoreBoot: Boot to ready, cold (no metadata cache) and warm (the
// cache a previous session saved). heap-MB is the live heap afterwards,
// once background loads (emoji, usergroups) have landed.
func BenchmarkCoreBoot(b *testing.B) {
	fs := perfServer(b)
	warm := b.TempDir()
	a := bootOnce(b, fs, warm)
	time.Sleep(time.Second) // background emoji/usergroups
	if err := a.Close(); err != nil {
		b.Fatal(err)
	}
	shutdown(a)
	if fi, err := os.Stat(filepath.Join(warm, "meta.json")); err == nil {
		b.Logf("meta.json: %.1f MB", float64(fi.Size())/(1<<20))
	}
	for _, mode := range []string{"cold", "warm"} {
		b.Run(mode, func(b *testing.B) {
			var heap float64
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				dir := b.TempDir()
				if mode == "warm" {
					raw, _ := os.ReadFile(filepath.Join(warm, "meta.json"))
					_ = os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0o600)
				}
				runtime.GC()
				b.StartTimer()
				a := bootOnce(b, fs, dir)
				b.StopTimer()
				time.Sleep(500 * time.Millisecond)
				heap += heapMB()
				shutdown(a)
				a = nil
				b.StartTimer()
			}
			b.ReportMetric(heap/float64(b.N), "heap-MB")
		})
	}
}

// BenchmarkLoadCache is the metadata cache read alone (65k emoji, users,
// usergroups): the first thing Boot does.
func BenchmarkLoadCache(b *testing.B) {
	fs := perfServer(b)
	dir := b.TempDir()
	a := bootOnce(b, fs, dir)
	time.Sleep(time.Second)
	_ = a.Close()
	shutdown(a)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.New(dir).LoadCache()
	}
}

// shutdown calls App.Shutdown when it exists (the perf workflow also runs
// this file against a baseline tree that predates it).
func shutdown(a *App) {
	if s, ok := any(a).(interface{ Shutdown() }); ok {
		s.Shutdown()
	}
}
