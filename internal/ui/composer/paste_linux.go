//go:build linux && !android

package composer

import (
	"context"
	"errors"
	"os"
)

// Linux: Gio reads only UTF8_STRING (X11) or text/plain (Wayland), so the
// HTML comes from the same tools as clipboard images: wl-paste on
// Wayland, xclip on X11/XWayland. Without them, paste is plain text (plus
// list detection).

var errClipNoHTML = errors.New("no html on the clipboard")

func clipHTMLSupported() bool {
	return (os.Getenv("WAYLAND_DISPLAY") != "" && attHave("wl-paste")) ||
		(os.Getenv("DISPLAY") != "" && attHave("xclip"))
}

func clipReadHTML(ctx context.Context) ([]byte, error) {
	var list []string
	var get func(t string) []string
	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "" && attHave("wl-paste"):
		list = []string{"wl-paste", "--list-types"}
		get = func(t string) []string { return []string{"wl-paste", "--no-newline", "--type", t} }
	case os.Getenv("DISPLAY") != "" && attHave("xclip"):
		list = []string{"xclip", "-selection", "clipboard", "-t", "TARGETS", "-o"}
		get = func(t string) []string { return []string{"xclip", "-selection", "clipboard", "-t", t, "-o"} }
	default:
		return nil, errClipNoHTML
	}
	out, err := attRun(ctx, 1<<20, list[0], list[1:]...)
	if err != nil {
		return nil, errClipNoHTML
	}
	typ := clipPickHTMLType(attParseTargets(string(out)))
	if typ == "" {
		return nil, errClipNoHTML
	}
	args := get(typ)
	return attRun(ctx, phMaxHTML, args[0], args[1:]...)
}
