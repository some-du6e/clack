package views

import (
	"image/color"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A file row's 36px tile is FileThumbnail's for a file with no thumb to
// show: the file's svgicon (getSVGIconType), the same in both themes, so
// an .exe is Slack's generic blue page (Karim, 2026-11-23: "Slack's blue
// file icon vs our EXE tile") and Karim's information.txt (filetype
// "binary", 2026-11-18) the binary tile; a canvas is QuipIcon's
// canvas-content-filled on lagoon, a list BaseListIcon's lists-filled on
// honeycomb, each with its dark theme shades; a playable video and an
// audio file are MediaFileThumbnail's.
func TestFileTileSvgIcons(t *testing.T) {
	for _, c := range []struct {
		f   core.File
		art string
	}{
		{core.File{Name: "setup.exe", Filetype: "exe", Mime: "application/x-msdownload", PrettyType: "Executable"}, "fileicon-generic"},
		{core.File{Name: "information.txt", Filetype: "binary", Mime: "application/octet-stream", PrettyType: "Binary"}, "fileicon-binary"},
		{core.File{Name: "notes.txt", Filetype: "text", Mime: "text/plain", PrettyType: "Plain Text"}, "fileicon-file-snippet"},
		{core.File{Name: "notes", Filetype: "text", Mode: "snippet", PrettyType: "Plain Text"}, "fileicon-file-snippet"},
		{core.File{Name: "Hi", Filetype: "email", Mode: "email", PrettyType: "Email"}, "fileicon-email"},
		{core.File{Name: "q3.pdf", Filetype: "pdf", Mime: "application/pdf", PrettyType: "PDF"}, "fileicon-file-pdf"},
		{core.File{Name: "plan.docx", Filetype: "docx", PrettyType: "Word Document"}, "fileicon-file-word"},
		{core.File{Name: "budget.xlsx", Filetype: "xlsx", PrettyType: "Excel Spreadsheet"}, "fileicon-file-excel"},
		{core.File{Name: "logs.zip", Filetype: "zip", PrettyType: "Zip"}, "fileicon-archive"},
		{core.File{Name: "main.go", Filetype: "go", PrettyType: "Go"}, "fileicon-code"},
		{core.File{Name: "IMG_0002.png", Filetype: "png", Mime: "image/png", PrettyType: "PNG"}, "fileicon-file-image"}, // no thumbs: not fileIsImage
	} {
		for _, dark := range []bool{false, true} {
			lk := fileLook(&fileItem{c.f}, dark)
			if lk.art != c.art {
				t.Errorf("%s (dark %v): tile %+v, want the %q svgicon", c.f.Name, dark, lk, c.art)
			}
			if !icons.HasArt(lk.art) {
				t.Errorf("%s: %q isn't registered art", c.f.Name, lk.art)
			}
		}
	}
	for _, c := range []struct {
		f      core.File
		dark   bool
		bg, fg color.NRGBA
		glyph  string
	}{
		{core.File{Name: "Roadmap", Filetype: "quip", IsCanvas: true}, false, theme.PltColor("lagoon", "40", false), theme.PltColor("lagoon", "0", false), "canvas-content-filled"},
		{core.File{Name: "Roadmap", Filetype: "quip", IsCanvas: true}, true, theme.PltColor("lagoon", "60", true), theme.PltColor("lagoon", "100", true), "canvas-content-filled"},
		{core.File{Name: "Bugs", Filetype: "list", IsList: true}, false, theme.PltColor("honeycomb", "40", false), theme.PltColor("honeycomb", "0", false), "list-tab-filled"},
		{core.File{Name: "Bugs", Filetype: "list", IsList: true}, true, theme.PltColor("honeycomb", "50", true), theme.PltColor("honeycomb", "100", true), "list-tab-filled"},
		// MediaFileThumbnail: the frame under a 20% black overlay and
		// play-filled; an audio file overlay_audio's lagoon-50 and
		// sound-filled
		{core.File{Name: "demo.mp4", Filetype: "mp4", Mime: "video/mp4", IsVideo: true, Thumb: "https://files.slack.test/thumb_video.jpg"}, false, color.NRGBA{A: 0x33}, rgb(0xffffff), "play-filled"},
		{core.File{Name: "memo.mp3", Filetype: "mp3", Mime: "audio/mpeg"}, false, theme.PltColor("lagoon", "50", false), rgb(0xffffff), "sound-filled"},
		{core.File{Name: "memo.mp3", Filetype: "mp3", Mime: "audio/mpeg"}, true, theme.PltColor("lagoon", "50", true), rgb(0xffffff), "sound-filled"},
	} {
		lk := fileLook(&fileItem{c.f}, c.dark)
		if lk.art != "" || lk.bg != c.bg || lk.fg != c.fg || lk.glyph != c.glyph {
			t.Errorf("%s (dark %v): tile %+v, want %v %v %q", c.f.Name, c.dark, lk, c.bg, c.fg, c.glyph)
		}
		if !icons.Has(lk.glyph) && !icons.HasSlack(lk.glyph) {
			t.Errorf("%s: glyph %q isn't in the set", c.f.Name, lk.glyph)
		}
		if lk.video != c.f.IsVideo {
			t.Errorf("%s: video %v, want %v (its frame under the overlay)", c.f.Name, lk.video, c.f.IsVideo)
		}
	}
}
