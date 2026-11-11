package views

// The Files sidebar's rows, ported from Slack's web client (CDN build
// 4d0c78085232, fetched without a cookie): UnifiedFilesSidebarView
// (client-unified-files-sidebar-view cNNe) builds them, BrowseSidebar
// (find-tab_browse-sidebar tpiL) and BrowseSidebarItem /
// FileCollectionHeaderRow (boot-render Fl3U) draw them.
//
//   - the views: All files (canvas-browser), Canvases (canvas-content)
//     and Lists (lists), then the divider and Deleted;
//   - without custom_file_sections: Recently viewed (spacer-1, the clock
//     sub-header and the files, when any), then spacer-2, the Starred
//     sub-header and the starred files, the empty row or five shimmer rows;
//   - with custom_file_sections: the file-collections-divider, Recently
//     viewed as a collapsible collection header with its files and the
//     recently-viewed-collections-divider, five shimmer rows while
//     files.collections.list is out, then each collection's
//     FileCollectionHeaderRow (12px above all but the first) and, unless
//     collapsed, its files in the collection's sort, or for the starred
//     collection the empty row.
//
// A file row opens the file, its × removes it (from Recently viewed, from
// Starred, or from its section) and a right-click opens
// CanvasSidebarContextMenu / ListSidebarContextMenu /
// FileSidebarContextMenu. A collection header collapses on click
// (Option-click: all of them) and its ⋮ opens
// useFileCollectionMenuTemplate (boot-render Q1Ql).

import (
	"context"
	"image"
	"image/color"
	"io"
	"strconv"
	"strings"
	"time"

	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

type sbKind uint8

const (
	sbNav        sbKind = iota // a view: All files, Canvases, Lists, Assigned to you, Deleted
	sbDivider                  // browse_divider
	sbSpacer                   // spacer (12px)
	sbSubHeader                // sub-header: Recently viewed / Starred without file sections
	sbCollection               // file_collection: a section's header (or Recently viewed's)
	sbFile                     // a canvas, list or file
	sbEmpty                    // the starred collection's empty row
	sbShimmer                  // a placeholder row
)

// sbRow is one of the sidebar's rows (Slack's rows array).
type sbRow struct {
	kind  sbKind
	key   string // Slack's row id
	nav   int    // sbNav: the filesNav index
	label string
	icon  string
	// sbCollection: the collection (core.RecentlyViewedCollection for
	// Recently viewed). sbFile: the collection the row is listed in ("" in
	// Recently viewed and the plain Starred list).
	coll string
	// sbFile: "recently_viewed", "starred" (Starred without file
	// sections) or "custom" (a collection's)
	section string
	file    core.File
	first   bool // sbCollection: the first collection (no 12px above)
	n       int  // sbShimmer: its index
}

// sbRowState is a row's widgets.
type sbRowState struct {
	row, close, more widget.Clickable
	tip, moreTip     popups.TipState
	ctx              struct{ _ byte }
	moreOpen         bool // the ⋮ menu is open (ellipsisButtonActive)
	moreSize         image.Point
}

func (f *Files) rowState(key string) *sbRowState {
	if f.sbState == nil {
		f.sbState = map[string]*sbRowState{}
	}
	s := f.sbState[key]
	if s == nil {
		s = new(sbRowState)
		f.sbState[key] = s
	}
	return s
}

// sbColors are the Files sidebar's theme colours, flattened over the
// chrome: theme-content-inv-sec (text), -pry (pry), theme-surf-inv-ter
// (hover), the divider and the selected row's.
type sbColors struct {
	chrome, text, pry, hover, rule, selBg, selFg color.NRGBA
}

func (f *Files) sbCol() sbColors {
	c := &f.th.C
	chrome := filesChrome
	if c.ChromeA.A > 0 {
		chrome = theme.Mix(c.ChromeA, c.ChromeB, 0.5)
	}
	if c.SidebarBg.A > 0 {
		chrome = flatten(c.SidebarBg, chrome)
	}
	s := sbColors{chrome: chrome, text: flatten(filesNavText, chrome), pry: rgb(0xffffff), hover: flatten(filesNavHover, chrome),
		rule: flatten(filesNavRule, chrome), selBg: filesNavSelBg, selFg: filesNavSelFg}
	if c.SidebarText.A > 0 {
		s.text = flatten(c.SidebarText, chrome)
		s.hover = flatten(c.SidebarHover, chrome)
		s.rule = flatten(c.SidebarDivider, chrome)
		s.selBg, s.selFg = flatten(c.SidebarActive, chrome), c.SidebarActiveText
	}
	if c.SidebarTextUnread.A > 0 {
		s.pry = flatten(c.SidebarTextUnread, chrome)
	}
	return s
}

// sidebarRows builds the rows (UnifiedFilesSidebarView's e8).
func (f *Files) sidebarRows() []sbRow {
	a := f.h.Core()
	rows := []sbRow{{kind: sbNav, key: "all", nav: 0}, {kind: sbNav, key: "canvases", nav: 1}, {kind: sbNav, key: "lists", nav: 2}}
	if f.deletedNavShown() {
		rows = append(rows, sbRow{kind: sbDivider, key: "horizontal-divider"}, sbRow{kind: sbNav, key: "deleted", nav: 3})
	}
	if a == nil {
		return rows
	}
	recents := f.recentRows()
	if a.FileSectionsOn() {
		colls, _ := a.FileCollections()
		if len(colls) > 0 {
			rows = append(rows, sbRow{kind: sbDivider, key: "file-collections-divider"})
		}
		if len(recents) > 0 {
			rows = append(rows, sbRow{kind: sbCollection, key: core.RecentlyViewedCollection, coll: core.RecentlyViewedCollection,
				label: nsFileColl.T("Recently viewed")})
			if !a.FileCollectionCollapsed(core.RecentlyViewedCollection) {
				rows = append(rows, recents...)
			}
			rows = append(rows, sbRow{kind: sbDivider, key: "recently-viewed-collections-divider"})
		}
		if a.FileCollectionsLoading() {
			rows = append(rows, f.shimmerRows()...)
		}
		for i, c := range colls {
			rows = append(rows, sbRow{kind: sbCollection, key: "coll:" + c.ID, coll: c.ID, label: c.Name, first: i == 0})
			if c.Collapsed {
				continue
			}
			files := core.SortCollectionFiles(c.Files, c.SortOrDefault(), f.fileFor)
			n := 0
			for _, cf := range files {
				fl, ok := f.fileFor(cf.ID)
				if !ok || !sidebarShows(fl) {
					continue
				}
				n++
				rows = append(rows, sbRow{kind: sbFile, key: cf.ID + "-" + c.ID, coll: c.ID, section: "custom", file: fl})
			}
			if n == 0 && c.Starred() {
				rows = append(rows, sbRow{kind: sbEmpty, key: "empty-collection-" + c.ID})
			}
		}
		return rows
	}
	if len(recents) > 0 {
		rows = append(rows, sbRow{kind: sbSpacer, key: "spacer-1"},
			sbRow{kind: sbSubHeader, key: core.RecentlyViewedCollection, label: nsSearch.T("Recently viewed"), icon: "clock"})
		rows = append(rows, recents...)
	}
	rows = append(rows, sbRow{kind: sbSpacer, key: "spacer-2"},
		sbRow{kind: sbSubHeader, key: "starred-canvas-header", label: nsSearch.T("Starred"), icon: "star"})
	st, ok, _ := f.starred.get()
	switch {
	case !ok:
		rows = append(rows, f.shimmerRows()...)
	case len(st) == 0:
		rows = append(rows, sbRow{kind: sbEmpty, key: "empty-starred-lists"})
	default:
		for _, sf := range st {
			rows = append(rows, sbRow{kind: sbFile, key: sf.ID, section: "starred", file: sf})
		}
	}
	return rows
}

func (f *Files) shimmerRows() []sbRow {
	out := make([]sbRow, 5)
	for i := range out {
		out[i] = sbRow{kind: sbShimmer, key: "placeholder-list-" + strconv.Itoa(i), n: i}
	}
	return out
}

// recentRows are Recently viewed's rows: getRecentlyViewedFileIds' files
// the client knows, less the deleted ones and (without file sections) the
// starred ones.
func (f *Files) recentRows() []sbRow {
	a := f.h.Core()
	var out []sbRow
	for _, id := range a.RecentlyViewedFileIDs() {
		fl, ok := f.fileFor(id)
		if !ok || !sidebarShows(fl) || !a.FileSectionsOn() && a.FileStarred(id) {
			continue
		}
		out = append(out, sbRow{kind: sbFile, key: id, section: "recently_viewed", file: fl})
	}
	return out
}

// sidebarShows is the rows' filter: isFileDeleted and isTombstone files
// (and the ones files.info couldn't read) stay out.
func sidebarShows(fl core.File) bool { return fl.ID != "" && fl.Mode != "tombstone" }

// fileFor is the file store's getFileById for the sidebar: the files the
// browser, Starred or files.info loaded; an unknown id is fetched
// (files.info) and false until it answers.
func (f *Files) fileFor(id string) (core.File, bool) {
	if fl, ok := f.known[id]; ok {
		return fl, fl.ID != ""
	}
	for i := range f.items {
		if f.items[i].ID == id {
			f.learn(f.items[i].File)
			return f.items[i].File, true
		}
	}
	if f.asking[id] || f.h.Core() == nil {
		return core.File{}, false
	}
	if f.asking == nil {
		f.asking = map[string]bool{}
	}
	f.asking[id] = true
	a := f.h.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		fl, err := a.FileInfo(ctx, id)
		f.post(func() {
			delete(f.asking, id)
			if err != nil {
				fl = core.File{} // not visible or gone: the row stays out
			}
			if f.known == nil {
				f.known = map[string]core.File{}
			}
			f.known[id] = fl
		})
	}()
	return core.File{}, false
}

