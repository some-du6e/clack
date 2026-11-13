// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package opentype

import "bytes"

// Clack patch 2 (CLACK_PATCHES.md): a font file already in memory is read
// without copying its tables. The Loader's RawTable(To) returns a slice
// of the file's bytes for each uncompressed table instead of reading it
// into a new buffer (dst is not written to). Clack's embedded Lato faces
// are in the binary's read-only data, so their tables cost no heap at all;
// they were 6 MB copied. Nothing in this module writes into a table's
// bytes.

// BytesResource is a Resource over a font file in memory.
type BytesResource struct {
	*bytes.Reader
	b []byte
}

// NewBytesResource reads b, which must not change while fonts loaded from
// it are in use.
func NewBytesResource(b []byte) BytesResource { return BytesResource{bytes.NewReader(b), b} }

// Bytes is the whole file.
func (r BytesResource) Bytes() []byte { return r.b }
