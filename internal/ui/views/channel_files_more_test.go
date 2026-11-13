package views

// Round 2 of the Files & links tab (channel_files_more.go,
// link_details.go): the search's suggestions, the hover group's Share and
// star, View link details, the media viewer's gallery and the grid's
// keys, against fakeslack's #files-gallery; plus their shots
// (VIEWS_SHOTS_DIR, the files-view workflow).

import (
	"context"
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

func (h *cfHost) Navigate(r string) { h.opened = append(h.opened, "nav:"+r) }
func (h *cfHost) OpenLinkDetails(url, title, icon string) {
	h.opened = append(h.opened, "link-details:"+url+"|"+title)
}
func (h *cfHost) OpenMediaGallery(items []popups.GalleryItem, idx int) {
	h.opened = append(h.opened, "gallery:"+items[idx].File.ID+"/"+itoaTest(len(items)))
	if lb := popups.NewFileGallery(h, items, idx); lb != nil {
		h.ShowOverlay(lb)
	}
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// cfType sets the box's text as typing does and runs a frame.
func cfType(e *vaEnv, v *ChannelFiles, s string) {
	v.search.ed.SetText(s)
	e.frame()
}

func sugFileIDs(v *ChannelFiles) []string {
	var out []string
	for _, r := range v.sug.rows {
		if r.file == nil {
			out = append(out, "echo")
		} else {
			out = append(out, r.file.ID)
		}
	}
	return out
}

// Typing opens the list: the echo alone at first, then after 250 ms
// search.autocomplete.files for "in:<#C> q" and the top hit with the echo
// under it; the radio filters the files; ↑/↓ pick; a file row opens the
// file, the echo searches.
func TestChannelFilesSuggestions(t *testing.T) {
	v, h, e, fs := cfView(t, false)
	cfType(e, v, "report")
	if !v.sug.open {
		t.Fatal("no list after typing")
	}
	if n := len(fs.SearchForms("search.autocomplete.files")); n != 0 {
		t.Errorf("%d searches inside the debounce", n)
	}
	e.until(t, "the remote suggestions", func() bool {
		return len(fs.SearchForms("search.autocomplete.files")) == 1 && !v.sug.remote.busy() && len(v.sug.rows) >= 2
	})
	f := fs.SearchForms("search.autocomplete.files")[0]
	if f.Get("query") != "in:<#"+fakeslack.FilesChannelID+"> report" || f.Get("include_shares") != "true" {
		t.Errorf("form %v", f)
	}
	if got := strings.Join(sugFileIDs(v), ","); got != "F0FILESPDF01,echo" {
		t.Errorf("rows %s", got)
	}
	// Media keeps images and videos only
	v.setType(cfMedia)
	e.frame()
	if got := strings.Join(sugFileIDs(v), ","); got != "echo" {
		t.Errorf("media rows %s", got)
	}
	v.setType(cfAll)
	e.frame()
	// ↓ picks the first, ↑ from it unpicks, ↑ again the last
	v.sugMove(1)
	if v.sug.sel != 0 {
		t.Errorf("↓ picked %d", v.sug.sel)
	}
	v.sugMove(-1)
	v.sugMove(-1)
	if v.sug.sel != len(v.sug.rows)-1 {
		t.Errorf("↑ from none picked %d", v.sug.sel)
	}
	// the file row opens the PDF's page
	v.sugAccept(0)
	if v.sug.open || !strings.HasPrefix(h.last(), "url:") {
		t.Errorf("file row: open %v, opened %q", v.sug.open, h.last())
	}
	// the echo searches
	cfType(e, v, "report ")
	e.until(t, "the list again", func() bool { return v.sug.open && len(v.sug.rows) >= 1 })
	echo := -1
	for i, r := range v.sug.rows {
		if r.file == nil {
			echo = i
		}
	}
	v.sugAccept(echo)
	if v.query != "report" {
		t.Errorf("the echo searched %q", v.query)
	}
	// clearing closes the list
	cfType(e, v, "")
	if v.sug.open {
		t.Error("the list stayed open on an empty box")
	}
}

// The hover group: Share on files (not canvases or lists), the star on
// canvases and lists; Share opens the file share dialog, the star stars
// with addCanvasToFavorites' toast (without custom_file_sections), or
// opens the section menu (with it).
func TestChannelFilesHoverActions(t *testing.T) {
	v, h, e, fs := cfView(t, false)
	h.Core().SetExperimentGroup("custom_file_sections", "")
	e.frame()
	var pdf, canvas, list *fileItem
	for i := range v.files.items {
		it := &v.files.items[i]
		switch {
		case it.ID == "F0FILESPDF01":
			pdf = it
		case it.isCanvas():
			canvas = it
		case it.isList():
			list = it
		}
	}
	if pdf == nil || canvas == nil || list == nil {
		t.Fatal("missing rows")
	}
	if !cfShareShown(pdf) || cfShareShown(canvas) || cfShareShown(list) {
		t.Error("Share is for files only")
	}
	if v.starShown(pdf) || !v.starShown(canvas) || !v.starShown(list) {
		t.Error("the star is for canvases and lists")
	}
	v.openShare(*pdf)
	e.frame()
	if len(h.open()) != 1 {
		t.Fatalf("%d overlays after Share", len(h.open()))
	}
	dismissAll(h.filesHost)
	v.star(*canvas, image.Rect(900, 300, 932, 332))
	e.until(t, "the star", func() bool { return len(fs.StarredFiles()) == 1 })
	ts := h.toasts()
	if len(ts) == 0 || ts[len(ts)-1] != "Added "+canvas.title()+" to Starred. [View]" {
		t.Errorf("toasts %q", ts)
	}
	h.action()
	if h.last() != "canvas:"+canvas.ID {
		t.Errorf("View opened %q", h.last())
	}
	n := len(h.toasts())
	v.star(*list, image.Rect(900, 300, 932, 332))
	e.until(t, "the list star", func() bool { return len(fs.StarredFiles()) == 2 })
	if len(h.toasts()) != n {
		t.Errorf("a list toasted %q (only a child window does)", h.toasts()[n:])
	}
	// custom_file_sections: FileSectionDropdownButton instead of the star
	h.Core().SetExperimentGroup("custom_file_sections", "on")
	if !v.fv.sectionButtonShown(canvas) || !v.starShown(canvas) || v.starTip(canvas) != "" {
		t.Error("the section button doesn't replace the star")
	}
}

// View link details goes to the host with the link and LinkEntity's
// title; the pane finds the conversations that shared it, oldest first.
func TestChannelFilesLinkDetails(t *testing.T) {
	v, h, e, fs := cfView(t, false)
	e.frame()
	var l = v.links.links[0]
	if l.URL != fakeslack.LinkSharedTwice {
		t.Fatalf("first link %s", l.URL)
	}
	v.openLinkDetails(l)
	if got := h.last(); got != "link-details:"+l.URL+"|Roadmap: Q1 2026" {
		t.Errorf("opened %q", got)
	}
	d := NewLinkDetails(h, l.URL, "Roadmap: Q1 2026", l.IconURL)
	pe := &vaEnv{size: image.Pt(420, 900)}
	pe.draw = func(gtx layout.Context) { d.Layout(gtx) }
	pe.until(t, "the shares", func() bool { _, done := d.Shares(); return done })
	shares, _ := d.Shares()
	if len(shares) != 2 || shares[1].Channel != fakeslack.FilesChannelID {
		t.Fatalf("shares %+v", shares)
	}
	forms := fs.SearchForms("search.modules.messages")
	if len(forms) == 0 || forms[len(forms)-1].Get("search_context") != "desktop_link_details" || forms[len(forms)-1].Get("sort_dir") != "asc" {
		t.Errorf("forms %v", forms)
	}
	if linkPlain("Sharing <https://a.example/x|the doc> and <https://b.example/>") != "Sharing the doc and https://b.example/" {
		t.Error("linkPlain")
	}
}

// A media cell opens the viewer with the media browser's images to page
// through, at the clicked one.
func TestChannelFilesGallery(t *testing.T) {
	v, h, e, _ := cfView(t, false)
	v.setType(cfMedia)
	e.frame()
	var imgs []string
	for i := range v.media.items {
		if v.media.items[i].isImage() {
			imgs = append(imgs, v.media.items[i].ID)
		}
	}
	if len(imgs) < 2 {
		t.Fatalf("%d images", len(imgs))
	}
	var second fileItem
	for i := range v.media.items {
		if v.media.items[i].ID == imgs[1] {
			second = v.media.items[i]
		}
	}
	v.openMedia(second)
	if got := h.last(); got != "gallery:"+imgs[1]+"/"+itoaTest(len(imgs)) {
		t.Errorf("opened %q", got)
	}
	// a video opens its page
	for i := range v.media.items {
		if strings.HasPrefix(v.media.items[i].Mime, "video/") {
			v.openMedia(v.media.items[i])
			if !strings.HasPrefix(h.last(), "url:") {
				t.Errorf("video opened %q", h.last())
			}
		}
	}
}

// The grid's arrows (byyT): → and ← step with wrapping, ↓ / ↑ by a row,
// ↑ on the first row goes back to the list; in All's strip a move into a
// hidden row is taken and the cell stays.
func TestChannelFilesGridKeys(t *testing.T) {
	v, _, e, _ := cfView(t, false)
	e.size = image.Pt(700, 900) // a few columns
	v.setType(cfMedia)
	e.frame()
	e.frame()
	cols := v.gridColsN
	n := len(v.media.items)
	if cols < 2 || n <= cols {
		t.Fatalf("cols %d, %d cells", cols, n)
	}
	cell := func(i int) string { return "cell:" + v.media.items[i].ID }
	press := func(k key.Name) bool { return v.gridKey(key.Event{Name: k, State: key.Press}, v.kb.active) }
	v.kb.setActive(cell(0), true)
	if !press(key.NameRightArrow) || v.kb.active != cell(1) {
		t.Errorf("→: %s", v.kb.active)
	}
	if !press(key.NameDownArrow) || v.kb.active != cell(1+cols) {
		t.Errorf("↓: %s", v.kb.active)
	}
	v.kb.setActive(cell(cols), true)
	if !press(key.NameLeftArrow) || v.kb.active != cell(cols-1) {
		t.Errorf("← at a row's start: %s", v.kb.active)
	}
	if !press(key.NameRightArrow) || v.kb.active != cell(cols) {
		t.Errorf("→ at a row's end: %s", v.kb.active)
	}
	v.kb.setActive(cell(0), true)
	if press(key.NameUpArrow) {
		t.Error("↑ on the first row stayed in the grid")
	}
	if press(key.NameLeftArrow) {
		t.Error("← on the first cell found one")
	}
	// through the router: the list focused on the first cell
	if !v.kb.focusOn(cell(0)) {
		t.Fatal("cell not in the list")
	}
	e.frame()
	e.frame()
	kbPress(e, key.NameRightArrow, 0)
	if v.kb.active != cell(1) {
		t.Errorf("→ through the router: %s", v.kb.active)
	}
	// All: the strip
	v.setType(cfAll)
	e.frame()
	e.frame()
	if v.gridShown != cols {
		t.Fatalf("strip shows %d of %d", v.gridShown, cols)
	}
	v.kb.setActive(cell(0), true)
	if !press(key.NameDownArrow) || v.kb.active != cell(0) {
		t.Errorf("↓ in the strip: taken and stays, got %s", v.kb.active)
	}
}

// TestChannelFilesMoreShots: the suggestions, the hover groups, the share
// dialog, the link details pane and the media viewer, light and dark.
func TestChannelFilesMoreShots(t *testing.T) {
	if os.Getenv("VIEWS_SHOTS_DIR") == "" {
		t.Skip("VIEWS_SHOTS_DIR not set")
	}
	for _, dark := range []bool{false, true} {
		v, h, e, _ := cfView(t, dark)
		h.Core().SetExperimentGroup("custom_file_sections", "")
		settle := func() {
			for range 25 {
				e.frame()
				time.Sleep(15 * time.Millisecond)
			}
		}
		settle()
		cfType(e, v, "report")
		e.until(t, "the suggestions", func() bool { return !v.sug.remote.busy() && len(v.sug.rows) >= 2 })
		settle()
		vaShot(t, e, "chfiles-suggest", dark)
		cfType(e, v, "")
		// a canvas row's hover: the star (and the PDF's Share)
		v.setType(cfFiles)
		settle()
		hover := func(pred func(r cfRow) bool) {
			for _, r := range v.rowsFor() {
				if pred(r) {
					top, _, _ := v.kb.top(r.key)
					e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(e.size.X/2), float32(cfListY+top+20))})
					break
				}
			}
			e.frame()
			e.frame()
		}
		hover(func(r cfRow) bool { return r.kind == cfrFile && r.file.isCanvas() })
		vaShot(t, e, "chfiles-hover-star", dark)
		hover(func(r cfRow) bool { return r.kind == cfrFile && r.file.ID == "F0FILESPDF01" })
		vaShot(t, e, "chfiles-hover-share", dark)
		v.setType(cfLinks)
		settle()
		hover(func(r cfRow) bool { return r.kind == cfrLink })
		vaShot(t, e, "chfiles-link-hover", dark)
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(2, 2)})
		// the share dialog
		v.setType(cfFiles)
		settle()
		for i := range v.files.items {
			if v.files.items[i].ID == "F0FILESPDF01" {
				v.openShare(v.files.items[i])
			}
		}
		settle()
		vaShot(t, e, "chfiles-share", dark)
		dismissAll(h.filesHost)
		// the media viewer, paging
		v.setType(cfMedia)
		settle()
		for i := range v.media.items {
			if v.media.items[i].isImage() {
				v.openMedia(v.media.items[i])
				break
			}
		}
		settle()
		vaShot(t, e, "chfiles-viewer", dark)
		dismissAll(h.filesHost)
		// the link details pane
		d := NewLinkDetails(h, fakeslack.LinkSharedTwice, "Roadmap: Q1 2026", v.links.links[0].IconURL)
		pe := &vaEnv{size: image.Pt(420, 900)}
		pe.draw = func(gtx layout.Context) { d.Layout(gtx) }
		pe.until(t, "the shares", func() bool { _, done := d.Shares(); return done })
		for range 10 {
			pe.frame()
			time.Sleep(15 * time.Millisecond)
		}
		vaShot(t, pe, "chfiles-link-details", dark)
	}
}

