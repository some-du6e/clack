// SPDX-License-Identifier: Unlicense OR MIT

// Package winclip runs a process's Windows clipboard sessions one at a
// time (Clack patch, CLACK_PATCHES.md §55).
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
