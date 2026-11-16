package jsrt

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"
)

// growth is one run of growJS: what growing the JS heap cost.
type growth struct {
	moved      bool    // the wasm memory's base address changed
	allocated  uint64  // bytes the Go heap allocated meanwhile
	gcs        uint64  // Go collections meanwhile
	memBefore  uint32  // the wasm memory's size before and after
	memAfter   uint32  //
	stw        float64 // the longest stop-the-world meanwhile (ms)
	snap       Snapshot
	reservedOK bool // Snapshot.WasmReserved covers the memory
}

func (g growth) String() string {
	return fmt.Sprintf("the wasm memory %d → %d MiB (base address moved: %v), %.0f MiB allocated on the Go heap, %d Go collections, longest stop-the-world %.2f ms",
		g.memBefore>>20, g.memAfter>>20, g.moved, float64(g.allocated)/(1<<20), g.gcs, g.stw)
}

// growJS builds bufs ArrayBuffers of 4 MiB in a fresh runtime (the wasm
// memory grows by about that much) while measureSTW stops the world every
// 2 ms, and reports what it cost.
func growJS(t *testing.T, heap bool, bufs int) growth {
	t.Helper()
	r := newTest(t, func(o *Options) {
		o.Timeout = time.Minute
		o.MemoryLimit = 1024 << 20
		o.heapWasmMemory = heap
	})
	base := func() uintptr {
		b, _ := r.eng.mem.Read(0, 1)
		return uintptr(unsafe.Pointer(unsafe.SliceData(b)))
	}
	var g growth
	var b0, b1 uintptr
	sample := []metrics.Sample{{Name: "/gc/heap/allocs:bytes"}, {Name: "/gc/cycles/total:gc-cycles"}}
	runtime.GC()
	metrics.Read(sample)
	a0, c0 := sample[0].Value.Uint64(), sample[1].Value.Uint64()
	res := measureSTW(func() error {
		return r.Do(func(js *Ctx) error {
			b0, g.memBefore = base(), r.eng.mem.Size()
			_, err := js.Eval("grow.js", fmt.Sprintf(`globalThis.keep = []; for (let i = 0; i < %d; i++) keep.push(new ArrayBuffer(4 << 20)); keep.length`, bufs))
			b1, g.memAfter = base(), r.eng.mem.Size()
			return err
		})
	})
	if res.err != nil {
		t.Fatal(res.err)
	}
	metrics.Read(sample)
	g.allocated, g.gcs = sample[0].Value.Uint64()-a0, sample[1].Value.Uint64()-c0
	g.moved, g.stw = b0 != b1, float64(res.stw.Microseconds())/1000
	g.snap = r.snapshot(monoNow())
	g.reservedOK = g.snap.WasmReserved >= g.snap.WasmMemory && g.snap.WasmMemory == uint64(g.memAfter)
	r.Close() // the next run measures without this one's memory
	<-r.Done()
	debug.FreeOSMemory() // nor the Go heap it left behind (macOS pages slowly under pressure)
	return g
}

// The wasm memory grows in place: no copy (its base address stays), no Go
// heap allocation and no Go collection for it, and a stop-the-world never
// waits on a growth. The same growth with the memory in a Go slice grown
// like wazero's own (the engine before wasmmem.go) is measured alongside:
// it moves, allocates the memory's size several times over and holds a
// stop-the-world for the length of each copy.
func TestWasmMemoryGrowsInPlace(t *testing.T) {
	if testing.Short() {
		t.Skip("grows a JS heap by 512 MiB")
	}
	const bufs = 128 // 512 MiB
	heap := growJS(t, true, bufs)
	t.Logf("wasmmem: a Go slice (wazero's growth): %v", heap)
	res := growJS(t, false, bufs)
	t.Logf("wasmmem: reserved address space: %v", res)
	t.Logf("wasmmem: snapshot: %s", res.snap)
	if res.memAfter < res.memBefore+bufs*(4<<20) {
		t.Fatalf("the wasm memory grew %d → %d bytes for %d MiB of ArrayBuffers", res.memBefore, res.memAfter, bufs*4)
	}
	if heapMemoryWanted() {
		t.Log("wasmmem: CLACK_JSRT_HEAP_MEMORY is set: both sides are Go slices, so the checks below fail")
	}
	if res.moved {
		t.Errorf("growing the reserved wasm memory moved it (copied all of it)")
	}
	if !heap.moved {
		t.Errorf("the Go slice side never moved: this test no longer measures the copy")
	}
	if !res.reservedOK {
		t.Errorf("Snapshots don't show the reservation: %+v", res.snap)
	}
	if !strings.Contains(res.snap.String(), "outside the Go heap") {
		t.Errorf("a snapshot reads %q", res.snap)
	}
	if !raceOn {
		// the JS thread's own Go allocations (host calls, the task) are
		// kilobytes; the slice grown like wazero's allocated over 1 GiB
		if res.allocated > 32<<20 {
			t.Errorf("growing the reserved wasm memory by %d MiB allocated %d MiB on the Go heap", bufs*4, res.allocated>>20)
		}
		if heap.allocated < bufs*(4<<20) {
			t.Errorf("the Go slice side allocated only %d MiB: this test no longer measures the copy", heap.allocated>>20)
		}
	}
	if res.gcs > 1 {
		t.Errorf("growing the reserved wasm memory ran %d Go collections", res.gcs)
	}
	strict := os.Getenv("JSRT_STW_STRICT") != ""
	if strict && res.stw > float64(stwBound.Milliseconds()) {
		t.Errorf("a stop-the-world waited %.2f ms during the growth (bound %v; %.2f ms with a Go slice)", res.stw, stwBound, heap.stw)
	}
	if heap.stw >= float64(stwMeasurable.Milliseconds()) && res.stw > heap.stw/4 {
		t.Errorf("the reservation cut the longest stop-the-world only from %.2f to %.2f ms", heap.stw, res.stw)
	}
}

