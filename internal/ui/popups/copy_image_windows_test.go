//go:build windows

package popups

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var (
	ctUser32                    = windows.NewLazySystemDLL("user32.dll")
	ctKernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	ctOpenClipboard             = ctUser32.NewProc("OpenClipboard")
	ctCloseClipboard            = ctUser32.NewProc("CloseClipboard")
	ctEmptyClipboard            = ctUser32.NewProc("EmptyClipboard")
	ctGetClipboardData          = ctUser32.NewProc("GetClipboardData")
	ctIsFormatAvailable         = ctUser32.NewProc("IsClipboardFormatAvailable")
	ctRegisterFormat            = ctUser32.NewProc("RegisterClipboardFormatW")
	ctGlobalLock                = ctKernel32.NewProc("GlobalLock")
	ctGlobalUnlock              = ctKernel32.NewProc("GlobalUnlock")
	ctGlobalSize                = ctKernel32.NewProc("GlobalSize")
	ctRtlMoveMemory             = ctKernel32.NewProc("RtlMoveMemory")
	ctCFDIB             uintptr = 8
)

// ctOpen opens the clipboard for this (locked) thread, retrying while
// another process holds it.
func ctOpen(t *testing.T) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if r, _, _ := ctOpenClipboard.Call(0); r != 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the clipboard stayed busy")
}

// ctRead is clipboard format f's bytes (nil when absent).
func ctRead(t *testing.T, f uintptr) []byte {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctOpen(t)
	defer ctCloseClipboard.Call()
	h, _, _ := ctGetClipboardData.Call(f)
	if h == 0 {
		return nil
	}
	n, _, _ := ctGlobalSize.Call(h)
	p, _, _ := ctGlobalLock.Call(h)
	if p == 0 || n == 0 {
		return nil
	}
	defer ctGlobalUnlock.Call(h)
	b := make([]byte, n)
	ctRtlMoveMemory.Call(uintptr(unsafe.Pointer(&b[0])), p, n)
	return b
}

func ctAvailable(f uintptr) bool {
	r, _, _ := ctIsFormatAvailable.Call(f)
	return r != 0
}

// The real clipboard (CI's windows-applog job): the image menu's Copy
// Image on a fakeslack image puts its pixels there as Slack's desktop
// app does (webContents.copyImageAt: a bitmap), CF_DIB plus the "PNG"
// format, and both read back as the image.
func TestWinCopyImageRoundTrip(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_CLIPBOARD_TESTS") == "" {
		t.Skip("replaces the clipboard: set CLACK_WINDOWS_CLIPBOARD_TESTS=1")
	}
	name, _ := windows.UTF16PtrFromString("PNG")
	cfPNG, _, _ := ctRegisterFormat.Call(uintptr(unsafe.Pointer(name)))
	if cfPNG == 0 {
		t.Fatal("couldn't register the PNG format")
	}
	func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		ctOpen(t)
		ctEmptyClipboard.Call()
		ctCloseClipboard.Call()
	}()

	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(""))
	h := &tzHost{a: a, th: theme.New(false)}
	src := fs.URL + "img/64x48/" + fakeslack.TeamID + "/party/00000000000000ab.png"
	want := fetchPNG(t, src)

	NewImageMenu(h, image.Pt(10, 10), src, true).Items()[0].Do()
	deadline := time.Now().Add(20 * time.Second)
	for !ctAvailable(cfPNG) || !ctAvailable(ctCFDIB) {
		Pump()
		if ts := h.toastList(); len(ts) > 0 {
			t.Fatalf("Copy Image toasted %q", ts)
		}
		if time.Now().After(deadline) {
			t.Fatal("Copy Image put no image on the clipboard")
		}
		time.Sleep(20 * time.Millisecond)
	}

	got, err := png.Decode(bytes.NewReader(ctRead(t, cfPNG)))
	if err != nil {
		t.Fatalf("the clipboard's PNG: %v", err)
	}
	if got.Bounds().Size() != want.Bounds().Size() {
		t.Fatalf("the clipboard's PNG is %v, want %v", got.Bounds().Size(), want.Bounds().Size())
	}
	for _, p := range []image.Point{{0, 0}, {31, 20}, {63, 47}} {
		r1, g1, b1, a1 := got.At(p.X, p.Y).RGBA()
		r2, g2, b2, a2 := want.At(p.X, p.Y).RGBA()
		if r1>>8 != r2>>8 || g1>>8 != g2>>8 || b1>>8 != b2>>8 || a1>>8 != a2>>8 {
			t.Errorf("PNG pixel %v is %v, want %v", p, got.At(p.X, p.Y), want.At(p.X, p.Y))
		}
	}

	dib := ctRead(t, ctCFDIB)
	if len(dib) < 40 {
		t.Fatalf("CF_DIB is %d bytes", len(dib))
	}
	le := binary.LittleEndian
	w, ht, bpp := int32(le.Uint32(dib[4:])), int32(le.Uint32(dib[8:])), le.Uint16(dib[14:])
	if w != 64 || (ht != 48 && ht != -48) || bpp != 32 {
		t.Fatalf("CF_DIB %dx%d at %d bpp, want 64x48 at 32", w, ht, bpp)
	}
	hdr := int(le.Uint32(dib[0:]))
	row := 0 // the image's top row: the last in a bottom-up DIB
	if ht > 0 {
		row = int(ht) - 1
	}
	off := hdr + row*int(w)*4
	if len(dib) < off+4 {
		t.Fatalf("CF_DIB is %d bytes, short of its pixels", len(dib))
	}
	r, g, b, _ := want.At(0, 0).RGBA()
	if dib[off+2] != byte(r>>8) || dib[off+1] != byte(g>>8) || dib[off] != byte(b>>8) {
		t.Errorf("CF_DIB top-left pixel BGR %v, want %d %d %d", dib[off:off+3], b>>8, g>>8, r>>8)
	}
}
