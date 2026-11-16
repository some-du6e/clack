package jsrt

import (
	"errors"
	"math"
	"unsafe"

	"golang.org/x/sys/windows"
)

// reserve reserves n bytes of address space (MEM_RESERVE, PAGE_NOACCESS):
// no memory and no commit charge until commit.
func reserve(n uint64) ([]byte, error) {
	if n == 0 || n > math.MaxInt {
		return nil, errors.New("size out of range")
	}
	p, err := windows.VirtualAlloc(0, uintptr(n), windows.MEM_RESERVE, windows.PAGE_NOACCESS)
	if err != nil {
		return nil, err
	}
	return unsafe.Slice((*byte)(addrPointer(p)), int(n)), nil
}

// commit commits b, a page-aligned piece of a reservation, readable and
// writable (MEM_COMMIT; its pages are zero).
func commit(b []byte) error {
	_, err := windows.VirtualAlloc(uintptr(unsafe.Pointer(unsafe.SliceData(b))), uintptr(len(b)), windows.MEM_COMMIT, windows.PAGE_READWRITE)
	return err
}

// release frees a whole reservation, committed pages and address space
// (b exactly as reserve returned it).
func release(b []byte) error {
	return windows.VirtualFree(uintptr(unsafe.Pointer(unsafe.SliceData(b))), 0, windows.MEM_RELEASE)
}

// addrPointer is unsafe.Pointer(p) for an address VirtualAlloc returned:
// memory outside the Go heap, which the collector neither moves nor frees
// (spelled so vet's unsafeptr check, which can't know that, accepts it).
func addrPointer(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}
