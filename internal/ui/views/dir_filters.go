package views

// The Directories' Workspaces, Organizations, Title and Location filters:
// Slack's SearchFilterWorkspace (kGy0), SearchFilterOrgs (xlY+),
// SearchFilterTitle (4TgK) and SearchFilterLocation, each a
// SearchFilterSearchSelect (boot-shared 1Vs3) behind a filter pill whose
// text is getFilterTriggerContent's (search-filters HCYG).

import (
	"context"
	"image"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/chanent"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// dirPickOpt is one row of a filter popover.
type dirPickOpt struct {
	value, label string
	team         bool // a team: its icon and name (DnQ9 o)
	header       bool // a group heading ("Your workspaces", "Suggestions")
	clear        bool // "Clear all"
	group        bool // an option under a heading
	loading      bool // a loading row (BaseMissingChannel: the hash, MissingText's bar)
}

// dirPick is SearchFilterSearchSelect: a 280px c-menu under its pill (at
// the pill's left, like the other filter menus) holding a search input
// (234 wide, size medium: 36px, search icon, focused on open; 24px of
// wrapper padding above it, 16px below when there's nothing to list) and
// the options: 32px rows, 25px headings and "Clear all", the list padded
// 8px top and bottom. Single selects close on a pick; multi selects stay
// open and draw a checkbox before each option.
type dirPick struct {
	open    bool
	multi   bool
	btn     widget.Clickable
	catcher widget.Clickable
	panel   widget.Clickable
	srch    *search
	rows    []widget.Clickable
	shown   []dirPickOpt // the rows laid out last frame
	focus   bool
}

func newDirPick(placeholder string, multi bool) dirPick {
	return dirPick{srch: newSearch(placeholder), multi: multi}
}

// newDirPickIn is newDirPick with a placeholder that is Slack's string in
// ns.
func newDirPickIn(ns i18n.NS, placeholder string, multi bool) dirPick {
	return dirPick{srch: newSearchIn(ns, placeholder), multi: multi}
}

func (p *dirPick) query() string { return strings.TrimSpace(p.srch.text()) }

// toggle opens (empty query, input focused) or closes the popover.
func (p *dirPick) toggle() {
	p.open = !p.open
	if p.open {
		p.srch.ed.SetText("")
		p.focus = true
	}
}

// update handles the pill, the outside click and row clicks; it returns
// the picked option, if any. Call it before layout each frame.
func (p *dirPick) update(gtx C) (dirPickOpt, bool) {
	if p.btn.Clicked(gtx) {
		p.toggle()
	}
	if !p.open {
		return dirPickOpt{}, false
	}
	if p.catcher.Clicked(gtx) {
		p.open = false
		return dirPickOpt{}, false
	}
	p.srch.update(gtx)
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &p.srch.ed, Name: key.NameEscape})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			p.open = false
			return dirPickOpt{}, false
		}
	}
	for i := range p.shown {
		if i < len(p.rows) && p.rows[i].Clicked(gtx) {
			o := p.shown[i]
			if o.header || o.loading {
				continue
			}
			if !p.multi {
				p.open = false
			}
			return o, true
		}
	}
	return dirPickOpt{}, false
}

