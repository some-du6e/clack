//go:build windows

package composer

import (
	"context"
	"errors"

	"gioui.org/app/winclip"

	"github.com/some-du6e/clack/internal/ui/richclip"
)

// Windows: Gio reads only CF_UNICODETEXT; the HTML is the registered
// "HTML Format" (CF_HTML). A paste reads it with its text, files and
// picture in one session (paste_snap_windows.go); this is the read on its
// own, one winclip.Read too.

var errClipNoHTML = errors.New("no html on the clipboard")

func clipHTMLSupported() bool { return true }

func clipReadHTML(ctx context.Context) ([]byte, error) {
	snap, err := winclip.Read(ctx, func(f winclip.Format) int64 {
		if f.Name == richclip.HTMLFormat {
			return phMaxHTML + 4096
		}
		return 0
	})
	if err != nil {
		return nil, errClipNoHTML
	}
	f, ok := snap.Named(richclip.HTMLFormat)
	if !ok {
		return nil, errClipNoHTML
	}
	b, ok := snap.Bytes(f.ID)
	if !ok {
		return nil, errClipNoHTML
	}
	return cfHTML(b), nil
}
