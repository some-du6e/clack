package views

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// unthumbedIDs are #files-gallery's images Slack made no thumbnails for
// (a 828×25208 screenshot), by file id.
func unthumbedIDs(t *testing.T, fs *fakeslack.Server) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, name := range []string{fakeslack.FilesUnthumbedImage, fakeslack.FilesUnthumbedSized} {
		for _, f := range fs.FilesCaseFiles(name) {
			out[f.ID] = true
		}
	}
	if len(out) != 2 {
		t.Fatalf("unthumbed fixtures %v", out)
	}
	return out
}

// The Files browser's Images: an image Slack made no thumbs for isn't
// fileIsImage, so its row is a file (the type tile, never a thumbnail)
// and a click opens its page (getMainFileAction "download"), not the
// media viewer, which can't show the original; thumbnailed images still
// draw their thumbnail and open in the viewer.
func TestFilesViewUnthumbedImage(t *testing.T) {
	v, h, e, fs := filesView(t)
	e.size = image.Pt(1200, 3000) // every Images row on screen
	un := unthumbedIDs(t, fs)
	for i := range v.typeOn {
		v.typeOn[i] = fileTypeMenu[i].label == "Images"
	}
	v.reload = true
	e.until(t, "the Images results", func() bool {
		n := 0
		for i := range v.items {
			if un[v.items[i].ID] {
				n++
			}
		}
		return n == len(un)
	})
	e.frame()
	// one sweep down the list finds where each row is (rows are taller
	// than the step)
	x := 328 + 200 // past the sidebar, over the rows' titles
	rowY := map[string]int{}
	for y := 0; y < e.size.Y; y += 10 {
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(x), float32(y))})
		e.frame()
		for id, c := range v.clicks {
			if _, ok := rowY[id]; !ok && c.row.Hovered() {
				rowY[id] = y + 10
			}
		}
	}
	clickRow := func(id string) {
		t.Helper()
		y, ok := rowY[id]
		if !ok {
			t.Fatalf("row %s not on screen", id)
		}
		e.click(image.Pt(x, y))
	}
	thumbed := ""
	for i := range v.items {
		it := &v.items[i]
		if un[it.ID] {
			if it.isImage() || it.Thumb != "" {
				t.Errorf("%s: isImage %v Thumb %q, want a plain file", it.ID, it.isImage(), it.Thumb)
			}
			if _, _, icon := fileTileStyle(it, false); icon != "image" {
				t.Errorf("%s: tile icon %q, want image (getFileTypeSvgIconName: png)", it.ID, icon)
			}
			clickRow(it.ID)
			if got := h.last(); got != "url:"+it.Permalink || it.Permalink == "" {
				t.Errorf("%s opened %q, want its page %q", it.ID, got, it.Permalink)
			}
			continue
		}
		if thumbed == "" && it.isImage() && it.Thumb != "" && it.URL != "" {
			thumbed = it.ID
		}
	}
	if thumbed == "" {
		t.Fatal("no thumbnailed image in the Images results")
	}
	clickRow(thumbed)
	if id, ok := h.viewer(); !ok || id != thumbed {
		t.Errorf("thumbnailed %s opened the viewer %v on file %q (last %q), want the viewer on its file", thumbed, ok, id, h.last())
	}
}

// The channel's Files tab: the un-thumbnailed images are in Photos and
// videos (search's images type) but aren't fileIsImage, so their cell is
// FileImageThumbnail's icon fallback, a click opens the file's page
// (handleFileContainerClick's "download"), and the viewer's gallery from a
// thumbnailed image (fileChannelMediaInfo) leaves them out.
func TestChannelFilesUnthumbedImage(t *testing.T) {
	v, h, e, fs := cfView(t, false)
	e.size = image.Pt(1100, 2000)
	un := unthumbedIDs(t, fs)
	v.setType(cfMedia)
	e.frame()
	e.frame()
	if v.gridColsN <= 0 {
		t.Fatal("no media grid")
	}
	w := min(968, e.size.X-32)
	cols, gap := v.gridColsN, 16
	cw := (w - gap*(cols-1)) / cols
	cellAt := func(id string) image.Point {
		t.Helper()
		idx := -1
		for i := range v.media.items {
			if v.media.items[i].ID == id {
				idx = i
			}
		}
		key := "cell:" + id
		top, ok, _ := v.kb.top(key)
		if idx < 0 || !ok {
			t.Fatalf("cell %s (index %d) not laid out", id, idx)
		}
		p := image.Pt((e.size.X-w)/2+(idx%cols)*(cw+gap)+cw/2, cfListY+top+70)
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
		e.frame()
		if !v.rowOf(key).row.Hovered() {
			t.Fatalf("cell %s isn't at %v", id, p)
		}
		return p
	}
	seen, images := 0, 0
	thumbed := ""
	for i := range v.media.items {
		it := v.media.items[i]
		switch {
		case un[it.ID]:
			seen++
			if it.isImage() || isMediaFile(&it) || it.Thumb != "" {
				t.Errorf("%s: isImage %v isMediaFile %v Thumb %q, want a plain file", it.ID, it.isImage(), isMediaFile(&it), it.Thumb)
			}
			if icon := imageTypeIcon(it.Filetype); icon != "image" {
				t.Errorf("%s: fallback icon %q", it.ID, icon)
			}
			e.click(cellAt(it.ID))
			if got := h.last(); got != "url:"+it.Permalink || it.Permalink == "" {
				t.Errorf("%s opened %q, want its page %q", it.ID, got, it.Permalink)
			}
		case it.isImage() && it.URL != "":
			images++
			if thumbed == "" {
				thumbed = it.ID
			}
		}
	}
	if seen != len(un) || thumbed == "" {
		t.Fatalf("media: %d of %d unthumbed images, thumbed %q", seen, len(un), thumbed)
	}
	e.click(cellAt(thumbed))
	if got := h.last(); got != "gallery:"+thumbed+"/"+itoaTest(images) {
		t.Errorf("thumbnailed %s opened %q, want the viewer with the %d thumbnailed images", thumbed, got, images)
	}
}