// popover records the open popover under a pill of size btn and defers it
// above everything; sel reports a selected value, team draws a team row.
func (p *dirPick) popover(b *base, gtx C, btn image.Point, opts []dirPickOpt, sel func(string) bool, empty string) {
	if !p.open {
		return
	}
	p.shown = opts
	for len(p.rows) < len(opts) {
		p.rows = append(p.rows, widget.Clickable{})
	}
	if p.focus {
		gtx.Execute(key.FocusCmd{Tag: &p.srch.ed})
		p.focus = false
	}
	m := op.Record(gtx.Ops)
	func() {
		big := 1 << 14
		defer op.Offset(image.Pt(-big, -big)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(2*big, 2*big))
		p.catcher.Layout(g, func(gtx C) D { return D{Size: gtx.Constraints.Min} })
	}()
	w := gtx.Dp(280)
	optH, headH, padY := gtx.Dp(32), gtx.Dp(25), gtx.Dp(8)
	rowH := func(o dirPickOpt) int {
		if o.header || o.clear {
			return headH
		}
		return optH
	}
	listH := 0
	q := p.query()
	showEmpty := len(opts) == 0 && q != "" && empty != ""
	if len(opts) > 0 || showEmpty {
		listH = 2 * padY
		for _, o := range opts {
			listH += rowH(o)
		}
		if showEmpty {
			listH += optH
		}
	}
	inputTop, inputH := gtx.Dp(24), gtx.Dp(36)
	h := inputTop + inputH
	if len(opts) == 0 && q == "" {
		h += gtx.Dp(16) // p-search_filter__multi-select-input--empty
	}
	h += listH
	func() {
		defer op.Offset(image.Pt(0, btn.Y+gtx.Dp(4))).Push(gtx.Ops).Pop()
		r := image.Rect(0, 0, w, h)
		rad := gtx.Dp(8)
		theme.Shadow(gtx, r, rad, gtx.Dp(12), argb(0x1f000000))
		rrect(gtx, r, rad, b.p.Surface)
		border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.Ring)
		g := gtx
		g.Constraints = layout.Exact(r.Max)
		p.panel.Layout(g, func(gtx C) D { return D{Size: r.Max} })
		iw := gtx.Dp(234)
		dirAt(gtx, image.Pt((w-iw)/2, inputTop), func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(iw, inputH))
			return dirSearchField(b, gtx, p.srch, dirFieldStyle{H: 36, Icon: 16, PadL: 12, Gap: 4, Border: b.p.Faint, Over: b.p.Surface}, nil)
		})
		y := inputTop + inputH + padY
		if len(opts) == 0 && q == "" {
			return
		}
		if showEmpty {
			dirAt(gtx, image.Pt(gtx.Dp(24), y), func(gtx C) D {
				gtx.Constraints = layout.Exact(image.Pt(w-gtx.Dp(48), optH))
				return layout.W.Layout(gtx, func(gtx C) D { return b.label(gtx, 15, wReg, b.p.Sec, empty) })
			})
			return
		}
		for i, o := range opts {
			rh := rowH(o)
			dirAt(gtx, image.Pt(0, y), func(gtx C) D {
				gtx.Constraints = layout.Exact(image.Pt(w, rh))
				if o.header {
					return dirAt(gtx, image.Pt(gtx.Dp(24), 0), func(gtx C) D {
						gtx.Constraints = layout.Exact(image.Pt(w-gtx.Dp(48), rh))
						return layout.W.Layout(gtx, func(gtx C) D { return b.label(gtx, 13, wReg, b.p.Sec, o.label) })
					})
				}
				return p.rows[i].Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					return p.row(b, gtx, o, image.Pt(w, rh), sel != nil && sel(o.value), p.rows[i].Hovered())
				})
			})
			y += rh
		}
	}()
	op.Defer(gtx.Ops, m.Stop())
}

