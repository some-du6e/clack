package popups

// The omniswitcher's quick filter row (gantry-v2-async-client-boot-shared
// oMYD QuickFilterRow, wired by rs63 OmniswitcherContent, build
// 4d0c78085232).
//
// The filter button (QuickFilterMenu: "Show filters" / "Hide filters",
// icon refine 20) toggles the row; the row also shows by itself the first
// time the box gets a filter token or a mention while nothing is selected.
// It hides while a completer's menu or a date picker is open.
//
// The row (.rowContainer__MF87N: gap 8, padding 0 16px 12px, under the
// input in .c-search__input_wrapper) holds outline FilterPills
// (.p-explorer_controls__button.c-filter-pill--quick-filter: 28 tall,
// padding 0 11, radius 8, otl-ter border, content-sec 12px bold, a 14px
// icon 4px before the label; active: content-hgl-1 with white text): In
// this channel (In this DM) while a conversation is open, From me,
// Includes me, Has a link, Threads only, In Activity, Last 7 days. A pill
// toggles its filter: on, its token and a space go at the end of the box;
// off, the box loses the modifier's tokens for that value (and a mention
// typed after "from:" with its "from:"). Last 7 days is
// after:<a week ago> before:<tomorrow> (getDatePresetStringValuesMap).
// The pills that don't fit (useOverflowMeasurement) move to the + menu's
// "Quick filters" section; its "More options" type an incomplete
// modifier for the completers: From someone (from:), Includes someone
// (with:), In channel (in:), Message has (has:), Message is (is:), File
// type (type:), My reaction (hasmy:), and Date: with
// omni_date_quick_filters a submenu (On, Before, After, Range), else
// during:.

