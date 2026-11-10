package views

import (
	"context"
	"html"
	"image"
	"image/color"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// fileItem is a core.File with the Files view's display helpers.
type fileItem struct{ core.File }

func (f *fileItem) title() string {
	if f.Title != "" {
		return f.Title
	}
	return f.Name
}

func (f *fileItem) isImage() bool { return f.IsImage || strings.HasPrefix(f.Mime, "image/") }

func (f *fileItem) isCanvas() bool {
	return f.IsCanvas || f.Filetype == "quip" || f.Filetype == "canvas" || strings.EqualFold(f.PrettyType, "canvas")
}

func (f *fileItem) isList() bool {
	return f.IsList || f.Filetype == "list" || strings.EqualFold(f.PrettyType, "list")
}

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
	plus      widget.Clickable
	newBtn    widget.Clickable
	emptyNew  widget.Clickable

	search *search
	deb    debounce
	query  string

	tplOpen   bool
	tplToggle widget.Clickable
	seeAll    widget.Clickable
	tplClicks []widget.Clickable
	tplList   widget.List
	tpls      async[[]core.ListTemplate] // lists.templates, for the strip's list templates
	tplsAsked bool

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
	data   async[core.FilePage]
	more   async[core.FilePage]
	items  []fileItem
	page   int
	pages  int
	list   widget.List
	clicks map[string]*fileClicks
	reload bool

	// the sidebar's Starred section: files.favorites.list, then files.info
	// for the files the browser hasn't loaded
	starred    async[[]core.File]
	starAsked  bool
	starClicks map[string]*widget.Clickable

	// results of writes (star, rename, delete, create from a template),
	// run on the UI goroutine by update
	doneMu sync.Mutex
	done   []func()

	editName widget.Editor // Edit file details' File name
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

// fileListTemplates are the Templates strip's list templates, in Slack's
// order (ExplorerActionsTemplates: ProjectTracker, FeedbackTracker,
// QuarterlyPlan by their production template file ids, PTs7.op), picked
// from lists.templates (fetchTemplates).
//
// Slack's strip also has canvas templates (search.modules.files
// "type:canvas_template from:<@USLACKBOT>", the ones named
// EmployeeOnboardingTemplate, MonthlyNewsletterTemplate, WeeklySyncTemplate
// and OOOCoveragePlan), and a tile opens the canvas template gallery
// (openCanvasTemplateGalleryModal) on it. Clack has no template gallery and
// the gallery's create call is in a chunk not ported, so it draws only the
// list templates.
var fileListTemplates = [...]string{"F07QP7D3TC5", "F07QS06GTNX", "F07QRV73FNF"}

// fileTemplate is one Templates tile.
type fileTemplate struct {
	id, title, desc string
	list            bool
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
	f := &Files{base: newBase(h), search: newSearch(filesNav[0].placeholder), clicks: map[string]*fileClicks{}, reload: true, tplOpen: true,
		starClicks: map[string]*widget.Clickable{}}
	f.editName.SingleLine, f.editName.Submit = true, true
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
		f.data.load(f.h, false, func(ctx context.Context) (core.FilePage, error) { return a.DeletedFiles(ctx) })
		return
	}
	q := core.FilesOpts{Kind: f.kind(), Query: f.query, Mine: f.seg == 1, SharedWithMe: f.seg == 2, Page: 1}
	f.data.load(f.h, false, func(ctx context.Context) (core.FilePage, error) { return a.Files(ctx, q) })
}

