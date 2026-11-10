package views

// The Files view's file actions against internal/fakeslack (synthetic
// files only): starring, the ⋯ menu, Delete, Edit file details, list
// templates and the file opener.

import (
	"image"
	"os"
	"strings"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// filesHost records what the Files view opens.
type filesHost struct {
	*vaHost
	opened []string
}

func (h *filesHost) OpenCanvasFull(id string) { h.opened = append(h.opened, "canvas:"+id) }
func (h *filesHost) OpenListFull(id string)   { h.opened = append(h.opened, "list:"+id) }
func (h *filesHost) OpenCanvas(id string)     { h.opened = append(h.opened, "split-canvas:"+id) }
func (h *filesHost) OpenList(id string)       { h.opened = append(h.opened, "split-list:"+id) }
func (h *filesHost) OpenURL(u string)         { h.opened = append(h.opened, "url:"+u) }
func (h *filesHost) OpenLightbox(img richtext.Image, _ core.Message) {
	h.opened = append(h.opened, "lightbox:"+img.URL)
}

func (h *filesHost) last() string {
	if len(h.opened) == 0 {
		return ""
	}
	return h.opened[len(h.opened)-1]
}

func filesView(t *testing.T) (*Files, *filesHost, *vaEnv, *fakeslack.Server) {
	t.Helper()
	app, fs := vaFake(t)
	h := &filesHost{vaHost: &vaHost{slickHost: &slickHost{app: app, th: theme.New(false)}}}
	v := NewFiles(h)
	e := &vaEnv{size: image.Pt(1200, 900)}
	e.draw = func(gtx layout.Context) {
		v.Layout(gtx)
		for _, o := range h.open() {
			o.Layout(gtx)
		}
	}
	e.until(t, "the files", func() bool { return v.item(fakeslack.CanvasDM) != nil })
	return v, h, e, fs
}

func (f *Files) item(id string) *fileItem {
	for i := range f.items {
		if f.items[i].ID == id {
			return &f.items[i]
		}
	}
	return nil
}

func menuLabels(items []popups.MenuItem) string {
	var out []string
	for _, it := range items {
		if it.Separator {
			out = append(out, "-")
		} else {
			out = append(out, it.Label)
		}
	}
	return strings.Join(out, "|")
}

// The ⋯ menu on your own canvas, and the star: files.favorites.add, the
// canvas under Starred, "Unstar canvas", then files.favorites.remove.
func TestFilesViewStarAndMenu(t *testing.T) {
	v, _, e, fs := filesView(t)
	it := *v.item(fakeslack.CanvasDM)
	want := "Add to saved|Add to starred|Copy link to canvas|-|Open in split view|-|Delete canvas"
	if got := menuLabels(v.fileMenuItems(it)); got != want {
		t.Errorf("canvas menu = %s\nwant %s", got, want)
	}
	v.toggleStar(it.File)
	e.until(t, "the star", func() bool { s := fs.StarredFiles(); return len(s) == 1 && s[0] == fakeslack.CanvasDM })
	if st, _, _ := v.starred.get(); len(st) != 1 || st[0].ID != fakeslack.CanvasDM {
		t.Errorf("Starred rows = %+v", st)
	}
	if got := menuLabels(v.fileMenuItems(it)); !strings.Contains(got, "Unstar canvas") {
		t.Errorf("starred canvas menu = %s", got)
	}
	v.toggleStar(it.File)
	e.until(t, "the unstar", func() bool { return len(fs.StarredFiles()) == 0 })
	if st, _, _ := v.starred.get(); len(st) != 0 {
		t.Errorf("Starred rows after unstar = %+v", st)
	}
	// someone else's list: no Delete, Unstar list once starred
	for _, x := range v.items {
		if x.isList() && x.User != fakeslack.SelfID {
			got := menuLabels(v.fileMenuItems(x))
			if strings.Contains(got, "Delete") || !strings.Contains(got, "Copy link to list") {
				t.Errorf("list menu = %s", got)
			}
			break
		}
	}
}

// Delete canvas: DeleteFileDialog, then files.delete; the row goes.
func TestFilesViewDelete(t *testing.T) {
	v, h, e, fs := filesView(t)
	it := *v.item(fakeslack.CanvasDM)
	v.confirmDelete(it)
	if len(h.open()) != 1 {
		t.Fatalf("overlays = %d, want the delete dialog", len(h.open()))
	}
	v.deleteFile(it)
	e.until(t, "the delete", func() bool { return fs.FileDeleted(fakeslack.CanvasDM) && v.item(fakeslack.CanvasDM) == nil })
}

// Edit file details on your own hosted file: files.edit, the title at once.
func TestFilesViewRename(t *testing.T) {
	v, h, e, fs := filesView(t)
	it := fileItem{core.File{ID: fakeslack.SearchFileID, Title: "Zephyr launch checklist", Mode: "hosted", User: fakeslack.SelfID,
		Filetype: "pdf", Permalink: "https://fake.slack.com/files/x"}}
	v.items = append(v.items, it)
	want := "Add to saved|Copy link to file|-|Edit file details|-|Delete file"
	if got := menuLabels(v.fileMenuItems(it)); got != want {
		t.Errorf("file menu = %s\nwant %s", got, want)
	}
	v.editDetails(it, "Edit file details")
	if len(h.open()) != 1 {
		t.Fatalf("overlays = %d, want Edit file details", len(h.open()))
	}
	v.rename(it, "Renamed checklist")
	if got := v.item(fakeslack.SearchFileID).title(); got != "Renamed checklist" {
		t.Errorf("title = %q", got)
	}
	e.until(t, "files.edit", func() bool { return fs.FileTitle(fakeslack.SearchFileID) == "Renamed checklist" })
}

// The strip's list templates are Slack's three, from lists.templates; a
// tile makes the list and opens it.
func TestFilesViewTemplates(t *testing.T) {
	v, h, e, _ := filesView(t)
	e.until(t, "the templates", func() bool { return len(v.templateItems()) == 3 })
	var names []string
	for _, tp := range v.templateItems() {
		names = append(names, tp.title)
	}
	if got := strings.Join(names, "|"); got != "Project tracker|Feedback tracker|Quarterly plan" {
		t.Errorf("templates = %s", got)
	}
	v.createFromTemplate(v.templateItems()[0])
	e.until(t, "the new list", func() bool { return strings.HasPrefix(h.last(), "list:F0LISTNEW") })
	v.nav = 1 // Canvases: no canvas templates in Clack
	if n := len(v.templateItems()); n != 0 {
		t.Errorf("Canvases templates = %d", n)
	}
}

// openFile: canvases and lists in full view, images in the viewer, the
// rest at their permalink (Slack's file details / viewer aren't in Clack).
func TestFilesViewOpen(t *testing.T) {
	v, h, _, _ := filesView(t)
	v.open(*v.item(fakeslack.CanvasDM))
	if h.last() != "canvas:"+fakeslack.CanvasDM {
		t.Errorf("canvas opened %q", h.last())
	}
	openFile(h, fileItem{core.File{ID: "F1", IsList: true}})
	if h.last() != "list:F1" {
		t.Errorf("list opened %q", h.last())
	}
	openFile(h, fileItem{core.File{ID: "F2", Mime: "image/png", IsImage: true, URL: "https://files.example/a.png"}})
	if h.last() != "lightbox:https://files.example/a.png" {
		t.Errorf("image opened %q", h.last())
	}
	openFile(h, fileItem{core.File{ID: "F3", Filetype: "pdf", Permalink: "https://fake.slack.com/files/U/F3/a.pdf"}})
	if h.last() != "url:https://fake.slack.com/files/U/F3/a.pdf" {
		t.Errorf("pdf opened %q", h.last())
	}
	// a message attachment goes the same way (adapt.go's OpenFile)
	hostHandler{h}.OpenFile(&richtext.File{ID: "F4", Type: "quip", PrettyType: "Canvas"})
	if h.last() != "canvas:F4" {
		t.Errorf("attachment canvas opened %q", h.last())
	}
}

func (h *filesHost) PointerPos() image.Point { return image.Pt(1100, 330) }

// TestFilesViewShots saves the Files view with a starred canvas, its ⋯
// menu, Delete canvas and Edit file details, light and dark
// (VIEWS_SHOTS_DIR, the files-view workflow).
func TestFilesViewShots(t *testing.T) {
	if os.Getenv("VIEWS_SHOTS_DIR") == "" {
		t.Skip("VIEWS_SHOTS_DIR not set")
	}
	for _, dark := range []bool{false, true} {
		v, h, e, fs := filesView(t)
		h.th = theme.New(dark)
		e.until(t, "the templates", func() bool { return len(v.templateItems()) == 3 })
		it := *v.item(fakeslack.CanvasDM)
		v.toggleStar(it.File)
		e.until(t, "the star", func() bool { return len(fs.StarredFiles()) == 1 })
		e.frame()
		vaShot(t, e, "files-starred", dark)
		v.openMenu(it)
		e.frame()
		vaShot(t, e, "files-menu", dark)
		dismissAll(h)
		v.confirmDelete(it)
		e.frame()
		vaShot(t, e, "files-delete", dark)
		dismissAll(h)
		v.editDetails(fileItem{core.File{ID: fakeslack.SearchFileID, Title: "Zephyr launch checklist", Mode: "hosted",
			User: fakeslack.SelfID, Filetype: "pdf"}}, "Edit file details")
		e.frame()
		vaShot(t, e, "files-edit", dark)
	}
}

func dismissAll(h *filesHost) {
	for _, o := range h.open() {
		if d, ok := o.(interface{ Dismiss() }); ok {
			d.Dismiss()
		}
	}
}
