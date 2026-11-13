// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package fontscan

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// mapFontFile maps path read-only (Clack patch 3, clack_mmap.go). The view
// outlives the mapping and file handles, which are closed here.
func mapFontFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	n := fi.Size()
	if n <= 0 || int64(int(n)) != n {
		return nil, errors.New("fontscan: can't map an empty or huge file")
	}
	h, err := syscall.CreateFileMapping(syscall.Handle(f.Fd()), nil, syscall.PAGE_READONLY, 0, 0, nil)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(h)
	addr, err := syscall.MapViewOfFile(h, syscall.FILE_MAP_READ, 0, 0, 0)
	if err != nil {
		return nil, err
	}
	// the view's address is memory Go doesn't manage: read it as a pointer
	p := *(*unsafe.Pointer)(unsafe.Pointer(&addr))
	return unsafe.Slice((*byte)(p), int(n)), nil
}