// learn keeps a loaded file for the sidebar.
func (f *Files) learn(fl core.File) {
	if fl.ID == "" {
		return
	}
	if f.known == nil {
		f.known = map[string]core.File{}
	}
	f.known[fl.ID] = fl
}

// sidebarUpdate runs last frame's rows' clicks.
func (f *Files) sidebarUpdate(gtx C) {
	a := f.h.Core()
	for _, r := range f.sbRows {
		s := f.sbState[r.key]
		if s == nil {
			continue
		}
		switch r.kind {
		case sbNav:
			if s.row.Clicked(gtx) {
				f.selectNav(r.nav)
			}
		case sbCollection:
			// the ⋮ sits inside the row, and Gio hands a click to the
			// enclosing row's handler too: a click on it doesn't toggle
			more := s.more.Clicked(gtx)
			for {
				c, ok := s.row.Update(gtx)
				if !ok {
					break
				}
				if ctxclick.Context(c) || more {
					continue
				}
				switch {
				case c.Modifiers.Contain(key.ModAlt): // expand / collapse every collection
					a.SetAllFileCollectionsCollapsed(!a.FileCollectionCollapsed(r.coll))
				default:
					a.ToggleFileCollectionCollapsed(r.coll)
				}
			}
			if more {
				f.openCollectionMenu(r.coll, f.buttonRect(&s.more, s.moreSize))
			}
			f.ctxMenuEvents(gtx, s, func() {
				if r.coll != core.RecentlyViewedCollection {
					f.showMenuAtPointer(f.collectionMenuItems(r.coll), 0)
				}
			})
		case sbFile:
			if f.organize { // Bulk manage: a click picks the file
				if s.row.Clicked(gtx) {
					f.picked[r.file.ID] = !f.picked[r.file.ID]
					if !f.picked[r.file.ID] {
						delete(f.picked, r.file.ID)
					}
				}
				continue
			}
			closed := s.close.Clicked(gtx) // inside the row: its click reaches the row too
			if ctxclick.Clicked(gtx, &s.row) && !closed {
				openFile(f.h, fileItem{r.file})
			}
			if closed {
				f.dismissRow(r)
			}
			f.ctxMenuEvents(gtx, s, func() { f.showMenuAtPointer(f.fileRowMenu(r), 220) })
		}
	}
}

// ctxMenuEvents runs open on a context press on the row.
func (f *Files) ctxMenuEvents(gtx C, s *sbRowState, open func()) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &s.ctx, Kinds: pointer.Press})
		if !ok {
			return
		}
		if e, ok := ev.(pointer.Event); ok && ctxclick.Press(e) {
			open()
		}
	}
}

// selectNav picks a view (a click on All files, Canvases, …).
func (f *Files) selectNav(i int) {
	if f.nav != i {
		f.nav, f.reload = i, true
		f.search.ph = filesNav[i].placeholder
	}
	if c, ok := f.h.(PaneChooser); ok {
		c.PaneChosen()
	}
}

// dismissRow is a file row's ×: Recently viewed's removeOpenFileAndPersist,
// Starred's unstar (removeCanvasFromFavorites / removeListFromFavorites),
// a section's removeFileFromCollectionThunk.
func (f *Files) dismissRow(r sbRow) {
	a := f.h.Core()
	switch r.section {
	case "recently_viewed":
		a.RemoveOpenFile(r.file.ID)
	case "starred":
		f.toggleStar(r.file)
	case "custom":
		f.removeFromCollection(r.file, r.coll, false)
	}
}

