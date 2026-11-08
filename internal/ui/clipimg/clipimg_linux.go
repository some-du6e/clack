//go:build linux && !android

package clipimg

import (
	"bytes"
	"context"
	"image"
	"os"
	"os/exec"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/uiguard"
)

func write(ctx context.Context, img image.Image) error {
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
	data, err := encodePNG(img)
	if err != nil {
		return err
	}
	// Both tools fork to serve the clipboard and the parent exits; no
	// stdout pipe is attached, so Run doesn't wait for the child.
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Stdin = bytes.NewReader(data)
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
