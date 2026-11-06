package views

import (
	"context"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// Files is Slack's Files tab: a left nav (All files, Canvases, Lists),
// then the file browser with search, type filter chips and a list/grid
// switch.
type Files struct {
	base
	navClicks [3]widget.Clickable
	nav       int // 0 all, 1 canvases, 2 lists

	search  *search
	deb     debounce
	query   string
	types   *pills
	mine    toggle
	gridBtn widget.Clickable
	listBtn widget.Clickable
	grid    bool
	retry   widget.Clickable

	data    async[filePage]
	more    async[filePage]
	items   []fileItem
	page    int
	pages   int
	list    widget.List
	clicks  map[string]*widget.Clickable
	menus   map[string]*widget.Clickable
	reload  bool
	lastKey string
}

var fileNav = [3]struct{ label, icon, kind string }{
	{"All files", "files", ""},
	{"Canvases", "canvas", "canvases"},
	{"Lists", "list-view", "lists"},
}

// typeFilter labels and their fileTypes keys (All files only).
var fileTypeLabels = []string{"All types", "Images", "PDFs", "Snippets", "Documents", "Zips"}
var fileTypeKinds = []string{"", "images", "pdfs", "snippets", "docs", "zips"}

// NewFiles builds the Files view (route "files").
func NewFiles(h Host) *Files {
	f := &Files{base: newBase(h), search: newSearch("Search files"), types: newPills(fileTypeLabels...),
		clicks: map[string]*widget.Clickable{}, menus: map[string]*widget.Clickable{}, reload: true}
	f.list.Axis = layout.Vertical
	return f
}

func (f *Files) kind() string {
	if f.nav > 0 {
		return fileNav[f.nav].kind
	}
	return fileTypeKinds[f.types.Sel]
}

func (f *Files) start() {
	kind, q, mine := f.kind(), f.query, f.mine.Value
	a := f.h.Core()
	f.page, f.pages = 1, 0
	f.data.load(f.h, false, func(ctx context.Context) (filePage, error) {
		return listFiles(ctx, a, kind, q, mine, 1)
	})
}

func (f *Files) loadMore() {
	if f.more.busy() || f.page >= f.pages {
		return
	}
	kind, q, mine, next := f.kind(), f.query, f.mine.Value, f.page+1
	a := f.h.Core()
	f.more.load(f.h, false, func(ctx context.Context) (filePage, error) {
		return listFiles(ctx, a, kind, q, mine, next)
	})
}

func (f *Files) update(gtx C) {
	for i := range f.navClicks {
		if f.navClicks[i].Clicked(gtx) && f.nav != i {
			f.nav, f.reload = i, true
		}
	}
	if f.types.update(gtx) {
		f.reload = true
	}
	if f.mine.Update(gtx) {
		f.reload = true
	}
	if f.gridBtn.Clicked(gtx) {
		f.grid = true
	}
	if f.listBtn.Clicked(gtx) {
		f.grid = false
	}
	if f.retry.Clicked(gtx) {
		f.reload = true
	}
	if t, changed, submit := f.search.update(gtx); changed || submit {
		f.deb.set(t, gtx.Now)
		if submit {
			f.deb.at = time.Time{}
		}
	}
	if q, ok := f.deb.due(gtx, 300*time.Millisecond); ok && q != f.query {
		f.query, f.reload = q, true
	}
	if f.reload {
		f.reload = false
		f.start()
	}
	// fold in pages as they arrive
	if p, ok, err := f.data.get(); ok && err == nil {
		key := pageKey(p)
		if key != f.lastKey {
			f.lastKey = key
			f.items = append(f.items[:0], p.Files...)
			f.page, f.pages = max(p.Page, 1), p.Pages
		}
	}
	if p, ok, err := f.more.get(); ok && err == nil && len(p.Files) > 0 && p.Page == f.page+1 {
		f.items = append(f.items, p.Files...)
		f.page, f.pages = p.Page, p.Pages
		f.more.mutate(func(v *filePage) { v.Files = nil })
	}
	for _, it := range f.items {
		c := f.clicks[it.ID]
		if c == nil {
			continue
		}
		if c.Clicked(gtx) {
			f.open(it)
		}
		if m := f.menus[it.ID]; m != nil && m.Clicked(gtx) {
			f.h.Toast("File actions aren't supported yet")
		}
	}
}

func pageKey(p filePage) string {
	if len(p.Files) == 0 {
		return "empty"
	}
	return p.Files[0].ID + "/" + p.Files[len(p.Files)-1].ID
}

func (f *Files) open(it fileItem) {
	switch {
	case it.isImage() && it.URL != "":
		f.h.OpenLightbox(richtext.Image{URL: it.URL, Alt: it.title(), W: it.ThumbW, H: it.ThumbH, Title: it.title(), Proxy: true}, core.Message{})
	case len(it.Channels) > 0:
		f.h.Navigate("c/" + it.Channels[0])
	case it.isCanvas():
		f.h.Toast("Opening canvases isn't supported yet")
	default:
		f.h.Toast("Opening this file isn't supported yet")
	}
}

// Layout draws the view.
func (f *Files) Layout(gtx C) D {
	f.frame()
	f.update(gtx)
	return row(gtx, layout.Start,
		layout.Rigid(f.layoutNav),
		layout.Rigid(func(gtx C) D { return vline(gtx, f.p.Border) }),
		layout.Flexed(1, f.layoutMain),
	)
}

// layoutNav is the 260px Files sidebar.
func (f *Files) layoutNav(gtx C) D {
	w := gtx.Dp(260)
	size := image.Pt(w, gtx.Constraints.Max.Y)
	gtx.Constraints = layout.Exact(size)
	col(gtx,
		layout.Rigid(func(gtx C) D { return f.header(gtx, "Files") }),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 8, Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
				ch := make([]layout.FlexChild, 0, len(fileNav))
				for i := range fileNav {
					i := i
					ch = append(ch, layout.Rigid(func(gtx C) D {
						sel := f.nav == i
						return f.hoverable(gtx, &f.navClicks[i], sel, 6, func(gtx C) D {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							fg := f.p.Text
							if sel {
								fg = f.p.SelText
							}
							g := gtx
							g.Constraints.Min.Y = gtx.Dp(28)
							return layout.Inset{Left: 12, Right: 12}.Layout(g, func(gtx C) D {
								return layout.W.Layout(gtx, func(gtx C) D {
									return row(gtx, layout.Middle,
										layout.Rigid(func(gtx C) D { return f.icon(gtx, fileNav[i].icon, 18, fg) }),
										hgap(8),
										layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wReg, fg, fileNav[i].label) }),
									)
								})
							})
						})
					}))
					ch = append(ch, vgap(2))
				}
				return col(gtx, ch...)
			})
		}),
	)
	return D{Size: size}
}