func (f *Files) loadMore() {
	if f.kind() == "deleted" || f.more.busy() || f.data.busy() || f.page == 0 || f.page >= f.pages {
		return
	}
	a := f.h.Core()
	q := core.FilesOpts{Kind: f.kind(), Query: f.query, Mine: f.seg == 1, SharedWithMe: f.seg == 2, Page: f.page + 1}
	f.more.load(f.h, false, func(ctx context.Context) (core.FilePage, error) { return a.Files(ctx, q) })
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
	f.drainDone()
	if a := f.h.Core(); a != nil && !f.tplsAsked {
		f.tplsAsked = true
		f.tpls.load(f.h, false, func(ctx context.Context) ([]core.ListTemplate, error) { return a.ListTemplates(ctx) })
	}
	if f.h.Core() != nil && !f.starAsked {
		f.starAsked = true
		f.loadStarred()
	}
	for i := range f.navClicks {
		if f.navClicks[i].Clicked(gtx) {
			if f.nav != i {
				f.nav, f.reload = i, true
				f.search.ph = filesNav[i].placeholder
			}
			if c, ok := f.h.(PaneChooser); ok {
				c.PaneChosen()
			}
		}
	}
	for i := range f.segClicks {
		if f.segClicks[i].Clicked(gtx) && f.seg != i {
			f.seg, f.reload = i, true
		}
	}
	if f.plus.Clicked(gtx) || f.newBtn.Clicked(gtx) || f.emptyNew.Clicked(gtx) {
		if cc, ok := f.h.(interface{ CreateCanvas() }); ok && filesNav[f.nav].kind != "lists" {
			cc.CreateCanvas()
		} else if cl, ok := f.h.(interface{ NewListMenu() }); ok {
			cl.NewListMenu() // mount_features_lists.go
		}
	}
	if f.tplToggle.Clicked(gtx) {
		f.tplOpen = !f.tplOpen
	}
	// "See all" (Lists only here, see templatesHead): Slack's list
	// templates modal; Clack's lists.templates menu.
	if f.seeAll.Clicked(gtx) && f.kind() == "lists" {
		if cl, ok := f.h.(interface{ NewListMenu() }); ok {
			cl.NewListMenu() // mount_features_lists.go
		}
	}
	tpls := f.templateItems()
	for i := range f.tplClicks {
		if f.tplClicks[i].Clicked(gtx) && i < len(tpls) {
			f.createFromTemplate(tpls[i])
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
			f.toggleStar(it.File)
		}
		if c.menu.Clicked(gtx) {
			f.openMenu(*it)
		}
		if c.owner.Clicked(gtx) && it.User != "" {
			f.h.OpenProfile(image.Point{}, it.User)
		}
	}
	if st, ok, _ := f.starred.get(); ok {
		for _, sf := range st {
			if c := f.starClicks[sf.ID]; c != nil && c.Clicked(gtx) {
				openFile(f.h, fileItem{sf})
			}
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

func (f *Files) open(it fileItem) { openFile(f.h, it) }

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

// LayoutList and LayoutPane are LayoutNav and LayoutMain under the names
// the simplified layout asks for (one at a time).
func (f *Files) LayoutList(gtx C) D { return f.LayoutNav(gtx) }
func (f *Files) LayoutPane(gtx C) D { return f.LayoutMain(gtx) }

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
	// The column sits on the theme's chrome gradient (flat mid color) under
	// the sidebar tint; every translucent sidebar color is flattened over
	// it in sRGB (see flatten) so it matches the shell's Home sidebar.
	c := &f.th.C
	chrome := filesChrome
	if c.ChromeA.A > 0 {
		chrome = theme.Mix(c.ChromeA, c.ChromeB, 0.5)
	}
	if c.SidebarBg.A > 0 {
		chrome = flatten(c.SidebarBg, chrome)
	}
	navText, navHover, navRule := flatten(filesNavText, chrome), flatten(filesNavHover, chrome), flatten(filesNavRule, chrome)
	navSelBg, navSelFg := filesNavSelBg, filesNavSelFg
	if c.SidebarText.A > 0 {
		navText = flatten(c.SidebarText, chrome)
		navHover = flatten(c.SidebarHover, chrome)
		navRule = flatten(c.SidebarDivider, chrome)
		navSelBg, navSelFg = flatten(c.SidebarActive, chrome), c.SidebarActiveText
	}
	fill(gtx, image.Rectangle{Max: size}, chrome)
	gtx.Constraints = layout.Exact(size)
	navItem := func(i int) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			c := &f.navClicks[i]
			return c.Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				w, h := gtx.Constraints.Max.X, gtx.Dp(28)
				sel := f.nav == i
				fg := navText
				switch {
				case sel:
					rrect(gtx, image.Rect(0, 0, w, h), gtx.Dp(4), navSelBg)
					fg = navSelFg
				case c.Hovered():
					rrect(gtx, image.Rect(0, 0, w, h), gtx.Dp(4), navHover)
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
		return layout.Inset{Top: 12, Bottom: 12}.Layout(gtx, func(gtx C) D { return hline(gtx, navRule) })
	})
	return layout.Inset{Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
		return col(gtx,
			layout.Rigid(func(gtx C) D {
				g := gtx
				g.Constraints.Min.Y = gtx.Dp(52)
				return layout.Inset{Top: 12, Bottom: 12, Left: 8}.Layout(g, func(gtx C) D {
					return row(gtx, layout.Middle,
						// Slack's settings button ("More options",
						// unified-files-sidebar-settings-menu) renders only
						// when its menu has items, and its one item, "Bulk
						// manage files", needs custom_file_sections (file
						// sections Clack doesn't have): without them there's
						// no gear.
						layout.Flexed(1, func(gtx C) D { return f.label(gtx, 18, wBlack, rgb(0xffffff), "Files") }),
						layout.Rigid(func(gtx C) D { return f.iconButton(gtx, &f.plus, "plus", 28, 20, navText) }),
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
							layout.Rigid(func(gtx C) D { return f.icon(gtx, "star", 16, navText) }),
							hgap(8),
							layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wReg, navText, "Starred") }),
						)
					})
				})
			}),
			layout.Rigid(func(gtx C) D {
				st, ok, _ := f.starred.get()
				if ok && len(st) > 0 {
					return f.starredRows(gtx, st, navText, navHover)
				}
				return layout.Inset{Left: 32, Right: 8, Top: 2}.Layout(gtx, f.starHint)
			}),
		)
	})
}

