package jsrt

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"
)

// stwBound is the longest a Go stop-the-world may wait on the JS thread
// while QuickJS runs one of its native loops. The yields come every ~10k
// steps, a millisecond or so; the rest is CI noise. It's checked only with
// JSRT_STW_STRICT set (the jsrt workflow, where this package's tests run on
// their own): under `go test ./...` the other packages' tests share the
// runner and the longest wait grows with the load (13 ms alone, 26 ms in a
// ci shard), so there only the yields-off/on ratio is checked.
const stwBound = 50 * time.Millisecond

// stwMeasurable: a case whose longest stop-the-world with the yields off
// is shorter than this is too small to compare (on the runners these cases
// take 50-900 ms).
const stwMeasurable = 20 * time.Millisecond

// TestStopTheWorldBounded: Go can't stop a goroutine inside wasm code, so
// a stop-the-world (each GC cycle, runtime.ReadMemStats, runtime.Stack)
// waits until the JS thread next calls the host, and every goroutine,
// the UI's included, sits paused meanwhile. QuickJS's own C loops yield to
// the host every ~10k steps (csrc/quickjs-yield.patch). Each case runs a
// native operation over a large heap twice, with the yields off (the
// engine before the patch) and on, while other goroutines stop the world
// every 2 ms and keep a 1 ms heartbeat; with the yields on, the longest
// stop-the-world must be under a quarter of the yields-off one (and under
// stwBound with JSRT_STW_STRICT). Only an engine given by CLACK_JSRT_WASM
// (the bench job's previous engine) may lack the yields, and it only
// reports its numbers: the committed one must have them.
func TestStopTheWorldBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a few hundred MB of JS heap")
	}
	r := newTest(t, func(o *Options) {
		o.Timeout = 5 * time.Minute // these tasks are long on purpose
		o.MemoryLimit = 1536 << 20
	})
	var patched bool
	_ = r.Do(func(js *Ctx) error { patched = r.eng.fns["jr_set_yield"] != nil; return nil })
	if !patched {
		if os.Getenv("CLACK_JSRT_WASM") == "" {
			t.Fatal("the committed jsrt.wasm has no yields (jr_set_yield missing): csrc/quickjs-yield.patch isn't in it, or assets.pack is stale")
		}
		t.Logf("stw: this engine has no yields (jr_set_yield missing): numbers only")
	}
	strict := os.Getenv("JSRT_STW_STRICT") != ""
	eval := func(src string) {
		t.Helper()
		if err := r.Do(func(js *Ctx) error { _, err := js.Eval("stw.js", src); return err }); err != nil {
			t.Fatalf("%s: %v", src, err)
		}
	}
	gc := func(js *Ctx) error { r.eng.call("jr_gc"); return nil }
	evalOp := func(src string) func(js *Ctx) error {
		return func(js *Ctx) error { _, err := js.Eval("stw-op.js", src); return err }
	}
	const live = 1_000_000
	cases := []struct {
		name  string
		setup string // before each run, unmeasured
		op    func(js *Ctx) error
	}{
		{"QuickJS GC over 1M live objects in cycles",
			fmt.Sprintf(`if (!globalThis.big) { globalThis.big = []; for (let i = 0; i < %d; i++) { const o = { i }; o.me = o; big.push(o); } }`, live),
			gc},
		{"QuickJS GC freeing 1M objects in cycles",
			fmt.Sprintf(`globalThis.big = []; for (let i = 0; i < %d; i++) { const o = { i }; o.me = o; big.push(o); } globalThis.big = null;`, live),
			gc},
		{"QuickJS GC over a 1M-entry Map",
			`if (!globalThis.m) { globalThis.m = new Map(); for (let i = 0; i < 1e6; i++) m.set(i, { i }); }`,
			gc},
		{"JSON.stringify of 200k records",
			`globalThis.m = null; if (!globalThis.flat) globalThis.flat = Array.from({ length: 200000 }, (_, i) => ({ i, s: 'item ' + i, a: [i, i + 1, i + 2] }));`,
			evalOp(`globalThis.json = JSON.stringify(flat); json.length`)},
		{"JSON.parse of 200k records",
			`if (!globalThis.json) globalThis.json = JSON.stringify(flat); globalThis.back = null;`,
			evalOp(`globalThis.back = JSON.parse(json); back.length`)},
		{"Array.prototype.sort of 300k strings",
			`globalThis.back = null; globalThis.flat = null; if (!globalThis.strs) globalThis.strs = Array.from({ length: 300000 }, (_, i) => 'k' + ((i * 7919) % 300000));`,
			evalOp(`strs.slice().sort().length`)},
	}
	for _, c := range cases {
		var res [2]stwResult
		for i, on := range []bool{false, true} {
			eval(c.setup)
			every := 0 // off
			if on {
				every = 10000
			}
			if err := r.Do(func(js *Ctx) error { r.eng.setYield(every); return nil }); err != nil {
				t.Fatal(err)
			}
			y0 := r.yields.Load()
			var mem0, mem1 uint32
			res[i] = measureSTW(func() error {
				return r.Do(func(js *Ctx) error {
					mem0 = r.eng.mem.Size()
					err := c.op(js)
					mem1 = r.eng.mem.Size()
					return err
				})
			})
			res[i].yields = r.yields.Load() - y0
			res[i].grew = mem1 > mem0
			res[i].mem = mem1
			if res[i].err != nil {
				t.Fatalf("%s: %v", c.name, res[i].err)
			}
		}
		off, on := res[0], res[1]
		// growing the wasm memory past its capacity copies all of it in Go (wazero's
		// append): one memmove that no stop-the-world can interrupt either
		grew := ""
		if on.grew {
			grew = fmt.Sprintf("; the wasm memory grew, to %d MiB", on.mem>>20)
		}
		t.Logf("stw: %s: took %v; longest stop-the-world %v with the yields off, %v on (%d yields%s); longest heartbeat gap %v off, %v on",
			c.name, on.op.Round(time.Millisecond), off.stw.Round(10*time.Microsecond), on.stw.Round(10*time.Microsecond), on.yields, grew,
			off.gap.Round(10*time.Microsecond), on.gap.Round(10*time.Microsecond))
		if !patched {
			continue
		}
		if on.yields == 0 {
			t.Errorf("%s: no yields with them on", c.name)
		}
		if strict && on.stw > stwBound {
			t.Errorf("%s: a stop-the-world waited %v on the JS thread (bound %v; %v with the yields off)", c.name, on.stw, stwBound, off.stw)
		}
		switch {
		case off.stw < stwMeasurable:
			t.Logf("stw: %s: only %v with the yields off: too small here to show what they change", c.name, off.stw)
		case on.stw > off.stw/4:
			t.Errorf("%s: the yields cut the longest stop-the-world only from %v to %v", c.name, off.stw, on.stw)
		}
	}
	_ = r.Do(func(js *Ctx) error { r.eng.setYield(10000); return nil })
	if os.Getenv("JSRT_STW_SNAPSHOT") != "" {
		for _, s := range Snapshots() {
			t.Logf("snapshot: %s", s)
		}
	}
}

