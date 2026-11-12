package views

import (
	"image"
	"slices"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The past huddles' filters: FilterRow (Igs2) in .container__PBWFF
// (centred, at most 968 wide, padding 0 20px 20px, gap 8). Each change
// sets the huddleHistory slice's filter and runs filterHuddleHistory
// (huddles.history with transformFiltersForApi's args).
//
// huddle_page_enhancements off (the account's group): FilterHuddleType
// (SearchFilterSingleSelect: "All huddles" / "Missed huddles", a radio
// menu 280 wide), FilterHuddleWithUsers ("With", "With name", "With N
// people"), FilterExcludeOthers once someone is picked ("Include only
// this person"), FilterHuddleIn ("In", "In #channel", "In N places"),
// each a FilterPill (FS4w) like the directories'. Their popovers are
// Slack's SearchFilterWith / SearchFilterIn: the picks, "Clear all" past
// one, and "Suggestions" (the first five people and four conversations of
// the loaded history); Slack's search box in them isn't drawn.
//
// huddle_page_enhancements on: the All / Missed pills, then at the right
// In, With, Include only, Has ("Meeting notes", "Thread", "Files &
// links") and "Reset filters". Its Date picker (SearchFilterDate's
// presets and calendar) isn't drawn. A row too narrow for its pills folds
// into Slack's "Filters (N)" and FiltersModal, which Clack doesn't draw.

// withSuggestions is useFilterHuddleWithUsers (yd0k): the first five
// people (not you) across the loaded history's participant_history.
func (v *Huddles) withSuggestions() []string {
	v.mu.Lock()
	hs := append([]core.Huddle(nil), v.hist.huddles...)
	v.mu.Unlock()
	self := v.selfID()
	var out []string
	for _, h := range hs {
		for _, id := range h.History {
			if id != self && !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// inSuggestions is useFilterHuddleInChannels (ZO7s): the first four
// conversations (one per channel) of the loaded history whose
// channel_type is mpim, private or public and that you can see.
func (v *Huddles) inSuggestions() []string {
	v.mu.Lock()
	hs := append([]core.Huddle(nil), v.hist.huddles...)
	v.mu.Unlock()
	var out []string
	for _, h := range hs {
		ch := roomChannel(h)
		if ch == "" || slices.Contains(out, ch) {
			continue
		}
		switch h.ChannelType {
		case "mpim", "private", "public":
		default:
			continue
		}
		v.mu.Lock()
		gone := v.missing[ch]
		v.mu.Unlock()
		if gone {
			continue
		}
		out = append(out, ch)
		if len(out) == 4 {
			break
		}
	}
	return out
}

func toggleIn(xs []string, x string) []string {
	if slices.Contains(xs, x) {
		return slices.DeleteFunc(slices.Clone(xs), func(y string) bool { return y == x })
	}
	return append(slices.Clone(xs), x)
}

// filter actions (jNSy's setters, then filterHuddleHistory)
func (v *Huddles) setType(t string) {
	if t == v.filters.typ {
		return
	}
	v.filters.typ = t
	v.refetch()
}

func (v *Huddles) toggleWith(id string) {
	v.filters.with = toggleIn(v.filters.with, id)
	if len(v.filters.with) == 0 {
		v.filters.usersOnly = false // setHuddleHistoryFilterWith: usersOnly needs someone
	}
	v.refetch()
}

func (v *Huddles) toggleInChannel(id string) {
	v.filters.in = toggleIn(v.filters.in, id)
	v.refetch()
}

func (v *Huddles) toggleHas(k string) {
	v.filters.has = toggleIn(v.filters.has, k)
	v.refetch()
}

// resetFilters is FiltersResetButton: setClearHuddleHistoryFilters and
// the fetch.
func (v *Huddles) resetFilters() {
	v.filters = hdFilters{typ: "all"}
	v.refetch()
}

// hdHasOptions is getHasFilterOptions (aQic tJ) with their icons (iA).
var hdHasOptions = [...]struct{ key, label, icon string }{
	{"meeting_notes", "Meeting notes", "ai-summary"},
	{"thread", "Thread", "threads"},
	{"files_and_links", "Files & links", "canvas-browser"},
}

func (v *Huddles) layoutFilters(gtx C) D {
	w := gtx.Constraints.Max.X
	box := min(w, gtx.Dp(968))
	x0 := (w-box)/2 + gtx.Dp(20)
	cw := max(0, box-2*gtx.Dp(20))
	enh := v.enhancements()
	f := &v.filters

	typeC, withC, onlyC, inC := v.clk("f:type"), v.clk("f:with"), v.clk("f:only"), v.clk("f:in")
	allC, missedC, hasC, resetC := v.clk("f:all"), v.clk("f:missed"), v.clk("f:has"), v.clk("f:reset")
	if typeC.Clicked(gtx) {
		v.openTypeMenu(gtx, typeC)
	}
	if allC.Clicked(gtx) {
		v.setType("all")
	}
	if missedC.Clicked(gtx) {
		v.setType("missed")
	}
	if withC.Clicked(gtx) {
		v.openWithMenu(gtx, withC)
	}
	if onlyC.Clicked(gtx) {
		f.usersOnly = !f.usersOnly
		v.refetch()
	}
	if inC.Clicked(gtx) {
		v.openInMenu(gtx, inC)
	}
	if hasC.Clicked(gtx) {
		v.openHasMenu(gtx, hasC)
	}
	if resetC.Clicked(gtx) {
		v.resetFilters()
	}

	withLabel := nsHuddles.T("With")
	switch n := len(f.with); {
	case n == 1:
		withLabel = withLabel + " " + v.userName(f.with[0])
	case n > 1:
		withLabel = nsHuddles.T("With {count, plural, =1 {{count} person} other {{count} people}}", "count", n)
	}
	inLabel := nsHuddles.T("In")
	switch n := len(f.in); {
	case n == 1:
		_, name := v.channelIconAndName(f.in[0], nil)
		if c, ok := v.h.Core().Conv(f.in[0]); ok && c.Kind == "channel" {
			name = "#" + name
		}
		inLabel = inLabel + " " + name
	case n > 1:
		inLabel = nsHuddles.T("In {count} places", "count", n)
	}
	only := nsHuddles.T("Include only {count, plural, =1 {this person} other {these people}}", "count", len(f.with))

	var lead, trail []layout.FlexChild
	add := func(dst *[]layout.FlexChild, wd W) {
		if len(*dst) > 0 {
			*dst = append(*dst, hgap(8))
		}
		*dst = append(*dst, layout.Rigid(wd))
	}
	if !enh {
		typeLabel := nsHuddles.T("All huddles")
		if f.typ == "missed" {
			typeLabel = nsHuddles.T("Missed huddles")
		}
		add(&lead, func(gtx C) D {
			return dirFilterPill(&v.base, gtx, typeC, dirPill{label: typeLabel, caret: true, active: f.typ == "missed"})
		})
		add(&lead, func(gtx C) D {
			return dirFilterPill(&v.base, gtx, withC, dirPill{label: withLabel, caret: true, active: len(f.with) > 0})
		})
		if len(f.with) > 0 {
			add(&lead, func(gtx C) D { return dirFilterPill(&v.base, gtx, onlyC, dirPill{label: only, active: f.usersOnly}) })
		}
		add(&lead, func(gtx C) D {
			return dirFilterPill(&v.base, gtx, inC, dirPill{label: inLabel, caret: true, active: len(f.in) > 0})
		})
	} else {
		// FilterHuddleType's pill group (gap 4)
		lead = append(lead,
			layout.Rigid(func(gtx C) D {
				return dirFilterPill(&v.base, gtx, allC, dirPill{label: nsHuddles.T("All"), active: f.typ != "missed"})
			}),
			hgap(4),
			layout.Rigid(func(gtx C) D {
				return dirFilterPill(&v.base, gtx, missedC, dirPill{label: nsHuddles.T("Missed"), active: f.typ == "missed"})
			}),
		)
		icon := "channel"
		if len(f.in) == 1 {
			icon = ""
		}
		add(&trail, func(gtx C) D {
			return dirFilterPill(&v.base, gtx, inC, dirPill{icon: icon, label: inLabel, caret: true, active: len(f.in) > 0})
		})
		add(&trail, func(gtx C) D {
			return dirFilterPill(&v.base, gtx, withC, dirPill{icon: "user", label: withLabel, caret: true, active: len(f.with) > 0})
		})
		if len(f.with) > 0 {
			add(&trail, func(gtx C) D { return dirFilterPill(&v.base, gtx, onlyC, dirPill{label: only, active: f.usersOnly}) })
		}
		hasLabel := nsHuddles.T("Has")
		if n := len(f.has); n > 0 {
			hasLabel = nsHuddles.T("Has {count, plural, =1 {{count} file type} other {{count} file types}}", "count", n)
		}
		add(&trail, func(gtx C) D {
			return dirFilterPill(&v.base, gtx, hasC, dirPill{icon: "canvas-browser", label: hasLabel, caret: true, active: len(f.has) > 0})
		})
		if f.resettable() {
			add(&trail, func(gtx C) D {
				return layout.Inset{Top: 5}.Layout(gtx, func(gtx C) D { return v.boldLink(gtx, resetC, nsHuddles.T("Reset filters")) })
			})
		}
	}
	defer opOffset(gtx, image.Pt(x0, 0)).Pop()
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(cw, 0), Max: image.Pt(cw, gtx.Constraints.Max.Y)}
	kids := append([]layout.FlexChild{}, lead...)
	if len(trail) > 0 {
		kids = append(kids, layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }))
		kids = append(kids, trail...)
	}
	d := layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(g, kids...)
	return D{Size: image.Pt(w, d.Size.Y+gtx.Dp(20))}
}

// boldLink is .resetLink__fYsbk's link: 13px bold, content-hgl-1,
// underlined on hover.
func (v *Huddles) boldLink(gtx C, c *widget.Clickable, label string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		fg := v.tok("dt_color-content-hgl-1")
		d := v.label(gtx, 13, wBold, fg, label)
		if c.Hovered() {
			y := d.Size.Y - gtx.Dp(2)
			fill(gtx, image.Rect(0, y, d.Size.X, y+max(1, gtx.Dp(1))), fg)
		}
		return d
	})
}