// starredRows lists the starred files under Starred (Slack's
// browse-sidebar-item rows: the file's type icon and title, 28px).
func (f *Files) starredRows(gtx C, st []core.File, fg, hover color.NRGBA) D {
	ch := make([]layout.FlexChild, 0, len(st))
	for i := range st {
		sf := st[i]
		c := f.starClicks[sf.ID]
		if c == nil {
			c = new(widget.Clickable)
			f.starClicks[sf.ID] = c
		}
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return c.Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				w, h := gtx.Constraints.Max.X, gtx.Dp(28)
				if c.Hovered() {
					rrect(gtx, image.Rect(0, 0, w, h), gtx.Dp(4), hover)
				}
				g := gtx
				g.Constraints = layout.Exact(image.Pt(w, h))
				it := fileItem{sf}
				icon := "files"
				switch {
				case it.isCanvas():
					icon = "canvas"
				case it.isList():
					icon = "list-view"
				}
				layout.Inset{Left: 8, Right: 8}.Layout(g, func(gtx C) D {
					return layout.W.Layout(gtx, func(gtx C) D {
						return row(gtx, layout.Middle,
							layout.Rigid(func(gtx C) D { return f.icon(gtx, icon, 16, fg) }),
							hgap(8),
							layout.Flexed(1, func(gtx C) D { return f.label(gtx, 15, wReg, fg, it.title()) }),
						)
					})
				})
				return D{Size: image.Pt(w, h)}
			})
		}))
	}
	return col(gtx, ch...)
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
	showTpl := f.kind() != "deleted" && f.query == "" && len(f.templateItems()) > 0
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
		// Slack stacks the results as one list: rows share their 1px
		// dividers, only the first and last get the 12px corners.
		bottom := unit.Dp(0)
		if k == len(vis)-1 {
			bottom = 8
		}
		return layout.Inset{Left: 35, Right: 35, Bottom: bottom}.Layout(gtx, func(gtx C) D {
			return f.card(gtx, &f.items[vis[k]], k == 0, k == len(vis)-1)
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
							tagBg, tagFg = rgb(0x040b3d), rgb(0xe4e8f4) // base-education / content-education
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
			// ExplorerActionsTemplates: no "See all" on All files; on
			// Canvases it opens the canvas template gallery, which Clack
			// doesn't have
			if f.kind() != "lists" {
				return D{}
			}
			return f.linkButton(gtx, &f.seeAll, 15, "See all")
		}),
	)
}

// templateItems are the strip's tiles for the current section, from
// lists.templates (see fileListTemplates): All files and Lists show the
// list templates, Canvases none.
func (f *Files) templateItems() []fileTemplate {
	if f.kind() != "" && f.kind() != "lists" {
		return nil
	}
	got, ok, err := f.tpls.get()
	if !ok || err != nil {
		return nil
	}
	out := make([]fileTemplate, 0, len(fileListTemplates))
	for _, id := range fileListTemplates {
		for _, t := range got {
			if t.ID == id {
				out = append(out, fileTemplate{id: t.ID, title: t.Name, desc: t.Description, list: true})
			}
		}
	}
	if n := len(out); len(f.tplClicks) < n {
		f.tplClicks = append(f.tplClicks, make([]widget.Clickable, n-len(f.tplClicks))...)
	}
	return out
}

// templates: tiles h 227, radius 12, 8px gap; All files scrolls a strip of
// 266px tiles, Canvases/Lists fill the width with 3 tiles.
func (f *Files) templates(gtx C) D {
	tpls := f.templateItems()
	gap := gtx.Dp(8)
	h := gtx.Dp(227)
	if f.nav != 0 {
		n := min(len(tpls), 3)
		w := (gtx.Constraints.Max.X - gap*2) / 3
		ch := make([]layout.FlexChild, 0, 6)
		for i := 0; i < n; i++ {
			i := i
			if i > 0 {
				ch = append(ch, layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gap, 0)} }))
			}
			ch = append(ch, layout.Rigid(func(gtx C) D { return f.tile(gtx, tpls[i], &f.tplClicks[i], image.Pt(w, h)) }))
		}
		return row(gtx, layout.Start, ch...)
	}
	w := gtx.Dp(266)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(gtx.Constraints.Max.X, h+gtx.Dp(2)))
	f.tplList.Layout(g, len(tpls), func(gtx C, k int) D {
		return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx C) D { return f.tile(gtx, tpls[k], &f.tplClicks[k], image.Pt(w, h)) })
	})
	return D{Size: image.Pt(gtx.Constraints.Max.X, h+gtx.Dp(2))}
}

