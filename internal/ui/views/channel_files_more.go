package views

// The Files & links tab, round 2 (files-links-2; CDN build 0e695dd18302,
// boot-shared 0fb3750ed9646c84; docs/FILES-AUDIT.md "Files & links tab,
// round 2"):
//
//   - The search's suggestions. ExplorerSearch (l7jR) is the Omniswitcher
//     (drV9) with useChannelAttachmentsExplorer's searcherOptions
//     (entities.files {channel}, limit 30, tiered, the type filter) and
//     hideAutocompleteWithEmptyQuery: while the box has text a list opens
//     under it. Its rows are the searcher's files (remote:
//     search.autocomplete.files "in:<#C> q", core.ChannelFileSuggestions;
//     local: the files the tab has loaded whose words start with the
//     query's) kept by the filter (All: every file; Media: images and
//     videos; Files: the rest; Links: none), with the query echo ("Show
//     results for: q", queryUser) after the top hit as the omniswitcher's
//     post-processing puts it. The remote search waits for 250 ms of
//     quiet, as Clack's omniswitcher does for its file rows. Rows are 40
//     tall; nothing picked, the first is pseudo-selected with the Enter
//     key; ↑/↓ pick (from none: the first or the last; past either end:
//     none again), Enter accepts the picked or pseudo-selected row:
//     the echo is onSearch's type-query completion (the search Clack had),
//     a file opens (useFileClick 2uBe: openFile). Escape or losing focus
//     closes the list (ExplorerSearch's onBlur), accepting closes it too.
//   - FileActions' hover group: Download, Share (share-message; files only:
//     a canvas opens openShareQuipModal and a list openShareListsModal,
//     whose dialogs Clack hasn't got), the canvas / list star
//     (renderFavoriteCanvas / renderFavoriteList; FileSectionDropdownButton
//     under custom_file_sections, as the Files view), More actions.
//     Starring a canvas outside the Files view toasts "Added {title} to
//     Starred." with View (addCanvasToFavorites); a list toasts only in a
//     child window (addListToFavorites), so not here.
//   - LinkEntity's View link details (info, 18): the link details view in
//     the secondary pane (toLinkDetails; ui/link_details.go).
//   - The media viewer: a cell's click passes fileChannelMediaInfo, the
//     media browser's loaded files, and the viewer pages through them
//     (popups.NewFileGallery); Clack's viewer shows images, so videos
//     still open their page.
//   - The grid's keys (jAOO Grid, byyT useContainerKeyboardNavigation):
//     ↑ the cell above (row > 0), ↓ the cell below when there is one, ←
//     the previous cell (the row before's last at a row's start), → the
//     next (the row after's first at its end); a key that finds no cell
//     goes on to the list (↑ above the first row, ↓ under the last). The
//     collapsed strip in All keeps every media file as a grid key and
//     hides the rows after the first (p-explorer_grid__hidden): a move
//     into a hidden cell is taken but focus can't land there, so it stays.