// pillRect is a filter pill's rectangle, for its menu's anchor.
func (v *Huddles) pillRect(gtx C, c *widget.Clickable) image.Rectangle {
	return v.buttonRect(c, image.Pt(gtx.Dp(120), gtx.Dp(28)))
}

// openTypeMenu is SearchFilterSingleSelect's menu: "All huddles" and
// "Missed huddles" as radio rows, 280 wide, bottom-left.
func (v *Huddles) openTypeMenu(gtx C, c *widget.Clickable) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	cur := v.filters.typ
	items := []popups.MenuItem{
		{Key: "all", Label: nsHuddles.T("All huddles"), Radio: true, Checked: cur != "missed", Do: func() { v.setType("all") }},
		{Key: "missed", Label: nsHuddles.T("Missed huddles"), Radio: true, Checked: cur == "missed", Do: func() { v.setType("missed") }},
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: v.pillRect(gtx, c), Width: 280, Popover: "bottom-left"}))
}

// openWithMenu is SearchFilterWith's list: the people picked (checked),
// "Clear all" past one, then "Suggestions".
func (v *Huddles) openWithMenu(gtx C, c *widget.Clickable) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	var items []popups.MenuItem
	for _, id := range v.filters.with {
		id := id
		items = append(items, popups.MenuItem{Key: "with-" + id, Label: v.userName(id), Checked: true, KeepOpen: true,
			Do: func() { v.toggleWith(id) }})
	}
	if len(v.filters.with) > 1 {
		items = append(items, popups.MenuItem{Key: "clear", Label: nsSearch.T("Clear all"), Do: func() {
			v.filters.with, v.filters.usersOnly = nil, false
			v.refetch()
		}})
	}
	var sug []popups.MenuItem
	for _, id := range v.withSuggestions() {
		if slices.Contains(v.filters.with, id) {
			continue
		}
		id := id
		sug = append(sug, popups.MenuItem{Key: "sug-" + id, Label: v.userName(id), KeepOpen: true, Do: func() { v.toggleWith(id) }})
	}
	if len(sug) > 0 {
		items = append(items, popups.MenuItem{Header: nsSearch.T("Suggestions")})
		items = append(items, sug...)
	}
	if len(items) == 0 {
		items = []popups.MenuItem{{Note: nsSearch.T("Nothing found")}}
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: v.pillRect(gtx, c), Width: 280, Popover: "bottom-left"}))
}

