package popups

// The results page's filter row, as Slack's SearchFilters draws it
// (gantry search-filters_tsx, client-boot XJW+; CDN build 4d0c78085232):
//
//   - each tab shows the pills _p lists, in its order, kept to the tab's
//     filters (getDefaultFiltersByEntity): Messages From, In, a divider,
//     Includes me (with:me), Has a link, Threads only, Last 7 days; DMs
//     With; Files From, In, File type, Date, Only my channels; Canvases
//     Created By, From, In, Date. Channels and People have filters Clack
//     doesn't build (channel_type, profile fields), so only the source.
//   - useSearchFiltersResponsive fits them: every pill (the source pill
//     first) is measured and kept while it fits in the row less 24px, the
//     More button and, with filters applied, the Reset button (each pill
//     counting its width + 8).
//   - More (maybeRenderFiltersLink) lists the tab's filters that aren't
//     pills (With always on Messages); with none it isn't drawn (DMs).
//   - Reset (p-search_filter__reset_button, 13px link colour, 8px after
//     More) shows when filters are applied and clears them, the Exclude
//     bot messages and Only my channels prefs too.

import (
	"context"
	"image"
	"slices"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's search filter ids (cX/T hv).
const (
	srFSources    = "sources"
	srFFrom       = "filter-from"
	srFIn         = "filter-in"
	srFWith       = "filter-with"
	srFDate       = "filter-date"
	srFFileType   = "filter-file-type"
	srFOnlyMine   = "filter-only-my-channels"
	srFExclBots   = "filter-exclude-non-members"
	srFExact      = "filter-exact-matches-only"
	srFExclArch   = "filter-exclude-archived"
	srFReaction   = "filter-reaction"
	srFHasFile    = "filter-has-file"
	srFHasAction  = "filter-has-action"
	srFHasLink    = "filter-has-link"
	srFIsDM       = "filter-is-dm"
	srFIsSaved    = "filter-is-saved"
	srFIsTodos    = "filter-is-todos"
	srFIsThread   = "filter-is-thread"
	srFHasPin     = "filter-has-pin"
	srFLast7      = "filter-last-7-days"
	srFIsActivity = "filter-is-activity"
	srFCreator    = "creator"
)

// srFiltersShown is _p: the pills a tab shows, in order (Slack's list
// intersected with the tab's filters).
func srFiltersShown(tab int) []string {
	switch tab {
	case srTabMessages:
		return []string{srFFrom, srFIn, srFWith, srFHasLink, srFIsThread, srFLast7}
	case srTabDMs:
		return []string{srFWith}
	case srTabFiles:
		return []string{srFFrom, srFIn, srFFileType, srFDate, srFOnlyMine}
	case srTabCanvases:
		return []string{srFCreator, srFFrom, srFIn, srFDate}
	}
	return nil
}

// srFiltersDefault is getDefaultFiltersByEntity (To-dos with later_todos).
func srFiltersDefault(tab int, laterTodos bool) []string {
	switch tab {
	case srTabMessages:
		f := []string{srFFrom, srFIn, srFWith, srFDate, srFFileType, srFOnlyMine, srFExclBots, srFExact, srFExclArch,
			srFReaction, srFHasFile, srFHasAction, srFHasLink, srFIsDM, srFIsSaved}
		if laterTodos {
			f = append(f, srFIsTodos)
		}
		return append(f, srFIsThread, srFHasPin, srFLast7, srFIsActivity)
	case srTabDMs:
		return []string{srFWith}
	case srTabFiles:
		return []string{srFFrom, srFIn, srFWith, srFDate, srFFileType, srFOnlyMine}
	case srTabCanvases:
		return []string{srFFrom, srFIn, srFWith, srFDate, srFCreator}
	}
	return nil
}

// srMoreAllowed is maybeRenderFiltersLink's allowedFilters: the tab's
// filters that aren't pills, With always on Messages.
func srMoreAllowed(tab int, visible []string, laterTodos bool) []string {
	var out []string
	for _, f := range srFiltersDefault(tab, laterTodos) {
		if (f == srFWith && tab == srTabMessages) || !slices.Contains(visible, f) {
			out = append(out, f)
		}
	}
	return out
}

// srFitFilters is useSearchFiltersResponsive: how many of the pills
// (widths in order) fit in avail, each taking its width + 8.
func srFitFilters(widths []int, avail, gap int) int {
	n := 0
	for _, w := range widths {
		if avail <= w+gap {
			break
		}
		avail -= w + gap
		n++
	}
	return n
}

// srPillOf maps a filter id to its pill slot.
func srPillOf(f string) int {
	switch f {
	case srFSources:
		return srPType
	case srFFrom:
		return srPFrom
	case srFIn:
		return srPIn
	case srFWith:
		return srPMe
	case srFHasLink:
		return srPLink
	case srFIsThread:
		return srPThread
	case srFLast7:
		return srPWeek
	case srFDate:
		return srPDate
	case srFFileType:
		return srPFileType
	case srFOnlyMine:
		return srPOnlyMine
	case srFCreator:
		return srPCreator
	}
	return -1
}

// srPillSpec is a pill's look.
type srPillSpec struct {
	icon, label     string
	chevron, active bool
}

// srTeammates is "{n} teammate(s)".
func srTeammates(pre string, n int) string {
	if n == 1 {
		return pre + " 1 teammate"
	}
	return pre + " " + strconv.Itoa(n) + " teammates"
}

// values returns key's (non-negated) values in the query.
func (v *SearchView) values(key string) []string {
	var out []string
	for _, m := range v.q.Mods {
		if m.Key == key && !m.Neg {
			out = append(out, m.Val)
		}
	}
	return out
}

// pillSpec is a filter's pill label (the filter label renderer): From,
// In, With and Created by name their one choice ("From Ann") or count
// them; Date shows its range; File type its one type or the count.
// Slack draws a chosen person's avatar before the name; Clack's Pill draws
// icons only, so the person icon stays.
func (v *SearchView) pillSpec(f string) srPillSpec {
	person := func(key, empty, pre, icon string) srPillSpec {
		vals := v.values(key)
		switch len(vals) {
		case 0:
			return srPillSpec{icon: icon, label: empty, chevron: true}
		case 1:
			return srPillSpec{icon: icon, label: pre + " " + strings.TrimPrefix(v.refName("from", vals[0]), "@"), chevron: true, active: true}
		}
		return srPillSpec{icon: icon, label: srTeammates(pre, len(vals)), chevron: true, active: true}
	}
	switch f {
	case srFSources:
		return srPillSpec{icon: srTabIcons[v.tab], label: srTabNames[v.tab], chevron: true}
	case srFFrom:
		return person("from", "From", "From", "user")
	case srFCreator:
		s := person("creator", "Created By", "Created by", "")
		return s
	case srFWith:
		if v.tab != srTabDMs { // Includes me: with:<@self>
			self := v.selfRef()
			return srPillSpec{icon: "user", label: "Includes me", active: self != "" && v.q.Has("with", self)}
		}
		return person("with", "With", "With", "")
	case srFIn:
		var in []string
		for _, x := range v.values("in") {
			if !strings.EqualFold(x, "activity") {
				in = append(in, x)
			}
		}
		switch len(in) {
		case 0:
			return srPillSpec{icon: "channel", label: "In", chevron: true}
		case 1:
			return srPillSpec{icon: "channel", label: "In " + v.refName("in", in[0]), chevron: true, active: true}
		}
		return srPillSpec{icon: "channel", label: "In " + strconv.Itoa(len(in)) + " places", chevron: true, active: true}
	case srFHasLink:
		return srPillSpec{icon: "link", label: "Has a link", active: v.q.Has("has", "link")}
	case srFIsThread:
		return srPillSpec{icon: "threads", label: "Threads only", active: v.q.Has("is", "thread")}
	case srFLast7:
		return srPillSpec{icon: "calendar", label: "Last 7 days", active: v.q.Has("after", srLast7(time.Now()))}
	case srFDate:
		after, before, on := v.q.Get("after"), v.q.Get("before"), v.q.Get("on")
		if l := srDateLabel(after, before, on, time.Now()); l != "" {
			return srPillSpec{label: l, chevron: true, active: true}
		}
		return srPillSpec{label: "Date", chevron: true}
	case srFFileType:
		ts := v.values("type")
		switch len(ts) {
		case 0:
			return srPillSpec{label: "File type", chevron: true}
		case 1:
			return srPillSpec{label: srFileTypeLabel(ts[0]), chevron: true, active: true}
		}
		return srPillSpec{label: strconv.Itoa(len(ts)) + " file types", chevron: true, active: true}
	case srFOnlyMine:
		c := v.h.Core()
		return srPillSpec{label: "Only my channels", active: c != nil && c.PrefBool("search_only_my_channels", false)}
	}
	return srPillSpec{}
}

// srFileTypeLabel is getFileTypeLabels' name for a type: modifier value.
func srFileTypeLabel(val string) string {
	for _, ft := range srFileTypes {
		if strings.EqualFold(ft.val, val) {
			return ft.label
		}
	}
	return val
}

// selfRef is the viewer as a with:/from: value ("" before the snapshot).
func (v *SearchView) selfRef() string {
	if snap, ok := v.snapshot(); ok && snap.Self.ID != "" {
		return "<@" + snap.Self.ID + ">"
	}
	return ""
}

// srPillWidth is Pill's width for a spec (10 + icon 16+6 + label + 6+12+8
// with the chevron, else 10).
func srPillWidth(gtx layout.Context, th *theme.Theme, s srPillSpec) int {
	w := dp(gtx, 10) + textW(gtx, th, 13, theme.Bold, s.label)
	if s.icon != "" {
		w += dp(gtx, 16) + dp(gtx, 6)
	}
	if s.chevron {
		w += dp(gtx, 6) + dp(gtx, 12) + dp(gtx, 8)
	} else {
		w += dp(gtx, 10)
	}
	return w
}

// filtersApplied is getAppliedFilters: a modifier in the query, Exact
// matches only, or the Exclude bot messages / Only my channels prefs.
func (v *SearchView) filtersApplied() bool {
	if len(v.q.Mods) > 0 || v.exact {
		return true
	}
	c := v.h.Core()
	return c != nil && (c.PrefBool("search_exclude_bots", false) || c.PrefBool("search_only_my_channels", false))
}

// pillRow lays out the filter row at (x, y) up to right.
func (v *SearchView) pillRow(gtx layout.Context, th *theme.Theme, cl *Colors, x, y, right int) {
	for i := range v.pillR {
		v.pillR[i] = image.Rectangle{}
	}
	gap := dp(gtx, 8)
	row := append([]string{srFSources}, srFiltersShown(v.tab)...)
	specs := make([]srPillSpec, len(row))
	widths := make([]int, len(row))
	for i, f := range row {
		specs[i] = v.pillSpec(f)
		widths[i] = srPillWidth(gtx, th, specs[i])
	}
	more := srPillSpec{icon: "filter", label: "More", chevron: true, active: v.moreActive()}
	// what's left for the pills: the row less 24, More (when it has
	// filters) and Reset (+8) when filters are applied
	avail := right - x - dp(gtx, 24)
	applied := v.filtersApplied()
	resetW := 0
	if applied {
		resetW = textW(gtx, th, 13, theme.Regular, "Reset")
		avail -= resetW + gap
	}
	laterTodos := false
	if c := v.h.Core(); c != nil {
		laterTodos = c.Experiment("later_todos") == "on"
	}
	// the row's last element is More when it has filters to offer; when
	// the pills overflow they go to More, which then shows
	moreW := srPillWidth(gtx, th, more)
	needMore := len(srMoreAllowed(v.tab, row[1:], laterTodos)) > 0
	room := avail
	if needMore {
		room -= moreW
	}
	n := srFitFilters(widths, room, gap)
	if n < len(row) && !needMore {
		n = srFitFilters(widths, avail-moreW, gap)
	}
	n = max(n, 1) // the source pill always shows
	v.visible = append(v.visible[:0], row[:n]...)
	for i := 0; i < n; i++ {
		f := row[i]
		if f == srFWith && v.tab != srTabDMs && i > 0 {
			// p-search_filter__divider: 1x24, margin 0 12px, otl-ter
			x += dp(gtx, 4)
			ph := dp(gtx, 28)
			fill(gtx, image.Rect(x, y+(ph-dp(gtx, 24))/2, x+maxInt(1, dp(gtx, 1)), y+(ph+dp(gtx, 24))/2), cl.Divider)
			x += maxInt(1, dp(gtx, 1)) + dp(gtx, 12)
		}
		s := specs[i]
		x += v.pill(gtx, th, cl, srPillOf(f), x, y, s.icon, s.label, s.chevron, s.active) + gap
	}
	if len(srMoreAllowed(v.tab, v.visible[1:], laterTodos)) > 0 {
		x += v.pill(gtx, th, cl, srPMore, x, y, more.icon, more.label, more.chevron, more.active) + gap
	}
	if applied {
		ph := dp(gtx, 28)
		v.pillR[srPReset] = image.Rect(x, y, x+resetW, y+ph)
		at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &v.pills[srPReset], nil, func(gtx layout.Context) layout.Dimensions {
				d := vcenter(gtx, ph, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, cl.Link, "Reset")
				})
				if v.pills[srPReset].Hovered() { // :hover underlines
					uy := (ph+dp(gtx, 13))/2 + dp(gtx, 1)
					fill(gtx, image.Rect(0, uy, d.Size.X, uy+maxInt(1, dp(gtx, 1))), cl.Link)
				}
				return layout.Dimensions{Size: image.Pt(d.Size.X, ph)}
			})
		})
	}
}

