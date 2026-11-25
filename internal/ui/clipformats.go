package ui

import (
	"bytes"
	"context"
	"io"

	"gioui.org/io/clipboard"
	"gioui.org/layout"
)

// Copy Image's clipboard on Linux (clipimg.SetOwner): Electron's Chromium
// owns the clipboard from its window and offers every format of a copy
// at once (image/png, text/html, text/x-moz-url), so Clack's window
// offers clipimg's formats through Gio (a clipboard.FormatsType write:
// Gio patch 69), in the frame after they arrive, as copyText's text is
// written. Windows and macOS write theirs natively; their windows never
// get any.

// offerClipboardFormats is clipimg's Owner: it gives the window formats
// (clipboard.EncodeFormats) for its next frame and returns once that
// frame wrote them, or ctx is done (then they aren't written). A newer
// copy replaces formats still waiting.
func (a *App) offerClipboardFormats(ctx context.Context, b []byte) error {
	done := make(chan struct{})
	a.clipMu.Lock()
	if a.clipFormatsDone != nil {
		close(a.clipFormatsDone) // replaced by this copy, as a later copy replaces an earlier one
	}
	a.clipFormats, a.clipFormatsDone = b, done
	a.clipMu.Unlock()
	a.Invalidate()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
	}
	a.clipMu.Lock()
	defer a.clipMu.Unlock()
	select {
	case <-done: // the frame took them meanwhile
		return nil
	default:
	}
	if a.clipFormatsDone == done {
		a.clipFormats, a.clipFormatsDone = nil, nil
	}
	return ctx.Err()
}

// writeClipboardFormats writes the formats waiting, if any (the frame's
// key processing, beside copyText's text).
func (a *App) writeClipboardFormats(gtx layout.Context) {
	a.clipMu.Lock()
	b, done := a.clipFormats, a.clipFormatsDone
	a.clipFormats, a.clipFormatsDone = nil, nil
	a.clipMu.Unlock()
	if done == nil {
		return
	}
	gtx.Execute(clipboard.WriteCmd{Type: clipboard.FormatsType, Data: io.NopCloser(bytes.NewReader(b))})
	close(done)
}