// row draws one option: c-select_options_list__option (15px, padding 0
// 24px; the selected one in highlight blue and bold, after Slack's check
// glyph in a single select; the active one white on blue) with
// p-search_filter__list_item's content.
func (p *dirPick) row(b *base, gtx C, o dirPickOpt, sz image.Point, selected, active bool) D {
	fg := b.p.Text
	weight := wReg
	if selected {
		fg, weight = b.p.Link, wBold
	}
	if active {
		fill(gtx, image.Rectangle{Max: sz}, b.p.Link)
		fg = rgb(0xffffff)
	}
	x := gtx.Dp(24)
	if o.clear { // c-select_options_list__clear_option: 13px highlight
		c := b.p.Link
		if active {
			c = rgb(0xffffff)
		}
		return dirAt(gtx, image.Pt(x, 0), func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(sz.X-2*x, sz.Y))
			return layout.W.Layout(gtx, func(gtx C) D { return b.label(gtx, 13, wReg, c, o.label) })
		})
	}
	if selected && !p.multi {
		// ::before "\e285" (check), margin 0 2px 0 -18px, at the 15px size
		dirAt(gtx, image.Pt(x-gtx.Dp(18), (sz.Y-gtx.Dp(15))/2), func(gtx C) D { return b.icon(gtx, "check", 15, fg) })
	}
	if p.multi {
		// p-search_filter__list_item--checkbox::before: 13px, 12px gap
		cb := gtx.Dp(13)
		r := image.Rect(x, (sz.Y-cb)/2, x+cb, (sz.Y-cb)/2+cb)
		rad := gtx.Dp(3)
		if selected {
			rrect(gtx, r, rad, b.p.Link)
			dirAt(gtx, r.Min, func(gtx C) D { return b.icon(gtx, "check", 13, rgb(0xffffff)) })
		} else {
			rrect(gtx, r, rad, b.p.Surface)
			border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.Faint)
		}
		x += cb + gtx.Dp(12)
	}
	tw := sz.X - x - gtx.Dp(24)
	if o.team {
		// BaseTeamIcon (20px) and a 6px gap, then the name
		s := gtx.Dp(20)
		if c := b.h.Core(); c != nil {
			if _, icon, _ := c.Team(o.value, 20); icon != "" {
				dirAt(gtx, image.Pt(x, (sz.Y-s)/2), func(gtx C) D { return dirImage(b, gtx, icon, image.Pt(s, s), gtx.Dp(4)) })
			}
		}
		x += s + gtx.Dp(6)
		tw -= s + gtx.Dp(6)
	}
	label := o.label
	dirAt(gtx, image.Pt(x, 0), func(gtx C) D {
		gtx.Constraints = layout.Exact(image.Pt(max(0, tw), sz.Y))
		if o.loading {
			e := chanent.Ent{Text: "#", Bar: chanent.BarWidth(o.value)}
			return layout.W.Layout(gtx, func(gtx C) D { return b.chanEntity(gtx, e, 15, weight, fg) })
		}
		return layout.W.Layout(gtx, func(gtx C) D { return b.label(gtx, 15, weight, fg, label) })
	})
	return D{Size: sz}
}

// dirMatch is a forgiving name match for the popovers' search (Slack
// scores with its fuzzy matcher; Clack keeps the names that contain the
// query's words, in order).
func dirMatch(name, q string) bool {
	n := strings.ToLower(name)
	for _, w := range strings.Fields(strings.ToLower(q)) {
		i := strings.Index(n, w)
		if i < 0 {
			return false
		}
		n = n[i+len(w):]
	}
	return true
}

// ---- the filters' state and options ----

// dirTeamName is a team's name once core has read it (EnsureTeam runs in
// the background for the ones it hasn't).
func (d *Directories) dirTeamName(id string) string {
	c := d.h.Core()
	if c == nil {
		return id
	}
	if name, _, ok := c.Team(id, 20); ok && name != "" {
		return name
	}
	if d.teamAsked == nil {
		d.teamAsked = map[string]bool{}
	}
	if !d.teamAsked[id] {
		d.teamAsked[id] = true
		h := d.h
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			c.EnsureTeam(ctx, id)
			h.Invalidate()
		}()
	}
	return id
}

// workspaceOpts are SearchFilterWorkspace's options: "All workspaces",
// the selected workspace, then "Your workspaces" (the rest); a query
// lists the matching workspaces instead.
func (d *Directories) workspaceOpts(t int) []dirPickOpt {
	c := d.h.Core()
	if c == nil {
		return nil
	}
	sel := d.team[t]
	var teams []string
	for _, id := range c.EnterpriseTeams() {
		if id != sel {
			teams = append(teams, id)
		}
	}
	if q := d.wsPick[t].query(); q != "" {
		var out []dirPickOpt
		all := append([]string(nil), c.EnterpriseTeams()...)
		for _, id := range all {
			if name := d.dirTeamName(id); dirMatch(name, q) {
				out = append(out, dirPickOpt{value: id, label: name, team: true})
			}
		}
		return out
	}
	out := []dirPickOpt{{value: "", label: nsSearch.T("All workspaces")}}
	if sel != "" {
		out = append(out, dirPickOpt{value: sel, label: d.dirTeamName(sel), team: true})
	}
	if len(teams) > 0 {
		out = append(out, dirPickOpt{value: "workspaces-label", label: nsSearch.T("Your workspaces"), header: true})
		for _, id := range teams {
			out = append(out, dirPickOpt{value: id, label: d.dirTeamName(id), team: true, group: true})
		}
	}
	return out
}