import (
	"context"
	"image"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// cfSugMax is the suggestion list's cut (the omniswitcher's places, 15).
const cfSugMax = 15

// cfSugDebounce is the remote search's quiet time.
const cfSugDebounce = 250 * time.Millisecond

type cfSugRes struct {
	q     string
	files []core.File
}

type cfSugRow struct {
	file *fileItem // nil: the query echo
}

// cfSuggest is the explorer search's suggestion list.
type cfSuggest struct {
	open    bool
	text    string
	changed time.Time
	fired   bool
	remote  async[cfSugRes]
	sel     int // the picked row, -1 none
	rows    []cfSugRow
	items   []fileItem // the rows' files (remote ones aren't in the browsers)
	clicks  [cfSugMax + 1]widget.Clickable
	focused bool
}

// sugWords are the query's lower-case words.
func sugWords(q string) []string { return strings.Fields(strings.ToLower(q)) }

// sugLocalMatch is the searcher's local match for a file: every query word
// starts one of the title's or name's words.
func sugLocalMatch(it *fileItem, words []string) bool {
	if len(words) == 0 {
		return false
	}
	toks := strings.FieldsFunc(strings.ToLower(it.title()+" "+it.Name), func(r rune) bool {
		return r == ' ' || r == '-' || r == '_' || r == '.' || r == '/' || r == '(' || r == ')'
	})
	for _, w := range words {
		hit := false
		for _, t := range toks {
			if strings.HasPrefix(t, w) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

func isMediaFile(it *fileItem) bool {
	return strings.HasPrefix(it.Mime, "image/") || strings.HasPrefix(it.Mime, "video/") || it.isImage()
}

// sugKeep is searcherOptions' filter for the Attachment type.
func (v *ChannelFiles) sugKeep(it *fileItem) bool {
	switch v.typ {
	case cfAll:
		return true
	case cfMedia:
		return isMediaFile(it)
	case cfFiles:
		return !isMediaFile(it)
	}
	return false // Links
}

// buildSuggestions makes the list's rows for the box's text.
func (v *ChannelFiles) buildSuggestions() {
	s := &v.sug
	words := sugWords(s.text)
	s.items = s.items[:0]
	seen := map[string]bool{}
	add := func(it fileItem) {
		if it.ID == "" || seen[it.ID] || !v.sugKeep(&it) || len(s.items) >= cfSugMax-1 {
			return
		}
		seen[it.ID] = true
		s.items = append(s.items, it)
	}
	if res, ok, err := s.remote.get(); ok && err == nil {
		for _, f := range v.fv.cleanTitles(res.files) {
			if res.q == s.text || sugLocalMatch(&f, words) { // stale answers stay only while they match
				add(f)
			}
		}
	}
	for _, it := range v.allFiles() {
		if sugLocalMatch(it, words) {
			add(*it)
		}
	}
	s.rows = s.rows[:0]
	if len(s.items) == 0 {
		s.rows = append(s.rows, cfSugRow{})
	}
	for i := range s.items {
		s.rows = append(s.rows, cfSugRow{file: &s.items[i]})
		if i == 0 {
			s.rows = append(s.rows, cfSugRow{}) // the echo after the top hit
		}
	}
	if s.sel >= len(s.rows) {
		s.sel = -1
	}
}

// sugUpdate runs the list before the search box sees its events: the keys
// it takes while open, the debounced remote search, the rows' clicks and
// the blur.
func (v *ChannelFiles) sugUpdate(gtx C) {
	s := &v.sug
	ed := &v.search.ed
	focused := gtx.Focused(ed)
	if s.open {
		fs := []event.Filter{
			key.Filter{Focus: ed, Name: key.NameUpArrow}, key.Filter{Focus: ed, Name: key.NameDownArrow},
			key.Filter{Focus: ed, Name: key.NameReturn}, key.Filter{Focus: ed, Name: key.NameEnter},
			key.Filter{Focus: ed, Name: key.NameEscape},
		}
		for {
			ev, ok := gtx.Event(fs...)
			if !ok {
				break
			}
			e, ok := ev.(key.Event)
			if !ok || e.State != key.Press {
				continue
			}
			switch e.Name {
			case key.NameUpArrow:
				v.sugMove(-1)
			case key.NameDownArrow:
				v.sugMove(1)
			case key.NameReturn, key.NameEnter:
				i := s.sel
				if i < 0 {
					i = 0
				}
				v.sugAccept(i)
			case key.NameEscape:
				s.open = false
			}
		}
	}
	for i := range s.clicks {
		if s.clicks[i].Clicked(gtx) && s.open && i < len(s.rows) {
			v.sugAccept(i)
		}
	}
	if s.open && !s.fired {
		due := s.changed.Add(cfSugDebounce)
		if gtx.Now.Before(due) {
			gtx.Execute(op.InvalidateCmd{At: due})
		} else {
			s.fired = true
			if a := v.h.Core(); a != nil {
				ch, q := v.ch, s.text
				s.remote.load(v.h, true, func(ctx context.Context) (cfSugRes, error) {
					fs, err := a.ChannelFileSuggestions(ctx, ch, q)
					return cfSugRes{q: q, files: fs}, err
				})
			}
		}
	}
	if s.focused && !focused && s.open && !v.sugPressed() {
		s.open = false // ExplorerSearch's onBlur: the list closes
	}
	s.focused = focused
}

func (v *ChannelFiles) sugPressed() bool {
	for i := range v.sug.clicks {
		if v.sug.clicks[i].Pressed() {
			return true
		}
	}
	return false
}

// sugChanged is the box's text changing: the list opens for any text
// (hideAutocompleteWithEmptyQuery) and the remote search waits again.
func (v *ChannelFiles) sugChanged(gtx C, t string) {
	s := &v.sug
	s.text = t
	s.sel = -1
	if t == "" {
		s.open = false
		return
	}
	s.open, s.changed, s.fired = true, gtx.Now, false
}

// sugMove is incrementSelectedIndex / decrementSelectedIndex.
func (v *ChannelFiles) sugMove(d int) {
	s := &v.sug
	n := len(s.rows)
	if n == 0 {
		return
	}
	switch {
	case s.sel < 0 && d > 0:
		s.sel = 0
	case s.sel < 0:
		s.sel = n - 1
	case s.sel+d < 0 || s.sel+d >= n:
		s.sel = -1
	default:
		s.sel += d
	}
}

// sugAccept is onAccept: the echo searches, a file opens; the list closes.
func (v *ChannelFiles) sugAccept(i int) {
	s := &v.sug
	if i < 0 || i >= len(s.rows) {
		return
	}
	r := s.rows[i]
	s.open, s.sel = false, -1
	if r.file == nil {
		if t := strings.TrimSpace(v.search.ed.Text()); t != "" {
			v.runSearch(t)
		}
		return
	}
	v.open(*r.file)
}

// suggestions draws the list under the search box (top: the box's bottom).
func (v *ChannelFiles) suggestions(gtx C, top int) {
	s := &v.sug
	if !s.open || len(s.rows) == 0 {
		return
	}
	full := gtx.Constraints.Max.X
	w := min(gtx.Dp(968), max(0, full-gtx.Dp(32)))
	x := (full - w) / 2
	rowH := gtx.Dp(40)
	pad := gtx.Dp(8)
	h := 2*pad + rowH*len(s.rows)
	r := image.Rect(x, top+gtx.Dp(4), x+w, top+gtx.Dp(4)+h)
	theme.Shadow(gtx, r, gtx.Dp(8), gtx.Dp(6), argb(0x26000000))
	rrect(gtx, r, gtx.Dp(8), v.p.Surface)
	border(gtx, r, gtx.Dp(8), max(1, gtx.Dp(1)), v.p.Ring)
	for i, row := range s.rows {
		i, row := i, row
		y := r.Min.Y + pad + i*rowH
		dirAt(gtx, image.Pt(x, y), func(gtx C) D {
			return v.sugRow(gtx, i, &row, image.Pt(w, rowH))
		})
	}
}

// sugRow is SuggestionFile / SuggestionQuery: the 20px icon in a 24px box
// 16 in, the 15px title at 52; picked (--selected) #1264a3 with white
// text, pseudo-selected base-ter with the Enter key 16 from the end.
func (v *ChannelFiles) sugRow(gtx C, i int, r *cfSugRow, sz image.Point) D {
	s := &v.sug
	c := &s.clicks[i]
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		picked := i == s.sel
		pseudo := s.sel < 0 && i == 0
		fg, fg2 := v.p.Text, v.p.Sec
		switch {
		case picked:
			fill(gtx, image.Rectangle{Max: sz}, v.p.SelBg)
			fg, fg2 = v.p.SelText, v.p.SelText
		case pseudo || c.Hovered():
			fill(gtx, image.Rectangle{Max: sz}, v.p.Track)
		}
		ic := "search"
		title := ""
		if r.file != nil {
			ic = "file"
			if r.file.isCanvas() {
				ic = "canvas-content" // getSuggestionIcon: DQ (a canvas) → canvas-content
			}
			title = r.file.title()
		}
		is := gtx.Dp(20)
		dirAt(gtx, image.Pt(gtx.Dp(16)+(gtx.Dp(24)-is)/2, (sz.Y-is)/2), func(gtx C) D { return v.icon(gtx, ic, 20, fg2) })
		right := sz.X - gtx.Dp(16)
		if pseudo {
			label := "Enter"
			tw := v.th.Measure(gtx, 13, wReg, label)
			cw, ch := tw+gtx.Dp(12), gtx.Dp(20)
			kr := image.Rect(right-cw, (sz.Y-ch)/2, right, (sz.Y+ch)/2)
			rrect(gtx, kr, gtx.Dp(4), v.p.Ring)
			rrect(gtx, kr.Inset(max(1, gtx.Dp(1))), gtx.Dp(3), v.p.Surface)
			dirAt(gtx, image.Pt(kr.Min.X+gtx.Dp(6), kr.Min.Y), func(gtx C) D {
				gtx.Constraints = layout.Exact(image.Pt(tw+1, ch))
				return layout.W.Layout(gtx, func(gtx C) D { return v.label(gtx, 13, wReg, v.p.Text, label) })
			})
			right -= cw + gtx.Dp(16)
		}
		tx := gtx.Dp(52)
		dirAt(gtx, image.Pt(tx, 0), func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(max(0, right-tx), sz.Y))
			return layout.W.Layout(gtx, func(gtx C) D {
				if r.file != nil {
					return v.label(gtx, 15, wReg, fg, title)
				}
				// SuggestionQuery for queryUser: "Show results for: " then the query
				lead := nsSearch.T("Show results for:") + " "
				lw := v.th.Measure(gtx, 15, wReg, lead)
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wReg, fg, lead) }),
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Max.X = max(0, gtx.Constraints.Max.X-lw)
						return v.label(gtx, 15, wBold, fg, s.text)
					}),
				)
			})
		})
		return D{Size: sz}
	})
}