import (
	"image"
	"slices"
	"strings"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's tokens the row and the pills paint with (slack-kit-gantry-v2
// styles: light, dark).
var (
	tokCtrPry      = chipTok{"var(--dt_color-ctr-pry)", rgb(0xffffff), rgb(0x212428)}
	tokCtrPryHover = chipTok{"var(--dt_color-ctr-pry-hover)", rgba8(0x454447, 0x0f), rgba8(0xf8f8f8, 0x14)}
	tokOtlTer      = chipTok{"var(--dt_color-otl-ter)", rgba8(0x5e5d60, 0x21), rgba8(0x797c81, 0x4d)}
	tokContentSec  = chipTok{"var(--dt_color-content-sec)", rgb(0x454447), rgb(0xb9babd)}
	tokBaseSec     = chipTok{"var(--dt_color-base-sec)", rgb(0xf8f8f8), rgb(0x1a1d21)}
	tokBasePry     = chipTok{"var(--dt_color-base-pry)", rgb(0xffffff), rgb(0x1a1d21)}
	tokSurfHgl3    = chipTok{"var(--dt_color-surf-hgl-3)", rgba8(0xffc600, 0x2e), rgba8(0xa07300, 0x2e)}
	tokContentHgl3 = chipTok{"var(--dt_color-content-hgl-3)", rgb(0x6b5000), rgb(0xdea700)}
	tokSurfInv     = chipTok{"var(--dt_color-surf-inv)", rgba8(0xffffff, 0x2e), rgba8(0xffffff, 0x2e)}
)

// srQF is one of the row's buttons (QuickFilterRow's $ and Last 7 days).
type srQF struct {
	key, label, icon string
	mod, entity      string // the filter's modifier and entityId
	display          string // displayName
	entityType       string // "user", "channel" or "static" ("" from the modifier)
	date             bool   // QuickFilterDateButton
	clog             string // clogValue
}

// srQFState is the row's state between frames.
type srQFState struct {
	show   bool // K: isFilterRowVisible
	hadTok bool // eI: the box had a token or a mention
	btns   []widget.Clickable
	more   widget.Clickable
	moreR  image.Rectangle // the + button, window coords
	shown  int             // the buttons that fit (visibleCount)
	tip    TipState        // the filter button's tooltip
}

// quickFilters is the row's buttons in order.
func (p *SearchPopover) quickFilters() []srQF {
	var out []srQF
	c := p.h.Core()
	self, me := "", "me"
	if c != nil {
		u := c.Self()
		self = u.ID
		// H?.profile?.display_name || H?.real_name || "me"
		switch {
		case u.DisplayName != "":
			me = u.DisplayName
		case u.RealName != "":
			me = u.RealName
		}
	}
	// usePrimaryViewId, when it looks like a conversation's
	if cur := srCurrentPlace(); c != nil && srLooksLikeChannelID(cur) {
		if cv, ok := c.Conv(cur); ok {
			f := srQF{key: "in-this-channel", label: "In this channel", icon: "channel", mod: "in", entity: cur,
				display: convTitle(p.h, cv), entityType: "channel", clog: "in_this_channel"}
			if cv.Kind == "im" || cv.Kind == "mpim" {
				f.label = "In this DM"
			}
			if cv.Kind == "im" && cv.User != "" {
				f.entity = cv.User
			}
			out = append(out, f)
		}
	}
	return append(out,
		srQF{key: "from-me", label: "From me", icon: "user", mod: "from", entity: self, display: me, clog: "me"},
		srQF{key: "includes-me", label: "Includes me", icon: "user", mod: "with", entity: self, display: me, clog: "me"},
		srQF{key: "has-link", label: "Has a link", icon: "link", mod: "has", entity: "link", display: "link"},
		srQF{key: "threads-only", label: "Threads only", icon: "threads", mod: "is", entity: "thread", display: "thread"},
		srQF{key: "in-activity", label: "In Activity", icon: "notifications", mod: "in", entity: "activity",
			display: "activity", clog: "in_activity"},
		srQF{key: "last-7-days", label: "Last 7 days", icon: "calendar", date: true})
}

// srQFUser reports a modifier whose values are people (fsda z0).
func srQFUser(mod string) bool { return srACMemberTypes[strings.TrimPrefix(mod, "-")] }

// srQFToken is the token a button or a menu item inserts: label is
// "mod:" and the display name ("@" before a person's on the row).
func (p *SearchPopover) srQFToken(f srQF, label string) srToken {
	t := srToken{label: label, mod: f.mod, entity: f.entity}
	typ := strings.TrimPrefix(f.mod, "-")
	switch {
	case srQFUser(f.mod):
		t.req = f.mod + ":<@" + f.entity + ">"
	case typ == "in" && f.entity == "activity":
		t.req = f.mod + ":activity"
	case typ == "in" && strings.HasPrefix(f.entity, "U"):
		t.req = f.mod + ":<@" + f.entity + ">" // a DM: its person (InQuerySuggestionToken)
	case typ == "in" && srIsMpim(p.h, f.entity):
		t.req = f.mod + ":<#" + f.entity + ">"
	case typ == "in":
		name := f.display
		if c := p.h.Core(); c != nil {
			if n := c.ConvRef(f.entity).Name; n != "" {
				name = n
			}
		}
		t.req = f.mod + ":<#" + f.entity + "|" + name + ">"
	default:
		t.req = label
	}
	// getSearchFilterIconName (no isPrivate: the row doesn't pass one)
	kind := srAIValue
	if srQFUser(f.mod) || typ == "in" && strings.HasPrefix(f.entity, "U") {
		kind = srAIMember
	} else if typ == "in" && f.entity != "activity" {
		kind = srAIChannel
	}
	t.icon = srFilterIcon(typ, srACItem{kind: kind, id: f.entity})
	return t
}

// srIsMpim reports a group DM's id.
func srIsMpim(h Host, id string) bool {
	if c := h.Core(); c != nil {
		if cv, ok := c.Conv(id); ok {
			return cv.Kind == "mpim"
		}
	}
	return false
}

// qfSelected is useQuickFilterState's selectedIds: the values of mod's
// tokens, and the mentions typed right after "mod:".
func (p *SearchPopover) qfSelected(mod string) []string {
	c := mod + ":"
	ps := p.box().pieces()
	var ids []string
	for i, pc := range ps {
		switch {
		case pc.tok != nil && !pc.tok.mention:
			if pc.tok.mod == mod {
				ids = append(ids, pc.tok.entity)
			}
		case pc.tok != nil && i > 0 && ps[i-1].tok == nil && strings.HasSuffix(ps[i-1].text, c):
			ids = append(ids, pc.tok.entity)
		}
	}
	return ids
}

// qfRemove is useQuickFilterState's u: the box without mod's tokens (for
// entity only, unless it is "") and the mentions typed after "mod:", that
// text too.
func (p *SearchPopover) qfRemove(mod, entity string) {
	c := mod + ":"
	ps := p.box().pieces()
	drop := make([]bool, len(ps))
	for i, pc := range ps {
		switch {
		case pc.tok != nil && !pc.tok.mention:
			if pc.tok.mod == mod && (entity == "" || pc.tok.entity == entity) {
				drop[i] = true
			}
		case pc.tok != nil && i > 0 && ps[i-1].tok == nil && strings.HasSuffix(ps[i-1].text, c) &&
			(entity == "" || pc.tok.entity == entity):
			drop[i], drop[i-1] = true, true
		}
	}
	var out []srPiece
	for i, pc := range ps {
		if !drop[i] {
			out = append(out, pc)
			continue
		}
		if i+1 < len(ps) && drop[i+1] && pc.tok == nil && strings.HasSuffix(pc.text, c) {
			if t := strings.TrimSuffix(pc.text, c); t != "" {
				out = append(out, srText(t))
			}
		}
	}
	p.filterInsert(out, true, false, -1)
}

// qfToggle is a button's click (handleSelectionToggle).
func (p *SearchPopover) qfToggle(f srQF) {
	if f.date {
		p.qfToggleWeek()
		return
	}
	if slices.Contains(p.qfSelected(f.mod), f.entity) {
		p.qfRemove(f.mod, f.entity)
		return
	}
	user := f.entityType == "user" || f.entityType == "" && srQFUser(f.mod)
	label := f.mod + ":" + f.display
	if user {
		label = f.mod + ":@" + f.display
	}
	p.filterInsert([]srPiece{srTokPiece(p.srQFToken(f, label)), srSpace()}, false, false, -1)
}

// srDatePresetWeek is getDatePresetStringValuesMap's LAST_7_DAYS: after a
// week ago, before tomorrow.
func srDatePresetWeek(now time.Time) (after, before string) {
	return now.AddDate(0, 0, -7).Format(srDateFormat), now.AddDate(0, 0, 1).Format(srDateFormat)
}

// srWeekTokens is createLastWeekOps.
func srWeekTokens(now time.Time) []srPiece {
	a, b := srDatePresetWeek(now)
	return []srPiece{srTokPiece(srDateToken("after", a, false)), srSpace(), srTokPiece(srDateToken("before", b, false)), srSpace()}
}

// qfWeekActive is QuickFilterDateButton's isActive: both of the preset's
// dates in the box, as tokens or typed.
func (p *SearchPopover) qfWeekActive(now time.Time) bool {
	a, b := srDatePresetWeek(now)
	ra, rb := "after:"+a, "before:"+b
	hasA, hasB := false, false
	for _, pc := range p.box().pieces() {
		switch {
		case pc.tok != nil && !pc.tok.mention:
			hasA = hasA || pc.tok.mod == "after" && pc.tok.entity == a
			hasB = hasB || pc.tok.mod == "before" && pc.tok.entity == b
		case pc.tok == nil:
			hasA = hasA || strings.Contains(pc.text, ra)
			hasB = hasB || strings.Contains(pc.text, rb)
		}
	}
	return hasA && hasB
}

// qfToggleWeek is QuickFilterDateButton's click: off, the box loses the
// preset's tokens and typed words; on, they go at the end.
func (p *SearchPopover) qfToggleWeek() {
	now := time.Now()
	if !p.qfWeekActive(now) {
		p.filterInsert(srWeekTokens(now), false, false, -1)
		return
	}
	a, b := srDatePresetWeek(now)
	ra, rb := "after:"+a, "before:"+b
	var out []srPiece
	for _, pc := range p.box().pieces() {
		if pc.tok != nil {
			if !pc.tok.mention && (pc.tok.mod == "after" && pc.tok.entity == a || pc.tok.mod == "before" && pc.tok.entity == b) {
				continue
			}
			out = append(out, pc)
			continue
		}
		var keep []string
		for _, w := range strings.Split(pc.text, " ") {
			if w != ra && w != rb {
				keep = append(keep, w)
			}
		}
		if t := strings.Join(keep, " "); t != "" {
			out = append(out, srText(t))
		}
	}
	p.filterInsert(out, true, false, -1)
}

// qfActive reports a button's filter in the box.
func (p *SearchPopover) qfActive(f srQF, now time.Time) bool {
	if f.date {
		return p.qfWeekActive(now)
	}
	return slices.Contains(p.qfSelected(f.mod), f.entity)
}

// qfAutoShow is OmniswitcherContent's effect: with nothing selected, the
// row shows when the box gets its first token or mention.
func (p *SearchPopover) qfAutoShow() {
	if p.picked {
		return
	}
	has := len(p.tokens) > 0
	if has && !p.qf.hadTok {
		p.qf.show = true
	}
	p.qf.hadTok = has
}

// qfVisible is eM: the row shows while it is toggled on and no completer
// menu or date picker is open.
func (p *SearchPopover) qfVisible() bool {
	return p.qf.show && !p.ac.open && !p.dpShown()
}

// qfRowH is the row's height under the input (the 28px pills and 12px of
// padding), 0 while it hides.
func (p *SearchPopover) qfRowH(gtx layout.Context) int {
	if !p.qfVisible() {
		return 0
	}
	return dp(gtx, 40)
}

// qfToggleRow is the filter button's click (eN): the row toggles and the
// box keeps the focus.
func (p *SearchPopover) qfToggleRow() {
	p.qf.show = !p.qf.show
	p.refocus = true
}

// srQFPillW is a pill's width: 1px borders, padding 0 11, the 14px icon
// and its 4px gap, the 12px bold label; at least 32.
func srQFPillW(gtx layout.Context, th *theme.Theme, label string, icon bool) int {
	w := 2*max(1, dp(gtx, 1)) + 2*dp(gtx, 11) + textW(gtx, th, 12, theme.Bold, label)
	if icon {
		w += dp(gtx, 14) + dp(gtx, 4)
	}
	return max(w, dp(gtx, 32))
}

// qfLayout draws the row at the card's (0, y), w wide.
func (p *SearchPopover) qfLayout(gtx layout.Context, th *theme.Theme, cl *Colors, y, w int) {
	fs := p.quickFilters()
	if len(p.qf.btns) < len(fs) {
		p.qf.btns = make([]widget.Clickable, len(fs))
	}
	now := gtx.Now
	for i := range fs {
		if p.qf.btns[i].Clicked(gtx) {
			p.qfToggle(fs[i])
			p.h.Invalidate()
		}
	}
	if p.qf.more.Clicked(gtx) {
		p.qfOpenMenu(fs)
	}
	// useOverflowMeasurement: the + button and the padding, then each
	// pill with the 8px gap while they fit
	pad, gap, plus := dp(gtx, 16), dp(gtx, 8), dp(gtx, 28)
	used := plus + 2*pad
	shown := 0
	widths := make([]int, len(fs))
	for i, f := range fs {
		widths[i] = srQFPillW(gtx, th, f.label, true)
		if used+widths[i]+gap > w {
			break
		}
		used += widths[i] + gap
		shown++
	}
	p.qf.shown = shown
	h := dp(gtx, 28)
	x := pad
	for i := 0; i < shown; i++ {
		f := fs[i]
		at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			return p.qfPill(gtx, th, &p.qf.btns[i], image.Pt(widths[i], h), f.icon, f.label, p.qfActive(f, now))
		})
		x += widths[i] + gap
	}
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		return p.qfPill(gtx, th, &p.qf.more, image.Pt(plus, h), "plus", "", false)
	})
	p.qf.moreR = image.Rectangle{Min: image.Pt(x, y), Max: image.Pt(x+plus, y+h)}.Add(p.rect.Min)
}

