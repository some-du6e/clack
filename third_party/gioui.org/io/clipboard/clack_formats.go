// SPDX-License-Identifier: Unlicense OR MIT

package clipboard

import "encoding/binary"

// Clack patch (CLACK_PATCHES.md §68): one write of several formats, as a
// browser's copy puts them on the clipboard (an image's PNG beside its
// <img> markup and a bookmark of its URL), which the X11 and Wayland
// windows offer each under its own MIME type.

// FormatsType is the Type of a WriteCmd whose Data is EncodeFormats'
// encoding of the formats to offer: an X11 window owns CLIPBOARD and
// answers TARGETS and each format's target, a Wayland one offers each
// type from its data source. A text write afterwards replaces them, as
// before. Windows and macOS ignore it (Clack writes those clipboards
// natively: internal/ui/clipimg).
const FormatsType = "application/x-gio-clipboard-formats"

// Format is one MIME type's data.
type Format struct {
	Type string
	Data []byte
}

// EncodeFormats is fs as FormatsType's Data: for each, the type's length
// (uint32, little-endian), the type, the data's length and the data.
func EncodeFormats(fs []Format) []byte {
	n := 0
	for _, f := range fs {
		n += 8 + len(f.Type) + len(f.Data)
	}
	b := make([]byte, 0, n)
	for _, f := range fs {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(f.Type)))
		b = append(b, f.Type...)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(f.Data)))
		b = append(b, f.Data...)
	}
	return b
}

// DecodeFormats is EncodeFormats' reverse; ok is false when b isn't one
// (a length past its end, an empty type).
func DecodeFormats(b []byte) (fs []Format, ok bool) {
	next := func() ([]byte, bool) {
		if len(b) < 4 {
			return nil, false
		}
		n := binary.LittleEndian.Uint32(b)
		if uint64(n) > uint64(len(b)-4) {
			return nil, false
		}
		v := b[4 : 4+n]
		b = b[4+n:]
		return v, true
	}
	for len(b) > 0 {
		t, ok := next()
		if !ok || len(t) == 0 {
			return nil, false
		}
		d, ok := next()
		if !ok {
			return nil, false
		}
		fs = append(fs, Format{Type: string(t), Data: d})
	}
	return fs, true
}
