package views

// The channel's Files & links tab: Slack's legacy files_tab_view
// (boot-shared 3ztk), which the account gets with resources_channel_tab
// off, around the Explorer (yKEr) with useChannelAttachmentsExplorer
// (JG8Q). Build 0e695dd18302 / boot-shared 0fb3750ed9646c84 and
// client-boot-styles; docs/FILES-AUDIT.md "Files & links tab".
//
//   - .files_tab_view: base-sec, 20px down; the search (ExplorerSearch,
//     placeholder "Search") 16px over the results, both at most 968 wide
//     in 16px gutters (p-explorer_search__content, p-explorer_results__
//     contained-result).
//   - "controls": the Attachment type radios (All / Files / Media / Links;
//     "From huddles" needs files_and_artifacts_from_meetings, off for the
//     account) on the left, the Sort select on the right (Newest, Oldest,
//     and Most relevant first with a search outside All).
//   - All: without a search, "Photos and videos" with See all and one row
//     of the media grid (ExpandableGrid collapsed, 140px cells 16 apart),
//     16px; then the files and links in one list by share time
//     (getChannelAttachmentsMixedRows; with a search the media join the
//     list), then "More results by type" cards for the browsers with more
//     pages. Media, Files and Links: that browser alone, with Load More.
//   - File rows are ContextualFile (DraggableExplorerFile, .listFile:
//     full width): the pillow's icon, title and "Shared by {name} on
//     {date}" (FileEntityUserTimestamp with displaySharedBy), the action
//     text on hover, FileActions' Download and More actions. Links are
//     LinkEntity: the favicon (else link 20), title or host, the host (or
//     the path) under it and "Open in browser" on hover, and More actions
//     (Copy link). Clicks: handleFileContainerClick with source
//     CHANNEL_TAB (canvases and lists in the side panel, images in the
//     viewer), links in the browser.
//   - Searching is the explorer's onSearch (Enter) and onResetSearch (the
//     text cleared): all three browsers get the query; outside All the
//     sort becomes Most relevant.
//
// Keyboard: the rows are one virtual list (vlist_keys.go): ↑/↓, Home/End,
// PageUp/PageDown and Enter, the media cells as rows of their own.

