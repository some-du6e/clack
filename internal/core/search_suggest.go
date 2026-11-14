package core

// The window's search filter suggestions, as Slack's client keeps them
// (searchResults[windowId].suggestions; client-boot rZ6M parseSuggestions,
// yLIV q7 getFilterSuggestions / Gb, build 2c0d02ca130f). Every new search
// (A+yj "Set a request from the editable request": fetchSuggestions
// defaults to true, the messages module's answer) merges the answer's
// filter_suggestions into the last ones: the request's own in: and from:
// first (checked, no count), then the five with the most results among
// the answer's and the last ones, one per id. Activity's In picker lists
// the "in" ones with no query (IKpm ActivityFilterBySpecific "Suggestions",
// views/activity_in.go), and three loading rows while a messages search
// is in flight with none yet (6/pc mL).

import (
	"regexp"
	"sort"
	"sync"
	"sync/atomic"
)

type searchSuggState struct {
	mu       sync.Mutex
	in, from []FilterSuggestion
	loading  atomic.Int32 // messages searches in flight
}

// searchSuggTop is parseSuggestions' take(…, 5).
const searchSuggTop = 5

// The request's in: and from: filters (d4lr vA's queryFilter): the
// conversation or person each names; a negated one (-in:) isn't one.
var (
	suggInRef   = regexp.MustCompile(`(?i)(?:^|\s)in:<[#@]?([A-Z0-9]+)[^>]*>`)
	suggFromRef = regexp.MustCompile(`(?i)(?:^|\s)from:<@([A-Z0-9]+)[^>]*>`)
)

func suggFilterIDs(re *regexp.Regexp, q string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(q, -1) {
		out = append(out, m[1])
	}
	return out
}

// noteSuggestions is parseSuggestions for a messages search of query
// that answered res: from drops the checked people and you; in is the
// answer's (search_exclude_channels already dropped, withoutExcluded).
func (a *App) noteSuggestions(query string, res SearchResult) {
	self := a.Self().ID
	var from []FilterSuggestion
	for _, s := range res.FilterFrom {
		if !s.Checked && s.ID != self {
			from = append(from, s)
		}
	}
	st := &a.ssugg
	st.mu.Lock()
	defer st.mu.Unlock()
	st.from = suggMerge(suggFilterIDs(suggFromRef, query), from, st.from)
	st.in = suggMerge(suggFilterIDs(suggInRef, query), res.FilterIn, st.in)
}

// suggMerge is uniqBy(concat(checked, take(uniqBy(sortBy(concat(fresh,
// prev), -facet_count), "id"), 5)), "id"): lodash's sortBy is stable, and
// uniqBy keeps the first of each id.
func suggMerge(checked []string, fresh, prev []FilterSuggestion) []FilterSuggestion {
	pool := append(append([]FilterSuggestion(nil), fresh...), prev...)
	sort.SliceStable(pool, func(i, j int) bool { return pool[i].Count > pool[j].Count })
	top := suggUniq(pool)
	if len(top) > searchSuggTop {
		top = top[:searchSuggTop]
	}
	out := make([]FilterSuggestion, 0, len(checked)+len(top))
	for _, id := range checked {
		out = append(out, FilterSuggestion{ID: id, Checked: true})
	}
	return suggUniq(append(out, top...))
}

func suggUniq(ss []FilterSuggestion) []FilterSuggestion {
	seen := map[string]bool{}
	out := ss[:0:0]
	for _, s := range ss {
		if !seen[s.ID] {
			seen[s.ID] = true
			out = append(out, s)
		}
	}
	return out
}

// SearchSuggestions is getFilterSuggestions for the window: the in and
// from suggestions the searches so far left (none before the first).
func (a *App) SearchSuggestions() (in, from []FilterSuggestion) {
	st := &a.ssugg
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]FilterSuggestion(nil), st.in...), append([]FilterSuggestion(nil), st.from...)
}

// SearchMessagesLoading is 6/pc mL: the messages module is loading (a
// messages search is in flight).
func (a *App) SearchMessagesLoading() bool { return a.ssugg.loading.Load() > 0 }
