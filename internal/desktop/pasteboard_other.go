//go:build !darwin || !cgo

package desktop

import (
	"context"
	"errors"
)

// The pasteboard reads' errors (pasteboard_darwin.go has the same).
var (
	ErrPasteboardNoImage     = errors.New("no image on the pasteboard")
	ErrPasteboardTooBig      = errors.New("pasteboard data over the limit")
	ErrPasteboardBusy        = errors.New("the main thread didn't answer the pasteboard read")
	ErrPasteboardUnsupported = errors.New("no pasteboard on this system")
)

// PasteboardTypes is macOS' general pasteboard; elsewhere there's none.
func PasteboardTypes(context.Context) ([]string, error) { return nil, ErrPasteboardUnsupported }

// PasteboardFiles is macOS' general pasteboard; elsewhere there's none.
func PasteboardFiles(context.Context) ([]string, error) { return nil, ErrPasteboardUnsupported }

// PasteboardPNG is macOS' general pasteboard; elsewhere there's none.
func PasteboardPNG(context.Context, int64) ([]byte, error) { return nil, ErrPasteboardUnsupported }