func (f *Files) layoutMain(gtx C) D {
	gtx.Constraints.Min = gtx.Constraints.Max
	title := fileNav[f.nav].label
	return col(gtx,
		layout.Rigid(func(gtx C) D {
			return f.header(gtx, title,
				layout.Rigid(func(gtx C) D { return f.viewSwitch(gtx) }),
			)
		}),
		layout.Rigid(func(gtx C) D { return hline(gtx, f.p.Border) }),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 16, Left: 20, Right: 20, Bottom: 12}.Layout(gtx, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D { return f.searchBox(gtx, f.search, 36) }),
					vgap(12),
					layout.Rigid(func(gtx C) D {
						return row(gtx, layout.Middle,
							layout.Rigid(func(gtx C) D {
								if f.nav != 0 {
									return D{}
								}
								return f.pillRow(gtx, f.types)
							}),
							layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
							layout.Rigid(func(gtx C) D { return f.toggleSwitch(gtx, &f.mine, "Created by you") }),
						)
					}),
				)
			})
		}),
		layout.Flexed(1, f.layoutBody),
	)
}

// viewSwitch is the list/grid segmented control in the header.
func (f *Files) viewSwitch(gtx C) D {
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D {
			c := f.p.Muted
			if !f.grid {
				c = f.p.Text
			}
			return f.segment(gtx, &f.listBtn, !f.grid, "list-view", c)
		}),
		layout.Rigid(func(gtx C) D {
			c := f.p.Muted
			if f.grid {
				c = f.p.Text
			}
			return f.segment(gtx, &f.gridBtn, f.grid, "apps", c)
		}),
	)
}

func (f *Files) segment(gtx C, c *widget.Clickable, sel bool, icon string, fg color.NRGBA) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		s := gtx.Dp(28)
		r := image.Rect(0, 0, s, s)
		if sel {
			rrect(gtx, r, gtx.Dp(6), f.p.Chip)
		} else if c.Hovered() {
			rrect(gtx, r, gtx.Dp(6), f.p.Hover)
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(s, s))
		layout.Center.Layout(g, func(gtx C) D { return f.icon(gtx, icon, 18, fg) })
		return D{Size: image.Pt(s, s)}
	})
}