import (
	"context"
	"image"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var (
	nsTabbed      = i18n.NS("tabbed-channel") // useChannelAttachmentsExplorer, files_tab_view
	nsExplorerLnk = i18n.NS("explorer_link")  // LinkEntity
)

// The Attachment type values (JG8Q: all, files, media, links).
const (
	cfAll   = "all"
	cfFiles = "files"
	cfMedia = "media"
	cfLinks = "links"
)

var cfTypes = [...]struct{ value, label string }{
	{cfAll, "All"}, {cfFiles, "Files"}, {cfMedia, "Media"}, {cfLinks, "Links"},
}

// cfFileBrowser is one of the tab's file browsers (useBrowser with
// isInfinitePagination: every loaded page in order).
type cfFileBrowser struct {
	data  async[core.FilePage]
	items []fileItem
	page  int // the last page loaded (0: none yet)
	pages int
	sort  core.ChannelFilesSort
}

func (b *cfFileBrowser) loading() bool { _, ok, _ := b.data.get(); return !ok || b.data.busy() }

// hasMore is the explorer's eP / eD: more than one page and not on the
// last.
func (b *cfFileBrowser) hasMore() bool { return b.pages > 1 && b.page < b.pages }

type cfLinkBrowser struct {
	data  async[core.ChannelLinkPage]
	links []core.ChannelLink
	page  int
	pages int
	sort  core.ChannelFilesSort
}

func (b *cfLinkBrowser) loading() bool { _, ok, _ := b.data.get(); return !ok || b.data.busy() }
func (b *cfLinkBrowser) hasMore() bool { return b.pages > 1 && b.page < b.pages }

// cfRowClicks are a row's widgets.
type cfRowClicks struct {
	row, download, menu, info widget.Clickable
	menuSize                  image.Point
	downloadTip, menuTip      popups.TipState
}

// ChannelFiles is a channel's Files & links tab.
type ChannelFiles struct {
	base
	fv *Files // the Files view's row helpers: file tiles, the ⋯ menu, delete, rename
	ch string

	search *search
	query  string
	typ    string

	typeClicks [len(cfTypes)]widget.Clickable
	sortBtn    widget.Clickable
	sortScrim  widget.Clickable
	sortOpen   bool
	sortClicks [3]widget.Clickable
	seeAll     widget.Clickable
	moreType   [3]widget.Clickable // Files, Media, Links cards
	loadMore   widget.Clickable

	media, files cfFileBrowser
	links        cfLinkBrowser
	started      bool

	// preview is the tab's peek (files_tab_view isPreview: the channel-tab
	// slug's FilesTabPeek, channel_files_peek.go)
	preview bool

	list  widget.List
	kb    *vkList
	rows  map[string]*cfRowClicks
	cells []cfCell // last frame's media cells (keyboard)
	gridW int
	seen  time.Time
}

// cfCell is a media cell's place in the grid element.
type cfCell struct {
	key      string
	top, h   int
	elem     int
	fileItem *fileItem
}

// NewChannelFiles builds ch's Files & links tab.
func NewChannelFiles(h Host, ch string) *ChannelFiles {
	v := &ChannelFiles{base: newBase(h), fv: NewFiles(h), ch: ch, typ: cfAll, rows: map[string]*cfRowClicks{},
		search: newSearchIn(nsTabbed, "Search")}
	v.media.sort, v.files.sort, v.links.sort = core.ChannelFilesNewest, core.ChannelFilesNewest, core.ChannelFilesNewest
	v.kb = newVKList(&v.list)
	return v
}

// Channel is the tab's channel.
func (v *ChannelFiles) Channel() string { return v.ch }

// Refresh refetches the three browsers from page 1 (Slack's browsers
// refetch when the tab mounts again).
func (v *ChannelFiles) Refresh() {
	v.loadFiles(&v.media, true, 1)
	v.loadFiles(&v.files, false, 1)
	v.loadLinks(1)
}

func (v *ChannelFiles) loadFiles(b *cfFileBrowser, media bool, page int) {
	a := v.h.Core()
	if a == nil {
		return
	}
	o := core.ChannelFilesOpts{Channel: v.ch, Media: media, Query: v.query, Sort: b.sort, Page: page}
	if page == 1 {
		b.page, b.pages = 0, 0
	}
	b.data.load(v.h, page > 1, func(ctx context.Context) (core.FilePage, error) { return a.ChannelFiles(ctx, o) })
}

func (v *ChannelFiles) loadLinks(page int) {
	a := v.h.Core()
	if a == nil {
		return
	}
	ch, q, s := v.ch, v.query, v.links.sort
	if page == 1 {
		v.links.page, v.links.pages = 0, 0
	}
	v.links.data.load(v.h, page > 1, func(ctx context.Context) (core.ChannelLinkPage, error) {
		return a.ChannelLinks(ctx, ch, q, s, page)
	})
}

// apply folds an arrived page into its browser (page 1 replaces).
func (b *cfFileBrowser) apply(fv *Files) {
	p, ok, err := b.data.get()
	if !ok || err != nil || b.data.busy() || p.Page == 0 || p.Page == b.page {
		return
	}
	items := fv.cleanTitles(p.Files)
	if p.Page <= 1 {
		b.items = items
	} else {
		seen := map[string]bool{}
		for _, it := range b.items {
			seen[it.ID] = true
		}
		for _, it := range items {
			if !seen[it.ID] {
				b.items = append(b.items, it)
			}
		}
	}
	b.page, b.pages = p.Page, p.Pages
}

func (b *cfLinkBrowser) apply() {
	p, ok, err := b.data.get()
	if !ok || err != nil || b.data.busy() || p.Page == 0 || p.Page == b.page {
		return
	}
	if p.Page <= 1 {
		b.links = nil
	}
	seen := map[string]bool{}
	for _, l := range b.links {
		seen[l.URL] = true
	}
	for _, l := range p.Links {
		if !seen[l.URL] {
			b.links = append(b.links, l)
		}
	}
	b.page, b.pages = p.Page, p.Pages
}

// sortValue is the Sort select's value: the files browser's (eu).
func (v *ChannelFiles) sortValue() core.ChannelFilesSort { return v.files.sort }

// sortOptions are eW: Newest and Oldest, Most relevant first with a
// search outside All.
func (v *ChannelFiles) sortOptions() []struct {
	label string
	value core.ChannelFilesSort
} {
	opts := []struct {
		label string
		value core.ChannelFilesSort
	}{{nsSearch.T("Newest"), core.ChannelFilesNewest}, {nsSearch.T("Oldest"), core.ChannelFilesOldest}}
	if v.query != "" && v.typ != cfAll {
		opts = append([]struct {
			label string
			value core.ChannelFilesSort
		}{{nsBrowsers.T("Most relevant"), core.ChannelFilesRelevant}}, opts...)
	}
	return opts
}

func (v *ChannelFiles) sortLabel() string {
	for _, o := range v.sortOptions() {
		if o.value == v.sortValue() {
			return o.label
		}
	}
	return nsSearch.T("Newest")
}

// setType is eL: the type changes, and going back to All with Most
// relevant puts the files browser on Newest.
func (v *ChannelFiles) setType(t string) {
	if t == v.typ {
		return
	}
	v.typ = t
	if t == cfAll && v.files.sort == core.ChannelFilesRelevant {
		v.files.sort = core.ChannelFilesNewest
		v.loadFiles(&v.files, false, 1)
	}
	v.list.Position = layout.Position{} // scrollToTop
}

// setSort is eR: every browser takes the sort.
func (v *ChannelFiles) setSort(s core.ChannelFilesSort) {
	if s == v.sortValue() {
		return
	}
	v.media.sort, v.files.sort, v.links.sort = s, s, s
	v.Refresh()
}

// runSearch is onSearch's type-query completion: the query goes to every
// browser; in All the sort stays, elsewhere it becomes Most relevant.
func (v *ChannelFiles) runSearch(q string) {
	v.query = q
	if v.typ != cfAll {
		v.media.sort, v.files.sort, v.links.sort = core.ChannelFilesRelevant, core.ChannelFilesRelevant, core.ChannelFilesRelevant
	} else {
		v.media.sort, v.links.sort = v.files.sort, v.files.sort
	}
	v.list.Position = layout.Position{}
	v.Refresh()
}

// resetSearch is onResetSearch: the browsers go back to their defaults.
func (v *ChannelFiles) resetSearch() {
	v.query = ""
	v.media.sort, v.files.sort, v.links.sort = core.ChannelFilesNewest, core.ChannelFilesNewest, core.ChannelFilesNewest
	v.Refresh()
}

func (v *ChannelFiles) rowOf(key string) *cfRowClicks {
	c := v.rows[key]
	if c == nil {
		c = new(cfRowClicks)
		v.rows[key] = c
	}
	return c
}

func (v *ChannelFiles) update(gtx C) {
	v.fv.drainDone()
	if !v.started || !v.seen.IsZero() && gtx.Now.Sub(v.seen) > 2*time.Second {
		v.started = true
		v.Refresh() // opened (again): the browsers refetch
	}
	v.seen = gtx.Now
	v.media.apply(v.fv)
	v.files.apply(v.fv)
	v.links.apply()
	for i := range v.typeClicks {
		if v.typeClicks[i].Clicked(gtx) {
			v.setType(cfTypes[i].value)
		}
	}
	if v.sortBtn.Clicked(gtx) {
		v.sortOpen = !v.sortOpen
	}
	opts := v.sortOptions()
	for i := range v.sortClicks {
		if v.sortClicks[i].Clicked(gtx) {
			v.sortOpen = false
			if i < len(opts) {
				v.setSort(opts[i].value)
			}
		}
	}
	if v.seeAll.Clicked(gtx) {
		v.setType(cfMedia)
	}
	for i, t := range []string{cfFiles, cfMedia, cfLinks} {
		if v.moreType[i].Clicked(gtx) {
			v.setType(t)
		}
	}
	if v.loadMore.Clicked(gtx) {
		v.more()
	}
	if t, changed, submit := v.search.update(gtx); submit && t != "" {
		v.runSearch(t)
	} else if changed && t == "" && v.query != "" {
		v.resetSearch()
	}
	// the rows' buttons sit inside the row, and Gio hands their clicks to
	// the row too: the row acts only when none of them took it (Slack's
	// buttons stop propagation)
	for _, it := range v.allFiles() {
		c := v.rows[it.ID]
		if c == nil {
			continue
		}
		dl, menu := c.download.Clicked(gtx), c.menu.Clicked(gtx)
		if c.row.Clicked(gtx) && !dl && !menu {
			v.open(*it)
		}
		if ph, ok := v.h.(popups.Host); ok && dl {
			popups.DownloadFile(ph, cfRich(it))
		}
		if menu {
			v.openFileMenu(*it, v.fv.buttonRect(&c.menu, c.menuSize))
		}
	}
	for i := range v.links.links {
		l := v.links.links[i]
		c := v.rows["link:"+l.URL]
		if c == nil {
			continue
		}
		menu := c.menu.Clicked(gtx)
		if c.row.Clicked(gtx) && !menu {
			v.openLink(l.URL)
		}
		if menu {
			v.openLinkMenu(l, v.fv.buttonRect(&c.menu, c.menuSize))
		}
	}
}

// more is the infinite-pagination row's Load More for the view's browser.
func (v *ChannelFiles) more() {
	switch v.typ {
	case cfMedia:
		if v.media.hasMore() && !v.media.data.busy() {
			v.loadFiles(&v.media, true, v.media.page+1)
		}
	case cfFiles:
		if v.files.hasMore() && !v.files.data.busy() {
			v.loadFiles(&v.files, false, v.files.page+1)
		}
	case cfLinks:
		if v.links.hasMore() && !v.links.data.busy() {
			v.loadLinks(v.links.page + 1)
		}
	}
}

func (v *ChannelFiles) allFiles() []*fileItem {
	out := make([]*fileItem, 0, len(v.media.items)+len(v.files.items))
	for i := range v.media.items {
		out = append(out, &v.media.items[i])
	}
	for i := range v.files.items {
		out = append(out, &v.files.items[i])
	}
	return out
}

// open is handleFileContainerClick with source CHANNEL_TAB:
// getMainFileAction's open for canvases and lists (openCanvas / openList
// into the side panel), view for images (the media viewer), and the
// file's page for the rest (Clack has no PDF, video or details view; see
// openFile).
func (v *ChannelFiles) open(it fileItem) {
	switch {
	case it.isCanvas() && it.ID != "":
		if co, ok := v.h.(interface{ OpenCanvas(id string) }); ok {
			co.OpenCanvas(it.ID)
			return
		}
	case it.isList() && it.ID != "":
		if lo, ok := v.h.(interface{ OpenList(id string) }); ok {
			lo.OpenList(it.ID)
			return
		}
	}
	openFile(v.h, it)
}

// openLink is LinkEntity's onListItemSelect: window.open(url, "_blank").
func (v *ChannelFiles) openLink(u string) {
	if o, ok := v.h.(interface{ OpenURL(string) }); ok {
		o.OpenURL(u)
	}
}

// openFileMenu is the row's More actions: the Files view's menu (the items
// Clack has of getFileActionsMenuItems), at FileActions' Popover.
func (v *ChannelFiles) openFileMenu(it fileItem, btn image.Rectangle) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	items := v.fv.fileMenuItems(it)
	if len(items) == 0 {
		return
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: btn, Width: 240,
		Popover: "right-bottom", OffsetX: 4, TryOpposite: true}))
}