// qfPill draws one FilterPill (isQuickFilter) sz big: an icon and a
// label, or the + button's 16px plus alone.
func (p *SearchPopover) qfPill(gtx layout.Context, th *theme.Theme, btn *widget.Clickable, sz image.Point, ic, label string, active bool) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		box := image.Rectangle{Max: sz}
		rad, bw := dp(gtx, 8), max(1, dp(gtx, 1))
		bg, edge, fg := tokCtrPry.of(th), tokOtlTer.of(th), tokContentSec.of(th)
		if btn.Hovered() {
			// .c-button--outline:hover's base-sec under
			// .p-explorer_controls__button:hover's ctr-pry-hover
			bg = theme.Over(tokCtrPryHover.of(th), tokBaseSec.of(th))
		}
		if active {
			bg, edge, fg = tokContentHgl1.of(th), tokContentHgl1.of(th), rgb(0xffffff)
			if btn.Hovered() {
				bg = theme.Over(tokCtrPryHover.of(th), bg)
			}
		}
		// the border paints over the card (border-box)
		rrect(gtx, box, rad, theme.Over(edge, colorsOf(th).Bg))
		rrect(gtx, box.Inset(bw), rad-bw, theme.Over(bg, colorsOf(th).Bg))
		if label == "" {
			is := dp(gtx, 16)
			at(gtx, image.Pt((sz.X-is)/2, (sz.Y-is)/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, ic, 16, fg)
			})
		} else {
			x := bw + dp(gtx, 11)
			is := dp(gtx, 14)
			at(gtx, image.Pt(x, (sz.Y-is)/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, ic, 14, fg)
			})
			x += is + dp(gtx, 4)
			at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 12, theme.Bold, fg, label)
				})
			})
		}
		pointerCursor(gtx, sz)
		return layout.Dimensions{Size: sz}
	})
}