// The Manage tabs card (ManageChannelTabs): the channel's tabs in order
// with hidden ones; Hide tab / Show tab (conversations.removeTab /
// addTab) and Move tab down (conversations.moveTab with after).
func TestChannelFilesManageTabs(t *testing.T) {
	for _, dark := range []bool{false, true} {
		_, h, _, fs := cfView(t, dark)
		fs.Permissions = map[string][]string{fakeslack.FilesChannelID: {core.PermManageChannelTabs}} // useCanEditTabs on Grid
		d := NewChannelDetails(h, fakeslack.FilesChannelID)
		e := &vaEnv{size: image.Pt(700, 900)}
		e.draw = func(gtx layout.Context) {
			d.Layout(gtx)
			for _, o := range h.open() {
				o.Layout(gtx)
			}
		}
		e.frame()
		d.ShowTab("Tabs")
		m := d.manageTabs()
		e.until(t, "the tab order", func() bool {
			_, ok, _ := m.order.get()
			can, cok, _ := m.can.get()
			return ok && cok && can
		})
		order := m.manageOrder()
		if len(order) != 2 || order[0].ID != "channel" || order[1].ID != "files" {
			t.Fatalf("order %+v", order)
		}
		d.toggleTabVisible(m, order[1])
		e.until(t, "the hide", func() bool {
			g := fs.GalleryTabs()
			o := m.manageOrder()
			return len(g) == 1 && g[0] == "-files" && !m.order.busy() && len(o) == 2 && o[1].Disabled
		})
		d.toggleTabVisible(m, m.manageOrder()[1])
		e.until(t, "the show", func() bool {
			g := fs.GalleryTabs()
			return len(g) == 1 && g[0] == "files" && !m.order.busy() && !m.manageOrder()[1].Disabled
		})
		fs.SetGalleryTabs("files", "workflows", "pins")
		m.order.load(h, true, func(ctx context.Context) ([]core.TabState, error) {
			return h.Core().TabOrder(ctx, fakeslack.FilesChannelID)
		})
		e.until(t, "three tabs", func() bool { return !m.order.busy() && len(m.manageOrder()) == 4 })
		d.moveTab(m, m.manageOrder(), 1, 2) // Files & links down
		e.until(t, "the move", func() bool {
			g := fs.GalleryTabs()
			return len(g) == 3 && g[0] == "workflows" && g[1] == "files" && !m.order.busy()
		})
		w := fs.TabWrites()
		if last := w[len(w)-1]; last.Get("method") != "conversations.moveTab" || last.Get("id") != "files" || last.Get("after") != "workflows" {
			t.Errorf("move %v", last)
		}
		// a hidden Pins and a hovered row for the shot
		d.toggleTabVisible(m, m.manageOrder()[3])
		e.until(t, "Pins hidden", func() bool { return !m.order.busy() && m.manageOrder()[3].Disabled })
		for range 5 {
			e.frame()
		}
		for y := 100; y < 800; y += 10 {
			e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(350, float32(y))})
			e.frame()
			if c := m.clk["row:files"]; c != nil && c.Hovered() {
				break
			}
		}
		e.frame()
		vaShot(t, e, "chfiles-manage-tabs", dark)
	}
}