// openLinkMenu is LinkEntity's More actions: Copy link ("Link copied to
// clipboard", 3s). Add to folder (bookmark folders) isn't in Clack.
func (v *ChannelFiles) openLinkMenu(l core.ChannelLink, btn image.Rectangle) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	link := l.URL
	items := []popups.MenuItem{{Label: nsExplorerLnk.T("Copy link"), DoCtx: func(gtx layout.Context) {
		gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(link))})
		v.h.Toast(nsExplorerLnk.T("Link copied to clipboard"))
	}}}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: btn, Width: 240,
		Popover: "right-bottom", OffsetX: 4, TryOpposite: true}))
}

// cfRich is a row's file as a message attachment (DownloadFile's).
func cfRich(it *fileItem) *richtext.File {
	return &richtext.File{ID: it.ID, Name: it.Name, Title: it.Title, Type: it.Filetype, PrettyType: it.PrettyType,
		Mime: it.Mime, User: it.User, URL: it.URL, Permalink: it.Permalink, Size: it.Size, Created: it.Created,
		IsImage: it.isImage()}
}

// ---- rows ----

// cfRow is one row of the explorer list.
type cfRow struct {
	key   string
	kind  int // cfr*
	file  *fileItem
	link  *core.ChannelLink
	first bool
	last  bool
	title string
	empty string
	cards []int // more-results-by-type: moreType indexes
}

const (
	cfrControls = iota
	cfrLoading
	cfrSection // "Photos and videos" with See all
	cfrStrip   // the media grid, one row (All)
	cfrGrid    // the media grid, every row (Media)
	cfrSpacer
	cfrFile
	cfrLink
	cfrMoreTypes
	cfrPagination
	cfrEmpty
)

// cfMixed is one of getChannelAttachmentsMixedRows' entries.
type cfMixed struct {
	file *fileItem
	link *core.ChannelLink
	ts   float64
}

func fileShareTS(it *fileItem) float64 {
	if it.Timestamp != 0 {
		return float64(it.Timestamp)
	}
	return float64(it.Created)
}

