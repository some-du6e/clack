package popups

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"unicode/utf8"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Emoji picker, measured from app.slack.com (2026-11): card 361×518,
// radius 6, ring 1px rgba(94,93,96,.13) + 0 4px 6px -4px and
// 0 10px 15px -3px rgba(0,0,0,.098).
//
//	y   0.. 50  "Emoji | GIFs" tabs, 15/700; active: #1d1c1d + 2px #83388a
//	            bottom bar, inactive #454447; 1px otl-ter line under both
//	            (layoutTop has the dark values and Slack's icon states)
//	y  50.. 96  category tabs: 32×28 tiles radius 4 at x 10 + 34i, y 60;
//	            20px icons #868686, active tile #dddddd with #1d1c1d icon
//	y  96..146  search input at x 18, y 103, 326×36, radius 8, 15px,
//	            focus ring 1px #1264a3 + 4px rgba(18,100,163,.3)
//	y 146..458  list (virtual rows, getRowHeight): each group is a 1px
//	            empty-header-row, a 27px heading row (13/700 at x 21 in a
//	            24px line) and 33px emoji rows (the list's last one 43);
//	            9 cells 36×32 radius 6 from x 15, 22px images. The sticky
//	            heading is 26 tall (text at x 20, bg primary at .95). The
//	            hovered or keyboard-selected cell is tinted by its
//	            data-color-index, (9 × screen row + column) mod 6: #9bdf58,
//	            #95d3fe, #f7e922, #eea6fc, #ffd58f, #d3d1ff at .7 (both
//	            modes). The scrollbar thumb (8px, radius 4, min 36,
//	            primary foreground at .52) shows while the pointer is over
//	            the list.
//	y 458..518  footer #f8f8f8, 1px rgba(0,0,0,.15) top border: idle
//	            "Add Emoji" (85×28 at 14,16) or the hovered emoji (32px at
//	            13,14, name 14.4/700 and aliases 12.8/400 #616061 at x 57);
//	            the skin tone toggle (22px hand, "Skin Tone" 14.4/700 #454447
//	            while no tone is set) at right, its popover above it
//	            (emoji_tone.go).
const (
	epW, epH       = 361, 518
	epTopH         = 50
	epCatX, epCatY = 10, 60
	epCatStep      = 34
	epCatW, epCatH = 32, 28
	epInputX       = 18
	epInputY       = 103
	epInputH       = 36 // measured: 326×36 at (18,103)
	epListY        = 146
	epFootH        = 60
	epHdrH         = 27 // heading row (getRowHeight "group")
	epHdrGap       = 1  // the empty-header-row before every group
	epStickyH      = 26 // the sticky heading band
	epRowH         = 33
	epLastExtra    = 10 // the list's last emoji row is 43
	epCellX        = 15
	epCellW        = 36
	// .p-emoji_picker__list_item{width:36px;margin:0 1px -1px 0}: one
	// cell every 37px (the 1px right margin between them)
	epCellPitch = 37
	epCellH     = 32
	epImgSz     = 22
	epCols      = 9
	epEmbInputY = 7 // hideTabs: .p-emoji_picker--style-v2 .p-emoji_picker__input_container padding-top
)

// EmojiPickerSize is the picker card size in dp (361×518), for callers
// that anchor it.
func EmojiPickerSize() image.Point { return image.Pt(epW, epH) }

// epRow is one list row: a group heading (n == 0) or up to 9 emoji
// lists[list][start : start+n]. y is the row's top in px.
type epRow struct {
	y     int32
	start int32
	si    int32 // Slack's screen row index (data-color-index)
	list  uint8
	n     uint8
}

// epCell addresses a grid cell (row index into rows, column); row -1 = none.
type epCell struct{ row, col int }

var epNoCell = epCell{-1, -1}

// epLists holds the id slices rows point into: groups 0..9, then search.
type epLists = [epNumGroups + 1][]int32

// epLayoutRows flattens lists into heading + emoji rows (search: only the
// search list). A heading row is gap + hdr tall (Slack's 1px
// empty-header-row and its 27px group row), emoji rows rowH, the last one
// rowH + last. It returns the rows, each group's heading row (-1 when
// absent) and the total height. Heights are px.
func epLayoutRows(dst []epRow, lists *epLists, search bool, hdr, gap, rowH, last int) ([]epRow, [epNumGroups + 1]int32, int) {
	dst = dst[:0]
	var heads [epNumGroups + 1]int32
	for i := range heads {
		heads[i] = -1
	}
	y, si := 0, int32(0)
	add := func(g int) {
		ids := lists[g]
		if len(ids) == 0 {
			return
		}
		heads[g] = int32(len(dst))
		dst = append(dst, epRow{y: int32(y), si: si + 1, list: uint8(g)})
		y += gap + hdr
		si += 2 // empty-header-row, group
		for s := 0; s < len(ids); s += epCols {
			n := minInt(epCols, len(ids)-s)
			dst = append(dst, epRow{y: int32(y), start: int32(s), si: si, list: uint8(g), n: uint8(n)})
			y += rowH
			si++
		}
	}
	if search {
		add(epGroupSearch)
	} else {
		for g := 0; g < epNumGroups; g++ {
			add(g)
		}
	}
	if len(dst) > 0 && dst[len(dst)-1].n > 0 {
		y += last
	}
	return dst, heads, y
}

