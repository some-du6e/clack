package views

import (
	"context"
	"html"
	"image"
	"image/color"
	"regexp"
	"sort"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Files is the 2026 Files rail tab: a themed Files sidebar (All files,
// Canvases, Lists, Deleted, Starred) next to a #f8f8f8 primary pane with
// search, the Templates strip, segment buttons (All / Created by you /
// Shared with you), the Types menu, the sort select, and file cards.
//
// The shell can draw the whole view with Layout, or host LayoutNav in its
// sidebar slot and LayoutMain in the primary pane.
type Files struct {
	base
	nav       int // filesNav index
	navClicks [len(filesNav)]widget.Clickable
	gear      widget.Clickable
	plus      widget.Clickable
	newBtn    widget.Clickable
	emptyNew  widget.Clickable

	search *search
	deb    debounce
	query  string

	tplOpen   bool
	tplToggle widget.Clickable
	seeAll    widget.Clickable
	tplClicks [len(fileTemplates)]widget.Clickable
	tplList   widget.List

	seg       int
	segClicks [3]widget.Clickable

	typesOpen  bool
	typesBtn   widget.Clickable
	typesScrim widget.Clickable
	typeOn     [len(fileTypeMenu)]bool
	typeClicks [len(fileTypeMenu)]widget.Clickable

	sortOpen   bool
	sortBtn    widget.Clickable
	sortScrim  widget.Clickable
	sortSel    int
	sortClicks [2]widget.Clickable
	filtersBtn widget.Clickable

	retry  widget.Clickable
	data   async[filePage]
	more   async[filePage]
	items  []fileItem
	page   int
	pages  int
	list   widget.List
	clicks map[string]*fileClicks
	reload bool
}

type fileClicks struct {
	row, star, menu, owner widget.Clickable
}

var filesNav = [...]struct{ label, icon, kind, title, placeholder string }{
	{"All files", "files", "", "All files", "Search files"},
	{"Canvases", "canvas", "canvases", "Canvases", "Search canvases"},
	{"Lists", "list-view", "lists", "Lists", "Search lists"},
	{"Deleted", "trash", "deleted", "Deleted", "Search deleted files"},
}

// Template tiles: UI strings from Slack's Files page (canvas and list
// templates). Creating from a template isn't supported.
var fileTemplates = [...]struct {
	title, desc string
	list        bool
}{
	{"Employee Onboarding", "Welcome new people.", false},
	{"Project tracker", "Manage and monitor tasks as a team", true},
	{"Monthly Newsletter", "Broadcast your announcements.", false},
	{"Feedback tracker", "A streamlined approach to keeping track of feedback from coworkers", true},
	{"Out of Office Coverage Plan", "Share how your work will be covered.", false},
	{"Quarterly plan", "A simple format for planning which projects to tackle", false},
}

// The Types menu (All files). The first six are on by default, like Slack.
var fileTypeMenu = [...]struct {
	label, icon string
	match       func(f *fileItem) bool
}{
	{"Lists", "list-view", func(f *fileItem) bool { return f.isList() }},
	{"Canvases and Documents", "canvas", func(f *fileItem) bool { return f.isCanvas() || isDocType(f) }},
	{"Spreadsheets", "list-view", func(f *fileItem) bool { return isSheet(f) }},
	{"Presentations", "image", func(f *fileItem) bool { return isSlides(f) }},
	{"PDFs", "files", func(f *fileItem) bool { return f.Filetype == "pdf" }},
	{"Skill Sets", "sparkle", func(f *fileItem) bool { return false }},
	{"Audio", "volume", func(f *fileItem) bool { return strings.HasPrefix(f.Mime, "audio/") }},
	{"Videos", "video", func(f *fileItem) bool { return strings.HasPrefix(f.Mime, "video/") }},
	{"Images", "image", func(f *fileItem) bool { return f.isImage() }},
	{"Snippets", "code", func(f *fileItem) bool { return f.Mode == "snippet" || isCode(f.Filetype) || f.Filetype == "text" }},
	{"Emails", "mail", func(f *fileItem) bool { return f.Filetype == "email" || f.Mode == "email" }},
}

var fileSorts = [2]string{"Recently viewed", "Last updated"}

// NewFiles builds the Files view (route "files").
func NewFiles(h Host) *Files {
	f := &Files{base: newBase(h), search: newSearch(filesNav[0].placeholder), clicks: map[string]*fileClicks{}, reload: true, tplOpen: true}
	for i := 0; i < 6; i++ {
		f.typeOn[i] = true
	}
	f.list.Axis = layout.Vertical
	f.tplList.Axis = layout.Horizontal
	return f
}

func (f *Files) kind() string { return filesNav[f.nav].kind }

func (f *Files) start() {
	f.items, f.page, f.pages = f.items[:0], 0, 0
	a := f.h.Core()
	if f.kind() == "deleted" {
		f.data.load(f.h, false, func(ctx context.Context) (filePage, error) { return deletedFiles(ctx, a) })
		return
	}
	q := fileQuery{Kind: f.kind(), Query: f.query, Mine: f.seg == 1, Page: 1}
	f.data.load(f.h, false, func(ctx context.Context) (filePage, error) { return listFiles(ctx, a, q) })
}

func (f *Files) loadMore() {
	if f.kind() == "deleted" || f.more.busy() || f.data.busy() || f.page == 0 || f.page >= f.pages {
		return
	}
	a := f.h.Core()
	q := fileQuery{Kind: f.kind(), Query: f.query, Mine: f.seg == 1, Page: f.page + 1}
	f.more.load(f.h, false, func(ctx context.Context) (filePage, error) { return listFiles(ctx, a, q) })
}

// visible reports whether a loaded file passes the client-side filters.
func (f *Files) visible(it *fileItem) bool {
	switch f.kind() {
	case "canvases":
		if !it.isCanvas() {
			return false
		}
	case "lists":
		if !it.isList() {
			return false
		}
	case "":
		hit, all := false, true
		for i := range fileTypeMenu {
			if !f.typeOn[i] {
				all = false
				continue
			}
			if fileTypeMenu[i].match(it) {
				hit = true
			}
		}
		if !all && !hit {
			return false
		}
	}
	if f.seg == 2 && it.User == f.selfID() {
		return false
	}
	return true
}

func (f *Files) typeCount() int {
	n := 0
	for _, on := range f.typeOn {
		if on {
			n++
		}
	}
	return n
}

func (f *Files) update(gtx C) {
	for i := range f.navClicks {
		if f.navClicks[i].Clicked(gtx) && f.nav != i {
			f.nav, f.reload = i, true
			f.search.ph = filesNav[i].placeholder
		}
	}
	for i := range f.segClicks {
		if f.segClicks[i].Clicked(gtx) && f.seg != i {
			// "Created by you" filters server-side; the others client-side.
			if f.seg == 1 || i == 1 {
				f.reload = true
			}
			f.seg = i
		}
	}
	if f.gear.Clicked(gtx) {
		f.h.Toast("File settings aren't supported yet")
	}
	if f.plus.Clicked(gtx) || f.newBtn.Clicked(gtx) || f.emptyNew.Clicked(gtx) {
		f.h.Toast("Creating canvases and lists isn't supported yet")
	}
	if f.tplToggle.Clicked(gtx) {
		f.tplOpen = !f.tplOpen
	}
	if f.seeAll.Clicked(gtx) {
		f.h.Toast("Templates aren't supported yet")
	}
	for i := range f.tplClicks {
		if f.tplClicks[i].Clicked(gtx) {
			f.h.Toast("Creating from a template isn't supported yet")
		}
	}
	if f.typesBtn.Clicked(gtx) {
		f.typesOpen = !f.typesOpen
	}
	for i := range f.typeClicks {
		if f.typeClicks[i].Clicked(gtx) {
			f.typeOn[i] = !f.typeOn[i]
		}
	}
	if f.sortBtn.Clicked(gtx) {
		f.sortOpen = !f.sortOpen
	}
	for i := range f.sortClicks {
		if f.sortClicks[i].Clicked(gtx) {
			f.sortSel, f.sortOpen = i, false
			f.sortItems()
		}
	}
	if f.filtersBtn.Clicked(gtx) {
		f.h.Toast("Search filters aren't supported here yet; use search modifiers like from:@name")
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
	if p, ok, err := f.data.get(); ok && err == nil && f.page == 0 {
		f.items = append(f.items[:0], f.cleanTitles(p.Files)...)
		f.page, f.pages = max(p.Page, 1), p.Pages
		f.sortItems()
	}
	if p, ok, err := f.more.get(); ok && err == nil && p.Page == f.page+1 {
		f.items = append(f.items, f.cleanTitles(p.Files)...)
		f.page, f.pages = p.Page, p.Pages
		f.sortItems()
	}
	for i := range f.items {
		it := &f.items[i]
		c := f.clicks[it.ID]
		if c == nil {
			continue
		}
		if c.row.Clicked(gtx) {
			f.open(*it)
		}
		if c.star.Clicked(gtx) {
			f.h.Toast("Starring files isn't supported yet")
		}
		if c.menu.Clicked(gtx) {
			f.h.Toast("File actions aren't supported yet")
		}
		if c.owner.Clicked(gtx) && it.User != "" {
			f.h.OpenProfile(image.Point{}, it.User)
		}
	}
}

func (f *Files) sortItems() {
	if f.sortSel == 1 {
		sort.SliceStable(f.items, func(i, j int) bool {
			return fileUpdated(&f.items[i]) > fileUpdated(&f.items[j])
		})
		return
	}
	sort.SliceStable(f.items, func(i, j int) bool { return f.items[i].Created > f.items[j].Created })
}

func fileUpdated(it *fileItem) int64 {
	if it.Modified > 0 {
		return it.Modified
	}
	return it.Created
}

func (f *Files) open(it fileItem) {
	switch {
	case it.isImage() && it.URL != "":
		f.h.OpenLightbox(richtext.Image{URL: it.URL, Alt: it.title(), W: it.ThumbW, H: it.ThumbH, Title: it.title(), Proxy: true}, core.Message{})
	case len(it.Channels) > 0:
		f.h.Navigate("c/" + it.Channels[0])
	case it.isCanvas():
		f.h.Toast("Opening canvases isn't supported yet")
	case it.isList():
		f.h.Toast("Opening lists isn't supported yet")
	default:
		f.h.Toast("Opening this file isn't supported yet")
	}
}

// Layout draws the Files sidebar and the primary pane side by side.
func (f *Files) Layout(gtx C) D {
	f.frame()
	f.update(gtx)
	gtx.Constraints.Min = gtx.Constraints.Max
	return row(gtx, layout.Start,
		layout.Rigid(func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(328), gtx.Constraints.Max.Y))
			return f.nav_(gtx)
		}),
		layout.Flexed(1, f.main),
	)
}