// showMenuAtPointer opens items as a context menu at the pointer.
func (f *Files) showMenuAtPointer(items []popups.MenuItem, width float32) {
	ph, ok := f.h.(popups.Host)
	if !ok || len(items) == 0 {
		return
	}
	var at image.Point
	if pp, ok := f.h.(interface{ PointerPos() image.Point }); ok {
		at = pp.PointerPos()
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: image.Rectangle{Min: at, Max: at}, Width: width}))
}

// ---- drawing ----

// sidebarList draws the rows under the header (BrowseSidebarList: one
// scrolling list).
func (f *Files) sidebarList(gtx C, sc sbColors) D {
	rows := f.sidebarRows()
	f.sbRows = rows
	f.sbList.Axis = layout.Vertical
	return f.sbList.Layout(gtx, len(rows), func(gtx C, i int) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return f.sidebarRow(gtx, rows[i], sc)
	})
}

func (f *Files) sidebarRow(gtx C, r sbRow, sc sbColors) D {
	switch r.kind {
	case sbNav:
		return f.navRow(gtx, r, sc)
	case sbDivider:
		return f.sidebarDivider(gtx, sc.rule)
	case sbSpacer: // .p-browse_sidebar__spacer
		return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(12))}
	case sbSubHeader:
		return f.subHeader(gtx, r, sc)
	case sbCollection:
		return f.collectionHeader(gtx, r, sc)
	case sbFile:
		return f.fileRow(gtx, r, sc)
	case sbEmpty:
		// the "empty" row: EmptyState (isExtended) in the 8px item
		// container with padding_left_150 (24) padding_right_50 (8)
		// padding_top_0; the description's 8px margin and --extended's
		// 20px bottom padding under it
		return layout.Inset{Left: 24, Right: 8, Bottom: 8 + 20}.Layout(gtx, f.starHint)
	case sbShimmer:
		return f.shimmerRow(gtx, r.n, sc.text)
	}
	return D{}
}

// navRow is a view's browse-sidebar-item: 28px, radius 4, the 16px icon
// 8px before the 15px label; the selected one in theme-base-hgl-1.
func (f *Files) navRow(gtx C, r sbRow, sc sbColors) D {
	s := f.rowState(r.key)
	nv := filesNav[r.nav]
	return s.row.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		w, h := gtx.Constraints.Max.X, gtx.Dp(28)
		sel := f.nav == r.nav
		fg := sc.text
		switch {
		case sel:
			rrect(gtx, image.Rect(0, 0, w, h), gtx.Dp(4), sc.selBg)
			fg = sc.selFg
		case s.row.Hovered():
			rrect(gtx, image.Rect(0, 0, w, h), gtx.Dp(4), sc.hover)
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		layout.Inset{Left: 8, Right: 8}.Layout(g, func(gtx C) D {
			return layout.W.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return f.icon(gtx, nv.icon, 16, fg) }),
					hgap(8),
					layout.Flexed(1, func(gtx C) D { return f.label(gtx, 15, wReg, fg, nsFiles.T(nv.label)) }), // UnifiedFilesExplorer
				)
			})
		})
		return D{Size: image.Pt(w, h)}
	})
}

// subHeader is browse_sidebar_sub_header: 28px, padding 0 8px 0 20px from
// the list's edge, .p-browse_sidebar's override (15px, weight 400, the
// sidebar's text colour), the 16px icon 8px before the label.
func (f *Files) subHeader(gtx C, r sbRow, sc sbColors) D {
	g := gtx
	g.Constraints.Min.Y = gtx.Dp(28)
	return layout.Inset{Left: 12}.Layout(g, func(gtx C) D {
		return layout.W.Layout(gtx, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return f.icon(gtx, r.icon, 16, sc.text) }),
				hgap(8),
				layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wReg, sc.text, r.label) }),
			)
		})
	})
}

// collectionHeader is FileCollectionHeaderRow: .container (radius 4, the
// theme-surf-inv-ter hover fill, padding-right 8, 12px above unless it's
// the first collection) holding the 28px .fileCollectionRow (padding 0
// 8px, 15px) with the section's emoji, the star (the starred
// collection), "section" or Recently viewed's clock, which the collapse
// caret (caret-down-filled / caret-right-filled) replaces on hover, 8px
// before the label; then the 20px ⋮ (ellipsis-vertical-filled, "More
// actions"), shown on hover or while its menu is open. Recently viewed has
// no ⋮ and no spacing.
func (f *Files) collectionHeader(gtx C, r sbRow, sc sbColors) D {
	a := f.h.Core()
	s := f.rowState(r.key)
	recents := r.coll == core.RecentlyViewedCollection
	top := unit.Dp(12)
	if r.first || recents {
		top = 0
	}
	var coll core.FileCollection
	if !recents {
		coll, _ = a.FileCollection(r.coll)
	}
	collapsed := a.FileCollectionCollapsed(r.coll)
	return layout.Inset{Top: top}.Layout(gtx, func(gtx C) D {
		w, h := gtx.Constraints.Max.X, gtx.Dp(28)
		d := s.row.Layout(gtx, func(gtx C) D {
			pointerCursor(gtx)
			hov := s.row.Hovered() || s.more.Hovered() || s.moreOpen
			if hov {
				rrect(gtx, image.Rect(0, 0, w, h), gtx.Dp(4), sc.hover)
			}
			bx := w // the buttons' left edge
			if !recents && (hov || s.moreOpen) {
				bw := gtx.Dp(20)
				bx = w - gtx.Dp(8) - bw
				func() {
					defer op.Offset(image.Pt(bx, (h-bw)/2)).Push(gtx.Ops).Pop()
					g := gtx
					g.Constraints = layout.Exact(image.Pt(bw, bw))
					s.moreSize = image.Pt(bw, bw)
					s.more.Layout(g, func(gtx C) D {
						pointerCursor(gtx)
						if s.more.Hovered() || s.moreOpen {
							rrect(gtx, image.Rect(0, 0, bw, bw), gtx.Dp(4), sc.hover)
						}
						layout.Center.Layout(gtx, func(gtx C) D { return f.icon(gtx, "ellipsis-vertical-filled", 16, sc.text) })
						return D{Size: image.Pt(bw, bw)}
					})
					popups.Tooltip(gtx, f.th, &s.moreTip, s.more.Hovered(), image.Pt(bw, bw), popups.Above, nsFileColl.T("More actions"), "")
				}()
				bx -= gtx.Dp(4)
			}
			g := gtx
			g.Constraints = layout.Exact(image.Pt(max(0, bx), h))
			layout.Inset{Left: 8, Right: 8}.Layout(g, func(gtx C) D {
				return layout.W.Layout(gtx, func(gtx C) D {
					return row(gtx, layout.Middle,
						layout.Rigid(func(gtx C) D {
							if hov {
								caret := "caret-down-filled"
								if collapsed {
									caret = "caret-right-filled"
								}
								return f.icon(gtx, caret, 16, sc.text)
							}
							if recents {
								return f.icon(gtx, "clock", 16, sc.text)
							}
							return f.collectionIcon(gtx, coll, false, 16, sc.text)
						}),
						hgap(8),
						layout.Flexed(1, func(gtx C) D { return f.label(gtx, 15, wReg, sc.text, r.label) }),
					)
				})
			})
			return D{Size: image.Pt(w, h)}
		})
		f.ctxArea(gtx, s, d.Size)
		return d
	})
}