// EmojiPicker is Slack's emoji picker overlay.
type EmojiPicker struct {
	base
	h      Host
	onPick func(name string)

	anchorPt   image.Point
	anchorRect image.Rectangle
	anchorKind uint8 // 0 default, 1 point, 2 rect
	side       Side  // Above (default) or Below

	scrim scrim
	block blocker
	rect  image.Rectangle

	ed        widget.Editor
	focused   bool
	query     string
	searching bool
	emptyMsg  string

	idx    *emojiIndex
	full   bool
	freq   []int32
	lists  epLists
	search epSearcher

	rows      []epRow
	groupRow  [epNumGroups + 1]int32
	totalH    int
	rowsScale float32
	rowsDirty bool
	rowFn     layout.ListElement

	list     layout.List
	viewH    int
	gridTag  int
	ptr      image.Point
	ptrIn    bool
	press    epCell
	pressed  bool
	hover    epCell
	sel      epCell
	tabLock  int
	lockN    int
	lockPos  [2]int
	drawnTab int

	topTabs [2]widget.Clickable
	cats    [epNumGroups]widget.Clickable
	catTips [epNumGroups]TipState
	addBtn  widget.Clickable
	gifsBtn widget.Clickable // EmojiListNoResults' Search GIFs Instead
	toneBtn widget.Clickable
	toneUI  epToneUI // the skin tone popover (emoji_tone.go)
	tone    int

	prevID    int32
	prevName  string
	prevAlias string

	// GIFs tab (emoji_gif.go)
	gifMode      bool
	gif          gifPanel
	onGif        func(g Gif)
	gifCh, gifTS string

	// embedded: the bare EmojiPicker inside another card with hideTabs
	// (HuddleQuickEmojiPicker's Reactions tab; LayoutEmbedded)
	embedded bool

	// EmojiPickerPopover (the message reacted to) and slackc state
	msgCh, msgTS, msgThread string
	key                     string
	win                     image.Point
	scale                   float32
}

// NewEmojiPicker opens the picker near the bottom center of the window.
func NewEmojiPicker(h Host, onPick func(name string)) *EmojiPicker {
	p := &EmojiPicker{h: h, onPick: onPick, side: Above, key: nextKey("emoji")}
	p.ed = widget.Editor{SingleLine: true, Submit: true}
	p.list.Axis = layout.Vertical
	p.sel, p.hover, p.press = epNoCell, epNoCell, epNoCell
	p.tabLock, p.drawnTab, p.prevID = -1, -1, -1
	p.tone = epTone(h)
	p.rowFn = p.layoutRow
	p.refreshIndex()
	return p
}

// NewEmojiPickerAt opens the picker above anchor (window px, e.g. the click
// point of an emoji button) with its left edge at anchor.X; it flips below
// and clamps at the window edges.
func NewEmojiPickerAt(h Host, anchor image.Point, onPick func(name string)) *EmojiPicker {
	p := NewEmojiPicker(h, onPick)
	p.anchorPt, p.anchorKind = anchor, 1
	return p
}

// NewEmbeddedEmojiPicker is the bare EmojiPicker that HuddleQuickEmojiPicker
// (gantry-v2-async-client-boot-shared Vw0B) puts in its Reactions tab:
// hideTabs (no group tabs), no Emoji | GIFs row (that row belongs to
// EmojiAndGIFPickerPopover, which isn't there), no card of its own. The
// owner draws it with LayoutEmbedded and closes its card once Dismissed.
func NewEmbeddedEmojiPicker(h Host, onPick func(name string)) *EmojiPicker {
	p := NewEmojiPicker(h, onPick)
	p.embedded = true
	return p
}

// LayoutEmbedded draws an embedded picker into gtx.Constraints.Max at the
// origin: the search box (.p-emoji_picker__input_container: padding-top
// 7px, 17.5px at the sides, 7px under), the list and the footer, which
// grows to fill the box (.p-huddle_quick_emoji_picker__tab_panel
// .c-menu.p-emoji_picker: a 260px column, 200px in compact mode).
func (p *EmojiPicker) LayoutEmbedded(gtx layout.Context) layout.Dimensions {
	drain()
	if p.done {
		return layout.Dimensions{}
	}
	stacked(gtx, p)
	sz := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{}
	p.refreshIndex()
	p.scale = gtx.Metric.PxPerDp
	if isTop(p) && !p.toneUI.open && escape(gtx, nil) {
		p.Dismiss()
		return layout.Dimensions{}
	}
	p.win = sz
	th := p.h.Theme()
	cl := colorsOf(th)
	fill(gtx, image.Rectangle{Max: sz}, cl.EmojiBg)
	p.layoutBody(gtx, th, cl, sz)
	return layout.Dimensions{Size: sz}
}

// inputY and listY are the search box's and the list's tops: under the
// tab rows, or with hideTabs at the top (7px, then the 36px box and 7px).
func (p *EmojiPicker) inputY() float32 {
	if p.embedded {
		return epEmbInputY
	}
	return epInputY
}

func (p *EmojiPicker) listY() float32 {
	if p.embedded {
		return epEmbInputY + epInputH + 7
	}
	return epListY
}

// SetSide sets the preferred side of the anchor (Above by default; Below
// for anchors near the top such as the status modal's emoji button).
func (p *EmojiPicker) SetSide(s Side) {
	if s == Below {
		p.side = Below
	} else {
		p.side = Above
	}
}

// SetAnchorRect anchors the picker to a rectangle (window px): it opens
// above r with its left edge aligned to r's (like Slack over the
// composer), flipping below / clamping as needed.
func (p *EmojiPicker) SetAnchorRect(r image.Rectangle) {
	p.anchorRect, p.anchorKind = r, 2
}

// refreshIndex picks up a newer (custom-inclusive) index when it lands.
func (p *EmojiPicker) refreshIndex() {
	if p.idx != nil && p.full {
		return
	}
	x, full := epIndexFor(p.h)
	p.full = full
	if x == p.idx {
		return
	}
	p.idx = x
	for g := 1; g < epNumGroups; g++ {
		p.lists[g] = x.group[g]
	}
	p.loadFrequent()
	if p.searching {
		p.lists[epGroupSearch], _ = p.search.search(x, p.query, 0)
		p.sel = epNoCell
		if len(p.lists[epGroupSearch]) > 0 {
			p.sel = epCell{1, 0}
		}
	} else {
		p.sel = epNoCell
	}
	p.prevID = -1
	p.rowsDirty = true
}

// loadFrequent fills Frequently Used from getFrequentlyUsedEmoji's names
// (duplicates kept, as Slack's list can repeat a default); names the
// index doesn't have yet (custom emoji still loading) are skipped until
// the full index arrives.
func (p *EmojiPicker) loadFrequent() {
	p.freq = p.freq[:0]
	for _, n := range epFrequentNames(p.h) {
		if id, ok := p.idx.lookup(n); ok {
			p.freq = append(p.freq, id)
		}
	}
	p.lists[epGroupFrequent] = p.freq
}

