//go:build windows

package clipimg

import (
	"context"
	"errors"
	"image"
	"unsafe"

	"gioui.org/app/winclip"
	"golang.org/x/sys/windows"
)

var (
	ciUser32                   = windows.NewLazySystemDLL("user32.dll")
	ciKernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	ciEmptyClipboard           = ciUser32.NewProc("EmptyClipboard")
	ciSetClipboardData         = ciUser32.NewProc("SetClipboardData")
	ciRegisterClipboardFormatW = ciUser32.NewProc("RegisterClipboardFormatW")
	ciGlobalAlloc              = ciKernel32.NewProc("GlobalAlloc")
	ciGlobalFree               = ciKernel32.NewProc("GlobalFree")
	ciGlobalLock               = ciKernel32.NewProc("GlobalLock")
	ciGlobalUnlock             = ciKernel32.NewProc("GlobalUnlock")
	ciRtlMoveMemory            = ciKernel32.NewProc("RtlMoveMemory")
)

const (
	cfDIBV5      = 17
	gmemMoveable = 0x0002
	gmemZeroInit = 0x0040
)

// winFormat is a clipboard format to set: its id and a moveable global
// block of its data (the caller's to free until the clipboard takes it);
// bitmap marks the image's own formats.
type winFormat struct {
	id, mem uintptr
	bitmap  bool
}

// write puts Chromium's ClipboardWin formats on the clipboard, in its
// order (chromium.go): "PNG" and CF_DIBV5, then with a tag "HTML Format"
// and "UniformResourceLocatorW".
func write(ctx context.Context, img image.Image, tag *Tag, charge func(int64) error) error {
	if ciUser32.Load() != nil || ciKernel32.Load() != nil {
		return ErrUnsupported
	}
	pngData, err := encodePNG(ctx, img, charge)
	if err != nil {
		return err
	}
	var fs []winFormat
	defer func() {
		for _, f := range fs {
			if f.mem != 0 {
				ciGlobalFree.Call(f.mem) // still ours: the clipboard didn't take it
			}
		}
	}()
	add := func(id, mem uintptr, bitmap bool) {
		if id != 0 && mem != 0 {
			fs = append(fs, winFormat{id, mem, bitmap})
		} else if mem != 0 {
			ciGlobalFree.Call(mem)
		}
	}
	// WriteBitmap: the PNG first (Word takes the first format it reads,
	// and the PNG keeps transparency), then the DIBV5
	pngMem, err := globalChunks(pngData.chunks, pngData.Len())
	if err != nil {
		return err
	}
	add(registerFormat("PNG"), pngMem, true)
	dib, err := newDIBV5(img, charge)
	if err != nil {
		return err
	}
	add(cfDIBV5, dib, true)
	if html := imageMarkup(tag); html != "" {
		cf := append(cfHTML(html), 0) // CreateGlobalData's NUL
		if err := charge(int64(len(cf))); err != nil {
			return err
		}
		m, err := globalBytes(cf)
		if err != nil {
			return err
		}
		add(registerFormat("HTML Format"), m, false)
	}
	if u := winURL(tag); u != nil {
		if err := charge(int64(len(u))); err != nil {
			return err
		}
		m, err := globalBytes(u)
		if err != nil {
			return err
		}
		add(registerFormat("UniformResourceLocatorW"), m, false)
	}
	// the process's one clipboard session at a time (winclip)
	return winclip.Session(ctx, 0, func() error {
		if r, _, _ := ciEmptyClipboard.Call(); r == 0 {
			return errors.New("couldn't empty the clipboard")
		}
		bitmap := false
		for i := range fs {
			if r, _, _ := ciSetClipboardData.Call(fs[i].id, fs[i].mem); r != 0 {
				fs[i].mem = 0 // the clipboard's now
				bitmap = bitmap || fs[i].bitmap
			}
		}
		if !bitmap {
			return errors.New("the clipboard refused the image")
		}
		return nil
	})
}

// registerFormat is the id of the registered clipboard format name (0 when
// it can't be registered).
func registerFormat(name string) uintptr {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0
	}
	id, _, _ := ciRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(p)))
	return id
}

// globalBytes is a moveable global block holding a copy of b.
func globalBytes(b []byte) (uintptr, error) {
	return globalChunks([][]byte{b}, len(b))
}

// globalChunks is a moveable global block holding the chunks, n bytes in
// all, one after another.
func globalChunks(chunks [][]byte, n int) (uintptr, error) {
	h, _, _ := ciGlobalAlloc.Call(gmemMoveable, uintptr(max(n, 1)))
	if h == 0 {
		return 0, errors.New("out of memory")
	}
	p, _, _ := ciGlobalLock.Call(h)
	if p == 0 {
		ciGlobalFree.Call(h)
		return 0, errors.New("couldn't lock the clipboard memory")
	}
	o := uintptr(0)
	for _, c := range chunks {
		if len(c) > 0 {
			ciRtlMoveMemory.Call(p+o, uintptr(unsafe.Pointer(&c[0])), uintptr(len(c)))
			o += uintptr(len(c))
		}
	}
	ciGlobalUnlock.Call(h)
	return h, nil
}

// newDIBV5 is img as skia::CreateDIBV5ImageDataFromN32SkBitmap makes it: a
// zeroed (GHND) moveable global block with dibV5Header and img's rows
// bottom-up, premultiplied BGRA (premulBGRARow). It's written a row at a
// time from one row of Go memory (charged), never as a whole in Go's heap.
func newDIBV5(img image.Image, charge func(int64) error) (uintptr, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if err := charge(int64(dibV5HeaderSize) + 4*int64(w)); err != nil {
		return 0, err
	}
	hmem, _, _ := ciGlobalAlloc.Call(gmemMoveable|gmemZeroInit, uintptr(dibV5HeaderSize+w*h*4))
	if hmem == 0 {
		return 0, errors.New("out of memory")
	}
	p, _, _ := ciGlobalLock.Call(hmem)
	if p == 0 {
		ciGlobalFree.Call(hmem)
		return 0, errors.New("couldn't lock the clipboard memory")
	}
	defer ciGlobalUnlock.Call(hmem)
	hdr := dibV5Header(w, h)
	ciRtlMoveMemory.Call(p, uintptr(unsafe.Pointer(&hdr[0])), dibV5HeaderSize)
	row := make([]byte, 4*w)
	for y := 0; y < h; y++ {
		premulBGRARow(row, img, b.Min.Y+y)
		ciRtlMoveMemory.Call(p+uintptr(dibV5HeaderSize+(h-1-y)*w*4), uintptr(unsafe.Pointer(&row[0])), uintptr(4*w))
	}
	return hmem, nil
}