// collectionIcon is FileSectionEmoji: the section's emoji, the star
// (star-filled when selected) for the starred collection, else "section".
func (f *Files) collectionIcon(gtx C, c core.FileCollection, selected bool, size unit.Dp, col color.NRGBA) D {
	if c.Emoji != "" {
		if u, _, ok := f.h.Core().Emoji(strings.Trim(c.Emoji, ":")); ok && u != "" {
			return f.emojiImg(gtx, u, size)
		}
	}
	if c.Starred() {
		if selected {
			return f.icon(gtx, "star-filled", size, col)
		}
		return f.icon(gtx, "star", size, col)
	}
	return f.icon(gtx, "section", size, col)
}

// ctxArea registers the row's context-press target over sz (passing
// clicks through to the row).
func (f *Files) ctxArea(gtx C, s *sbRowState, sz image.Point) {
	st := clip.Rect(image.Rectangle{Max: sz}).Push(gtx.Ops)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &s.ctx)
	pass.Pop()
	st.Pop()
}

// fileRow is a canvas or list browse-sidebar-item
// (p-browse_sidebar__item--collection_indent: 24px in; the file's
// product icon, canvas-content / lists / file, 16px in a 20px box 8px
// before the 15px title), the radius 4 hover fill, and on hover the
// p-browse_sidebar__item_close_button (a 28px "close" IconButton at .64,
// 1 under the pointer). In Bulk manage the row's checkbox comes first and
// a click picks it.
func (f *Files) fileRow(gtx C, r sbRow, sc sbColors) D {
	s := f.rowState(r.key)
	it := fileItem{r.file}
	editMode := f.organize
	picked := editMode && f.picked[r.file.ID]
	d := s.row.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		w, h := gtx.Constraints.Max.X, gtx.Dp(28)
		fg := sc.text
		hov := s.row.Hovered() || s.close.Hovered()
		switch {
		case picked:
			rrect(gtx, image.Rect(0, 0, w, h), gtx.Dp(4), sc.selBg)
			fg = sc.selFg
		case hov:
			rrect(gtx, image.Rect(0, 0, w, h), gtx.Dp(4), sc.hover)
		}
		right := w
		if hov && !editMode {
			bw := gtx.Dp(28)
			right = w - bw
			func() {
				defer op.Offset(image.Pt(right, 0)).Push(gtx.Ops).Pop()
				g := gtx
				g.Constraints = layout.Exact(image.Pt(bw, h))
				s.close.Layout(g, func(gtx C) D {
					pointerCursor(gtx)
					c := theme.WithAlpha(fg, uint8(int(fg.A)*64/100))
					if s.close.Hovered() {
						c = fg
					}
					return layout.Center.Layout(gtx, func(gtx C) D { return f.icon(gtx, "close", 16, c) })
				})
			}()
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(max(0, right), h))
		layout.Inset{Left: 24, Right: 8}.Layout(g, func(gtx C) D {
			return layout.W.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D {
						if !editMode {
							return D{}
						}
						return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return f.sbCheckbox(gtx, picked, sc) })
					}),
					layout.Rigid(func(gtx C) D {
						sz := gtx.Dp(20)
						g := gtx
						g.Constraints = layout.Exact(image.Pt(sz, sz))
						return layout.Center.Layout(g, func(gtx C) D { return f.icon(gtx, sidebarFileIcon(&it), 16, fg) })
					}),
					hgap(8),
					layout.Flexed(1, func(gtx C) D { return f.label(gtx, 15, wReg, fg, it.title()) }),
				)
			})
		})
		return D{Size: image.Pt(w, h)}
	})
	f.ctxArea(gtx, s, d.Size)
	return d
}

// sidebarFileIcon is the rows' SmallFileListEntity icon: canvas-content
// for canvases, lists for lists, file for the rest.
func sidebarFileIcon(it *fileItem) string {
	switch {
	case it.isCanvas():
		return "canvas-content"
	case it.isList():
		return "lists"
	}
	return "files"
}

// sbCheckbox is Bulk manage's row checkbox (QG0J Checkbox, 16px, radius 4).
func (f *Files) sbCheckbox(gtx C, on bool, sc sbColors) D {
	sz := gtx.Dp(16)
	r := image.Rect(0, 0, sz, sz)
	if on {
		rrect(gtx, r, gtx.Dp(4), f.lt(rgb(0x1264a3), rgb(0x1d9bd1)))
		g := gtx
		g.Constraints = layout.Exact(image.Pt(sz, sz))
		layout.Center.Layout(g, func(gtx C) D { return f.icon(gtx, "check", 14, rgb(0xffffff)) })
	} else {
		border(gtx, r, gtx.Dp(4), max(1, gtx.Dp(1)), sc.text)
	}
	return D{Size: image.Pt(sz, sz)}
}

// shimmerRow is a placeholder (type "shimmer": p-more_files_sidebar__
// item--shimmer, 28px, the lists / canvas-content icon in a 20px
// .c-file_entity__icon box, 8px, then a Shimmer bar of random width).
func (f *Files) shimmerRow(gtx C, i int, fg color.NRGBA) D {
	widths := [...]int{62, 48, 70, 54, 40}
	w, h := gtx.Constraints.Max.X, gtx.Dp(28)
	icon := "lists"
	if i%2 == 1 {
		icon = "canvas-content"
	}
	x := gtx.Dp(8)
	at := op.Offset(image.Pt(x+(gtx.Dp(20)-gtx.Dp(16))/2, (h-gtx.Dp(16))/2)).Push(gtx.Ops)
	f.icon(gtx, icon, 16, theme.WithAlpha(fg, fg.A/2))
	at.Pop()
	bx := x + gtx.Dp(20) + gtx.Dp(8)
	bw := (w - bx - gtx.Dp(8)) * widths[i%len(widths)] / 100
	rrect(gtx, image.Rect(bx, (h-gtx.Dp(10))/2, bx+bw, (h+gtx.Dp(10))/2), gtx.Dp(5), theme.WithAlpha(fg, 0x26))
	return D{Size: image.Pt(w, h)}
}

