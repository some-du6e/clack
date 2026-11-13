// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

//go:build linux || darwin || freebsd || netbsd || openbsd

package fontscan

import (
	"errors"
	"os"
	"syscall"
)

// mapFontFile maps path read-only (Clack patch 3, clack_mmap.go).
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
	return syscall.Mmap(int(f.Fd()), 0, int(n), syscall.PROT_READ, syscall.MAP_SHARED)
}
