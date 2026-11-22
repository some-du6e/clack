package popups

import (
	"image/color"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

// Search's file tile for a file Slack has a file-type svgicon for
// (getSVGIconType): binary-4f9150d.svg's #616061 tile and 01,
// file-snippet-0a4c72a.svg's #1D9BD1 tile and T for a text file, the
// email's envelope. Karim's information.txt (2026-11-18) is filetype
// "binary".
func TestSearchFileTileSvgIcons(t *testing.T) {
	for _, c := range []struct {
		f    core.File
		bg   color.NRGBA
		icon string
	}{
		{core.File{Name: "information.txt", Filetype: "binary", Mime: "application/octet-stream", PrettyType: "Binary"}, rgb(0x616061), "file-binary"},
		{core.File{Name: "notes.txt", Filetype: "text", Mime: "text/plain", PrettyType: "Plain Text"}, rgb(0x1d9bd1), "file-text"},
		{core.File{Name: "notes", Filetype: "text", Mode: "snippet", PrettyType: "Plain Text"}, rgb(0x1d9bd1), "file-text"},
		{core.File{Name: "Hi", Filetype: "email", Mode: "email", PrettyType: "Email"}, rgb(0x1d9bd1), "file-email"},
	} {
		for _, dark := range []bool{false, true} {
			bg, fg, icon := srFileTileStyle(c.f, dark)
			if bg != c.bg || fg != rgb(0xffffff) || icon != c.icon {
				t.Errorf("%s (dark %v): tile %v %v %q, want %v white %q", c.f.Name, dark, bg, fg, icon, c.bg, c.icon)
			}
		}
	}
}