// pillClicked runs a pill's action.
func (v *SearchView) pillClicked(gtx layout.Context, i int) {
	switch i {
	case srPType:
		v.typeMenu(gtx)
	case srPFrom:
		v.peopleMenu(gtx, "from", "Anyone", srPFrom)
	case srPCreator:
		v.peopleMenu(gtx, "creator", "Anyone", srPCreator)
	case srPIn:
		v.inMenu(gtx)
	case srPMe:
		if v.tab == srTabDMs {
			v.peopleMenu(gtx, "with", "Anyone", srPMe)
			return
		}
		if self := v.selfRef(); self != "" { // Includes me: with:<@self>
			v.setQuery(v.q.Toggle("with", self))
		}
	case srPLink:
		v.setQuery(v.q.Toggle("has", "link"))
	case srPThread:
		v.setQuery(v.q.Toggle("is", "thread"))
	case srPWeek: // the Date menu's Last 7 days
		now := time.Now()
		if v.q.Has("after", srLast7(now)) {
			v.setQuery(srWithRange(v.q, "", ""))
		} else {
			v.setQuery(srWithRange(v.q, srLast7(now), srDate(now.AddDate(0, 0, 1))))
		}
	case srPDate:
		v.openMenu(gtx, v.pillR[srPDate], v.dateItems(gtx, srPDate), 240)
	case srPFileType:
		v.openMenu(gtx, v.pillR[srPFileType], v.fileTypeItems(), 240)
	case srPOnlyMine:
		v.togglePref("search_only_my_channels")()
	case srPMore:
		v.moreMenu(gtx)
	case srPReset:
		v.resetFilters()
	}
}

