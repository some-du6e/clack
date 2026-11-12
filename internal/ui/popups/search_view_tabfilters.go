package popups

// The Channels and People tabs' filters, More's counts and the source
// pill's Slack logo, from Slack's CDN build d3de3a487b1c:
//
//   - client-boot XJW+ _p and getDefaultFiltersByEntity: Channels shows
//     All channels, Channel type, Workspace and Org as pills; People shows
//     Title, Location (Workspace with enterprise options, Department with
//     a SCIM department field), and keeps Account type, Org and
//     Celebration for More. Clack draws the Organizations pill and More's
//     Organization row nowhere: they list the connected orgs (xlY+ fetches
//     them, then sends external_shared_team_ids), which core doesn't load.
//   - gantry search-filters_tsx (HCYG): SearchFilterAllChannels (a6: All
//     channels, Channels you’ve joined, Other channels; the
//     search_only_my_channels and search_hide_my_channels prefs),
//     SearchFilterChannelTypes (sD/tw: Any channel type, Public, Private,
//     Archived, External), SearchFilterWorkspace (kGy0: All workspaces,
//     then the enterprise's teams), SearchFilterLocation (K4vW) and
//     SearchFilterTitle (4TgK): the chosen values, Clear all past one,
//     then the first five searchSharedFacets not chosen under
//     Suggestions; getFilterTriggerContent's labels ("Location: X",
//     "Multiple locations"), useFilterActiveState's getMoreFiltersCount
//     ("More (N)") and MoreFiltersDropdown's blue badgeCounts.
//   - SearchFilterSourcesDisplay draws slack_logo_mark-8890ba0.svg
//     (appIconSmall: 15px) before every Slack source's name.

import (
	"context"
	"image"
	"image/color"
	"slices"
	"strconv"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
)

// "Other channels" is slack_connect_hub_next's (a6).
var nsSCHub = i18n.NS("slack_connect_hub_next")

// More filter ids for the Channels and People tabs (cX/T hv).
const (
	srFAllChannels = "filter-all-channels"
	srFChannelType = "filter-channel-type"
	srFWorkspace   = "filter-workspace"
	srFOrg         = "filter-org"
	srFTitle       = "filter-title"
	srFLocation    = "filter-location"
	srFDepartment  = "filter-department"
	srFAccountType = "filter-account-type"
	srFCelebration = "filter-celebration"
	srFDivision    = "filter-division"
)

// srChannelTypes are SearchFilterChannelTypes' options (sD) with their
// labels (getChannelTypeLabels; External is member_profile's).
var srChannelTypes = [...]struct{ val, label string }{
	{"all", "Any channel type"}, {"public", "Public"}, {"private", "Private"}, {"archived", "Archived"}, {"shared", "External"},
}

// srChannelTypeLabel is a Channel type value's label.
func srChannelTypeLabel(v string) string {
	if v == "shared" {
		return nsProfile.T("External")
	}
	for _, o := range srChannelTypes {
		if o.val == v {
			return nsSearch.T(o.label)
		}
	}
	return nsSearch.T("Any channel type")
}

// srAccountTypes are More > Account type's radio rows (b7, filtered to
// all, owners, admins, full, guests, deactivated, profile_only; Frontline
// only with frontline_users on, which the account isn't in).
var srAccountTypes = [...]struct{ val, label string }{
	{"all", "Any account type"}, {"owners", "Owners"}, {"admins", "Admins"}, {"full", "Regular members"},
	{"guests", "Guests"}, {"deactivated", "Deactivated"}, {"profile_only", "Not on Slack"},
}

// srCelebrations are More > Celebration's rows (us: New hire, Work
// anniversary) and their labelIcons.
var srCelebrations = [...]struct{ val, label, icon string }{
	{"new_hire", "New hire", "sparkles"}, {"anniversary", "Work anniversary", "emoji-celebration"},
}

// srAllChannelsLabel is SearchFilterAllChannels' selected option: Channels
// you’ve joined while search_only_my_channels is on, else Other channels
// while search_hide_my_channels is, else All channels.
func srAllChannelsLabel(onlyMine, hideMine bool) string {
	switch {
	case onlyMine:
		return nsSearch.T("Channels you’ve joined")
	case hideMine:
		return nsSCHub.T("Other channels")
	}
	return nsSearch.T("All channels")
}

func (v *SearchView) pref(name string) bool {
	c := v.h.Core()
	return c != nil && c.PrefBool(name, false)
}