// openInMenu is SearchFilterIn's list (channels and group DMs only).
func (v *Huddles) openInMenu(gtx C, c *widget.Clickable) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	name := func(id string) string {
		_, n := v.channelIconAndName(id, nil)
		if cv, ok := v.h.Core().Conv(id); ok && cv.Kind == "channel" {
			return "#" + n
		}
		return n
	}
	var items []popups.MenuItem
	for _, id := range v.filters.in {
		id := id
		items = append(items, popups.MenuItem{Key: "in-" + id, Label: name(id), Checked: true, KeepOpen: true,
			Do: func() { v.toggleInChannel(id) }})
	}
	if len(v.filters.in) > 1 {
		items = append(items, popups.MenuItem{Key: "clear", Label: nsSearch.T("Clear all"), Do: func() {
			v.filters.in = nil
			v.refetch()
		}})
	}
	var sug []popups.MenuItem
	for _, id := range v.inSuggestions() {
		if slices.Contains(v.filters.in, id) {
			continue
		}
		id := id
		sug = append(sug, popups.MenuItem{Key: "sug-" + id, Label: name(id), KeepOpen: true, Do: func() { v.toggleInChannel(id) }})
	}
	if len(sug) > 0 {
		items = append(items, popups.MenuItem{Header: nsSearch.T("Suggestions")})
		items = append(items, sug...)
	}
	if len(items) == 0 {
		items = []popups.MenuItem{{Note: nsSearch.T("Nothing found")}}
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: v.pillRect(gtx, c), Width: 280, Popover: "bottom-left"}))
}

// openHasMenu is FilterHuddleHas' multi-select ("Huddle has"): Meeting
// notes, Thread, Files & links, with their icons.
func (v *Huddles) openHasMenu(gtx C, c *widget.Clickable) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	items := []popups.MenuItem{{Header: nsHuddles.T("Huddle has")}}
	for _, o := range hdHasOptions {
		o := o
		items = append(items, popups.MenuItem{Key: o.key, Label: nsHuddles.T(o.label), Icon: o.icon,
			Checked: slices.Contains(v.filters.has, o.key), KeepOpen: true, Do: func() { v.toggleHas(o.key) }})
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: v.pillRect(gtx, c), Width: 280, Popover: "bottom-left"}))
}
