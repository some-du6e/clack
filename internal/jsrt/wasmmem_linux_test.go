package jsrt

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// A fresh engine's initial memory isn't populated: populate is for the
// pages a growth adds, where the wasm's first write to each faults inside
// a stop-the-world's wait, and the initial memory is mostly the C stack
// (-z stack-size, 8 MiB) that a runtime barely reaches into. Populated with
// the rest (populate's first cut, jsrt-stw-populate 687521dc2), every
// engine held all of it resident, about 9 MiB, before any JS ran. The
// resident part is read with mincore; the process's RSS across four more
// engines is logged alongside.
func TestWasmMemoryInitialUnpopulated(t *testing.T) {
	newTest(t) // the first engine: the module's compilation and the like
	rss0, err := processRSS()
	if err != nil {
		t.Fatal(err)
	}
	var rs []*Runtime
	for i := 0; i < 4; i++ {
		rs = append(rs, newTest(t))
	}
	rss1, err := processRSS()
	if err != nil {
		t.Fatal(err)
	}
	r := rs[len(rs)-1]
	initial := r.wasmMem.initial.Load()
	if r.wasmMem.reserved.Load() == 0 || initial == 0 {
		t.Fatalf("no reservation (%d bytes) or no initial memory (%d bytes): the wasm memory is a Go slice", r.wasmMem.reserved.Load(), initial)
	}
	var res int64
	err = r.Do(func(js *Ctx) error {
		b, ok := r.eng.mem.Read(0, uint32(initial))
		if !ok {
			return fmt.Errorf("the memory (%d bytes) doesn't hold the initial %d", r.eng.mem.Size(), initial)
		}
		var err error
		res, err = resident(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("wasmmem: a fresh engine's initial memory is %d KiB, %d KiB of it resident; the process's RSS went %d → %d MiB for 4 more engines (%.1f MiB each)",
		initial>>10, res>>10, rss0>>20, rss1>>20, float64(rss1-rss0)/4/(1<<20))
	if res > int64(initial)/2 {
		t.Errorf("a fresh engine holds %d KiB of its %d KiB initial memory resident: it was populated", res>>10, initial>>10)
	}
}

// resident is how much of b (page-aligned at its start) is in memory
// (mincore).
func resident(b []byte) (int64, error) {
	ps := os.Getpagesize()
	vec := make([]byte, (len(b)+ps-1)/ps)
	if _, _, e := unix.Syscall(unix.SYS_MINCORE, uintptr(unsafe.Pointer(unsafe.SliceData(b))), uintptr(len(b)), uintptr(unsafe.Pointer(unsafe.SliceData(vec)))); e != 0 {
		return 0, fmt.Errorf("mincore: %w", e)
	}
	var n int64
	for _, v := range vec {
		n += int64(v & 1)
	}
	return n * int64(ps), nil
}

// processRSS is the process's resident set (/proc/self/statm), in bytes.
func processRSS() (int64, error) {
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return 0, fmt.Errorf("/proc/self/statm reads %q", b)
	}
	n, err := strconv.ParseInt(f[1], 10, 64)
	if err != nil {
		return 0, err
	}
	return n * int64(os.Getpagesize()), nil
}