func (p *EmojiPicker) ensureRows(gtx layout.Context) {
	if !p.rowsDirty && p.rowsScale == gtx.Metric.PxPerDp {
		return
	}
	p.rowsDirty = false
	p.rowsScale = gtx.Metric.PxPerDp
	p.rows, p.groupRow, p.totalH = epLayoutRows(p.rows, &p.lists, p.searching,
		dp(gtx, epHdrH), dp(gtx, epHdrGap), dp(gtx, epRowH), dp(gtx, epLastExtra))
}

func (p *EmojiPicker) setQuery(gtx layout.Context, q string) {
	q = strings.TrimSpace(q)
	if q == p.query {
		return
	}
	p.query = q
	p.searching = epNormQuery(q) != ""
	p.lists[epGroupSearch] = nil
	p.sel = epNoCell
	if p.searching {
		p.lists[epGroupSearch], _ = p.search.search(p.idx, q, 0)
		if len(p.lists[epGroupSearch]) > 0 {
			p.sel = epCell{1, 0}
		}
		p.emptyMsg = nsGifPicker.T("No emoji to show") // EmojiListNoResults (Zu56): the gif-picker namespace
	}
	p.rowsDirty = true
	p.ensureRows(gtx)
	p.list.Position = layout.Position{}
	p.tabLock = -1
	p.prevID = -1
}

// rowIDs returns the emoji ids of an emoji row.
func (p *EmojiPicker) rowIDs(r epRow) []int32 {
	return p.lists[r.list][r.start : r.start+int32(r.n)]
}

func (p *EmojiPicker) rowHeight(i int) int {
	if i+1 < len(p.rows) {
		return int(p.rows[i+1].y - p.rows[i].y)
	}
	return p.totalH - int(p.rows[i].y)
}

