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

func write(ctx context.Context, img image.Image) error {
	if ciUser32.Load() != nil || ciKernel32.Load() != nil {
		return ErrUnsupported
	}
	pngData, err := encodePNG(img)
	if err != nil {
		return err
	}
	dib := toDIB(img)
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
	if err := setData(cfDIB, dib); err != nil {
		return err
	}
	// Apps that keep transparency (browsers, Office) read "PNG".
	if name, err := windows.UTF16PtrFromString("PNG"); err == nil {
		if f, _, _ := ciRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name))); f != 0 {
			_ = setData(f, pngData)
		}
	}
	return nil
}

// setData hands a copy of data to the clipboard as format f.
func setData(f uintptr, data []byte) error {
	h, _, _ := ciGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return errors.New("out of memory")
	}
	p, _, _ := ciGlobalLock.Call(h)
	if p == 0 {
		ciGlobalFree.Call(h)
		return errors.New("couldn't lock the clipboard memory")
	}
	ciRtlMoveMemory.Call(p, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	ciGlobalUnlock.Call(h)
	if r, _, _ := ciSetClipboardData.Call(f, h); r == 0 {
		ciGlobalFree.Call(h) // still ours when SetClipboardData fails
		return errors.New("the clipboard refused the image")
	}
	return nil
}

// toDIB is img as a CF_DIB: a BITMAPINFOHEADER and 32-bit bottom-up BGR
// rows, composited onto white (CF_DIB readers ignore alpha).
func toDIB(img image.Image) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]byte, dibHeaderSize+w*h*4)
	le := binary.LittleEndian
	le.PutUint32(out[0:], dibHeaderSize)
	le.PutUint32(out[4:], uint32(w))
	le.PutUint32(out[8:], uint32(h)) // positive: bottom-up
	le.PutUint16(out[12:], 1)        // planes
	le.PutUint16(out[14:], 32)       // bits per pixel
	le.PutUint32(out[16:], biRGB)
	le.PutUint32(out[20:], uint32(w*h*4))
	px := out[dibHeaderSize:]
	for y := 0; y < h; y++ {
		row := px[(h-1-y)*w*4:]
		for x := 0; x < w; x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA() // premultiplied, 16-bit
			over := func(c uint32) byte { return byte((c + (0xffff - a)) >> 8) }
			row[x*4+0] = over(bl)
			row[x*4+1] = over(g)
			row[x*4+2] = over(r)
			row[x*4+3] = 0xff
		}
	}
	return out
}