// orgOpts are SearchFilterOrgs': the selected organizations ("Clear all"
// after two or more), then "Suggestions": the connected ones not chosen.
func (d *Directories) orgOpts() []dirPickOpt {
	list, _, _ := d.orgList.get()
	q := d.orgPick.query()
	var out []dirPickOpt
	if q != "" {
		for _, id := range list {
			if name := d.dirTeamName(id); dirMatch(name, q) {
				out = append(out, dirPickOpt{value: id, label: name, team: true})
			}
		}
		return out
	}
	for _, id := range d.orgs {
		out = append(out, dirPickOpt{value: id, label: d.dirTeamName(id), team: true})
	}
	if len(d.orgs) > 1 {
		out = append(out, dirPickOpt{value: "clear", label: nsSearch.T("Clear all"), clear: true})
	}
	var sug []dirPickOpt
	for _, id := range list {
		if !dirHas(d.orgs, id) {
			sug = append(sug, dirPickOpt{value: id, label: d.dirTeamName(id), team: true, group: true})
		}
	}
	if len(sug) > 0 {
		out = append(out, dirPickOpt{value: "suggestions-label", label: nsSearch.T("Suggestions"), header: true})
		out = append(out, sug...)
	}
	return out
}

// facetOpts are SearchFilterTitle's and SearchFilterLocation's: the
// selections ("Clear all" after two or more), then up to five suggested
// values not chosen; a query lists the matching suggestions and the query
// itself (useEchoQuery).
func facetOpts(p *dirPick, chosen []string, facets []core.Facet) []dirPickOpt {
	var out []dirPickOpt
	if q := p.query(); q != "" {
		for _, f := range facets {
			if dirMatch(f.Value, q) {
				out = append(out, dirPickOpt{value: f.Value, label: f.Value})
			}
		}
		return append(out, dirPickOpt{value: q, label: q})
	}
	for _, v := range chosen {
		out = append(out, dirPickOpt{value: v, label: v})
	}
	if len(chosen) > 1 {
		out = append(out, dirPickOpt{value: "clear", label: nsSearch.T("Clear all"), clear: true})
	}
	var sug []dirPickOpt
	for _, f := range facets[:min(5, len(facets))] {
		if f.Value != "" && !dirHas(chosen, f.Value) {
			sug = append(sug, dirPickOpt{value: f.Value, label: f.Value, group: true})
		}
	}
	if len(sug) > 0 {
		out = append(out, dirPickOpt{value: "Suggestions", label: nsSearch.T("Suggestions"), header: true})
		out = append(out, sug...)
	}
	return out
}