func (f *Files) tile(gtx C, t fileTemplate, c *widget.Clickable, sz image.Point) D {
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
			tint = rgb(0x002f4b) // plt-lagoon-5
			if t.list {
				tint = rgb(0x361e00) // plt-honeycomb-0
			}
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
			// base-inv-hgl-1 (filled blue): #1264a3 light, #0a77a7 dark
			bg := f.lt(rgb(0x1264a3), rgb(0x0a77a7))
			if c.Hovered() {
				bg = flatten(f.lt(argb(0x21122a59), argb(0x14d8eef6)), bg) // base-inv-hgl-1-hover
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

// card is one row of Slack's c-pillow_file list: white, 1px #1d1c1d@.1
// top line (plus the bottom one on the last row), radius 12 on the list's
// outer corners only, 65px tall (66 for the last); 36px type tile, 15px
// bold name, 13px #616061 meta; owner avatar, star, kebab.
func (f *Files) card(gtx C, it *fileItem, first, last bool) D {
	c := f.clicksOf(it.ID)
	return c.row.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		m := op.Record(gtx.Ops)
		bw := max(1, gtx.Dp(1))
		h := gtx.Dp(65)
		if last {
			h += bw
		}
		d := layout.Inset{Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			gtx.Constraints.Min.Y = 0
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
				layout.Rigid(func(gtx C) D {
					// FileActions' renderFavoriteCanvas / renderFavoriteList:
					// the star is on canvases and lists only
					if f.kind() == "deleted" || !it.isCanvas() && !it.isList() {
						return D{}
					}
					return f.starButton(gtx, &c.star, f.h.Core().FileStarred(it.ID))
				}),
				layout.Rigid(func(gtx C) D {
					if f.kind() == "deleted" {
						return D{}
					}
					return f.iconButton(gtx, &c.menu, "kebab", 28, 18, f.p.Meta)
				}),
			)
		})
		call := m.Stop()
		r := image.Rectangle{Max: image.Pt(d.Size.X, h)}
		rad := gtx.Dp(12)
		var tr, br int
		if first {
			tr = rad
		}
		if last {
			br = rad
		}
		// frame: the row in the line colour, then the surface inset by 1px
		// (no bottom inset between rows: the next row's top line divides).
		in := image.Rect(r.Min.X+bw, r.Min.Y+bw, r.Max.X-bw, r.Max.Y)
		if last {
			in.Max.Y -= bw
		}
		func() {
			defer clip.RRect{Rect: r, NW: tr, NE: tr, SW: br, SE: br}.Push(gtx.Ops).Pop()
			fill(gtx, r, theme.Over(f.p.CardLine, f.p.Surface))
		}()
		func() {
			defer clip.RRect{Rect: in, NW: max(tr-bw, 0), NE: max(tr-bw, 0), SW: max(br-bw, 0), SE: max(br-bw, 0)}.Push(gtx.Ops).Pop()
			fill(gtx, in, f.p.Surface)
		}()
		// the content centred in the 64px under the top line
		func() {
			defer op.Offset(image.Pt(0, bw+(gtx.Dp(64)-d.Size.Y)/2)).Push(gtx.Ops).Pop()
			call.Add(gtx.Ops)
		}()
		return D{Size: r.Size()}
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
	bg, fg, icon := fileTileStyle(it, f.th.Dark)
	rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), bg)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(s, s))
	layout.Center.Layout(g, func(gtx C) D { return f.icon(gtx, icon, 20, fg) })
	return D{Size: image.Pt(s, s)}
}

