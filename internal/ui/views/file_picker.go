package views

// Slack's unified file picker as a bookmark folder opens it (the add
// menu's "File, canvas, or list" under unified_file_picker: FolderCreateMenu
// eE → boot-shared zk1i UnifiedFilePicker, source "channel_folder",
// showListViewPicker, onInsertNewCanvas / onInsertNewList, no upload), from
// the web client (build 859cadc0e0ab, read from the public CDN without a
// cookie):
//
//   - A modal at most 950 wide titled "Attach a file" ("Add a tab" only
//     for source channel_tab). .filePickerModalContent__HaFPm (16 down):
//     the search over the whole width, the sidebar (.sidebar__BD0_e: 29%
//     wide, 52 down, 8 in) and the results (.p-explorer_results: the right
//     68%, at least 400 tall).
//   - The sidebar's tabs: All Files, then N yE kept by their gates:
//     Canvases (canvases on), Lists (lists on), Surfaces and Skills (off
//     for the account: surfaces_skills_unipicker unassigned), PDFs,
//     Documents, Spreadsheets, Presentations, Images; no external
//     providers (file_picker_app_install_states unassigned). A row
//     (.itemButton__VCojk) is 32 tall, 13px, radius 4, padding 0 16 0 8:
//     a 20px icon slot 8 before the label (Slack's logo, 16px, on All
//     Files only; hidden on the rest), the label, and the tab's result
//     count on the right in content-ter (none on All Files); every row but
//     All Files and the hovered or selected one has the 2px surf-pry line
//     at 16 in (the tree's line). Hovered: surf-pry. Selected
//     (.itemButtonSelected__LKGuU): base-inv-hgl-1 with content-inv-pry
//     bold text and count.
//   - Each tab searches its file types (+XZr RF: All Files is lists,
//     canvases, documents, spreadsheets, presentations, PDFs and images)
//     through the unified files explorer (core.Files), all tabs at once
//     for their counts when the picker opens and when the query changes
//     (ed, eg), the query from the search field ("Search files", "Search
//     canvases", "Search lists", "Search PDFs", …).
//   - The results start with the controls: Filter (All, Shared with you,
//     Created by you) and Sort (Recently viewed or Last updated; with a
//     query Most relevant or Last updated); then a row per file
//     (unified-file-result: the file's 36px icon, its title and its line),
//     the picked one active; "No results" / "You may want to try
//     adjusting your filters." when there are none. A click picks a file,
//     a double click inserts it.
//   - The footer: Create New (outline, plus 16: a menu of Canvas and List,
//     bottom-left) on the left; Cancel and the primary Insert, disabled
//     until a file is picked, loading while it inserts.
//   - Insert (onFileInserted for the folder, eI):
//     maybeOpenShareFileAsFolderItemConfirmationModal (ien3 V): files.info
//     (reason "open-add-file-in-folder-share-dialog") unless the file is
//     known; straight into the folder (bookmarks.add type file) for the
//     viewer's own DM, a file already shared in the conversation, or an
//     external file; else ShareFileAsChannelFolderItemConfirmationModal
//     ("Share this file?", "When you share <b>{fileName}</b> here, anyone
//     in <b>{channel}</b> can access it." with the DM and group DM forms,
//     for a canvas or list the access select Can edit / Can view, Cancel
//     and Share: bookmarks.add with access_level). The picker closes.
//   - Create New's Canvas (ej) makes an untitled canvas, List (eN) a blank
//     list (to-do mode under lists_grid_todos); either goes through the
//     same share-into-folder step, then opens.
//
// Not ported: the external providers' tabs and their connect flows, the
// Surfaces and Skills tabs (gated off for the account), the canvases and
// lists tabs' own explorers (Clack runs the unified files explorer with
// their types), infinite pagination past the first page, and the share
// modal's external-sharing variants ("Some people can’t view this canvas"),
// whose org policy reads Clack doesn't have.

