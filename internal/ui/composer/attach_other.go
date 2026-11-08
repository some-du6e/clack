//go:build !linux || android

package composer

import (
	"context"
	"os/exec"
	"runtime"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/uiguard"
)

// macOS: clipboard images via pngpaste (if installed) or osascript, and an
// osascript "choose file" picker when the ui root set no explorer.
// Windows and the rest: no clipboard images yet; the picker is the explorer
// or the in-app modal.

// attExplorerUsable reports whether the Gio explorer's chooser is safe to
// call. macOS and Windows use native panels that return promptly, so the
// explorer is always usable there (the portal-hang problem is Linux only).
func attExplorerUsable() bool { return true }

func attClipboardSupported() bool { return runtime.GOOS == "darwin" }

func attClipboardImage(ctx context.Context, explicit bool) ([]byte, string, error) {
	if runtime.GOOS != "darwin" {
		return nil, "", errAttClipNone
	}
	info, err := attRun(ctx, 1<<20, "osascript", "-e", "clipboard info")
	if err != nil || !attMacClipHasImage(string(info), explicit) {
		return nil, "", errAttNoClipImage
	}
	if attHave("pngpaste") {
		if data, err := attRun(ctx, attMaxFileSize, "pngpaste", "-"); err == nil && len(data) > 0 {
			return data, "image/png", nil
		}
	}
	out, err := attRun(ctx, 2*attMaxFileSize+64, "osascript", "-e", "the clipboard as «class PNGf»")
	if err != nil {
		return nil, "", errAttNoClipImage
	}
	data, err := attParseAppleData(string(out))
	if err != nil {
		return nil, "", err
	}
	return data, "image/png", nil
}

const attMacChooseScript = `set fs to choose file with prompt "Upload files" with multiple selections allowed
set out to ""
repeat with f in fs
set out to out & POSIX path of f & linefeed
end repeat
return out`

func attNativePick(ctx context.Context) (paths []string, ok bool, err error) {
	uiguard.OffUI("exec osascript")
	if runtime.GOOS != "darwin" || !attHave("osascript") {
		return nil, false, nil
	}
	out, err := exec.CommandContext(ctx, "osascript", "-e", attMacChooseScript).Output()
	return attPickResult(out, err)
}
