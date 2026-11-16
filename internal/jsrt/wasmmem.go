package jsrt

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/tetratelabs/wazero/experimental"
)

// The engine's wasm linear memory lives outside the Go heap.
//
// wazero's default memory is a Go []byte, and growing it past its
// capacity is an append: a new slice and one memmove of everything in it.
// QuickJS's heap is in that memory, so as a plugin's heap grew to a few
// hundred MB every growth copied all of it, in a single memmove no
// stop-the-world can interrupt, and left the old slice for the collector:
// 333-520 MB allocated per perf.yml plugins pass, 50-70% of all the
// process allocated. Instead each engine reserves address space for its
// memory's maximum once (mmap PROT_NONE, VirtualAlloc MEM_RESERVE) and
// commits pages at its end as the memory grows (mprotect, MEM_COMMIT):
// growth happens in place, copies nothing and allocates nothing the Go
// collector sees. The cap stays wazero's (WithMemoryLimitPages: Grow
// refuses past it before it asks for memory) and the reservation is that
// cap, so it is also the most the memory can ever take.
//
// The reservation is released when the engine closes, which happens on
// the JS thread once it has left the wasm (engine.close, from the loop's
// shutdown): never under a JS thread still running in it, so a Runtime the
// watchdog abandons keeps its memory, as its Go slice was kept before,
// until its stuck thread returns. Restarted runtimes return their address
// space; TestReservationReleased checks that the OS gets it back.
// Where address space can't be reserved (a 32-bit build, an address-space
// rlimit) the memory falls back to a Go slice, as before.

// heapMemoryWanted reports whether CLACK_JSRT_HEAP_MEMORY asks for the
// wasm memory in a Go slice, as before this file (for comparing): any
// value but 0, off, false and no.
func heapMemoryWanted() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CLACK_JSRT_HEAP_MEMORY"))) {
	case "", "0", "off", "false", "no":
		return false
	}
	return true
}

// heapMemoryLogged: the first engine made in a Go slice for
// CLACK_JSRT_HEAP_MEMORY logs it.
var heapMemoryLogged sync.Once

// commitStep is how much more of a reservation is committed at a time (at
// least what the memory grows by): dlmalloc grows the wasm memory a few
// pages at a time, and one mprotect per MiB is plenty.
const commitStep = 1 << 20

// reservedWasm counts the address space reserved for wasm memories in
// this process (bytes, and reservations): tests, and freeze reports.
var reservedWasm, reservations atomic.Int64

// ReservedWasmMemory is the address space this process holds for plugin
// engines' wasm memories (bytes) and in how many reservations: one per
// engine not yet closed (an engine abandoned by its watchdog included).
// Any goroutine.
func ReservedWasmMemory() (bytes int64, n int) {
	return reservedWasm.Load(), int(reservations.Load())
}

// wasmMemStats is what a Runtime knows of its engine's memory, for
// Snapshots: atomics, read from any goroutine.
type wasmMemStats struct {
	size     atomic.Uint64 // the memory's length in bytes
	reserved atomic.Uint64 // address space reserved for it; 0 = it's a Go slice
}

// wasmAllocator is the experimental.MemoryAllocator newEngine hands
// wazero (through the instantiation context). An engine has one memory.
type wasmAllocator struct {
	stats *wasmMemStats
	heap  bool                      // a Go slice, as wazero's own (tests measuring the difference)
	err   error                     // why no reservation was made (the memory is a Go slice)
	mem   experimental.LinearMemory // the memory it made
}

func (a *wasmAllocator) Allocate(capacity, max uint64) experimental.LinearMemory {
	if !a.heap {
		buf, err := reserve(max)
		if err == nil {
			reservedWasm.Add(int64(len(buf)))
			reservations.Add(1)
			a.stats.reserved.Store(uint64(len(buf)))
			a.mem = &reservedMemory{buf: buf, stats: a.stats}
			return a.mem
		}
		a.err = fmt.Errorf("reserving %d MiB of address space: %w", max>>20, err)
	}
	if capacity > max {
		capacity = max
	}
	if capacity > math.MaxInt {
		capacity = 0
	}
	a.mem = &heapMemory{buf: make([]byte, 0, capacity), stats: a.stats}
	return a.mem
}

// free frees the memory after an instantiation that failed: wazero frees
// a memory only with its module (Close), and a module whose start function
// trapped (or whose data didn't fit) is dropped without one. Free twice is
// fine.
func (a *wasmAllocator) free() {
	if a.mem != nil {
		a.mem.Free()
	}
}

// reservedMemory is a wasm memory in address space reserved outside the Go
// heap. Reallocate runs on the JS thread (memory.grow); Free on the JS
// thread when the engine closes.
type reservedMemory struct {
	buf       []byte // the whole reservation, as reserve returned it; nil once freed
	committed uint64 // bytes from buf's start that are committed
	stats     *wasmMemStats
}

func (m *reservedMemory) Reallocate(size uint64) []byte {
	if m.buf == nil || size > uint64(len(m.buf)) {
		return nil
	}
	if size > m.committed {
		end := min((size+commitStep-1)/commitStep*commitStep, uint64(len(m.buf)))
		if err := commit(m.buf[m.committed:end]); err != nil {
			// out of memory or commit charge: memory.grow returns -1 and
			// QuickJS's malloc NULL, so the allocation throws in JS
			return nil
		}
		m.committed = end
	}
	m.stats.size.Store(size)
	return m.buf[:size:size]
}

func (m *reservedMemory) Free() {
	if m.buf == nil {
		return
	}
	n := len(m.buf)
	if err := release(m.buf); err != nil {
		// the range stays reserved (and counted); nothing can use it again
		return
	}
	m.buf, m.committed = nil, 0
	reservedWasm.Add(-int64(n))
	reservations.Add(-1)
	m.stats.size.Store(0)
	m.stats.reserved.Store(0)
}

// heapMemory is a wasm memory in a Go slice, grown the way wazero grows
// its own (append): the fallback when no address space could be reserved.
type heapMemory struct {
	buf   []byte
	stats *wasmMemStats
}

func (m *heapMemory) Reallocate(size uint64) []byte {
	if size > math.MaxInt {
		return nil
	}
	if n := uint64(len(m.buf)); size <= uint64(cap(m.buf)) {
		m.buf = m.buf[:size]
	} else {
		m.buf = append(m.buf, make([]byte, size-n)...)
	}
	m.stats.size.Store(size)
	return m.buf
}

func (m *heapMemory) Free() {
	m.buf = nil
	m.stats.size.Store(0)
}
