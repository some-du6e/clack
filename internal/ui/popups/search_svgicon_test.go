package popups

import (
	"image"
	"slices"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Search's file tile is FileEntityThumbnailIcon as the Files browser draws
// it (FileThumbnail, boot-shared PQXV): the svgicon getSVGIconType picks
// for the file object, so Karim's information.txt (filetype "binary",
// 2026-11-18) is binary-4f9150d.svg's 01, a text file the snippet page,
// an email the envelope and an .exe Slack's generic blue page (Karim,
// 2026-11-23: "Slack's blue file icon vs our EXE tile"); a canvas, a list
// and a folder their own tiles, a playable video and audio file
// MediaFileThumbnail's play-filled and sound-filled. What each tile
// draws, not what it picks.
func TestSearchFileTileSvgIcons(t *testing.T) {
	for _, c := range []struct {
		f    core.File
		drew string
	}{
		{core.File{Name: "information.txt", Filetype: "binary", Mime: "application/octet-stream", PrettyType: "Binary"}, "fileicon-binary"},
		{core.File{Name: "notes.txt", Filetype: "text", Mime: "text/plain", PrettyType: "Plain Text"}, "fileicon-file-snippet"},
		{core.File{Name: "notes", Filetype: "text", Mode: "snippet", PrettyType: "Plain Text"}, "fileicon-file-snippet"},
		{core.File{Name: "Hi", Filetype: "email", Mode: "email", PrettyType: "Email"}, "fileicon-email"},
		{core.File{Name: "setup.exe", Filetype: "exe", Mime: "application/x-msdownload", PrettyType: "Executable"}, "fileicon-generic"},
		{core.File{Name: "plan.docx", Filetype: "docx", PrettyType: "Word Document"}, "fileicon-file-word"},
		{core.File{Name: "q3.pdf", Filetype: "pdf", Mime: "application/pdf", PrettyType: "PDF"}, "fileicon-file-pdf"},
		{core.File{Name: "memo.ogg", Filetype: "ogg", Mime: "audio/ogg", PrettyType: "Ogg"}, "fileicon-file-audio"},
		{core.File{Name: "Roadmap", Filetype: "quip", Mode: "quip", IsCanvas: true}, "canvas-content-filled"},
		{core.File{Name: "Bugs", Filetype: "list", Mode: "list", IsList: true}, "list-tab-filled"},
		{core.File{Name: "Designs", Filetype: "folder"}, icons.Slack("folder-filled")},
		{core.File{Name: "Onboarding", Filetype: "folder_skill_set"}, "book"},
		{core.File{Name: "demo.mp4", Filetype: "mp4", Mime: "video/mp4", IsVideo: true}, icons.Slack("play-filled")},
		{core.File{Name: "memo.mp3", Filetype: "mp3", Mime: "audio/mpeg", IsAudio: true}, icons.Slack("sound-filled")},
	} {
		for _, dark := range []bool{false, true} {
			var drawn []string
			prev := icons.DrawHook
			icons.DrawHook = func(n string) { drawn = append(drawn, n) }
			gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
				Constraints: layout.Exact(image.Pt(36, 36))}
			th := theme.New(dark)
			dims := srFileTile(gtx, &tzHost{th: th}, th, c.f, 36)
			icons.DrawHook = prev
			if !slices.Contains(drawn, c.drew) || dims.Size != image.Pt(36, 36) {
				t.Errorf("%s (dark %v): drew %q at %v, want %q in a 36px tile", c.f.Name, dark, drawn, dims.Size, c.drew)
			}
			for _, n := range drawn {
				if n == "missing" {
					t.Errorf("%s (dark %v): drew the missing-icon box", c.f.Name, dark)
				}
			}
		}
	}
}