// ---- hover actions ----

// shareShown is renderShare for the tab's rows, with what Clack can share:
// a file (a canvas or list share is its own dialog).
func cfShareShown(it *fileItem) bool {
	return it.ID != "" && !it.isCanvas() && !it.isList() && it.Mode != "tombstone"
}

// starShown: renderFavoriteCanvas / renderFavoriteList (or the section
// button under custom_file_sections).
func (v *ChannelFiles) starShown(it *fileItem) bool {
	if v.fv.sectionButtonShown(it) {
		return true
	}
	return !v.h.Core().FileSectionsOn() && (it.isCanvas() || it.isList())
}

// shareLabel is getShareLabel's tooltip (asButton false): "Share file…".
func cfShareLabel() string { return nsFile.T("Share file…") }

// openShare is FileActions onShare for a file: openShareDialog.
func (v *ChannelFiles) openShare(it fileItem) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	tile := it
	m := popups.NewShareFileModal(ph, popups.ShareFileOpts{ID: it.ID, Title: it.title(), Permalink: it.Permalink,
		Preview: func(gtx C) D { return v.sharePreview(gtx, &tile) },
		Shared: func(ch, ts string) {
			// navigate to the destination at the share (same team)
			if ts != "" {
				v.h.Navigate("c/" + ch + "/t/" + ts)
			} else {
				v.h.Navigate("c/" + ch)
			}
		}})
	ph.ShowOverlay(m)
}