// fileTileStyle is the per-type tile Slack uses (Lists: #e19b06 with a
// #fcf2d9 glyph, Canvases teal, PDFs red, ...).
func fileTileStyle(it *fileItem, dark bool) (bg, fg color.NRGBA, icon string) {
	white := rgb(0xffffff)
	switch {
	case it.isList():
		if dark {
			return rgb(0xaa6c00), rgb(0x361e00), "list-view" // plt-honeycomb-40 / -0
		}
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
		if dark {
			return rgb(0x3b3d42), white, "code"
		}
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
func (f *Files) cleanTitles(in []core.File) []fileItem {
	out := make([]fileItem, len(in))
	for i := range in {
		out[i] = fileItem{in[i]}
	}
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

// starHint is Starred's empty row: "Click the ⭐ star on any canvas or
// list to add it here for later." ("on any file" with
// unified_files_3p_primary_custom_sections), the star drawn as Slack's
// emoji image (Lato has no emoji glyphs).
func (f *Files) starHint(gtx C) D {
	const lead = "Click the "
	tail := "star on any canvas or list to add it here for later."
	if f.h.Core().Experiment("unified_files_3p_primary_custom_sections") == "on" {
		tail = "star on any file to add it here for later."
	}
	w := f.th.Measure(gtx, 15, wReg, lead)
	url, _, ok := f.h.Core().Emoji("star")
	if !ok {
		return f.para(gtx, 15, 22, wReg, filesNavText, 0, "Click the "+tail)
	}
	// Lay the sentence with spaces reserving the emoji's width, then draw
	// the emoji over the gap (it sits on the first line).
	sp := f.th.Measure(gtx, 15, wReg, " ")
	n := 1
	if sp > 0 {
		n = (gtx.Dp(18) + sp - 1) / sp
	}
	d := f.para(gtx, 15, 22, wReg, filesNavText, 0, lead+strings.Repeat(" ", n)+tail)
	func() {
		defer opOffset(gtx, image.Pt(w+gtx.Dp(1), gtx.Dp(3))).Pop()
		f.emojiImg(gtx, url, 16)
	}()
	return d
}

// ---- file actions ----

// post queues fn for the UI goroutine (drainDone) and wakes the window.
func (f *Files) post(fn func()) {
	f.doneMu.Lock()
	f.done = append(f.done, fn)
	f.doneMu.Unlock()
	f.h.Invalidate()
}

func (f *Files) drainDone() {
	f.doneMu.Lock()
	ds := f.done
	f.done = nil
	f.doneMu.Unlock()
	for _, fn := range ds {
		fn()
	}
}

// write runs work off the UI goroutine, then then(err) on it.
func (f *Files) write(work func(ctx context.Context, a *core.App) error, then func(err error)) {
	a := f.h.Core()
	if a == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := work(ctx, a)
		f.post(func() { then(err) })
	}()
}

// loadStarred fills Starred: files.favorites.list, each file from the
// loaded pages or files.info (Slack reads them from its file store).
func (f *Files) loadStarred() {
	a := f.h.Core()
	known := map[string]core.File{}
	for _, it := range f.items {
		known[it.ID] = it.File
	}
	if st, ok, _ := f.starred.get(); ok {
		for _, sf := range st {
			known[sf.ID] = sf
		}
	}
	f.starred.load(f.h, true, func(ctx context.Context) ([]core.File, error) {
		ids, err := a.StarredFiles(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]core.File, 0, len(ids))
		for _, id := range ids {
			sf, ok := known[id]
			if !ok {
				if sf, err = a.FileInfo(ctx, id); err != nil {
					continue // deleted or no longer visible: Slack filters those out too
				}
			}
			out = append(out, sf)
		}
		return out, nil
	})
}

// starButton is FileActions' star (c-file__star_button): "star", or
// "star-filled" on c-file__unstar_button's sunflower-0 fill with
// sunflower-60 (hover sunflower-5 / -70).
func (f *Files) starButton(gtx C, c *widget.Clickable, on bool) D {
	if !on {
		return f.iconButton(gtx, c, "star", 28, 18, f.p.Meta)
	}
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		sz := gtx.Dp(28)
		bg, fg := rgb(0xfff4d9), rgb(0x9a6500) // plt-sunflower-0 / -60
		if c.Hovered() {
			bg, fg = rgb(0xffe7a8), rgb(0x7a4f00) // plt-sunflower-5 / -70
		}
		rrect(gtx, image.Rect(0, 0, sz, sz), gtx.Dp(8), bg)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(sz, sz))
		layout.Center.Layout(g, func(gtx C) D { return f.icon(gtx, "star-filled", 18, fg) })
		return D{Size: image.Pt(sz, sz)}
	})
}

// toggleStar is the star button and the menu's "Add to starred" /
// "Unstar canvas" / "Unstar list" (makeCanvasCollection,
// removeListCollection, …): the file joins or leaves Starred at once,
// files.favorites.add/remove goes out, and a failure puts it back. The
// Files view shows no toast (addCanvasToFavorites toasts only outside the
// unified files tab).
func (f *Files) toggleStar(file core.File) {
	a := f.h.Core()
	if a == nil {
		return
	}
	on := !a.FileStarred(file.ID)
	a.SetFileStarred(file.ID, on)
	f.setStarredRow(file, on)
	f.write(func(ctx context.Context, a *core.App) error {
		a.SetFileStarred(file.ID, on) // StarFile sets it again and reverts on failure
		return a.StarFile(ctx, file.ID, on)
	}, func(err error) {
		if err != nil {
			f.setStarredRow(file, !on)
		}
	})
}

