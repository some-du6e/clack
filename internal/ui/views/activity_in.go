package views

// The "In" picker, as Slack's activity inbox draws it (gantry
// client-boot-render IKpm ActivityFilterBySpecific and
// ActivityFilterSearchSelect, build 4d0c78085232; docs/ACTIVITY.md
// "Filters"):
//
//   - "Specific channel or section" in the filter menu turns on the
//     view's showFilterIn: the bar gets the In pill after the filter
//     button, and its popover opens (m+ho te: e6("showFilterIn"), ec(true)).
//     A saved view naming channels or sections starts with it on (RY04
//     hasActiveSpecificFilters).
//   - The pill (ActivityFilterPill, p-refine_button c-filter-pill: 32px,
//     8px in, at most 109px, 13px bold, the caret at the end) reads "In",
//     "In: {section}" after the section's emoji or icon, "In: {channel}",
//     or "In {n} places"; pressed (p-activity_ia4_page__filter_bar--active)
//     on the All tab while it names any.
//   - The popover (280px, a search input "Ex. #project-unicorn", multi
//     select rows of 32px with their checkbox, 25px headings) lists what's
//     picked, "Clear all" past one, "Recent" (the channel last open and its
//     section, wv6G PA / YE) and, with a query, the matching sections and
//     conversations (useSearchQuery "activity-filters-in-filter": channels
//     and group DMs, people as their DMs, sections; up to 150). A pick
//     toggles the conversation or section in the view's filters and the
//     feed loads again; "Clear all" drops them all
//     (clearChannelsAndSections); closing it with nothing picked turns the
//     picker off again.
//   - With no query, after "Recent", "Suggestions": the window's last
//     searches' "in" suggestions (yLIV Gb: searchResults[window]
//     .suggestions.in, core.SearchSuggestions), those the store knows (a
//     conversation, or a person: getChannelOrMemberById) and not picked,
//     each labelled as its entity; three loading rows (BaseMissingChannel)
//     while a messages search runs with none yet (6/pc mL). A person
//     picked filters by their DM (WJ+z addFilterChannelId:
//     getDmIdByUserId, else the person's id). The new view's "In" lists
//     the same (fNIs: the same ActivityFilterBySpecific).