// togglePref flips a search pref (handleSearchPrefChange) and searches
// again.
func (v *SearchView) togglePref(name string) func() {
	return func() {
		c := v.h.Core()
		if c == nil {
			return
		}
		val := !c.PrefBool(name, false)
		run(v.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, c.SetPref(ctx, name, val) },
			func(_ struct{}, err error) {
				if err != nil {
					v.h.Toast("Couldn't save that preference: " + errText(err))
					return
				}
				v.search(1)
			})
	}
}

// resetFilters is Reset: every filter off (the query keeps its words),
// Exact matches only off, and the Exclude bot messages and Only my
// channels prefs set false.
func (v *SearchView) resetFilters() {
	c := v.h.Core()
	var prefs []string
	if c != nil {
		for _, p := range []string{"search_exclude_bots", "search_only_my_channels"} {
			if c.PrefBool(p, false) {
				prefs = append(prefs, p)
			}
		}
	}
	v.exact = false
	v.setQuery(v.q.ClearMods())
	if len(prefs) == 0 || c == nil {
		return
	}
	run(v.h, func(ctx context.Context) (struct{}, error) {
		for _, p := range prefs {
			if err := c.SetPref(ctx, p, false); err != nil {
				return struct{}{}, err
			}
		}
		return struct{}{}, nil
	}, func(_ struct{}, err error) {
		if err != nil {
			v.h.Toast("Couldn't save that preference: " + errText(err))
		}
		v.search(1)
	})
}