func dirHas(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// dirToggle adds v to list or takes it out.
func dirToggle(list []string, v string) []string {
	for i, s := range list {
		if s == v {
			return append(list[:i:i], list[i+1:]...)
		}
	}
	return append(list, v)
}

// pickFor returns the popover behind filter label on tab t, or nil.
func (d *Directories) pickFor(t int, label string) *dirPick {
	switch {
	case label == "Workspaces":
		return &d.wsPick[t]
	case t == dirTabChannels && label == "Organizations":
		return &d.orgPick
	case t == dirTabPeople && label == "Title":
		return &d.titlePick
	case t == dirTabPeople && label == "Location":
		return &d.locPick
	}
	return nil
}

// pickTrigger is getFilterTriggerContent's text for a popover filter, and
// whether the filter is active (the pill turns highlight blue).
func (d *Directories) pickTrigger(t int, label string) (text, team string, active bool) {
	switch {
	case label == "Workspaces":
		if id := d.team[t]; id != "" {
			return nsSearch.T("In") + "\u00a0" + d.dirTeamName(id), id, true
		}
	case label == "Organizations":
		switch len(d.orgs) {
		case 0:
		case 1:
			return nsSearch.T("With") + "\u00a0" + d.dirTeamName(d.orgs[0]), d.orgs[0], true
		default:
			return nsSearch.T("With {count, plural, =1 {{count} organization} other {{count} organizations}}", "count", len(d.orgs)), "", true
		}
	case label == "Title":
		switch len(d.titles) {
		case 0:
		case 1:
			return nsSearch.T("Title:") + "\u00a0" + d.titles[0], "", true
		default:
			return nsSearch.T("Multiple titles"), "", true
		}
	case label == "Location":
		switch len(d.locs) {
		case 0:
		case 1:
			return nsSearch.T("Location:") + "\u00a0" + d.locs[0], "", true
		default:
			return nsSearch.T("Multiple locations"), "", true
		}
	}
	return nsSearch.T(label), "", false
}

// pickOpts are the options of the popover behind label on tab t and
// which of them are selected.
func (d *Directories) pickOpts(t int, label string) ([]dirPickOpt, func(string) bool) {
	switch label {
	case "Workspaces":
		sel := d.team[t]
		return d.workspaceOpts(t), func(v string) bool { return v == sel }
	case "Organizations":
		return d.orgOpts(), func(v string) bool { return dirHas(d.orgs, v) }
	case "Title":
		return facetOpts(&d.titlePick, d.titles, d.facetT), func(v string) bool { return dirHas(d.titles, v) }
	case "Location":
		return facetOpts(&d.locPick, d.locs, d.facetL), func(v string) bool { return dirHas(d.locs, v) }
	}
	return nil, nil
}

// applyPick applies a pick from the popover behind label on tab t and
// reports whether the results need reloading.
func (d *Directories) applyPick(t int, label string, o dirPickOpt) bool {
	switch label {
	case "Workspaces":
		if d.team[t] == o.value {
			return false
		}
		d.team[t] = o.value
	case "Organizations":
		if o.clear {
			d.orgs = nil
		} else {
			d.orgs = dirToggle(d.orgs, o.value)
		}
	case "Title":
		if o.clear {
			d.titles = nil
		} else {
			d.titles = dirToggle(d.titles, o.value)
		}
	case "Location":
		if o.clear {
			d.locs = nil
		} else {
			d.locs = dirToggle(d.locs, o.value)
		}
	default:
		return false
	}
	return true
}

// closePicks closes every filter popover.
func (d *Directories) closePicks() {
	for i := range d.wsPick {
		d.wsPick[i].open = false
	}
	d.orgPick.open, d.titlePick.open, d.locPick.open = false, false, false
}

// takeFacets keeps the people browser's filter suggestions
// (hydrateFacetSuggestions: merged by value, at most every 4 minutes).
func (d *Directories) takeFacets(now time.Time, ts, ls []core.Facet) {
	if len(ts) == 0 && len(ls) == 0 {
		return
	}
	if !d.facetAt.IsZero() && now.Sub(d.facetAt) <= 4*time.Minute {
		return
	}
	d.facetT, d.facetL, d.facetAt = mergeFacets(d.facetT, ts), mergeFacets(d.facetL, ls), now
}

func mergeFacets(a, b []core.Facet) []core.Facet {
	seen := map[string]bool{}
	var out []core.Facet
	for _, f := range append(append([]core.Facet(nil), a...), b...) {
		if !seen[f.Value] {
			seen[f.Value] = true
			out = append(out, f)
		}
	}
	return out
}

// ---- for tests and screenshots ----

// FilterText is the selected tab's filter pill text for filter label
// ("Workspaces", "Organizations", "Title", "Location").
func (d *Directories) FilterText(label string) string {
	text, _, _ := d.pickTrigger(d.tab.Sel, label)
	return text
}

// OpenFilter opens the popover behind filter label on the selected tab.
func (d *Directories) OpenFilter(label string) bool {
	p := d.pickFor(d.tab.Sel, label)
	if p == nil {
		return false
	}
	d.closeMenus()
	if !p.open {
		p.toggle()
	}
	return true
}

// FilterOptions lists the option labels of filter label's popover.
func (d *Directories) FilterOptions(label string) []string {
	opts, _ := d.pickOpts(d.tab.Sel, label)
	out := make([]string, len(opts))
	for i, o := range opts {
		out[i] = o.label
	}
	return out
}

// PickFilter picks the option with value in filter label's popover, as a
// click on it does.
func (d *Directories) PickFilter(label, value string) bool {
	t := d.tab.Sel
	p := d.pickFor(t, label)
	if p == nil {
		return false
	}
	opts, _ := d.pickOpts(t, label)
	for _, o := range opts {
		if o.value == value && !o.header {
			if !p.multi {
				p.open = false
			}
			if d.applyPick(t, label, o) {
				d.load(t, d.srch[t].text())
			}
			return true
		}
	}
	return false
}