// rowsFor builds the explorer's rows (useChannelAttachmentsExplorer, not a
// preview).
func (v *ChannelFiles) rowsFor() []cfRow {
	if v.preview {
		return v.previewRows()
	}
	rows := []cfRow{{key: "controls", kind: cfrControls}}
	loadingRow := cfRow{key: "list-loading", kind: cfrLoading}
	empty := func(s string) cfRow { return cfRow{key: "list-empty", kind: cfrEmpty, empty: s} }
	switch v.typ {
	case cfAll:
		media := v.media.items
		if v.query == "" && len(media) > 0 {
			rows = append(rows, cfRow{key: "media-title", kind: cfrSection, title: i18n.T("files", "Photos and videos")},
				cfRow{key: "list-media", kind: cfrStrip}, cfRow{key: "media-gallery-spacer", kind: cfrSpacer})
		}
		exclude := v.query == ""
		none := (exclude || len(media) == 0) && len(v.files.items) == 0 && len(v.links.links) == 0
		if ((!exclude && v.media.loading()) || v.files.loading() || v.links.loading()) && none {
			return append(rows, loadingRow)
		}
		var mixed []cfMixed
		if !exclude {
			for i := range media {
				mixed = append(mixed, cfMixed{file: &media[i], ts: fileShareTS(&media[i])})
			}
		}
		for i := range v.files.items {
			mixed = append(mixed, cfMixed{file: &v.files.items[i], ts: fileShareTS(&v.files.items[i])})
		}
		for i := range v.links.links {
			mixed = append(mixed, cfMixed{link: &v.links.links[i], ts: v.links.links[i].Timestamp})
		}
		oldest := v.sortValue() == core.ChannelFilesOldest
		sort.SliceStable(mixed, func(i, j int) bool {
			if oldest {
				return mixed[i].ts < mixed[j].ts
			}
			return mixed[i].ts > mixed[j].ts
		})
		if len(mixed) == 0 && (!exclude || len(media) == 0) {
			if v.query != "" {
				return append(rows, empty(nsTabbed.T(`No results for "{query}" in this channel.`, "query", v.query)))
			}
			return append(rows, empty(nsTabbed.T("No files or links found for this channel.")))
		}
		for i, m := range mixed {
			r := cfRow{first: i == 0, last: i == len(mixed)-1}
			if m.file != nil {
				r.key, r.kind, r.file = m.file.ID, cfrFile, m.file
			} else {
				r.key, r.kind, r.link = "link:"+m.link.URL, cfrLink, m.link
			}
			rows = append(rows, r)
		}
		var cards []int
		if v.files.hasMore() {
			cards = append(cards, 0)
		}
		if v.media.hasMore() {
			cards = append(cards, 1)
		}
		if v.links.hasMore() {
			cards = append(cards, 2)
		}
		if len(cards) > 0 {
			rows = append(rows, cfRow{key: "more-results-by-type", kind: cfrMoreTypes, cards: cards})
		}
	case cfMedia:
		switch {
		case v.media.loading() && len(v.media.items) == 0:
			rows = append(rows, cfRow{key: "list-loading", kind: cfrLoading})
		case len(v.media.items) > 0:
			rows = append(rows, cfRow{key: "list-media", kind: cfrGrid})
			if v.media.hasMore() {
				rows = append(rows, cfRow{key: "pagination", kind: cfrPagination})
			}
		case v.query != "":
			rows = append(rows, empty(i18n.T("files", `No media results for "{query}" in this channel.`, "query", v.query)))
		default:
			rows = append(rows, empty(i18n.T("files", "No media found for this channel.")))
		}
	case cfFiles:
		switch {
		case v.files.loading() && len(v.files.items) == 0:
			rows = append(rows, cfRow{key: "list-loading", kind: cfrLoading})
		case len(v.files.items) > 0:
			for i := range v.files.items {
				rows = append(rows, cfRow{key: v.files.items[i].ID, kind: cfrFile, file: &v.files.items[i],
					first: i == 0, last: i == len(v.files.items)-1})
			}
			if v.files.hasMore() {
				rows = append(rows, cfRow{key: "pagination", kind: cfrPagination})
			}
		case v.query != "":
			rows = append(rows, empty(i18n.T("files", `No file results for "{query}" in this channel.`, "query", v.query)))
		default:
			rows = append(rows, empty(i18n.T("files", "No files found for this channel.")))
		}
	case cfLinks:
		switch {
		case v.links.loading() && len(v.links.links) == 0:
			rows = append(rows, cfRow{key: "list-loading", kind: cfrLoading})
		case len(v.links.links) > 0:
			for i := range v.links.links {
				rows = append(rows, cfRow{key: "link:" + v.links.links[i].URL, kind: cfrLink, link: &v.links.links[i],
					first: i == 0, last: i == len(v.links.links)-1})
			}
			if v.links.hasMore() {
				rows = append(rows, cfRow{key: "pagination", kind: cfrPagination})
			}
		case v.query != "":
			rows = append(rows, empty(i18n.T("files", `No link results for "{query}" in this channel.`, "query", v.query)))
		default:
			rows = append(rows, empty(i18n.T("files", "No links found for this channel.")))
		}
	}
	return rows
}

// ---- layout ----

// Layout draws the tab.
func (v *ChannelFiles) Layout(gtx C) D {
	v.frame()
	v.fv.frame()
	v.update(gtx)
	sz := gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: sz}, v.p.Page) // .files_tab_view: --dt_color-base-sec
	gtx.Constraints = layout.Exact(sz)
	if v.preview { // no inputId: no ExplorerSearch
		return layout.Inset{Top: 20}.Layout(gtx, v.results)
	}
	return layout.Inset{Top: 20}.Layout(gtx, func(gtx C) D {
		return col(gtx,
			layout.Rigid(func(gtx C) D {
				// .p-explorer_search__container: 16 under; __content: at
				// most 1000 wide with 16px padding
				return layout.Inset{Bottom: 16}.Layout(gtx, func(gtx C) D {
					return v.contained(gtx, func(gtx C) D { return v.searchBox(gtx, v.search, 36) })
				})
			}),
			layout.Flexed(1, v.results),
		)
	})
}

// contained lays w out at p-explorer_results__contained-result's width:
// 100% − 32px, at most 968, centred.
func (v *ChannelFiles) contained(gtx C, w W) D {
	full := gtx.Constraints.Max.X
	cw := min(gtx.Dp(968), max(0, full-gtx.Dp(32)))
	if v.preview { // .files_tab_view__preview .p-explorer_results__contained-result: 100%
		cw = full
	}
	g := gtx
	g.Constraints.Min.X, g.Constraints.Max.X = cw, cw
	m := op.Record(gtx.Ops)
	d := w(g)
	call := m.Stop()
	func() {
		defer op.Offset(image.Pt((full-cw)/2, 0)).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}()
	return D{Size: image.Pt(full, d.Size.Y), Baseline: d.Baseline}
}

func (v *ChannelFiles) results(gtx C) D {
	rows := v.rowsFor()
	items := make([]vkItem, 0, len(rows))
	for i, r := range rows {
		switch r.kind {
		case cfrFile, cfrLink:
			items = append(items, vkItem{key: r.key, elem: i, focusable: true})
		case cfrStrip, cfrGrid:
			for _, c := range v.cells {
				if c.elem == i {
					items = append(items, vkItem{key: c.key, elem: i, focusable: true})
				}
			}
		}
	}
	v.kb.setItems(items)
	v.kb.update(gtx, gtx.Constraints.Max.Y, vkOpts{Click: v.kbClick})
	v.kb.begin()
	v.cells = v.cells[:0]
	d := v.scrollList(gtx, &v.list, len(rows), func(gtx C, i int) D {
		r := rows[i]
		return v.kb.elem(i, v.contained(gtx, func(gtx C) D { return v.row(gtx, i, &r) }))
	})
	v.kb.area(gtx, image.Rectangle{Max: d.Size})
	v.kb.after()
	return d
}