// peopleMenu is a person pill's menu (From, Created by, With in DMs):
// Anyone, you, then people, each setting key:<@id>.
func (v *SearchView) peopleMenu(gtx layout.Context, key, anyone string, slot int) {
	cur := v.q.Get(key)
	items := []MenuItem{{Label: anyone, Checked: cur == "", Do: func() { v.setQuery(v.q.Set(key, "")) }}, Sep()}
	snap, ok := v.snapshot()
	if ok {
		type person struct{ id, name string }
		var ps []person
		self := snap.Self.ID
		for id, u := range snap.Users {
			if u.Deleted || id == self {
				continue
			}
			ps = append(ps, person{id, userName(u)})
		}
		slices.SortFunc(ps, func(a, b person) int { return strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name)) })
		if len(ps) > 15 {
			ps = ps[:15]
		}
		if self != "" {
			val := "<@" + self + ">"
			items = append(items, MenuItem{Label: userName(snap.Self) + " (you)", Icon: "user",
				Checked: cur == val || strings.EqualFold(cur, "me"), Do: func() { v.setQuery(v.q.Set(key, val)) }})
		}
		for _, p := range ps {
			val := "<@" + p.id + ">"
			items = append(items, MenuItem{Label: p.name, Icon: "user", Checked: cur == val, Do: func() { v.setQuery(v.q.Set(key, val)) }})
		}
	}
	v.openMenu(gtx, v.pillR[slot], items, 260)
}