// LayoutNav draws only the themed Files sidebar (fills the constraints).
func (f *Files) LayoutNav(gtx C) D {
	f.frame()
	f.update(gtx)
	return f.nav_(gtx)
}

// LayoutMain draws only the primary pane.
func (f *Files) LayoutMain(gtx C) D {
	f.frame()
	f.update(gtx)
	return f.main(gtx)
}

// ---- themed sidebar ----

var (
	filesChrome   = rgb(0x3f0e40)
	filesNavText  = argb(0xe6f6e4ff)
	filesNavSelBg = rgb(0xf9edff)
	filesNavSelFg = rgb(0x39063a)
	filesNavHover = argb(0x14f9edff)
	filesNavRule  = argb(0x38f9edff)
)

func (f *Files) nav_(gtx C) D {
	size := gtx.Constraints.Max
	chrome := filesChrome
	if f.th.Dark {
		chrome = rgb(0x1a1d21)
	}
	fill(gtx, image.Rectangle{Max: size}, chrome)
	if f.th.C.SidebarBg.A > 0 {
		fill(gtx, image.Rectangle{Max: size}, f.th.C.SidebarBg)
	}
	gtx.Constraints = layout.Exact(size)
	navItem := func(i int) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			c := &f.navClicks[i]
			return c.Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				w, h := gtx.Constraints.Max.X, gtx.Dp(28)
				sel := f.nav == i
				fg := filesNavText
				switch {
				case sel:
					rrect(gtx, image.Rect(0, 0, w, h), gtx.Dp(4), filesNavSelBg)
					fg = filesNavSelFg
				case c.Hovered():
					rrect(gtx, image.Rect(0, 0, w, h), gtx.Dp(4), filesNavHover)
				}
				g := gtx
				g.Constraints = layout.Exact(image.Pt(w, h))
				layout.Inset{Left: 8, Right: 8}.Layout(g, func(gtx C) D {
					return layout.W.Layout(gtx, func(gtx C) D {
						return row(gtx, layout.Middle,
							layout.Rigid(func(gtx C) D { return f.icon(gtx, filesNav[i].icon, 16, fg) }),
							hgap(8),
							layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wReg, fg, filesNav[i].label) }),
						)
					})
				})
				return D{Size: image.Pt(w, h)}
			})
		})
	}
	rule := layout.Rigid(func(gtx C) D {
		return layout.Inset{Top: 12, Bottom: 12}.Layout(gtx, func(gtx C) D { return hline(gtx, filesNavRule) })
	})
	return layout.Inset{Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
		return col(gtx,
			layout.Rigid(func(gtx C) D {
				g := gtx
				g.Constraints.Min.Y = gtx.Dp(52)
				return layout.Inset{Top: 12, Bottom: 12, Left: 8}.Layout(g, func(gtx C) D {
					return row(gtx, layout.Middle,
						layout.Flexed(1, func(gtx C) D { return f.label(gtx, 18, wBlack, rgb(0xffffff), "Files") }),
						layout.Rigid(func(gtx C) D { return f.iconButton(gtx, &f.gear, "settings", 28, 20, filesNavText) }),
						hgap(4),
						layout.Rigid(func(gtx C) D { return f.iconButton(gtx, &f.plus, "plus", 28, 20, filesNavText) }),
					)
				})
			}),
			navItem(0), navItem(1), navItem(2),
			rule,
			navItem(3),
			rule,
			layout.Rigid(func(gtx C) D {
				g := gtx
				g.Constraints.Min.Y = gtx.Dp(28)
				return layout.Inset{Left: 8}.Layout(g, func(gtx C) D {
					return layout.W.Layout(gtx, func(gtx C) D {
						return row(gtx, layout.Middle,
							layout.Rigid(func(gtx C) D { return f.icon(gtx, "star", 16, filesNavText) }),
							hgap(8),
							layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wReg, filesNavText, "Starred") }),
						)
					})
				})
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 32, Right: 8, Top: 2}.Layout(gtx, f.starHint)
			}),
		)
	})
}

