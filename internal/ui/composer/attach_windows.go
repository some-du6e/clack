//go:build windows

package composer

import (
	"context"
	"errors"

	"gioui.org/app/winclip"

	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// Windows clipboard paste, read the way Chromium's ClipboardWin does for
// Slack desktop: the registered "PNG" format when an app put one there (it
// keeps transparency), else the bitmap (CF_DIB / CF_DIBV5) as a PNG, and
// CF_HDROP (files copied in Explorer) as those files. Every read is one
// winclip.Read: one session, each format's block copied out within its
// size and a limit (winclip.CopyData), parsed after it closes. A paste
// reads them with its text in one snapshot (paste_snap_windows.go); these
// are the reads on their own. The pickers are the Gio explorer's native
// dialog or the in-app modal.

// Standard clipboard formats (winuser.h).
const (
	attCFDIB   = 8
	attCFHDROP = 15
	attCFDIBV5 = 17
)

func attExplorerUsable() bool { return true }

func attClipboardSupported() bool { return true }

func attNativePick(context.Context) ([]string, bool, error) { return nil, false, nil }

// attRead is winclip.Read off the UI goroutine, the busy error a toast's.
func attRead(ctx context.Context, want func(winclip.Format) int64) (*winclip.Snapshot, error) {
	uiguard.OffUI("a clipboard session")
	snap, err := winclip.Read(ctx, want)
	if errors.Is(err, winclip.ErrBusy) {
		return nil, errAttClipBusy
	}
	return snap, err
}

// attClipboardFiles returns the paths of files copied in Explorer (or any
// app that puts a CF_HDROP on the clipboard), nil when there are none.
func attClipboardFiles(ctx context.Context) ([]string, error) {
	snap, err := attRead(ctx, func(f winclip.Format) int64 {
		if f.ID == attCFHDROP {
			return 1 << 24
		}
		return 0
	})
	if err != nil {
		return nil, err
	}
	if b, ok := snap.Bytes(attCFHDROP); ok {
		return attParseHDROP(b), nil
	}
	return nil, nil
}

// attClipboardImage reads the clipboard's image as a PNG. Unless explicit,
// an office app's copy (rich text plus a rendered picture of it) isn't an
// image, as on Linux and macOS.
func attClipboardImage(ctx context.Context, explicit bool) ([]byte, string, error) {
	dib := false
	snap, err := attRead(ctx, func(f winclip.Format) int64 {
		switch {
		case f.Name == "PNG":
			return attMaxFileSize
		case (f.ID == attCFDIB || f.ID == attCFDIBV5) && !dib:
			dib = true // the one its owner placed; the other is Windows' conversion
			return attMaxFileSize
		}
		return 0
	})
	if err != nil {
		return nil, "", err
	}
	return snapImage(snap, explicit)
}