func (f *Files) setStarredRow(file core.File, on bool) {
	st, _, _ := f.starred.get()
	out := make([]core.File, 0, len(st)+1)
	if on {
		out = append(out, file)
	}
	for _, x := range st {
		if x.ID != file.ID {
			out = append(out, x)
		}
	}
	f.starred.mu.Lock()
	f.starred.val, f.starred.loaded = out, true
	f.starred.mu.Unlock()
}

// createFromTemplate is a list template tile: Slack opens the template's
// preview (ListTemplatePreviewModal) and its "Use template" makes the list
// with lists.create {template_id}; Clack has no preview, so the tile makes
// the list and opens it in full view.
func (f *Files) createFromTemplate(t fileTemplate) {
	var id string
	f.write(func(ctx context.Context, a *core.App) error {
		var err error
		id, err = a.CreateList(ctx, "", t.id)
		return err
	}, func(err error) {
		if err != nil {
			f.h.Toast("Couldn't create the list: " + friendlyErr(err))
			return
		}
		if lo, ok := f.h.(interface{ OpenListFull(id string) }); ok {
			lo.OpenListFull(id)
		}
	})
}

// copyLinkLabel is getCopyLinkLabel.
func copyLinkLabel(it *fileItem) string {
	switch {
	case it.isList():
		return "Copy link to list"
	case it.isCanvas():
		return "Copy link to canvas"
	}
	return "Copy link to file"
}

// fileMenuItems is getFileActionsMenuItems as the Files view's results
// call it (unified-file-result: FileActions with isUnifiedFiles,
// showMoreListBrowserActions, includeExternalLinkAction off, the root
// download button on so no overflow Download, and custom_file_sections
// off), keeping the items Clack can do:
//
//	Add to saved / Remove from Saved (Save for later / Remove from Later
//	without later_todos) · Add to starred / Unstar canvas / Unstar list ·
//	Copy link to … | Edit file details (your hosted files) | Open in split
//	view (canvases, lists) | Delete canvas / list / file (your own)
//
// Left out, since Clack has no screen for them: Ask Slackbot, Add to-do,
// Share canvas / Share list, View file details, Open in new window,
// Report to Slack.
func (f *Files) fileMenuItems(it fileItem) []popups.MenuItem {
	a := f.h.Core()
	if a == nil {
		return nil
	}
	self := a.Self().ID
	own := it.User != "" && it.User == self
	var groups [][]popups.MenuItem
	var g []popups.MenuItem
	// save_file
	id := it.ID
	saved := a.FileSaved(id)
	todos := a.Experiment("later_todos") == "on"
	label, icon := "Save for later", "bookmark"
	switch {
	case todos && saved:
		label, icon = "Remove from Saved", "bookmark-filled"
	case todos:
		label = "Add to saved"
	case saved:
		label, icon = "Remove from Later", "bookmark-filled"
	}
	g = append(g, popups.MenuItem{Label: label, Icon: icon, Do: func() {
		f.write(func(ctx context.Context, a *core.App) error {
			if saved {
				return a.UnsaveFile(ctx, id)
			}
			return a.SaveFile(ctx, id)
		}, func(err error) {
			if err != nil {
				f.h.Toast("Couldn’t update your saved items: " + friendlyErr(err))
			}
		})
	}})
	if it.isCanvas() || it.isList() {
		label := "Add to starred"
		if a.FileStarred(id) {
			label = "Unstar canvas"
			if it.isList() {
				label = "Unstar list"
			}
		}
		file := it.File
		g = append(g, popups.MenuItem{Label: label, Do: func() { f.toggleStar(file) }})
	}
	if link := it.Permalink; link != "" {
		g = append(g, popups.MenuItem{Label: copyLinkLabel(&it), DoCtx: func(gtx layout.Context) {
			gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(link))})
			f.h.Toast("Link copied to clipboard")
		}})
	}
	groups = append(groups, g)
	// edit_file: the uploader's hosted (or email) files
	if own && (it.Mode == "hosted" || it.Mode == "email") {
		label := "Edit file details"
		if it.isImage() {
			label = "Edit image details"
		}
		groups = append(groups, []popups.MenuItem{{Label: label, Do: func() { f.editDetails(it, label) }}})
	}
	// Open in split view: canvases and lists (Slack hides it in the
	// simplified layout, isBBModeEnabled; the Host doesn't expose that)
	if it.isCanvas() || it.isList() {
		if it.isCanvas() {
			if co, ok := f.h.(interface{ OpenCanvas(id string) }); ok {
				groups = append(groups, []popups.MenuItem{{Label: "Open in split view", Icon: "move-to-split-view", Do: func() { co.OpenCanvas(id) }}})
			}
		} else if lo, ok := f.h.(interface{ OpenList(id string) }); ok {
			groups = append(groups, []popups.MenuItem{{Label: "Open in split view", Icon: "move-to-split-view", Do: func() { lo.OpenList(id) }}})
		}
	}
	// delete_file: your own files
	if own && it.Mode != "tombstone" {
		label := "Delete file"
		switch {
		case it.isCanvas():
			label = "Delete canvas"
		case it.isList():
			label = "Delete list"
		}
		groups = append(groups, []popups.MenuItem{{Label: label, Danger: true, Do: func() { f.confirmDelete(it) }}})
	}
	var items []popups.MenuItem
	for _, g := range groups {
		if len(g) == 0 {
			continue
		}
		if len(items) > 0 {
			items = append(items, popups.Sep())
		}
		items = append(items, g...)
	}
	return items
}

