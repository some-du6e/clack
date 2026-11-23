// SPDX-License-Identifier: Unlicense OR MIT

// Package winclip runs a process's Windows clipboard sessions one at a
// time (Clack patch, CLACK_PATCHES.md §65).
//
// GetClipboardData hands a reader its own copy of a format's data, a
// handle the clipboard owns and that is good only while the clipboard is
// open, and Windows lets two threads that open the clipboard with no
// window (OpenClipboard(NULL)) hold it at the same time. A paste in Clack
// read it that way from two goroutines at once (the rich text, and the
// files and picture), and one's CloseClipboard freed the copy the other
// was reading: GlobalSize on the freed handle ended the process with
// STATUS_HEAP_CORRUPTION (0xc0000374) inside ntdll, where Go never sees
// it. Every session of the process, Gio's own (a window's text paste and
// copy) and Clack's, goes through Session: one open, the reads or writes,
// one close, and only then the next.
package winclip

import (
	"errors"
	"unicode/utf16"
)

// Format is a clipboard format: its id and, for a registered one (0xc000
// and up), its name ("HTML Format", "PNG").
type Format struct {
	ID   uint32
	Name string
}

// Snapshot is what one session read (Read): the formats on the clipboard
// in the order its owner put them there (the ones Windows synthesizes
// after them), and a copy of the data of each one asked for. A paste reads
// all it needs in one snapshot, so its text, rich text, files and picture
// are one copy's, however the clipboard changes meanwhile.
type Snapshot struct {
	Formats []Format
	Data    map[uint32][]byte
	TooBig  map[uint32]bool // past the limit asked for: not read
}

// Named is the format with name, if the clipboard has it.
func (s *Snapshot) Named(name string) (Format, bool) {
	if s != nil {
		for _, f := range s.Formats {
			if f.Name == name {
				return f, true
			}
		}
	}
	return Format{}, false
}

// Has reports whether the clipboard has format id.
func (s *Snapshot) Has(id uint32) bool {
	if s != nil {
		for _, f := range s.Formats {
			if f.ID == id {
				return true
			}
		}
	}
	return false
}

// Bytes is format id's data, if it was read.
func (s *Snapshot) Bytes(id uint32) ([]byte, bool) {
	if s == nil {
		return nil, false
	}
	b, ok := s.Data[id]
	return b, ok
}

// Errors of CopyData.
var (
	ErrNoData    = errors.New("winclip: no data in that format")
	ErrTooBig    = errors.New("winclip: the data is past its limit")
	ErrNotMemory = errors.New("winclip: that format's handle isn't global memory")
)

// maxFormats bounds the formats a snapshot lists.
const maxFormats = 512

// UTF16Text decodes CF_UNICODETEXT's bytes, as Chromium's
// ClipboardWin::ReadText reads them (GlobalSize / 2 code units, then
// TrimAfterNull): little-endian UTF-16 up to the first NUL, or to the end
// when there is none; an odd last byte is dropped, an unpaired surrogate
// is U+FFFD.
func UTF16Text(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		if u[i] == 0 {
			u = u[:i]
			break
		}
	}
	return string(utf16.Decode(u))
}

// OnBusy, when set, hears of a window's copy or paste (Gio's own) that
// failed because another app kept the clipboard open (write: a copy). It
// runs on its own goroutine. Clack shows a toast: the copy didn't happen.
var OnBusy func(write bool)

// ReportBusy calls OnBusy.
func ReportBusy(write bool) {
	if f := OnBusy; f != nil {
		go f(write)
	}
}
