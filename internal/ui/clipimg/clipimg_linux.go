//go:build linux && !android

package clipimg

import (
	"context"
	"image"
	"os"
	"os/exec"
	"time"

	"gioui.org/io/clipboard"

	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// On Linux Electron's Chromium owns the clipboard from its own window
// (ClipboardOzone) and offers every format of a copy at once: image/png,
// text/html and text/x-moz-url for Copy Image (chromium.go). Clack's window
// does the same through Gio (a clipboard.FormatsType write: Gio patch 68)
// once the ui root has given clipimg its owner (SetOwner). Without one (no
// window: tools, tests) the image goes to wl-copy or xclip, which offer
// one type each: image/png alone.

func write(ctx context.Context, img image.Image, tag *Tag, charge func(int64) error) error {
	if p := owner.Load(); p != nil {
		o := *p
		data, err := encodePNG(ctx, img, charge)
		if err != nil {
			return err
		}
		fs := []clipboard.Format{{Type: "image/png"}}
		if m := imageMarkup(tag); m != "" {
			fs = append(fs, clipboard.Format{Type: "text/html", Data: []byte(m)})
		}
		if u := mozURL(tag); u != nil {
			fs = append(fs, clipboard.Format{Type: "text/x-moz-url", Data: u})
		}
		// the formats in one buffer for the window, the PNG copied in
		// (charged: it's one more copy of the image in Go's heap)
		n := int64(data.Len())
		for _, f := range fs[1:] {
			n += int64(len(f.Data))
		}
		if err := charge(2*n + 64); err != nil {
			return err
		}
		png := make([]byte, data.Len())
		data.copyTo(png)
		fs[0].Data = png
		return o(ctx, clipboard.EncodeFormats(fs))
	}
	uiguard.OffUI("exec wl-copy/xclip (copy image)")
	var args []string
	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "" && have("wl-copy"):
		args = []string{"wl-copy", "--type", "image/png"}
	case os.Getenv("DISPLAY") != "" && have("xclip"):
		args = []string{"xclip", "-selection", "clipboard", "-t", "image/png", "-i"}
	default:
		return ErrUnsupported
	}
	data, err := encodePNG(ctx, img, charge)
	if err != nil {
		return err
	}
	// Both tools fork to serve the clipboard and the parent exits; no
	// stdout pipe is attached, so Run doesn't wait for the child.
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Stdin = data.reader()
	// The forked server child may keep our stdin pipe open: without
	// WaitDelay, Run could wait on it forever (TestEveryExecSetsWaitDelay).
	c.WaitDelay = 2 * time.Second
	return c.Run()
}

func have(name string) bool {
	uiguard.OffUI("exec.LookPath " + name)
	_, err := exec.LookPath(name)
	return err == nil
}