// rowAt is the row containing content offset y (px).
func (p *EmojiPicker) rowAt(y int) int {
	lo, hi := 0, len(p.rows)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if int(p.rows[mid].y) <= y {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// scrollTop is the content offset at the top of the viewport.
func (p *EmojiPicker) scrollTop() int {
	if len(p.rows) == 0 {
		return 0
	}
	f := p.list.Position.First
	if f < 0 {
		f = 0
	}
	if f >= len(p.rows) {
		f = len(p.rows) - 1
	}
	return int(p.rows[f].y) + p.list.Position.Offset
}

func (p *EmojiPicker) activeTab() int {
	if p.searching || len(p.rows) == 0 {
		return 0
	}
	if p.tabLock >= 0 {
		return p.tabLock
	}
	g := int(p.rows[p.rowAt(p.scrollTop()+1)].list)
	if g >= epNumGroups {
		return 0
	}
	return g
}

func (p *EmojiPicker) clickTab(i int) {
	if p.searching {
		p.ed.SetText("")
		p.query, p.searching = "", false
		p.lists[epGroupSearch] = nil
		p.sel = epNoCell
		p.rowsDirty = true
	}
	p.list.Position = layout.Position{}
	p.tabLock = -1
	if i == 0 {
		return
	}
	if p.rowsDirty {
		// rows are rebuilt with px metrics in Layout; the heading index
		// doesn't depend on them.
		_, heads, _ := epLayoutRows(nil, &p.lists, false, 1, 0, 1, 0)
		p.groupRow = heads
	}
	if r := p.groupRow[i]; r >= 0 {
		p.list.Position = layout.Position{First: int(r), BeforeEnd: true}
		p.tabLock, p.lockN = i, 1
	}
}

// ensureVisible scrolls so row i is fully visible below the sticky heading.
func (p *EmojiPicker) ensureVisible(gtx layout.Context, i int) {
	if i < 0 || i >= len(p.rows) || p.viewH <= 0 {
		return
	}
	scroll := p.scrollTop()
	top := int(p.rows[i].y)
	bot := top + p.rowHeight(i)
	sticky := dp(gtx, epStickyH)
	var nt int
	switch {
	case top < scroll+sticky:
		nt = top - sticky
	case bot > scroll+p.viewH:
		nt = bot - p.viewH
	default:
		return
	}
	if nt < 0 {
		nt = 0
	}
	k := p.rowAt(nt)
	p.list.Position = layout.Position{First: k, Offset: nt - int(p.rows[k].y), BeforeEnd: true}
	p.tabLock = -1
}

func (p *EmojiPicker) nextEmojiRow(from, dir int) int {
	for i := from + dir; i >= 0 && i < len(p.rows); i += dir {
		if p.rows[i].n > 0 {
			return i
		}
	}
	return -1
}

func (p *EmojiPicker) move(gtx layout.Context, name key.Name) {
	if len(p.rows) == 0 {
		return
	}
	s := p.sel
	if s.row < 0 || s.row >= len(p.rows) || p.rows[s.row].n == 0 {
		if r := p.nextEmojiRow(-1, 1); r >= 0 {
			p.sel = epCell{r, 0}
			p.ensureVisible(gtx, r)
		}
		return
	}
	n := int(p.rows[s.row].n)
	switch name {
	case key.NameRightArrow:
		if s.col+1 < n {
			s.col++
		} else if r := p.nextEmojiRow(s.row, 1); r >= 0 {
			s = epCell{r, 0}
		}
	case key.NameLeftArrow:
		if s.col > 0 {
			s.col--
		} else if r := p.nextEmojiRow(s.row, -1); r >= 0 {
			s = epCell{r, int(p.rows[r].n) - 1}
		}
	case key.NameDownArrow:
		if r := p.nextEmojiRow(s.row, 1); r >= 0 {
			s = epCell{r, minInt(s.col, int(p.rows[r].n)-1)}
		}
	case key.NameUpArrow:
		if r := p.nextEmojiRow(s.row, -1); r >= 0 {
			s = epCell{r, minInt(s.col, int(p.rows[r].n)-1)}
		}
	}
	p.sel = s
	p.prevID = -1
	p.ensureVisible(gtx, s.row)
}

func (p *EmojiPicker) cellID(c epCell) (int32, bool) {
	if c.row < 0 || c.row >= len(p.rows) {
		return 0, false
	}
	r := p.rows[c.row]
	if r.n == 0 || c.col < 0 || c.col >= int(r.n) {
		return 0, false
	}
	return p.rowIDs(r)[c.col], true
}

// emojiName is the name onPick receives for id (with the skin tone).
func (p *EmojiPicker) emojiName(id int32) string {
	name := p.idx.names[id]
	if p.tone >= 2 && p.idx.hasSkins(id) {
		name += "::skin-tone-" + strconv.Itoa(p.tone)
	}
	return name
}

func (p *EmojiPicker) pickCell(c epCell, keep bool) {
	id, ok := p.cellID(c)
	if !ok {
		return
	}
	name := p.emojiName(id)
	epRecordUse(p.h, name, p.query)
	if !keep {
		p.Dismiss()
	}
	if p.onPick != nil {
		p.onPick(name)
	}
}

func (p *EmojiPicker) enter(keep bool) {
	if _, ok := p.cellID(p.sel); ok {
		p.pickCell(p.sel, keep)
		return
	}
	if p.searching {
		if r := p.nextEmojiRow(-1, 1); r >= 0 {
			p.pickCell(epCell{r, 0}, keep)
		}
	}
}

func (p *EmojiPicker) keys(gtx layout.Context) {
	for !p.done {
		fs := [7]event.Filter{
			key.Filter{Focus: &p.ed, Name: key.NameUpArrow},
			key.Filter{Focus: &p.ed, Name: key.NameDownArrow},
			key.Filter{Focus: &p.ed, Name: key.NameReturn, Optional: key.ModShift},
			key.Filter{Focus: &p.ed, Name: key.NameEnter, Optional: key.ModShift},
			key.Filter{Focus: &p.ed, Name: key.NameTab, Optional: key.ModShift},
			key.Filter{Focus: &p.ed, Name: key.NameLeftArrow},
			key.Filter{Focus: &p.ed, Name: key.NameRightArrow},
		}
		n := 5
		if p.sel.row >= 0 {
			n = 7 // Left/Right move the grid selection once there is one
		}
		e, ok := gtx.Event(fs[:n]...)
		if !ok {
			return
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.NameReturn, key.NameEnter:
			p.enter(ke.Modifiers.Contain(key.ModShift))
		case key.NameTab:
			if p.searching {
				continue
			}
			dir := 1
			if ke.Modifiers.Contain(key.ModShift) {
				dir = -1
			}
			t := p.activeTab()
			for k := 1; k <= epNumGroups; k++ {
				g := ((t+dir*k)%epNumGroups + epNumGroups) % epNumGroups
				if g == 0 || p.groupRow[g] >= 0 {
					p.clickTab(g)
					break
				}
			}
		default:
			p.move(gtx, ke.Name)
		}
	}
}

// position places the card in the window.
func (p *EmojiPicker) position(gtx layout.Context, sz, win image.Point) image.Point {
	m := dp(gtx, 8)
	g := dp(gtx, 4)
	switch p.anchorKind {
	case 1:
		a := image.Rect(p.anchorPt.X, p.anchorPt.Y-g, p.anchorPt.X, p.anchorPt.Y+g)
		return place(a, sz, win, m, p.side)
	case 2:
		a := p.anchorRect
		a.Min.Y -= g
		a.Max.Y += g
		return place(a, sz, win, m, p.side)
	}
	return clampPt(image.Pt((win.X-sz.X)/2, win.Y-sz.Y-dp(gtx, 120)), sz, win, m)
}

// epCard paints the picker card: p-emoji_and_gif_pickers_container's
// rgb(var(--sk_primary_background)) and box-shadow 0 0 0 1px
// var(--dt_color-otl-ter), 0 4px 6px -4px #00000019, 0 10px 15px -3px
// #00000019 (the same literals in dark mode).
func epCard(cl *Colors, _ bool) cardLook {
	sh, ring := rgba(0x000000, .098), cl.TabLine
	return cardLook{fill: cl.EmojiBg, ring: ring, shadow: func(gtx layout.Context, r image.Rectangle, rad int) {
		softShadow(gtx, r.Inset(dp(gtx, 3)), rad, dp(gtx, 10), dp(gtx, 15), sh)
		softShadow(gtx, r.Inset(dp(gtx, 4)), rad, dp(gtx, 4), dp(gtx, 6), sh)
	}}
}

// Layout draws the picker.
func (p *EmojiPicker) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	if p.done {
		return layout.Dimensions{}
	}
	stacked(gtx, p)
	win := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{}

	p.refreshIndex()
	p.scale = gtx.Metric.PxPerDp

	if isTop(p) && !p.toneUI.open && escape(gtx, nil) { // the open skin tone popover takes its own Escape
		p.Dismiss()
		return layout.Dimensions{}
	}
	if p.scrim.Layout(gtx) {
		p.Dismiss()
		return layout.Dimensions{}
	}

	p.win = win
	slackc.RenderLazy(gtx, "EmojiPickerPopover", p.popoverProps, p.layoutPopover)
	return layout.Dimensions{Size: win}
}

// ---- Slack boundaries: EmojiPickerPopover > EmojiPicker ----

var epClass = slackc.Class("p-emoji_picker c-menu p-emoji_picker--style-v2 p-emoji_picker--with_gif_picker", "data-qa", "emoji-picker")

// SetMessage names the message the picker reacts to (EmojiPickerPopover's
// channelId, ts and threadTs; OwlMode reads them).
func (p *EmojiPicker) SetMessage(channelID, ts, threadTS string) {
	p.msgCh, p.msgTS, p.msgThread = channelID, ts, threadTS
}

// popoverProps are EmojiPickerPopover's props; the picker itself is its
// EmojiPicker child, so a plugin's context wrap reaches it.
func (p *EmojiPicker) popoverProps() slackc.Props {
	ch := p.msgCh
	if ch == "" {
		ch = p.gifCh
	}
	var target image.Rectangle
	switch p.anchorKind {
	case 1:
		target = pointRect(p.anchorPt)
	case 2:
		target = p.anchorRect
	}
	ctype := "message_pane"
	if p.msgTS == "" {
		ctype = "composer"
	}
	return slackc.Props{
		"$key":                 p.key,
		"channelId":            ch,
		"ts":                   p.msgTS,
		"threadTs":             p.msgThread,
		"messageContainerType": ctype,
		"targetBounds":         rectProps(target, p.scale),
		"onClose":              p.Dismiss,
		"onOpen":               func() {},
		"children":             slackc.Child("EmojiPicker", slackc.Props{}, p.layoutCard),
	}
}

func (p *EmojiPicker) pickerProps() slackc.Props { return slackc.Props{"$key": p.key} }

func (p *EmojiPicker) layoutPopover(gtx layout.Context, pp slackc.Props) layout.Dimensions {
	return childrenOr(gtx, pp, "EmojiPicker", p.pickerProps, p.layoutCard)
}

// layoutCard is EmojiPicker's native: the card at its window position.
func (p *EmojiPicker) layoutCard(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	th := p.h.Theme()
	cl := colorsOf(th)
	win := p.win
	sz := image.Pt(dp(gtx, epW), dp(gtx, epH))
	if mh := win.Y - 2*dp(gtx, 8); sz.Y > mh {
		sz.Y = maxInt(mh, dp(gtx, epListY+epFootH+epRowH*2))
	}
	pos := p.position(gtx, sz, win)
	p.rect = image.Rectangle{Min: pos, Max: pos.Add(sz)}
	st := epClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{Size: win}
	}
	// the card: .c-menu's rule (a theme's background and border) over
	// the picker's own look
	styledCardLook(gtx, st, epCard(cl, th.Dark), pos, sz, dp(gtx, 6), func(gtx layout.Context) {
		p.layoutBody(gtx, th, cl, sz)
	})
	return layout.Dimensions{Size: win}
}

