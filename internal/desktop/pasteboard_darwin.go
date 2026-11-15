//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework ImageIO -framework CoreGraphics
#include <stdlib.h>
void *clack_pboard_types(long ms, long *n, int *status);
void *clack_pboard_files(long ms, long *n, int *status);
void *clack_pboard_image(long ms, long max, long *n, int *status, int *png, void **img);
void *clack_pboard_encode(void *img, long max, long *n, int *status);
void clack_pboard_release(void *img);
*/
import "C"

import (
	"bytes"
	"context"
	"errors"
	"time"
	"unsafe"
)

// macOS general pasteboard reads for the composer's paste (⌘V): what
// Chromium's ClipboardMac gives Slack's paste handler. pasteboard_darwin.m
// does the reading on the main thread without ever blocking it.

// The pasteboard reads' errors (pasteboard_other.go has the same).
var (
	ErrPasteboardNoImage     = errors.New("no image on the pasteboard")
	ErrPasteboardTooBig      = errors.New("pasteboard data over the limit")
	ErrPasteboardBusy        = errors.New("the main thread didn't answer the pasteboard read")
	ErrPasteboardUnsupported = errors.New("no pasteboard on this system")
)

// pbWait is the longest one read waits for the main thread.
const pbWait = time.Second

// pbStatus is pasteboard_darwin.m's CLACK_PB_*.
const (
	pbOK = iota
	pbNone
	pbTooBig
	pbBusy
)

func pbErr(status C.int, none error) error {
	switch status {
	case pbOK:
		return nil
	case pbTooBig:
		return ErrPasteboardTooBig
	case pbBusy:
		return ErrPasteboardBusy
	}
	return none
}

// pbWaitMS is how long a read waits: pbWait, or less when ctx ends sooner.
func pbWaitMS(ctx context.Context) C.long {
	d := pbWait
	if dl, ok := ctx.Deadline(); ok {
		d = min(d, time.Until(dl))
	}
	return C.long(max(d.Milliseconds(), 1))
}

// pbTake copies a malloc'd result into Go memory and frees it.
func pbTake(p unsafe.Pointer, n C.long) []byte {
	if p == nil {
		return nil
	}
	defer C.free(p)
	if n <= 0 {
		return nil
	}
	return C.GoBytes(p, C.int(n))
}

func pbStrings(b []byte) []string {
	var ss []string
	for _, s := range bytes.Split(b, []byte{0}) {
		if len(s) > 0 {
			ss = append(ss, string(s))
		}
	}
	return ss
}

// PasteboardTypes lists the general pasteboard's types (UTIs such as
// public.png, public.rtf, public.file-url), in the order the owner put them.
func PasteboardTypes(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var n C.long
	var st C.int
	b := pbTake(C.clack_pboard_types(pbWaitMS(ctx), &n, &st), n)
	if err := pbErr(st, nil); err != nil {
		return nil, err
	}
	return pbStrings(b), nil
}

// PasteboardFiles returns the paths of the files on the general pasteboard
// (each item's public.file-url: files copied in Finder), nil when there
// are none. Chromium's ReadFilenames, which Slack's paste gets as the
// paste's files.
func PasteboardFiles(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var n C.long
	var st C.int
	b := pbTake(C.clack_pboard_files(pbWaitMS(ctx), &n, &st), n)
	if err := pbErr(st, nil); err != nil {
		return nil, err
	}
	return pbStrings(b), nil
}

// PasteboardPNG returns the general pasteboard's image as PNG, as
// Chromium's ReadPng: public.png's bytes as they are, else whatever image
// NSImage reads from the pasteboard (TIFF, JPEG, PDF…) encoded as PNG.
// ErrPasteboardNoImage when there's none, ErrPasteboardTooBig past limit
// bytes. The encode runs on the calling goroutine.
func PasteboardPNG(ctx context.Context, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var n C.long
	var st, png C.int
	var img unsafe.Pointer
	b := pbTake(C.clack_pboard_image(pbWaitMS(ctx), C.long(limit), &n, &st, &png, &img), n)
	if err := pbErr(st, ErrPasteboardNoImage); err != nil {
		C.clack_pboard_release(img) // nil: a CGImage comes only with pbOK
		return nil, err
	}
	if png != 0 {
		return b, nil
	}
	if img == nil {
		return nil, ErrPasteboardNoImage
	}
	if err := ctx.Err(); err != nil {
		C.clack_pboard_release(img)
		return nil, err
	}
	b = pbTake(C.clack_pboard_encode(img, C.long(limit), &n, &st), n) // releases img
	if err := pbErr(st, ErrPasteboardNoImage); err != nil {
		return nil, err
	}
	return b, nil
}