// dateItems are the Date submenu's rows (Any time, the presets, On…,
// Before…, After…, Range…); anchor is the pill the date picker opens at.
func (v *SearchView) dateItems(gtx layout.Context, anchor int) []MenuItem {
	now := time.Now()
	dates := []MenuItem{{Label: "Any time", Radio: true, Checked: v.q.Get("after") == "" && v.q.Get("before") == "" && v.q.Get("on") == "",
		Do: func() { v.setQuery(srWithRange(v.q, "", "")) }}}
	for _, d := range srDateRanges {
		after, before := srDate(d.from(now)), srDate(d.end(now))
		dates = append(dates, MenuItem{Label: d.label, Radio: true, Checked: v.q.Get("after") == after && v.q.Get("before") == before,
			Do: func() { v.setQuery(srWithRange(v.q, after, before)) }})
	}
	pick := func(key string) func() {
		return func() {
			r := v.pillR[anchor]
			if r.Empty() {
				r = v.pillR[srPMore]
			}
			v.h.ShowOverlay(NewDatePicker(v.h, v.win(gtx, r), now, time.Time{}, func(day time.Time) {
				q := v.q.Set("on", "").Set("during", "")
				switch key {
				case "on":
					q = q.Set("before", "").Set("after", "").Set("on", srDate(day))
				case "range":
					q = q.Set("after", srDate(day.AddDate(0, 0, -1))) // the range's first day; Before… ends it
				default:
					q = q.Set(key, srDate(day))
				}
				v.setQuery(q)
			}))
		}
	}
	return append(dates, Sep(), MenuItem{Label: "On…", Do: pick("on")}, MenuItem{Label: "Before…", Do: pick("before")},
		MenuItem{Label: "After…", Do: pick("after")}, MenuItem{Label: "Range…", Do: pick("range")})
}

// fileTypeItems are the File type rows (checkboxes with their icons).
func (v *SearchView) fileTypeItems() []MenuItem {
	th := v.h.Theme()
	var types []MenuItem
	for _, ft := range srFileTypes {
		val := ft.val
		types = append(types, MenuItem{Label: ft.label, Widget: srCheckIconRow(th, ft.label, ft.icon, func() bool { return v.q.Has("type", val) }),
			Do: func() { v.setQuery(v.q.Toggle("type", val)) }, KeepOpen: true})
	}
	return types
}