// layoutBody draws the card at the origin.
func (p *EmojiPicker) layoutBody(gtx layout.Context, th *theme.Theme, cl *Colors, sz image.Point) {
	rad := dp(gtx, 6)
	local := image.Rectangle{Max: sz}
	p.block.Layout(gtx, local)

	for i := range p.topTabs {
		if p.topTabs[i].Clicked(gtx) {
			p.setGifMode(i == 1)
		}
	}
	if p.gifMode {
		cs := clipRRect(gtx, local, rad)
		p.layoutTop(gtx, th, cl, sz.X)
		p.layoutGifPicker(gtx, th, cl, sz)
		cs.Pop()
		return
	}

	p.ensureRows(gtx)
	p.keys(gtx)
	if p.done {
		return
	}
	for {
		e, ok := p.ed.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.ChangeEvent:
			p.setQuery(gtx, p.ed.Text())
		case widget.SubmitEvent:
			p.enter(false)
		}
	}
	if p.done {
		return
	}
	if !p.toneUI.open && (!p.focused || (isTop(p) && !gtx.Focused(&p.ed))) {
		gtx.Execute(key.FocusCmd{Tag: &p.ed})
		p.focused = true
	}

	cs := clipRRect(gtx, local, rad)
	if !p.embedded {
		p.layoutTop(gtx, th, cl, sz.X)
		p.layoutCats(gtx, th, cl)
	}
	at(gtx, image.Pt(dp(gtx, epInputX), dp(gtx, p.inputY())), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(sz.X-2*dp(gtx, epInputX), dp(gtx, epInputH)))
		// c-input_text on --dt_color-base-pry (the card's primary background)
		return fieldOn(gtx, th, &p.ed, nsEmojiPicker.T("Search all emoji"), "", epInputH, 15, cl.EmojiBg)
	})
	foot := dp(gtx, epFootH)
	p.layoutList(gtx, th, cl, image.Rect(0, dp(gtx, p.listY()), sz.X, sz.Y-foot))
	p.layoutFooter(gtx, th, cl, image.Rect(0, sz.Y-foot, sz.X, sz.Y))
	p.layoutTonePopover(gtx, th, cl, sz, sz.Y-foot)
	cs.Pop()
	if tab := p.activeTab(); tab != p.drawnTab && !p.embedded { // hideTabs: no tab row to catch up
		gtx.Execute(op.InvalidateCmd{}) // the tab row was drawn before the list scrolled
	}
}

// layoutTop draws the "Emoji | GIFs" tab row (c-tabs__tab in
// p-emoji_and_gif_pickers_container): 15px bold labels, content-sec
// inactive and content-pry when active or hovered, a 20px icon 4px left of
// the label (epTabIcon), the active tab's 2px --dt_color-theme-base-pry
// underline (a theme's accent; aubergine by default) over the row's 1px
// --dt_color-otl-ter line.
func (p *EmojiPicker) layoutTop(gtx layout.Context, th *theme.Theme, cl *Colors, w int) {
	hh := dp(gtx, epTopH)
	fill(gtx, image.Rect(0, hh-border1(gtx), w, hh), theme.Over(cl.TabLine, cl.EmojiBg))
	half := w / 2
	for i := 0; i < 2; i++ {
		x0, tw := 0, half
		if i == 1 {
			x0, tw = half, w-half
		}
		active := (i == 1) == p.gifMode
		hov := p.topTabs[i].Hovered()
		label, col := nsEmojiPicker.T("Emoji"), cl.TabFg2
		if i == 1 {
			label = nsEmojiPicker.T("GIFs")
		}
		if active || hov {
			col = cl.TabFg
		}
		// the coloured icon is inked #1d1c1d; a hovered inactive one takes
		// the page background in dark mode (Slack's --dark rule)
		ink := cl.TabIcon
		if active {
			ink = rgb(0x1d1c1d)
		} else if hov {
			ink = rgb(0x1d1c1d)
			if th.Dark {
				ink = cl.EmojiBg
			}
		}
		at(gtx, image.Pt(x0, 0), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &p.topTabs[i], nil, func(gtx layout.Context) layout.Dimensions {
				lw := textW(gtx, th, 15, theme.Bold, label)
				ig, gap := dp(gtx, 20), dp(gtx, 4)
				x := (tw - (ig + gap + lw)) / 2
				at(gtx, image.Pt(x, (hh-ig)/2), func(gtx layout.Context) layout.Dimensions {
					return epTabIcon(gtx, i, active || hov, ink, p.tone)
				})
				at(gtx, image.Pt(x+ig+gap, 0), func(gtx layout.Context) layout.Dimensions {
					return vcenter(loose(gtx, image.Pt(lw+1, hh)), hh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Bold, col, label)
					})
				})
				if active {
					fill(gtx, image.Rect(0, hh-dp(gtx, 2), tw, hh), cl.Aubergine)
				}
				return layout.Dimensions{Size: image.Pt(tw, hh)}
			})
		})
	}
}