// sharePreview is ShareDialogPreview's file: the file entity (36px tile,
// 15px Bold title, the type under it) in 12px padding.
func (v *ChannelFiles) sharePreview(gtx C, it *fileItem) D {
	v.frame()
	v.fv.frame()
	w := gtx.Constraints.Max.X
	pad := gtx.Dp(12)
	h := 2*pad + gtx.Dp(36)
	dirAt(gtx, image.Pt(pad, pad), func(gtx C) D { return v.fv.fileTile(gtx, it, 36) })
	tx := pad + gtx.Dp(36) + gtx.Dp(12)
	dirAt(gtx, image.Pt(tx, pad-gtx.Dp(2)), func(gtx C) D {
		gtx.Constraints.Max.X = max(0, w-tx-pad)
		return col(gtx,
			layout.Rigid(func(gtx C) D {
				return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: wBold, Color: v.p.Text, MaxLines: 1}, it.title())
			}),
			layout.Rigid(func(gtx C) D {
				return v.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 16, Weight: wReg, Color: v.p.Muted, MaxLines: 1}, fileKindLabel(it))
			}),
		)
	})
	return D{Size: image.Pt(w, h)}
}

// star is the star button's click: the section menu under
// custom_file_sections, else toggleStar, with addCanvasToFavorites' toast
// for a canvas starred here.
func (v *ChannelFiles) star(it fileItem, btn image.Rectangle) {
	if v.fv.sectionButtonShown(&it) {
		v.fv.openMoveMenu(it, btn)
		return
	}
	a := v.h.Core()
	if a == nil {
		return
	}
	starring := !a.FileStarred(it.ID)
	v.fv.toggleStar(it.File)
	if starring && it.isCanvas() {
		text := nsFiles.T("Added {fileName} to Starred.", "fileName", it.title())
		id := it.ID
		if t, ok := v.h.(interface {
			ToastAction(text, label string, do func())
		}); ok {
			t.ToastAction(text, nsFiles.T("View"), func() {
				if o, ok := v.h.(interface{ OpenCanvasFull(id string) }); ok {
					o.OpenCanvasFull(id)
				}
			})
		} else {
			v.h.Toast(text)
		}
	}
}

