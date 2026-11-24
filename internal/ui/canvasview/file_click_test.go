package canvasview

import (
	"testing"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// lightboxHost records the media viewer's opens.
type lightboxHost struct {
	testHost
	opened []string
}

func (h *lightboxHost) OpenLightbox(img richtext.Image, _ core.Message) {
	h.opened = append(h.opened, img.URL)
}

// A file embedded in a canvas keeps its click (the review of 93ab723be:
// its card had no filetype, mode or URLs, so a PDF, zip or .docx asked
// for a download with no URL and nothing happened): an image opens the
// media viewer on the original, any other file previewFile's file
// details, which Clack has as the file's page; never a download.
func TestCanvasFileClick(t *testing.T) {
	h := &lightboxHost{testHost: testHost{th: theme.New(false)}}
	var urls []string
	v := New(h, "F0CANVAS01", Full, Actions{OpenURL: func(u string) { urls = append(urls, u) }})
	d := &core.CanvasDoc{Files: map[string]core.CanvasFile{
		"F0PDF": {ID: "F0PDF", Name: "q3.pdf", Title: "q3.pdf", Mime: "application/pdf", Filetype: "pdf", Mode: "hosted", PrettyType: "PDF",
			URL: "https://files.slack.com/files-pri/T1-F0PDF/q3.pdf", Download: "https://files.slack.com/files-pri/T1-F0PDF/download/q3.pdf", Permalink: "https://x.slack.com/files/U1/F0PDF/q3.pdf"},
		"F0ZIP": {ID: "F0ZIP", Name: "a.zip", Title: "a.zip", Mime: "application/zip", Filetype: "zip", Mode: "hosted", PrettyType: "Zip",
			URL: "https://files.slack.com/files-pri/T1-F0ZIP/a.zip", Download: "https://files.slack.com/files-pri/T1-F0ZIP/download/a.zip", Permalink: "https://x.slack.com/files/U1/F0ZIP/a.zip"},
		"F0IMG": {ID: "F0IMG", Name: "p.png", Title: "p.png", Mime: "image/png", Filetype: "png", Mode: "hosted", PrettyType: "PNG", IsImage: true,
			URL: "https://files.slack.com/files-pri/T1-F0IMG/p.png", Thumb: "https://files.slack.com/files-tmb/T1-F0IMG/p_720.png", ThumbW: 720, ThumbH: 480,
			Permalink: "https://x.slack.com/files/U1/F0IMG/p.png"},
	}}
	file := func(id string) *richtext.File {
		rd, ok := v.richDoc(d, &canvas.Block{Kind: canvas.File, FileID: id})
		if !ok || len(rd.Blocks) != 1 || rd.Blocks[0].File == nil {
			t.Fatalf("%s: no file block (%v, %+v)", id, ok, rd.Blocks)
		}
		return rd.Blocks[0].File
	}
	r := &handler{v: v}
	for _, id := range []string{"F0PDF", "F0ZIP"} {
		n := len(urls)
		f := file(id)
		if f.Type == "" || f.Mode == "" || f.Download == "" {
			t.Errorf("%s: the card has type %q mode %q download %q (the canvas file's)", id, f.Type, f.Mode, f.Download)
		}
		r.OpenFile(f)
		if len(urls) != n+1 || urls[n] != d.Files[id].Permalink {
			t.Errorf("%s: the click opened %v, want its page %s", id, urls[n:], d.Files[id].Permalink)
		}
	}
	r.OpenFile(file("F0IMG"))
	if len(h.opened) != 1 || h.opened[0] != d.Files["F0IMG"].URL {
		t.Errorf("the image opened the viewer on %v, want its original %s", h.opened, d.Files["F0IMG"].URL)
	}
}