import (
	"context"
	"image"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var nsUFP = i18n.NS("files") // the picker's strings (zk1i: new l.Ay("files"))

// fpTab is one of the picker's sidebar tabs.
type fpTab struct {
	id    string
	title string
	kind  string   // core.FilesOpts.Kind
	types []string // core.FilesOpts.Types
}

func fpTabs(c *core.App) []fpTab {
	tabs := []fpTab{{id: "files", title: nsUFP.T("All Files"), types: []string{"lists", "quip", "documents", "spreadsheets", "presentations", "pdfs", "images"}}}
	tabs = append(tabs,
		fpTab{id: "canvas", title: nsUFP.T("Canvases"), kind: "canvases"},
		fpTab{id: "lists", title: nsUFP.T("Lists"), kind: "lists"},
		fpTab{id: "pdfs", title: nsUFP.T("PDFs"), types: []string{"pdfs"}},
		fpTab{id: "documents", title: nsUFP.T("Documents"), types: []string{"documents"}},
		fpTab{id: "spreadsheets", title: nsUFP.T("Spreadsheets"), types: []string{"spreadsheets"}},
		fpTab{id: "presentations", title: nsUFP.T("Presentations"), types: []string{"presentations"}},
		fpTab{id: "images", title: nsUFP.T("Images"), types: []string{"images"}},
	)
	return tabs
}

// placeholder is UnifiedFilePickerSearch's input text for the tab.
func (t fpTab) placeholder() string {
	switch t.id {
	case "canvas":
		return nsUFP.T("Search canvases")
	case "lists":
		return nsUFP.T("Search lists")
	case "documents":
		return nsUFP.T("Search documents")
	case "spreadsheets":
		return nsUFP.T("Search spreadsheets")
	case "presentations":
		return nsUFP.T("Search presentations")
	case "pdfs":
		return nsUFP.T("Search PDFs")
	case "images":
		return nsUFP.T("Search images")
	}
	return nsUFP.T("Search files")
}

// FilePicker is the unified file picker for a folder.
type FilePicker struct {
	base
	fv     *Files
	cf     *ChannelFiles
	modal  *popups.Modal
	ch     string
	folder string
	tabs   []fpTab
	sel    int

	search    widget.Editor
	query     string // the query searched
	typedAt   time.Time
	filter    string // "" all | "shared" | "mine"
	sort      string // "" the default | "timestamp" | "score"
	filterBtn widget.Clickable
	sortBtn   widget.Clickable
	filterSz  image.Point
	sortSz    image.Point
	sideBtns  []widget.Clickable

	mu      sync.Mutex
	gen     int
	counts  map[string]int
	files   []core.File
	loading bool
	err     bool

	list   widget.List
	rows   map[string]*widget.Clickable
	picked string

	inserting bool
	insert    *popups.ModalButton
	focused   bool
}

// NewFolderFilePicker opens the picker for folder (its folder bookmark
// id; "" the channel's loose bookmarks) in ch.
func NewFolderFilePicker(h Host, ch, folder string) *FilePicker {
	v := &FilePicker{base: newBase(h), fv: NewFiles(h), cf: NewChannelFiles(h, ch), ch: ch, folder: folder,
		counts: map[string]int{}, rows: map[string]*widget.Clickable{}}
	v.tabs = fpTabs(h.Core())
	v.sideBtns = make([]widget.Clickable, len(v.tabs))
	v.search.SingleLine = true
	v.list.Axis = layout.Vertical
	ph, _ := h.(popups.Host)
	v.insert = &popups.ModalButton{Label: nsUFP.T("Insert"), Kind: popups.BtnPrimary,
		Enabled: func() bool { return v.picked != "" && !v.inserting }, Do: func() bool { v.Insert(); return false }}
	create := &popups.ModalButton{Label: nsUFP.T("Create New"), Kind: popups.BtnOutline, Icon: "plus", Left: true,
		Do: func() bool { v.createMenu(); return false }}
	v.modal = popups.NewModal(ph, popups.ModalOpts{Title: nsUFP.T("Attach a file"), Width: 950, Body: v.layout,
		Buttons: []*popups.ModalButton{create, {Label: nsUFP.T("Cancel"), Kind: popups.BtnOutline}, v.insert}})
	v.fetch()
	return v
}

func (v *FilePicker) Layout(gtx C) D    { return v.modal.Layout(gtx) }
func (v *FilePicker) Dismissed() bool   { return v.modal.Dismissed() }
func (v *FilePicker) Dismiss()          { v.modal.Dismiss() }
func (v *FilePicker) Tab() string       { return v.tabs[v.sel].id }
func (v *FilePicker) Picked() string    { return v.picked }
func (v *FilePicker) SetQuery(q string) { v.search.SetText(q); v.query = q; v.fetch() }

// Files are the selected tab's results (tests).
func (v *FilePicker) Files() []core.File {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]core.File(nil), v.files...)
}