// ---- primary pane ----

func (f *Files) paneBg() color.NRGBA { return f.p.Page }

func (f *Files) main(gtx C) D {
	size := gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: size}, f.paneBg())
	gtx.Constraints = layout.Exact(size)
	title := filesNav[f.nav].title
	return col(gtx,
		layout.Rigid(func(gtx C) D {
			g := gtx
			g.Constraints.Min.Y = gtx.Dp(56)
			return layout.Inset{Top: 13, Bottom: 13, Left: 24, Right: 16}.Layout(g, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Flexed(1, func(gtx C) D { return f.label(gtx, 18, wBlack, f.p.Text, title) }),
					layout.Rigid(func(gtx C) D {
						if f.kind() == "deleted" {
							return D{}
						}
						return f.newButton(gtx)
					}),
				)
			})
		}),
		layout.Flexed(1, f.body),
	)
}

// newButton: small primary, green, radius 4, "+ New".
func (f *Files) newButton(gtx C) D {
	return f.newBtn.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		tw := f.th.Measure(gtx, 13, wBold, "New")
		sz := image.Pt(tw+gtx.Dp(12+12+16+4), gtx.Dp(28))
		bg := f.p.Green
		if f.newBtn.Hovered() {
			bg = f.p.GreenHover
		}
		rrect(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), bg)
		g := gtx
		g.Constraints = layout.Exact(sz)
		layout.Center.Layout(g, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return f.icon(gtx, "plus", 16, rgb(0xffffff)) }),
				hgap(4),
				layout.Rigid(func(gtx C) D { return f.label(gtx, 13, wBold, rgb(0xffffff), "New") }),
			)
		})
		return D{Size: sz}
	})
}