// qfMenuItems is the + menu's template (en): the buttons that didn't fit
// under "Quick filters", then "More options".
func (p *SearchPopover) qfMenuItems(fs []srQF) []MenuItem {
	var quick []MenuItem
	for i := p.qf.shown; i < len(fs); i++ {
		f := fs[i]
		if f.date {
			quick = append(quick, MenuItem{Label: f.label, Icon: "calendar", Do: func() {
				p.filterInsert(srWeekTokens(time.Now()), false, false, -1)
			}})
			continue
		}
		quick = append(quick, MenuItem{Label: f.label, Icon: f.icon, Do: func() { p.qfInsertValue(f) }})
	}
	incomplete := func(label, ic, mod string) MenuItem {
		return MenuItem{Label: label, Icon: ic, Hint: mod + ":", Do: func() {
			p.filterInsert([]srPiece{srText(mod + ":")}, false, true, -1)
		}}
	}
	more := []MenuItem{
		incomplete("From someone", "user", "from"),
		incomplete("Includes someone", "user", "with"),
		incomplete("In channel", "channel", "in"),
		incomplete("Message has", "message", "has"),
		incomplete("Message is", "message", "is"),
	}
	date := p.dpOn()
	if !date {
		more = append(more, incomplete("Date", "calendar", "during"))
	}
	more = append(more, incomplete("File type", "files", "type"), incomplete("My reaction", "reaction", "hasmy"))
	if date {
		sub := func(label, mod string) MenuItem {
			it := incomplete(label, "", mod)
			return it
		}
		more = append(more, MenuItem{Label: "Date", Icon: "calendar", Sub: []MenuItem{
			sub("On", "on"), sub("Before", "before"), sub("After", "after"),
			{Label: "Range", Hint: "after: before:", Do: p.enterRange},
		}})
	}
	if len(quick) == 0 {
		return more
	}
	out := append([]MenuItem{{Header: "Quick filters"}}, quick...)
	out = append(out, Sep(), MenuItem{Header: "More options"})
	return append(out, more...)
}