// tabPillSpec is the Channels and People pills' labels
// (getFilterTriggerContent; the selects show their selected option).
func (v *SearchView) tabPillSpec(f string) (srPillSpec, bool) {
	switch f {
	case srFAllChannels:
		only, hide := v.pref("search_only_my_channels"), v.pref("search_hide_my_channels")
		return srPillSpec{label: srAllChannelsLabel(only, hide), chevron: true, active: only || hide}, true
	case srFChannelType:
		return srPillSpec{label: srChannelTypeLabel(v.chanType), chevron: true, active: v.chanType != "" && v.chanType != "all"}, true
	case srFWorkspace:
		return srPillSpec{label: nsSearch.T("Workspaces"), chevron: true}, true
	case srFTitle:
		return srMultiPill(v.titles, "Title", "Title:", "Multiple titles"), true
	case srFLocation:
		return srMultiPill(v.locs, "Location", "Location:", "Multiple locations"), true
	}
	return srPillSpec{}, false
}

// srMultiPill is a Title / Location trigger: the word, "Word: value" for
// one (a no-break space after the colon), the "Multiple …" label for more.
func srMultiPill(vals []string, empty, one, many string) srPillSpec {
	switch len(vals) {
	case 0:
		return srPillSpec{label: nsSearch.T(empty), chevron: true}
	case 1:
		return srPillSpec{label: nsSearch.T(one) + " " + vals[0], chevron: true, active: true}
	}
	return srPillSpec{label: nsSearch.T(many), chevron: true, active: true}
}

// allChannelsMenu is SearchFilterAllChannels' select. Its handler toggles
// the chosen pref (Channels you’ve joined flips search_only_my_channels,
// Other channels search_hide_my_channels) and All channels turns both
// off; Other channels is disabled for private channels.
func (v *SearchView) allChannelsMenu(gtx layout.Context) {
	only, hide := v.pref("search_only_my_channels"), v.pref("search_hide_my_channels")
	sel := "all_channels"
	if only {
		sel = "my_channels"
	} else if hide {
		sel = "other_channels"
	}
	items := []MenuItem{
		{Key: "all_channels", Label: nsSearch.T("All channels"), Radio: true, Checked: sel == "all_channels", Do: func() {
			v.setPrefs(map[string]bool{"search_only_my_channels": false, "search_hide_my_channels": false})
		}},
		{Key: "my_channels", Label: nsSearch.T("Channels you’ve joined"), Radio: true, Checked: sel == "my_channels",
			Do: v.togglePref("search_only_my_channels")},
		{Key: "other_channels", Label: nsSCHub.T("Other channels"), Radio: true, Checked: sel == "other_channels",
			Disabled: v.chanType == "private", Do: v.togglePref("search_hide_my_channels")},
	}
	v.openMenu(gtx, v.pillR[srPAllChannels], items, 240)
}

// channelTypeMenu is SearchFilterChannelTypes' select: Private is
// disabled while Other channels is on.
func (v *SearchView) channelTypeMenu(gtx layout.Context) {
	hide := v.pref("search_hide_my_channels")
	cur := v.chanType
	if cur == "" {
		cur = "all"
	}
	var items []MenuItem
	for _, o := range srChannelTypes {
		val := o.val
		items = append(items, MenuItem{Key: val, Label: srChannelTypeLabel(val), Radio: true, Checked: cur == val,
			Disabled: val == "private" && hide, Do: func() {
				if val == "all" {
					val = ""
				}
				if v.chanType != val {
					v.chanType = val
					v.search(1)
				}
			}})
	}
	v.openMenu(gtx, v.pillR[srPChanType], items, 240)
}

// workspaceMenu is SearchFilterWorkspace off Grid: All workspaces and no
// teams to pick.
func (v *SearchView) workspaceMenu(gtx layout.Context) {
	v.openMenu(gtx, v.pillR[srPWorkspace], []MenuItem{{Key: "all", Label: nsSearch.T("All workspaces"), Radio: true, Checked: true, Do: func() {}}}, 240)
}

// srFacetMenu is SearchFilterLocation / SearchFilterTitle's options: the
// chosen values (checked; a click takes one off), Clear all past one, then
// "Suggestions" with the first five facets not chosen.
func srFacetMenu(chosen []string, facets []core.Facet, toggle func(string), clear func()) []MenuItem {
	var items []MenuItem
	for _, c := range chosen {
		val := c
		items = append(items, MenuItem{Label: val, Checked: true, Do: func() { toggle(val) }})
	}
	if len(chosen) > 1 {
		items = append(items, MenuItem{Key: "clear_all", Label: nsSearch.T("Clear all"), Do: clear})
	}
	var sugg []MenuItem
	for i, f := range facets {
		if i == 5 {
			break
		}
		if f.Value == "" || slices.Contains(chosen, f.Value) {
			continue
		}
		val := f.Value
		sugg = append(sugg, MenuItem{Label: val, Do: func() { toggle(val) }})
	}
	if len(sugg) > 0 {
		items = append(items, MenuItem{Header: nsSearch.T("Suggestions")})
		items = append(items, sugg...)
	}
	if len(items) == 0 {
		items = append(items, MenuItem{Note: nsSearch.T("Nothing found")})
	}
	return items
}