func (f *Files) layoutBody(gtx C) D {
	_, loaded, err := f.data.get()
	switch {
	case !loaded:
		return f.skeleton(gtx, 8, 36)
	case err != nil && len(f.items) == 0:
		return f.errorState(gtx, err, &f.retry)
	case len(f.items) == 0:
		return f.empty(gtx)
	}
	if f.grid {
		return f.layoutGrid(gtx)
	}
	return f.scrollList(gtx, &f.list, len(f.items)+1, func(gtx C, i int) D {
		if i == len(f.items) {
			return f.footer(gtx)
		}
		if i > len(f.items)-8 {
			f.loadMore()
		}
		return f.fileRow(gtx, &f.items[i])
	})
}

func (f *Files) empty(gtx C) D {
	switch {
	case f.query != "":
		return f.emptyState(gtx, "search", "No results", "Try different keywords or remove filters.", nil, "")
	case f.nav == 1:
		return f.emptyState(gtx, "canvas", "No canvases yet", "Canvases you create or that are shared with you will show up here.", nil, "")
	case f.nav == 2:
		return f.emptyState(gtx, "list-view", "No lists yet", "Lists you create or that are shared with you will show up here.", nil, "")
	}
	return f.emptyState(gtx, "files", "No files yet", "Files shared in your conversations will show up here.", nil, "")
}

func (f *Files) footer(gtx C) D {
	if f.more.busy() {
		return layout.Inset{Top: 12, Bottom: 24}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return f.centered(gtx, 13, wReg, f.p.Muted, "Loading…")
		})
	}
	return D{Size: image.Pt(0, gtx.Dp(24))}
}

func (f *Files) clickOf(id string) (*widget.Clickable, *widget.Clickable) {
	c := f.clicks[id]
	if c == nil {
		c = new(widget.Clickable)
		f.clicks[id] = c
	}
	m := f.menus[id]
	if m == nil {
		m = new(widget.Clickable)
		f.menus[id] = m
	}
	return c, m
}

// fileRow: 36px type tile, bold title, muted "<creator> · <date> · <type>".
func (f *Files) fileRow(gtx C, it *fileItem) D {
	c, menu := f.clickOf(it.ID)
	return layout.Inset{Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
		return f.hoverable(gtx, c, false, 8, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Inset{Top: 8, Bottom: 8, Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return f.fileTile(gtx, it, 36) }),
					hgap(12),
					layout.Flexed(1, func(gtx C) D {
						return col(gtx,
							layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wBold, f.p.Text, it.title()) }),
							vgap(2),
							layout.Rigid(func(gtx C) D { return f.label(gtx, 13, wReg, f.p.Muted, f.fileMeta(it)) }),
						)
					}),
					layout.Rigid(func(gtx C) D {
						if !c.Hovered() {
							return D{Size: image.Pt(gtx.Dp(28), 0)}
						}
						return f.iconButton(gtx, menu, "more", 28, 18, f.p.Muted)
					}),
				)
			})
		})
	})
}

func (f *Files) fileMeta(it *fileItem) string {
	parts := make([]string, 0, 3)
	if it.User != "" {
		parts = append(parts, f.userName(it.User))
	}
	if t := listTime(unixTime(it.Created)); t != "" {
		parts = append(parts, t)
	}
	if t := fileKindLabel(it); t != "" {
		parts = append(parts, t)
	}
	return strings.Join(parts, "  ·  ")
}

func fileKindLabel(it *fileItem) string {
	switch {
	case it.isCanvas():
		return "Canvas"
	case it.isList():
		return "List"
	case it.PrettyType != "":
		return it.PrettyType
	}
	return strings.ToUpper(it.Filetype)
}

// fileTile draws the thumbnail (images) or a colored type tile.
func (f *Files) fileTile(gtx C, it *fileItem, size unit.Dp) D {
	s := gtx.Dp(size)
	if it.Thumb != "" && it.isImage() && f.h.Images() != nil {
		if img, ok := f.h.Images().Get(it.Thumb, s*2); ok {
			return themeImage(gtx, img, image.Pt(s, s), gtx.Dp(8))
		}
		return f.th.Placeholder(gtx, image.Pt(s, s), gtx.Dp(8))
	}
	bg, icon := fileTileStyle(it)
	rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), bg)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(s, s))
	layout.Center.Layout(g, func(gtx C) D { return f.icon(gtx, icon, size*0.55, rgb(0xffffff)) })
	return D{Size: image.Pt(s, s)}
}

