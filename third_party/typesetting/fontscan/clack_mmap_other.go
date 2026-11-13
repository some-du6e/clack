// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

//go:build !(linux || darwin || freebsd || netbsd || openbsd || windows)

package fontscan

import "errors"

// mapFontFile can't map here: fonts are read as before (clack_mmap.go).
func mapFontFile(path string) ([]byte, error) {
	return nil, errors.New("fontscan: no memory mapping on this system")
}