func srToggleIn(list []string, v string) []string {
	if i := slices.Index(list, v); i >= 0 {
		return slices.Delete(slices.Clone(list), i, i+1)
	}
	return append(slices.Clone(list), v)
}

func (v *SearchView) titleMenu(gtx layout.Context) {
	items := srFacetMenu(v.titles, v.facetTitles, func(s string) { v.titles = srToggleIn(v.titles, s); v.search(1) },
		func() { v.titles = nil; v.search(1) })
	v.openMenu(gtx, v.pillR[srPTitle], items, 266)
}

func (v *SearchView) locationMenu(gtx layout.Context) {
	items := srFacetMenu(v.locs, v.facetLocs, func(s string) { v.locs = srToggleIn(v.locs, s); v.search(1) },
		func() { v.locs = nil; v.search(1) })
	v.openMenu(gtx, v.pillR[srPLocation], items, 266)
}

// setPrefs sets several search prefs, then searches again.
func (v *SearchView) setPrefs(prefs map[string]bool) {
	c := v.h.Core()
	if c == nil {
		return
	}
	run(v.h, func(ctx context.Context) (struct{}, error) {
		for name, val := range prefs {
			if c.PrefBool(name, false) == val {
				continue
			}
			if err := c.SetPref(ctx, name, val); err != nil {
				return struct{}{}, err
			}
		}
		return struct{}{}, nil
	}, func(_ struct{}, err error) {
		if err != nil {
			v.h.Toast("Couldn't save that preference: " + errText(err))
			return
		}
		v.search(1)
	})
}

// ---- More's counts (useFilterActiveState getMoreFiltersCount) ----

// srMoreState is what getMoreFiltersCount and the badges read.
type srMoreState struct {
	from, in, with, types, reacts, mine, creators []string
	excludeBots, onlyMine                         bool
	isDM, isThread, isSaved, isTodos, hasPin      bool
	hasFile, hasLink, hasAction, date, last7      bool
	exact, exclArch                               bool
	acctType, chanType                            string
	celebs                                        []string
	self                                          string
	dms                                           bool
}

func (v *SearchView) moreState() srMoreState {
	s := srMoreState{from: v.values("from"), in: v.values("in"), with: v.values("with"), types: v.values("type"),
		mine: v.values("hasmy"), creators: v.values("creator"), self: v.selfRef(), dms: v.tab == srTabDMs,
		excludeBots: v.pref("search_exclude_bots"), onlyMine: v.pref("search_only_my_channels"),
		exact: v.exact, exclArch: v.exclArch, acctType: v.acctType, chanType: v.chanType, celebs: v.celebs}
	for _, h := range v.values("has") {
		switch strings.ToLower(h) {
		case "pin":
			s.hasPin = true
		case "file":
			s.hasFile = true
		case "link":
			s.hasLink = true
		case "action":
			s.hasAction = true
		default:
			if strings.HasPrefix(h, ":") {
				s.reacts = append(s.reacts, h)
			}
		}
	}
	for _, is := range v.values("is") {
		switch strings.ToLower(is) {
		case "dm":
			s.isDM = true
		case "thread":
			s.isThread = true
		case "saved":
			s.isSaved = true
		case "todos":
			s.isTodos = true
		}
	}
	s.date = v.q.Get("after") != "" || v.q.Get("before") != "" || v.q.Get("on") != "" || v.q.Get("during") != ""
	s.last7 = v.q.Has("after", srLast7(time.Now())) && v.q.Get("before") == srDate(time.Now().AddDate(0, 0, 1))
	return s
}