// Count is a tab's result count (tests; -1 until it answers).
func (v *FilePicker) Count(id string) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	if n, ok := v.counts[id]; ok {
		return n
	}
	return -1
}

// SelectTab is a click on a sidebar tab (eZ: the query, pick and sort
// reset).
func (v *FilePicker) SelectTab(id string) {
	for i, t := range v.tabs {
		if t.id == id && i != v.sel {
			v.sel = i
			v.search.SetText("")
			v.query, v.picked, v.filter, v.sort = "", "", "", ""
			v.fetch()
		}
	}
}

// Pick is a click on a result.
func (v *FilePicker) Pick(id string) { v.picked = id; v.h.Invalidate() }

func (v *FilePicker) opts(t fpTab) core.FilesOpts {
	o := core.FilesOpts{Kind: t.kind, Types: t.types, Query: v.query, Sort: v.sort, Page: 1}
	switch v.filter {
	case "shared":
		o.SharedWithMe = true
	case "mine":
		o.Mine = true
	}
	return o
}

// fetch searches every tab (their counts) and keeps the selected tab's
// files.
func (v *FilePicker) fetch() {
	c := v.h.Core()
	if c == nil {
		return
	}
	v.mu.Lock()
	v.gen++
	gen := v.gen
	v.loading, v.err = true, false
	v.mu.Unlock()
	sel := v.sel
	for i, t := range v.tabs {
		o := v.opts(t)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			page, err := c.Files(ctx, o)
			v.mu.Lock()
			defer v.mu.Unlock()
			if gen != v.gen {
				return
			}
			if err == nil {
				v.counts[t.id] = page.Total
			}
			if i == sel {
				v.loading = false
				v.err = err != nil
				v.files = page.Files
			}
			v.h.Invalidate()
		}()
	}
}

func (v *FilePicker) pickedFile() (core.File, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, f := range v.files {
		if f.ID == v.picked {
			return f, true
		}
	}
	return core.File{}, false
}

// Insert is tr: the picked file into the folder (ien3), then the picker
// closes.
func (v *FilePicker) Insert() {
	f, ok := v.pickedFile()
	if !ok || v.inserting {
		return
	}
	v.inserting = true
	ShareFileIntoFolder(v.h, v.ch, v.folder, f, func(added bool) {
		v.inserting = false
		v.modal.Dismiss()
	})
}

// createMenu is CreateNewCanvasOrListMenu.
func (v *FilePicker) createMenu() {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	items := []popups.MenuItem{
		{Key: "canvas", Label: nsUFP.T("Canvas"), Icon: "canvas-content", Do: func() { v.modal.Dismiss(); v.createAndInsert(false) }},
		{Key: "list", Label: nsUFP.T("List"), Icon: "lists", Do: func() { v.modal.Dismiss(); v.createAndInsert(true) }},
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Width: 240, Popover: "bottom-left"}))
}