type stwResult struct {
	op     time.Duration // how long the operation took
	stw    time.Duration // the longest runtime.ReadMemStats (a stop-the-world)
	gap    time.Duration // the longest gap in a 1 ms heartbeat
	yields uint64
	grew   bool   // the wasm memory grew during op
	mem    uint32 // its size after
	err    error
}

// measureSTW runs op while one goroutine stops the world every 2 ms
// (runtime.ReadMemStats) and another sleeps 1 ms at a time, and returns
// the longest of each.
func measureSTW(op func() error) stwResult {
	var (
		res  stwResult
		wg   sync.WaitGroup
		stop = make(chan struct{})
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			default:
			}
			t0 := time.Now()
			runtime.ReadMemStats(&m)
			if d := time.Since(t0); d > res.stw {
				res.stw = d
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	go func() {
		defer wg.Done()
		last := time.Now()
		for {
			select {
			case <-stop:
				return
			default:
			}
			time.Sleep(time.Millisecond)
			now := time.Now()
			if d := now.Sub(last); d > res.gap {
				res.gap = d
			}
			last = now
		}
	}()
	time.Sleep(20 * time.Millisecond) // both running before op starts
	t0 := time.Now()
	res.err = op()
	res.op = time.Since(t0)
	close(stop)
	wg.Wait()
	return res
}

// Snapshots answers while the JS thread is busy, and shows a running task.
func TestSnapshotsWhileBusy(t *testing.T) {
	r := newTest(t, func(o *Options) { o.Timeout = time.Minute })
	var patched bool
	_ = r.Do(func(js *Ctx) error { patched = r.eng.fns["jr_heap"] != nil; return nil })
	if !patched {
		if os.Getenv("CLACK_JSRT_WASM") == "" {
			t.Fatal("the committed jsrt.wasm has no jr_heap: csrc/quickjs-yield.patch isn't in it, or assets.pack is stale")
		}
		t.Skip("this engine has no jr_heap (built before csrc/quickjs-yield.patch)")
	}
	r.Expose("hold", func(js *Ctx, this Value, args []Value) (any, error) {
		time.Sleep(600 * time.Millisecond)
		return nil, nil
	})
	done := make(chan error, 1)
	go func() {
		done <- r.Do(func(js *Ctx) error { _, err := js.Eval("hold.js", `__clack.hold()`); return err })
	}()
	time.Sleep(300 * time.Millisecond)
	var mine *Snapshot
	for _, s := range Snapshots() {
		if s.Task > 0 {
			mine = &s
		}
	}
	if mine == nil || mine.Task < 200*time.Millisecond {
		t.Fatalf("no busy runtime in Snapshots (%+v)", Snapshots())
	}
	t.Logf("busy: %s", mine)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_ = r.Do(func(js *Ctx) error { r.eng.call("jr_gc"); return nil })
	var found bool
	for _, s := range Snapshots() {
		if s.GCs > 0 && s.Heap > 0 && s.Task == 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("no idle runtime with a collection and a heap size: %+v", Snapshots())
	}
	r.Close()
	<-r.Done()
	if _, ok := live.Load(r); ok {
		t.Error("a closed runtime is still in Snapshots")
	}
}