// moreMenu is MoreFiltersDropdown (the e8 template): the rows for the
// filters the row doesn't show as pills (srMoreAllowed). Saved
// preferences (the search_exclude_bots / search_only_my_channels prefs),
// Results (Exact matches only; Exclude archived only with
// search_explore_freshness_signals on), then From, In, With people,
// Message has (File, Link, Action), Message is (A direct message, In a
// thread, Saved, To-dos with later_todos, Pinned, In Activity), Date, File
// type, Reactions and Creator, each only when allowed. Every row maps to
// the modifier app.slack.com adds (captured 2026-11-08).
func (v *SearchView) moreMenu(gtx layout.Context) {
	th := v.h.Theme()
	c := v.h.Core()
	laterTodos := c != nil && c.Experiment("later_todos") == "on"
	visible := v.visible
	if len(visible) > 0 {
		visible = visible[1:] // the source pill
	} else {
		visible = srFiltersShown(v.tab)
	}
	allowed := srMoreAllowed(v.tab, visible, laterTodos)
	shown := func(f string) bool { return slices.Contains(allowed, f) }
	pref := func(name string) bool { return c != nil && c.PrefBool(name, false) }
	toggle := func(key, val string) func() { return func() { v.setQuery(v.q.Toggle(key, val)) } }
	check := func(label string, on func() bool, do func()) MenuItem {
		return MenuItem{Label: label, Widget: srCheckRow(th, label, on), Do: do, KeepOpen: true}
	}
	has := func(key, val string) func() bool { return func() bool { return v.q.Has(key, val) } }

	// people: you, then the people the results came from
	people := func(key string) []MenuItem {
		var out []MenuItem
		snap, ok := v.snapshot()
		if !ok || snap.Self.ID == "" {
			return nil
		}
		val := "<@" + snap.Self.ID + ">"
		out = append(out, check(userName(snap.Self)+" (you)", has(key, val), toggle(key, val)))
		for _, f := range v.fromSugg {
			if f.ID == snap.Self.ID || len(out) == 8 {
				continue
			}
			name := f.ID
			if u, ok := lookupUser(v.h, f.ID); ok {
				name = userName(u)
			}
			val := "<@" + f.ID + ">"
			out = append(out, check(name, has(key, val), toggle(key, val)))
		}
		return out
	}

	var items []MenuItem
	saved := shown(srFExclBots) || shown(srFOnlyMine)
	if saved {
		items = append(items, MenuItem{Header: "Saved preferences"})
	}
	if shown(srFExclBots) {
		items = append(items, check("Exclude bot messages", func() bool { return pref("search_exclude_bots") }, v.togglePref("search_exclude_bots")))
	}
	if shown(srFOnlyMine) {
		items = append(items, check("Only my channels", func() bool { return pref("search_only_my_channels") }, v.togglePref("search_only_my_channels")))
	}
	if saved {
		items = append(items, Sep())
	}
	exclArch := shown(srFExclArch) && c != nil && c.Experiment("search_explore_freshness_signals") == "on"
	if shown(srFExact) || exclArch {
		items = append(items, MenuItem{Header: "Results"})
	}
	if shown(srFExact) {
		items = append(items, check("Exact matches only", func() bool { return v.exact }, func() { v.exact = !v.exact; v.search(1) }))
	}
	if exclArch {
		items = append(items, check("Exclude archived", func() bool { return v.exclArch }, func() { v.exclArch = !v.exclArch; v.search(1) }))
	}
	if shown(srFExact) || exclArch {
		items = append(items, Sep())
	}
	if shown(srFFrom) {
		items = append(items, MenuItem{Label: "From", Icon: "user", Sub: people("from")})
	}
	if shown(srFIn) {
		var in []MenuItem
		for _, cv := range v.inChannels() {
			val := "<#" + cv.ID + ">"
			in = append(in, check(cv.Name, func() bool { return srRefID(v.q.Get("in")) == cv.ID }, toggle("in", val)))
		}
		items = append(items, MenuItem{Label: "In", Icon: "channel", Sub: in})
	}
	if shown(srFWith) {
		items = append(items, MenuItem{Label: "With people", Icon: "user", Sub: people("with")})
	}
	if shown(srFHasFile) || shown(srFHasLink) || shown(srFHasAction) {
		var sub []MenuItem
		if shown(srFHasFile) {
			sub = append(sub, check("File", has("has", "file"), toggle("has", "file")))
		}
		sub = append(sub, check("Link", has("has", "link"), toggle("has", "link")))
		if shown(srFHasAction) {
			sub = append(sub, check("Action", has("has", "action"), toggle("has", "action")))
		}
		items = append(items, MenuItem{Label: "Message has", Icon: "message", Sub: sub})
	}
	if shown(srFIsDM) || shown(srFIsThread) || shown(srFIsSaved) || shown(srFIsTodos) || shown(srFHasPin) || shown(srFIsActivity) {
		var sub []MenuItem
		if shown(srFIsDM) {
			sub = append(sub, check("A direct message", has("is", "dm"), toggle("is", "dm")))
		}
		sub = append(sub, check("In a thread", has("is", "thread"), toggle("is", "thread")))
		if shown(srFIsSaved) {
			sub = append(sub, check("Saved", has("is", "saved"), toggle("is", "saved")))
		}
		if shown(srFIsTodos) {
			sub = append(sub, check("To-dos", has("is", "todos"), toggle("is", "todos")))
		}
		if shown(srFHasPin) {
			sub = append(sub, check("Pinned", has("has", "pin"), toggle("has", "pin")))
		}
		if shown(srFIsActivity) {
			sub = append(sub, check("In Activity", has("in", "activity"), toggle("in", "activity")))
		}
		items = append(items, MenuItem{Label: "Message is", Icon: "message", Sub: sub})
	}
	if shown(srFDate) {
		items = append(items, MenuItem{Label: "Date", Icon: "calendar",
			Subtitle: srDateLabel(v.q.Get("after"), v.q.Get("before"), v.q.Get("on"), time.Now()), Sub: v.dateItems(gtx, srPMore)})
	}
	if shown(srFFileType) {
		items = append(items, MenuItem{Label: "File type", Icon: "file", Sub: v.fileTypeItems()})
	}
	if shown(srFReaction) {
		var reacts []MenuItem
		for _, r := range srTopReactions {
			val := ":" + r + ":"
			reacts = append(reacts, check(val, has("has", val), toggle("has", val)))
		}
		reacts = append(reacts, MenuItem{Label: "More reactions", Do: func() {
			anchor := v.win(gtx, v.pillR[srPMore])
			v.h.OpenEmojiPicker(image.Pt(anchor.Min.X, anchor.Max.Y+dp(gtx, 4)), func(name string) {
				if name = strings.Trim(name, ":"); name != "" {
					v.setQuery(v.q.Toggle("has", ":"+name+":"))
				}
			})
		}})
		items = append(items, MenuItem{Label: "Reactions", Icon: "emoji", Sub: reacts})
	}
	if shown(srFCreator) {
		items = append(items, MenuItem{Label: "Creator", Icon: "user", Sub: people("creator")})
	}
	v.openMenu(gtx, v.pillR[srPMore], items, 240) // MoreFiltersMenu width 240
}

