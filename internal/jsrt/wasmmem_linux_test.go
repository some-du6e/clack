package jsrt

import (
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// The pages a growth commits are populated in populate's read syscall, so
// the wasm's own writes to them fault on almost none: a fault the
// goroutine running the wasm takes holds every stop-the-world until it's
// done, one in a syscall doesn't (wasmmem_unix.go). Growing the memory by
// 512 MiB of ArrayBuffers, the process's faults outside populate's reads
// stay under a tenth of the pages grown. Unpopulated, the wasm faulted on
// every one, and on the macOS runners some of those faults took 100-260
// ms (TestWasmMemoryGrowsInPlace's longest stop-the-world on
// macos-15-intel: 137, 211 and 262 ms in 3 of 7 jsrt runs since
// 2026-11-16, 126 ms on train-229's 45256775420). Linux counts every
// zero-fill fault in getrusage's ru_minflt.
func TestWasmMemoryPopulated(t *testing.T) {
	r := newTest(t, func(o *Options) {
		o.Timeout = time.Minute
		o.MemoryLimit = 1024 << 20
	})
	if r.wasmMem.reserved.Load() == 0 {
		t.Fatal("no reservation: the wasm memory is a Go slice")
	}
	var r0, r1 unix.Rusage
	var m0, m1 uint32
	p0 := populateFaults.Load()
	err := r.Do(func(js *Ctx) error {
		m0 = r.eng.mem.Size()
		if err := unix.Getrusage(unix.RUSAGE_SELF, &r0); err != nil {
			return err
		}
		_, err := js.Eval("grow.js", `globalThis.keep = []; for (let i = 0; i < 128; i++) keep.push(new ArrayBuffer(4 << 20)); keep.length`)
		if err := unix.Getrusage(unix.RUSAGE_SELF, &r1); err != nil {
			return err
		}
		m1 = r.eng.mem.Size()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	pages := int64(m1-m0) / int64(os.Getpagesize())
	total := r1.Minflt + r1.Majflt - r0.Minflt - r0.Majflt
	in := populateFaults.Load() - p0
	out := total - in
	t.Logf("wasmmem: the memory grew %d → %d MiB (%d pages): %d faults, %d of them in populate's reads, %d elsewhere", m0>>20, m1>>20, pages, total, in, out)
	if pages < 100<<20/int64(os.Getpagesize()) {
		t.Fatalf("the memory grew only %d pages", pages)
	}
	if out > pages/10 {
		t.Errorf("growing the wasm memory by %d pages, %d faults came outside populate's reads (in the wasm, where a stop-the-world waits them out), %d in them", pages, out, in)
	}
}