// createAndInsert is ej / eN: a new canvas (or list), into the folder,
// then opened.
func (v *FilePicker) createAndInsert(list bool) {
	c := v.h.Core()
	if c == nil {
		return
	}
	ch, folder, h := v.ch, v.folder, v.h
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var id string
		var err error
		if list {
			id, err = c.CreateListWith(ctx, core.CreateListOpts{TemplateID: "blank", Reason: "create-list-from-channel-folder",
				TodoMode: c.Experiment("lists_grid_todos") == "on"})
		} else {
			id, err = c.CreateCanvas(ctx, "", "", "")
		}
		if err != nil || id == "" {
			return
		}
		f, err := c.FileInfo(ctx, id)
		if err != nil {
			return
		}
		postTo(h, func() {
			ShareFileIntoFolder(h, ch, folder, f, func(added bool) {
				if list {
					if lo, ok := h.(interface{ OpenListFull(id string) }); ok {
						lo.OpenListFull(id)
					}
				} else if co, ok := h.(interface{ OpenCanvasFull(id string) }); ok {
					co.OpenCanvasFull(id)
				}
			})
		})
	}()
}

func postTo(h Host, f func()) {
	if p, ok := h.(interface{ Post(func()) }); ok {
		p.Post(f)
		return
	}
	f()
}

// ---- layout ----

func (v *FilePicker) update(gtx C) {
	if !v.focused {
		gtx.Execute(key.FocusCmd{Tag: &v.search})
		v.focused = true
	}
	for {
		e, ok := v.search.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.ChangeEvent); ok {
			v.typedAt = gtx.Now
		}
	}
	if q := strings.TrimSpace(v.search.Text()); q != v.query && !v.typedAt.IsZero() {
		if d := gtx.Now.Sub(v.typedAt); d >= 300*time.Millisecond {
			v.query, v.picked = q, ""
			v.fetch()
		} else {
			v.h.Invalidate()
		}
	}
	for i := range v.sideBtns {
		if v.sideBtns[i].Clicked(gtx) {
			v.SelectTab(v.tabs[i].id)
		}
	}
	if v.filterBtn.Clicked(gtx) {
		v.filterMenu(v.fv.buttonRect(&v.filterBtn, v.filterSz))
	}
	if v.sortBtn.Clicked(gtx) {
		v.sortMenu(v.fv.buttonRect(&v.sortBtn, v.sortSz))
	}
	for id, r := range v.rows {
		for {
			cl, ok := r.Update(gtx)
			if !ok {
				break
			}
			v.picked = id
			if cl.NumClicks >= 2 { // onFileDoubleClicked: insert
				v.Insert()
			}
		}
	}
}

func (v *FilePicker) filterLabel() string {
	switch v.filter {
	case "shared":
		return nsBrowsers.T("Shared with you")
	case "mine":
		return nsBrowsers.T("Created by you")
	}
	return nsBrowsers.T("All")
}

func (v *FilePicker) sortLabel() string {
	switch {
	case v.sort == "timestamp":
		return nsBrowsers.T("Last updated")
	case v.query != "":
		return nsBrowsers.T("Most relevant")
	}
	return nsBrowsers.T("Recently viewed")
}

func (v *FilePicker) filterMenu(btn image.Rectangle) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	set := func(f string) func() {
		return func() {
			v.filter = f
			if f == "mine" && v.query == "" && v.sort == "" {
				v.sort = "timestamp" // Created by you sorts by Last updated (QX NEWEST)
			}
			v.picked = ""
			v.fetch()
		}
	}
	items := []popups.MenuItem{
		{Label: nsBrowsers.T("All"), Checked: v.filter == "", Do: set("")},
		{Label: nsBrowsers.T("Shared with you"), Checked: v.filter == "shared", Do: set("shared")},
		{Label: nsBrowsers.T("Created by you"), Checked: v.filter == "mine", Do: set("mine")},
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: btn, Width: 240, Popover: "bottom-left"}))
}