// openLinkDetails is LinkEntity's View link details.
func (v *ChannelFiles) openLinkDetails(l core.ChannelLink) {
	title, _ := cfLinkLabels(&l)
	if o, ok := v.h.(interface{ OpenLinkDetails(url, title, icon string) }); ok {
		o.OpenLinkDetails(l.URL, title, l.IconURL)
	}
}

// ---- the media viewer ----

// openMedia is the media cell's handleFileContainerClick with
// fileChannelMediaInfo: images open in the viewer with the media
// browser's loaded images to page through, videos on their page.
func (v *ChannelFiles) openMedia(it fileItem) {
	g, ok := v.h.(interface {
		OpenMediaGallery(items []popups.GalleryItem, idx int)
	})
	if !ok || !it.isImage() || it.URL == "" {
		v.open(it)
		return
	}
	var items []popups.GalleryItem
	idx := 0
	for i := range v.media.items {
		m := &v.media.items[i]
		if !m.isImage() || m.URL == "" { // fileIsViewableInMediaViewer, as far as Clack's viewer goes
			continue
		}
		if m.ID == it.ID {
			idx = len(items)
		}
		f := cfRich(m)
		items = append(items, popups.GalleryItem{File: f, Img: richtext.Image{URL: m.URL, Alt: m.title(), W: m.ThumbW,
			H: m.ThumbH, Title: m.title(), Proxy: true}})
	}
	if len(items) == 0 {
		v.open(it)
		return
	}
	g.OpenMediaGallery(items, idx)
}

// ---- grid keys ----

// gridKey is the grid's onKeyDown for an active cell: true when the grid
// took the key (a move, or one into a hidden cell of the strip).
func (v *ChannelFiles) gridKey(e key.Event, active string) bool {
	if !strings.HasPrefix(active, "cell:") || v.gridColsN <= 0 {
		return false
	}
	cols := v.gridColsN
	keys := make([]string, len(v.media.items))
	at := -1
	for i := range v.media.items {
		keys[i] = "cell:" + v.media.items[i].ID
		if keys[i] == active {
			at = i
		}
	}
	if at < 0 {
		return false
	}
	r, c := at/cols, at%cols
	idx := func(r, c int) int { return r*cols + c }
	to := -1
	switch e.Name {
	case key.NameUpArrow:
		if r > 0 {
			to = idx(r-1, c)
		}
	case key.NameDownArrow:
		if idx(r+1, c) < len(keys) {
			to = idx(r+1, c)
		}
	case key.NameLeftArrow:
		if c > 0 {
			to = idx(r, c-1)
		} else if r > 0 {
			to = idx(r-1, cols-1)
		}
	case key.NameRightArrow:
		if c+1 < cols && idx(r, c+1) < len(keys) {
			to = idx(r, c+1)
		} else if idx(r+1, 0) < len(keys) {
			to = idx(r+1, 0)
		}
	default:
		return false
	}
	if to < 0 || to >= len(keys) {
		return false // the list moves on
	}
	if to >= v.gridShown {
		return true // a hidden cell of the strip: focus stays
	}
	v.kb.setActive(keys[to], true)
	if !v.kb.inView(keys[to]) {
		v.kb.reveal(keys[to])
	}
	return true
}