// body is one scrolling column: search, templates, filter bar, cards.
func (f *Files) body(gtx C) D {
	_, loaded, err := f.data.get()
	vis := make([]int, 0, len(f.items))
	for i := range f.items {
		if f.visible(&f.items[i]) {
			vis = append(vis, i)
		}
	}
	// Client-side filters can hide a whole page: keep fetching (bounded).
	if loaded && len(vis) < 12 && f.page < f.pages && f.page < 8 {
		f.loadMore()
	}
	showTpl := f.kind() != "deleted" && f.query == ""
	const (
		rSearch = iota
		rTplHead
		rTiles
		rBar
		rFirst
	)
	n := rFirst
	var state int // 0 rows, 1 loading, 2 error, 3 empty
	switch {
	case !loaded:
		state, n = 1, n+1
	case err != nil && len(f.items) == 0:
		state, n = 2, n+1
	case len(vis) == 0 && !f.more.busy():
		state, n = 3, n+1
	default:
		n += len(vis) + 1
	}
	return f.scrollList(gtx, &f.list, n, func(gtx C, i int) D {
		in := layout.Inset{Left: 35, Right: 35}
		switch i {
		case rSearch:
			return layout.Inset{Left: 35, Right: 35, Bottom: 20}.Layout(gtx, func(gtx C) D {
				return f.searchField(gtx)
			})
		case rTplHead:
			if !showTpl {
				return D{}
			}
			return in.Layout(gtx, f.templatesHead)
		case rTiles:
			if !showTpl || !f.tplOpen {
				return D{}
			}
			return layout.Inset{Left: 33, Right: 33, Top: 4, Bottom: 20}.Layout(gtx, f.templates)
		case rBar:
			if f.kind() == "deleted" {
				return D{}
			}
			return layout.Inset{Left: 35, Right: 35, Bottom: 16}.Layout(gtx, f.filterBar)
		}
		switch state {
		case 1:
			return f.skeleton(gtx, 6, 36)
		case 2:
			g := gtx
			g.Constraints.Min.Y = gtx.Dp(300)
			return f.errorState(g, err, &f.retry)
		case 3:
			g := gtx
			g.Constraints.Min.Y = gtx.Dp(300)
			return f.empty(g)
		}
		k := i - rFirst
		if k >= len(vis) {
			return f.footer(gtx)
		}
		if k > len(vis)-8 {
			f.loadMore()
		}
		return layout.Inset{Left: 35, Right: 35, Bottom: 8}.Layout(gtx, func(gtx C) D {
			return f.card(gtx, &f.items[vis[k]])
		})
	})
}