// fileTileStyle is the per-type tile color Slack uses.
func fileTileStyle(it *fileItem) (color.NRGBA, string) {
	switch {
	case it.isCanvas():
		return rgb(0x2eb67d), "canvas"
	case it.isList():
		return rgb(0x7c3085), "list-view"
	case it.Filetype == "pdf":
		return rgb(0xe01e5a), "files"
	case it.isImage():
		return rgb(0x1264a3), "image"
	case strings.HasPrefix(it.Mime, "video/"):
		return rgb(0x1264a3), "video"
	case strings.HasPrefix(it.Mime, "audio/"):
		return rgb(0x7c3085), "volume"
	case it.Filetype == "zip" || it.Filetype == "gzip" || it.Filetype == "tar":
		return rgb(0x616061), "files"
	case it.Filetype == "text" || it.Mode == "snippet" || isCode(it.Filetype):
		return rgb(0x1d1c1d), "code"
	}
	return rgb(0x1264a3), "files"
}

func isCode(ft string) bool {
	switch ft {
	case "go", "javascript", "python", "c", "cpp", "java", "rust", "shell", "json", "yaml", "html", "css", "markdown", "typescript", "ruby", "php", "sql", "swift", "kotlin":
		return true
	}
	return false
}

// layoutGrid lays cards in a responsive grid (min 220px columns, 16 gap).
func (f *Files) layoutGrid(gtx C) D {
	pad := gtx.Dp(20)
	gap := gtx.Dp(16)
	avail := gtx.Constraints.Max.X - 2*pad
	cols := max(1, (avail+gap)/(gtx.Dp(220)+gap))
	cw := (avail - (cols-1)*gap) / cols
	rows := (len(f.items) + cols - 1) / cols
	return f.scrollList(gtx, &f.list, rows+1, func(gtx C, r int) D {
		if r == rows {
			return f.footer(gtx)
		}
		if r > rows-3 {
			f.loadMore()
		}
		return layout.Inset{Left: unit.Dp(20), Right: unit.Dp(20), Bottom: unit.Dp(16)}.Layout(gtx, func(gtx C) D {
			ch := make([]layout.FlexChild, 0, cols*2)
			for c := 0; c < cols; c++ {
				i := r*cols + c
				if c > 0 {
					ch = append(ch, layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gap, 0)} }))
				}
				ch = append(ch, layout.Rigid(func(gtx C) D {
					if i >= len(f.items) {
						return D{Size: image.Pt(cw, 0)}
					}
					g := gtx
					g.Constraints = layout.Exact(image.Pt(cw, gtx.Constraints.Max.Y))
					g.Constraints.Min.Y = 0
					return f.fileCard(g, &f.items[i], cw)
				}))
			}
			return row(gtx, layout.Start, ch...)
		})
	})
}

func (f *Files) fileCard(gtx C, it *fileItem, w int) D {
	c, _ := f.clickOf(it.ID)
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		th := w * 3 / 4
		rad := gtx.Dp(8)
		bw := max(1, gtx.Dp(1))
		thumbR := image.Rect(0, 0, w, th)
		bg := f.p.Hover
		if c.Hovered() {
			bg = f.p.Chip
		}
		rrect(gtx, thumbR, rad, bg)
		if it.Thumb != "" && f.h.Images() != nil {
			if img, ok := f.h.Images().Get(it.Thumb, w); ok {
				themeImage(gtx, img, thumbR.Size(), rad)
			}
		} else {
			tile, icon := fileTileStyle(it)
			s := gtx.Dp(48)
			func() {
				defer opOffset(gtx, image.Pt((w-s)/2, (th-s)/2)).Pop()
				rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(10), tile)
				g := gtx
				g.Constraints = layout.Exact(image.Pt(s, s))
				layout.Center.Layout(g, func(gtx C) D { return f.icon(gtx, icon, 26, rgb(0xffffff)) })
			}()
		}
		border(gtx, thumbR, rad, bw, f.p.Border)
		func() {
			defer opOffset(gtx, image.Pt(0, th+gtx.Dp(8))).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(w, gtx.Dp(60))}
			col(g,
				layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wBold, f.p.Text, it.title()) }),
				vgap(2),
				layout.Rigid(func(gtx C) D { return f.label(gtx, 13, wReg, f.p.Muted, f.fileMeta(it)) }),
			)
		}()
		return D{Size: image.Pt(w, th+gtx.Dp(8)+gtx.Dp(44))}
	})
}
