package core

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/some-du6e/clack/internal/acrank"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Slack's sidebar text filter, ported from its web client (build
// 4d0c78085232, 2026-11-10; public CDN chunks):
//
//   - sidebar_text_filter (client-boot O2By isSidebarTextFilterEnabled,
//     export ZN) puts SidebarTextFilterInput under the sidebar header
//     (client-channel-list-view SYcU). What is typed reaches the rows 150
//     ms later (boot-render Yuqx SidebarTextFilterProvider's
//     debouncedFilterText).
//   - with a non-empty filter, use_home_channel_sidebar_visible_rows
//     (boot-render B7kx, its tD memo) keeps, in each section, the
//     conversations it holds before the section's own filter
//     (channelsAssociatedOrdered) whose name matches; sections without a
//     match go, the rest show expanded with nothing hidden.
//   - names (lJBI channelNamesMap): a channel's name, a DM partner's
//     pref-compliant name, a group DM's other members' names sorted and
//     joined with "," (under mpdm_naming a renamed one is "<name>
//     <members>"). Matching is bFRk: lowercased, "-" and " " removed,
//     substring.
//   - sidebar_text_filter_m2 (export sO) also offers closed DMs and group
//     DMs (lJBI closedDmChannelIds) whose member names match (/HdK
//     useClosedDmFilterMatches), in Direct messages, sorted by their read
//     cursor, newest first; focusing the input loads sidebar.dms first when
//     it hasn't answered (SYcU createThunkFetchSidebarDms).
//   - sidebar_text_filter_m3 (export Nv) matches with Fyfu
//     createSidebarFuzzyFilter instead: Slack's FuzzyMatcher (fuzzy limit
//     3) or the query's dashed words inside the name's, and a group DM
//     also matches when every word of the query matches one of its
//     members.

// Clack's experiment names for the filter (TestExperimentReadsListed).
const (
	ExpSidebarTextFilter   = "sidebar_text_filter"
	ExpSidebarTextFilterM2 = "sidebar_text_filter_m2"
	ExpSidebarTextFilterM3 = "sidebar_text_filter_m3"
)

// SidebarTextFilterOn is isSidebarTextFilterEnabled (O2By ZN).
func (a *App) SidebarTextFilterOn() bool { return a.experiment(ExpSidebarTextFilter) == "on" }

// SidebarTextFilterM2 is O2By sO: the filter is on and so is m2.
func (a *App) SidebarTextFilterM2() bool {
	return a.SidebarTextFilterOn() && a.experiment(ExpSidebarTextFilterM2) == "on"
}

// SidebarTextFilterM3 is O2By Nv: the filter is on and so is m3.
func (a *App) SidebarTextFilterM3() bool {
	return a.SidebarTextFilterOn() && a.experiment(ExpSidebarTextFilterM3) == "on"
}

// normalizeSidebarFilterText is bFRk $.
func normalizeSidebarFilterText(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "-", "")
	return strings.ReplaceAll(s, " ", "")
}

// sidebarFilterMatch is bFRk U: norm is normalizeSidebarFilterText(query).
func sidebarFilterMatch(name, norm string) bool {
	return strings.Contains(normalizeSidebarFilterText(name), norm)
}

// sepRun is `${FuzzyMatcher uT.source}+` with the g and u flags.
var sepRun = regexp.MustCompile(`[- _'.\x{2019}\x{200b}()/\x{FF0F}]+`)

// toDashedWords is Fyfu's: "-" and the normalized text, every run of the
// matcher's separators one "-".
func toDashedWords(s string) string {
	return sepRun.ReplaceAllString("-"+acrank.Normalize(s), "-")
}

// sidebarNameMatcher is Fyfu createMatcher.
func sidebarNameMatcher(q string) func(string) bool {
	m := acrank.NewMatcher(q, acrank.MatcherOpts{FuzzyLimit: 3})
	words := toDashedWords(q)
	some := strings.ReplaceAll(words, "-", "") != ""
	return func(name string) bool {
		return some && strings.Contains(toDashedWords(name), words) || m.NormalizeAndScore(name) <= 3
	}
}

// fuzzySplit is Fyfu's member separator /[,，、､| ]/.
var fuzzySplit = regexp.MustCompile(`[,\x{FF0C}\x{3001}\x{FF64}| ]`)

// sidebarFuzzyFilter is Fyfu createSidebarFuzzyFilter.
type sidebarFuzzyFilter struct {
	name  func(string) bool
	words []func(string) bool
}

func newSidebarFuzzyFilter(q string) *sidebarFuzzyFilter {
	f := &sidebarFuzzyFilter{name: sidebarNameMatcher(q)}
	for _, w := range fuzzySplit.Split(q, -1) {
		if w = strings.TrimPrefix(w, "@"); w != "" {
			f.words = append(f.words, sidebarNameMatcher(w))
		}
	}
	return f
}

