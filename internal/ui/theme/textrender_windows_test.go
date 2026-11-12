//go:build windows

package theme

import (
	"image"
	"image/color"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

// TestD3D11WindowSRGB opens a real Gio window (the D3D11 swapchain Clack
// draws into on Windows: app/d3d11_windows.go, DXGI_FORMAT_R8G8B8A8_UNORM_SRGB
// with a render target view inheriting it) and reads its pixels back from
// the screen through GDI. Half-alpha white over black must come out ≈188
// (blended in linear light, encoded to sRGB on write), not 128 (a non-sRGB
// target, which would wash every edge and translucent colour out); opaque
// #404040 must stay 64. Skipped when the runner's desktop can't be read
// (the capture comes back black).
func TestD3D11WindowSRGB(t *testing.T) {
	if os.Getenv("CLACK_TEXT_RENDER_DIR") == "" {
		t.Skip("CLACK_TEXT_RENDER_DIR not set (font-shots' text-render-windows job sets it)")
	}
	hwndc := make(chan uintptr, 1)
	frames := make(chan image.Point, 64)
	w := new(app.Window)
	w.Option(app.Title("clack-srgb-probe"), app.Size(unit.Dp(320), unit.Dp(160)))
	done := make(chan struct{})
	go func() {
		defer close(done)
		var ops op.Ops
		for {
			switch e := w.Event().(type) {
			case app.Win32ViewEvent:
				if e.HWND != 0 {
					select {
					case hwndc <- e.HWND:
					default:
					}
				}
			case app.FrameEvent:
				ops.Reset()
				sz := e.Size
				paint.FillShape(&ops, color.NRGBA{A: 0xff}, clip.Rect{Max: sz}.Op())
				paint.FillShape(&ops, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x80}, clip.Rect{Max: image.Pt(sz.X/2, sz.Y)}.Op())
				paint.FillShape(&ops, color.NRGBA{R: 0x40, G: 0x40, B: 0x40, A: 0xff}, clip.Rect{Min: image.Pt(sz.X/2, 0), Max: sz}.Op())
				e.Frame(&ops)
				select {
				case frames <- sz:
				default:
				}
			case app.DestroyEvent:
				return
			}
		}
	}()
	defer func() {
		w.Perform(system.ActionClose)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}()
	var hwnd uintptr
	select {
	case hwnd = <-hwndc:
	case <-time.After(30 * time.Second):
		t.Skip("no Win32ViewEvent in 30 s (no desktop on this runner?)")
	}
	var sz image.Point
	for i := 0; i < 2; i++ {
		select {
		case sz = <-frames:
		case <-time.After(30 * time.Second):
			t.Skip("no frames in 30 s")
		}
	}
	user32, gdi32 := syscall.NewLazyDLL("user32.dll"), syscall.NewLazyDLL("gdi32.dll")
	call := func(dll *syscall.LazyDLL, name string, args ...uintptr) uintptr {
		r, _, _ := dll.NewProc(name).Call(args...)
		return r
	}
	call(user32, "SetForegroundWindow", hwnd)
	time.Sleep(2 * time.Second) // let DWM compose the presented frame
	var org struct{ X, Y int32 }
	call(user32, "ClientToScreen", hwnd, uintptr(unsafe.Pointer(&org)))
	var rc struct{ L, T, R, B int32 }
	call(user32, "GetClientRect", hwnd, uintptr(unsafe.Pointer(&rc)))
	cw, ch := int(rc.R-rc.L), int(rc.B-rc.T)
	t.Logf("window %#x: client %dx%d at (%d,%d), last frame %v", hwnd, cw, ch, org.X, org.Y, sz)
	if cw <= 4 || ch <= 4 {
		t.Skipf("client area %dx%d", cw, ch)
	}
	sdc := call(user32, "GetDC", 0)
	defer call(user32, "ReleaseDC", 0, sdc)
	mdc := call(gdi32, "CreateCompatibleDC", sdc)
	defer call(gdi32, "DeleteDC", mdc)
	bmp := call(gdi32, "CreateCompatibleBitmap", sdc, uintptr(cw), uintptr(ch))
	defer call(gdi32, "DeleteObject", bmp)
	call(gdi32, "SelectObject", mdc, bmp)
	const srccopy = 0x00CC0020
	call(gdi32, "BitBlt", mdc, 0, 0, uintptr(cw), uintptr(ch), sdc, uintptr(org.X), uintptr(org.Y), srccopy)
	type bitmapInfoHeader struct {
		Size                         uint32
		Width, Height                int32
		Planes, BitCount             uint16
		Compression, SizeImage       uint32
		XPelsPerMeter, YPelsPerMeter int32
		ClrUsed, ClrImportant        uint32
		_                            [4]byte // bmiColors (unused for 32-bit BI_RGB)
	}
	bi := bitmapInfoHeader{Size: 40, Width: int32(cw), Height: -int32(ch), Planes: 1, BitCount: 32}
	buf := make([]byte, cw*ch*4)
	if n := call(gdi32, "GetDIBits", mdc, bmp, 0, uintptr(ch), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), 0); n == 0 {
		t.Skip("GetDIBits failed")
	}
	px := func(x, y int) [3]int { i := (y*cw + x) * 4; return [3]int{int(buf[i+2]), int(buf[i+1]), int(buf[i])} } // BGRA
	half, solid := px(cw/4, ch/2), px(3*cw/4, ch/2)
	t.Logf("half-alpha white over black: %v; opaque #404040: %v", half, solid)
	if half == ([3]int{}) && solid == ([3]int{}) {
		t.Skip("the screen capture is black (desktop not readable on this runner)")
	}
	if half[0] < 180 || half[0] > 196 {
		t.Errorf("half-alpha white over black on screen = %v, want ≈188 (sRGB swapchain, linear blending); 128 means a non-sRGB target", half)
	}
	if solid[0] < 61 || solid[0] > 67 {
		t.Errorf("opaque #404040 on screen = %v, want 64", solid)
	}
}