// kbClick is Enter on the active row: the row's click.
func (v *ChannelFiles) kbClick(k string) {
	if strings.HasPrefix(k, "link:") {
		v.openLink(strings.TrimPrefix(k, "link:"))
		return
	}
	for _, it := range v.allFiles() {
		if it.ID == k || "cell:"+it.ID == k {
			if strings.HasPrefix(k, "cell:") {
				v.openMedia(*it)
			} else {
				v.open(*it)
			}
			return
		}
	}
}

func (v *ChannelFiles) row(gtx C, i int, r *cfRow) D {
	switch r.kind {
	case cfrControls:
		return v.controls(gtx)
	case cfrLoading:
		return v.loadingRows(gtx)
	case cfrSection:
		return v.section(gtx, r.title)
	case cfrStrip:
		return v.grid(gtx, i, false)
	case cfrGrid:
		return v.grid(gtx, i, true)
	case cfrSpacer:
		return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(16))}
	case cfrFile:
		return v.fileRow(gtx, r.file, r.first, r.last)
	case cfrLink:
		return v.linkRow(gtx, r.link, r.first, r.last)
	case cfrMoreTypes:
		return v.moreTypes(gtx, r.cards)
	case cfrPagination:
		return v.pagination(gtx)
	case cfrEmpty:
		return v.emptyRow(gtx, r.empty)
	}
	return D{}
}

// controls is ExplorerControls: .p-explorer_controls (5px down, 8 under):
// the expanded radio group on the left (p-explorer_controls__button, 8px
// apart, the picked one in content-hgl-1), the Sort select on the right
// (active off Newest).
func (v *ChannelFiles) controls(gtx C) D {
	return layout.Inset{Top: 5, Bottom: 8}.Layout(gtx, func(gtx C) D {
		ch := make([]layout.FlexChild, 0, len(cfTypes)*2+2)
		for i := range cfTypes {
			i := i
			if i > 0 {
				ch = append(ch, hgap(8))
			}
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return v.fv.segButton(gtx, &v.typeClicks[i], v.typ == cfTypes[i].value, "", nsTabbed.T(cfTypes[i].label), false)
			}))
		}
		ch = append(ch, layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }))
		ch = append(ch, layout.Rigid(func(gtx C) D {
			d := v.fv.segButton(gtx, &v.sortBtn, v.sortValue() != core.ChannelFilesNewest, "", v.sortLabel(), true)
			// the select's menu under it, right-aligned
			const mw = 220
			v.dropdown(gtx, &v.sortOpen, &v.sortScrim, image.Pt(d.Size.X-gtx.Dp(mw), d.Size.Y+gtx.Dp(4)), mw, v.sortMenu)
			return d
		}))
		return row(gtx, layout.Middle, ch...)
	})
}

func (v *ChannelFiles) sortMenu(gtx C) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	opts := v.sortOptions()
	return actStack(gtx, len(opts), func(gtx C, i int) D {
		return v.menuItem(gtx, &v.sortClicks[i], "", opts[i].label, true, v.sortValue() == opts[i].value)
	})
}

// loadingRows is the tab's listLoadingFragment: seven explorer result
// skeletons (first, five, last).
func (v *ChannelFiles) loadingRows(gtx C) D {
	return v.skeleton(gtx, 7, 36)
}

// section is the "section-label" row: .p-explorer_section (13px Bold at
// .7, 16 above, 8 under) with See all (c-link--button) at the right.
func (v *ChannelFiles) section(gtx C, title string) D {
	return layout.Inset{Top: 16, Bottom: 8}.Layout(gtx, func(gtx C) D {
		muted := theme.WithAlpha(v.p.Text, 0xb3)
		return row(gtx, layout.Middle,
			layout.Flexed(1, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.W.Layout(gtx, func(gtx C) D { return v.label(gtx, 13, wBold, muted, title) })
			}),
			layout.Rigid(func(gtx C) D {
				return v.seeAll.Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					d := v.label(gtx, 13, wBold, v.p.Link, nsTabbed.T("See all"))
					if v.seeAll.Hovered() {
						fill(gtx, image.Rect(0, d.Size.Y-gtx.Dp(2), d.Size.X, d.Size.Y-gtx.Dp(1)), v.p.Link)
					}
					return d
				})
			}),
		)
	})
}

// gridCols is useGridDimensions: floor(width / (140 + 16)) columns, each
// (width − gaps) / columns wide.
func gridCols(gtx C, w int) (cols, cw, gap int) {
	gap = gtx.Dp(16)
	cols = max(1, w/(gtx.Dp(140)+gap))
	cw = (w - gap*(cols-1)) / cols
	return cols, cw, gap
}

// grid is the media ExpandableGrid: cells 140 tall (.files_tab_view_content
// .p-file_card__container), 16 apart; collapsed it shows one row.
func (v *ChannelFiles) grid(gtx C, elem int, expanded bool) D {
	w := gtx.Constraints.Max.X
	cols, cw, gap := gridCols(gtx, w)
	ch := gtx.Dp(140)
	items := v.media.items
	n := len(items)
	if !expanded {
		n = min(n, cols)
	}
	nrows := (n + cols - 1) / cols
	for i := 0; i < n; i++ {
		it := &items[i]
		x, y := (i%cols)*(cw+gap), (i/cols)*(ch+gap)
		key := "cell:" + it.ID
		v.cells = append(v.cells, cfCell{key: key, top: y, h: ch, elem: elem, fileItem: it})
		v.kb.sub(key, y, ch)
		c := v.rowOf(key)
		if c.row.Clicked(gtx) {
			v.openMedia(*it)
		}
		dirAt(gtx, image.Pt(x, y), func(gtx C) D {
			return v.mediaCell(gtx, c, it, image.Pt(cw, ch), v.kb.ringOn(key))
		})
	}
	h := 0
	if nrows > 0 {
		h = nrows*ch + (nrows-1)*gap
	}
	return D{Size: image.Pt(w, h)}
}

// openMedia is the media cell's handleFileContainerClick: images in the
// viewer, videos on their page (Clack has no video player).
func (v *ChannelFiles) openMedia(it fileItem) { v.open(it) }