// searchField: h 36, white, 1px #5e5d60@.13, radius 8, 20px magnifier.
func (f *Files) searchField(gtx C) D {
	hgt := gtx.Dp(36)
	w := gtx.Constraints.Max.X
	r := image.Rect(0, 0, w, hgt)
	rad := gtx.Dp(8)
	focused := gtx.Focused(&f.search.ed)
	rrect(gtx, r, rad, f.p.Bg)
	if focused {
		border(gtx, r.Inset(-gtx.Dp(2)), rad+gtx.Dp(2), gtx.Dp(4), theme.WithAlpha(f.p.Link, 0x4d))
		border(gtx, r, rad, max(1, gtx.Dp(1)), f.p.Link)
	} else {
		border(gtx, r, rad, max(1, gtx.Dp(1)), f.p.Border)
	}
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, hgt))
	layout.Inset{Left: 14, Right: 12}.Layout(g, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return f.icon(gtx, "search", 20, f.p.Sec) }),
			hgap(14),
			layout.Flexed(1, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Stack{Alignment: layout.W}.Layout(gtx,
					layout.Stacked(func(gtx C) D {
						if f.search.ed.Len() > 0 {
							return D{}
						}
						return f.label(gtx, 15, wReg, f.p.Muted, f.search.ph)
					}),
					layout.Stacked(func(gtx C) D { return f.th.Editor(gtx, &f.search.ed, 15, f.p.Text, f.p.Mention) }),
				)
			}),
		)
	})
	return D{Size: image.Pt(w, hgt)}
}

func (f *Files) templatesHead(gtx C) D {
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D {
			return f.tplToggle.Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				caret := "caret-down"
				if !f.tplOpen {
					caret = "caret-right"
				}
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return f.icon(gtx, caret, 16, f.p.Text) }),
					hgap(4),
					layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wBold, f.p.Text, "Templates") }),
					hgap(8),
					layout.Rigid(func(gtx C) D {
						tw := f.th.Measure(gtx, 12, wReg, "New")
						sz := image.Pt(tw+gtx.Dp(8), gtx.Dp(20))
						tagBg, tagFg := rgb(0xf0f3fc), rgb(0x112377)
						if f.th.Dark {
							tagBg, tagFg = rgb(0x1c2647), rgb(0xc5d1ff)
						}
						rrect(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), tagBg)
						g := gtx
						g.Constraints = layout.Exact(sz)
						layout.Center.Layout(g, func(gtx C) D { return f.label(gtx, 12, wReg, tagFg, "New") })
						return D{Size: sz}
					}),
				)
			})
		}),
		layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
		layout.Rigid(func(gtx C) D {
			if f.nav == 0 {
				return D{}
			}
			return f.linkButton(gtx, &f.seeAll, 15, "See all")
		}),
	)
}

// templates: tiles h 227, radius 12, 8px gap; All files scrolls a strip of
// 266px tiles, Canvases/Lists fill the width with 3 tiles.
func (f *Files) templates(gtx C) D {
	idx := make([]int, 0, len(fileTemplates))
	for i, t := range fileTemplates {
		switch {
		case f.nav == 1 && t.list, f.nav == 2 && !t.list:
			continue
		}
		idx = append(idx, i)
	}
	gap := gtx.Dp(8)
	h := gtx.Dp(227)
	if f.nav != 0 {
		if len(idx) > 3 {
			idx = idx[:3]
		}
		w := (gtx.Constraints.Max.X - gap*2) / 3
		ch := make([]layout.FlexChild, 0, 6)
		for k, i := range idx {
			i := i
			if k > 0 {
				ch = append(ch, layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gap, 0)} }))
			}
			ch = append(ch, layout.Rigid(func(gtx C) D { return f.tile(gtx, i, image.Pt(w, h)) }))
		}
		return row(gtx, layout.Start, ch...)
	}
	w := gtx.Dp(266)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(gtx.Constraints.Max.X, h+gtx.Dp(2)))
	f.tplList.Layout(g, len(idx), func(gtx C, k int) D {
		return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx C) D { return f.tile(gtx, idx[k], image.Pt(w, h)) })
	})
	return D{Size: image.Pt(gtx.Constraints.Max.X, h+gtx.Dp(2))}
}

func (f *Files) tile(gtx C, i int, sz image.Point) D {
	t := fileTemplates[i]
	c := &f.tplClicks[i]
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		rad := gtx.Dp(12)
		r := image.Rectangle{Max: sz}
		rrect(gtx, r, rad, f.p.Surface)
		// tinted preview (blue canvases, yellow lists) with a white page.
		ph := gtx.Dp(130)
		tint := rgb(0xc1f0fe)
		if t.list {
			tint = rgb(0xfce8b3)
		}
		pageC, line0, line1 := rgb(0xffffff), rgb(0xd0d0d0), rgb(0xe8e8e8)
		if f.th.Dark {
			tint = theme.Mix(tint, f.p.Surface, 0.8)
			pageC, line0, line1 = rgb(0x2c2d30), rgb(0x55565a), rgb(0x3d3e42)
		}
		func() {
			defer clipRR(gtx, image.Rect(0, 0, sz.X, ph), rad, rad, 0, 0).Pop()
			fill(gtx, image.Rect(0, 0, sz.X, ph), tint)
			page := image.Rect(gtx.Dp(20), gtx.Dp(20), sz.X-gtx.Dp(20), ph+gtx.Dp(8))
			theme.Shadow(gtx, page, gtx.Dp(8), gtx.Dp(8), argb(0x1a000000))
			rrect(gtx, page, gtx.Dp(8), pageC)
			// a few skeleton lines suggesting the template body
			y := page.Min.Y + gtx.Dp(14)
			for k, frac := range [...]int{60, 85, 72, 40} {
				lc := line1
				if k == 0 {
					lc = line0
				}
				x0 := page.Min.X + gtx.Dp(14)
				rrect(gtx, image.Rect(x0, y, x0+(page.Dx()-gtx.Dp(28))*frac/100, y+gtx.Dp(6)), gtx.Dp(3), lc)
				y += gtx.Dp(14)
			}
		}()
		fill(gtx, image.Rect(0, ph, sz.X, ph+max(1, gtx.Dp(1))), f.p.Border)
		func() {
			defer opOffset(gtx, image.Pt(gtx.Dp(16), ph+gtx.Dp(16))).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(sz.X-gtx.Dp(32), sz.Y-ph-gtx.Dp(16))}
			col(g,
				layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wBold, f.p.Text, t.title) }),
				layout.Rigid(func(gtx C) D { return f.para(gtx, 13, 16, wReg, f.p.Muted, 2, t.desc) }),
			)
		}()
		border(gtx, r, rad, max(1, gtx.Dp(1)), f.p.Ring)
		if c.Hovered() {
			rrect(gtx, r, rad, f.p.Hover)
		}
		return D{Size: sz}
	})
}