// count is getMoreFiltersCount(allowed): each applied filter that More
// offers, the lists by their length; Includes me alone (with: you, not on
// DMs) and Last 7 days don't count.
func (s srMoreState) count(allowed []string) int {
	in := func(f string) bool { return slices.Contains(allowed, f) }
	n := 0
	add := func(on bool, f string, k int) {
		if on && in(f) {
			n += k
		}
	}
	add(s.excludeBots, srFExclBots, 1)
	add(s.onlyMine, srFOnlyMine, 1)
	add(len(s.from) > 0, srFFrom, len(s.from))
	places := 0
	activity := false
	for _, x := range s.in {
		if strings.EqualFold(x, "activity") {
			activity = true
		} else {
			places++
		}
	}
	add(len(s.in) > 0, srFIn, places)
	if len(s.with) > 0 && in(srFWith) && (s.dms || len(s.with) != 1 || s.self == "" || s.with[0] != s.self) {
		n += len(s.with)
	}
	add(len(s.reacts)+len(s.mine) > 0, srFReaction, len(s.reacts)+len(s.mine))
	add(s.isDM, srFIsDM, 1)
	add(s.isThread, srFIsThread, 1)
	add(s.isSaved, srFIsSaved, 1)
	add(s.isTodos, srFIsTodos, 1)
	add(activity, srFIsActivity, 1)
	add(s.hasPin, srFHasPin, 1)
	add(s.hasFile, srFHasFile, 1)
	add(s.hasLink, srFHasLink, 1)
	add(s.hasAction, srFHasAction, 1)
	add(s.date && !s.last7, srFDate, 1)
	add(len(s.types) > 0, srFFileType, len(s.types))
	add(s.acctType != "" && s.acctType != "all", srFAccountType, 1)
	add(s.chanType != "" && s.chanType != "all", srFChannelType, 1)
	add(len(s.celebs) > 0, srFCelebration, len(s.celebs))
	add(len(s.creators) > 0, srFCreator, len(s.creators))
	add(s.exact, srFExact, 1)
	add(s.exclArch, srFExclArch, 1)
	return n
}

// srMoreLabel is the More button's text: "More", then " (N)" (lB "More"
// and the count, ee).
func srMoreLabel(n int) string {
	if n > 0 {
		return nsSearch.T("More") + " (" + i18n.FormatNumber(i18n.Locale(), float64(n)) + ")"
	}
	return nsSearch.T("More")
}

// srBadge marks a More submenu with its selection count (blue, kept on
// the highlighted row: .p-more_filters_dropdown__menu).
func srMoreBadge(it MenuItem, n int) MenuItem {
	if n > 0 {
		it.BadgeCount, it.BadgeStyle, it.BadgeHighlighted = n, "blue", true
	}
	return it
}

// peopleMoreItems is More's Account type row (radio rows, the
// account_type values) when More offers it.
func (v *SearchView) peopleMoreItems(shown func(string) bool) []MenuItem {
	if !shown(srFAccountType) {
		return nil
	}
	cur := v.acctType
	if cur == "" {
		cur = "all"
	}
	var sub []MenuItem
	for _, o := range srAccountTypes {
		val := o.val
		sub = append(sub, MenuItem{Key: val, Label: nsSearch.T(o.label), Radio: true, Checked: cur == val, Do: func() {
			if val == "all" {
				val = ""
			}
			v.acctType = val
			v.search(1)
		}})
	}
	n := 0
	if cur != "all" {
		n = 1
	}
	return []MenuItem{srMoreBadge(MenuItem{Label: "Account type", Icon: "user", Sub: sub}.In(nsSearch), n)}
}

// celebrationItem is More's Celebration row: New hire and Work
// anniversary checkboxes with their labelIcons.
func (v *SearchView) celebrationItem() MenuItem {
	th := v.h.Theme()
	var sub []MenuItem
	for _, o := range srCelebrations {
		val, label := o.val, nsSearch.T(o.label)
		sub = append(sub, MenuItem{Key: val, Label: label, KeepOpen: true,
			Widget: srCheckIconRow(th, label, o.icon, func() bool { return slices.Contains(v.celebs, val) }),
			Do:     func() { v.celebs = srToggleIn(v.celebs, val); v.search(1) }})
	}
	return srMoreBadge(MenuItem{Label: "Celebration", Icon: "emoji-celebration", Sub: sub}.In(nsSearch), len(v.celebs))
}

// srTrues counts the true flags (a submenu's badge).
func srTrues(bs ...bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}

// ---- the source pill's Slack logo ----

// srSlackMark is the icon name Pill draws as Slack's four-colour logo.
const srSlackMark = "slack-logo-mark"