// ---- the other tabs' sorts (search-filter-sort FileEntity, CanvasEntity,
// ChannelEntity, PeopleEntity) ----

// srTabSortOpt is a Sort select option.
type srTabSortOpt struct{ label, val string }

// srTabHasSort: the tabs BaseSearchFilterSort draws a Sort select for
// (not DMs).
func srTabHasSort(tab int) bool {
	switch tab {
	case srTabMessages, srTabFiles, srTabCanvases, srTabChannels, srTabPeople:
		return true
	}
	return false
}

// srTabSorts are a tab's options before the divider and Edit default;
// relevant is "Most relevant", with " (default)" unless stick_user_sort.
func srTabSorts(tab int) []srTabSortOpt {
	switch tab {
	case srTabFiles, srTabCanvases:
		o := []srTabSortOpt{{"Most relevant", "score"}, {"Oldest", "timestamp_asc"}, {"Newest", "created"},
			{"A to Z", "name"}, {"Z to A", "name_desc"}}
		if tab == srTabCanvases {
			o = append(o, srTabSortOpt{"Last updated", "timestamp"})
		}
		return o
	case srTabChannels:
		return []srTabSortOpt{{"Most relevant", "relevant"}, {"Newest", "created"}, {"Oldest", "created_asc"},
			{"Most members", "member_count"}, {"Fewest members", "member_count_asc"}, {"A to Z", "name"}, {"Z to A", "name_desc"}}
	case srTabPeople:
		return []srTabSortOpt{{"Most relevant", "relevant"}, {"A to Z", "name"}, {"Z to A", "name_desc"}}
	}
	return nil
}

