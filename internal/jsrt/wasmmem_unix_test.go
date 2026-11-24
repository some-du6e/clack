//go:build unix

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
// 512 MiB of ArrayBuffers, populate writes all of the growth, and the
// process's faults outside its reads stay under a tenth of the pages grown.
// Unpopulated, the wasm faulted on every one, and on the macOS runners some
// of those faults took 100-260 ms (TestWasmMemoryGrowsInPlace's longest
// stop-the-world on macos-15-intel: 137, 211 and 262 ms in 3 of 7 jsrt
// runs since 2026-11-16, 126 ms on train-229's 45256775420), where the
// stop-the-world ratios pass about as often without populate as with it.
// getrusage's ru_minflt counts every zero-fill fault on Linux and on macOS
// (XNU: the task's faults less its pageins); a control checks that it
// does on this runner, so a count of none means none.
func TestWasmMemoryPopulated(t *testing.T) {
	const fresh = 1024
	ps := os.Getpagesize()
	b, err := unix.Mmap(-1, 0, fresh*ps, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		t.Fatal(err)
	}
	f0, err := processFaults()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < fresh; i++ {
		b[i*ps] = 1
	}
	f1, err := processFaults()
	if err != nil {
		t.Fatal(err)
	}
	_ = unix.Munmap(b)
	t.Logf("wasmmem: control: writing %d fresh pages counted %d faults", fresh, f1-f0)
	if f1-f0 < fresh/2 {
		t.Fatalf("writing %d fresh pages counted %d faults: getrusage doesn't count zero-fill faults here, so the counts below can't tell", fresh, f1-f0)
	}
	if !populateWanted() {
		t.Log("wasmmem: CLACK_JSRT_POPULATE is off: nothing is populated, so the checks below fail")
	}

	r := newTest(t, func(o *Options) {
		o.Timeout = time.Minute
		o.MemoryLimit = 1024 << 20
	})
	if r.wasmMem.reserved.Load() == 0 {
		t.Fatal("no reservation: the wasm memory is a Go slice")
	}
	var g0, g1 int64
	var m0, m1 uint32
	p0, w0 := populateFaults.Load(), populatedBytes.Load()
	err = r.Do(func(js *Ctx) error {
		m0 = r.eng.mem.Size()
		var err error
		if g0, err = processFaults(); err != nil {
			return err
		}
		_, err = js.Eval("grow.js", `globalThis.keep = []; for (let i = 0; i < 128; i++) keep.push(new ArrayBuffer(4 << 20)); keep.length`)
		g, ferr := processFaults()
		if ferr != nil {
			return ferr
		}
		g1, m1 = g, r.eng.mem.Size()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	grew := int64(m1 - m0)
	pages := grew / int64(ps)
	total := g1 - g0
	in := populateFaults.Load() - p0
	out := total - in
	wrote := populatedBytes.Load() - w0
	t.Logf("wasmmem: the memory grew %d → %d MiB (%d pages), populate wrote %d MiB: %d faults, %d of them in populate's reads, %d elsewhere", m0>>20, m1>>20, pages, wrote>>20, total, in, out)
	if pages < 100<<20/int64(ps) {
		t.Fatalf("the memory grew only %d pages", pages)
	}
	// populate writes what each growth commits, from the end of what it
	// wrote before: at most a commit step of the growth was written ahead
	if wrote < grew-commitStep {
		t.Errorf("the memory grew %d MiB and populate wrote %d MiB of it: it didn't run on the growth", grew>>20, wrote>>20)
	}
	if out > pages/10 {
		t.Errorf("growing the wasm memory by %d pages, %d faults came outside populate's reads (in the wasm, where a stop-the-world waits them out), %d in them", pages, out, in)
	}
}

// processFaults is the page faults the process took so far (getrusage).
func processFaults() (int64, error) {
	var r unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &r); err != nil {
		return 0, err
	}
	return faults(&r), nil
}
