//go:build darwin && !ios

package composer

import (
	"context"
	"errors"

	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// macOS paste reads the general pasteboard natively (internal/desktop),
// what Chromium's ClipboardMac hands Slack's paste handler: the files of
// Finder's ⌘C (each item's public.file-url), else the image (public.png as
// it is, else any image NSImage reads, as a PNG). The picker is the Gio
// explorer's NSOpenPanel, or osascript's "choose file" when the ui root
// set no explorer.

// attExplorerUsable reports whether the Gio explorer's chooser is safe to
// call. macOS uses native panels that return promptly, so the explorer is
// always usable there (the portal-hang problem is Linux only).
func attExplorerUsable() bool { return true }

func attClipboardSupported() bool { return true }

// attClipboardFiles returns the paths of the files copied in Finder (or
// any app that puts file URLs on the pasteboard), nil when there are none.
func attClipboardFiles(ctx context.Context) ([]string, error) {
	uiguard.OffUI("pasteboard files read") // waits for the main thread
	return desktop.PasteboardFiles(ctx)
}

// attClipboardImage reads the pasteboard's image as a PNG. Copied files
// aren't an image (Finder puts each file's icon next to its URL; Chromium's
// GetStandardFormats drops the image then). Unless explicit, an office
// app's copy (rich text plus a rendered picture of it) isn't one either, as
// on Linux and Windows.
func attClipboardImage(ctx context.Context, explicit bool) ([]byte, string, error) {
	uiguard.OffUI("pasteboard image read") // waits for the main thread
	types, err := desktop.PasteboardTypes(ctx)
	if err != nil {
		return nil, "", attMacPasteErr(err)
	}
	if attMacHasType(types, "public.file-url") || !explicit && attMacRichCopy(types) {
		return nil, "", errAttNoClipImage
	}
	data, err := desktop.PasteboardPNG(ctx, attMaxFileSize)
	if err != nil {
		return nil, "", attMacPasteErr(err)
	}
	return data, "image/png", nil
}

// attMacPasteErr maps internal/desktop's pasteboard errors to the paste's.
func attMacPasteErr(err error) error {
	switch {
	case errors.Is(err, desktop.ErrPasteboardTooBig):
		return errAttTooBig
	case errors.Is(err, desktop.ErrPasteboardBusy):
		return errAttClipBusy
	case errors.Is(err, desktop.ErrPasteboardUnsupported):
		return errAttClipNone
	}
	return errAttNoClipImage
}

// errAttClipBusy: the main thread was too busy to read the pasteboard.
var errAttClipBusy = errors.New("The clipboard is busy. Try pasting again.")

func attMacHasType(types []string, want string) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

// attMacRichCopy reports whether a pasteboard's types include rich text
// (public.rtf, RTFD; the old osascript check's «class RTF »), or a rich
// format attRichCopy knows by name.
func attMacRichCopy(types []string) bool {
	for _, t := range types {
		switch t {
		case "public.rtf", "com.apple.flat-rtfd", "com.apple.rtfd":
			return true
		}
	}
	return attRichCopy(types)
}

const attMacChooseScript = `set fs to choose file with prompt "Upload files" with multiple selections allowed
set out to ""
repeat with f in fs
set out to out & POSIX path of f & linefeed
end repeat
return out`

func attNativePick(ctx context.Context) (paths []string, ok bool, err error) {
	if !attHave("osascript") {
		return nil, false, nil
	}
	out, err := attRun(ctx, 1<<24, "osascript", "-e", attMacChooseScript)
	return attPickResult(out, err)
}