// ---- menus ----

// canCreateSection is useFileCollectionMenuTemplate's and
// FileSectionMoveToMenu's isOnFreePlan check: free teams can't add
// sections.
func (f *Files) canCreateSection() bool { return !f.h.Core().OnFreePlan() }

// collectionMenuItems is useFileCollectionMenuTemplate: "Create…" with
// New canvas and New list, Sidebar section (not on a free plan), then for
// the starred collection a divider and "Sort by", for a custom one a
// divider, Rename section, Sort by, a divider and Delete section.
func (f *Files) collectionMenuItems(id string) []popups.MenuItem {
	a := f.h.Core()
	c, ok := a.FileCollection(id)
	if !ok {
		return nil
	}
	items := []popups.MenuItem{
		{Header: nsFileColl.T("Create…")},
		{Key: "new_canvas", Label: nsFileColl.T("New canvas"), Icon: "add-channel-canvas", Do: func() { f.createCanvasIn(id) }},
		{Key: "new_list", Label: nsFileColl.T("New list"), Icon: "lists", Do: func() { f.createListIn(id) }},
	}
	if f.canCreateSection() {
		items = append(items, popups.MenuItem{Key: "file_section", Label: nsFileColl.T("Sidebar section"), Icon: "section", Do: func() { f.sectionModal("") }})
	}
	cur := c.SortOrDefault()
	sortItem := func(key, label, v string) popups.MenuItem {
		return popups.MenuItem{Key: key, Label: nsFileColl.T(label), Radio: true, Checked: cur == v, Do: func() { f.setCollectionSort(id, v) }}
	}
	sortBy := popups.MenuItem{Key: "sort-by", Label: nsFileColl.T("Sort by"), Sub: []popups.MenuItem{
		sortItem("sort-last-viewed", "Last viewed", core.CollectionSortRecentlyViewed),
		sortItem("sort-last-updated", "Last updated", core.CollectionSortLastUpdated),
		sortItem("sort-a-z", "A-Z", core.CollectionSortAlpha),
		sortItem("sort-date-added", "Date added", core.CollectionSortDateAdded),
	}}
	if c.Starred() {
		return append(items, popups.Sep(), sortBy)
	}
	return append(items, popups.Sep(),
		popups.MenuItem{Key: "rename", Label: nsFileColl.T("Rename section"), Icon: "edit", Do: func() { f.sectionModal(id) }},
		sortBy, popups.Sep(),
		popups.MenuItem{Key: "delete_section", Label: nsFileColl.T("Delete section"), Icon: "trash", Danger: true, Do: func() { f.deleteSection(id) }})
}

// openCollectionMenu is the header's ⋮: a Popover at right-bottom with
// offsetX -4, offsetY 4 and tryOppositePosition, 300 wide.
func (f *Files) openCollectionMenu(id string, btn image.Rectangle) {
	ph, ok := f.h.(popups.Host)
	if !ok {
		return
	}
	items := f.collectionMenuItems(id)
	if len(items) == 0 {
		return
	}
	s := f.rowState("coll:" + id)
	s.moreOpen = true
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: btn, Width: 300, Popover: "right-bottom",
		OffsetX: -4, OffsetY: 4, TryOpposite: true, OnClose: func() { s.moreOpen = false }}))
}

// fileRowMenu is the row's context menu: ListSidebarContextMenu for lists,
// CanvasSidebarContextMenu for canvases, FileSidebarContextMenu for the
// rest, with "Move to section" (includeMoveToSection under
// custom_file_sections), the copy link (useCopyLinkMenuItem), "Open in
// split view" (not in the simplified layout), "Unstar canvas" / "Unstar
// list" on Starred without file sections and "Remove from recently viewed"
// on Recently viewed. Left out, as in the Files view's ⋯: View canvas /
// list details, Share canvas / list and Open in new window (Clack has no
// file details view or child windows).
func (f *Files) fileRowMenu(r sbRow) []popups.MenuItem {
	a := f.h.Core()
	it := fileItem{r.file}
	id := r.file.ID
	sections := a.FileSectionsOn()
	starred := r.section != "recently_viewed" // in Starred or a section (isStarred: c3 / uU)
	var head []popups.MenuItem
	if sections {
		head = append(head, popups.MenuItem{Key: "move-to-section", Label: nsMenu.T("Move to section"), Icon: "folder",
			Sub: f.moveToItems(r.file, true, "file_sidebar")}, popups.Sep())
	}
	var mid []popups.MenuItem
	if link := it.Permalink; link != "" {
		mid = append(mid, popups.MenuItem{Key: "copy_file_link", Label: copyLinkLabel(&it), DoCtx: func(gtx layout.Context) {
			gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(link))})
			f.h.Toast(nsFile.T("Link copied to clipboard"))
		}})
	}
	if !it.isCanvas() && !it.isList() {
		// FileSidebarContextMenu: the canvas items, then Remove from
		// recently viewed after a separator (separators trimmed)
		out := append(head, mid...)
		if !starred {
			if len(out) > 0 && !out[len(out)-1].Separator {
				out = append(out, popups.Sep())
			}
			out = append(out, popups.MenuItem{Label: nsFile.T("Remove from recently viewed"), Do: func() { f.removeRecent(r) }})
		}
		return trimSeps(out)
	}
	var split []popups.MenuItem
	if it.isCanvas() {
		if co, ok := f.h.(interface{ OpenCanvas(id string) }); ok {
			split = append(split, popups.MenuItem{Key: popups.SplitKey, Label: nsFile.T("Open in split view"), Icon: "move-to-split-view", Do: func() { co.OpenCanvas(id) }})
		}
	} else if lo, ok := f.h.(interface{ OpenList(id string) }); ok {
		split = append(split, popups.MenuItem{Key: popups.SplitKey, Label: nsLists.T("Open in split view"), Icon: "move-to-split-view", Do: func() { lo.OpenList(id) }})
	}
	m := !starred || sections // Unstar hidden
	var tail []popups.MenuItem
	if !m {
		label := nsFile.T("Unstar canvas") // CanvasSidebarContextMenu
		if it.isList() {
			label = nsLists.T("Unstar list") // ListSidebarContextMenu
		}
		file := r.file
		tail = append(tail, popups.MenuItem{Label: label, Do: func() { f.toggleStar(file) }})
	}
	if !starred {
		recent := nsFile.T("Remove from recently viewed")
		if it.isList() {
			recent = nsLists.T("Remove from recently viewed")
		}
		tail = append(tail, popups.MenuItem{Label: recent, Do: func() { f.removeRecent(r) }})
	}
	out := append(append([]popups.MenuItem{}, head...), mid...)
	out = append(out, popups.Sep())
	out = append(out, split...)
	if !(m && starred) && len(tail) > 0 { // separator-surfaces / separator-open-in-new-window
		out = append(out, popups.Sep())
	}
	out = append(out, tail...)
	return trimSeps(out)
}

