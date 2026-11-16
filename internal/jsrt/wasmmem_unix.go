//go:build unix

package jsrt

import (
	"errors"
	"math"

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