// srTabSortPref is the pref a sticky sort is kept in (setFileSearchSort
// for files and canvases; search_channel_sort, search_people_sort).
func srTabSortPref(tab int) string {
	switch tab {
	case srTabFiles, srTabCanvases:
		return "search_file_sort"
	case srTabChannels:
		return "search_channel_sort"
	case srTabPeople:
		return "search_people_sort"
	}
	return ""
}

// srTabSortValues are the values a tab's sort pref may hold (cX/T: VU
// for files and canvases, aJ for channels, iO for people).
func srTabSortValues(tab int) []string {
	switch tab {
	case srTabFiles, srTabCanvases:
		return []string{"score", "timestamp", "timestamp_asc", "name", "name_desc", "last_read", "last_engaged", "created"}
	case srTabChannels:
		return []string{"relevant", "name", "created", "created_asc", "member_count", "member_count_asc", "name_desc"}
	case srTabPeople:
		return []string{"relevant", "name", "name_desc"}
	}
	return nil
}

// srDefaultTabSort is a tab's sort when a search starts: with
// stick_user_sort its pref when it holds one of the tab's values (kwsV r,
// mfHU Kn / y6), else Most relevant. Files take any file sort, Last
// updated too, though their select only names it Most relevant.
func srDefaultTabSort(c interface {
	PrefBool(string, bool) bool
	PrefString(string) string
}, tab int) string {
	opts := srTabSorts(tab)
	if len(opts) == 0 {
		return ""
	}
	if c != nil && c.PrefBool("stick_user_sort", false) {
		if p := c.PrefString(srTabSortPref(tab)); slices.Contains(srTabSortValues(tab), p) {
			return p
		}
	}
	return opts[0].val
}

// tabSortLabel is the selected option's label (Most relevant when the
// value isn't one of the tab's, as FileEntity falls back).
func (v *SearchView) tabSortLabel() string {
	c := v.h.Core()
	sticky := c != nil && c.PrefBool("stick_user_sort", false)
	opts := srTabSorts(v.tab)
	for i, o := range opts {
		if o.val == v.tabSort[v.tab] {
			if i == 0 && !sticky {
				return o.label + " (default)"
			}
			return o.label
		}
	}
	if !sticky {
		return "Most relevant (default)"
	}
	return "Most relevant"
}

// tabSortMenu is a non-message tab's Sort select: its options, a
// divider, Edit default. With stick_user_sort the choice is also saved in
// the tab's pref.
func (v *SearchView) tabSortMenu(gtx layout.Context) {
	c := v.h.Core()
	sticky := c != nil && c.PrefBool("stick_user_sort", false)
	tab := v.tab
	var items []MenuItem
	for i, o := range srTabSorts(tab) {
		label, val := o.label, o.val
		if i == 0 && !sticky {
			label += " (default)"
		}
		items = append(items, MenuItem{Label: label, Checked: v.tabSort[tab] == val, Do: func() {
			if v.tabSort[tab] != val {
				v.tabSort[tab] = val
				if sticky && (tab == srTabFiles || tab == srTabCanvases) {
					// both read search_file_sort (kwsV r)
					v.tabSort[srTabFiles], v.tabSort[srTabCanvases] = val, val
				}
				v.search(1)
			}
			if sticky {
				pref := srTabSortPref(tab)
				run(v.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, c.SetPref(ctx, pref, val) },
					func(_ struct{}, err error) {
						if err != nil {
							v.h.Toast("Couldn't save that preference: " + errText(err))
						}
					})
			}
		}})
	}
	items = append(items, Sep(), MenuItem{Label: "Edit default", Do: func() {
		if OpenPreferencesSection != nil {
			OpenPreferencesSection("advanced")
		}
	}})
	v.openMenu(gtx, v.sortR, items, 240)
}

// srWithoutFilters is RelaxationSuggestion's click: the query with every
// filter Slack clears (in, from, with, creator, mentions, has, is, my
// reactions, file type, before, after, on) taken out; the words stay.
func srWithoutFilters(q srQuery) srQuery {
	clear := map[string]bool{"in": true, "from": true, "with": true, "creator": true, "to": true, "has": true, "is": true,
		"hasmy": true, "type": true, "before": true, "after": true, "on": true, "during": true}
	var mods []srMod
	for _, m := range q.Mods {
		if !clear[m.Key] {
			mods = append(mods, m)
		}
	}
	return q.withMods(mods)
}
