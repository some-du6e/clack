package views

import (
	"image/color"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/icons"
)

// A file row's 36px tile is FileThumbnail's for a file with no thumb to
// show: the file's svgicon (getSVGIconType), the same in both themes, so
// an .exe is Slack's generic blue page (Karim, 2026-11-23: "Slack's blue
// file icon vs our EXE tile") and Karim's information.txt (filetype
// "binary", 2026-11-18) the binary tile; a canvas is QuipIcon's
// canvas-content-filled on lagoon, a list BaseListIcon's lists-filled on
// honeycomb, a folder its folder tile, each with its dark theme shades; a
// playable video and a playable audio file are MediaFileThumbnail's, any
// other audio file IconThumbnail's.
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
		// fileIsPlayableAudio is an m4a, mp3 or wav: an ogg is IconThumbnail's
		// file-audio svgicon, a flac (no svgicon of its own) the generic page
		{core.File{Name: "memo.ogg", Filetype: "ogg", Mime: "audio/ogg", PrettyType: "Ogg"}, "fileicon-file-audio"},
		{core.File{Name: "take.flac", Filetype: "flac", Mime: "audio/flac", PrettyType: "FLAC"}, "fileicon-generic"},
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
		// Slack's kit values: lagoon-40/-0 light, lagoon-60/-100 dark;
		// honeycomb-40/-0 light, honeycomb-50/-100 dark
		{core.File{Name: "Roadmap", Filetype: "quip", IsCanvas: true}, false, rgb(0x1cb6eb), rgb(0xe3f8ff), "canvas-content-filled"},
		{core.File{Name: "Roadmap", Filetype: "quip", IsCanvas: true}, true, rgb(0x2ba5ce), rgb(0xd8eef6), "canvas-content-filled"},
		{core.File{Name: "Bugs", Filetype: "list", IsList: true}, false, rgb(0xe19b06), rgb(0xfcf2d9), "list-tab-filled"},
		{core.File{Name: "Bugs", Filetype: "list", IsList: true}, true, rgb(0xc48305), rgb(0xf4e9ce), "list-tab-filled"},
		// p-file_thumbnail__folder_icon: white folder-filled on gray-50;
		// --skill-set: indigo-60 book on indigo-5
		{core.File{Name: "Designs", Filetype: "folder"}, false, rgb(0x7c7a7f), rgb(0xffffff), "folder-filled"},
		{core.File{Name: "Designs", Filetype: "folder"}, true, rgb(0x797c81), rgb(0xffffff), "folder-filled"},
		{core.File{Name: "Onboarding", Filetype: "folder_skill_set"}, false, rgb(0xdce3fa), rgb(0x3651c0), "book"},
		{core.File{Name: "Onboarding", Filetype: "folder_skill_set"}, true, rgb(0x0a165a), rgb(0x657ace), "book"},
		// MediaFileThumbnail: the frame under a 20% black overlay and
		// play-filled; a playable audio file overlay_audio's lagoon-50 and
		// sound-filled
		{core.File{Name: "demo.mp4", Filetype: "mp4", Mime: "video/mp4", IsVideo: true, Thumb: "https://files.slack.test/thumb_video.jpg"}, false, color.NRGBA{A: 0x33}, rgb(0xffffff), "play-filled"},
		{core.File{Name: "memo.mp3", Filetype: "mp3", Mime: "audio/mpeg", IsAudio: true}, false, rgb(0x0e9dd3), rgb(0xffffff), "sound-filled"},
		{core.File{Name: "memo.mp3", Filetype: "mp3", Mime: "audio/mpeg", IsAudio: true}, true, rgb(0x1699ca), rgb(0xffffff), "sound-filled"},
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