// mediaCell is ExplorerGridMedia: the file card's preview (cover, radius
// 12 at the cell's focus ring, base-ter under it), a play badge on
// videos.
func (v *ChannelFiles) mediaCell(gtx C, c *cfRowClicks, it *fileItem, sz image.Point, ring bool) D {
	return c.row.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		rad := gtx.Dp(12)
		r := image.Rectangle{Max: sz}
		rrect(gtx, r, rad, v.p.Track)
		if it.Thumb != "" && v.h.Images() != nil {
			if img, ok := v.h.Images().Get(it.Thumb, max(sz.X, sz.Y)); ok {
				themeImage(gtx, img, sz, rad)
			}
		}
		if strings.HasPrefix(it.Mime, "video/") {
			// the video file's play pill (VideoMessageFile at rest)
			ps := gtx.Dp(28)
			p := image.Pt(gtx.Dp(8), sz.Y-gtx.Dp(8)-ps)
			rrect(gtx, image.Rectangle{Min: p, Max: p.Add(image.Pt(gtx.Dp(40), ps))}, gtx.Dp(14), argb(0xcc1d1c1d))
			dirAt(gtx, p.Add(image.Pt(gtx.Dp(12), (ps-gtx.Dp(16))/2)), func(gtx C) D { return v.icon(gtx, "play", 16, rgb(0xffffff)) })
		}
		if c.row.Hovered() {
			rrect(gtx, r, rad, argb(0x14000000))
		}
		if ring {
			v.vkRing(gtx, r, rad)
		}
		return D{Size: sz}
	})
}

// fileRow is a DraggableExplorerFile in the list: the c-pillow_file (12px
// padding, 1px otl-ter, rows sharing their lines, radius 12 at the ends),
// the 36px tile, the 15px Bold title and the 13px detail line.
func (v *ChannelFiles) fileRow(gtx C, it *fileItem, first, last bool) D {
	c := v.rowOf(it.ID)
	ring := v.kb.ringOn(it.ID)
	return c.row.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		hover := c.row.Hovered() || c.menu.Hovered() || c.download.Hovered()
		bw := max(1, gtx.Dp(1))
		w := gtx.Constraints.Max.X
		inner := gtx.Dp(36)
		h := 2*gtx.Dp(12) + inner + bw
		if last {
			h += bw
		}
		v.listFrame(gtx, image.Pt(w, h), first, last, hover)
		dirAt(gtx, image.Pt(gtx.Dp(12)+bw, bw+gtx.Dp(12)), func(gtx C) D { return v.fv.fileTile(gtx, it, 36) })
		tx := bw + gtx.Dp(12) + inner + gtx.Dp(12)
		tw := max(0, w-tx-gtx.Dp(12)-bw)
		detail := v.fileDetail(it)
		if hover {
			detail = cfActionText(it)
		}
		dirAt(gtx, image.Pt(tx, bw+gtx.Dp(12)-gtx.Dp(2)), func(gtx C) D {
			gtx.Constraints.Max.X = tw
			return col(gtx,
				layout.Rigid(func(gtx C) D {
					return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: wBold, Color: v.p.Text, MaxLines: 1}, it.title())
				}),
				layout.Rigid(func(gtx C) D {
					return v.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 16, Weight: wReg, Color: v.p.Muted, MaxLines: 1}, detail)
				}),
			)
		})
		if hover {
			v.fileActions(gtx, c, it, w-bw-gtx.Dp(12), h)
		}
		if ring {
			v.vkRing(gtx, image.Rect(0, 0, w, h), gtx.Dp(12))
		}
		if last || v.preview { // a preview row has its own bottom line
			return D{Size: image.Pt(w, h)}
		}
		return D{Size: image.Pt(w, h-bw)} // the next row's top line is this row's bottom
	})
}

// listFrame draws an explorer list row's box: the line colour with the
// surface inset by 1px, radius 12 on the first row's top and the last
// row's bottom; hover darkens the line (c-pillow_file:hover otl-sec).
func (v *ChannelFiles) listFrame(gtx C, sz image.Point, first, last, hover bool) {
	bw := max(1, gtx.Dp(1))
	r := image.Rectangle{Max: sz}
	rad := gtx.Dp(12)
	var tr, br int
	if first {
		tr = rad
	}
	if last {
		br = rad
	}
	line := theme.Over(v.p.CardLine, v.p.Surface)
	if v.preview {
		// .files_tab_view__preview .p-explorer_result--list .c-pillow_file:
		// no border but a 1px otl-ter bottom, radius 0; none on the last
		fill(gtx, r, v.p.Surface)
		if !last {
			fill(gtx, image.Rect(0, sz.Y-bw, sz.X, sz.Y), line)
		}
		return
	}
	in := image.Rect(bw, bw, sz.X-bw, sz.Y)
	if last {
		in.Max.Y -= bw
	}
	func() {
		defer clip.RRect{Rect: r, NW: tr, NE: tr, SW: br, SE: br}.Push(gtx.Ops).Pop()
		fill(gtx, r, line)
	}()
	func() {
		defer clip.RRect{Rect: in, NW: max(tr-bw, 0), NE: max(tr-bw, 0), SW: max(br-bw, 0), SE: max(br-bw, 0)}.Push(gtx.Ops).Pop()
		fill(gtx, in, v.p.Surface)
	}()
	_ = hover
}

// fileDetail is FileEntityUserTimestamp with displaySharedBy: "Shared by
// {name}" (MemberName isChannelFiles, no "(you)": showYou isn't passed)
// and "today" / "yesterday" or "on {date}" (toCalendarDateOrNameDay with
// a shortened month) for getFileTimestamp (updated, else created).
func (v *ChannelFiles) fileDetail(it *fileItem) string {
	by := ""
	if it.User != "" {
		by = i18n.T("member_profile", "Shared by {name}", "name", v.userName(it.User))
	}
	t := unixTime(fileUpdated(it))
	if t.IsZero() {
		return by
	}
	now := time.Now()
	day := cfCalendarDay(t, now)
	when := i18n.T("files", "on {date}", "date", day)
	if sameDay(t, now) || sameDay(t, now.AddDate(0, 0, -1)) {
		when = i18n.T("files", "{newDate}", "newDate", strings.ToLower(day))
	}
	if by == "" {
		return when
	}
	return by + " " + when
}