func (v *FilePicker) sortMenu(btn image.Rectangle) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	set := func(s string) func() { return func() { v.sort = s; v.picked = ""; v.fetch() } }
	first := popups.MenuItem{Label: nsBrowsers.T("Recently viewed"), Checked: v.sort == "", Do: set("")}
	if v.query != "" {
		first = popups.MenuItem{Label: nsBrowsers.T("Most relevant"), Checked: v.sort == "" || v.sort == "score", Do: set("score")}
	}
	items := []popups.MenuItem{first, {Label: nsBrowsers.T("Last updated"), Checked: v.sort == "timestamp", Do: set("timestamp")}}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: btn, Width: 240, Popover: "bottom-right"}))
}

func (v *FilePicker) layout(gtx C) D {
	v.frame()
	v.fv.frame()
	v.cf.frame()
	v.update(gtx)
	w := gtx.Constraints.Max.X
	y := gtx.Dp(16) // .filePickerModalContent__HaFPm
	// the search over the whole width
	sh := gtx.Dp(36)
	dirAt(gtx, image.Pt(0, y), func(gtx C) D { return v.searchField(gtx, w, sh) })
	top := y + gtx.Dp(52) // .sidebar__BD0_e margin-top 52
	sw := w * 29 / 100
	rw := w * 68 / 100
	rh := gtx.Dp(400)
	dirAt(gtx, image.Pt(gtx.Dp(8), top), func(gtx C) D { return v.sidebar(gtx, sw) })
	dirAt(gtx, image.Pt(w-rw, top), func(gtx C) D { return v.results(gtx, rw, rh) })
	return D{Size: image.Pt(w, top+max(rh, len(v.tabs)*gtx.Dp(32)))}
}

func (v *FilePicker) searchField(gtx C, w, h int) D {
	r := image.Rect(0, 0, w, h)
	rad := gtx.Dp(8)
	bw := max(1, gtx.Dp(1))
	if gtx.Focused(&v.search) {
		rrect(gtx, r.Inset(-gtx.Dp(3)), rad+gtx.Dp(3), theme.WithAlpha(v.lt(rgb(0x1264a3), rgb(0x1d9bd1)), 0x4d))
		rrect(gtx, r.Inset(-bw), rad+bw, v.lt(rgb(0x1264a3), rgb(0x1d9bd1)))
		rrect(gtx, r, rad, v.p.Surface)
	} else {
		rrect(gtx, r, rad, v.p.SegLine)
		rrect(gtx, r.Inset(bw), rad-bw, v.p.Surface)
	}
	dirAt(gtx, image.Pt(gtx.Dp(10), (h-gtx.Dp(20))/2), func(gtx C) D { return v.icon(gtx, "search", 20, v.p.Muted) })
	x := gtx.Dp(38)
	tw := max(1, w-x-gtx.Dp(12))
	dirAt(gtx, image.Pt(x, 0), func(gtx C) D {
		return vmid(gtx, h, func(gtx C) D {
			gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, h)}
			if v.search.Len() == 0 {
				dirAt(gtx, image.Point{}, func(gtx C) D {
					return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: wReg, Color: v.p.Muted, MaxLines: 1},
						v.tabs[v.sel].placeholder())
				})
			}
			return v.th.Editor(gtx, &v.search, 15, v.p.Text, v.p.SelBg)
		})
	})
	return D{Size: r.Max}
}