var cfGridExtra = []key.Filter{{Name: key.NameLeftArrow}, {Name: key.NameRightArrow}}

// starAction is the hover group's star (c-file__star_button, a 32px
// c-message_actions__button): "star", or "star-filled" on
// c-file__unstar_button's sunflower-0 with sunflower-60 (hover sunflower-5
// / -70).
func (v *ChannelFiles) starAction(gtx C, c *widget.Clickable, on bool) D {
	if !on {
		return v.actionButton(gtx, c, "star")
	}
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		sz := gtx.Dp(32)
		bg, fg := rgb(0xfff4d9), rgb(0x9a6500)
		if c.Hovered() {
			bg, fg = rgb(0xffe7a8), rgb(0x7a4f00)
		}
		rrect(gtx, image.Rect(0, 0, sz, sz), gtx.Dp(8), bg)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(sz, sz))
		layout.Center.Layout(g, func(gtx C) D { return v.icon(gtx, "star-filled", 18, fg) })
		return D{Size: image.Pt(sz, sz)}
	})
}

// starTip is the star's tooltip: renderFavoriteCanvas "Add to starred" /
// "Unstar canvas", renderFavoriteList "Add to starred" / "Unstar list";
// none for the section button (it has its own, FileSectionDropdownButton's).
func (v *ChannelFiles) starTip(it *fileItem) string {
	if v.fv.sectionButtonShown(it) {
		return ""
	}
	on := v.h.Core().FileStarred(it.ID)
	switch {
	case !on:
		return nsFile.T("Add to starred")
	case it.isList():
		return nsFile.T("Unstar list")
	}
	return nsFile.T("Unstar canvas")
}

// ---- Add to folder ----

// addLinkToFolder is LinkEntity's "Add to folder" click: AddBookmarkModal
// with modalHeaderTitle "Add link to folder", the link (initialLink) and
// its title (initialPreview), showSetFolder over the channel's folders.
// With none it says the link goes to a Bookmarks folder and makes it
// first (addBookmarkFolderForChannel: bookmarks.add type folder); then
// bookmarks.add {channel, link, title, type: link, parent_id}
// (core.AddFolderLink), and "Added {fileName} to folder {folderName}"
// for 5 s. Slack's row is a submenu (AddOrMoveToFolderMenuItems), but for
// a link that renders nothing (it lists only for a file in the store), so
// Clack draws the row without one.
func (v *ChannelFiles) addLinkToFolder(l core.ChannelLink) {
	ph, ok := v.h.(popups.Host)
	a := v.h.Core()
	if !ok || a == nil {
		return
	}
	ch := v.ch
	var opts []popups.FolderOption
	titles := map[string]string{}
	for _, f := range a.ChannelFolders(ch) {
		opts = append(opts, popups.FolderOption{ID: f.ID, Title: f.Title})
		titles[f.ID] = f.Title
	}
	h := v.h
	toast := ""
	m := popups.NewBookmarkLinkModal(ph, popups.BookmarkLinkOpts{Title: nsExplorerLnk.T("Add link to folder"), Link: l.URL,
		Name: l.Title, ShowFolder: true, Folders: opts,
		SaveIn: func(ctx context.Context, link, name, folder string) error {
			fname := titles[folder]
			if folder == "" {
				f, err := a.AddBookmarkFolder(ctx, ch, nsTabbed.T("Bookmarks"))
				if err != nil {
					return err
				}
				folder, fname = f.ID, f.Title
			}
			b, err := a.AddFolderLink(ctx, ch, folder, link, name)
			if err != nil {
				return err
			}
			what := b.Title
			if what == "" {
				what = link
			}
			toast = nsTabbed.T("Added {fileName} to folder {folderName}", "fileName", what, "folderName", fname)
			return nil
		}})
	m.OnSaved = func() { h.Toast(toast) } // on the UI goroutine, after the save
	ph.ShowOverlay(m)
}