// trimSeps drops leading, trailing and doubled separators.
func trimSeps(in []popups.MenuItem) []popups.MenuItem {
	var out []popups.MenuItem
	for _, it := range in {
		if it.Separator && (len(out) == 0 || out[len(out)-1].Separator) {
			continue
		}
		out = append(out, it)
	}
	for len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}

// removeRecent is "Remove from recently viewed" (removeOpenFileAndPersist).
func (f *Files) removeRecent(r sbRow) { f.h.Core().RemoveOpenFile(r.file.ID) }

// moveToItems is FileSectionMoveToMenu (boot-shared QLxC): "Move to…",
// every collection as a radio row (its emoji, star or section icon and
// name; a click on the current one takes the file out), then a divider
// and "Create section" (not on a free plan), and "Remove from {name}"
// when the file is in one. addToRecents puts a removed file back in
// Recently viewed (addToRecentsOnRemove).
func (f *Files) moveToItems(file core.File, addToRecents bool, source string) []popups.MenuItem {
	a := f.h.Core()
	if !a.FileSectionsOn() {
		return nil
	}
	colls, _ := a.FileCollections()
	cur, in := a.FileCollectionOf(file.ID)
	items := []popups.MenuItem{{Header: nsFiles.T("Move to…")}} // FileSectionMoveToMenu
	for _, c := range colls {
		c := c
		sel := in && cur.ID == c.ID
		items = append(items, popups.MenuItem{Key: c.ID, Label: c.Name, Radio: true, Checked: sel,
			LabelText: func(gtx layout.Context, col color.NRGBA) layout.Dimensions {
				return f.collectionName(gtx, c, sel, col)
			},
			Do: func() {
				if sel {
					f.removeFromCollection(file, c.ID, addToRecents)
					return
				}
				f.moveToCollection(file, c, false)
			}})
	}
	if f.canCreateSection() {
		items = append(items, popups.Sep(), popups.MenuItem{Key: "create-section", Label: nsFiles.T("Create section"), Icon: "plus",
			Do: func() { f.sectionModalThen("", func(c core.FileCollection) { f.moveToCollection(file, c, false) }) }})
	}
	if in {
		cid := cur.ID
		items = append(items, popups.MenuItem{Key: "remove-from-collection", Label: nsFiles.T("Remove from {collection}", "collection", cur.Name),
			Do: func() { f.removeFromCollection(file, cid, addToRecents) }})
	}
	return items
}

// collectionName is FileSectionName: FileSectionEmoji, 4px, the name.
func (f *Files) collectionName(gtx C, c core.FileCollection, sel bool, col color.NRGBA) D {
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D { return f.collectionIcon(gtx, c, sel, 16, col) }),
		hgap(4),
		layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wReg, col, c.Name) }),
	)
}

// ---- writes ----

// moveToCollection is moveFileToCollection: the file joins the collection
// at once and "Added {file} to {section}." toasts with Undo (the Files
// view is the primary view), which moves it back to where it was or out
// again; quiet skips the toast (bulk moves).
func (f *Files) moveToCollection(file core.File, c core.FileCollection, quiet bool) {
	a := f.h.Core()
	f.learn(file)
	from := ""
	if prev, ok := a.FileCollectionOf(file.ID); ok {
		from = prev.ID
	}
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := a.MoveFileToCollection(ctx, file.ID, c.ID)
		done <- err
		f.h.Invalidate()
	}()
	if quiet {
		return
	}
	name := c.Name
	if c.Emoji != "" {
		name = c.Emoji + " " + name
	}
	it := fileItem{file}
	text := nsFiles.T("Added {file} to {section}.", "file", it.title(), "section", name) // MoveFileToast
	undo := func() {
		f.write(func(ctx context.Context, a *core.App) error {
			<-done
			if from != "" {
				_, err := a.MoveFileToCollection(ctx, file.ID, from)
				return err
			}
			return a.RemoveFileFromCollection(ctx, file.ID, c.ID)
		}, func(error) {})
	}
	if tu, ok := f.h.(interface{ ToastUndo(string, func()) }); ok {
		tu.ToastUndo(text, undo)
		return
	}
	f.h.Toast(text)
}

// removeFromCollection is removeFileFromCollectionThunk and, with
// addToRecents, addOpenFileAndPersist right after it (not for files the
// media viewer opens).
func (f *Files) removeFromCollection(file core.File, id string, addToRecents bool) {
	f.learn(file)
	f.write(func(ctx context.Context, a *core.App) error { return a.RemoveFileFromCollection(ctx, file.ID, id) }, func(error) {})
	if it := (fileItem{file}); addToRecents && !it.isImage() {
		f.h.Core().AddOpenFile(file.ID)
	}
}

// setCollectionSort is changeCollectionSort.
func (f *Files) setCollectionSort(id, sortBy string) {
	f.write(func(ctx context.Context, a *core.App) error { return a.SetFileCollectionSort(ctx, id, sortBy) }, func(error) {})
}

// createCanvasIn is createCanvasInCollection: a new canvas, moved into the
// collection, then opened.
func (f *Files) createCanvasIn(id string) {
	var made core.File
	f.write(func(ctx context.Context, a *core.App) error {
		cid, err := a.CreateCanvas(ctx, "", "", "")
		if err != nil {
			return err
		}
		made = core.File{ID: cid, Title: "Untitled", IsCanvas: true, Filetype: "quip"}
		if fl, err := a.FileInfo(ctx, cid); err == nil {
			made = fl
		}
		_, err = a.MoveFileToCollection(ctx, cid, id)
		return err
	}, func(err error) {
		if made.ID != "" {
			f.learn(made)
			if co, ok := f.h.(interface{ OpenCanvasFull(id string) }); ok {
				co.OpenCanvasFull(made.ID)
			}
		}
		if err != nil && made.ID == "" {
			f.h.Toast("Couldn't create the canvas: " + friendlyErr(err))
		}
	})
}

