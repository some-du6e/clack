//go:build windows

package composer

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// Windows clipboard paste, read the way Chromium's ClipboardWin does for
// Slack desktop: the registered "PNG" format when an app put one there (it
// keeps transparency), else the bitmap (CF_DIB / CF_DIBV5) as a PNG, and
// CF_HDROP (files copied in Explorer) as those files. The pickers are the
// Gio explorer's native dialog or the in-app modal.

var (
	attUser32               = windows.NewLazySystemDLL("user32.dll")
	attKernel32             = windows.NewLazySystemDLL("kernel32.dll")
	attOpenClipboard        = attUser32.NewProc("OpenClipboard")
	attCloseClipboard       = attUser32.NewProc("CloseClipboard")
	attEnumClipboardFormats = attUser32.NewProc("EnumClipboardFormats")
	attGetClipboardData     = attUser32.NewProc("GetClipboardData")
	attGetClipFormatName    = attUser32.NewProc("GetClipboardFormatNameW")
	attGlobalLock           = attKernel32.NewProc("GlobalLock")
	attGlobalUnlock         = attKernel32.NewProc("GlobalUnlock")
	attGlobalSize           = attKernel32.NewProc("GlobalSize")
	attRtlMoveMemory        = attKernel32.NewProc("RtlMoveMemory")
)

// Standard clipboard formats (winuser.h).
const (
	attCFDIB   = 8
	attCFHDROP = 15
	attCFDIBV5 = 17
)

func attExplorerUsable() bool { return true }

func attClipboardSupported() bool { return true }

func attNativePick(context.Context) ([]string, bool, error) { return nil, false, nil }

// attWinFormat is one format on the clipboard; name is set for registered
// formats ("PNG", "Rich Text Format", "HTML Format").
type attWinFormat struct {
	id   uintptr
	name string
}

// attWithClipboard opens the clipboard for f. Another app (or Gio reading
// the text of the same Ctrl+V) may hold it for a moment, so opening
// retries for up to half a second.
func attWithClipboard(ctx context.Context, f func() error) error {
	uiguard.OffUI("OpenClipboard")
	if attUser32.Load() != nil || attKernel32.Load() != nil {
		return errAttClipNone
	}
	// The clipboard belongs to the thread that opened it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for i := 0; ; i++ {
		if r, _, _ := attOpenClipboard.Call(0); r != 0 {
			break
		}
		if i == 50 || ctx.Err() != nil {
			return errors.New("The clipboard is busy. Try pasting again.")
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer attCloseClipboard.Call()
	return f()
}

// attClipFormats lists the clipboard's formats in the order the owner put
// them there (the ones Windows synthesizes come after). Call it with the
// clipboard open.
func attClipFormats() []attWinFormat {
	var fs []attWinFormat
	var f uintptr
	for {
		f, _, _ = attEnumClipboardFormats.Call(f)
		if f == 0 {
			return fs
		}
		x := attWinFormat{id: f}
		if f >= 0xc000 {
			var buf [256]uint16
			if n, _, _ := attGetClipFormatName.Call(f, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); n > 0 {
				x.name = windows.UTF16ToString(buf[:n])
			}
		}
		fs = append(fs, x)
	}
}

// attClipBytes copies format f's global memory out of the clipboard, at
// most limit bytes (errAttTooBig past that). Call it with the clipboard
// open.
func attClipBytes(f uintptr, limit int64) ([]byte, error) {
	h, _, _ := attGetClipboardData.Call(f)
	if h == 0 {
		return nil, errAttNoClipImage
	}
	n, _, _ := attGlobalSize.Call(h)
	if n == 0 {
		return nil, errAttNoClipImage
	}
	if int64(n) > limit {
		return nil, errAttTooBig
	}
	p, _, _ := attGlobalLock.Call(h)
	if p == 0 {
		return nil, errAttNoClipImage
	}
	defer attGlobalUnlock.Call(h)
	// Copy the locked block out before unlocking it (RtlMoveMemory, so no
	// uintptr becomes a Go pointer).
	b := make([]byte, n)
	attRtlMoveMemory.Call(uintptr(unsafe.Pointer(&b[0])), p, n)
	return b, nil
}

// attClipboardFiles returns the paths of files copied in Explorer (or any
// app that puts a CF_HDROP on the clipboard), nil when there are none.
func attClipboardFiles(ctx context.Context) ([]string, error) {
	var paths []string
	err := attWithClipboard(ctx, func() error {
		for _, f := range attClipFormats() {
			if f.id == attCFHDROP {
				b, err := attClipBytes(attCFHDROP, 1<<24)
				if err != nil {
					return err
				}
				paths = attParseHDROP(b)
				return nil
			}
		}
		return nil
	})
	return paths, err
}

// attClipboardImage reads the clipboard's image as a PNG. Unless explicit,
// an office app's copy (rich text plus a rendered picture of it) isn't an
// image, as on Linux and macOS.
func attClipboardImage(ctx context.Context, explicit bool) ([]byte, string, error) {
	var data []byte
	var isPNG bool
	err := attWithClipboard(ctx, func() error {
		fs := attClipFormats()
		names := make([]string, 0, len(fs))
		var pngID, dibID uintptr
		for _, f := range fs {
			if f.name != "" {
				names = append(names, f.name)
			}
			switch {
			case f.name == "PNG" && pngID == 0:
				pngID = f.id
			case (f.id == attCFDIB || f.id == attCFDIBV5) && dibID == 0:
				// The one the app placed; the other is Windows' conversion.
				dibID = f.id
			}
		}
		if !explicit && attRichCopy(names) {
			return errAttNoClipImage
		}
		if pngID != 0 {
			if b, err := attClipBytes(pngID, attMaxFileSize); err == errAttTooBig {
				return err
			} else if err == nil && bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) {
				data, isPNG = b, true
				return nil
			}
		}
		if dibID == 0 {
			return errAttNoClipImage
		}
		b, err := attClipBytes(dibID, attMaxFileSize)
		data = b
		return err
	})
	if err != nil {
		return nil, "", err
	}
	if !isPNG {
		// Convert after closing the clipboard so other apps aren't kept
		// waiting.
		if data, err = attDIBToPNG(data); err != nil {
			return nil, "", err
		}
		if len(data) > attMaxFileSize {
			return nil, "", errAttTooBig
		}
	}
	return data, "image/png", nil
}