// matchesMpimMembers: every word of the query matches one of members.
func (f *sidebarFuzzyFilter) matchesMpimMembers(members []string) bool {
	if len(f.words) == 0 {
		return false
	}
	for _, w := range f.words {
		hit := false
		for _, m := range members {
			if w(m) {
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

// FilterNames is what the text filter matches against, from the snapshot.
type FilterNames struct {
	// Names is lJBI's channelNamesMap for every sidebar candidate.
	Names map[string]string
	// Members is mpimMemberNamesMap: a group DM's other members' names,
	// sorted.
	Members map[string][]string
	// Closed is closedDmChannelIds: DMs and group DMs that are closed, read
	// and have no draft or huddle, in the store's order.
	Closed []string
	// ClosedNames and ClosedMembers are /HdK's resolveMemberName results:
	// a closed DM's partner, a closed group DM's other members (sorted, and
	// joined with "," in ClosedNames).
	ClosedNames   map[string]string
	ClosedMembers map[string][]string
	// LastRead is each conversation's read cursor (zJU5 getAllLastReads),
	// the m2 order.
	LastRead map[string]string
}

// FilterSection is one section for FilterSidebar: in, Rows is every
// conversation it holds before its own filter; out, the matches.
type FilterSection struct {
	ID, Type string
	Rows     []string
}

// FilterSidebar is B7kx's tD: nil when query is blank (no filter), else
// the sections with a match, in order, each with its matching rows (the
// m2 closed DMs go into Direct messages, which is appended at the end
// when nothing else in it matched). An empty, non-nil result is
// TextFilterNoResultsRow's case.
func FilterSidebar(secs []FilterSection, query string, m2, m3 bool, n FilterNames) []FilterSection {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	var fuzzy *sidebarFuzzyFilter
	var norm string
	match := func(name string) bool { return sidebarFilterMatch(name, norm) }
	if m3 {
		fuzzy = newSidebarFuzzyFilter(q)
		match = fuzzy.name
	} else {
		norm = normalizeSidebarFilterText(q)
	}
	out := []FilterSection{}
	for _, s := range secs {
		var rows []string
		for _, id := range s.Rows {
			if name := n.Names[id]; name != "" && match(name) {
				rows = append(rows, id)
				continue
			}
			if fuzzy != nil && len(n.Members[id]) > 0 && fuzzy.matchesMpimMembers(n.Members[id]) {
				rows = append(rows, id)
			}
		}
		if len(rows) > 0 {
			out = append(out, FilterSection{ID: s.ID, Type: s.Type, Rows: rows})
		}
	}
	if !m2 {
		return out
	}
	closed := closedDMMatches(q, m3, n)
	if len(closed) == 0 {
		return out
	}
	// the DM section's matches and the closed ones, by read cursor (newest
	// first), then by name (QJub)
	names := map[string]string{}
	for id, name := range closed {
		names[id] = name
	}
	dmAt := -1
	var have []string
	for i, s := range out {
		if s.Type == "direct_messages" {
			dmAt, have = i, s.Rows
			break
		}
	}
	for _, id := range have {
		if name := n.Names[id]; name != "" {
			names[id] = name
		}
	}
	var ids []string
	seen := map[string]bool{}
	add := func(id string) {
		if names[id] != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, id := range have {
		add(id)
	}
	for _, id := range n.Closed { // ta: Object.keys of the matches, in candidate order
		if _, ok := closed[id]; ok {
			add(id)
		}
	}
	col := collate.New(language.AmericanEnglish, collate.Numeric)
	sort.SliceStable(ids, func(i, j int) bool {
		ri, rj := n.LastRead[ids[i]], n.LastRead[ids[j]]
		if ri == "" {
			ri = "0"
		}
		if rj == "" {
			rj = "0"
		}
		if ri != rj {
			return ri > rj // string order, as Slack compares the cursors
		}
		return qjubLess(col, names[ids[i]], names[ids[j]])
	})
	if dmAt >= 0 {
		out[dmAt].Rows = ids
		return out
	}
	for _, s := range secs { // nothing open matched: the DM section, last
		if s.Type == "direct_messages" {
			return append(out, FilterSection{ID: s.ID, Type: s.Type, Rows: ids})
		}
	}
	return out
}

// qjubLess is QJub's sorter: Intl.Collator("en-US", {numeric: true}), an
// empty name first.
func qjubLess(col *collate.Collator, a, b string) bool {
	if a == b {
		return false
	}
	if a == "" || b == "" {
		return a == ""
	}
	return col.CompareString(a, b) < 0
}

// closedDMMatches is /HdK useClosedDmFilterMatches: the closed DMs whose
// partner, or closed group DMs whose members, match; id -> name.
func closedDMMatches(q string, m3 bool, n FilterNames) map[string]string {
	var fuzzy *sidebarFuzzyFilter
	norm := ""
	if m3 {
		fuzzy = newSidebarFuzzyFilter(q)
	} else {
		norm = normalizeSidebarFilterText(q)
	}
	out := map[string]string{}
	for _, id := range n.Closed {
		name := n.ClosedNames[id]
		if name == "" {
			continue
		}
		if members, ok := n.ClosedMembers[id]; ok { // a group DM
			if fuzzy != nil && fuzzy.matchesMpimMembers(members) || fuzzy == nil && sidebarFilterMatch(name, norm) {
				out[id] = name
			}
			continue
		}
		if fuzzy != nil && fuzzy.name(name) || fuzzy == nil && sidebarFilterMatch(name, norm) {
			out[id] = name
		}
	}
	return out
}

// prefMemberName is getPrefCompliantMemberNamePure (client-boot vUCu
// oj): the real name under display_real_names, else the display name,
// falling back to the real name.
func prefMemberName(u *slack.User, realNames bool) string {
	if u == nil {
		return ""
	}
	if !realNames {
		if n := strings.TrimSpace(u.Profile.DisplayName); n != "" {
			return n
		}
	}
	for _, n := range []string{u.Profile.RealName, u.RealName} {
		if n = strings.TrimSpace(n); n != "" && n != "​" {
			return n
		}
	}
	return store.DisplayName(u)
}

// filterNames builds FilterNames for the snapshot: the sidebar's
// candidates (every conversation a section holds) and the closed DMs.
func (a *App) filterNames(convs []Conv, secs []Section, counts map[string]Count) FilterNames {
	n := FilterNames{Names: map[string]string{}, Members: map[string][]string{}, ClosedNames: map[string]string{},
		ClosedMembers: map[string][]string{}, LastRead: map[string]string{}}
	a.S.RLock()
	realNames := a.S.Prefs.DisplayRealNames
	self := a.S.Self.ID
	a.S.RUnlock()
	col := collate.New(language.AmericanEnglish, collate.Numeric)
	member := func(id string) string { // resolveMemberName: nothing when unknown
		u, ok := a.S.User(id)
		if !ok {
			return ""
		}
		if n := prefMemberName(u, realNames); n != id {
			return n
		}
		return ""
	}
	others := func(c Conv) []string {
		var out []string
		for _, m := range c.Members {
			if m == self {
				continue
			}
			if name := member(m); name != "" {
				out = append(out, name)
			}
		}
		col.SortStrings(out)
		return out
	}
	byID := make(map[string]Conv, len(convs))
	for _, c := range convs {
		byID[c.ID] = c
	}
	inSection := map[string]bool{}
	for _, s := range secs {
		for _, id := range s.All {
			inSection[id] = true
		}
		for _, id := range s.Channels {
			inSection[id] = true
		}
	}
	for id := range inSection {
		c, ok := byID[id]
		if !ok {
			continue
		}
		switch c.Kind {
		case "im":
			n.Names[id] = member(c.User)
		case "mpim":
			ms := others(c)
			n.Members[id] = ms
			joined := strings.Join(ms, ",")
			if custom, ok := a.MpdmCustomName(id); ok {
				joined = custom + " " + joined
			}
			n.Names[id] = joined
		default:
			n.Names[id] = c.Name
		}
	}
	for _, c := range convs {
		if (c.Kind != "im" && c.Kind != "mpim") || inSection[c.ID] || !a.closedDM(c.ID) {
			continue
		}
		n.Closed = append(n.Closed, c.ID)
		if c.Kind == "im" {
			if name := member(c.User); name != "" {
				n.ClosedNames[c.ID] = name
			}
			continue
		}
		if ms := others(c); len(ms) > 0 {
			n.ClosedMembers[c.ID] = ms
			n.ClosedNames[c.ID] = strings.Join(ms, ",")
		}
	}
	for id, cnt := range counts {
		if cnt.LastRead != "" {
			n.LastRead[id] = cnt.LastRead
		}
	}
	return n
}

// closedDM is lJBI's closedDmChannelIds test for a DM or group DM: a
// candidate (not archived, not with a deleted user or a system user) that
// is closed and read. Clack keeps no draft or huddle state in core, so
// those don't reopen it here.
func (a *App) closedDM(id string) bool {
	c, ok := a.S.Conv(id)
	if !ok || c.IsArchived || !(c.IsIM || c.IsMPIM) {
		return false
	}
	if c.IsIM {
		if systemDMUsers[c.User] {
			return false
		}
		if u, ok := a.S.User(c.User); ok && u.Deleted {
			return false
		}
	}
	if a.dmOpen(id) {
		return false
	}
	e, _ := a.S.Count(id)
	return !e.HasUnreads
}

// SidebarDMsKnown reports whether sidebar.dms has answered (Slack's
// dmsTab.sidebarDmsHydrated, cLYF mu).
func (a *App) SidebarDMsKnown() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dms.known
}

// FetchSidebarDMs is createThunkFetchSidebarDms (reason
// "sidebar-text-filter-dms"): sidebar.dms, recorded the way boot records
// it, then the sidebar is rebuilt.
func (a *App) FetchSidebarDMs(ctx context.Context) error {
	dms, err := a.C.SidebarDMs(ctx)
	if err != nil {
		return err
	}
	a.setSidebarDMs(dms)
	a.emit(Event{Type: "sidebar_changed"})
	return nil
}