// createListIn is createListInCollection: a new list (navigateAfterCreate),
// moved into the collection.
func (f *Files) createListIn(id string) {
	var made core.File
	f.write(func(ctx context.Context, a *core.App) error {
		lid, err := a.CreateList(ctx, "", "")
		if err != nil {
			return err
		}
		made = core.File{ID: lid, IsList: true, Filetype: "list"}
		if fl, err := a.FileInfo(ctx, lid); err == nil {
			made = fl
		}
		_, err = a.MoveFileToCollection(ctx, lid, id)
		return err
	}, func(err error) {
		if made.ID != "" {
			f.learn(made)
			if lo, ok := f.h.(interface{ OpenListFull(id string) }); ok {
				lo.OpenListFull(made.ID)
			}
		}
		if err != nil && made.ID == "" {
			f.h.Toast("Couldn't create the list: " + friendlyErr(err))
		}
	})
}

// deleteSection is deleteFileCollection: DeleteFileCollectionModal
// ("Delete sidebar section?", "Are you sure you want to delete
// <strong>{name}</strong>?", Cancel and Confirm, the default primary
// submit, focused), then files.collections.delete.
func (f *Files) deleteSection(id string) {
	ph, ok := f.h.(popups.Host)
	if !ok {
		return
	}
	c, ok := f.h.Core().FileCollection(id)
	if !ok {
		return
	}
	confirm := &popups.ModalButton{Label: nsDialog.T("Confirm"), Kind: popups.BtnPrimary, Do: func() bool {
		f.write(func(ctx context.Context, a *core.App) error { return a.DeleteFileCollection(ctx, id) }, func(err error) {
			if err != nil {
				f.h.Toast("Couldn’t delete the section: " + friendlyErr(err))
			}
		})
		return true
	}}
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title:     nsFileColl.T("Delete sidebar section?"),
		AutoFocus: confirm,
		Body: func(gtx layout.Context) layout.Dimensions {
			// "Are you sure you want to delete <strong>{collectionName}</strong>?",
			// the name (FileSectionName) where the locale puts it
			before, after := around(nsFileColl, "Are you sure you want to delete <strong>{collectionName}</strong>?", "collectionName")
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wReg, f.p.Text, before) }),
				layout.Rigid(func(gtx C) D { return f.collectionIcon(gtx, c, false, 16, f.p.Text) }),
				hgap(4),
				layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wBold, f.p.Text, c.Name) }),
				layout.Flexed(1, func(gtx C) D { return f.label(gtx, 15, wReg, f.p.Text, after) }),
			)
		},
		Buttons: []*popups.ModalButton{{Label: nsDialog.T("Cancel"), Kind: popups.BtnOutline}, confirm}, // the generic dialog's
	}))
}

// sectionModal is FileSectionCreateModal: "Create a sidebar section" (or
// for id "Rename this section"), "Name your section" over the 36px input
// with the emoji picker in its first 44px (a 1px right border) and "Clear
// all" (close-filled) at its right once there's a name or an emoji
// (placeholder "Enter a section name"), Close (outline) and Save
// (primary, off without a name). Save runs files.collections.create
// {name, emoji} or files.collections.update {collection, name, emoji}.
func (f *Files) sectionModal(id string) { f.sectionModalThen(id, nil) }

func (f *Files) sectionModalThen(id string, created func(core.FileCollection)) {
	ph, ok := f.h.(popups.Host)
	if !ok {
		return
	}
	a := f.h.Core()
	title := nsFiles.T("Create a sidebar section") // FileSectionCreateModal
	var cur core.FileCollection
	if id != "" {
		c, ok := a.FileCollection(id)
		if !ok {
			return
		}
		cur, title = c, nsFiles.T("Rename this section")
	}
	st := &sectionModalState{emoji: cur.Emoji}
	st.ed.SingleLine, st.ed.Submit = true, true
	st.ed.SetText(cur.Name)
	st.ed.SetCaret(st.ed.Len(), st.ed.Len())
	var m *popups.Modal
	save := func() bool {
		name := strings.TrimSpace(st.ed.Text())
		if name == "" || st.pending {
			return false
		}
		st.pending = true
		emoji := st.emoji
		var made core.FileCollection
		f.write(func(ctx context.Context, a *core.App) error {
			if id != "" {
				return a.RenameFileCollection(ctx, id, name, emoji)
			}
			c, err := a.CreateFileCollection(ctx, name, emoji)
			made = c
			return err
		}, func(err error) {
			st.pending = false
			if err != nil {
				f.h.Toast("Couldn’t save the section: " + friendlyErr(err))
				return
			}
			m.Dismiss()
			if created != nil && made.ID != "" {
				created(made)
			}
		})
		return false
	}
	saveBtn := &popups.ModalButton{Label: nsFiles.T("Save"), Kind: popups.BtnPrimary, Enabled: func() bool {
		return strings.TrimSpace(st.ed.Text()) != "" && !st.pending
	}, Do: save}
	m = popups.NewModal(ph, popups.ModalOpts{
		Title: title,
		Body: func(gtx layout.Context) layout.Dimensions {
			return f.sectionModalBody(gtx, ph, &m, st, save)
		},
		FocusStops: func() []event.Tag { return []event.Tag{&st.ed} },
		Buttons:    []*popups.ModalButton{{Label: nsFiles.T("Close"), Kind: popups.BtnOutline}, saveBtn},
	})
	ph.ShowOverlay(m)
}

// sectionModalState is the section modal's input.
type sectionModalState struct {
	ed                 widget.Editor
	emoji              string
	emojiBtn, clearBtn widget.Clickable
	focused, pending   bool
	picked             chan string
}