// openMenu is the row's ⋯ ("More actions", more_file_actions): Slack's
// menu opens beside the button (right-bottom); Clack anchors it at the
// pointer, which is on the button.
func (f *Files) openMenu(it fileItem) {
	ph, ok := f.h.(popups.Host)
	if !ok {
		return
	}
	items := f.fileMenuItems(it)
	if len(items) == 0 {
		return
	}
	var at image.Point
	if pp, ok := f.h.(interface{ PointerPos() image.Point }); ok {
		at = pp.PointerPos()
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: image.Rectangle{Min: at, Max: at}, Width: 240}))
}

// confirmDelete is DeleteFileDialog: "Delete canvas" / "Are you sure you
// want to delete this canvas?" / "Delete canvas" (lists alike; other files
// "Delete file" / "… delete this file permanently?" / "Yes, Delete This
// File"), the file under the text, then files.delete. On success the row
// goes (tombstoneFiles); a failure toasts "Oops! We couldn’t delete that
// file". Lists leave at once (shouldOptimisticallyDeleteFile).
func (f *Files) confirmDelete(it fileItem) {
	ph, ok := f.h.(popups.Host)
	if !ok {
		return
	}
	title, text, action := "Delete file", "Are you sure you want to delete this file permanently?", "Yes, Delete This File"
	switch {
	case it.isCanvas():
		title, text, action = "Delete canvas", "Are you sure you want to delete this canvas?", "Delete canvas"
	case it.isList():
		title, text, action = "Delete list", "Are you sure you want to delete this list?", "Delete list"
	}
	popups.Confirm(ph, title, text, action, true, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D { return f.deletePreview(gtx, &it) })
	}, func() { f.deleteFile(it) }).OldDialog() // DeleteFileDialog: DeprecatedBaseDialog, focusOnPrimaryButton
}

// deleteFile is DeleteFileDialog's onGo: files.delete {file}.
func (f *Files) deleteFile(it fileItem) {
	id := it.ID
	if it.isList() {
		f.dropItem(id)
	}
	f.write(func(ctx context.Context, a *core.App) error { return a.DeleteFile(ctx, id) }, func(err error) {
		if err != nil {
			f.h.Toast("Oops! We couldn’t delete that file")
			f.reload = true
			return
		}
		f.dropItem(id)
		if a := f.h.Core(); a != nil && a.FileStarred(id) {
			a.SetFileStarred(id, false)
			f.setStarredRow(it.File, false)
		}
	})
}

// deletePreview is the dialog's file (FilePreview with "You • time").
func (f *Files) deletePreview(gtx C, it *fileItem) D {
	meta := "You"
	if t := unixTime(it.Created); !t.IsZero() {
		meta += " • " + listTime(t)
	}
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D { return f.fileTile(gtx, it, 36) }),
		hgap(12),
		layout.Flexed(1, func(gtx C) D {
			return col(gtx,
				layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wBold, f.p.Text, it.title()) }),
				layout.Rigid(func(gtx C) D { return f.label(gtx, 13, wReg, f.p.Meta, meta) }),
			)
		}),
	)
}

func (f *Files) dropItem(id string) {
	out := f.items[:0]
	for _, x := range f.items {
		if x.ID != id {
			out = append(out, x)
		}
	}
	f.items = out
}