// qfInsertValue is the + menu's quick filter item (createFilterClick
// with a value): the token "mod:" and the display name, with the mention
// look when it names the viewer, and a space.
func (p *SearchPopover) qfInsertValue(f srQF) {
	t := p.srQFToken(f, f.mod+":"+f.display)
	if c := p.h.Core(); c != nil && f.entity != "" && f.entity == c.Self().ID {
		t.self = true // shouldHighlightMention
	}
	p.filterInsert([]srPiece{srTokPiece(t), srSpace()}, false, false, -1)
}

// qfOpenMenu opens the + menu (Popover bottom-left, offsetY 8).
func (p *SearchPopover) qfOpenMenu(fs []srQF) {
	p.h.ShowOverlay(NewMenuOpts(p.h, p.qfMenuItems(fs), MenuOpts{Anchor: p.qf.moreR, Popover: "bottom-left", OffsetY: 8,
		OnClose: func() { p.refocus = true }}))
}

// filterButton draws QuickFilterMenu at (x, y) of the input row: 24px,
// radius 8, the refine icon 20 in content-sec (content-pry on hover);
// while the row shows, base-pry on content-hgl-1. Its tooltip is "Show
// filters" or "Hide filters" (300 ms, below).
func (p *SearchPopover) filterButton(gtx layout.Context, th *theme.Theme, x, y int) {
	on := p.qf.show
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		s := dp(gtx, 24)
		d := p.filter.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			box := image.Rectangle{Max: image.Pt(s, s)}
			fg := tokContentSec.of(th)
			if p.filter.Hovered() {
				fg = tokContentPry.of(th)
			}
			if on {
				rrect(gtx, box, dp(gtx, 8), tokContentHgl1.of(th))
				fg = tokBasePry.of(th)
			}
			is := dp(gtx, 20)
			at(gtx, image.Pt((s-is)/2, (s-is)/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "filter", 20, fg)
			})
			defer clip.Rect(box).Push(gtx.Ops).Pop()
			pointer.CursorPointer.Add(gtx.Ops)
			return layout.Dimensions{Size: box.Max}
		})
		label := "Show filters"
		if on {
			label = "Hide filters"
		}
		p.qf.tip.Delay = 300 * time.Millisecond
		Tooltip(gtx, th, &p.qf.tip, p.filter.Hovered(), d.Size, Below, label, "")
		return d
	})
}