// filterBar: segment buttons left; Types / sort / Filters right.
func (f *Files) filterBar(gtx C) D {
	all := "All"
	switch f.nav {
	case 1:
		all = "All Canvases"
	case 2:
		all = "All Lists"
	}
	segs := [3]string{all, "Created by you", "Shared with you"}
	ch := make([]layout.FlexChild, 0, 12)
	for i := range segs {
		i := i
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D {
				return f.segButton(gtx, &f.segClicks[i], f.seg == i, "", segs[i], false)
			})
		}))
	}
	ch = append(ch, layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }))
	if f.nav == 0 {
		ch = append(ch, layout.Rigid(func(gtx C) D {
			label := "All types"
			if n := f.typeCount(); n < len(fileTypeMenu) {
				label = plural(n, "Type")
			}
			d := f.segButton(gtx, &f.typesBtn, true, "filter", label, true)
			f.dropdown(gtx, &f.typesOpen, &f.typesScrim, image.Pt(0, d.Size.Y+gtx.Dp(4)), 280, f.typesMenu)
			return d
		}), hgap(8))
	}
	ch = append(ch,
		layout.Rigid(func(gtx C) D {
			d := f.segButton(gtx, &f.sortBtn, true, "", fileSorts[f.sortSel], true)
			f.dropdown(gtx, &f.sortOpen, &f.sortScrim, image.Pt(0, d.Size.Y+gtx.Dp(4)), 220, f.sortMenu)
			return d
		}),
		hgap(8),
		layout.Rigid(func(gtx C) D {
			return f.filtersBtn.Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				sz := image.Pt(gtx.Dp(42), gtx.Dp(28))
				r := image.Rectangle{Max: sz}
				rrect(gtx, r, gtx.Dp(4), f.p.Bg)
				if f.filtersBtn.Hovered() {
					rrect(gtx, r, gtx.Dp(4), f.p.Hover)
				}
				border(gtx, r, gtx.Dp(4), max(1, gtx.Dp(1)), f.p.SegLine)
				g := gtx
				g.Constraints = layout.Exact(sz)
				layout.Center.Layout(g, func(gtx C) D { return f.icon(gtx, "filter-sliders", 18, f.p.Text) })
				return D{Size: sz}
			})
		}),
	)
	return row(gtx, layout.Middle, ch...)
}

// segButton: h 28, padding 0 11px, radius 4, 13px bold. Active: #1264a3
// fill, white text. Inactive: white with a 1px #1d1c1d@.3 border.
func (f *Files) segButton(gtx C, c *widget.Clickable, active bool, icon, label string, caret bool) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		fg := f.p.Text
		if active {
			fg = rgb(0xffffff)
		}
		m := op.Record(gtx.Ops)
		d := layout.Inset{Left: 11, Right: 11}.Layout(gtx, func(gtx C) D {
			g := gtx
			g.Constraints.Min.Y = gtx.Dp(28)
			return layout.W.Layout(g, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D {
						if icon == "" {
							return D{}
						}
						return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return f.icon(gtx, icon, 16, fg) })
					}),
					layout.Rigid(func(gtx C) D { return f.label(gtx, 13, wBold, fg, label) }),
					layout.Rigid(func(gtx C) D {
						if !caret {
							return D{}
						}
						return layout.Inset{Left: 2}.Layout(gtx, func(gtx C) D { return f.icon(gtx, "caret-down", 16, fg) })
					}),
				)
			})
		})
		call := m.Stop()
		r := image.Rectangle{Max: d.Size}
		rad := gtx.Dp(4)
		if active {
			bg := f.p.Link
			if c.Hovered() {
				bg = rgb(0x0b4c8c)
			}
			rrect(gtx, r, rad, bg)
		} else {
			rrect(gtx, r, rad, f.p.Bg)
			if c.Hovered() {
				rrect(gtx, r, rad, f.p.Hover)
			}
			border(gtx, r, rad, max(1, gtx.Dp(1)), f.p.SegLine)
		}
		call.Add(gtx.Ops)
		return d
	})
}