// cfCalendarDay is toCalendarDateOrNameDay(ts, {shortenMonth}): Today /
// Yesterday, else "Nov 9th" this year and "Nov 9th, 2025" before.
func cfCalendarDay(t, now time.Time) string {
	l := i18n.Locale()
	if sameDay(t, now) || sameDay(t, now.AddDate(0, 0, -1)) {
		return i18n.RelativeDay(l, t, now, true)
	}
	return i18n.CalendarDate(l, t, i18n.CalendarOpts{ShortenMonth: true, ExcludeYear: t.Year() == now.Year()})
}

// cfActionText is getActionText for the row's main action as Clack does
// it: Open canvas / Open list, View {type} in Slack for images, and Open
// in browser for what opens in the browser.
func cfActionText(it *fileItem) string {
	switch {
	case it.isCanvas():
		return nsFiles.T("Open {fileType}", "fileType", "canvas")
	case it.isList():
		return nsFiles.T("Open {fileType}", "fileType", "list")
	case it.isImage():
		return nsFiles.T("View {fileType} in Slack", "fileType", fileKindLabel(it))
	}
	return nsFiles.T("Open in browser")
}

// fileActions is FileActions' hover group (c-message_actions__group: a
// 4px-padded card, radius 12, 1px .13 ring) at the row's top right:
// Download (cloud-download) for hosted files, then More actions.
func (v *ChannelFiles) fileActions(gtx C, c *cfRowClicks, it *fileItem, right, rowH int) {
	bs := gtx.Dp(32)
	pad := gtx.Dp(4)
	n := 1
	dl := it.Mode == "hosted" && !it.isCanvas() && !it.isList() && it.URL != ""
	if dl {
		n++
	}
	gw := 2*pad + n*bs
	gh := 2*pad + bs
	org := image.Pt(right-gw, (rowH-gh)/2)
	r := image.Rectangle{Min: org, Max: org.Add(image.Pt(gw, gh))}
	theme.Shadow(gtx, r, gtx.Dp(12), gtx.Dp(3), argb(0x14000000))
	rrect(gtx, r, gtx.Dp(12), v.p.Surface)
	border(gtx, r, gtx.Dp(12), max(1, gtx.Dp(1)), v.p.Ring)
	x := org.X + pad
	th := v.h.Theme()
	if dl {
		dirAt(gtx, image.Pt(x, org.Y+pad), func(gtx C) D {
			d := v.actionButton(gtx, &c.download, "download")
			popups.Tooltip(gtx, th, &c.downloadTip, c.download.Hovered(), d.Size, popups.Above, nsFile.T("Download"), "")
			return d
		})
		x += bs
	}
	dirAt(gtx, image.Pt(x, org.Y+pad), func(gtx C) D {
		d := v.actionButton(gtx, &c.menu, "kebab")
		c.menuSize = d.Size
		popups.Tooltip(gtx, th, &c.menuTip, c.menu.Hovered(), d.Size, popups.Above, nsFile.T("More actions"), "")
		return d
	})
}

// actionButton is c-message_actions__button: 32px, radius 8, an 18px icon
// in foreground_max_solid, the hover fill and primary icon colour.
func (v *ChannelFiles) actionButton(gtx C, c *widget.Clickable, icon string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		s := gtx.Dp(32)
		col := v.p.Sec
		if c.Hovered() {
			rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), v.p.Hover)
			col = v.p.Text
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(s, s))
		layout.Center.Layout(g, func(gtx C) D { return v.icon(gtx, icon, 18, col) })
		return D{Size: image.Pt(s, s)}
	})
}

// linkRow is LinkEntity in the list (compact Entity with its border,
// renderedInListVariant, as tall as a file row): 12px padding, a 24px favicon (else link 20),
// 12px, the primary label (title, else the host) over the secondary (the
// host when titled, else path + query, "" for "/"; "Open in browser" on
// hover), More actions on hover.
func (v *ChannelFiles) linkRow(gtx C, l *core.ChannelLink, first, last bool) D {
	key := "link:" + l.URL
	c := v.rowOf(key)
	ring := v.kb.ringOn(key)
	primary, secondary := cfLinkLabels(l)
	return c.row.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		hover := c.row.Hovered() || c.menu.Hovered()
		bw := max(1, gtx.Dp(1))
		w := gtx.Constraints.Max.X
		// one height for every row: the list variant isn't isAutoHeight, so
		// a link without a second line keeps the row's height, its label
		// centred
		h := 2*gtx.Dp(12) + gtx.Dp(36) + bw
		textH := gtx.Dp(22)
		if secondary != "" {
			textH += gtx.Dp(18)
		}
		if last {
			h += bw
		}
		v.listFrame(gtx, image.Pt(w, h), first, last, hover)
		is := gtx.Dp(24)
		iy := (h - is) / 2
		dirAt(gtx, image.Pt(bw+gtx.Dp(12), iy), func(gtx C) D { return v.favicon(gtx, l.IconURL) })
		tx := bw + gtx.Dp(12) + is + gtx.Dp(12)
		tw := max(0, w-tx-gtx.Dp(12)-bw)
		sec := secondary
		if hover && sec != "" {
			sec = nsExplorerLnk.T("Open in browser")
		}
		dirAt(gtx, image.Pt(tx, bw+(h-bw-textH)/2), func(gtx C) D {
			gtx.Constraints.Max.X = tw
			return col(gtx,
				layout.Rigid(func(gtx C) D {
					return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: wBold, Color: v.p.Text, MaxLines: 1}, primary)
				}),
				layout.Rigid(func(gtx C) D {
					if sec == "" {
						return D{}
					}
					return v.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Weight: wReg, Color: v.p.Muted, MaxLines: 1}, sec)
				}),
			)
		})
		if hover {
			bs := gtx.Dp(28) // the Entity actions bar's smedium buttons
			pad := gtx.Dp(4)
			gw, gh := 2*pad+bs, 2*pad+bs
			org := image.Pt(w-bw-gtx.Dp(12)-gw, (h-gh)/2)
			r := image.Rectangle{Min: org, Max: org.Add(image.Pt(gw, gh))}
			rrect(gtx, r, gtx.Dp(8), v.p.Surface)
			border(gtx, r, gtx.Dp(8), max(1, gtx.Dp(1)), v.p.Ring)
			th := v.h.Theme()
			dirAt(gtx, org.Add(image.Pt(pad, pad)), func(gtx C) D {
				d := v.iconButton(gtx, &c.menu, "kebab", 28, 18, v.p.Sec)
				c.menuSize = d.Size
				popups.Tooltip(gtx, th, &c.menuTip, c.menu.Hovered(), d.Size, popups.Above, nsExplorerLnk.T("More actions"), "")
				return d
			})
		}
		if ring {
			v.vkRing(gtx, image.Rect(0, 0, w, h), gtx.Dp(12))
		}
		if last || v.preview {
			return D{Size: image.Pt(w, h)}
		}
		return D{Size: image.Pt(w, h-bw)}
	})
}