func (f *Files) sectionModalBody(gtx C, ph popups.Host, m **popups.Modal, st *sectionModalState, save func() bool) D {
	if !st.focused {
		st.focused = true
		gtx.Execute(key.FocusCmd{Tag: &st.ed})
	}
	for {
		e, ok := st.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok {
			save()
		}
	}
	if st.picked != nil {
		select {
		case name := <-st.picked:
			st.emoji = ":" + strings.Trim(name, ":") + ":"
		default:
		}
	}
	if st.emojiBtn.Clicked(gtx) && *m != nil {
		if st.picked == nil {
			st.picked = make(chan string, 1)
		}
		ch := st.picked
		at := popups.ModalBodyPoint(gtx, *m, image.Pt(0, gtx.Dp(4+22+8+36)))
		ph.OpenEmojiPicker(at, func(name string) {
			select {
			case ch <- name:
			default:
			}
			f.h.Invalidate()
		})
	}
	if st.clearBtn.Clicked(gtx) { // "Clear all": the name and the emoji
		st.ed.SetText("")
		st.emoji = ""
		gtx.Execute(key.FocusCmd{Tag: &st.ed})
	}
	return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D { // margin_top_25
		return col(gtx,
			layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wBold, f.p.Text, nsFiles.T("Name your section")) }),
			layout.Rigid(func(gtx C) D { return D{Size: image.Pt(0, gtx.Dp(8))} }),
			layout.Rigid(func(gtx C) D { return f.sectionNameInput(gtx, st) }),
			layout.Rigid(func(gtx C) D { return D{Size: image.Pt(0, gtx.Dp(20))} }),
		)
	})
}

// sectionNameInput is .file-section-input-wrapper: the 36px input
// (padding 0 44px), the 44px emoji picker at its left (a 1px
// sk_foreground_high_solid right border; the emoji, else "emoji"), and
// "Clear all" at top 6, right 8.
func (f *Files) sectionNameInput(gtx C, st *sectionModalState) D {
	w, h := gtx.Constraints.Max.X, gtx.Dp(36)
	r := image.Rect(0, 0, w, h)
	rad := gtx.Dp(4)
	rrect(gtx, r, rad, f.p.Bg)
	if gtx.Focused(&st.ed) {
		border(gtx, r.Inset(-gtx.Dp(2)), rad+gtx.Dp(2), gtx.Dp(3), f.p.Focus)
		border(gtx, r, rad, max(1, gtx.Dp(1)), f.p.Link)
	} else {
		border(gtx, r, rad, max(1, gtx.Dp(1)), f.p.BorderStr)
	}
	pw := gtx.Dp(44)
	func() {
		g := gtx
		g.Constraints = layout.Exact(image.Pt(pw, h))
		st.emojiBtn.Layout(g, func(gtx C) D {
			pointerCursor(gtx)
			fill(gtx, image.Rect(pw-max(1, gtx.Dp(1)), max(1, gtx.Dp(1)), pw, h-max(1, gtx.Dp(1))), f.lt(rgb(0x868686), rgb(0x696a6d)))
			return layout.Center.Layout(gtx, func(gtx C) D {
				if st.emoji != "" {
					if u, _, ok := f.h.Core().Emoji(strings.Trim(st.emoji, ":")); ok && u != "" {
						return f.emojiImg(gtx, u, 20)
					}
				}
				if st.emojiBtn.Hovered() {
					return f.icon(gtx, "emoji-filled", 20, f.p.Text)
				}
				return f.icon(gtx, "emoji", 20, f.p.Sec)
			})
		})
	}()
	if st.ed.Len() > 0 || st.emoji != "" {
		func() {
			cs := gtx.Dp(20)
			defer op.Offset(image.Pt(w-gtx.Dp(8)-cs, (h-cs)/2)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Exact(image.Pt(cs, cs))
			st.clearBtn.Layout(g, func(gtx C) D {
				pointerCursor(gtx)
				return layout.Center.Layout(gtx, func(gtx C) D { return f.icon(gtx, "close-filled", 16, f.lt(rgb(0x616061), rgb(0xababad))) })
			})
		}()
	}
	func() {
		defer op.Offset(image.Pt(pw+gtx.Dp(8), 0)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(max(0, w-2*pw-gtx.Dp(8)), h))
		layout.W.Layout(g, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Stack{Alignment: layout.W}.Layout(gtx,
				layout.Stacked(func(gtx C) D {
					if st.ed.Len() > 0 {
						return D{}
					}
					return f.label(gtx, 15, wReg, f.p.Faint, nsFiles.T("Enter a section name"))
				}),
				layout.Stacked(func(gtx C) D { return f.th.Editor(gtx, &st.ed, 15, f.p.Text, f.p.Mention) }),
			)
		})
	}()
	return D{Size: image.Pt(w, h)}
}

// ---- the results' section button ----

// sectionButtonShown is FileActions' renderFavoriteCanvas / List /
// Surface / 3pFile under custom_file_sections: FileSectionDropdownButton
// on canvases and lists, and on every other file with
// unified_files_3p_primary_custom_sections (isUnifiedFiles).
func (f *Files) sectionButtonShown(it *fileItem) bool {
	a := f.h.Core()
	if !a.FileSectionsOn() {
		return false
	}
	return it.isCanvas() || it.isList() || a.Experiment("unified_files_3p_primary_custom_sections") == "on"
}

// sectionButton is FileSectionDropdownButton ("Move file"): in the starred
// collection the 20px star-filled on the view header's filled star
// button; in a section with an emoji that emoji; else "section" (in a
// section) or "star" (in none). A click opens FileSectionMoveToMenu at
// bottom-right.
func (f *Files) sectionButton(gtx C, c *fileClicks, it *fileItem) D {
	a := f.h.Core()
	coll, in := a.FileCollectionOf(it.ID)
	var d D
	switch {
	case in && coll.Starred():
		d = f.starButton(gtx, &c.star, true)
	case in && coll.Emoji != "":
		d = c.star.Layout(gtx, func(gtx C) D {
			pointerCursor(gtx)
			sz := gtx.Dp(28)
			if c.star.Hovered() {
				rrect(gtx, image.Rect(0, 0, sz, sz), gtx.Dp(8), f.p.Hover)
			}
			g := gtx
			g.Constraints = layout.Exact(image.Pt(sz, sz))
			layout.Center.Layout(g, func(gtx C) D { return f.collectionIcon(gtx, coll, false, 16, f.p.Meta) })
			return D{Size: image.Pt(sz, sz)}
		})
	case in:
		d = f.iconButton(gtx, &c.star, "section", 28, 18, f.p.Meta)
	default:
		d = f.iconButton(gtx, &c.star, "star", 28, 18, f.p.Meta)
	}
	c.starSize = d.Size
	popups.Tooltip(gtx, f.th, &c.starTip, c.star.Hovered(), d.Size, popups.BelowRight, nsFiles.T("Move file"), "")
	return d
}

// openMoveMenu is the section button's FileSectionMoveToMenu (320 wide).
func (f *Files) openMoveMenu(it fileItem, btn image.Rectangle) {
	ph, ok := f.h.(popups.Host)
	if !ok {
		return
	}
	items := f.moveToItems(it.File, true, "file_pillow")
	if len(items) == 0 {
		return
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: btn, Width: 320, Popover: "bottom-right"}))
}