// editDetails is EditFileDialog for one file: "Edit file details", the
// "File name" field (required, at most 255 characters; placeholder
// "big-sur-trip-aerial.png") and Done, which runs editFileApi: the title
// changes at once, files.edit goes out, and a failure puts it back.
// Slack's image variant adds a preview and an alt text field; Clack edits
// the name only.
func (f *Files) editDetails(it fileItem, title string) {
	ph, ok := f.h.(popups.Host)
	if !ok {
		return
	}
	f.editName.SetText(it.title())
	f.editName.SetCaret(f.editName.Len(), 0)
	valid := func() bool {
		t := strings.TrimSpace(f.editName.Text())
		return t != "" && len([]rune(t)) <= 255
	}
	focused := false
	submit := func() { f.rename(it, strings.TrimSpace(f.editName.Text())) }
	th := f.h.Theme()
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title: title,
		Body: func(gtx layout.Context) layout.Dimensions {
			if !focused {
				focused = true
				gtx.Execute(key.FocusCmd{Tag: &f.editName})
			}
			return col(gtx,
				layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wBold, f.p.Text, "File name") }),
				layout.Rigid(func(gtx C) D { return D{Size: image.Pt(0, gtx.Dp(8))} }),
				layout.Rigid(func(gtx C) D {
					return popups.Field(gtx, th, &f.editName, "big-sur-trip-aerial.png", "", 40, 15)
				}),
			)
		},
		FocusStops: func() []event.Tag { return []event.Tag{&f.editName} },
		Buttons: []*popups.ModalButton{{Label: "Done", Kind: popups.BtnPrimary, Enabled: valid, Do: func() bool {
			if !valid() {
				return false
			}
			submit()
			return true
		}}},
	}))
}

// rename is editFileApi: the new title shows at once, files.edit goes
// out, and a failure puts the old one back.
func (f *Files) rename(it fileItem, name string) {
	old := it.title()
	if name == old || name == "" {
		return
	}
	id := it.ID
	f.setTitle(id, name)
	f.write(func(ctx context.Context, a *core.App) error { return a.RenameFile(ctx, id, name) }, func(err error) {
		if err != nil {
			f.setTitle(id, old) // editFileApi's catch: no toast
		}
	})
}

func (f *Files) setTitle(id, title string) {
	for i := range f.items {
		if f.items[i].ID == id {
			f.items[i].Title = title
		}
	}
}

// openFile is Slack's handleFileContainerClick from the Files view
// (source UNIFIED_FILES_EXPLORER_RESULTS) and from file attachments, by
// getMainFileAction:
//
//   - open: canvases in the main view (openCanvas), lists in full view
//     (openList);
//   - view: images in the media viewer (Clack's lightbox);
//   - remote / external: the file's link in the browser;
//   - view for PDFs, videos and audio, detail (snippets, posts, emails) and
//     download: Slack's viewer, file details view or a download. Clack's
//     viewer shows images only and it has no file details view, so these
//     open the file's page in the browser (its permalink), as message
//     files do.
func openFile(h Host, it fileItem) {
	switch {
	case it.isCanvas() && it.ID != "":
		if co, ok := h.(interface{ OpenCanvasFull(id string) }); ok {
			co.OpenCanvasFull(it.ID)
			return
		}
	case it.isList() && it.ID != "":
		if lo, ok := h.(interface{ OpenListFull(id string) }); ok {
			lo.OpenListFull(it.ID) // mount_features_lists.go
			return
		}
	case it.isImage() && it.URL != "":
		h.OpenLightbox(richtext.Image{URL: it.URL, Alt: it.title(), W: it.ThumbW, H: it.ThumbH, Title: it.title(), Proxy: true}, core.Message{})
		return
	}
	u := it.Permalink
	if u == "" {
		u = it.URL
	}
	if u == "" {
		return
	}
	if o, ok := h.(interface{ OpenURL(string) }); ok {
		o.OpenURL(u)
	}
}

// fileFromRich is a message attachment as a Files item, for openFile.
func fileFromRich(f *richtext.File) fileItem {
	it := fileItem{core.File{ID: f.ID, Name: f.Name, Title: f.Title, Filetype: f.Type, PrettyType: f.PrettyType,
		Mime: f.Mime, User: f.User, URL: f.URL, Permalink: f.Permalink, Size: f.Size, Created: f.Created,
		IsCanvas: core.IsCanvasFile(f.Type, "", f.PrettyType), IsList: core.IsListFile(f.Type, "", f.PrettyType), IsImage: f.IsImage}}
	if f.Thumb != nil {
		it.ThumbW, it.ThumbH = f.Thumb.W, f.Thumb.H
	}
	return it
}