import (
	"image"
	"slices"
	"sort"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// actInPicker is ActivityFilterBySpecific's state in the bar.
type actInPicker struct {
	pick     dirPick
	inited   bool
	openSoon bool // the menu's "Specific channel or section" opens it
	pill     image.Point
	opts     []dirPickOpt // the rows the popover lists (for tests)
}

func (p *actInPicker) init() {
	if !p.inited {
		p.pick = newDirPickIn(nsActivity, "Ex. #project-unicorn", true)
		p.inited = true
	}
}

// actInSectionPrefix marks a section's value in the options (Slack tells
// them apart by id: sZOa isChannelSectionId).
const actInSectionPrefix = "section:"

// inSelected is the view's picked conversations, then its sections
// (q3Ev $ / fNIs B: filterChannels.concat(filterChannelSections)).
func (v *Activity) inSelected() []string {
	out := append([]string(nil), v.channels...)
	for _, s := range v.sections {
		out = append(out, actInSectionPrefix+s)
	}
	return out
}

// inClicks handles the picker; it reports whether the feed must load
// again.
func (v *Activity) inClicks(gtx C) bool {
	p := &v.in
	p.init()
	if !v.showIn {
		p.pick.open = false
		return false
	}
	if p.openSoon {
		p.openSoon = false
		if !p.pick.open {
			p.pick.toggle()
		}
	}
	wasOpen := p.pick.open
	o, ok := p.pick.update(gtx)
	reload := false
	if ok {
		reload = v.inToggle(o)
	}
	if wasOpen && !p.pick.open && len(v.channels) == 0 && len(v.sections) == 0 {
		// onPopoverClose with nothing picked: clearChannelsAndSections
		// (m+ho tn: showFilterIn off)
		v.showIn = false
	}
	return reload
}

// inToggle applies a pick: Clear all, or one conversation or section on
// or off (P0K2's filterChannels / filterChannelSections toggles).
func (v *Activity) inToggle(o dirPickOpt) bool {
	switch {
	case o.clear:
		if len(v.channels) == 0 && len(v.sections) == 0 {
			return false
		}
		v.channels, v.sections = nil, nil
		return true
	case strings.HasPrefix(o.value, actInSectionPrefix):
		id := strings.TrimPrefix(o.value, actInSectionPrefix)
		v.sections = actToggleID(v.sections, id)
		return true
	case o.value != "":
		v.channels = actToggleID(v.channels, v.inFilterID(o.value))
		return true
	}
	return false
}

// inFilterID is addFilterChannelId (WJ+z): a person's id becomes their
// DM's (getDmIdByUserId), or stays when there's none.
func (v *Activity) inFilterID(id string) string {
	if !actLooksLikeUser(id) {
		return id
	}
	for _, cv := range v.h.Core().Snapshot().Convs {
		if cv.Kind == "im" && cv.User == id {
			return cv.ID
		}
	}
	return id
}

// actLooksLikeUser is 5oKS's user id test (U… or W…).
func actLooksLikeUser(id string) bool {
	return len(id) > 1 && (id[0] == 'U' || id[0] == 'W')
}

func actToggleID(list []string, id string) []string {
	if i := slices.Index(list, id); i >= 0 {
		return slices.Delete(slices.Clone(list), i, i+1)
	}
	return append(slices.Clone(list), id)
}

// inOptions is the popover's list (IKpm ew, or the query's results).
func (v *Activity) inOptions() []dirPickOpt {
	snap := v.h.Core().Snapshot()
	sel := v.inSelected()
	if q := v.in.pick.query(); q != "" {
		return v.inSearch(snap, q)
	}
	return v.inBlank(snap, sel)
}

// inBlank is the popover's list with no query (IKpm ex): what's picked,
// "Clear all" past one, "Recent", then "Suggestions".
func (v *Activity) inBlank(snap core.Snapshot, sel []string) []dirPickOpt {
	c := v.h.Core()
	picked := map[string]bool{}
	for _, s := range sel {
		picked[s] = true
	}
	var out []dirPickOpt
	for _, s := range sel {
		out = append(out, dirPickOpt{value: s, label: v.inLabel(snap, s)})
	}
	if len(sel) > 1 {
		out = append(out, dirPickOpt{value: "clear", label: nsActivity.T("Clear all"), clear: true})
	}
	// Recent: the channel last open (wv6G PA) and its section (YE: Starred
	// when it's starred, else the section that lists it)
	var recent []dirPickOpt
	if ch := popups.LatestConversation(); ch != "" {
		if _, ok := c.Conv(ch); ok {
			if !picked[ch] {
				recent = append(recent, dirPickOpt{value: ch, label: v.inLabel(snap, ch), group: true})
			}
			if sec := actSectionOf(snap, ch); sec != "" && !picked[actInSectionPrefix+sec] {
				recent = append(recent, dirPickOpt{value: actInSectionPrefix + sec, label: v.inLabel(snap, actInSectionPrefix+sec), group: true})
			}
		}
	}
	if len(recent) > 0 {
		out = append(out, dirPickOpt{value: "recent", label: nsActivity.T("Recent"), header: true})
		out = append(out, recent...)
	}
	// Suggestions (ek): the window's in suggestions the store knows, not
	// picked; the loading rows (J) while a messages search runs without any
	var sugg []dirPickOpt
	in, _ := c.SearchSuggestions()
	for _, s := range in {
		if picked[s.ID] {
			continue
		}
		switch {
		case core.LooksLikeChannelID(s.ID):
			if _, ok := c.Conv(s.ID); ok {
				sugg = append(sugg, dirPickOpt{value: s.ID, label: v.inLabel(snap, s.ID), group: true})
			}
		case actLooksLikeUser(s.ID):
			if _, ok := c.S.User(s.ID); ok {
				sugg = append(sugg, dirPickOpt{value: s.ID, label: v.userName(s.ID), group: true})
			}
		}
	}
	if len(sugg) == 0 && c.SearchMessagesLoading() {
		for i := 1; i <= 3; i++ {
			sugg = append(sugg, dirPickOpt{value: "dummy-loader" + strconv.Itoa(i), loading: true, group: true})
		}
	}
	if len(sugg) > 0 {
		out = append(out, dirPickOpt{value: "Suggestions", label: nsActivity.T("Suggestions"), header: true})
		out = append(out, sugg...)
	}
	return out
}

// actSectionOf is wv6G YE: Starred for a starred conversation, else the
// first section that lists it.
func actSectionOf(snap core.Snapshot, ch string) string {
	for _, s := range snap.Sections {
		if s.Type == "stars" && (slices.Contains(s.All, ch) || slices.Contains(s.Channels, ch)) {
			return s.ID
		}
	}
	for _, s := range snap.Sections {
		if slices.Contains(s.All, ch) || slices.Contains(s.Channels, ch) {
			return s.ID
		}
	}
	return ""
}

// inSearch is the query's results: sections, then conversations (people
// as their DMs), the names that start with the query first.
func (v *Activity) inSearch(snap core.Snapshot, q string) []dirPickOpt {
	type hit struct {
		opt    dirPickOpt
		prefix bool
	}
	var hits []hit
	ql := strings.ToLower(strings.TrimLeft(q, "#@"))
	match := func(name string) (bool, bool) {
		n := strings.ToLower(strings.TrimLeft(name, "#@"))
		if !dirMatch(n, ql) {
			return false, false
		}
		return true, strings.HasPrefix(n, ql)
	}
	for _, s := range snap.Sections {
		if s.ID == "" {
			continue
		}
		name := actSectionName(s)
		if ok, pre := match(name); ok {
			hits = append(hits, hit{dirPickOpt{value: actInSectionPrefix + s.ID, label: name}, pre})
		}
	}
	for _, cv := range snap.Convs {
		name := v.convTitle(cv.ID, nil)
		if ok, pre := match(name); ok {
			hits = append(hits, hit{dirPickOpt{value: cv.ID, label: name}, pre})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].prefix && !hits[j].prefix })
	if len(hits) > 150 {
		hits = hits[:150]
	}
	out := make([]dirPickOpt, len(hits))
	for i, h := range hits {
		out[i] = h.opt
	}
	return out
}