// Options.MemoryLimit still caps the JS heap (an allocation past it
// throws), and the wasm memory still stops at its cap (the reservation is
// the cap; memory.grow past it fails, malloc returns NULL). The runtime
// goes on working after both.
func TestWasmMemoryLimit(t *testing.T) {
	const limit = 64 << 20
	r := newTest(t, func(o *Options) {
		o.Timeout = time.Minute
		o.MemoryLimit = limit
	})
	err := r.Do(func(js *Ctx) error {
		_, err := js.Eval("big.js", `globalThis.keep = []; for (let i = 0; i < 64; i++) keep.push(new ArrayBuffer(4 << 20)); keep.length`)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "out of memory") {
		t.Fatalf("256 MiB of ArrayBuffers under a %d MiB JS heap limit: %v, want out of memory", limit>>20, err)
	}
	most := uint64(memoryLimitPages(limit)) << 16 // the wasm memory's cap
	if snap := r.snapshot(monoNow()); snap.WasmReserved != most {
		t.Fatalf("%d MiB reserved for a memory capped at %d MiB: %s", snap.WasmReserved>>20, most>>20, snap)
	}
	err = r.Do(func(js *Ctx) error {
		if p := uint32(r.eng.call("jr_malloc", most)); p != 0 {
			return fmt.Errorf("jr_malloc(%d), the whole cap, returned %#x", most, p)
		}
		if size := uint64(r.eng.mem.Size()); size > most {
			return fmt.Errorf("the wasm memory is %d bytes, past its %d-byte cap", size, most)
		}
		// most of the headroom above the JS limit is there for buffers Go
		// copies in: one that fits still does
		p := uint32(r.eng.call("jr_malloc", 256<<20))
		if p == 0 {
			return fmt.Errorf("jr_malloc(256 MiB) failed with %d MiB of headroom", (most-uint64(r.eng.mem.Size()))>>20)
		}
		r.eng.mem.WriteByte(p+256<<20-1, 1)
		r.eng.free(p)
		_, err := js.Eval("after.js", `globalThis.keep = null; [1, 2, 3].map(x => x * 2).join()`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Closing a Runtime releases its reservation (TestReservationReleased:
// the OS gets the address space back). A Runtime the watchdog abandons
// keeps its memory while its JS thread is still in the wasm, and releases
// it once that thread returns: unmapped under it, the thread's next memory
// access would crash the process.
func TestWasmReservationClosed(t *testing.T) {
	waitDone := func(r *Runtime) {
		t.Helper()
		select {
		case <-r.Done():
		case <-time.After(10 * time.Second):
			t.Fatal("the runtime didn't close")
		}
	}
	for i := 0; i < 5; i++ {
		r := newTest(t)
		if r.wasmMem.reserved.Load() == 0 {
			t.Skip("no reservation here (the wasm memory is a Go slice)")
		}
		r.Close()
		waitDone(r)
		if n := r.wasmMem.reserved.Load(); n != 0 {
			t.Fatalf("runtime %d still holds %d bytes of reservation after Close", i, n)
		}
	}

	r := newTest(t, func(o *Options) { o.Timeout = 200 * time.Millisecond })
	release, back := make(chan struct{}), make(chan struct{})
	r.Expose("stuck", func(js *Ctx, this Value, args []Value) (any, error) {
		<-release
		return nil, nil
	})
	go func() {
		defer close(back)
		_ = r.Do(func(js *Ctx) error {
			// after the host call returns, the thread runs on in the wasm
			// (the interrupt throws at its next poll) and touches its heap
			_, err := js.Eval("stuck.js", `__clack.stuck(); const a = []; for (let i = 0; i < 1e5; i++) a.push({ i }); a.length`)
			return err
		})
	}()
	waitDone(r) // the watchdog gave up on it (Timeout + 1 s)
	if r.wasmMem.reserved.Load() == 0 {
		t.Fatal("an abandoned runtime released its memory while its JS thread was still in the wasm")
	}
	close(release)
	<-back
	for deadline := time.Now().Add(10 * time.Second); r.wasmMem.reserved.Load() != 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the abandoned runtime's thread returned but its reservation wasn't released")
		}
	}
	if _, ok := live.Load(r); ok {
		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
			if _, ok := live.Load(r); !ok {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the abandoned runtime is still listed after its thread returned")
			}
		}
	}
}

// An engine whose instantiation fails after its memory was made frees
// the memory: when the module's own start function traps (or its data
// doesn't fit), wazero drops the module without closing it, and a
// reservation isn't the collector's to free. (A trap in _initialize, the
// start function newEngine names, wazero closes the module for.)
func TestWasmReservationFailedInstantiate(t *testing.T) {
	// (memory 1) (func unreachable) (export "_initialize" (func 0)) (start 0)
	bad := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x04, 0x01, 0x60, 0x00, 0x00, // type: func () -> ()
		0x03, 0x02, 0x01, 0x00, // function 0 of type 0
		0x05, 0x03, 0x01, 0x00, 0x01, // memory: min 1 page
		0x07, 0x0f, 0x01, 0x0b, '_', 'i', 'n', 'i', 't', 'i', 'a', 'l', 'i', 'z', 'e', 0x00, 0x00, // export
		0x08, 0x01, 0x00, // start: function 0
		0x0a, 0x05, 0x01, 0x03, 0x00, 0x00, 0x0b, // code: unreachable
	}
	old := engineWasm
	engineWasm = func() []byte { return bad }
	defer func() { engineWasm = old }()
	r := &Runtime{opts: Options{MemoryLimit: 64 << 20, Log: func(level, msg string) { t.Logf("js %s: %s", level, msg) }}}
	e, err := newEngine(r)
	if err == nil {
		e.close()
		t.Fatal("an engine whose start function traps instantiated")
	}
	t.Logf("wasmmem: the failed instantiation: %v", err)
	if n := r.wasmMem.reserved.Load(); n != 0 {
		t.Fatalf("the failed instantiation left %d bytes reserved", n)
	}
}

// CLACK_JSRT_HEAP_MEMORY takes 0, off, false and no as off and anything
// else as on, like CLACK_FLIGHT_RECORDER; the first engine it puts in a Go
// slice logs it, later ones don't, and =0 leaves the reservation on.
func TestHeapMemoryEnv(t *testing.T) {
	for v, want := range map[string]bool{"": false, "0": false, "off": false, "False": false, " no ": false, "1": true, "yes": true, "on": true} {
		t.Setenv("CLACK_JSRT_HEAP_MEMORY", v)
		if got := heapMemoryWanted(); got != want {
			t.Errorf("CLACK_JSRT_HEAP_MEMORY=%q: %v, want %v", v, got, want)
		}
	}
	t.Setenv("CLACK_JSRT_HEAP_MEMORY", "1")
	heapMemoryLogged = sync.Once{}
	var mu sync.Mutex
	lines := 0
	logf := func(level, msg string) {
		if strings.Contains(msg, "CLACK_JSRT_HEAP_MEMORY") {
			mu.Lock()
			lines++
			mu.Unlock()
		}
	}
	for i := 0; i < 2; i++ {
		r := newTest(t, func(o *Options) { o.Log = logf })
		if res, size := r.wasmMem.reserved.Load(), r.wasmMem.size.Load(); res != 0 || size == 0 {
			t.Fatalf("engine %d with CLACK_JSRT_HEAP_MEMORY=1: %d bytes reserved, a %d-byte memory", i, res, size)
		}
		r.Close()
		<-r.Done()
	}
	mu.Lock()
	if lines != 1 {
		t.Errorf("%d log lines for two engines in Go slices, want 1", lines)
	}
	mu.Unlock()
	t.Setenv("CLACK_JSRT_HEAP_MEMORY", "0")
	if r := newTest(t); r.wasmMem.reserved.Load() == 0 {
		t.Error("CLACK_JSRT_HEAP_MEMORY=0 put the wasm memory in a Go slice")
	}
}

// The address space really goes back to the OS: 4096 reservations of 128
// GiB, each touched and released, are 512 TiB, more than a 64-bit
// process's address space (128 TiB on x86-64, Windows and macOS), so
// reserve fails partway through if release leaks.
func TestReservationReleased(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) < 8 {
		t.Skip("32-bit: no reservations")
	}
	const n, size = 4096, 128 << 30
	if b, err := reserve(size); err != nil {
		t.Skipf("can't reserve %d GiB at once on %s/%s: %v", size>>30, runtime.GOOS, runtime.GOARCH, err)
	} else if err := release(b); err != nil {
		t.Fatalf("release: %v", err)
	}
	for i := 0; i < n; i++ {
		b, err := reserve(size)
		if err != nil {
			t.Fatalf("reservation %d of %d GiB failed (release leaks address space?): %v", i, size>>30, err)
		}
		if err := commit(b[:1<<20]); err != nil {
			t.Fatalf("commit: %v", err)
		}
		if b[12345] != 0 {
			t.Fatal("a committed page isn't zero")
		}
		b[12345] = 1
		if err := release(b); err != nil {
			t.Fatalf("release: %v", err)
		}
	}
}

// A reservation is committed from its start as the memory grows, in
// commitStep pieces, and only up to its size; a fresh one reads zero.
func TestReservedMemoryReallocate(t *testing.T) {
	var st wasmMemStats
	a := &wasmAllocator{stats: &st}
	lm := a.Allocate(0, 64<<20)
	if a.err != nil {
		t.Skipf("no reservation here: %v", a.err)
	}
	defer lm.Free()
	m := lm.(*reservedMemory)
	if b := m.Reallocate(0); b == nil || len(b) != 0 {
		t.Fatalf("Reallocate(0) = %v", b)
	}
	b := m.Reallocate(3 << 16)
	if len(b) != 3<<16 || cap(b) != 3<<16 || m.committed != commitStep {
		t.Fatalf("3 pages: len %d cap %d, committed %d", len(b), cap(b), m.committed)
	}
	b[len(b)-1] = 7
	b2 := m.Reallocate(commitStep + 1<<16)
	if unsafe.SliceData(b2) != unsafe.SliceData(b) || b2[3<<16-1] != 7 || b2[len(b2)-1] != 0 || m.committed != 2*commitStep {
		t.Fatalf("grown: moved %v, kept %d, new %d, committed %d", unsafe.SliceData(b2) != unsafe.SliceData(b), b2[3<<16-1], b2[len(b2)-1], m.committed)
	}
	if st.size.Load() != commitStep+1<<16 || st.reserved.Load() != 64<<20 {
		t.Fatalf("stats: size %d, reserved %d", st.size.Load(), st.reserved.Load())
	}
	if m.Reallocate(64<<20+1<<16) != nil {
		t.Fatal("grew past the reservation")
	}
	if b := m.Reallocate(64 << 20); len(b) != 64<<20 || m.committed != 64<<20 {
		t.Fatalf("the whole reservation: len %d, committed %d", len(b), m.committed)
	}
	m.Free()
	m.Free() // twice is fine
	if m.Reallocate(1<<16) != nil || st.reserved.Load() != 0 {
		t.Fatal("a freed reservation grew, or still counts")
	}
}

// BenchmarkWasmMemoryGrowth: a fresh runtime's JS heap grown by 256 MiB
// of ArrayBuffers, with the wasm memory in a reservation (reserved) and in
// a Go slice grown like wazero's own (go-slice, the engine before
// wasmmem.go). B/op is what the Go heap allocated for it; gcs/op the Go
// collections it ran.
func BenchmarkWasmMemoryGrowth(b *testing.B) {
	for _, heap := range []bool{false, true} {
		name := "reserved"
		if heap {
			name = "go-slice"
		}
		b.Run(name, func(b *testing.B) {
			sample := []metrics.Sample{{Name: "/gc/cycles/total:gc-cycles"}}
			var gcs uint64
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				r := newTest(b, func(o *Options) {
					o.Timeout = time.Minute
					o.MemoryLimit = 1024 << 20
					o.heapWasmMemory = heap
				})
				runtime.GC()
				metrics.Read(sample)
				g0 := sample[0].Value.Uint64()
				b.StartTimer()
				err := r.Do(func(js *Ctx) error {
					_, err := js.Eval("grow.js", `globalThis.keep = []; for (let i = 0; i < 64; i++) keep.push(new ArrayBuffer(4 << 20)); keep.length`)
					return err
				})
				b.StopTimer()
				if err != nil {
					b.Fatal(err)
				}
				metrics.Read(sample)
				gcs += sample[0].Value.Uint64() - g0
				r.Close()
				<-r.Done()
				b.StartTimer()
			}
			b.ReportMetric(float64(gcs)/float64(b.N), "gcs/op")
		})
	}
}
