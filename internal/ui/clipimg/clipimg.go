// Package clipimg puts an image on the system clipboard. Gio's clipboard
// carries text only, so the image goes through the platform: wl-copy or
// xclip on Linux (the tools the composer pastes images with), NSPasteboard
// on macOS, and user32 (CF_DIB plus the registered "PNG" format) on
// Windows. Write blocks (exec, or AppKit's main thread on macOS), so call
// it off the UI goroutine: it reports a UI-goroutine caller to uiguard.
package clipimg

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"

	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// ErrUnsupported means this system has no way to put an image on the
// clipboard (on Linux: neither wl-copy nor xclip is installed).
var ErrUnsupported = errors.New("copying images isn't supported on this system")

// Write replaces the clipboard with img.
func Write(ctx context.Context, img image.Image) error {
	uiguard.OffUI("clipimg.Write")
	if img == nil || img.Bounds().Empty() {
		return errors.New("empty image")
	}
	return write(ctx, img)
}

func encodePNG(img image.Image) ([]byte, error) {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