// actSectionName is a section's name ("Starred" for the stars section:
// p4Ef yF).
func actSectionName(s core.Section) string {
	if s.Type == "stars" && s.Name == "" {
		return nsSidebar.T("Starred")
	}
	return s.Name
}

// inLabel names a value: a section's name, a conversation's title.
func (v *Activity) inLabel(snap core.Snapshot, val string) string {
	if id, ok := strings.CutPrefix(val, actInSectionPrefix); ok {
		for _, s := range snap.Sections {
			if s.ID == id {
				return actSectionName(s)
			}
		}
		return id
	}
	return v.convTitle(val, nil)
}

// inPillText is ek's trigger content: "In", "In: {name}" with one, "In
// {n} places" past one; lead is the section's emoji or icon (one section).
func (v *Activity) inPillText() (text, icon, emoji string) {
	sel := v.inSelected()
	in := nsActivity.T("In:") + "\u00a0" // ActivityFilterBySpecific: "In:", then the name
	switch len(sel) {
	case 0:
		return nsActivity.T("In"), "", ""
	case 1:
		snap := v.h.Core().Snapshot()
		if id, ok := strings.CutPrefix(sel[0], actInSectionPrefix); ok {
			for _, s := range snap.Sections {
				if s.ID == id {
					if s.EmojiURL != "" {
						return in + actSectionName(s), "", s.EmojiURL
					}
					return in + actSectionName(s), "section", ""
				}
			}
			return in + id, "section", ""
		}
		return in + v.convTitle(sel[0], nil), "", ""
	}
	return actInPlaces(len(sel)), "", ""
}

// inPill is the bar's In pill and its popover.
func (v *Activity) inPill(gtx C) D {
	p := &v.in
	p.init()
	text, icon, emoji := v.inPillText()
	active := len(v.inSelected()) > 0 && v.slot() == 0
	d := p.pick.btn.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		fg, bg, line := v.p.Text, v.p.Bg, v.p.Border
		switch {
		case active && p.pick.btn.Hovered():
			fg, bg, line = v.p.QuickHoverFg, v.p.QuickHoverBg, v.p.QuickHoverBorder
		case active:
			fg, bg, line = v.p.QuickFg, v.p.QuickBg, v.p.QuickBorder
		}
		h := gtx.Dp(actBarH)
		pad := gtx.Dp(8)
		maxW := gtx.Dp(109)
		m := op.Record(gtx.Ops)
		lead := 0
		if icon != "" || emoji != "" {
			lead = gtx.Dp(16) + gtx.Dp(4)
		}
		caret := gtx.Dp(16)
		tw := v.th.Measure(gtx, 13, wBold, text)
		w := min(maxW, max(gtx.Dp(56), pad+lead+tw+gtx.Dp(4)+caret+pad))
		func() {
			x := pad
			if lead > 0 {
				s := gtx.Dp(16)
				dirAt(gtx, image.Pt(x, (h-s)/2), func(gtx C) D {
					if emoji != "" {
						return v.emojiImg(gtx, emoji, 16)
					}
					return v.icon(gtx, icon, 16, fg)
				})
				x += lead
			}
			dirAt(gtx, image.Pt(x, 0), func(gtx C) D {
				gtx.Constraints = layout.Exact(image.Pt(max(0, w-x-pad-caret-gtx.Dp(4)), h))
				return layout.W.Layout(gtx, func(gtx C) D { return v.para(gtx, 13, 18, wBold, fg, 1, text) })
			})
			ic := "caret-down"
			if p.pick.open {
				ic = "caret-up"
			}
			dirAt(gtx, image.Pt(w-pad-caret, (h-caret)/2), func(gtx C) D { return v.icon(gtx, ic, 16, fg) })
		}()
		call := m.Stop()
		r := image.Rect(0, 0, w, h)
		rad := gtx.Dp(8)
		rrect(gtx, r, rad, bg)
		if !active && (p.pick.btn.Hovered() || p.pick.open) {
			rrect(gtx, r, rad, v.p.Hover)
		}
		border(gtx, r, rad, max(1, gtx.Dp(1)), line)
		call.Add(gtx.Ops)
		return D{Size: r.Max}
	})
	p.pill = d.Size
	if p.pick.open {
		p.opts = v.inOptions()
		picked := map[string]bool{}
		for _, s := range v.inSelected() {
			picked[s] = true
		}
		p.pick.popover(&v.base, gtx, d.Size, p.opts, func(val string) bool { return picked[val] }, nsActivity.T("Nothing found"))
	}
	return d
}