// sidebar is UnifiedFilePickerSidebar.
func (v *FilePicker) sidebar(gtx C, w int) D {
	v.mu.Lock()
	counts := make(map[string]int, len(v.counts))
	for k, n := range v.counts {
		counts[k] = n
	}
	v.mu.Unlock()
	h := gtx.Dp(32)
	for i, t := range v.tabs {
		sel := i == v.sel
		dirAt(gtx, image.Pt(0, i*h), func(gtx C) D {
			return v.sideBtns[i].Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				hov := v.sideBtns[i].Hovered()
				r := image.Rect(0, 0, w, h)
				fg := v.p.Text
				wt := wReg
				switch {
				case sel:
					rrect(gtx, r, gtx.Dp(4), v.lt(rgb(0x1264a3), rgb(0x1d9bd1))) // base-inv-hgl-1
					fg, wt = rgb(0xffffff), wBold
				case hov:
					rrect(gtx, r, gtx.Dp(4), v.p.Surface) // surf-pry
				}
				if !sel && !hov && t.id != "files" { // the tree line at 16
					fill(gtx, image.Rect(gtx.Dp(16), 0, gtx.Dp(18), h), v.lt(rgb(0xffffff), rgb(0x1a1d21)))
				}
				x := gtx.Dp(8)
				if t.id == "files" {
					dirAt(gtx, image.Pt(x+gtx.Dp(2), (h-gtx.Dp(16))/2), func(gtx C) D { return v.icon(gtx, "slack-logo", 16, fg) })
				}
				x += gtx.Dp(28)
				cw := 0
				label := ""
				if n, ok := counts[t.id]; ok && t.id != "files" {
					label = strconv.Itoa(n)
				}
				cc := v.p.Muted
				if sel {
					cc = rgb(0xffffff)
				}
				if label != "" {
					cw = v.th.Measure(gtx, 13, wt, label)
					dirAt(gtx, image.Pt(w-gtx.Dp(16)-cw, 0), func(gtx C) D {
						return vmid(gtx, h, func(gtx C) D {
							return v.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Weight: wt, Color: cc, MaxLines: 1}, label)
						})
					})
				}
				dirAt(gtx, image.Pt(x, 0), func(gtx C) D {
					gtx.Constraints = layout.Constraints{Max: image.Pt(max(0, w-x-gtx.Dp(16)-cw-gtx.Dp(4)), h)}
					return vmid(gtx, h, func(gtx C) D {
						return v.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Weight: wt, Color: fg, MaxLines: 1}, t.title)
					})
				})
				return D{Size: r.Max}
			})
		})
	}
	return D{Size: image.Pt(w, len(v.tabs)*h)}
}

// selectButton is a controls select: its value and a caret.
func (v *FilePicker) selectButton(gtx C, c *widget.Clickable, label string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		lw := v.th.Measure(gtx, 13, wBold, label)
		w, h := gtx.Dp(12)+lw+gtx.Dp(4)+gtx.Dp(16)+gtx.Dp(8), gtx.Dp(28)
		r := image.Rect(0, 0, w, h)
		bg := v.p.Surface
		if c.Hovered() {
			bg = theme.Over(v.p.Hover, v.p.Surface)
		}
		rrect(gtx, r, gtx.Dp(8), bg)
		border(gtx, r, gtx.Dp(8), max(1, gtx.Dp(1)), v.p.SegLine)
		dirAt(gtx, image.Pt(gtx.Dp(12), 0), func(gtx C) D {
			return vmid(gtx, h, func(gtx C) D {
				return v.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Weight: wBold, Color: v.p.Text, MaxLines: 1}, label)
			})
		})
		dirAt(gtx, image.Pt(gtx.Dp(12)+lw+gtx.Dp(4), (h-gtx.Dp(16))/2), func(gtx C) D { return v.icon(gtx, "caret-down", 16, v.p.Text) })
		return D{Size: r.Max}
	})
}