func (p *EmojiPicker) layoutCats(gtx layout.Context, th *theme.Theme, cl *Colors) {
	for i := 0; i < epNumGroups; i++ {
		if p.cats[i].Clicked(gtx) {
			p.clickTab(i)
		}
	}
	active := p.activeTab()
	p.drawnTab = active
	sz := image.Pt(dp(gtx, epCatW), dp(gtx, epCatH))
	ig := dp(gtx, 20)
	for i := 0; i < epNumGroups; i++ {
		x := dp(gtx, epCatX) + i*dp(gtx, epCatStep)
		at(gtx, image.Pt(x, dp(gtx, epCatY)), func(gtx layout.Context) layout.Dimensions {
			// .p-emoji_picker__group_tab: the active tile is
			// sk_foreground_low_solid (radius 4); hover only darkens the
			// icon to sk_primary_foreground (no tile)
			d := clickBox(gtx, &p.cats[i], func(gtx layout.Context, s image.Point, hovered bool) {
				if i == active {
					rrect(gtx, image.Rectangle{Max: s}, dp(gtx, 4), cl.Tab)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				col := cl.EmojiCatIcon
				if i == active || p.cats[i].Hovered() {
					col = cl.Fg
				}
				at(gtx, image.Pt((sz.X-ig)/2, (sz.Y-ig)/2), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(epTabIcons[i])(gtx, 20, col)
				})
				return layout.Dimensions{Size: sz}
			})
			Tooltip(gtx, th, &p.catTips[i], p.cats[i].Hovered(), d.Size, Below, epGroupLabel(epTabNames[i]), "")
			return d
		})
	}
}

// gridEvents tracks the pointer over the list (handler-local coords).
func (p *EmojiPicker) gridEvents(gtx layout.Context) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &p.gridTag,
			Kinds: pointer.Move | pointer.Drag | pointer.Press | pointer.Release | pointer.Enter | pointer.Leave | pointer.Cancel})
		if !ok {
			return
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Enter, pointer.Move, pointer.Drag:
			p.ptr, p.ptrIn = pe.Position.Round(), true
		case pointer.Leave, pointer.Cancel:
			p.ptrIn, p.pressed = false, false
		case pointer.Press:
			p.ptr, p.ptrIn = pe.Position.Round(), true
			p.press = p.hitTest(gtx, p.ptr)
			p.pressed = p.press.row >= 0 && pe.Buttons&pointer.ButtonPrimary != 0
		case pointer.Release:
			p.ptr = pe.Position.Round()
			if p.pressed {
				p.pressed = false
				if c := p.hitTest(gtx, p.ptr); c == p.press {
					p.pickCell(c, pe.Modifiers.Contain(key.ModShift))
				}
			}
		}
	}
}

// hitTest maps a viewport point to a cell (none over the sticky heading).
func (p *EmojiPicker) hitTest(gtx layout.Context, pt image.Point) epCell {
	if !p.ptrIn || len(p.rows) == 0 || pt.Y < dp(gtx, epStickyH) || pt.Y >= p.viewH {
		return epNoCell
	}
	i := p.rowAt(p.scrollTop() + pt.Y)
	r := p.rows[i]
	x := pt.X - dp(gtx, epCellX)
	if r.n == 0 || x < 0 {
		return epNoCell
	}
	pitch := dp(gtx, epCellPitch)
	col := x / pitch
	if col >= int(r.n) || x-col*pitch >= dp(gtx, epCellW) { // the 1px margin is no cell
		return epNoCell
	}
	return epCell{i, col}
}