func (f *Files) typesMenu(gtx C) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return actStack(gtx, len(fileTypeMenu), func(gtx C, i int) D {
		return f.menuItem(gtx, &f.typeClicks[i], fileTypeMenu[i].icon, fileTypeMenu[i].label, true, f.typeOn[i])
	})
}

func (f *Files) sortMenu(gtx C) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return actStack(gtx, len(fileSorts), func(gtx C, i int) D {
		return f.menuItem(gtx, &f.sortClicks[i], "", fileSorts[i], true, f.sortSel == i)
	})
}

func (f *Files) empty(gtx C) D {
	switch {
	case f.query != "":
		return f.emptyState(gtx, "search", "No results", "Try different keywords or remove filters.", nil, "")
	case f.nav == 1:
		return f.emptyState(gtx, "", "No canvases yet", "Your recently viewed or updated canvases will show up here.", &f.emptyNew, "+ New Canvas")
	case f.nav == 2:
		return f.emptyState(gtx, "", "No lists yet", "Your recently viewed or updated lists will show up here.", &f.emptyNew, "+ New List")
	case f.nav == 3:
		return f.emptyState(gtx, "", "No deleted files", "Files you delete stay here for a while before they're gone for good.", nil, "")
	}
	return f.emptyState(gtx, "", "No files yet", "Files you share or view will show up here.", nil, "")
}

func (f *Files) footer(gtx C) D {
	if f.more.busy() {
		return layout.Inset{Top: 8, Bottom: 24}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return f.centered(gtx, 13, wReg, f.p.Muted, "Loading…")
		})
	}
	return D{Size: image.Pt(0, gtx.Dp(24))}
}

func (f *Files) clicksOf(id string) *fileClicks {
	c := f.clicks[id]
	if c == nil {
		c = new(fileClicks)
		f.clicks[id] = c
	}
	return c
}

// card: white, radius 12, 1px #1d1c1d@.1, padding 12, h 66; 36px type
// tile, 15px bold name, 13px #616061 meta; owner avatar, star, kebab.
func (f *Files) card(gtx C, it *fileItem) D {
	c := f.clicksOf(it.ID)
	return c.row.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		m := op.Record(gtx.Ops)
		d := layout.UniformInset(12).Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return f.fileTile(gtx, it, 36) }),
				hgap(12),
				layout.Flexed(1, func(gtx C) D {
					return col(gtx,
						layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wBold, f.p.Text, it.title()) }),
						layout.Rigid(func(gtx C) D { return f.label(gtx, 13, wReg, f.p.Meta, f.fileMeta(it)) }),
					)
				}),
				layout.Rigid(func(gtx C) D {
					if it.User == "" {
						return D{}
					}
					u, _ := f.user(it.User)
					return c.owner.Layout(gtx, func(gtx C) D { return f.avatar(gtx, u.Avatar, 20) })
				}),
				hgap(4),
				layout.Rigid(func(gtx C) D { return f.iconButton(gtx, &c.star, "star", 28, 18, f.p.Meta) }),
				layout.Rigid(func(gtx C) D { return f.iconButton(gtx, &c.menu, "kebab", 28, 18, f.p.Meta) }),
			)
		})
		call := m.Stop()
		r := image.Rectangle{Max: d.Size}
		rad := gtx.Dp(12)
		rrect(gtx, r, rad, f.p.Surface)
		call.Add(gtx.Ops)
		border(gtx, r, rad, max(1, gtx.Dp(1)), f.p.CardLine)
		return d
	})
}

func (f *Files) fileMeta(it *fileItem) string {
	parts := make([]string, 0, 3)
	if it.User != "" {
		parts = append(parts, f.userName(it.User))
	}
	if t := unixTime(fileUpdated(it)); !t.IsZero() {
		if sameDay(t, time.Now()) {
			parts = append(parts, "Last updated today")
		} else {
			parts = append(parts, "Last updated "+listTime(t))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, fileKindLabel(it))
	}
	return strings.Join(parts, " · ")
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

// fileTile draws the thumbnail (images) or Slack's colored type tile.
func (f *Files) fileTile(gtx C, it *fileItem, size unit.Dp) D {
	s := gtx.Dp(size)
	if it.Thumb != "" && it.isImage() && f.h.Images() != nil {
		if img, ok := f.h.Images().Get(it.Thumb, s*2); ok {
			return themeImage(gtx, img, image.Pt(s, s), gtx.Dp(8))
		}
		return f.th.Placeholder(gtx, image.Pt(s, s), gtx.Dp(8))
	}
	bg, fg, icon := fileTileStyle(it)
	rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), bg)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(s, s))
	layout.Center.Layout(g, func(gtx C) D { return f.icon(gtx, icon, 20, fg) })
	return D{Size: image.Pt(s, s)}
}