// results is the explorer's list: the controls, the files, the empty
// state.
func (v *FilePicker) results(gtx C, w, h int) D {
	v.mu.Lock()
	files := append([]core.File(nil), v.files...)
	loading := v.loading
	v.mu.Unlock()
	// controls
	ch := gtx.Dp(28)
	dirAt(gtx, image.Point{}, func(gtx C) D {
		d := v.selectButton(gtx, &v.filterBtn, nsBrowsers.T("Filter")+": "+v.filterLabel())
		v.filterSz = d.Size
		return d
	})
	sl := nsBrowsers.T("Sort") + ": " + v.sortLabel()
	sw := gtx.Dp(12) + v.th.Measure(gtx, 13, wBold, sl) + gtx.Dp(28)
	dirAt(gtx, image.Pt(w-sw, 0), func(gtx C) D {
		d := v.selectButton(gtx, &v.sortBtn, sl)
		v.sortSz = d.Size
		return d
	})
	y := ch + gtx.Dp(12)
	lh := h - y
	switch {
	case loading && len(files) == 0:
		for i := 0; i < 5; i++ {
			rh := gtx.Dp(60)
			o := image.Pt(gtx.Dp(12), y+i*rh+(rh-gtx.Dp(36))/2)
			rrect(gtx, image.Rectangle{Min: o, Max: o.Add(image.Pt(gtx.Dp(36), gtx.Dp(36)))}, gtx.Dp(8), v.p.Skel)
			rrect(gtx, image.Rect(o.X+gtx.Dp(48), o.Y+gtx.Dp(4), o.X+gtx.Dp(48)+w*40/100, o.Y+gtx.Dp(16)), gtx.Dp(4), v.p.Skel)
			rrect(gtx, image.Rect(o.X+gtx.Dp(48), o.Y+gtx.Dp(22), o.X+gtx.Dp(48)+w*25/100, o.Y+gtx.Dp(32)), gtx.Dp(4), v.p.Skel)
		}
	case len(files) == 0:
		dirAt(gtx, image.Pt(0, y+gtx.Dp(80)), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, lh)}
			return col(gtx,
				layout.Rigid(func(gtx C) D {
					return v.th.Layout(gtx, theme.TextStyle{Size: 18, LineHeight: 24, Weight: wBlack, Color: v.p.Text, Align: text.Middle}, nsUFP.T("No results"))
				}),
				layout.Rigid(func(gtx C) D {
					return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: wReg, Color: v.p.Muted, Align: text.Middle},
						nsUFP.T("You may want to try adjusting your filters."))
				}),
			)
		})
	default:
		dirAt(gtx, image.Pt(0, y), func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(w, lh))
			return v.scrollList(gtx, &v.list, len(files), func(gtx C, i int) D { return v.resultRow(gtx, files[i], i, len(files)) })
		})
	}
	return D{Size: image.Pt(w, h)}
}

// resultRow is a unified-file-result row: the file's icon, title and
// line, the picked one active.
func (v *FilePicker) resultRow(gtx C, f core.File, i, n int) D {
	r := v.rows[f.ID]
	if r == nil {
		r = new(widget.Clickable)
		v.rows[f.ID] = r
	}
	fi := fileItem{File: f}
	w := gtx.Constraints.Max.X
	h := gtx.Dp(60)
	return r.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		active := v.picked == f.ID
		box := image.Rect(0, 0, w, h)
		fg, sec := v.p.Text, v.p.Muted
		switch {
		case active: // p-explorer_result--active
			rrect(gtx, box, gtx.Dp(8), v.lt(rgb(0x1264a3), rgb(0x1d9bd1)))
			fg, sec = rgb(0xffffff), rgb(0xffffff)
		case r.Hovered():
			rrect(gtx, box, gtx.Dp(8), theme.Over(v.p.Hover, v.p.Surface))
		}
		dirAt(gtx, image.Pt(gtx.Dp(12), (h-gtx.Dp(36))/2), func(gtx C) D { return v.fv.fileTile(gtx, &fi, unit.Dp(36)) })
		tx := gtx.Dp(12) + gtx.Dp(36) + gtx.Dp(12)
		tw := max(0, w-tx-gtx.Dp(12))
		dirAt(gtx, image.Pt(tx, (h-gtx.Dp(40))/2), func(gtx C) D {
			gtx.Constraints.Max.X = tw
			return col(gtx,
				layout.Rigid(func(gtx C) D {
					return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: wBold, Color: fg, MaxLines: 1}, fi.title())
				}),
				layout.Rigid(func(gtx C) D {
					return v.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Weight: wReg, Color: sec, MaxLines: 1}, v.cf.fileDetail(&fi))
				}),
			)
		})
		return D{Size: box.Max}
	})
}
