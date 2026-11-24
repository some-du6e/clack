package composer

import (
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// A chip's icon is the svgicon getSVGIconType picks for the File being
// uploaded, by its MIME type (getFileTypeFromMimeType) where Slack has an
// svgicon for that type, else generic: an .exe is Slack's generic blue
// page (Karim, 2026-11-23: "Slack's blue file icon vs our EXE tile"), as a
// .docx is before the server names its filetype; a recorded clip goes by
// its subtype. Every name is drawable art.
func TestAttIconArt(t *testing.T) {
	for _, c := range []struct {
		sub, mime, typ, art string
	}{
		{"", "application/x-msdownload", "generic", "fileicon-generic"},
		{"", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "generic", "fileicon-generic"},
		{"", "application/pdf", "pdf", "fileicon-file-pdf"},
		{"", "application/zip", "zip", "fileicon-archive"},
		{"", "application/vnd.ms-excel", "csv", "fileicon-file-excel"},
		{"", "text/plain", "text", "fileicon-file-snippet"},
		{"", "text/javascript", "generic", "fileicon-generic"}, // javascript is a known type, but G40A has no icon for it
		{"", "audio/mpeg", "audio", "fileicon-file-audio"},
		{"", "video/mp4", "mp4", "fileicon-file-video"},
		{"", "image/png", "png", "fileicon-file-image"},
		{"", "image/jpeg", "generic", "fileicon-generic"}, // "jpeg" isn't one of the known types; "image" has no icon
		{"slack_audio", "audio/webm", "audio", "fileicon-file-audio"},
		{"slack_video", "video/webm", "video", "fileicon-file-video"},
		{"", "", "generic", "fileicon-generic"},
	} {
		typ := attSVGType(c.sub, c.mime)
		art := richview.FileIconArt(&richtext.File{Type: typ})
		if typ != c.typ || art != c.art {
			t.Errorf("subtype %q, MIME %q: type %q art %q, want %q %q", c.sub, c.mime, typ, art, c.typ, c.art)
		}
		if !icons.HasArt(art) {
			t.Errorf("%q isn't registered art", art)
		}
	}
	// a file restored from a draft is Slack's file object (files.info): its
	// own filetype picks the icon, whatever its name's MIME type says
	for _, c := range []struct {
		f   richtext.File
		art string
	}{
		{richtext.File{Type: "docx"}, "fileicon-file-word"},
		{richtext.File{Type: "binary"}, "fileicon-binary"},
		{richtext.File{Type: "zip"}, "fileicon-archive"},
	} {
		f := c.f
		v := attView{c: &attChip{}, typ: "application/x-msdownload", server: &f}
		if got := attIconArt(v); got != c.art {
			t.Errorf("restored %q: art %q, want %q", c.f.Type, got, c.art)
		}
	}
}