// cfLinkLabels are LinkEntity's labels: the title or the host, then the
// host (titled) or the path and query ("" for "/").
func cfLinkLabels(l *core.ChannelLink) (primary, secondary string) {
	u, err := url.Parse(l.URL)
	host, path := "", ""
	if err == nil {
		host = u.Hostname()
		path = u.EscapedPath()
		if path == "" {
			path = "/"
		}
		if u.RawQuery != "" {
			path += "?" + u.RawQuery
		}
	}
	if l.Title != "" {
		return l.Title, host
	}
	if path == "/" {
		path = ""
	}
	if host == "" {
		host = l.URL
	}
	return host, path
}

// favicon is LinkEntity's icon: the 24px icon_url image, else (none or not
// loading) the link glyph at 20.
func (v *ChannelFiles) favicon(gtx C, src string) D {
	s := gtx.Dp(24)
	if src != "" && v.h.Images() != nil {
		if img, ok := v.h.Images().Get(src, s*2); ok {
			return themeImage(gtx, img, image.Pt(s, s), 0)
		}
	}
	dirAt(gtx, image.Pt((s-gtx.Dp(20))/2, (s-gtx.Dp(20))/2), func(gtx C) D { return v.icon(gtx, "link", 20, v.p.Text) })
	return D{Size: image.Pt(s, s)}
}

// moreTypes is MoreResultsByType: "More results by type" (15px Bold, 36
// above, 20 under) and the category cards (42 tall, 12px padding, 1px
// otl-ter, ctr-pry, radius 12, 12 apart; the 16px icon, 8px, the 13px Bold
// label, caret-right at the end; surf-pry on hover), 32 under.
func (v *ChannelFiles) moreTypes(gtx C, cards []int) D {
	labels := [...]struct{ icon, label string }{{"files", "Files"}, {"image", "Media"}, {"link", "Links"}}
	return layout.Inset{Top: 36, Bottom: 32}.Layout(gtx, func(gtx C) D {
		return col(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 20}.Layout(gtx, func(gtx C) D {
					return v.label(gtx, 15, wBold, v.p.Text, nsTabbed.T("More results by type"))
				})
			}),
			layout.Rigid(func(gtx C) D {
				ch := make([]layout.FlexChild, 0, 2*len(cards))
				for i, k := range cards {
					k := k
					if i > 0 {
						ch = append(ch, hgap(12))
					}
					ch = append(ch, layout.Rigid(func(gtx C) D {
						return v.typeCard(gtx, &v.moreType[k], labels[k].icon, nsTabbed.T(labels[k].label))
					}))
				}
				return row(gtx, layout.Middle, ch...)
			}),
		)
	})
}

func (v *ChannelFiles) typeCard(gtx C, c *widget.Clickable, icon, label string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		tw := v.th.Measure(gtx, 13, wBold, label)
		pad := gtx.Dp(12)
		w := pad + gtx.Dp(16) + gtx.Dp(8) + tw + gtx.Dp(4) + gtx.Dp(16) + pad
		sz := image.Pt(w, gtx.Dp(42))
		r := image.Rectangle{Max: sz}
		rad := gtx.Dp(12)
		bg := v.p.Surface
		if c.Hovered() {
			bg = v.lt(rgb(0xf8f8f8), rgb(0x2c2d31)) // --dt_color-surf-pry
		}
		rrect(gtx, r, rad, bg)
		border(gtx, r, rad, max(1, gtx.Dp(1)), v.p.CardLine)
		x := pad
		dirAt(gtx, image.Pt(x, (sz.Y-gtx.Dp(16))/2), func(gtx C) D { return v.icon(gtx, icon, 16, v.p.Text) })
		x += gtx.Dp(16 + 8)
		dirAt(gtx, image.Pt(x, 0), func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(tw, sz.Y))
			return layout.W.Layout(gtx, func(gtx C) D { return v.label(gtx, 13, wBold, v.p.Text, label) })
		})
		dirAt(gtx, image.Pt(w-pad-gtx.Dp(16), (sz.Y-gtx.Dp(16))/2), func(gtx C) D { return v.icon(gtx, "caret-right", 16, v.p.Text) })
		return D{Size: sz}
	})
}

// pagination is ExplorerLoadMoreButton: an outline medium "Load More",
// centred, 8px down (p-explorer_pagination margin_top_50).
func (v *ChannelFiles) pagination(gtx C) D {
	return layout.Inset{Top: 8, Bottom: 16}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.N.Layout(gtx, func(gtx C) D {
			label := i18n.T("files", "Load More")
			kind := popups.BtnOutline
			busy := v.media.data.busy() && v.typ == cfMedia || v.files.data.busy() && v.typ == cfFiles || v.links.data.busy() && v.typ == cfLinks
			if busy {
				kind = popups.BtnDisabled
			}
			// c-button--medium: 36 tall, 0 12px, 15px Bold, as wide as its label
			bw := v.th.Measure(gtx, 15, wBold, label) + 2*gtx.Dp(12) + 2*max(1, gtx.Dp(1))
			gtx.Constraints.Min.X, gtx.Constraints.Max.X = bw, bw
			return popups.Button(gtx, v.h.Theme(), &v.loadMore, kind, popups.BtnMedium, label, "", 0)
		})
	})
}

// emptyRow is the "empty-state" row: .p-explorer_empty_state (200 tall,
// centred) with EmptyState's title (18px/24 Black, at most 380 wide).
func (v *ChannelFiles) emptyRow(gtx C, title string) D {
	w := gtx.Constraints.Max.X
	h := gtx.Dp(200)
	dirAt(gtx, image.Point{}, func(gtx C) D {
		gtx.Constraints = layout.Exact(image.Pt(w, h))
		return layout.Center.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(380))
			gtx.Constraints.Min.X = 0
			return v.th.Layout(gtx, theme.TextStyle{Size: 18, LineHeight: 24, Weight: wBlack, Color: v.p.Text, Align: text.Middle}, title)
		})
	})
	return D{Size: image.Pt(w, h)}
}