func (p *EmojiPicker) layoutList(gtx layout.Context, th *theme.Theme, cl *Colors, r image.Rectangle) {
	vw, vh := r.Dx(), r.Dy()
	p.viewH = vh
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	defer clip.Rect{Max: r.Size()}.Push(gtx.Ops).Pop()

	p.ensureRows(gtx) // a tab click may have cleared the search this frame
	p.gridEvents(gtx)
	if p.done {
		return
	}
	if len(p.rows) == 0 {
		p.hover = epNoCell
		if p.searching {
			p.layoutEmpty(gtx, th, cl, vw, vh)
		}
		return
	}
	p.hover = p.hitTest(gtx, p.ptr)
	lg := gtx
	lg.Constraints = layout.Exact(image.Pt(vw, vh))
	p.list.Layout(lg, len(p.rows), p.rowFn)
	if h := p.hitTest(gtx, p.ptr); h != p.hover {
		p.hover = h // the list scrolled under a resting pointer
		gtx.Execute(op.InvalidateCmd{})
	}
	pos := p.list.Position
	scroll := p.scrollTop()

	// tab lock: a clicked category stays active until the user scrolls
	if p.tabLock >= 0 {
		if p.lockN > 0 {
			p.lockN--
			p.lockPos = [2]int{pos.First, pos.Offset}
		} else if p.lockPos != [2]int{pos.First, pos.Offset} {
			p.tabLock = -1
		}
	}

	// sticky group heading
	hdr, band := dp(gtx, epHdrH), dp(gtx, epStickyH)
	first := minInt(maxInt(pos.First, 0), len(p.rows)-1)
	fr := p.rows[first]
	bandTop := int(fr.y) + p.rowHeight(first) - hdr - scroll // heading row top if first is a heading
	if fr.n != 0 || bandTop < 0 {
		hy := 0
		for k := first + 1; k < len(p.rows) && int(p.rows[k].y)-scroll < vh; k++ {
			if p.rows[k].n == 0 {
				if top := int(p.rows[k].y) + p.rowHeight(k) - hdr - scroll; top < band {
					hy = top - band
				}
				break
			}
		}
		bg := cl.EmojiBg
		bg.A = 242 // rgba(primary background, .95)
		m := dp(gtx, 6)
		fill(gtx, image.Rect(m, hy, vw-m, hy+band), bg)
		p.headingText(gtx, th, cl, int(fr.list), dp(gtx, 20), hy, band, vw)
	}

	// scrollbar thumb (supports_custom_scrollbar: only while hovered)
	if p.totalH > vh && p.ptrIn {
		tb := maxInt(dp(gtx, 36), vh*vh/p.totalH)
		ty := 0
		if span := p.totalH - vh; span > 0 {
			ty = minInt(maxInt(scroll, 0), span) * (vh - tb) / span
		}
		col := theme.WithAlpha(cl.Fg, 133) // .52
		x := vw - dp(gtx, 12)
		rrect(gtx, image.Rect(x, ty, x+dp(gtx, 8), ty+tb), dp(gtx, 4), col)
	}

	// prefetch the rows just below the viewport
	if im := p.h.Images(); im != nil {
		px := dp(gtx, epImgSz)
		last := pos.First + pos.Count
		for k := last; k < len(p.rows) && k < last+4; k++ {
			if rw := p.rows[k]; rw.n > 0 {
				for _, id := range p.rowIDs(rw) {
					im.Prefetch(p.idx.url(id, p.tone), px)
				}
			}
		}
	}

	// pointer tracking above the rows; PassOp keeps wheel scrolling working
	pass := pointer.PassOp{}.Push(gtx.Ops)
	a := clip.Rect{Max: image.Pt(vw, vh)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &p.gridTag)
	if p.hover.row >= 0 {
		pointer.CursorPointer.Add(gtx.Ops)
	}
	a.Pop()
	pass.Pop()
}

// headingText draws group list's name centred in an hb-tall line at
// (x, y).
func (p *EmojiPicker) headingText(gtx layout.Context, th *theme.Theme, cl *Colors, list, x, y, hb, w int) {
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(loose(gtx, image.Pt(w-dp(gtx, 40), hb)), hb, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 13, theme.Bold, cl.Fg, epGroupLabel(epGroupNames[list]))
		})
	})
}

// layoutRow draws list row i (layout.List callback).
func (p *EmojiPicker) layoutRow(gtx layout.Context, i int) layout.Dimensions {
	th := p.h.Theme()
	cl := colorsOf(th)
	r := p.rows[i]
	w := gtx.Constraints.Max.X
	h := p.rowHeight(i)
	if r.n == 0 {
		// .p-emoji_picker__heading: margin 0 6px in the row's 7+8px
		// padding, line-height 1.5rem at the 27px row's top
		p.headingText(gtx, th, cl, int(r.list), dp(gtx, 21), h-dp(gtx, epHdrH), dp(gtx, 24), w)
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
	cw, ch, isz := dp(gtx, epCellW), dp(gtx, epCellH), dp(gtx, epImgSz)
	x0, y0, rad := dp(gtx, epCellX), dp(gtx, 1), dp(gtx, 6)
	im := p.h.Images()
	// one tinted cell: the hovered one, else the keyboard selection
	lit := p.hover
	if lit.row < 0 {
		lit = p.sel
	}
	pitch := dp(gtx, epCellPitch)
	for j, id := range p.rowIDs(r) {
		x := x0 + j*pitch
		cell := image.Rect(x, y0, x+cw, y0+ch)
		if lit.row == i && lit.col == j {
			rrect(gtx, cell, rad, epCellTint(int(r.si)*epCols+j))
		}
		if img, ok := im.Get(p.idx.url(id, p.tone), isz); ok {
			// .p-emoji_picker__list_item{padding:.25rem 0 .2rem}: the
			// image sits 4px below the cell's top (.c-emoji vertical-align
			// top), not centred in its 32px
			t := op.Offset(image.Pt(x+(cw-isz)/2, y0+dp(gtx, 4))).Push(gtx.Ops)
			containImage(gtx, img, image.Pt(isz, isz))
			t.Pop()
		}
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}

func (p *EmojiPicker) layoutEmpty(gtx layout.Context, th *theme.Theme, cl *Colors, w, h int) {
	// EmojiListNoResults (Zu56): searchGifsInstead is passed inside the
	// Emoji | GIFs tabs (Y5Hl's noResultsSearchForGifWithQuery), so the
	// button shows unless the picker is embedded without them
	var btn *widget.Clickable
	if !p.embedded {
		btn = &p.gifsBtn
		if btn.Clicked(gtx) {
			p.searchGifsInstead(gtx)
			return
		}
	}
	p.noResults(gtx, th, cl, w, h, p.emptyMsg, nsGifPicker.T("Like your smarts, your search is unmatched."), btn, nsGifPicker.T("Search GIFs Instead"))
}

// searchGifsInstead is EmojiListNoResults' button (Y5Hl's
// noResultsSearchForGifWithQuery): the GIFs tab, searching the emoji
// query.
func (p *EmojiPicker) searchGifsInstead(gtx layout.Context) {
	q := p.query
	p.setGifMode(true)
	g := &p.gif
	g.ed.SetText(q)
	n := utf8.RuneCountInString(q)
	g.ed.SetCaret(n, n)
	g.typed, g.due = q, gtx.Now
	gtx.Execute(op.InvalidateCmd{})
}

// noResults is the pickers' empty search (EmojiListNoResults, Zu56; the
// GIF picker's, xVsA): .p-emoji_picker_no_results / .p-gif_picker__no_results,
// a column centred in the list with 16px gaps: the face_with_monocle emoji
// (vbPY's default c-emoji__medium, 22px), the header (15px/1.46668,
// Black) over the subtext (15px), centred in the picker's text colour,
// and the medium outline button when btn is set.
func (p *EmojiPicker) noResults(gtx layout.Context, th *theme.Theme, cl *Colors, w, h int, header, sub string, btn *widget.Clickable, label string) {
	gap, isz, lh := dp(gtx, 16), dp(gtx, 22), dp(gtx, 22)
	var bcall op.CallOp
	var bd layout.Dimensions
	if btn != nil {
		m := op.Record(gtx.Ops)
		bd = button(loose(gtx, image.Pt(w, dp(gtx, 36))), th, btn, BtnOutline, BtnMedium, label, "", 0, false)
		bcall = m.Stop()
	}
	textH := lh
	if sub != "" {
		textH += lh
	}
	total := isz + gap + textH
	if btn != nil {
		total += gap + bd.Size.Y
	}
	y := (h - total) / 2
	if id, ok := p.idx.byName["face_with_monocle"]; ok && p.h.Images() != nil {
		if img, ok := p.h.Images().Get(p.idx.url(id, 0), isz); ok {
			t := op.Offset(image.Pt((w-isz)/2, y)).Push(gtx.Ops)
			containImage(gtx, img, image.Pt(isz, isz))
			t.Pop()
		}
	}
	y += isz + gap
	line := func(s string, wt font.Weight) {
		tw := minInt(textW(gtx, th, 15, wt, s), w)
		at(gtx, image.Pt((w-tw)/2, y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(loose(gtx, image.Pt(tw+1, lh)), lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, wt, cl.Fg, s)
			})
		})
		y += lh
	}
	line(header, theme.Black)
	if sub != "" {
		line(sub, theme.Regular)
	}
	if btn != nil {
		t := op.Offset(image.Pt((w-bd.Size.X)/2, y+gap)).Push(gtx.Ops)
		bcall.Add(gtx.Ops)
		t.Pop()
	}
}

