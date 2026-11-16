//go:build windows

package clipimg

import (
	"context"
	"encoding/binary"
	"errors"
	"image"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ciUser32                   = windows.NewLazySystemDLL("user32.dll")
	ciKernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	ciOpenClipboard            = ciUser32.NewProc("OpenClipboard")
	ciCloseClipboard           = ciUser32.NewProc("CloseClipboard")
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
	cfDIB         = 8
	gmemMoveable  = 0x0002
	biRGB         = 0
	dibHeaderSize = 40
)

func write(ctx context.Context, img image.Image, charge func(int64) error) error {
	if ciUser32.Load() != nil || ciKernel32.Load() != nil {
		return ErrUnsupported
	}
	pngData, err := encodePNG(ctx, img, charge)
	if err != nil {
		return err
	}
	dib, err := newDIB(img, charge)
	if err != nil {
		return err
	}
	owned := false // the clipboard took dib
	defer func() {
		if !owned {
			ciGlobalFree.Call(dib)
		}
	}()
	// The clipboard belongs to the thread that opened it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for i := 0; ; i++ {
		if r, _, _ := ciOpenClipboard.Call(0); r != 0 {
			break
		}
		if i == 20 || ctx.Err() != nil {
			return errors.New("the clipboard is busy")
		}
		time.Sleep(5 * time.Millisecond)
	}
	defer ciCloseClipboard.Call()
	if r, _, _ := ciEmptyClipboard.Call(); r == 0 {
		return errors.New("couldn't empty the clipboard")
	}
	if r, _, _ := ciSetClipboardData.Call(cfDIB, dib); r == 0 {
		return errors.New("the clipboard refused the image")
	}
	owned = true
	// Apps that keep transparency (browsers, Office) read "PNG".
	if name, err := windows.UTF16PtrFromString("PNG"); err == nil {
		if f, _, _ := ciRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name))); f != 0 {
			_ = setData(f, pngData)
		}
	}
	return nil
}

// setData hands a copy of data to the clipboard as format f.
func setData(f uintptr, data *pngData) error {
	h, _, _ := ciGlobalAlloc.Call(gmemMoveable, uintptr(data.Len()))
	if h == 0 {
		return errors.New("out of memory")
	}
	p, _, _ := ciGlobalLock.Call(h)
	if p == 0 {
		ciGlobalFree.Call(h)
		return errors.New("couldn't lock the clipboard memory")
	}
	o := uintptr(0)
	for _, c := range data.chunks {
		if len(c) > 0 {
			ciRtlMoveMemory.Call(p+o, uintptr(unsafe.Pointer(&c[0])), uintptr(len(c)))
			o += uintptr(len(c))
		}
	}
	ciGlobalUnlock.Call(h)
	if r, _, _ := ciSetClipboardData.Call(f, h); r == 0 {
		ciGlobalFree.Call(h) // still ours when SetClipboardData fails
		return errors.New("the clipboard refused the image")
	}
	return nil
}

// newDIB is img as a CF_DIB in a moveable global block for
// SetClipboardData (the caller's to free until the clipboard takes it): a
// BITMAPINFOHEADER and 32-bit bottom-up BGR rows, composited onto white
// (CF_DIB readers ignore alpha). It's written a row at a time from one row
// of Go memory (charged), never as a whole in Go's heap.
func newDIB(img image.Image, charge func(int64) error) (uintptr, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if err := charge(int64(dibHeaderSize) + 4*int64(w)); err != nil {
		return 0, err
	}
	hmem, _, _ := ciGlobalAlloc.Call(gmemMoveable, uintptr(dibHeaderSize+w*h*4))
	if hmem == 0 {
		return 0, errors.New("out of memory")
	}
	p, _, _ := ciGlobalLock.Call(hmem)
	if p == 0 {
		ciGlobalFree.Call(hmem)
		return 0, errors.New("couldn't lock the clipboard memory")
	}
	defer ciGlobalUnlock.Call(hmem)
	hdr := make([]byte, dibHeaderSize)
	le := binary.LittleEndian
	le.PutUint32(hdr[0:], dibHeaderSize)
	le.PutUint32(hdr[4:], uint32(w))
	le.PutUint32(hdr[8:], uint32(h)) // positive: bottom-up
	le.PutUint16(hdr[12:], 1)        // planes
	le.PutUint16(hdr[14:], 32)       // bits per pixel
	le.PutUint32(hdr[16:], biRGB)
	le.PutUint32(hdr[20:], uint32(w*h*4))
	ciRtlMoveMemory.Call(p, uintptr(unsafe.Pointer(&hdr[0])), dibHeaderSize)
	row := make([]byte, 4*w)
	for y := 0; y < h; y++ {
		dibRow(row, img, b.Min.Y+y)
		ciRtlMoveMemory.Call(p+uintptr(dibHeaderSize+(h-1-y)*w*4), uintptr(unsafe.Pointer(&row[0])), uintptr(4*w))
	}
	return hmem, nil
}

// dibRow is row y of img as BGRX onto white, from its premultiplied 16-bit
// colours (RGBA64At where the image has it: At(x, y).RGBA() without a
// color.Color a pixel).
func dibRow(row []byte, img image.Image, y int) {
	b := img.Bounds()
	put := func(x int, r, g, bl, a uint32) {
		over := func(c uint32) byte { return byte((c + (0xffff - a)) >> 8) }
		row[x*4+0] = over(bl)
		row[x*4+1] = over(g)
		row[x*4+2] = over(r)
		row[x*4+3] = 0xff
	}
	if m, ok := img.(image.RGBA64Image); ok {
		for x := 0; x < b.Dx(); x++ {
			c := m.RGBA64At(b.Min.X+x, y)
			put(x, uint32(c.R), uint32(c.G), uint32(c.B), uint32(c.A))
		}
		return
	}
	for x := 0; x < b.Dx(); x++ {
		r, g, bl, a := img.At(b.Min.X+x, y).RGBA() // premultiplied, 16-bit
		put(x, r, g, bl, a)
	}
}