// ---- the new view's "In" (fNIs: ActivityFilterBySpecific ADD_TAB) ----

// actInField is the add-tab In: the full-width medium pill ("Channel or
// section…", 36px, 1px otl-sec, 15px regular) and its popover.
type actInField struct {
	pick   dirPick
	inited bool
	opts   []dirPickOpt // the rows the popover lists (tests)
}

func (f *actInField) init() {
	if !f.inited {
		f.pick = newDirPickIn(nsActivity, "Ex. #project-unicorn", true)
		f.inited = true
	}
}

// addTabIn is the add-tab In: label "In" (c-label), then the pill.
func (m *actViewModal) addTabIn(gtx C) D {
	v := m.v
	f := &m.in
	f.init()
	if o, ok := f.pick.update(gtx); ok {
		st := &m.st
		switch {
		case o.clear:
			st.channels, st.sections = nil, nil
		case strings.HasPrefix(o.value, actInSectionPrefix):
			st.sections = actToggleID(st.sections, strings.TrimPrefix(o.value, actInSectionPrefix))
		case o.value != "":
			st.channels = actToggleID(st.channels, v.inFilterID(o.value))
		}
		st.showIn = len(st.channels) > 0 || len(st.sections) > 0
	}
	return col(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D { return v.label(gtx, 15, wBold, v.p.Text, nsActivity.T("In")) })
		}),
		layout.Rigid(func(gtx C) D {
			sel := append(append([]string(nil), m.st.channels...), prefixAll(m.st.sections)...)
			text := nsActivity.T("Channel or section…")
			snap := v.h.Core().Snapshot()
			switch len(sel) {
			case 0:
			case 1:
				text = nsActivity.T("In:") + "\u00a0" + v.inLabel(snap, sel[0])
			default:
				text = actInPlaces(len(sel))
			}
			d := f.pick.btn.Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				w, h := gtx.Constraints.Max.X, gtx.Dp(36)
				r := image.Rect(0, 0, w, h)
				rad := gtx.Dp(8)
				rrect(gtx, r, rad, v.p.Bg)
				if f.pick.btn.Hovered() || f.pick.open {
					rrect(gtx, r, rad, v.p.Hover)
				}
				border(gtx, r, rad, max(1, gtx.Dp(1)), v.p.BorderStr)
				pad, caret := gtx.Dp(8), gtx.Dp(16)
				dirAt(gtx, image.Pt(pad, 0), func(gtx C) D {
					gtx.Constraints = layout.Exact(image.Pt(max(0, w-2*pad-caret-gtx.Dp(4)), h))
					return layout.W.Layout(gtx, func(gtx C) D { return v.para(gtx, 15, 22, wReg, v.p.Text, 1, text) })
				})
				ic := "caret-down"
				if f.pick.open {
					ic = "caret-up"
				}
				dirAt(gtx, image.Pt(w-pad-caret, (h-caret)/2), func(gtx C) D { return v.icon(gtx, ic, 16, v.p.Text) })
				return D{Size: r.Max}
			})
			if f.pick.open {
				picked := map[string]bool{}
				for _, s := range sel {
					picked[s] = true
				}
				var opts []dirPickOpt
				if q := f.pick.query(); q != "" {
					opts = v.inSearch(snap, q)
				} else {
					opts = v.inBlank(snap, sel)
				}
				f.opts = opts
				f.pick.popover(&v.base, gtx, d.Size, opts, func(val string) bool { return picked[val] }, nsActivity.T("Nothing found"))
			}
			return d
		}),
	)
}

func prefixAll(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = actInSectionPrefix + id
	}
	return out
}

// actInPlaces is ActivityFilterBySpecific's count: "In 3 places".
func actInPlaces(n int) string {
	return nsActivity.T("In {count, plural, =1 {{count} place} other {{count} places}}", "count", n)
}