// epTints are .p-emoji_picker__list_item[data-color-index] :hover and
// .key_selection backgrounds (no dark-mode override).
var epTints = [6]color.NRGBA{
	rgba(0x9bdf58, .7), rgba(0x95d3fe, .7), rgba(0xf7e922, .7),
	rgba(0xeea6fc, .7), rgba(0xffd58f, .7), rgba(0xd3d1ff, .7),
}

// epCellTint is EmojiListEmojiItem's dataColorIndex: index % 6, where
// index is 9 × the screen row + the column.
func epCellTint(index int) color.NRGBA { return epTints[index%6] }

// drawEmoji draws id at size dp with the current skin tone.
func (p *EmojiPicker) drawEmoji(gtx layout.Context, id int32, tone int, size float32) layout.Dimensions {
	px := dp(gtx, size)
	sz := image.Pt(px, px)
	if img, ok := p.h.Images().Get(p.idx.url(id, tone), px); ok {
		return containImage(gtx, img, sz)
	}
	return layout.Dimensions{Size: sz}
}

func (p *EmojiPicker) layoutFooter(gtx layout.Context, th *theme.Theme, cl *Colors, r image.Rectangle) {
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	w, h := r.Dx(), r.Dy()
	// .p-emoji_picker__footer: sk_foreground_min_solid, border-top 1px
	// #00000026 in both modes
	fill(gtx, image.Rect(0, 0, w, h), cl.EmojiFoot)
	// composited as the browser does (in sRGB): Gio's own alpha blend is
	// in linear light and lands lighter (231 for Slack's 211 over #f8f8f8)
	fill(gtx, image.Rect(0, 0, w, border1(gtx)), theme.Over(rgba(0x000000, .15), cl.EmojiFoot))

	right := p.layoutTone(gtx, th, cl, w, h)

	id, ok := p.cellID(p.hover)
	if !ok {
		id, ok = p.cellID(p.sel)
	}
	if !ok {
		p.prevID = -1
		if p.addBtn.Clicked(gtx) {
			p.Dismiss()
			p.h.ShowOverlay(NewAddEmojiModal(p.h))
			return
		}
		// .p-emoji_picker__add_custom_emoji: bottom 14.5px, left 14px
		at(gtx, image.Pt(dp(gtx, 14), h-dp(gtx, 14.5)-dp(gtx, 28)), func(gtx layout.Context) layout.Dimensions {
			return Button(gtx, th, &p.addBtn, BtnOutline, BtnSmall, nsEmojiPicker.T("Add Emoji"), "", 85)
		})
		return
	}
	if id != p.prevID {
		p.prevID = id
		names := p.idx.allNames(id)
		p.prevName = names[0]
		var b strings.Builder
		for k, n := range names {
			if k > 0 {
				b.WriteByte(' ')
			}
			b.WriteByte(':')
			b.WriteString(n)
			b.WriteByte(':')
		}
		p.prevAlias = b.String()
	}
	// .p-emoji_picker__preview (padding .3rem 0 0 .7rem): the 32px
	// c-emoji__large in preview_img (padding .7rem .5rem 0 .1rem) at
	// (12.8, 17.1); preview_text (14.4/700 sk_foreground_max_solid) at
	// x 57.2, its name line at 13.5 and the aliases (12.8/400) at 28.9.
	at(gtx, image.Pt(dp(gtx, 12.8), dp(gtx, 17.1)), func(gtx layout.Context) layout.Dimensions {
		return p.drawEmoji(gtx, id, p.tone, 32)
	})
	tx := dp(gtx, 57.2)
	tw := maxInt(w-right-tx-dp(gtx, 8), dp(gtx, 20))
	at(gtx, image.Pt(tx, dp(gtx, 13.5)), func(gtx layout.Context) layout.Dimensions {
		return text(loose(gtx, image.Pt(tw, dp(gtx, 20))), th, 14.4, theme.Bold, cl.Muted, p.prevName)
	})
	at(gtx, image.Pt(tx, dp(gtx, 28.9)), func(gtx layout.Context) layout.Dimensions {
		return text(loose(gtx, image.Pt(tw, dp(gtx, 18))), th, 12.8, theme.Regular, cl.Muted, p.prevAlias)
	})
}

// handID is the hand emoji used by the skin tone controls.
func (p *EmojiPicker) handID() (int32, bool) { return p.idx.lookup("hand") }