// fileTileStyle is the per-type tile Slack uses (Lists: #e19b06 with a
// #fcf2d9 glyph, Canvases teal, PDFs red, ...).
func fileTileStyle(it *fileItem) (bg, fg color.NRGBA, icon string) {
	white := rgb(0xffffff)
	switch {
	case it.isList():
		return rgb(0xe19b06), rgb(0xfcf2d9), "list-view"
	case it.isCanvas():
		return rgb(0x1d9bd1), white, "canvas"
	case it.Filetype == "pdf":
		return rgb(0xe01e5a), white, "files"
	case isSheet(it):
		return rgb(0x2eb67d), white, "list-view"
	case isSlides(it):
		return rgb(0xe8912d), white, "image"
	case it.isImage():
		return rgb(0x1264a3), white, "image"
	case strings.HasPrefix(it.Mime, "video/"):
		return rgb(0x1264a3), white, "video"
	case strings.HasPrefix(it.Mime, "audio/"):
		return rgb(0x7c3085), white, "volume"
	case it.Mode == "snippet" || isCode(it.Filetype) || it.Filetype == "text":
		return rgb(0x1d1c1d), white, "code"
	case isDocType(it):
		return rgb(0x1264a3), white, "files"
	}
	return rgb(0x616061), white, "files"
}

func isCode(ft string) bool {
	switch ft {
	case "go", "javascript", "python", "c", "cpp", "java", "rust", "shell", "json", "yaml", "html", "css", "markdown", "typescript", "ruby", "php", "sql", "swift", "kotlin":
		return true
	}
	return false
}

func isDocType(it *fileItem) bool {
	switch it.Filetype {
	case "doc", "docx", "gdoc", "odt", "rtf", "pages", "md", "markdown":
		return true
	}
	return strings.Contains(it.Mime, "wordprocessing") || strings.Contains(it.Mime, "msword")
}

func isSheet(it *fileItem) bool {
	switch it.Filetype {
	case "xls", "xlsx", "csv", "gsheet", "ods", "numbers":
		return true
	}
	return strings.Contains(it.Mime, "spreadsheet")
}

func isSlides(it *fileItem) bool {
	switch it.Filetype {
	case "ppt", "pptx", "gpres", "key", "odp":
		return true
	}
	return strings.Contains(it.Mime, "presentation")
}

// clipRR pushes a rounded-rect clip with per-corner radii.
func clipRR(gtx C, r image.Rectangle, nw, ne, se, sw int) clip.Stack {
	return clip.RRect{Rect: r, NW: nw, NE: ne, SE: se, SW: sw}.Push(gtx.Ops)
}

var fileMention = regexp.MustCompile(`<([@#!])([A-Z0-9]+)(?:\|([^>]*))?>`)

// cleanTitles unescapes Slack's &lt;&gt;&amp; and resolves <@U…>, <#C…>
// and <!here> tokens in file titles, the way Slack displays them.
func (f *Files) cleanTitles(in []fileItem) []fileItem {
	out := make([]fileItem, len(in))
	copy(out, in)
	for i := range out {
		t := out[i].Title
		if strings.Contains(t, "<") || strings.Contains(t, "&") {
			t = fileMention.ReplaceAllStringFunc(t, func(m string) string {
				sm := fileMention.FindStringSubmatch(m)
				if sm[3] != "" {
					return sm[1] + sm[3]
				}
				switch sm[1] {
				case "@":
					return "@" + f.userName(sm[2])
				case "#":
					return f.convTitle(sm[2], nil)
				}
				return "@" + strings.ToLower(sm[2])
			})
			out[i].Title = html.UnescapeString(t)
		}
	}
	return out
}

// starHint is "Click the ⭐ star on any file to add it here for later.",
// with the star drawn as Slack's emoji image (Lato has no emoji glyphs).
func (f *Files) starHint(gtx C) D {
	const lead = "Click the "
	w := f.th.Measure(gtx, 15, wReg, lead)
	url, _, ok := f.h.Core().Emoji("star")
	if !ok {
		return f.para(gtx, 15, 22, wReg, filesNavText, 0, "Click the star on any file to add it here for later.")
	}
	// Lay the sentence with spaces reserving the emoji's width, then draw
	// the emoji over the gap (it sits on the first line).
	sp := f.th.Measure(gtx, 15, wReg, " ")
	n := 1
	if sp > 0 {
		n = (gtx.Dp(18) + sp - 1) / sp
	}
	d := f.para(gtx, 15, 22, wReg, filesNavText, 0, lead+strings.Repeat(" ", n)+"star on any file to add it here for later.")
	func() {
		defer opOffset(gtx, image.Pt(w+gtx.Dp(1), gtx.Dp(3))).Pop()
		f.emojiImg(gtx, url, 16)
	}()
	return d
}
