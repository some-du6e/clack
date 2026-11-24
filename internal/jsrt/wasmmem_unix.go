//go:build unix

package jsrt

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"

	"golang.org/x/sys/unix"
)

// reserve maps n bytes of inaccessible anonymous memory: address space
// only, no memory and (Linux, MAP_NORESERVE) no commit charge until commit.
func reserve(n uint64) ([]byte, error) {
	if n == 0 || n > math.MaxInt {
		return nil, errors.New("size out of range")
	}
	return unix.Mmap(-1, 0, int(n), unix.PROT_NONE, unix.MAP_PRIVATE|unix.MAP_ANON|mapNoReserve)
}

// commit makes b, a page-aligned piece of a reservation, readable and
// writable. Its pages are zero until written (they were never touched).
func commit(b []byte) error {
	return unix.Mprotect(b, unix.PROT_READ|unix.PROT_WRITE)
}

// release unmaps a whole reservation (b exactly as reserve returned it).
func release(b []byte) error {
	return unix.Munmap(b)
}

// populate writes zeros over b, pages commit just made writable, from the
// kernel: a read of /dev/zero. A page's first write faults, and a fault
// in the wasm that writes it is code a Go stop-the-world must wait out
// (the goroutine is running), while one in the read syscall isn't (the
// world stops without a goroutine in a syscall). On macos-15-intel the
// faults of a 4 MiB ArrayBuffer's zeroing were up to 120-200 ms at once
// (the kernel reclaiming memory for them; TestWasmMemoryGrowsInPlace's
// longest stop-the-world, 1.7 ms on Apple silicon's runners). Where
// /dev/zero can't be read the pages are left to fault where they're used.
func populate(b []byte) {
	fd := zeroDevice()
	if fd < 0 {
		return
	}
	var r0, r1 unix.Rusage
	_ = unix.Getrusage(unix.RUSAGE_SELF, &r0)
	defer func() {
		if unix.Getrusage(unix.RUSAGE_SELF, &r1) == nil {
			populateFaults.Add(r1.Minflt + r1.Majflt - r0.Minflt - r0.Majflt)
		}
	}()
	for len(b) > 0 {
		n, err := unix.Read(fd, b)
		if err == unix.EINTR {
			continue
		}
		if err != nil || n <= 0 {
			return
		}
		b = b[n:]
	}
}

// populateFaults counts the page faults the process took during populate's
// reads (TestWasmMemoryPopulated tells them from the wasm's own).
var populateFaults atomic.Int64

// zeroFD is /dev/zero, opened the first time populate needs it (-1: it
// couldn't be).
var zeroFD struct {
	once sync.Once
	fd   int
}

func zeroDevice() int {
	zeroFD.once.Do(func() {
		fd, err := unix.Open("/dev/zero", unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			fd = -1
		}
		zeroFD.fd = fd
	})
	return zeroFD.fd
}