// srSlackMarkPaths are slack_logo_mark-8890ba0.svg's eight paths (a 256
// box, absolute M/L/C/Z) and fills.
var srSlackMarkPaths = [...]struct {
	d    string
	fill uint32
}{
	{"M53.79 161.37C53.79 176.22 41.75 188.26 26.89 188.26C12.04 188.26 0 176.22 0 161.37C0 146.52 12.04 134.47 26.89 134.47L53.79 134.47L53.79 161.37Z", 0xe3066a},
	{"M67.24 161.37C67.24 146.52 79.28 134.47 94.13 134.47C108.99 134.47 121.03 146.52 121.03 161.37L121.03 228.61C121.03 243.46 108.99 255.5 94.13 255.5C79.28 255.5 67.24 243.46 67.24 228.61L67.24 161.37Z", 0xe3066a},
	{"M94.13 53.79C79.28 53.79 67.24 41.75 67.24 26.89C67.24 12.04 79.28 0 94.13 0C108.99 0 121.03 12.04 121.03 26.89L121.03 53.79L94.13 53.79Z", 0x00b3ff},
	{"M94.13 67.24C108.99 67.24 121.03 79.28 121.03 94.13C121.03 108.99 108.99 121.03 94.13 121.03L26.89 121.03C12.04 121.03 0 108.99 0 94.13C0 79.28 12.04 67.24 26.89 67.24L94.13 67.24Z", 0x00b3ff},
	{"M201.71 94.13C201.71 79.28 213.75 67.24 228.61 67.24C243.46 67.24 255.5 79.28 255.5 94.13C255.5 108.99 243.46 121.03 228.61 121.03L201.71 121.03L201.71 94.13Z", 0x41b658},
	{"M188.26 94.13C188.26 108.99 176.22 121.03 161.37 121.03C146.52 121.03 134.47 108.99 134.47 94.13L134.47 26.89C134.47 12.04 146.52 0 161.37 0C176.22 0 188.26 12.04 188.26 26.89L188.26 94.13Z", 0x41b658},
	{"M161.37 201.71C176.22 201.71 188.26 213.75 188.26 228.61C188.26 243.46 176.22 255.5 161.37 255.5C146.52 255.5 134.47 243.46 134.47 228.61L134.47 201.71L161.37 201.71Z", 0xfcc003},
	{"M161.37 188.26C146.52 188.26 134.47 176.22 134.47 161.37C134.47 146.52 146.52 134.47 161.37 134.47L228.61 134.47C243.46 134.47 255.5 146.52 255.5 161.37C255.5 176.22 243.46 188.26 228.61 188.26L161.37 188.26Z", 0xfcc003},
}

// srSlackMarkDraw paints the logo px pixels square at the origin.
func srSlackMarkDraw(gtx layout.Context, px int) layout.Dimensions {
	k := float32(px) / 256
	for _, sp := range srSlackMarkPaths {
		var p clip.Path
		p.Begin(gtx.Ops)
		srSVGPath(&p, sp.d, k)
		paint.FillShape(gtx.Ops, srRGB(sp.fill), clip.Outline{Path: p.End()}.Op())
	}
	return layout.Dimensions{Size: image.Pt(px, px)}
}

func srRGB(v uint32) color.NRGBA { return rgb(v) }

// srSVGPath feeds absolute M/L/C/Z path data, scaled by k, into p.
func srSVGPath(p *clip.Path, d string, k float32) {
	f := strings.FieldsFunc(d, func(r rune) bool { return r == ' ' || r == ',' })
	var toks []string
	for _, w := range f { // split "M53.79" and "161.37C53.79" into commands and numbers
		start := 0
		for i, r := range w {
			if strings.ContainsRune("MLCZ", r) {
				if i > start {
					toks = append(toks, w[start:i])
				}
				toks = append(toks, string(r))
				start = i + 1
			}
		}
		if start < len(w) {
			toks = append(toks, w[start:])
		}
	}
	num := func(i int) float32 {
		x, _ := strconv.ParseFloat(toks[i], 32)
		return float32(x) * k
	}
	pt := func(i int) f32.Point { return f32.Pt(num(i), num(i+1)) }
	for i := 0; i < len(toks); {
		switch toks[i] {
		case "M":
			p.MoveTo(pt(i + 1))
			i += 3
		case "L":
			p.LineTo(pt(i + 1))
			i += 3
		case "C":
			p.CubeTo(pt(i+1), pt(i+3), pt(i+5))
			i += 7
		case "Z":
			p.Close()
			i++
		default:
			i++
		}
	}
}

// srMergeFacets is upsertFacet (CSKO): the kept facets, then the new
// ones whose value isn't there yet (lodash uniqBy on value).
func srMergeFacets(have, add []core.Facet) []core.Facet {
	out := slices.Clone(have)
	for _, f := range add {
		if !slices.ContainsFunc(out, func(h core.Facet) bool { return h.Value == f.Value }) {
			out = append(out, f)
		}
	}
	return out
}
